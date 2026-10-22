package page

import (
	"context"
	"errors"
	"image"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/render/blend"
)

// TestBoxGeometry sizes each page boundary as a viewer reads it: a box the
// page does not state, or states as no rectangle enclosing an area, is the
// crop box, and a box past the media box is reduced to it.
func TestBoxGeometry(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		page          string
		box           Box
		width, height int
	}{
		{"/MediaBox [0 0 100 50] /CropBox [10 10 60 40]", CropBox, 50, 30},
		{"/MediaBox [0 0 100 50] /CropBox [10 10 60 40]", MediaBox, 100, 50},
		{"/MediaBox [0 0 100 50] /CropBox [10 10 60 40]", TrimBox, 50, 30},
		{"/MediaBox [0 0 100 50] /CropBox [10 10 60 40] /TrimBox [20 20 20 30]", TrimBox, 50, 30},
		{"/MediaBox [0 0 100 50] /CropBox [10 10 60 40] /BleedBox [-10 0 70 50]", BleedBox, 70, 50},
		{"/MediaBox [0 0 100 50] /ArtBox [5 5 25 15]", ArtBox, 20, 10},
		{"/MediaBox [0 0 100 50] /ArtBox [5 5 25 15]", Box(200), 100, 50},
	} {
		_, pg := open(t, fixture{page: tc.page}.bytes(), limits.Limits{})
		g, err := BoxGeometry(pg, tc.box, 72)
		if err != nil {
			t.Fatalf("%s box %d: %v", tc.page, tc.box, err)
		}
		if g.Width != tc.width || g.Height != tc.height {
			t.Errorf("%s box %d: %dx%d, want %dx%d", tc.page, tc.box, g.Width, g.Height, tc.width, tc.height)
		}
	}
}

// TestRenderCMYK composites the page for a CMYK device, reading the page
// group's /CS no more, and a page drawn in sRGB has no CMYK.
func TestRenderCMYK(t *testing.T) {
	t.Parallel()
	f := fixture{
		page:    "/Group << /S /Transparency /CS /DeviceRGB >>",
		content: "0 0 0 1 k 0 0 50 100 re f 0.5 g 50 0 50 100 re f",
	}
	doc, pg := open(t, f.bytes(), limits.Limits{})
	ctx := context.Background()
	opts := f.options()
	opts.CMYK = true
	res, _, err := RenderGroup(ctx, doc, pg, opts)
	if err != nil {
		t.Fatal(err)
	}
	if k := res.Group.Space().Kind(); k != blend.CMYK {
		t.Fatalf("page group blends in %v, want CMYK", k)
	}
	img, err := res.CMYK(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds() != image.Rect(0, 0, 100, 100) {
		t.Errorf("bounds %v", img.Bounds())
	}
	if got := img.CMYKAt(25, 50); got.C != 0 || got.M != 0 || got.Y != 0 || got.K != 255 {
		t.Errorf("DeviceCMYK black %v, want K alone", got)
	}
	if got := img.CMYKAt(75, 50); got.C != 0 || got.M != 0 || got.Y != 0 || got.K != 128 {
		t.Errorf("DeviceGray 0.5 %v, want K 128 alone", got)
	}

	rgb, _, err := RenderGroup(ctx, doc, pg, f.options())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rgb.CMYK(ctx); !errors.Is(err, blend.ErrNotCMYK) {
		t.Errorf("CMYK of an sRGB render: %v, want ErrNotCMYK", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := res.CMYK(cancelled); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v, want context.Canceled", err)
	}
}
