package office

import (
	"bytes"
	"encoding/xml"
	"testing"
)

func FuzzTokenReader(f *testing.F) {
	f.Add([]byte(`<?xml version="1.0"?><a x="1"><b>text</b><!-- c --></a>`))
	f.Add([]byte(`<a><b>tex`))
	f.Fuzz(func(t *testing.T, data []byte) {
		const limit = 32
		in := NewTokenReader(bytes.NewReader(data), limit)
		d := xml.NewDecoder(in)
		last := int64(0)
		for {
			if _, err := d.RawToken(); err != nil {
				return
			}
			in.Mark()
			// The decoder looks one byte ahead at most past a token.
			if off := d.InputOffset(); off-last > limit+1 {
				t.Fatalf("a token of %d bytes passed a cap of %d", off-last, limit)
			}
			last = d.InputOffset()
		}
	})
}
