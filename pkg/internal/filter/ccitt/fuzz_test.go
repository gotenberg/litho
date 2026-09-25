package ccitt

import (
	"context"
	"errors"
	"testing"
)

// FuzzDecode feeds arbitrary data, under every coding, to the decoder. The
// contract is crash-freedom: every input either decodes to whole rows or
// returns an error carrying a sentinel, and rows come back with an error only
// when it is [ErrPartial]. [Check] agrees with it: it refuses the data as
// unsupported exactly when Decode does, and passes whatever Decode decodes,
// the rows before a partial end included.
func FuzzDecode(f *testing.F) {
	f.Add(bitsOf(rows2), int8(-1), uint8(8), false)
	f.Add(bitsOf(eol+"1"+row1D+eol+"0"+rowV0), int8(2), uint8(8), true)
	f.Add(bitsOf(row1D+row1D), int8(0), uint8(8), false)
	f.Add(bitsOf(rows2)[:1], int8(-1), uint8(8), false)
	f.Add(bitsOf("0000001 111"), int8(-1), uint8(8), false)
	f.Add(bitsOf(rowH+" 0000001 111"), int8(-1), uint8(8), false)
	f.Fuzz(func(t *testing.T, data []byte, k int8, columns uint8, align bool) {
		p := Params{K: int(k), Columns: int(columns), EncodedByteAlign: align, EndOfBlock: true}
		out, err := Decode(context.Background(), data, p)
		checkErr := Check(context.Background(), data, p)
		partial := errors.Is(err, ErrPartial)
		if errors.Is(checkErr, ErrUnsupported) != errors.Is(err, ErrUnsupported) || ((err == nil || partial) && checkErr != nil) {
			t.Fatalf("Check = %v, Decode = %v", checkErr, err)
		}
		if partial && len(out) == 0 {
			t.Fatalf("a partial end with no rows before it: %v", err)
		}
		if err != nil && !partial {
			if out != nil {
				t.Fatalf("rows returned with %v", err)
			}
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

// FuzzDecodeBlock feeds arbitrary data to the block decoder. The contract is
// crash-freedom: every input either decodes to exactly the rows asked for,
// within the bytes it reports consuming, or returns an error carrying a
// sentinel.
func FuzzDecodeBlock(f *testing.F) {
	f.Add(bitsOf(rows2+eol+eol), uint8(2), uint8(8))
	f.Add(bitsOf(rows2 + eol + eol)[:1], uint8(2), uint8(8))
	f.Fuzz(func(t *testing.T, data []byte, rows, columns uint8) {
		p := Params{K: -1, Columns: int(columns), Rows: int(rows), BlackIs1: true}
		out, used, err := DecodeBlock(context.Background(), data, p)
		if err != nil {
			if !errors.Is(err, ErrMalformed) && !errors.Is(err, ErrUnsupported) && !errors.Is(err, ErrLimit) {
				t.Fatalf("error without a sentinel: %v", err)
			}
			return
		}
		if len(out) != p.Rows*((p.Columns+7)/8) {
			t.Fatalf("%d bytes for %d rows of %d columns", len(out), p.Rows, p.Columns)
		}
		if used < 0 || used > len(data) {
			t.Fatalf("consumed %d of %d bytes", used, len(data))
		}
	})
}
