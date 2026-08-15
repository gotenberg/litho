package remediation

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// needAppearancesPDF builds a form that relies on /NeedAppearances. The field
// carries a value; its stored appearance either paints that value or is the
// placeholder a producer leaves when the viewer is expected to regenerate.
//
// The appearance also lists an /XObject its content never invokes, the shape
// that made an earlier version of this check find text in an unrelated image.
func needAppearancesPDF(appearanceDraws bool) []byte {
	body := "/Tx BMC\nEMC\n"
	if appearanceDraws {
		body = "/Tx BMC\nBT /Helv 9 Tf (150) Tj ET\nEMC\n"
	}

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] /NeedAppearances true >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (qty) /V (150) /Rect [10 700 90 720] " +
			"/AP << /N 5 0 R >> >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 80 20] "+
			"/Resources << /XObject << /Im4 6 0 R >> >> /Length %d >>\nstream\n%sendstream", len(body), body),
		// An image XObject whose stream happens to contain a text operator. The
		// appearance never invokes it.
		"<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] /Length 22 >>\nstream\nBT (elsewhere) Tj ET\nendstream",
	}

	var b bytes.Buffer
	b.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	start := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, start)
	return b.Bytes()
}

func runNeedAppearances(t *testing.T, data []byte) []string {
	t.Helper()
	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber())
	fixes, err := fixNeedAppearances(ctx, alloc, doc, Options{}, nil, "")
	if err != nil {
		t.Fatalf("fixNeedAppearances: %v", err)
	}
	if queued, ok := alloc.Pending()[1].(ast.PdfDict); ok {
		if af, ok := queued["AcroForm"].(ast.PdfDict); ok {
			if _, still := af["NeedAppearances"]; still {
				t.Error("the flag was rewritten but kept")
			}
		}
	}
	return fixes
}

// TestNeedAppearancesKeptWhenItIsWhatDrawsTheValue pins that the flag stays when
// removing it would blank a field.
//
// ISO 19005-2, 6.9 forbids /NeedAppearances, but removing it hands authority to
// the stored appearance. A producer may store a placeholder and let the viewer
// draw /V (ISO 32000-1, 12.7.3.3): pdfjs issue12233.pdf keeps 12-to-45 byte
// appearances that paint nothing, and remediation dropped the values "Man",
// "150" and "Red" from the page while reporting the flag removed.
func TestNeedAppearancesKeptWhenItIsWhatDrawsTheValue(t *testing.T) {
	t.Parallel()

	if fixes := runNeedAppearances(t, needAppearancesPDF(false)); len(fixes) != 0 {
		t.Errorf("reported %v, want no fix: removing the flag would blank the field", fixes)
	}
}

// TestNeedAppearancesRemovedWhenTheAppearanceDrawsTheValue pins the other side.
// A stored appearance that already paints the value loses nothing when the
// viewer stops regenerating, so the violation is fixed.
func TestNeedAppearancesRemovedWhenTheAppearanceDrawsTheValue(t *testing.T) {
	t.Parallel()

	fixes := runNeedAppearances(t, needAppearancesPDF(true))
	if len(fixes) == 0 {
		t.Error("the appearance already draws the value, so the flag should have been removed")
	}
}
