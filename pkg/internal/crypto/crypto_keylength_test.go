package crypto

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestKeyLengthBits pins where each handler's key length comes from.
//
// A V 4 dictionary need not carry a top-level /Length (ISO 32000-1, Table 20
// defines it only for V 2 and 3), and defaulting it to 40 bits cut the 128-bit
// AESV2 key to 5 bytes, so the right password failed.
func TestKeyLengthBits(t *testing.T) {
	t.Parallel()

	stdCF := func(filter ast.PdfDict) ast.PdfDict {
		return ast.PdfDict{"StdCF": filter}
	}
	for _, tc := range []struct {
		name string
		dict ast.PdfDict
		v    int
		want int
	}{
		{"V 1 without /Length takes the Table 20 default", ast.PdfDict{}, 1, 40},
		{"V 2 declares its own", ast.PdfDict{"Length": ast.PdfInt(128)}, 2, 128},
		// A declared zero is out of range, not absent: the derivation refuses it.
		{"V 2 declared zero is kept", ast.PdfDict{"Length": ast.PdfInt(0)}, 2, 0},
		{
			name: "V 4 without /Length reads the crypt filter, in bytes",
			dict: ast.PdfDict{"StmF": ast.PdfName("StdCF"), "CF": stdCF(ast.PdfDict{"CFM": ast.PdfName("AESV2"), "Length": ast.PdfInt(16)})},
			v:    4,
			want: 128,
		},
		{
			name: "V 4 crypt filter length written in bits",
			dict: ast.PdfDict{"StmF": ast.PdfName("StdCF"), "CF": stdCF(ast.PdfDict{"CFM": ast.PdfName("AESV2"), "Length": ast.PdfInt(128)})},
			v:    4,
			want: 128,
		},
		{
			name: "V 4 crypt filter with no /Length",
			dict: ast.PdfDict{"StmF": ast.PdfName("StdCF"), "CF": stdCF(ast.PdfDict{"CFM": ast.PdfName("AESV2")})},
			v:    4,
			want: 128,
		},
		{"V 4 with no crypt filter at all", ast.PdfDict{}, 4, 128},
		{
			name: "V 4 top-level /Length wins",
			dict: ast.PdfDict{"Length": ast.PdfInt(128), "StmF": ast.PdfName("StdCF"), "CF": stdCF(ast.PdfDict{"Length": ast.PdfInt(5)})},
			v:    4,
			want: 128,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := keyLengthBits(tc.dict, tc.v); got != tc.want {
				t.Errorf("keyLengthBits = %d, want %d", got, tc.want)
			}
		})
	}
}
