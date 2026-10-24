package page

import (
	"context"
	"image"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/render/raster"
	"github.com/gotenberg/litho/pkg/internal/render/surface"
)

// clippedPage paints under clips of every kind: a page with a hole cut out
// (even-odd), a curved clip, clips nested on clips, images, a shading, a
// transparency group with a soft mask and text, so the renders compared
// below reach every place a clip, Within or Shared applies.
func clippedPage(group string) fixture {
	return fixture{
		page: "/Group << /S /Transparency " + group + " >> /Resources << /XObject << /Im 5 0 R /Fm 6 0 R >> " +
			"/Shading << /Sh 7 0 R >> /Font << /F 8 0 R >> /ExtGState << /A 9 0 R >> >>",
		content: "q -1 -1 102 102 re 20.5 20.25 30 30 re W* n 0 0 1 rg 0 0 100 100 re f " +
			"q 40 0 0 40 10 50 cm /Im Do Q Q " +
			"q 50 50 m 90 50 l 90 90 50 90 50 50 c h W n /Sh sh " +
			"q 55.3 55.1 m 85 60 l 70 88 l h W n 1 0 0 rg 0 0 100 100 re f Q Q " +
			"q 0 0 m 100 0 l 100 100 l 0 100 l h 30 30 m 30 70 l 70 70 l 70 30 l h W n /A gs /Fm Do Q " +
			"q 33.7 5.2 m 60 5.2 l 60 30 l 33.7 30 l h 40 10 m 45 25 l 50 10 l h W* n " +
			"0 g BT /F 18 Tf 5 8 Td (Hidden) Tj ET Q " +
			"BT /F 12 Tf 1 0 0 1 3.3 92.1 Tm 0.2 g (litho) Tj ET q 25 0 0 12 70.3 3.1 cm /Im Do Q",
		objs: []string{
			stream("/Type /XObject /Subtype /Image /Width 2 /Height 2 /ColorSpace /DeviceRGB /BitsPerComponent 8 /Interpolate true",
				"\xff\x00\x00\x00\xff\x00\x00\x00\xff\xff\xff\x00"),
			stream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Group << /S /Transparency /K true >>",
				"0 1 0 rg 10 10 80 80 re f 1 0 1 rg 40 40 40 40 re f"),
			"<< /ShadingType 2 /ColorSpace /DeviceRGB /Coords [50 50 90 90] " +
				"/Function << /FunctionType 2 /Domain [0 1] /C0 [1 1 0] /C1 [0 1 1] /N 1 >> >>",
			"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
			"<< /ca 0.6 /BM /Multiply >>",
		},
	}
}

// renderBusy renders the clipped page with opts at 72 dpi over white and
// returns its sRGB canvas.
func renderClipped(t *testing.T, group string, opts Options) *surface.Canvas {
	t.Helper()
	doc, pg := open(t, clippedPage(group).bytes(), limits.Limits{})
	opts.DPI, opts.Background = 72, white
	res, _, err := RenderGroup(context.Background(), doc, pg, opts)
	if err != nil {
		t.Fatalf("RenderGroup: %v", err)
	}
	c, err := res.Canvas(context.Background())
	if err != nil {
		t.Fatalf("Canvas: %v", err)
	}
	return c
}

// sameBits reports the first pixel at which a and b differ, bit for bit,
// among those keep marks.
func sameBits(t *testing.T, a, b *surface.Canvas, keep func(x, y int) bool) {
	t.Helper()
	for y := range a.Height {
		for x := range a.Width {
			if !keep(x, y) {
				continue
			}
			for k := range 4 {
				i := 4*(y*a.Width+x) + k
				if math.Float32bits(a.Pix[i]) != math.Float32bits(b.Pix[i]) {
					t.Fatalf("pixel (%d, %d) channel %d: %v, want %v", x, y, k, b.Pix[i], a.Pix[i])
				}
			}
		}
	}
}

// TestWithinPaintsMarkedPixelsAlike renders the clipped page whole and within
// a scattered set of pixels: the marked pixels hold the same values, bit for
// bit, in an sRGB and in a CMYK page group.
func TestWithinPaintsMarkedPixelsAlike(t *testing.T) {
	t.Parallel()
	for _, group := range []string{"", "/CS /DeviceCMYK"} {
		full := renderClipped(t, group, Options{GroupSpace: true})
		within, err := surface.NewMask(full.Width, full.Height, int64(full.Width*full.Height))
		if err != nil {
			t.Fatal(err)
		}
		marked := func(x, y int) bool { return (x/7+y/5)%3 == 0 || (x > 60 && y < 20) }
		for y := range full.Height {
			for x := range full.Width {
				if marked(x, y) {
					within.A[y*full.Width+x] = 1
				}
			}
		}
		got := renderClipped(t, group, Options{GroupSpace: true, Within: within})
		sameBits(t, full, got, marked)
	}
}

// TestSharedRendersAlike renders the clipped page's text alone and the page
// whole sharing a [Shared], then without: each pixel holds the same values.
func TestSharedRendersAlike(t *testing.T) {
	t.Parallel()
	textOnly := Paths | Images | Shadings | Annotations
	shared := NewShared(1 << 20)
	sharedText := renderClipped(t, "", Options{Omit: textOnly, Shared: shared})
	sharedWhole := renderClipped(t, "", Options{Shared: shared})
	all := func(int, int) bool { return true }
	sameBits(t, renderClipped(t, "", Options{Omit: textOnly}), sharedText, all)
	sameBits(t, renderClipped(t, "", Options{}), sharedWhole, all)
	if len(shared.glyphs) == 0 || len(shared.forms.ops) == 0 {
		t.Errorf("the renders shared %d glyph coverages and %d forms, want some of each", len(shared.glyphs), len(shared.forms.ops))
	}
}

// TestOmitGlyphLeavesOutTheGlyphsItNames renders the clipped page leaving out
// every glyph by [Options.OmitGlyph], and none: the first matches the page
// drawn without its text, the second the page drawn whole, bit for bit, and
// the origins OmitGlyph is asked about are those [Options.Glyph] reports.
func TestOmitGlyphLeavesOutTheGlyphsItNames(t *testing.T) {
	t.Parallel()
	all := func(int, int) bool { return true }
	var asked, shown []geom.Point
	every := renderClipped(t, "", Options{OmitGlyph: func(p geom.Point) bool { asked = append(asked, p); return true }})
	sameBits(t, renderClipped(t, "", Options{Omit: Text}), every, all)
	none := renderClipped(t, "", Options{OmitGlyph: func(geom.Point) bool { return false }})
	sameBits(t, renderClipped(t, "", Options{Glyph: func(p geom.Point, _ geom.Rect) { shown = append(shown, p) }}), none, all)
	if len(asked) == 0 || len(asked) != len(shown) {
		t.Fatalf("OmitGlyph asked about %d glyphs, Glyph reported %d", len(asked), len(shown))
	}
	for i := range asked {
		if asked[i] != shown[i] {
			t.Errorf("glyph %d: OmitGlyph asked about %v, Glyph reported %v", i, asked[i], shown[i])
		}
	}
}

// TestClipNodeMatchesItsMask reads a chain of clipping paths over parts of
// the page before rasterising it whole: the values agree bit for bit with
// the mask the chain makes.
func TestClipNodeMatchesItsMask(t *testing.T) {
	t.Parallel()
	const w, h = 97, 61
	const budget = w * h
	path := func(pts ...geom.Point) geom.Path {
		var p geom.Path
		p.MoveTo(pts[0])
		for _, q := range pts[1:] {
			p.LineTo(q)
		}
		p.Close()
		return p
	}
	star := path(geom.Point{X: 48, Y: 2}, geom.Point{X: 70, Y: 58}, geom.Point{X: 10, Y: 20}, geom.Point{X: 88, Y: 20}, geom.Point{X: 26, Y: 58})
	hole := path(geom.Point{X: -1, Y: -1}, geom.Point{X: 98, Y: -1}, geom.Point{X: 98, Y: 62}, geom.Point{X: -1, Y: 62})
	inner := path(geom.Point{X: 30.5, Y: 10.25}, geom.Point{X: 60.75, Y: 10.25}, geom.Point{X: 60.75, Y: 40.5}, geom.Point{X: 30.5, Y: 40.5})
	hole.Ops, hole.Pts = append(hole.Ops, inner.Ops...), append(hole.Pts, inner.Pts...)
	var curve geom.Path
	curve.MoveTo(geom.Point{X: 5, Y: 5})
	curve.CubicTo(geom.Point{X: 90, Y: 0}, geom.Point{X: 95, Y: 60}, geom.Point{X: 50, Y: 58})
	curve.Close()

	c := newClip(w, h).intersectRect(geom.Rect{Min: geom.Point{X: 3.5, Y: 2.25}, Max: geom.Point{X: 90.2, Y: 58.7}})
	steps := []struct {
		p    geom.Path
		rule raster.FillRule
	}{{star, raster.NonZero}, {hole, raster.EvenOdd}, {curve, raster.NonZero}}
	for i, s := range steps {
		var err error
		if c, err = c.withPath(s.p, s.rule, true, w, h, budget); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			c = c.intersectRect(geom.Rect{Min: geom.Point{X: 1, Y: 4.5}, Max: geom.Point{X: 80.3, Y: 60}})
		}
	}
	boxes := []image.Rectangle{image.Rect(0, 0, 10, 10), image.Rect(20, 5, 45, 30), image.Rect(44, 0, 46, h), image.Rect(60, 30, 97, 61)}
	parts := make([][]float32, len(boxes))
	for i, b := range boxes {
		v, err := c.in(b, w, h, budget)
		if err != nil {
			t.Fatal(err)
		}
		parts[i] = v
	}
	if c.node.values != nil {
		t.Fatal("reading parts made the mask; the test reads too much")
	}
	full, err := c.mask1(w, h, budget)
	if err != nil {
		t.Fatal(err)
	}
	for i, b := range boxes {
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				got := parts[i][(y-b.Min.Y)*b.Dx()+x-b.Min.X]
				if want := full.A[y*w+x]; math.Float32bits(got) != math.Float32bits(want) {
					t.Fatalf("box %v pixel (%d, %d): %v, the mask holds %v", b, x, y, got, want)
				}
			}
		}
	}
}
