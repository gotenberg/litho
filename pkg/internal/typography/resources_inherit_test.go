package typography

import (
	"bytes"
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// TestExtractResourcesInheritedWhole pins that text extraction reads a page's
// names in the nearest /Resources dictionary whole (ISO 32000-1, 7.7.3.4):
// the page tree root lends a form showing Z, a page stating no /Resources
// shows it, and a page whose own /XObject lacks it shows nothing, as PDF.js
// and poppler read it, though the merged resources reach the form.
func TestExtractResourcesInheritedWhole(t *testing.T) {
	t.Parallel()
	form := "BT /F1 12 Tf 10 10 Td (Z) Tj ET"
	body := "/Fm1 Do"
	for _, tt := range []struct {
		name      string
		resources string
		want      string
	}{
		{"inherited", "", "Z"},
		{"own dictionary without the form", "/Resources << /XObject << >> >>", ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			data := []byte("%PDF-1.7\n" +
				"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
				"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 /Resources << /XObject << /Fm1 5 0 R >> >> >>\nendobj\n" +
				"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R " + tt.resources + " >>\nendobj\n" +
				"4 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
				"5 0 obj\n<< /Type /XObject /Subtype /Form /BBox [0 0 50 50] " +
				"/Resources << /Font << /F1 6 0 R >> >> /Length " + strconv.Itoa(len(form)) +
				" >>\nstream\n" + form + "\nendstream\nendobj\n" +
				"6 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
				"trailer\n<< /Size 7 /Root 1 0 R >>\n%%EOF\n")
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
			if _, merged := pages[0].Resources["XObject"]; !merged {
				t.Fatal("the merged resources lost the root's /XObject")
			}
			ops, err := content.ParsePageContent(ctx, doc, pages[0].Dict)
			if err != nil {
				t.Fatalf("ParsePageContent: %v", err)
			}
			elements, err := ExtractElements(ctx, doc, graphics.NewMachine([4]float64{0, 0, 612, 792}), pages[0], ops)
			if err != nil {
				t.Fatalf("ExtractElements: %v", err)
			}
			var got strings.Builder
			for _, el := range elements {
				got.WriteString(el.Text)
			}
			if got.String() != tt.want {
				t.Errorf("text = %q, want %q", got.String(), tt.want)
			}
		})
	}
}
