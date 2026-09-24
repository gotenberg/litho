package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestAnnotColorArrays pins ISO 19005-1 §6.5.3 t3 on /C and /IC colour arrays
// against RGB, non-RGB and unparseable OutputIntent colour spaces.
func TestAnnotColorArrays(t *testing.T) {
	withC := ast.PdfDict{"C": ast.PdfArray{}}
	withIC := ast.PdfDict{"IC": ast.PdfArray{}}
	none := ast.PdfDict{}

	// Non-RGB OutputIntent with a /C or /IC array fails §6.5.3 t3.
	if got := checkAnnotColorArrays(context.Background(), nopDoc(), withC, "CMYK", true, "loc"); !hasRule(got, Rule_19005_1_6_5_3_t03) {
		t.Errorf("CMYK OutputIntent + /C: got %v, want §6.5.3 t3", got)
	}
	if got := checkAnnotColorArrays(context.Background(), nopDoc(), withIC, "GRAY", true, "loc"); !hasRule(got, Rule_19005_1_6_5_3_t03) {
		t.Errorf("GRAY OutputIntent + /IC: got %v, want §6.5.3 t3", got)
	}

	// RGB OutputIntent permits /C and /IC.
	if got := checkAnnotColorArrays(context.Background(), nopDoc(), withC, "RGB ", true, "loc"); len(got) != 0 {
		t.Errorf("RGB OutputIntent + /C: got %v, want none", got)
	}

	// A present but undecodable profile leaves the colour space unresolved; stay
	// lenient to avoid false-rejecting a valid RGB profile litho cannot parse.
	if got := checkAnnotColorArrays(context.Background(), nopDoc(), withC, "", true, "loc"); len(got) != 0 {
		t.Errorf("unparseable OutputIntent profile + /C: got %v, want none", got)
	}

	// No /C or /IC array is conformant regardless of colour space.
	if got := checkAnnotColorArrays(context.Background(), nopDoc(), none, "CMYK", true, "loc"); len(got) != 0 {
		t.Errorf("no colour array: got %v, want none", got)
	}
}
