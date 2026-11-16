package mapparser

import (
	"bytes"
	"compress/zlib"
	"io"
	"sync"

	"github.com/klauspost/compress/zstd"
)

// maxDecompressedSize bounds the zstd output to guard against decompression bombs.
const maxDecompressedSize = 256 << 20

// zlibPool recycles zlib readers (and their ~40KB flate state) between calls.
var zlibPool sync.Pool

// decompressZlib inflates the zlib stream at the start of data and returns the
// decompressed bytes and the number of compressed bytes consumed.
// sizeHint pre-sizes the output buffer (0 for unknown).
func decompressZlib(data []byte, sizeHint int) ([]byte, int, error) {
	// *bytes.Reader implements io.ByteReader, so flate never reads past the
	// end of the stream and the consumed length is exact.
	r := bytes.NewReader(data)

	var z io.ReadCloser
	if pooled, ok := zlibPool.Get().(io.ReadCloser); ok {
		if err := pooled.(zlib.Resetter).Reset(r, nil); err != nil {
			zlibPool.Put(pooled)
			return nil, 0, err
		}
		z = pooled
	} else {
		var err error
		z, err = zlib.NewReader(r)
		if err != nil {
			return nil, 0, err
		}
	}
	defer zlibPool.Put(z)

	if sizeHint <= 0 {
		sizeHint = bytes.MinRead
	}
	buf := bytes.NewBuffer(make([]byte, 0, sizeHint))
	if _, err := buf.ReadFrom(z); err != nil {
		return nil, 0, err
	}

	return buf.Bytes(), len(data) - r.Len(), nil
}

// zstdDecoder is created once and shared: DecodeAll is safe for concurrent use,
// and creating a decoder per block costs ~0.5ms and ~3MB of allocations.
var zstdDecoder = sync.OnceValues(func() (*zstd.Decoder, error) {
	return zstd.NewReader(nil,
		zstd.WithDecoderConcurrency(0),
		zstd.WithDecoderMaxMemory(maxDecompressedSize),
	)
})

func decompressZstd(data []byte) ([]byte, error) {
	dec, err := zstdDecoder()
	if err != nil {
		return nil, err
	}
	return dec.DecodeAll(data, make([]byte, 0, MapDataSize+4096))
}
