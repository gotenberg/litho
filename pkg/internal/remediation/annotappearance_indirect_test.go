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

// indirectAPPDF builds a page with one widget whose /AP is written as an
// indirect reference. The appearance it points at draws text.
//
// # Corpus evidence
//
//   - pdfjs bug1778692.pdf: 36 of its 63 widgets write /AP as an indirect
//     reference, the shape this fixture reproduces.
func indirectAPPDF() []byte {
	appearance := "q BT /F1 9 Tf 0 g 2 2 Td (Desired Hours) Tj ET Q"

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		// /AP is an indirect reference, not a direct dictionary.
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (hours) /V (Desired Hours) " +
			"/Rect [10 700 160 722] /AP 5 0 R >>",
		"<< /N 6 0 R >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 150 22] /Length %d >>\nstream\n%s\nendstream",
			len(appearance), appearance),
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

// TestAnnotAppearanceKeepsAnIndirectAP pins that an annotation whose /AP is an
// indirect reference keeps the appearance it already has.
//
// Any object may be given indirectly (ISO 32000-1, 7.3.10). Type-asserting the
// dictionary instead of resolving it made every such annotation look as though
// it had none, and the fixer replaced a real appearance with a blank form to
// satisfy ISO 19005-2, 6.3.3, erasing filled-in values from a file that was
// already conformant.
//
// # Corpus evidence
//
//   - pdfjs bug1778692.pdf: the blank-form replacement blanked 36 of 63
//     widgets, the appearance streams drawing text fell from 11 to 2, and 147
//     characters of filled-in values vanished from a file veraPDF called
//     conformant.
func TestAnnotAppearanceKeepsAnIndirectAP(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := indirectAPPDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	annot, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 4})
	if err != nil {
		t.Fatalf("resolving the widget: %v", err)
	}
	if _, direct := annot["AP"].(ast.PdfDict); direct {
		t.Fatal("fixture is wrong: /AP must be an indirect reference")
	}
	if needsAppearance(ctx, doc, annot) {
		t.Error("a widget whose indirect /AP resolves to a dictionary with /N was reported as missing an appearance")
	}

	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
	if _, err := fixAnnotAppearance(ctx, alloc, doc, Options{}, nil, ""); err != nil {
		t.Fatalf("fixAnnotAppearance: %v", err)
	}

	// The appearance stream must survive untouched: nothing may be queued that
	// replaces object 6, and the widget must still point at the same /AP.
	if replaced, queued := alloc.Pending()[6]; queued {
		t.Errorf("the existing appearance stream was overwritten: %v", replaced)
	}
	if patched, queued := alloc.Pending()[4].(ast.PdfDict); queued {
		if ref, ok := patched["AP"].(ast.PdfIndirectRef); !ok || ref.ObjectNum != 5 {
			t.Errorf("the widget's /AP was rewritten to %v, want the original 5 0 R", patched["AP"])
		}
	}
}
