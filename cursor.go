package mapparser

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// ErrTruncated is returned (wrapped) when the mapblock data ends before a
// complete structure could be read.
var ErrTruncated = errors.New("unexpected end of mapblock data")

// cursor is a bounds-checked big-endian reader over a byte slice.
// Every accessor returns an error instead of panicking on short data.
type cursor struct {
	data []byte
	off  int
}

func (c *cursor) remaining() int {
	return len(c.data) - c.off
}

// rest returns the unread part of the data without advancing.
func (c *cursor) rest() []byte {
	return c.data[c.off:]
}

func (c *cursor) need(n int) error {
	if n < 0 || c.remaining() < n {
		return fmt.Errorf("%w: need %d bytes at offset %d, have %d", ErrTruncated, n, c.off, c.remaining())
	}
	return nil
}

func (c *cursor) skip(n int) error {
	if err := c.need(n); err != nil {
		return err
	}
	c.off += n
	return nil
}

func (c *cursor) take(n int) ([]byte, error) {
	if err := c.need(n); err != nil {
		return nil, err
	}
	b := c.data[c.off : c.off+n]
	c.off += n
	return b, nil
}

func (c *cursor) u8() (uint8, error) {
	if err := c.need(1); err != nil {
		return 0, err
	}
	v := c.data[c.off]
	c.off++
	return v, nil
}

func (c *cursor) u16() (uint16, error) {
	if err := c.need(2); err != nil {
		return 0, err
	}
	v := binary.BigEndian.Uint16(c.data[c.off:])
	c.off += 2
	return v, nil
}

func (c *cursor) u32() (uint32, error) {
	if err := c.need(4); err != nil {
		return 0, err
	}
	v := binary.BigEndian.Uint32(c.data[c.off:])
	c.off += 4
	return v, nil
}

// line returns the next '\n'-terminated line (without the terminator) and
// advances past it. A final line without terminator is returned as-is.
func (c *cursor) line() ([]byte, error) {
	if c.remaining() == 0 {
		return nil, fmt.Errorf("%w: expected text line at offset %d", ErrTruncated, c.off)
	}
	rest := c.data[c.off:]
	i := bytes.IndexByte(rest, '\n')
	if i < 0 {
		c.off = len(c.data)
		return rest, nil
	}
	c.off += i + 1
	return rest[:i], nil
}
