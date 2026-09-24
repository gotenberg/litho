package parser

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestParseDictKeepsKeysAfterStrayDelimiter pins that a stray delimiter among a
// dictionary's keys costs only that token.
//
// A stray keyword was already tolerated; a stray delimiter was not, and the
// asymmetry cost the whole dictionary. Damaged files produce both.
//
// # Corpus evidence
//
//   - pdfjs poppler-742-0-fuzzed.pdf: closes its /AcroForm with `/Q 0 > >>`
//     and lost every form field. qpdf and poppler read it.
//   - pdfjs poppler-937-0-fuzzed.pdf: writes `/Kids 3 0 R]` and lost its page
//     tree. qpdf and poppler read it.
func TestParseDictKeepsKeysAfterStrayDelimiter(t *testing.T) {
	for _, tc := range []struct {
		name string
		src  string
	}{
		{"stray closing angle", "<< /A 1 > /B 2 >>"},
		{"stray closing bracket", "<< /A 1 ] /B 2 >>"},
		{"stray bracket after a reference", "<< /A 3 0 R] /B 2 >>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			obj := parseOne(t, tc.src)
			dict, ok := obj.(ast.PdfDict)
			if !ok {
				t.Fatalf("ParseObject(%q) = %T, want ast.PdfDict", tc.src, obj)
			}
			if _, ok := dict["A"]; !ok {
				t.Errorf("ParseObject(%q) lost /A: %v", tc.src, dict)
			}
			if got, ok := dict["B"]; !ok {
				t.Errorf("ParseObject(%q) lost the key after the stray delimiter: %v", tc.src, dict)
			} else if got != ast.PdfInt(2) {
				t.Errorf("ParseObject(%q) /B = %v, want 2", tc.src, got)
			}
		})
	}
}

// TestParseDictStillClosesOnDoubleAngle checks the tolerance did not swallow a
// real dictionary terminator, which would run one dictionary into the next.
func TestParseDictStillClosesOnDoubleAngle(t *testing.T) {
	obj := parseOne(t, "<< /A 1 >> << /B 2 >>")
	dict, ok := obj.(ast.PdfDict)
	if !ok {
		t.Fatalf("ParseObject = %T, want ast.PdfDict", obj)
	}
	if _, taken := dict["B"]; taken {
		t.Errorf("first dictionary absorbed the second: %v", dict)
	}
	if got := dict["A"]; got != ast.PdfInt(1) {
		t.Errorf("/A = %v, want 1", got)
	}
}
