package filter

import (
	"bytes"
	"compress/zlib"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// flateEncode compresses data the way a PDF /FlateDecode stream stores it.
func flateEncode(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zlib.NewWriter(&buf)
	if _, err := w.Write(data); err != nil {
		t.Fatalf("compressing fixture: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("closing compressor: %v", err)
	}
	return buf.Bytes()
}

// TestDecodeStreamUpTo covers the contract an embedded-format check depends
// on: the bytes it judges are the ones the format's own filter receives.
//
// The JPEG2000 case is the regression. A JP2 codestream behind
// [/FlateDecode /JPXDecode] reached ISO 19005-2, 6.2.8.3 as a compressed
// buffer, which matches neither the JP2 signature nor the raw J2K marker, so
// the checks returned early and every violation behind an outer filter went
// unreported.
func TestDecodeStreamUpTo(t *testing.T) {
	t.Parallel()

	codestream := []byte{0xFF, 0x4F, 0xFF, 0x51, 'p', 'a', 'y', 'l', 'o', 'a', 'd', '!'}

	for _, tc := range []struct {
		name  string
		dict  ast.PdfDict
		data  []byte
		stop  ast.PdfName
		want  []byte
		found bool
	}{
		{
			name:  "stops before the format filter and undoes the outer one",
			dict:  ast.PdfDict{"Filter": ast.PdfArray{ast.PdfName("FlateDecode"), ast.PdfName("JPXDecode")}},
			data:  flateEncode(t, codestream),
			stop:  "JPXDecode",
			want:  codestream,
			found: true,
		},
		{
			name:  "format filter alone returns the raw bytes",
			dict:  ast.PdfDict{"Filter": ast.PdfName("JPXDecode")},
			data:  codestream,
			stop:  "JPXDecode",
			want:  codestream,
			found: true,
		},
		{
			name:  "absent format filter decodes the whole chain",
			dict:  ast.PdfDict{"Filter": ast.PdfName("FlateDecode")},
			data:  flateEncode(t, codestream),
			stop:  "JPXDecode",
			want:  codestream,
			found: false,
		},
		{
			name:  "no filter at all yields the bytes unchanged",
			dict:  ast.PdfDict{},
			data:  codestream,
			stop:  "JPXDecode",
			want:  codestream,
			found: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, found, err := DecodeStreamUpTo(context.Background(), ast.NewStream(tc.dict, tc.data), tc.stop)
			if err != nil {
				t.Fatalf("DecodeStreamUpTo: %v", err)
			}
			if found != tc.found {
				t.Errorf("found = %v, want %v", found, tc.found)
			}
			if !bytes.Equal(got, tc.want) {
				t.Errorf("bytes = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestDecodeStreamUpTo_UndecodableOuterFilterErrors asserts the deliberate
// difference from [DecodeStreamChecked]: a filter before the stop filter that
// cannot be decoded is an error, not a skip. Its output is the only route to
// the stop filter's input, so continuing would judge the wrong bytes.
func TestDecodeStreamUpTo_UndecodableOuterFilterErrors(t *testing.T) {
	t.Parallel()

	stream := ast.NewStream(ast.PdfDict{
		"Filter": ast.PdfArray{ast.PdfName("NoSuchDecode"), ast.PdfName("JPXDecode")},
	}, []byte("not a codestream"))

	if _, _, err := DecodeStreamUpTo(context.Background(), stream, "JPXDecode"); err == nil {
		t.Fatal("want an error when a filter before the stop filter cannot be decoded, got nil")
	}
}
