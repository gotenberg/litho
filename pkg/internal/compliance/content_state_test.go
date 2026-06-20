package compliance

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestInlineImageHasLZW(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		dict ast.PdfDict
		want bool
	}{
		{"abbrev /F /LZW", ast.PdfDict{"F": ast.PdfName("LZW")}, true},
		{"full /Filter /LZWDecode", ast.PdfDict{"Filter": ast.PdfName("LZWDecode")}, true},
		{"array /F [/LZW]", ast.PdfDict{"F": ast.PdfArray{ast.PdfName("LZW")}}, true},
		{"array with LZW among others", ast.PdfDict{"F": ast.PdfArray{ast.PdfName("AHx"), ast.PdfName("LZWDecode")}}, true},
		{"flate is fine", ast.PdfDict{"F": ast.PdfName("Fl")}, false},
		{"no filter", ast.PdfDict{"W": ast.PdfInt(10)}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := inlineImageHasLZW(tc.dict); got != tc.want {
				t.Errorf("inlineImageHasLZW(%v) = %v, want %v", tc.dict, got, tc.want)
			}
		})
	}
}
