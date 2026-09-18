package typography

import (
	"bytes"
	"context"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// formThenImagePDF draws a form XObject and then, back on the page, an image.
// Both live in the PAGE's resources; the form's own /Resources names neither.
func formThenImagePDF() []byte {
	form := "BT /F1 12 Tf (Z) Tj ET"
	body := "/Fm1 Do q 100 0 0 50 10 20 cm /Im1 Do Q"
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R >> /XObject << /Fm1 7 0 R /Im1 8 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"7 0 obj\n<< /Type /XObject /Subtype /Form /BBox [0 0 50 50] " +
		"/Resources << /Font << /F1 4 0 R >> >> /Length " + strconv.Itoa(len(form)) +
		" >>\nstream\n" + form + "\nendstream\nendobj\n" +
		"8 0 obj\n<< /Type /XObject /Subtype /Image /Width 1 /Height 1 " +
		"/ColorSpace /DeviceGray /BitsPerComponent 8 /Length 1 >>\nstream\n \nendstream\nendobj\n" +
		"trailer\n<< /Size 9 /Root 1 0 R >>\n%%EOF\n")
}

// TestFormRecursionRestoresResources pins that a Do puts the machine's
// resources back before the page continues.
//
// The form's /Resources names no /Im1, so if the recursion leaves the machine
// pointing at them, the page's own image cannot be resolved: it is counted
// unresolved and its placement is lost. A caller clearing a region would then
// refuse a page it can perfectly well read, or miss the image entirely.
func TestFormRecursionRestoresResources(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	data := formThenImagePDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) == 0 {
		t.Fatalf("ResolvePages: %v (%d pages)", err, len(pages))
	}

	ops, err := content.ParsePageContent(ctx, doc, pages[0].Dict)
	if err != nil {
		t.Fatalf("ParsePageContent: %v", err)
	}

	machine := graphics.NewMachine([4]float64{0, 0, 612, 792})
	machine.RecordImageBoxes = true
	if _, err := ExtractElements(ctx, doc, machine, pages[0], ops); err != nil {
		t.Fatalf("ExtractElements: %v", err)
	}

	if n := machine.UnresolvedXObjects(); n != 0 {
		t.Errorf("UnresolvedXObjects() = %d, want 0: the page's own /Im1 is reachable", n)
	}
	boxes := machine.ImageBoxes()
	if len(boxes) != 1 {
		t.Fatalf("ImageBoxes() = %v, want the page's image", boxes)
	}
	want := graphics.ImageBox{Name: "Im1", MinX: 10, MinY: 20, MaxX: 110, MaxY: 70}
	if boxes[0] != want {
		t.Errorf("box = %+v, want %+v", boxes[0], want)
	}
}

// TestFormRecursionRestoresDepth pins that geometry drawn after a Do is
// attributed to the page, not to the form that just returned.
func TestFormRecursionRestoresDepth(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	data := formThenImagePDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) == 0 {
		t.Fatalf("ResolvePages: %v (%d pages)", err, len(pages))
	}

	ops, err := content.ParsePageContent(ctx, doc, pages[0].Dict)
	if err != nil {
		t.Fatalf("ParsePageContent: %v", err)
	}

	machine := graphics.NewMachine([4]float64{0, 0, 612, 792})
	if _, err := ExtractElements(ctx, doc, machine, pages[0], ops); err != nil {
		t.Fatalf("ExtractElements: %v", err)
	}

	// The walk ends back on the page, so the depth it finishes at is the page's.
	if machine.FormDepth != 0 {
		t.Errorf("FormDepth = %d after the walk, want 0: the form's depth outlived its recursion", machine.FormDepth)
	}
}
