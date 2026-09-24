package ccitt

import (
	"context"
	"errors"
	"testing"
)

// FuzzDecode feeds arbitrary data, under every coding, to the decoder. The
// contract is crash-freedom: every input either decodes to whole rows or
// returns an error carrying a sentinel.
func FuzzDecode(f *testing.F) {
	f.Add(bitsOf(rows2), int8(-1), uint8(8), false)
	f.Add(bitsOf(eol+"1"+row1D+eol+"0"+rowV0), int8(2), uint8(8), true)
	f.Add(bitsOf(row1D+row1D), int8(0), uint8(8), false)
	f.Add(bitsOf(rows2)[:1], int8(-1), uint8(8), false)
	f.Fuzz(func(t *testing.T, data []byte, k int8, columns uint8, align bool) {
		p := Params{K: int(k), Columns: int(columns), EncodedByteAlign: align, EndOfBlock: true}
		out, err := Decode(context.Background(), data, p)
		if err != nil {
			if !errors.Is(err, ErrMalformed) && !errors.Is(err, ErrUnsupported) && !errors.Is(err, ErrLimit) {
				t.Fatalf("error without a sentinel: %v", err)
			}
			return
		}
		if rowBytes := (p.Columns + 7) / 8; len(out)%rowBytes != 0 {
			t.Fatalf("%d bytes is not whole rows of %d", len(out), rowBytes)
		}
	})
}
