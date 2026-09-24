package filter

import (
	"bytes"
	"context"
	"encoding/ascii85"
	"encoding/hex"
	"errors"
	"io"
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
// named the first byte of U+FFFD.
func TestDecodeASCIIHex_Invalid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		bad   byte
	}{
		{"letter past F", "48G5>", 'G'},
		{"VERTICAL TAB", "48\v65>", '\v'},
		{"SOH", "48\x0165>", 0x01},
		{"byte 80h", "48\x8065>", 0x80},
		{"byte FFh", "\xff>", 0xff},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := decodeASCIIHex([]byte(tt.input))
			if !errors.Is(err, ErrInvalidASCIIHex) {
				t.Fatalf("decodeASCIIHex(%q) error = %v, want %v", tt.input, err, ErrInvalidASCIIHex)
			}
			var invalid hex.InvalidByteError
			if !errors.As(err, &invalid) || byte(invalid) != tt.bad {
				t.Errorf("decodeASCIIHex(%q) error = %v, want it to name byte %#02x", tt.input, err, tt.bad)
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
// white space used to be skipped.
func TestDecodeASCII85_Invalid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
	}{
		{"group of 2^32", "s8W-\"~>"},
		{"group of uuuuu", "uuuuu~>"},
		{"overflowing group after a valid one", "87cURuuuuu~>"},
		{"overflowing group without EOD", "uuuuu"},
		{"final partial group completing past 2^32 - 1", "87cURuu~>"},
		{"final partial group of one character", "87cUR!~>"},
		{"z inside a group", "87z~>"},
		{"z ending a group", "!!!!z~>"},
		{"character past u", "87cU{~>"},
		{"lone tilde", "87cUR~"},
		{"tilde not followed by >", "87cUR~ >"},
		{"VERTICAL TAB", "87cUR\v~>"},
		{"SOH", "87c\x01URD]i,\"Ebo7~>"},
		{"byte 80h", "87c\x80URD]i,\"Ebo7~>"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := decodeASCII85([]byte(tt.input))
			if !errors.Is(err, ErrInvalidASCII85) {
				t.Fatalf("decodeASCII85(%q) = %x, %v, want %v", tt.input, got, err, ErrInvalidASCII85)
			}
			if got != nil {
				t.Errorf("decodeASCII85(%q) returned %x alongside its error", tt.input, got)
			}
		})
	}
}

// TestDecodeStream_ASCII85Overflow checks what a caller of each decoding
// entry point sees when an ASCII85Decode group exceeds 2^32 - 1 ahead of
// FlateDecode: an error wrapping [ErrInvalidASCII85], no bytes, and no skip.
// The tolerance that keeps a truncated FlateDecode stream's output does not
// reach it, and it is not [ErrUnsupportedFilter], which a chain skips.
func TestDecodeStream_ASCII85Overflow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	compressed := zlibCompress([]byte("litho refuses an ASCII85Decode group past 2^32 - 1"))
	// A group past 2^32 - 1 between otherwise valid data, starting on a group
	// boundary: the first half is a whole number of groups.
	half := len(compressed) / 2 &^ 3
	data := encodeASCII85(compressed[:half])
	data = append(data, "uuuuu"...)
	data = append(data, encodeASCII85(compressed[half:])...)
	data = append(data, "~>"...)
	stream := ast.NewStream(ast.PdfDict{
		"Filter": ast.PdfArray{ast.PdfName("ASCII85Decode"), ast.PdfName("FlateDecode")},
	}, data)

	got, incomplete, err := DecodeStreamChecked(ctx, stream)
	if !errors.Is(err, ErrInvalidASCII85) || errors.Is(err, ErrUnsupportedFilter) {
		t.Fatalf("DecodeStreamChecked error = %v, want %v and not %v", err, ErrInvalidASCII85, ErrUnsupportedFilter)
	}
	if got != nil || incomplete {
		t.Errorf("DecodeStreamChecked = %d bytes, incomplete %v, want no bytes and not incomplete", len(got), incomplete)
	}

	if _, _, err := DecodeStreamUpTo(ctx, stream, "FlateDecode"); !errors.Is(err, ErrInvalidASCII85) {
		t.Errorf("DecodeStreamUpTo error = %v, want %v", err, ErrInvalidASCII85)
	}

	rc, err := NewStreamReader(ctx, stream)
	if err == nil {
		if closeErr := rc.Close(); closeErr != nil {
			t.Errorf("closing the reader: %v", closeErr)
		}
	}
	if !errors.Is(err, ErrInvalidASCII85) {
		t.Errorf("NewStreamReader error = %v, want %v", err, ErrInvalidASCII85)
	}

	var d *Decoder
	if _, err := d.DecodeStream(ctx, stream); !errors.Is(err, ErrInvalidASCII85) {
		t.Errorf("(*Decoder)(nil).DecodeStream error = %v, want %v", err, ErrInvalidASCII85)
	}
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
