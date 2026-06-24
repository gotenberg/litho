package compliance

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// appearanceFontPDF builds a one-page PDF whose page declares NO /Font and whose
// only font is a subset Type1 font reachable solely through a Widget
// annotation's /AP /N appearance stream. fdExtra is spliced into the
// FontDescriptor (for example "/CharSet (/H)") to toggle the CharSet-presence
// defect that ISO 19005-1 §6.3.5 t2 reports for an un-listed Type1 subset.
func appearanceFontPDF(fdExtra string) []byte {
	apContent := "BT /F0 12 Tf (Hi) Tj ET"
	apStream := fmt.Sprintf(
		"<< /Type /XObject /Subtype /Form /BBox [0 0 200 100] "+
			"/Resources << /Font << /F0 6 0 R >> >> /Length %d >>\nstream\n%s\nendstream",
		len(apContent), apContent)

	return cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		// The page intentionally has no /Resources/Font.
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /Rect [0 0 200 100] /AP << /N 5 0 R >> >>",
		apStream,
		"<< /Type /Font /Subtype /Type1 /BaseFont /ABCDEF+Helvetica /FontDescriptor 7 0 R >>",
		"<< /Type /FontDescriptor /FontName /ABCDEF+Helvetica /Flags 4 "+fdExtra+">>",
	)
}

func firesRule(t *testing.T, data []byte, clause string, test int) bool {
	t.Helper()
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	viols, err := ValidatePDFA(context.Background(), doc, models.PDFA1b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	for _, e := range viols {
		v, ok := e.(Violation)
		if ok && v.Rule.Clause == clause && v.Rule.TestNumber == test {
			return true
		}
	}
	return false
}

// TestAppearanceFont_SubsetCharSetReached pins Project C: a Type1 subset used
// only inside an annotation appearance stream, with no page /Font, must still be
// checked for /CharSet presence (§6.3.5 t2). Before the appearance-font pass the
// font was unreachable and the defect was silently missed.
func TestAppearanceFont_SubsetCharSetReached(t *testing.T) {
	if !firesRule(t, appearanceFontPDF(""), "6.3.5", 2) {
		t.Error("missing /CharSet on a Type1 subset in an annotation appearance must fire 6.3.5 t2")
	}
}

// TestAppearanceFont_CharSetPresentNoFire is the control: a present /CharSet
// satisfies the part-1 presence requirement, so no §6.3.5 t2 violation. This
// guards against the appearance pass over-firing on a conformant subset.
func TestAppearanceFont_CharSetPresentNoFire(t *testing.T) {
	if firesRule(t, appearanceFontPDF("/CharSet (/H) "), "6.3.5", 2) {
		t.Error("a Type1 subset with /CharSet present must not fire 6.3.5 t2")
	}
}
