package main

import (
	"bufio"
	"crypto/sha256"
	_ "embed"
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/go-pdf/fpdf"
)

const (
	NewlineReplacement     = "¶"
	TabReplacement         = "→"
	UnprintableReplacement = "⌘"
)

func main() {
	dirPath := flag.String("dir", "", "Path to the input or output directory")
	outputPath := flag.String("out", "outfile.txt", "Path to output file when encoding. Ignored when decoding.")
	decompress := flag.String("decode", "", "If set, decode from the specified input file instead of encoding")
	pdf := flag.Bool("pdf", false, "Encode PDF file")
	flag.Parse()

	if *decompress != "" {
		if *dirPath == "" {
			fmt.Fprintln(os.Stderr, "Error: output directory path required for decoding. Use -dir <directory>")
			os.Exit(1)
		}
		f, err := os.Open(*decompress)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error opening input file: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()
		if err := decodeArchive(f, *dirPath); err != nil {
			fmt.Fprintf(os.Stderr, "Decoding error: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if *dirPath == "" {
		fmt.Fprintln(os.Stderr, "Error: input directory path required. Use -dir <directory>")
		os.Exit(1)
	}

	if *pdf && *outputPath == "outfile.txt" {
		*outputPath = "outfile.pdf"
	}

	f, err := os.Create(*outputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error creating output file: %v\n", err)
		os.Exit(1)
	}
	defer f.Close()

	var flow io.Writer = f
	if *pdf {
		r, w := io.Pipe()
		wg := &sync.WaitGroup{}
		wg.Add(1)
		go makePDF(flow, r, wg)
		flow = w
		defer func() {
			r.Close()
			wg.Wait()
		}()
	}

	err = walkAndEncode(*dirPath, flow)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Encoding error: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(`For PDF:
	Install:  brew install pandoc lualatex (macOS) // sudo apt install pandoc lualatex (Linux)
	Install: brew install --cask mactex-no-gui
	Install the fonts
	Run: to_pdf.sh `)
}

func decodeArchive(r io.Reader, baseDir string) error {
	scanner := bufio.NewScanner(r)
	scanner.Split(splitComma)
	for scanner.Scan() {
		tok := scanner.Text()
		if tok == "" {
			continue
		}
		switch tok {
		case "D":
			if !scanner.Scan() {
				return fmt.Errorf("expected directory name")
			}
			dir := filepath.Join(baseDir, unescapeCommas(scanner.Text()))
			if err := os.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("creating directory %s: %v", dir, err)
			}
		case "F":
			if !scanner.Scan() {
				return fmt.Errorf("expected file name")
			}
			filename := filepath.Join(baseDir, unescapeCommas(scanner.Text()))
			if !scanner.Scan() || !scanner.Scan() || !scanner.Scan() {
				return fmt.Errorf("expected file metadata")
			}
			if !scanner.Scan() {
				return fmt.Errorf("expected rune length")
			}
			runelen, err := strconv.Atoi(scanner.Text())
			if err != nil {
				return fmt.Errorf("invalid rune length")
			}
			f, err := os.Create(filename)
			if err != nil {
				return fmt.Errorf("creating file %s: %v", filename, err)
			}
			dec := bufio.NewWriter(f)
			rdr := decodeContent(scanner, runelen)
			if _, err := io.Copy(dec, rdr); err != nil {
				f.Close()
				return fmt.Errorf("writing decoded content: %v", err)
			}
			dec.Flush()
			f.Close()
		default:
			return fmt.Errorf("unexpected token %q", tok)
		}
	}
	return scanner.Err()
}

func splitComma(data []byte, atEOF bool) (advance int, token []byte, err error) {
	for i := 0; i < len(data); i++ {
		if data[i] == ',' {
			return i + 1, data[:i], nil
		}
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

func unescapeCommas(s string) string {
	return strings.ReplaceAll(s, "\\,", ",")
}

func decodeContent(scanner *bufio.Scanner, runelen int) io.Reader {
	pr, pw := io.Pipe()
	go func() {
		defer pw.Close()
		runesWritten := 0

		for runesWritten < runelen && scanner.Scan() {
			chunk := scanner.Text()
			i := 0
			for i < len(chunk) && runesWritten < runelen {
				if chunk[i] == UnprintableReplacement[0] {
					j := i + 1
					for j < len(chunk) && chunk[j] >= '0' && chunk[j] <= '9' {
						j++
					}
					if j >= len(chunk) || chunk[j] != ':' {
						return
					}
					size, _ := strconv.Atoi(chunk[i+1 : j])
					j++
					if j+size > len(chunk) {
						return
					}
					base64data := chunk[j : j+size]
					decoded, _ := base64.StdEncoding.DecodeString(base64data)
					pw.Write(decoded)
					i = j + size
					runesWritten += utf8.RuneCount(decoded)
				} else if chunk[i] == NewlineReplacement[0] {
					pw.Write([]byte{'\n'})
					i++
					runesWritten++
				} else if chunk[i] == TabReplacement[0] {
					pw.Write([]byte{'\t'})
					i++
					runesWritten++
				} else {
					r, size := utf8.DecodeRuneInString(chunk[i:])
					buf := make([]byte, utf8.RuneLen(r))
					utf8.EncodeRune(buf, r)
					pw.Write(buf)
					i += size
					runesWritten++
				}
			}
		}
	}()
	return pr
}

// //////////////
func walkAndEncode(root string, w io.Writer) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		relPath, _ := filepath.Rel(root, path)
		if info.IsDir() {
			if relPath != "." {
				_, err := fmt.Fprintf(w, "D,%s", escapeCommas(relPath))
				if err != nil {
					return err
				}
			}
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		hash := sha256.Sum256(data)
		shortHash := fmt.Sprintf("%x", hash[:])[:8]
		timestamp := info.ModTime().UTC().Format("2006-01-02")
		_, err = fmt.Fprintf(w, "F,%s,%d,%s,%s,", escapeCommas(relPath), len(data), timestamp, shortHash)
		if err != nil {
			return err
		}

		// Count runes first
		runeCounter := &runeCountingWriter{0}
		err = encodeData(data, runeCounter)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "%d,", runeCounter.runes)
		if err != nil {
			return err
		}

		// Write actual encoded data
		err = encodeData(data, w)
		if err != nil {
			return err
		}

		return nil
	})
}

type runeCountingWriter struct {
	runes int64
}

func (rcw *runeCountingWriter) Write(p []byte) (int, error) {
	for len(p) > 0 {
		r, size := utf8.DecodeRune(p)
		if r == utf8.RuneError && size == 1 {
			return 0, fmt.Errorf("invalid UTF-8 sequence")
		}
		rcw.runes++
		p = p[size:]
	}
	return len(p), nil
}

func escapeCommas(s string) string {
	return strings.ReplaceAll(s, ",", "\\,")
}

func encodeData(data []byte, w io.Writer) error {
	var unprintable []byte
	flushUnprintables := func() error {
		if len(unprintable) > 0 {
			enc := base64.StdEncoding.EncodeToString(unprintable)
			if _, err := fmt.Fprintf(w, "%s%d:%s", UnprintableReplacement, len(enc), enc); err != nil {
				return err
			}
			unprintable = unprintable[:0]
		}
		return nil
	}

	for _, b := range data {
		if b == '\n' {
			if err := flushUnprintables(); err != nil {
				return err
			}
			_, err := w.Write([]byte(NewlineReplacement))
			if err != nil {
				return err
			}
		} else if b == '\t' {
			if err := flushUnprintables(); err != nil {
				return err
			}
			_, err := w.Write([]byte(TabReplacement))
			if err != nil {
				return err
			}
		} else if b < 32 || b > 126 {
			unprintable = append(unprintable, b)
		} else {
			if err := flushUnprintables(); err != nil {
				return err
			}
			r, size := utf8.DecodeRune([]byte{b})
			if size > 0 {
				buf := make([]byte, utf8.RuneLen(r))
				utf8.EncodeRune(buf, r)
				_, err := w.Write(buf)
				if err != nil {
					return err
				}
			}
		}
	}
	return flushUnprintables()
}

//go:embed NotoSans-Regular.ttf
var notoSansRegular []byte

func makePDF(out io.Writer, r *io.PipeReader, wg *sync.WaitGroup) {
	defer wg.Done()
	p := fpdf.New("P", "in", "letter", "")
	p.AddUTF8FontFromBytes("noto", "", notoSansRegular)
	p.SetFont("noto", "", 9)

	// Set up footer function
	p.SetAutoPageBreak(true, 0.5) // Add 0.5 inch margin at bottom for footer
	p.SetFooterFunc(func() {
		p.SetY(-0.5) // Position at 0.5 inches from bottom
		p.SetFont("noto", "", 8)
		p.CellFormat(0, 0.2, fmt.Sprintf("Page %d", p.PageNo()), "", 0, "C", false, 0, "")
	})

	p.AddPage()
	var b [2048]byte
	for {
		data := b[:]
		n, err := r.Read(data)
		data = data[:n]
		if n > 0 {
			fmt.Print(string(data))
			p.Write(1, string(data))
		}
		if err != nil {
			if err != io.EOF {
				fmt.Println("Err reading: " + err.Error())
			}
			break
		}
	}

	if err := p.Output(out); err != nil {
		fmt.Println("Error writing file: " + err.Error())
	}
}
