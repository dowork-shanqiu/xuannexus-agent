package client

import (
	"bufio"
	"bytes"
	"io"
	"runtime"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/transform"
)

// multiEncodingReader wraps an io.Reader and tries multiple encodings
// It reads data and converts it to UTF-8 using the best matching encoding
type multiEncodingReader struct {
	source io.Reader
	buf    *bytes.Buffer
}

// newMultiEncodingReader creates a reader that automatically detects and converts encodings
func newMultiEncodingReader(r io.Reader) io.Reader {
	// On Unix-like systems, usually UTF-8 is used, so no conversion needed
	if runtime.GOOS != "windows" {
		return r
	}

	// On Windows, we need to handle multiple possible encodings
	return &multiEncodingReader{
		source: r,
		buf:    &bytes.Buffer{},
	}
}

// Read implements io.Reader interface
func (m *multiEncodingReader) Read(p []byte) (n int, err error) {
	// Read from source
	rawData := make([]byte, len(p))
	n, err = m.source.Read(rawData)
	if n == 0 {
		return n, err
	}

	// Get the actual data read
	rawData = rawData[:n]

	// Check if it's already valid UTF-8
	if utf8.Valid(rawData) {
		copy(p, rawData)
		return n, err
	}

	// Try to convert from various encodings
	converted := tryConvertToUTF8(rawData)
	copy(p, converted)
	return len(converted), err
}

// tryConvertToUTF8 tries to convert data from various encodings to UTF-8
func tryConvertToUTF8(data []byte) []byte {
	// If already valid UTF-8, return as is
	if utf8.Valid(data) {
		return data
	}

	// List of encodings to try (most common first for Windows)
	encodings := []encoding.Encoding{
		simplifiedchinese.GBK,       // Chinese Simplified (most common in Chinese regions)
		charmap.Windows1252,         // Western European (common in Western regions)
		traditionalchinese.Big5,     // Chinese Traditional
		japanese.ShiftJIS,           // Japanese
		korean.EUCKR,                // Korean
		charmap.ISO8859_1,           // Latin-1
	}

	// Try each encoding
	for _, enc := range encodings {
		decoder := enc.NewDecoder()
		result, err := io.ReadAll(transform.NewReader(bytes.NewReader(data), decoder))
		if err == nil && utf8.Valid(result) {
			return result
		}
	}

	// If all conversions fail, return original data
	// and let Go's string conversion handle it
	return data
}

// EncodingScanner wraps a Scanner to handle encoding conversion
type EncodingScanner struct {
	scanner *bufio.Scanner
}

// NewEncodingScanner creates a new scanner that handles encoding conversion
func NewEncodingScanner(r io.Reader) *EncodingScanner {
	encodingReader := newMultiEncodingReader(r)
	return &EncodingScanner{
		scanner: bufio.NewScanner(encodingReader),
	}
}

// Scan advances the scanner to the next token
func (es *EncodingScanner) Scan() bool {
	return es.scanner.Scan()
}

// Text returns the most recent token
func (es *EncodingScanner) Text() string {
	text := es.scanner.Text()
	// The text should already be UTF-8 from the encoding reader
	// but validate just in case
	if !utf8.ValidString(text) {
		// This shouldn't happen, but if it does, try one more conversion
		return string(tryConvertToUTF8([]byte(text)))
	}
	return text
}

// Err returns the first non-EOF error that was encountered
func (es *EncodingScanner) Err() error {
	return es.scanner.Err()
}

// sanitizeUTF8String is a simple UTF-8 validator for final safety checks
// It's exported with a simple name for use in command_listener
func sanitizeUTF8String(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	// If still invalid at this point, just replace bad sequences
	// This is a last resort - shouldn't happen if encoding reader worked
	var buf bytes.Buffer
	for len(s) > 0 {
		r, size := utf8.DecodeRuneInString(s)
		if r == utf8.RuneError && size == 1 {
			buf.WriteRune('�')
		} else {
			buf.WriteRune(r)
		}
		s = s[size:]
	}
	return buf.String()
}
