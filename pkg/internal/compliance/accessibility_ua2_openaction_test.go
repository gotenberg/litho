package compliance

import (
	"context"
	"testing"
)

// TestCheckStructureDestinations_NullOpenAction pins that a catalog
// /OpenAction resolving to null is no destination (ISO 14289-2, 8.8 t1).
//
// ISO 32000-1, 7.3.7 treats a null dictionary value as an absent entry, and
// 7.3.10 makes a reference to an undefined object a reference to null. pdfjs
// issue16839.pdf writes /OpenAction 0 0 R, the free-list head; litho reported
// it as a destination that is not a structure destination while veraPDF, which
// builds no destination from it, reported nothing. The control keeps a real
// page destination failing the rule.
func TestCheckStructureDestinations_NullOpenAction(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		openAction string
		fires      bool
	}{
		{"reference to object 0", "0 0 R", false},
		{"direct null", "null", false},
		{"page destination", "[3 0 R /Fit]", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, cat := tableDoc(t, 1,
				"<< /Type /Catalog /Pages 2 0 R /OpenAction "+tt.openAction+" >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
			)
			got := checkStructureDestinations(context.Background(), doc, cat, "PDF/UA-2")
			if fired := hasRule(got, Rule_14289_2_8_8_t01); fired != tt.fires {
				t.Errorf("/OpenAction %s: 8.8 t1 fired = %v, want %v (%v)", tt.openAction, fired, tt.fires, got)
			}
		})
	}
}
