This thing makes it easy to print out lots of source code with human-compatible compression:

- newlines and tabs become charecters. 
- Non-ASCII becomes a base64 encode.
- >10% Non-ASCII becomes all base64 encoded. 
- Filepaths, size (before, after), date, & sha256(partial) are written as a header

Decoder is obvious.

go run minall.go folder_to_convert

Follow instructions given. 