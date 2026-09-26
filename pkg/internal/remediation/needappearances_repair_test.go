package remediation

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// badAppearancePDF builds a page with one checked widget whose appearance stream
// carries neither /Subtype nor /BBox, and an /AcroForm that relies on
// /NeedAppearances to have it drawn.
//
// # Corpus evidence
//
//   - pdfjs checkbox-bad-appearance.pdf: is shaped this way.
func badAppearancePDF() []byte {
	appearance := "q 0 0 0.4 rg BT /F3 12 Tf 0 0 Td (4) Tj ET Q"

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] /NeedAppearances true >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Btn /T (box) /Rect [10 700 30 716] " +
			"/V /on /AS /on /AP << /N << /on 5 0 R /Off /Off >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(appearance), appearance),
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

// TestNeedAppearancesRepairKeepsTheAppearanceDrawable pins that removing
// /NeedAppearances leaves the stored appearances usable.
//
// ISO 19005-2, 6.9 forbids the flag, and dropping it hands authority back to the
// stored streams: the viewer stops regenerating and draws what the file holds.
// An appearance stream is a form XObject (ISO 32000-1, 12.5.5) and Table 95
// requires /Subtype /Form and /BBox, which a producer expecting regeneration can
// omit. Such a file then comes out conformant with its tick gone: the mark that
// says the box is checked simply stops being drawn.
//
// # Corpus evidence
//
//   - pdfjs checkbox-bad-appearance.pdf: came out conformant with its tick
//     gone.
func TestNeedAppearancesRepairKeepsTheAppearanceDrawable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := badAppearancePDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))

	fixes, err := fixNeedAppearances(ctx, alloc, doc, Options{}, nil, "")
	if err != nil {
		t.Fatalf("fixNeedAppearances: %v", err)
	}
	if !slices.ContainsFunc(fixes, func(f string) bool { return strings.Contains(f, "appearance stream") }) {
		t.Errorf("appearance streams were rewritten but not reported: %v", fixes)
	}

	stream, ok := alloc.Pending()[5].(ast.PdfStream)
	if !ok {
		t.Fatalf("appearance stream not rewritten: %T", alloc.Pending()[5])
	}
	if stream.Dict["Subtype"] != ast.PdfName("Form") {
		t.Errorf("/Subtype = %v, want /Form so a viewer draws it", stream.Dict["Subtype"])
	}
	box, ok := stream.Dict["BBox"].(ast.PdfArray)
	if !ok || len(box) != 4 {
		t.Fatalf("/BBox = %v, want a four-element array", stream.Dict["BBox"])
	}
	// Sized from /Rect [10 700 30 716].
	if box[2] != ast.PdfReal(20) || box[3] != ast.PdfReal(16) {
		t.Errorf("/BBox = %v, want [0 0 20 16] from the widget rectangle", box)
	}

	body, err := stream.Bytes(ctx)
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	if !strings.Contains(string(body), "(4) Tj") {
		t.Errorf("appearance body changed: %q", body)
	}
}
