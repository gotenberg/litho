package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestFixGraphicsRepairsAppearancesOnAPageWithoutResources pins that the
// graphics fixer repairs an annotation's appearance on a page whose content
// resolves no /Resources.
//
// An appearance stream is a form XObject that draws from its own /Resources
// (ISO 32000-1, 12.5.5 and 8.10.1), so what the page states or inherits has no
// bearing on it. The page loop skipped the page, its appearances with it, when
// no node of the page tree stated a dictionary under /Resources, and the
// appearance kept its /OPI and the /Interpolate of the image it draws.
func TestFixGraphicsRepairsAppearancesOnAPageWithoutResources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		resources string
	}{
		{name: "no /Resources", resources: ""},
		{name: "/Resources naming no dictionary", resources: "/Resources 7 0 R"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			doc, alloc := newTestDoc(t, buildSimplePDF([]string{
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " + tc.resources + " /Annots [4 0 R] >>",
				"<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /F 4 /AP << /N 5 0 R >> >>",
				"<< /Type /XObject /Subtype /Form /BBox [0 0 10 10] /OPI << /Version 1.3 >>" +
					" /Resources << /XObject << /Im1 6 0 R >> >> /Length 8 >>\nstream\n/Im1 Do\nendstream",
				"<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray" +
					" /BitsPerComponent 8 /Interpolate true /Length 1 >>\nstream\nx\nendstream",
				"[1 2 3]",
			}))

			if _, err := fixGraphics(ctx, alloc, doc, Options{}, nil, "PDF/A-2b"); err != nil {
				t.Fatalf("fixGraphics: %v", err)
			}

			form, ok := alloc.Pending()[5].(ast.PdfStream)
			if !ok {
				t.Fatalf("the appearance is %T, want a stream the repair rewrote", alloc.Pending()[5])
			}
			if _, has := form.Dict["OPI"]; has {
				t.Error("the appearance keeps its /OPI")
			}
			image, ok := alloc.Pending()[6].(ast.PdfStream)
			if !ok {
				t.Fatalf("the image is %T, want a stream the repair rewrote", alloc.Pending()[6])
			}
			if image.Dict["Interpolate"] != ast.PdfBool(false) {
				t.Errorf("the image the appearance draws has /Interpolate %v, want false", image.Dict["Interpolate"])
			}
		})
	}
}
