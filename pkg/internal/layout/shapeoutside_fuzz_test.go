package layout

import (
	"bytes"
	"compress/zlib"
	"testing"
)

func FuzzInflateFull(f *testing.F) {
	var b bytes.Buffer
	z := zlib.NewWriter(&b)
	_, _ = z.Write(bytes.Repeat([]byte{0, 128, 255}, 12)) // a bytes.Buffer takes every write
	_ = z.Close()                                         // likewise
	f.Add(b.Bytes(), 36)
	f.Add(b.Bytes()[:b.Len()/2], 36)
	f.Fuzz(func(_ *testing.T, data []byte, n int) {
		if n < 0 || n > 1<<16 {
			return
		}
		_ = inflateFull(data, make([]byte, n)) // only a panic fails the target
	})
}
