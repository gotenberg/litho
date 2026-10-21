package page

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/limits"
)

// TestRenderResourcesInheritedWhole pins that a page draws with the nearest
// /Resources dictionary whole (ISO 32000-1, 7.7.3.4), as poppler does: the
// page tree root lends a form painting red, a page stating no /Resources
// draws it, and a page stating a dictionary that lacks it draws nothing,
// though the reader's merged resources reach it.
func TestRenderResourcesInheritedWhole(t *testing.T) {
	for _, tt := range []struct {
		name      string
		resources string
		want      rgb
	}{
		{"inherited", "", red},
		{"own dictionary without the form", "/Resources << /ExtGState << >> >>", paper},
	} {
		t.Run(tt.name, func(t *testing.T) {
			data := buildPDF([]string{
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 /Resources << /XObject << /Fm 5 0 R >> >> >>",
				"<< /Type /Page /Parent 2 0 R /Contents 4 0 R /MediaBox [0 0 100 100] " + tt.resources + " >>",
				stream("", "/Fm Do"),
				stream("/Type /XObject /Subtype /Form /BBox [0 0 100 100]", "1 0 0 rg 0 0 100 100 re f"),
			})
			doc, pg := open(t, data, limits.Limits{})
			if _, merged := pg.Resources["XObject"]; !merged {
				t.Fatal("the merged resources lost the root's /XObject")
			}
			c, _, err := Render(context.Background(), doc, pg, Options{DPI: 72, Background: white})
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			expect(t, c.Image(), 2, map[[2]int]rgb{{50, 50}: tt.want})
		})
	}
}
