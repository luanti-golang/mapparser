package mapparser

import (
	"bytes"
)

// CountedReader counts the bytes read from the underlying reader.
//
// Deprecated: no longer used by the parser, kept for API compatibility.
type CountedReader struct {
	Reader *bytes.Reader
	Count  int
}

func (r *CountedReader) Read(p []byte) (int, error) {
	i, err := r.Reader.Read(p)
	r.Count += i
	return i, err
}

func (r *CountedReader) ReadByte() (byte, error) {
	b, err := r.Reader.ReadByte()
	if err == nil {
		r.Count++
	}
	return b, err
}
