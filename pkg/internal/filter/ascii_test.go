package filter

import (
	"bytes"
	"context"
	"encoding/ascii85"
	"encoding/hex"
	"errors"
	"io"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// encodeASCII85 encodes src as ISO 32000-1, 7.4.3 describes, without the EOD
// marker.
func encodeASCII85(src []byte) []byte {
	enc := make([]byte, ascii85.MaxEncodedLen(len(src)))
	return enc[:ascii85.Encode(enc, src)]
}

// TestDecodeASCIIHex_WhiteSpace decodes data holding each of the six
// white-space characters of ISO 32000-1, 7.2.2, Table 1, which 7.4.2 ignores.
// NUL and FORM FEED used to fail with "invalid byte".
func TestDecodeASCIIHex_WhiteSpace(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
	}{
		{"NUL", "48\x0065\x006C6C6F>"},
		{"HORIZONTAL TAB", "48\t656C6C\t6F>"},
		{"LINE FEED", "4865\n6C6C6F\n>"},
		{"FORM FEED", "\f48656C\f6C6F>"},
		{"CARRIAGE RETURN", "48656C6C6F\r\n>"},
		{"SPACE", "48 65 6C 6C 6F >"},
		{"all six", "\x00\t\n\f\r 4\x008\t6\n5\f6\rC 6C6F>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := decodeASCIIHex([]byte(tt.input))
			if err != nil {
				t.Fatalf("decodeASCIIHex(%q): %v", tt.input, err)
			}
			if string(got) != "Hello" {
				t.Errorf("decodeASCIIHex(%q) = %q, want %q", tt.input, got, "Hello")
			}
		})
	}
}

// TestDecodeASCIIHex_EOD covers the EOD marker and the odd final digit of
// ISO 32000-1, 7.4.2.
func TestDecodeASCIIHex_EOD(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  []byte
	}{
		{"empty", "", []byte{}},
		{"EOD only", ">", []byte{}},
		{"data after EOD ignored", "4142>zz not hex", []byte("AB")},
		{"odd digit before EOD", "41424>", []byte{0x41, 0x42, 0x40}},
		{"odd digit without EOD", "41424", []byte{0x41, 0x42, 0x40}},
		{"odd digit before white space and EOD", "4142 4 \n>", []byte{0x41, 0x42, 0x40}},
		{"lower case", "6c6f>", []byte("lo")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := decodeASCIIHex([]byte(tt.input))
			if err != nil {
				t.Fatalf("decodeASCIIHex(%q): %v", tt.input, err)
			}
			if !bytes.Equal(got, tt.want) {
				t.Errorf("decodeASCIIHex(%q) = %x, want %x", tt.input, got, tt.want)
			}
		})
	}
}

// TestDecodeASCIIHex_Invalid covers the characters ISO 32000-1, 7.4.2 makes an
// error, among them control characters Table 1 does not list as white space.
// The error names the byte the data holds, where decoding through strings.Map
// named the first byte of U+FFFD, and comes with the bytes of the digit pairs
// before it, a digit left without its pair contributing nothing.
func TestDecodeASCIIHex_Invalid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		input  string
		bad    byte
		prefix string
	}{
		{"letter past F", "48G5>", 'G', "H"},
		{"VERTICAL TAB", "48\v65>", '\v', "H"},
		{"SOH", "48\x0165>", 0x01, "H"},
		{"byte 80h", "48\x8065>", 0x80, "H"},
		{"byte FFh", "\xff>", 0xff, ""},
		{"after an odd digit", "48 6 5 6X6C>", 'X', "He"},
		{"before any digit", "Z48>", 'Z', ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := decodeASCIIHex([]byte(tt.input))
			if !errors.Is(err, ErrInvalidASCIIHex) {
				t.Fatalf("decodeASCIIHex(%q) error = %v, want %v", tt.input, err, ErrInvalidASCIIHex)
			}
			var invalid hex.InvalidByteError
			if !errors.As(err, &invalid) || byte(invalid) != tt.bad {
				t.Errorf("decodeASCIIHex(%q) error = %v, want it to name byte %#02x", tt.input, err, tt.bad)
			}
			if string(got) != tt.prefix {
				t.Errorf("decodeASCIIHex(%q) returned %q alongside its error, want %q", tt.input, got, tt.prefix)
			}
		})
	}
}

// TestDecodeASCII85_WhiteSpace decodes data holding each of the six
// white-space characters of ISO 32000-1, 7.2.2, Table 1, which 7.4.3 ignores,
// inside groups and between them.
func TestDecodeASCII85_WhiteSpace(t *testing.T) {
	t.Parallel()
	// "87cURD]i,\"Ebo7" is the ASCII85 encoding of "Hello World".
	tests := []struct {
		name  string
		input string
	}{
		{"NUL", "87c\x00URD]i,\x00\"Ebo7~>"},
		{"HORIZONTAL TAB", "87cUR\tD]i,\"Ebo7~>"},
		{"LINE FEED", "87cUR\nD]i,\"\nEbo7~>"},
		{"FORM FEED", "\f87cURD]i\f,\"Ebo7~>"},
		{"CARRIAGE RETURN", "87cURD]i,\"Ebo7\r\n~>"},
		{"SPACE", "87cUR D]i,\" Ebo7 ~>"},
		{"all six", "8\x007\tc\nU\fR\rD ]i,\"Ebo7~>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := decodeASCII85([]byte(tt.input))
			if err != nil {
				t.Fatalf("decodeASCII85(%q): %v", tt.input, err)
			}
			if string(got) != "Hello World" {
				t.Errorf("decodeASCII85(%q) = %q, want %q", tt.input, got, "Hello World")
			}
		})
	}
}

// TestDecodeASCII85_Groups covers the group arithmetic of ISO 32000-1, 7.4.3:
// the largest value a group can hold, the z shorthand, and the final partial
// groups of two to four characters, checked against encoding/ascii85 as the
// encoder, which 7.4.3 specifies in full.
func TestDecodeASCII85_Groups(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  []byte
	}{
		{"empty", "", []byte{}},
		{"EOD only", "~>", []byte{}},
		{"2^32 - 1", "s8W-!~>", []byte{0xff, 0xff, 0xff, 0xff}},
		{"zero group", "!!!!!~>", []byte{0, 0, 0, 0}},
		{"z", "z~>", []byte{0, 0, 0, 0}},
		{"z between groups", "s8W-!zs8W-!~>", []byte{0xff, 0xff, 0xff, 0xff, 0, 0, 0, 0, 0xff, 0xff, 0xff, 0xff}},
		{"z before a partial group", "zrr~>", []byte{0, 0, 0, 0, 0xff}},
		{"data after EOD ignored", "z~>uuuuu", []byte{0, 0, 0, 0}},
	}
	for _, tail := range [][]byte{
		{0x00},
		{0xff},
		{0x00, 0x00},
		{0xff, 0xff},
		{0x00, 0x00, 0x00},
		{0xff, 0xff, 0xff},
		{0x01, 0x02, 0x03},
		{0xfe, 0xff, 0x7f},
	} {
		src := append([]byte("Man "), tail...)
		tests = append(tests, struct {
			name  string
			input string
			want  []byte
		}{"final partial group of " + hex.EncodeToString(tail), string(encodeASCII85(src)) + "~>", src})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := decodeASCII85([]byte(tt.input))
			if err != nil {
				t.Fatalf("decodeASCII85(%q): %v", tt.input, err)
			}
			if !bytes.Equal(got, tt.want) {
				t.Errorf("decodeASCII85(%q) = %x, want %x", tt.input, got, tt.want)
			}
		})
	}
}

// TestDecodeASCII85_Invalid covers the characters and the character sequences
// ISO 32000-1, 7.4.3 makes an error. A group past 2^32 - 1 used to wrap and
// decode to four wrong bytes, and a control character Table 1 does not list as
// white space used to be skipped. The error comes with the bytes of the whole
// groups before the damage; a group it interrupts contributes nothing.
func TestDecodeASCII85_Invalid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		input  string
		prefix string
	}{
		{"group of 2^32", "s8W-\"~>", ""},
		{"group of uuuuu", "uuuuu~>", ""},
		{"overflowing group after a valid one", "87cURuuuuu~>", "Hell"},
		{"overflowing group without EOD", "uuuuu", ""},
		{"final partial group completing past 2^32 - 1", "87cURuu~>", "Hell"},
		{"final partial group of one character", "87cUR!~>", "Hell"},
		{"z inside a group", "87z~>", ""},
		{"z ending a group", "!!!!z~>", ""},
		{"z inside a group after z", "z87z~>", "\x00\x00\x00\x00"},
		{"character past u", "87cU{~>", ""},
		{"lone tilde", "87cUR~", "Hell"},
		{"tilde not followed by >", "87cUR~ >", "Hell"},
		{"VERTICAL TAB", "87cUR\v~>", "Hell"},
		{"SOH", "87c\x01URD]i,\"Ebo7~>", ""},
		{"byte 80h", "87cURD]i,\"\x80Ebo7~>", "Hello Wo"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := decodeASCII85([]byte(tt.input))
			if !errors.Is(err, ErrInvalidASCII85) {
				t.Fatalf("decodeASCII85(%q) = %x, %v, want %v", tt.input, got, err, ErrInvalidASCII85)
			}
			if string(got) != tt.prefix {
				t.Errorf("decodeASCII85(%q) returned %q alongside its error, want %q", tt.input, got, tt.prefix)
			}
		})
	}
}

// TestDecodeStream_ASCIIDamageKeepsPrefix checks what a caller of each decoding
// entry point sees when ASCII85Decode or ASCIIHexDecode data is damaged ahead
// of FlateDecode: the bytes decoded before the damage are kept and handed to
// FlateDecode, which reads them as it reads any truncated input, and the
// stream is reported incomplete. The damage is not [ErrUnsupportedFilter]: the
// filter is not skipped, and its encoded bytes never reach FlateDecode.
//
// Before, the damage failed the whole stream and no byte came back.
func TestDecodeStream_ASCIIDamageKeepsPrefix(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	plain := bytes.Repeat([]byte("litho keeps what an ASCII filter decoded before the damage. "), 40)
	compressed := zlibCompress(plain)
	// The damage starts on a group boundary, so the first half is exactly
	// the prefix both filters decode.
	half := len(compressed) / 2 &^ 3

	for _, tc := range []struct {
		name    string
		filter  ast.PdfName
		encoded []byte
		wantErr error
	}{
		{
			name:   "ASCII85Decode group past 2^32 - 1",
			filter: "ASCII85Decode",
			encoded: slices.Concat(encodeASCII85(compressed[:half]), []byte("uuuuu"),
				encodeASCII85(compressed[half:]), []byte("~>")),
			wantErr: ErrInvalidASCII85,
		},
		{
			name:   "ASCII85Decode character outside the encoding",
			filter: "A85",
			encoded: slices.Concat(encodeASCII85(compressed[:half]), []byte("{"),
				encodeASCII85(compressed[half:]), []byte("~>")),
			wantErr: ErrInvalidASCII85,
		},
		{
			name:   "ASCIIHexDecode character outside the encoding",
			filter: "ASCIIHexDecode",
			encoded: slices.Concat([]byte(hex.EncodeToString(compressed[:half])), []byte("G"),
				[]byte(hex.EncodeToString(compressed[half:])), []byte(">")),
			wantErr: ErrInvalidASCIIHex,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// The ASCII filter alone hands on the prefix, flagged incomplete.
			alone := ast.NewStream(ast.PdfDict{"Filter": tc.filter}, tc.encoded)
			got, incomplete, err := DecodeStreamChecked(ctx, alone)
			if err != nil || !incomplete || !bytes.Equal(got, compressed[:half]) {
				t.Fatalf("DecodeStreamChecked(%s) = %d bytes, incomplete %v, %v; want the %d bytes before the damage, incomplete",
					tc.filter, len(got), incomplete, err, half)
			}

			// FlateDecode after it behaves as it does on the truncated input
			// directly: the tolerance of decodeFlateDecode keeps what inflates.
			truncated := ast.NewStream(ast.PdfDict{"Filter": ast.PdfName("FlateDecode")}, compressed[:half])
			want, truncIncomplete, err := DecodeStreamChecked(ctx, truncated)
			if err != nil || truncIncomplete || len(want) == 0 || !bytes.HasPrefix(plain, want) {
				t.Fatalf("control: FlateDecode on the truncated input = %d bytes, incomplete %v, %v; want a prefix of the plain text",
					len(want), truncIncomplete, err)
			}
			chained := ast.NewStream(ast.PdfDict{
				"Filter": ast.PdfArray{tc.filter, ast.PdfName("FlateDecode")},
			}, tc.encoded)
			got, incomplete, err = DecodeStreamChecked(ctx, chained)
			if err != nil || !incomplete || !bytes.Equal(got, want) {
				t.Errorf("DecodeStreamChecked(%s, FlateDecode) = %d bytes, incomplete %v, %v; want the %d bytes FlateDecode gives the truncated input, incomplete",
					tc.filter, len(got), incomplete, err, len(want))
			}

			var d *Decoder
			if got, err := d.DecodeStream(ctx, chained); err != nil || !bytes.Equal(got, want) {
				t.Errorf("(*Decoder)(nil).DecodeStream = %d bytes, %v; want %d bytes", len(got), err, len(want))
			}
			// The report names the damaged filter, and skips none.
			if _, report, err := d.DecodeStreamReport(ctx, chained); err != nil ||
				!slices.Equal(report.Damaged, []ast.PdfName{tc.filter}) || len(report.Skipped) != 0 {
				t.Errorf("DecodeStreamReport = %+v, %v; want %s damaged and nothing skipped", report, err, tc.filter)
			}

			rc, err := NewStreamReader(ctx, chained)
			if err != nil {
				t.Fatalf("NewStreamReader: %v", err)
			}
			streamed, err := io.ReadAll(rc)
			if closeErr := rc.Close(); closeErr != nil {
				t.Errorf("closing the reader: %v", closeErr)
			}
			if err != nil || !bytes.Equal(streamed, want) {
				t.Errorf("NewStreamReader read %d bytes, %v; want %d bytes", len(streamed), err, len(want))
			}

			upTo, found, err := DecodeStreamUpTo(ctx, chained, "FlateDecode")
			if err != nil || !found || !bytes.Equal(upTo, compressed[:half]) {
				t.Errorf("DecodeStreamUpTo = %d bytes, found %v, %v; want the %d bytes before the damage", len(upTo), found, err, half)
			}
		})
	}

	// Damage before the first whole group leaves nothing to keep, and the
	// error stands, as it does for FlateDecode data that inflates to nothing.
	t.Run("nothing before the damage", func(t *testing.T) {
		t.Parallel()
		for filterName, encoded := range map[ast.PdfName]string{
			"ASCII85Decode":  "uuuuu87cUR~>",
			"ASCIIHexDecode": "G48>",
		} {
			stream := ast.NewStream(ast.PdfDict{
				"Filter": ast.PdfArray{filterName, ast.PdfName("FlateDecode")},
			}, []byte(encoded))
			got, incomplete, err := DecodeStreamChecked(ctx, stream)
			if !errors.Is(err, ErrInvalidASCII85) && !errors.Is(err, ErrInvalidASCIIHex) || errors.Is(err, ErrUnsupportedFilter) {
				t.Errorf("%s: DecodeStreamChecked error = %v, want the filter's own error", filterName, err)
			}
			if got != nil || incomplete {
				t.Errorf("%s: DecodeStreamChecked = %d bytes, incomplete %v, want no bytes", filterName, len(got), incomplete)
			}
			if _, _, err := DecodeStreamUpTo(ctx, stream, "FlateDecode"); err == nil {
				t.Errorf("%s: DecodeStreamUpTo kept nothing and reported no error", filterName)
			}
		}
	})
}

// TestDecodeStream_ASCIIHexFormFeed decodes an ASCIIHexDecode stream laid out
// with FORM FEED and NUL through the whole pipeline, including the FlateDecode
// stage it feeds.
func TestDecodeStream_ASCIIHexFormFeed(t *testing.T) {
	t.Parallel()
	plain := []byte("litho reads ASCIIHexDecode laid out with FORM FEED")
	encoded := []byte(hex.EncodeToString(zlibCompress(plain)))
	var laidOut []byte
	for i := 0; i < len(encoded); i += 16 {
		laidOut = append(laidOut, encoded[i:min(i+16, len(encoded))]...)
		laidOut = append(laidOut, '\f', 0x00)
	}
	laidOut = append(laidOut, '>')
	stream := ast.NewStream(ast.PdfDict{
		"Filter": ast.PdfArray{ast.PdfName("AHx"), ast.PdfName("Fl")},
	}, laidOut)

	rc, err := NewStreamReader(context.Background(), stream)
	if err != nil {
		t.Fatalf("NewStreamReader: %v", err)
	}
	got, err := io.ReadAll(rc)
	if closeErr := rc.Close(); closeErr != nil {
		t.Errorf("closing the reader: %v", closeErr)
	}
	if err != nil {
		t.Fatalf("reading the stream: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Errorf("got %q, want %q", got, plain)
	}
}
