pandoc outfile.txt -o output.pdf \
  --pdf-engine=lualatex \
  -V mainfont="Noto Sans" \
  -V fontsize=9pt \
  -V geometry:margin=1in \
  -H header.tex
