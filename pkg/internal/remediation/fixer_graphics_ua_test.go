package remediation

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
)

// TestMatchGraphics_ClaimsTheUAReferenceXObjectBan closes a restatement gap.
//
// ISO 14289-1 §7.20 t1 and ISO 19005-1 §6.2.6 t1 carry the same sentence word
// for word: a conforming file shall not contain any reference XObjects. The
// PDF/A spelling was claimed and repaired while the PDF/UA one was not, so a
// UA-1 target kept a /Ref this fixer already knew how to delete.
//
// [fixGraphics] takes the profile as an ignored parameter and never consults
// it, and fixResourcesGraphics deletes /Ref from every form XObject it walks,
// so the repair runs for a UA-1 target exactly as it does for PDF/A-1.
//
// Matched on the whole rule rather than the clause. t2 of the same clause is a
// different requirement, pinned by the control below.
func TestMatchGraphics_ClaimsTheUAReferenceXObjectBan(t *testing.T) {
	t.Parallel()

	if !matchGraphics(compliance.Violation{Rule: compliance.Rule_14289_1_7_20_t01}) {
		t.Error("must claim ISO 14289-1 7.20 t1: the reference XObject ban this fixer repairs")
	}
}

// TestMatchGraphics_LeavesTheUAFormXObjectTaggingAlone is the negative control
// for that clause, and the reason 7.20 is matched by rule and not by number.
//
// §7.20 t2 requires the content of form XObjects to be incorporated into
// structure elements per ISO 32000-1, 14.7.2. That is a tagging requirement:
// deleting /Ref does nothing for it, so claiming it would report a repair that
// never happens and drop the violation out of the residual.
func TestMatchGraphics_LeavesTheUAFormXObjectTaggingAlone(t *testing.T) {
	t.Parallel()

	if matchGraphics(compliance.Violation{Rule: compliance.Rule_14289_1_7_20_t02}) {
		t.Error("must not claim ISO 14289-1 7.20 t2: incorporating form XObject content into the structure tree is not a /Ref deletion")
	}
}
