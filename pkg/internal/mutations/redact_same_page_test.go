package mutations

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// samePageMembershipPDF builds a page drawing "SECRET" at y=500 under /OC
// /M: a visible copy when visible is set, else one a membership dictionary
// over a group that is off hides (ISO 32000-1, 8.11.2.2, Table 99: AnyOn).
func samePageMembershipPDF(visible bool) []byte {
	off := "/D << /OFF [6 0 R] >>"
	if visible {
		off = "/D << >>"
	}
	page := "/OC /M BDC BT /F1 12 Tf 100 500 Td (SECRET) Tj ET EMC"
	return objsPDF(
		"<< /Type /Catalog /Pages 2 0 R /OCProperties << /OCGs [6 0 R] "+off+" >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] "+
			"/Resources << /Font << /F1 5 0 R >> /Properties << /M 7 0 R >> >> /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(page), page),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Type /OCG /Name (L) >>",
		"<< /Type /OCMD /OCGs [6 0 R] >>",
	)
}

// TestSplitSamePageCopies_MembershipHiddenCopyKeepsItsHit pins that a copy
// a membership dictionary hides is not tolerated as visible text: the
// renderer does not draw it, so it is no copy a reader sees.
func TestSplitSamePageCopies_MembershipHiddenCopyKeepsItsHit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	hit := TokenHit{ObjectNum: 4, Token: "SECRET", Where: "stream"}
	regions := map[int][][4]float64{1: {{100, 690, 200, 720}}}
	for _, tc := range []struct {
		visible   bool
		tolerated int
	}{{visible: true, tolerated: 1}, {visible: false, tolerated: 0}} {
		doc, page, alloc := bakeDoc(t, samePageMembershipPDF(tc.visible))
		copies, rest, err := SplitSamePageCopies(ctx, alloc, doc, []*document.Page{page}, regions, []TokenHit{hit})
		if err != nil {
			t.Fatalf("visible=%v: SplitSamePageCopies: %v", tc.visible, err)
		}
		if len(copies) != tc.tolerated || len(rest) != 1-tc.tolerated {
			t.Errorf("visible=%v: copies %+v, rest %+v; want %d tolerated", tc.visible, copies, rest, tc.tolerated)
		}
	}
}
