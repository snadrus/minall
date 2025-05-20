This thing makes it easy to print out lots of source code with human-compatible compression:

- newlines and tabs become charecters. 
- Non-ASCII becomes a base64 encode.
- Filepaths, size (before, after), date, & sha256(partial) are written as a header

Decoder is included (and obvious), as are steps to print. 

go run minall.go -input folder_to_convert

Follow instructions given . 