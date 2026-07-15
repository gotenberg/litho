package parser

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestStreamUsesIdentityCryptFilter covers the exemption that keeps an
// Identity-crypt-filtered stream (for example an unencrypted /Metadata stream in
// an encrypted file) from being decrypted with the document's default crypt
// filter (ISO 32000-1 §7.6.5).
func TestStreamUsesIdentityCryptFilter(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		dict ast.PdfDict
		want bool
	}{
		{
			name: "crypt with explicit Identity name",
			dict: ast.PdfDict{"Filter": ast.PdfName("Crypt"), "DecodeParms": ast.PdfDict{"Name": ast.PdfName("Identity")}},
			want: true,
		},
		{
			name: "crypt with no decode params defaults to Identity",
			dict: ast.PdfDict{"Filter": ast.PdfName("Crypt")},
			want: true,
		},
		{
			name: "crypt with a named (non-Identity) filter is encrypted",
			dict: ast.PdfDict{"Filter": ast.PdfName("Crypt"), "DecodeParms": ast.PdfDict{"Name": ast.PdfName("StdCF")}},
			want: false,
		},
		{
			name: "crypt first in a filter array with matching Identity params",
			dict: ast.PdfDict{
				"Filter":      ast.PdfArray{ast.PdfName("Crypt"), ast.PdfName("FlateDecode")},
				"DecodeParms": ast.PdfArray{ast.PdfDict{"Name": ast.PdfName("Identity")}, ast.PdfNull{}},
			},
			want: true,
		},
		{
			name: "non-crypt filter",
			dict: ast.PdfDict{"Filter": ast.PdfName("FlateDecode")},
			want: false,
		},
		{
			name: "no filter",
			dict: ast.PdfDict{},
			want: false,
		},
	}
	for _, c := range cases {
		if got := streamUsesIdentityCryptFilter(c.dict); got != c.want {
			t.Errorf("%s: streamUsesIdentityCryptFilter = %v, want %v", c.name, got, c.want)
		}
	}
}
