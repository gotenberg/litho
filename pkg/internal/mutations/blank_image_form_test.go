package mutations

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// rasterFixture returns the first page of a 200 by 200 PDF whose page draws
// body. Object 4 is Helvetica as /F1, object 5 a 2 by 2 picture as the page's
// /Im0, object 6 a form as /Fm0 drawing form, with its own /Im0, object 7,
// another picture, and object 8 an axial shading as /Sh0.
func rasterFixture(t *testing.T, body, form string) (*document.Document, *document.Page) {
	t.Helper()
	stream := func(dict, data string) string {
		return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(data), data)
	}
	picture := "/Type /XObject /Subtype /Image /Width 2 /Height 2 /ColorSpace /DeviceRGB /BitsPerComponent 8"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 9 0 R /Resources << /Font << /F1 4 0 R >>" +
			" /XObject << /Im0 5 0 R /Fm0 6 0 R >> /Shading << /Sh0 8 0 R >> >> >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		stream(picture, "\xff\x00\x00\x00\xff\x00\x00\x00\xff\xff\xff\xff"),
		stream("/Type /XObject /Subtype /Form /BBox [0 0 200 200] /Resources << /Font << /F1 4 0 R >> /XObject << /Im0 7 0 R >> >>", form),
		stream(picture, "\x00\xff\x00\x00\xff\x00\x00\xff\x00\x00\xff\x00"),
		"<< /ShadingType 2 /ColorSpace /DeviceRGB /Coords [0 0 200 0] /Function << /FunctionType 2 /Domain [0 1] /C0 [1 0 0] /C1 [0 0 1] /N 1 >> >>",
		stream("", body),
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	ctx := context.Background()
	doc, err := document.NewDocument(ctx, bytes.NewReader(buf.Bytes()), int64(buf.Len()), nil)
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
	return doc, pages[0]
}

// TestRedactPageRegions_BlanksTheImageAFormDraws pins that an image a form
// draws inside the region is the one blanked, not the page's image of the same
// name: read in the page's resources, the form's /Im0 named the page's
// picture, which was blanked where the form's lay, and the form's picture kept
// its samples inside the region.
func TestRedactPageRegions_BlanksTheImageAFormDraws(t *testing.T) {
	t.Parallel()
	doc, page := rasterFixture(t, "q 20 0 0 20 150 150 cm /Im0 Do Q /Fm0 Do", "q 40 0 0 40 20 20 cm /Im0 Do Q")
	alloc := writer.NewObjAllocator(20)
	report, err := RedactPageRegions(context.Background(), alloc, doc, page, [][4]float64{{0, 0, 100, 100}})
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
	if !slices.Equal(report.BlankedImageObjects, []int{7}) {
		t.Errorf("BlankedImageObjects = %v, want the form's picture, 7", report.BlankedImageObjects)
	}
	if _, rewritten := alloc.Pending()[5]; rewritten {
		t.Error("the page's own picture, outside the region, was rewritten")
	}
}
