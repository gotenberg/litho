package pdf

import (
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// contentMarkingViolations returns the PDF/UA-1 violations of data under ISO
// 14289-1, 7.1 test 3: content neither tagged as real content nor marked as
// an artifact.
func contentMarkingViolations(t *testing.T, data []byte) []Violation {
	t.Helper()
	got, err := openColorPDF(t, data).ValidatePDFUAReport(t.Context(), models.PDFUA1)
	if err != nil {
		t.Fatalf("ValidatePDFUAReport: %v", err)
	}
	var marking []Violation
	for _, v := range got {
		if v.Clause == "7.1" && v.TestNumber == 3 {
			marking = append(marking, v)
		}
	}
	return marking
}

// TestStampText_KeepsATaggedDocumentsContentMarked pins that a stamp on a
// tagged document marks itself an artifact (ISO 32000-1, 14.8.2.2), so the
// document gains no violation of ISO 14289-1, 7.1 test 3. The stamp is
// appended after the structure tree was built, so it belongs to no
// marked-content sequence unless it declares one. The document is tagged and
// written first, then stamped by a second Writer, as a caller stamping a
// tagged file does.
func TestStampText_KeepsATaggedDocumentsContentMarked(t *testing.T) {
	t.Parallel()

	tagger := NewWriter(openColorPDF(t, untaggedHeadingPDF()))
	t.Cleanup(func() { _ = tagger.Close() })
	tagger.AutoTag()
	tagged := writeAll(t, tagger)
	if v := contentMarkingViolations(t, tagged); len(v) != 0 {
		t.Fatalf("precondition: the tagged document already violates 7.1 test 3: %v", v)
	}

	stamper := NewWriter(openColorPDF(t, tagged))
	t.Cleanup(func() { _ = stamper.Close() })
	if err := stamper.StampText(TextStamp{Text: "WATERMARK", X: 100, Y: 400}); err != nil {
		t.Fatalf("StampText: %v", err)
	}
	if v := contentMarkingViolations(t, writeAll(t, stamper)); len(v) != 0 {
		t.Errorf("the stamp left content neither tagged nor an artifact: %v", v)
	}
}
