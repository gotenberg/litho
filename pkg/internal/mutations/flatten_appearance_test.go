package mutations

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// TestAsFormXObjectSuppliesMissingEntries pins that an appearance stream gains
// what a form XObject needs before it is invoked from page content.
//
// An appearance stream is a form XObject by position (ISO 32000-1, 12.5.5), so a
// producer may omit /Subtype and readers still draw it through the annotation.
// Flattening moves it into the page's /XObject resources, where Do resolves it
// by dictionary and Table 95 makes /Subtype /Form and /BBox required. Copying it
// unchanged left pdfjs checkbox-bad-appearance.pdf with a "/FlatAP20 Do" every
// reader skipped, so a checked box lost its tick on a page that still reported
// a successful flatten.
func TestAsFormXObjectSuppliesMissingEntries(t *testing.T) {
	t.Parallel()

	body := []byte("q 0 0 0.4 rg BT /F3 12 Tf 0 0 Td (4) Tj ET Q")
	stream := ast.NewStream(ast.PdfDict{"Length": ast.PdfInt(len(body))}, body)

	got := asFormXObject(stream, 20, -12)

	if got.Dict["Subtype"] != ast.PdfName("Form") {
		t.Errorf("/Subtype = %v, want /Form", got.Dict["Subtype"])
	}
	if got.Dict["Type"] != ast.PdfName("XObject") {
		t.Errorf("/Type = %v, want /XObject", got.Dict["Type"])
	}
	box, ok := got.Dict["BBox"].(ast.PdfArray)
	if !ok || len(box) != 4 {
		t.Fatalf("/BBox = %v, want a four-element array", got.Dict["BBox"])
	}
	// The rectangle's corners may arrive in either order (7.9.5), so the box is
	// the absolute extent.
	if box[2] != ast.PdfReal(20) || box[3] != ast.PdfReal(12) {
		t.Errorf("/BBox = %v, want [0 0 20 12]", box)
	}

	data, err := got.Bytes(context.Background())
	if err != nil {
		t.Fatalf("Bytes: %v", err)
	}
	if string(data) != string(body) {
		t.Errorf("stream bytes changed: %q", data)
	}
}

// TestAsFormXObjectLeavesADeclaredBoxAlone pins that a stream carrying its own
// /BBox keeps it. ISO 32000-1, 12.5.5 maps that box onto the annotation's /Rect,
// so replacing it would move or rescale the appearance.
func TestAsFormXObjectLeavesADeclaredBoxAlone(t *testing.T) {
	t.Parallel()

	declared := ast.PdfArray{ast.PdfInt(1), ast.PdfInt(2), ast.PdfInt(3), ast.PdfInt(4)}
	stream := ast.NewStream(ast.PdfDict{
		"Subtype": ast.PdfName("Form"),
		"BBox":    declared,
	}, []byte("x"))

	got := asFormXObject(stream, 99, 99)
	box, ok := got.Dict["BBox"].(ast.PdfArray)
	if !ok || len(box) != 4 {
		t.Fatalf("/BBox = %v", got.Dict["BBox"])
	}
	for i := range declared {
		if box[i] != declared[i] {
			t.Fatalf("/BBox = %v, want the declared %v", box, declared)
		}
	}
}

// badAppearanceFormPDF builds a page with one checked widget whose appearance
// stream declares neither /Subtype nor /BBox, as pdfjs
// checkbox-bad-appearance.pdf does.
func badAppearanceFormPDF() []byte {
	appearance := "q 0 0 0.4 rg BT /F3 12 Tf 0 0 Td (4) Tj ET Q"

	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Btn /T (box) /Rect [10 700 30 716] " +
			"/V /on /AS /on /AP << /N << /on 5 0 R >> >> >>",
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

// TestFlattenFormEmitsADrawableXObject pins the wiring: the appearance the
// flatten path promotes into page resources must be one a reader will draw.
func TestFlattenFormEmitsADrawableXObject(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := badAppearanceFormPDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	alloc := writer.NewObjAllocator(doc.HighestObjectNumber())
	if err := FlattenForm(ctx, alloc, doc); err != nil {
		t.Fatalf("FlattenForm: %v", err)
	}

	var promoted []ast.PdfStream
	for _, obj := range alloc.Pending() {
		if s, ok := obj.(ast.PdfStream); ok {
			if _, isForm := s.Dict["BBox"]; isForm {
				promoted = append(promoted, s)
			}
		}
	}
	if len(promoted) != 1 {
		t.Fatalf("got %d promoted appearance streams, want 1", len(promoted))
	}
	if promoted[0].Dict["Subtype"] != ast.PdfName("Form") {
		t.Errorf("/Subtype = %v, want /Form so \"Do\" draws it", promoted[0].Dict["Subtype"])
	}
}
