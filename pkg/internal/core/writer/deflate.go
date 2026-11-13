package writer

import (
	"bytes"
	"compress/zlib"
	"fmt"
)

// DeflaterState is what the compressor of a [Deflater] holds once it has
// compressed a stream: the zlib writer at the default level, its hash
// tables, window and block buffers. TestDeflaterStateCoversACompressor
// fails when the compressor of the Go release outgrows it.
const DeflaterState = 768 << 10

// Deflater compresses streams one after another with one zlib writer at
// the default level and one output buffer, both reset between streams. A
// new zlib writer allocates [DeflaterState] bytes before its first output:
// a writer that compresses one stream a page builds that state once, not
// once a page.
//
// A Deflater is used by one goroutine. Its zero value is ready to use, and
// it holds nothing beyond its value once dropped.
type Deflater struct {
	z   *zlib.Writer
	buf bytes.Buffer
}

// Deflate returns data zlib-compressed, the format the FlateDecode filter
// reads (ISO 32000-1, 7.4.4), in a slice of its own whose capacity is its
// length. A zlib writer reset is in the state a new one starts in
// (compress/flate, Writer.Reset), so the bytes are the ones a new writer
// writes.
func (d *Deflater) Deflate(data []byte) ([]byte, error) {
	d.buf.Reset()
	if d.z == nil {
		d.z = zlib.NewWriter(&d.buf)
	} else {
		d.z.Reset(&d.buf)
	}
	if _, err := d.z.Write(data); err != nil {
		return nil, fmt.Errorf("compressing: %w", err)
	}
	if err := d.z.Close(); err != nil {
		return nil, fmt.Errorf("compressing: %w", err)
	}
	out := make([]byte, d.buf.Len())
	copy(out, d.buf.Bytes())
	return out, nil
}

// Held returns the bytes d keeps between streams: [DeflaterState] once it
// has compressed one, and the capacity of its output buffer, which grows
// to the largest stream it has compressed.
func (d *Deflater) Held() int64 {
	if d.z == nil {
		return 0
	}
	return DeflaterState + int64(d.buf.Cap())
}
