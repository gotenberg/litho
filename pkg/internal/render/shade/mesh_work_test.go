package shade

import (
	"errors"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/render/surface"
)

// TestMeshWorkCap checks the meshOverdraw bound: 500 triangles that each
// cover the whole 100x100 page visit 5 million pixels, past both 256 times
// the page and minMeshWork, and painting refuses rather than running on; one
// such triangle paints.
func TestMeshWorkCap(t *testing.T) {
	t.Parallel()
	tri := packBits(0, 8, 0, 8, 0, 8, 0, 8, 0, 8, 255, 8, 0, 8, 0, 8, 0, 8, 0, 8, 255, 8, 0, 8)
	shading := func(n int) fixture {
		return fixture{
			content: "/Sh sh",
			shading: streamObj("/ShadingType 4 /ColorSpace /DeviceGray /BitsPerCoordinate 8 /BitsPerComponent 8"+
				" /BitsPerFlag 8 /Decode [0 255 0 255 0 1]", strings.Repeat(tri, n)),
		}
	}
	paint := func(f fixture) error {
		ctx, doc := openFixture(t, f.bytes())
		canvas, err := surface.NewCanvas(pageSide, pageSide, pageSide*pageSide)
		if err != nil {
			t.Fatal(err)
		}
		sh, err := New(ctx, doc, ast.PdfIndirectRef{ObjectNum: 5}, deviceComponents)
		if err != nil {
			t.Fatalf("New: %v", err)
		}
		return sh.Paint(ctx, Target{Canvas: canvas, Convert: deviceRGB{}}, pageToDevice)
	}
	if err := paint(shading(1)); err != nil {
		t.Fatalf("one triangle: %v", err)
	}
	if err := paint(shading(500)); !errors.Is(err, ErrTooLarge) {
		t.Errorf("500 overlapping triangles: %v, want ErrTooLarge", err)
	}
}
