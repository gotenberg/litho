package pdf

import (
	"context"
	"errors"
	"fmt"
	"image/color"
	"testing"
)

// contentKindsPDF is a 200 by 50 page painting one 20-unit square of each
// kind of content, left to right: a Type 3 glyph whose description fills a
// path (black), a filled path (red), an image XObject (green) whose right
// half a clipping path cuts off, an inline image (blue), a shading (yellow)
// and an annotation appearance (magenta).
func contentKindsPDF() []byte {
	stream := func(dict, data string) string {
		return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(data), data)
	}
	page := "BT /T3 20 Tf 10 15 Td (a) Tj ET " +
		"1 0 0 rg 40 15 20 20 re f " +
		"q 70 15 10 20 re W n 20 0 0 20 70 15 cm /Im0 Do Q " +
		"q 20 0 0 20 100 15 cm BI /W 1 /H 1 /CS /RGB /BPC 8 ID \x00\x00\xff EI Q " +
		"q 130 15 20 20 re W n /Sh0 sh Q"
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 50] /Contents 4 0 R /Annots [5 0 R]" +
			" /Resources << /Font << /T3 7 0 R >> /XObject << /Im0 9 0 R >> /Shading << /Sh0 10 0 R >> >> >>",
		stream("", page),
		"<< /Type /Annot /Subtype /Square /Rect [160 15 180 35] /F 4 /AP << /N 6 0 R >> >>",
		stream("/Type /XObject /Subtype /Form /BBox [0 0 20 20]", "1 0 1 rg 0 0 20 20 re f"),
		"<< /Type /Font /Subtype /Type3 /FontBBox [0 0 1000 1000] /FontMatrix [0.001 0 0 0.001 0 0]" +
			" /CharProcs << /a 8 0 R >> /Encoding << /Type /Encoding /Differences [97 /a] >>" +
			" /FirstChar 97 /LastChar 97 /Widths [1000] >>",
		stream("", "1000 0 0 0 1000 1000 d1 0 0 1000 1000 re f"),
		stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceRGB /BitsPerComponent 8", "\x00\xff\x00"),
		"<< /ShadingType 2 /ColorSpace /DeviceRGB /Coords [130 0 150 0]" +
			" /Function << /FunctionType 2 /Domain [0 1] /C0 [1 1 0] /C1 [1 1 0] /N 1 >> >>",
	})
}

func TestRenderPageContent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := openLimited(t, contentKindsPDF(), Limits{})
	white := color.NRGBA{255, 255, 255, 255}
	kinds := []struct {
		kind RenderContent
		x    int
		ink  color.NRGBA
	}{
		{RenderText, 20, color.NRGBA{0, 0, 0, 255}},
		{RenderPaths, 50, color.NRGBA{255, 0, 0, 255}},
		{RenderImages, 75, color.NRGBA{0, 255, 0, 255}},
		{RenderImages, 110, color.NRGBA{0, 0, 255, 255}},
		{RenderShadings, 140, color.NRGBA{255, 255, 0, 255}},
		{RenderAnnotations, 170, color.NRGBA{255, 0, 255, 255}},
	}
	for _, sel := range []RenderContent{
		RenderAllContent, RenderText, RenderPaths, RenderImages, RenderShadings, RenderAnnotations,
		RenderImages | RenderText, 0,
	} {
		img, err := r.RenderPage(ctx, 1, WithRenderDPI(72), WithRenderContent(sel))
		if err != nil {
			t.Fatalf("content %#x: %v", uint8(sel), err)
		}
		for _, k := range kinds {
			want := white
			if sel&k.kind != 0 {
				want = k.ink
			}
			if got := nrgbaAt(t, img, k.x, 25); got != want {
				t.Errorf("content %#x: pixel at x %d is %v, want %v", uint8(sel), k.x, got, want)
			}
		}
		// The clipping path keeps cutting the image when paths are left out.
		if got := nrgbaAt(t, img, 85, 25); got != white {
			t.Errorf("content %#x: the clipped half of the image paints %v", uint8(sel), got)
		}
	}
	if _, err := r.RenderPage(ctx, 1, WithRenderContent(RenderAllContent+1)); !errors.Is(err, ErrInvalidRenderOptions) {
		t.Errorf("content %#x: %v, want ErrInvalidRenderOptions", uint8(RenderAllContent+1), err)
	}
}
