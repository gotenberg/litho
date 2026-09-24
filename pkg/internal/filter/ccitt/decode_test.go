package ccitt

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

// bitsOf packs a string of '0' and '1', spaces ignored, into bytes, padding
// the last with zeros.
func bitsOf(s string) []byte {
	s = strings.ReplaceAll(s, " ", "")
	out := make([]byte, (len(s)+7)/8)
	for i, c := range s {
		if c == '1' {
			out[i/8] |= 0x80 >> (i % 8)
		}
	}
	return out
}

// The row every test codes: three white pixels, two black, three white. Black
// is 0, so it packs to 11100111.
const (
	row1D   = "1000 11 1000"   // white 3, black 2, white 3 (T.4, Table 2)
	rowH    = "001 1000 11 1"  // horizontal white 3 black 2, then V0 to the edge
	rowV0   = "1 1 1"          // three V0 codes against an identical row
	eol     = "000000000001"   // T.4, 4.1.2
	pattern = byte(0b11100111) // the row, black as 0
	rows2   = "" + rowH + " " + rowV0
)

// TestDecode codes the same two rows every way ISO 32000-1, Table 11 allows.
func TestDecode(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		p    Params
		data string
		want []byte
	}{
		{"Group 4", Params{K: -1, Columns: 8}, rows2, []byte{pattern, pattern}},
		{"Group 4 with EOFB", Params{K: -1, Columns: 8, EndOfBlock: true}, rows2 + eol + eol + "1111", []byte{pattern, pattern}},
		{"Group 3 1D", Params{Columns: 8}, row1D + row1D, []byte{pattern, pattern}},
		{"Group 3 1D with EOLs", Params{Columns: 8, EndOfLine: true}, eol + row1D + eol + row1D, []byte{pattern, pattern}},
		{
			"Group 3 1D with fill and RTC",
			Params{Columns: 8, EndOfLine: true, EndOfBlock: true},
			"0000" + eol + row1D + eol + row1D + eol + eol + eol + eol + eol + eol,
			[]byte{pattern, pattern},
		},
		{"Group 3 mixed", Params{K: 2, Columns: 8}, "1 " + row1D + " 0 " + rowV0, []byte{pattern, pattern}},
		{"Group 3 mixed with EOLs", Params{K: 2, Columns: 8, EndOfLine: true}, eol + "1" + row1D + eol + "0" + rowV0, []byte{pattern, pattern}},
		{"rows stop the decoding", Params{K: -1, Columns: 8, Rows: 1}, rows2, []byte{pattern}},
		{"black is 1", Params{K: -1, Columns: 8, BlackIs1: true}, rows2, []byte{^pattern, ^pattern}},
		{"byte-aligned rows", Params{K: -1, Columns: 8, EncodedByteAlign: true}, rowH + "000000" + rowV0, []byte{pattern, pattern}},
	} {
		got, err := Decode(context.Background(), bitsOf(tc.data), tc.p)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if !bytes.Equal(got, tc.want) {
			t.Errorf("%s: %08b, want %08b", tc.name, got, tc.want)
		}
	}
}

// TestDecode_PadBits leaves the bits past the last column 0.
func TestDecode_PadBits(t *testing.T) {
	t.Parallel()

	// One black pixel: white 0 (00110101), black 1 (010).
	got, err := Decode(context.Background(), bitsOf("00110101 010"), Params{Columns: 1, Rows: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte{0}) {
		t.Errorf("%08b, want 00000000", got)
	}
}

// TestDecode_BrokenRow keeps the rows before a row the data garbles, and
// fails only when there are none.
func TestDecode_BrokenRow(t *testing.T) {
	t.Parallel()

	p := Params{Columns: 8}
	got, err := Decode(context.Background(), bitsOf(row1D+" 00000001 1"), p)
	if err != nil || !bytes.Equal(got, []byte{pattern}) {
		t.Errorf("garbled second row: %08b, %v; want the first row", got, err)
	}
	if _, err := Decode(context.Background(), bitsOf("00000001 1"), p); !errors.Is(err, ErrMalformed) {
		t.Errorf("garbled first row: %v, want %v", err, ErrMalformed)
	}
	if _, err := Decode(context.Background(), bitsOf("0000001 111"), Params{K: -1, Columns: 8}); !errors.Is(err, ErrUnsupported) {
		t.Errorf("uncompressed mode: %v, want %v", err, ErrUnsupported)
	}
}

// TestDecode_Limits refuses a width outside what a row may hold before
// decoding anything.
func TestDecode_Limits(t *testing.T) {
	t.Parallel()

	for _, p := range []Params{{Columns: 0}, {Columns: maxColumns + 1}, {Columns: 8, Rows: defaultMaxOutput + 1}, {Columns: 8, Rows: 2, MaxOutput: 1}} {
		if _, err := Decode(context.Background(), nil, p); !errors.Is(err, ErrLimit) {
			t.Errorf("%+v: %v, want %v", p, err, ErrLimit)
		}
	}
}

// TestDecode_Canceled stops at a cancelled context.
func TestDecode_Canceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Decode(ctx, bitsOf(rows2), Params{K: -1, Columns: 8}); !errors.Is(err, context.Canceled) {
		t.Errorf("%v, want %v", err, context.Canceled)
	}
}

// TestDecodeBlock pins what a block followed by other data reports: its rows,
// and the whole bytes the rows and an EOFB after them occupy, so the data
// after it is found (ITU-T T.88, 6.2.6). Rows that stop short are an error,
// where Decode would return them.
func TestDecodeBlock(t *testing.T) {
	t.Parallel()

	p := Params{K: -1, Columns: 8, Rows: 2, BlackIs1: true}
	next := "11111111"
	for _, tc := range []struct {
		name string
		data string
		used int
	}{
		// 13 bits of rows and 24 of EOFB end in the fifth byte.
		{"rows and EOFB", rows2 + eol + eol + "000" + next, 5},
		{"rows alone", rows2 + "000" + next, 2},
	} {
		got, used, err := DecodeBlock(context.Background(), bitsOf(tc.data), p)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if !bytes.Equal(got, []byte{^pattern, ^pattern}) || used != tc.used {
			t.Errorf("%s: %08b and %d bytes, want %08b and %d", tc.name, got, used, []byte{^pattern, ^pattern}, tc.used)
		}
	}
	// A row that fails names its own failure, here uncompressed mode, rather
	// than only the rows missing after it.
	for name, tc := range map[string]struct {
		data string
		want error
	}{
		"an EOFB after one row":           {rowH + eol + eol, ErrMalformed},
		"uncompressed mode in second row": {rowH + "0000001 111", ErrUnsupported},
		"data ending in a row":            {rowH + "1", ErrMalformed},
	} {
		if _, _, err := DecodeBlock(context.Background(), bitsOf(tc.data), p); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v, want %v", name, err, tc.want)
		}
	}
	if _, _, err := DecodeBlock(context.Background(), bitsOf(rows2), Params{K: -1, Columns: 8}); !errors.Is(err, ErrMalformed) {
		t.Errorf("no rows asked for: %v, want %v", err, ErrMalformed)
	}
}
