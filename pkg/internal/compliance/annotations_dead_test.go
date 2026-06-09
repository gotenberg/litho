package compliance

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

func TestAnnotColorArrays(t *testing.T) {
	withC := ast.PdfDict{"C": ast.PdfArray{}}
	withIC := ast.PdfDict{"IC": ast.PdfArray{}}
	none := ast.PdfDict{}

	// Non-RGB OutputIntent with a /C or /IC array fails §6.5.3 t3.
	if got := checkAnnotColorArrays(withC, "CMYK", "loc"); !hasRule(got, Rule_19005_1_6_5_3_t03) {
		t.Errorf("CMYK OutputIntent + /C: got %v, want §6.5.3 t3", got)
	}
	if got := checkAnnotColorArrays(withIC, "GRAY", "loc"); !hasRule(got, Rule_19005_1_6_5_3_t03) {
		t.Errorf("GRAY OutputIntent + /IC: got %v, want §6.5.3 t3", got)
	}

	// RGB OutputIntent permits /C and /IC.
	if got := checkAnnotColorArrays(withC, "RGB ", "loc"); len(got) != 0 {
		t.Errorf("RGB OutputIntent + /C: got %v, want none", got)
	}

	// An unresolved colour space is never flagged (FP-safe).
	if got := checkAnnotColorArrays(withC, "", "loc"); len(got) != 0 {
		t.Errorf("unresolved OutputIntent + /C: got %v, want none", got)
	}

	// No /C or /IC array is conformant regardless of colour space.
	if got := checkAnnotColorArrays(none, "CMYK", "loc"); len(got) != 0 {
		t.Errorf("no colour array: got %v, want none", got)
	}
}
