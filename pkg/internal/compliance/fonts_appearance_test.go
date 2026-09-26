package compliance

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// appearanceFontPDF builds a one-page PDF whose page declares NO /Font and whose
// only font is a subset Type1 font reachable solely through a Widget
// annotation's /AP /N appearance stream. fdExtra is spliced into the
// FontDescriptor (for example "/CharSet (/H)") to toggle the CharSet-presence
// defect that ISO 19005-1 §6.3.5 t2 reports for an un-listed Type1 subset, and
// extra objects follow object 7, so a descriptor can name a program at 8 0 R.
func appearanceFontPDF(fdExtra string, extra ...string) []byte {
	apContent := "BT /F0 12 Tf (Hi) Tj ET"
	apStream := fmt.Sprintf(
		"<< /Type /XObject /Subtype /Form /BBox [0 0 200 100] "+
			"/Resources << /Font << /F0 6 0 R >> >> /Length %d >>\nstream\n%s\nendstream",
		len(apContent), apContent)

	return cyclePDF(append([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		// The page intentionally has no /Resources/Font.
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /Rect [0 0 200 100] /AP << /N 5 0 R >> >>",
		apStream,
		"<< /Type /Font /Subtype /Type1 /BaseFont /ABCDEF+Helvetica /FontDescriptor 7 0 R >>",
		"<< /Type /FontDescriptor /FontName /ABCDEF+Helvetica /Flags 4 " + fdExtra + ">>",
	}, extra...)...)
}

// embeddedCharSetProgram returns the descriptor entries and the program
// stream of a Type1 subset whose /CharSet lists the two glyphs the appearance
// shows, H and i, over the embedded Lato-Bold CFF subset, stored as object
// programObj. The program has to be there: ISO 19005-1, 6.3.5 t2 fails the
// /CharSet of a subset the file does not embed, whatever it lists (see
// [TestCharSetOfAnUnembeddedSubset]).
func embeddedCharSetProgram(t *testing.T, programObj int) (fdExtra, program string) {
	t.Helper()
	cff, err := os.ReadFile("testdata/lato_bold_subset.cff")
	if err != nil {
		t.Fatalf("read CFF fixture: %v", err)
	}
	return fmt.Sprintf("/CharSet (/H/i) /FontFile3 %d 0 R ", programObj), exponentStream("/Subtype /Type1C", string(cff))
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

// TestAppearanceFont_SubsetCharSetReached pins that a Type1 subset used
// only inside an annotation appearance stream, with no page /Font, must still be
// checked for /CharSet presence (§6.3.5 t2). Before the appearance-font pass the
// font was unreachable and the defect was silently missed.
func TestAppearanceFont_SubsetCharSetReached(t *testing.T) {
	if !firesRule(t, appearanceFontPDF(""), "6.3.5", 2) {
		t.Error("missing /CharSet on a Type1 subset in an annotation appearance must fire 6.3.5 t2")
	}
}

// TestAppearanceFont_CharSetPresentNoFire is the control: an embedded subset
// whose /CharSet lists the glyphs it shows satisfies the part-1 rule, so no
// §6.3.5 t2 violation. This guards against the appearance pass over-firing on
// a conformant subset.
func TestAppearanceFont_CharSetPresentNoFire(t *testing.T) {
	fdExtra, program := embeddedCharSetProgram(t, 8)
	if firesRule(t, appearanceFontPDF(fdExtra, program), "6.3.5", 2) {
		t.Error("a Type1 subset with /CharSet present must not fire 6.3.5 t2")
	}
}

// nestedAppearanceFontPDF builds a one-page PDF whose page declares NO /Font and
// whose appearance stream declares none either: the only font hangs off a Form
// XObject the appearance invokes with a Do. veraPDF reaches such a font through
// annots[0]/appearance[0]/contentStream[0]/operators[N]/xObject[0]/contentStream[0]
// and litho stopped at the appearance's own /Resources/Font.
//
// # Corpus evidence
//
//   - pdfjs bug854315.pdf: writes this shape.
func nestedAppearanceFontPDF(fdExtra string, extra ...string) []byte {
	inner := "BT /F0 12 Tf (Hi) Tj ET"
	innerStream := fmt.Sprintf(
		"<< /Type /XObject /Subtype /Form /BBox [0 0 200 100] "+
			"/Resources << /Font << /F0 7 0 R >> >> /Length %d >>\nstream\n%s\nendstream",
		len(inner), inner)

	outer := "q /FRM Do Q"
	outerStream := fmt.Sprintf(
		"<< /Type /XObject /Subtype /Form /BBox [0 0 200 100] "+
			"/Resources << /XObject << /FRM 6 0 R >> >> /Length %d >>\nstream\n%s\nendstream",
		len(outer), outer)

	return cyclePDF(append([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		// Neither the page nor the appearance names a font.
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /Rect [0 0 200 100] /AP << /N 5 0 R >> >>",
		outerStream,
		innerStream,
		"<< /Type /Font /Subtype /Type1 /BaseFont /ABCDEF+Helvetica /FontDescriptor 8 0 R >>",
		"<< /Type /FontDescriptor /FontName /ABCDEF+Helvetica /Flags 4 " + fdExtra + ">>",
	}, extra...)...)
}

// TestAppearanceFont_NestedFormReached pins that a font reached only through a
// Form XObject invoked inside an annotation appearance is still judged. The
// appearance's own /Resources name no font, so the walk used to return before
// decoding the stream and the subset defect went unreported (ISO 19005-1
// §6.3.5 t2).
func TestAppearanceFont_NestedFormReached(t *testing.T) {
	if !firesRule(t, nestedAppearanceFontPDF(""), "6.3.5", 2) {
		t.Error("a Type1 subset behind a Form XObject in an appearance must fire 6.3.5 t2")
	}
}

// TestAppearanceFont_NestedFormCharSetPresentNoFire is the control: the same
// nesting with an embedded subset and a conformant /CharSet must stay silent,
// so reaching deeper does not turn into over-reporting.
func TestAppearanceFont_NestedFormCharSetPresentNoFire(t *testing.T) {
	fdExtra, program := embeddedCharSetProgram(t, 9)
	if firesRule(t, nestedAppearanceFontPDF(fdExtra, program), "6.3.5", 2) {
		t.Error("a nested Type1 subset with /CharSet present must not fire 6.3.5 t2")
	}
}
