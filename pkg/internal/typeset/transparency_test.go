package typeset_test

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

// solid returns a rectangle filled with c.
func solid(x, y, w, h float64, c typeset.Color) *typeset.Path {
	return &typeset.Path{Ops: rect(x, y, w, h), Fill: &typeset.Fill{Color: c}}
}

// rgb is an opaque colour.
func rgb(r, g, b float64) typeset.Color {
	return typeset.Color{R: r, G: g, B: b, A: 1}
}

// want8 converts a colour of channels in [0, 1] to 8 bits.
func want8(r, g, b float64) color.NRGBA {
	q := func(v float64) uint8 { return uint8(math.Round(min(max(v, 0), 1) * 255)) }
	return color.NRGBA{R: q(r), G: q(g), B: q(b), A: 255}
}

// pixelCheck is one pixel a test expects.
type pixelCheck struct {
	x, y int
	want color.NRGBA
	what string
}

// checkPixels fails t for each pixel of img off its expected colour by more
// than tol levels.
func checkPixels(t *testing.T, img image.Image, tol int, checks []pixelCheck) {
	t.Helper()
	for _, c := range checks {
		if !near(img, c.x, c.y, c.want, tol) {
			t.Errorf("%s: pixel (%d, %d) = %v, want %v", c.what, c.x, c.y, color.NRGBAModel.Convert(img.At(c.x, c.y)), c.want)
		}
	}
}

// writeRender writes doc and draws its first page at 72 dpi, failing t on
// a document that does not validate.
func writeRender(t *testing.T, doc *typeset.Document) ([]byte, image.Image) {
	t.Helper()
	if err := doc.Validate(t.Context()); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	out := write(t, doc)
	return out, render(t, open(t, out), 1)
}

// opacityDoc is the document of [TestGroupOpacityAsAWhole].
func opacityDoc(t *testing.T) *typeset.Document {
	t.Helper()
	squares := func() []typeset.Fragment {
		return []typeset.Fragment{solid(10, 10, 40, 40, rgb(1, 0, 0)), solid(30, 10, 40, 40, rgb(1, 0, 0))}
	}
	page := &typeset.Page{Width: 100, Height: 140, Content: []typeset.Fragment{
		&typeset.Group{Opacity: 0.5, Content: squares()},
		&typeset.Group{Transform: typeset.Matrix{A: 1, D: 1, F: 70}, Opacity: 0.5, Transparency: &typeset.TransparencyGroup{}, Content: squares()},
	}}
	return &typeset.Document{Pages: []*typeset.Page{page}}
}

// TestGroupOpacityAsAWhole locks what opacity means with and without a
// transparency group: two overlapping opaque red squares at opacity 0.5
// over white show the overlap darker as a plain group, each square
// compositing on its own (1 - 0.5 * 0.5 = 0.75 red), and as one even
// translucent shape as a transparency group (0.5 red throughout).
func TestGroupOpacityAsAWhole(t *testing.T) {
	t.Parallel()
	out, img := writeRender(t, opacityDoc(t))
	half, three := want8(1, 0.5, 0.5), want8(1, 0.25, 0.25)
	checkPixels(t, img, 2, []pixelCheck{
		{20, 30, half, "plain group, one square"},
		{40, 30, three, "plain group, overlap"},
		{20, 100, half, "transparency group, one square"},
		{40, 100, half, "transparency group, overlap"},
		{60, 100, half, "transparency group, other square"},
	})
	for _, want := range []string{"/S /Transparency", "/Subtype /Form", "/Fm1 Do"} {
		if !bytes.Contains(out, []byte(want)) && !bytes.Contains(inflated(t, out), []byte(want)) {
			t.Errorf("output lacks %s", want)
		}
	}
}

// blendRef computes the blend of the backdrop cb and the source cs by mode,
// as ISO 32000-1, 11.3.5 defines it.
func blendRef(mode typeset.BlendMode, cb, cs [3]float64) [3]float64 {
	sep := func(f func(b, s float64) float64) [3]float64 {
		return [3]float64{f(cb[0], cs[0]), f(cb[1], cs[1]), f(cb[2], cs[2])}
	}
	multiply := func(b, s float64) float64 { return b * s }
	screen := func(b, s float64) float64 { return b + s - b*s }
	hard := func(b, s float64) float64 {
		if s <= 0.5 {
			return multiply(b, 2*s)
		}
		return screen(b, 2*s-1)
	}
	lum := func(c [3]float64) float64 { return 0.3*c[0] + 0.59*c[1] + 0.11*c[2] }
	clip := func(c [3]float64) [3]float64 {
		l := lum(c)
		n, x := min(c[0], c[1], c[2]), max(c[0], c[1], c[2])
		for i := range c {
			if n < 0 {
				c[i] = l + (c[i]-l)*l/(l-n)
			}
			if x > 1 {
				c[i] = l + (c[i]-l)*(1-l)/(x-l)
			}
		}
		return c
	}
	setLum := func(c [3]float64, l float64) [3]float64 {
		d := l - lum(c)
		return clip([3]float64{c[0] + d, c[1] + d, c[2] + d})
	}
	sat := func(c [3]float64) float64 { return max(c[0], c[1], c[2]) - min(c[0], c[1], c[2]) }
	setSat := func(c [3]float64, s float64) [3]float64 {
		n, x := min(c[0], c[1], c[2]), max(c[0], c[1], c[2])
		var out [3]float64
		for i, v := range c {
			switch {
			case x == n:
			case v == x:
				out[i] = s
			case v == n:
			default:
				out[i] = (v - n) * s / (x - n)
			}
		}
		return out
	}
	switch mode {
	case typeset.BlendNormal:
		return cs
	case typeset.BlendMultiply:
		return sep(multiply)
	case typeset.BlendScreen:
		return sep(screen)
	case typeset.BlendOverlay:
		return sep(func(b, s float64) float64 { return hard(s, b) })
	case typeset.BlendDarken:
		return sep(func(b, s float64) float64 { return min(b, s) })
	case typeset.BlendLighten:
		return sep(func(b, s float64) float64 { return max(b, s) })
	case typeset.BlendColorDodge:
		return sep(func(b, s float64) float64 {
			if s < 1 {
				return min(1, b/(1-s))
			}
			return 1
		})
	case typeset.BlendColorBurn:
		return sep(func(b, s float64) float64 {
			if s > 0 {
				return 1 - min(1, (1-b)/s)
			}
			return 0
		})
	case typeset.BlendHardLight:
		return sep(hard)
	case typeset.BlendSoftLight:
		return sep(func(b, s float64) float64 {
			if s <= 0.5 {
				return b - (1-2*s)*b*(1-b)
			}
			d := math.Sqrt(b)
			if b <= 0.25 {
				d = ((16*b-12)*b + 4) * b
			}
			return b + (2*s-1)*(d-b)
		})
	case typeset.BlendDifference:
		return sep(func(b, s float64) float64 { return math.Abs(b - s) })
	case typeset.BlendExclusion:
		return sep(func(b, s float64) float64 { return b + s - 2*b*s })
	case typeset.BlendHue:
		return setLum(setSat(cs, sat(cb)), lum(cb))
	case typeset.BlendSaturation:
		return setLum(setSat(cb, sat(cs)), lum(cb))
	case typeset.BlendColor:
		return setLum(cs, lum(cb))
	case typeset.BlendLuminosity:
		return setLum(cb, lum(cs))
	}
	return cs
}

// blendModes lists the sixteen blend modes.
var blendModes = []typeset.BlendMode{
	typeset.BlendNormal, typeset.BlendMultiply, typeset.BlendScreen, typeset.BlendOverlay,
	typeset.BlendDarken, typeset.BlendLighten, typeset.BlendColorDodge, typeset.BlendColorBurn,
	typeset.BlendHardLight, typeset.BlendSoftLight, typeset.BlendDifference, typeset.BlendExclusion,
	typeset.BlendHue, typeset.BlendSaturation, typeset.BlendColor, typeset.BlendLuminosity,
}

// blendPage paints, for each blend mode, an opaque backdrop square of cb
// with an opaque square of cs over it, the mode on a plain group in the
// first row and on a transparency group in the second.
func blendPage(cb, cs [3]float64) *typeset.Page {
	page := &typeset.Page{Width: 16 * 20, Height: 40}
	for i, mode := range blendModes {
		x := float64(i * 20)
		page.Content = append(page.Content,
			solid(x, 0, 20, 40, rgb(cb[0], cb[1], cb[2])),
			&typeset.Group{Blend: mode, Content: []typeset.Fragment{solid(x+5, 5, 10, 10, rgb(cs[0], cs[1], cs[2]))}},
			&typeset.Group{Blend: mode, Transparency: &typeset.TransparencyGroup{}, Content: []typeset.Fragment{solid(x+5, 25, 10, 10, rgb(cs[0], cs[1], cs[2]))}},
		)
	}
	return page
}

// TestBlendModes locks the sixteen blend modes against ISO 32000-1,
// 11.3.5 computed by hand for two opaque flat colours, whose composite is
// the blend itself, on a fragment and on a group alike.
func TestBlendModes(t *testing.T) {
	t.Parallel()
	for _, pair := range [][2][3]float64{
		{{0.8, 0.4, 0.2}, {0.3, 0.6, 0.9}},
		{{0.2, 0.5, 0.9}, {0.9, 0.2, 0.4}},
	} {
		cb, cs := pair[0], pair[1]
		out, img := writeRender(t, &typeset.Document{Pages: []*typeset.Page{blendPage(cb, cs)}})
		var checks []pixelCheck
		for i, mode := range blendModes {
			b := blendRef(mode, cb, cs)
			want := want8(b[0], b[1], b[2])
			checks = append(checks,
				pixelCheck{i*20 + 10, 10, want, fmt.Sprintf("fragment, mode %d", mode)},
				pixelCheck{i*20 + 10, 30, want, fmt.Sprintf("group, mode %d", mode)},
			)
		}
		checkPixels(t, img, 2, checks)
		for _, name := range []string{"Multiply", "ColorDodge", "Luminosity", "SoftLight"} {
			if !bytes.Contains(out, []byte("/BM /"+name)) {
				t.Errorf("output lacks /BM /%s", name)
			}
		}
	}
}

// fragBlendDoc is the document of [TestBlendOnFragmentsAndOnGroups].
func fragBlendDoc(t *testing.T) *typeset.Document {
	t.Helper()
	pair := func() []typeset.Fragment {
		return []typeset.Fragment{solid(10, 10, 40, 40, rgb(0.5, 0.5, 0.5)), solid(30, 10, 40, 40, rgb(1, 1, 1))}
	}
	page := &typeset.Page{Width: 100, Height: 140, Content: []typeset.Fragment{
		solid(0, 0, 100, 140, rgb(1, 1, 0)),
		&typeset.Group{Blend: typeset.BlendMultiply, Content: pair()},
		&typeset.Group{Transform: typeset.Matrix{A: 1, D: 1, F: 70}, Blend: typeset.BlendMultiply, Transparency: &typeset.TransparencyGroup{Isolated: true}, Content: pair()},
	}}
	return &typeset.Document{Pages: []*typeset.Page{page}}
}

// TestBlendOnFragmentsAndOnGroups locks the difference between a blend
// mode on each fragment and on the group as a whole: over yellow, a grey
// square then a white one, both multiplied one by one, leave the grey's
// product where they overlap; painted normally into a group the group
// multiplies, white covers grey and the overlap keeps the yellow.
func TestBlendOnFragmentsAndOnGroups(t *testing.T) {
	t.Parallel()
	_, img := writeRender(t, fragBlendDoc(t))
	checkPixels(t, img, 2, []pixelCheck{
		{20, 30, want8(0.5, 0.5, 0), "fragments, grey alone"},
		{40, 30, want8(0.5, 0.5, 0), "fragments, overlap"},
		{60, 30, want8(1, 1, 0), "fragments, white alone"},
		{20, 100, want8(0.5, 0.5, 0), "group, grey alone"},
		{40, 100, want8(1, 1, 0), "group, overlap"},
	})
}

// isoKnockoutDoc is the document of [TestIsolatedAndKnockoutGroups].
func isoKnockoutDoc(t *testing.T) *typeset.Document {
	t.Helper()
	multiplied := func() []typeset.Fragment {
		return []typeset.Fragment{&typeset.Group{Blend: typeset.BlendMultiply, Content: []typeset.Fragment{solid(10, 10, 30, 30, rgb(0.5, 0.5, 0.5))}}}
	}
	halves := func() []typeset.Fragment {
		return []typeset.Fragment{
			solid(110, 10, 40, 30, typeset.Color{R: 1, A: 0.5}),
			solid(130, 10, 40, 30, typeset.Color{B: 1, A: 0.5}),
		}
	}
	page := &typeset.Page{Width: 200, Height: 100, Content: []typeset.Fragment{
		solid(0, 0, 100, 100, rgb(1, 1, 0)),
		&typeset.Group{Transparency: &typeset.TransparencyGroup{}, Content: multiplied()},
		&typeset.Group{Transform: typeset.Matrix{A: 1, D: 1, E: 50}, Transparency: &typeset.TransparencyGroup{Isolated: true}, Content: multiplied()},
		&typeset.Group{Transparency: &typeset.TransparencyGroup{}, Content: halves()},
		&typeset.Group{Transform: typeset.Matrix{A: 1, D: 1, F: 50}, Transparency: &typeset.TransparencyGroup{Knockout: true}, Content: halves()},
	}}
	return &typeset.Document{Pages: []*typeset.Page{page}}
}

// TestIsolatedAndKnockoutGroups locks the two group attributes (ISO
// 32000-1, 11.4.5 and 11.4.6). A grey square multiplied over yellow inside
// a non-isolated group blends with the yellow, and inside an isolated one
// with nothing, so it shows grey. Two squares at alpha 0.5, red then blue,
// show the blue over the red where they overlap, and in a knockout group
// the blue over the white backdrop alone.
func TestIsolatedAndKnockoutGroups(t *testing.T) {
	t.Parallel()
	out, img := writeRender(t, isoKnockoutDoc(t))
	checkPixels(t, img, 2, []pixelCheck{
		{25, 25, want8(0.5, 0.5, 0), "non-isolated multiply"},
		{75, 25, want8(0.5, 0.5, 0.5), "isolated multiply"},
		{120, 25, want8(1, 0.5, 0.5), "red alone"},
		{140, 25, want8(0.5, 0.25, 0.75), "blue over red"},
		{140, 75, want8(0.5, 0.5, 1), "knockout: blue over the backdrop"},
		{120, 75, want8(1, 0.5, 0.5), "knockout: red alone"},
	})
	for _, want := range []string{"/I true", "/K true"} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("output lacks %s", want)
		}
	}
}

// spaceDoc is the document of [TestGroupSpace].
func spaceDoc(t *testing.T) *typeset.Document {
	t.Helper()
	half := typeset.Color{R: 1, A: 0.5}
	page := &typeset.Page{Width: 100, Height: 50, Content: []typeset.Fragment{
		&typeset.Group{Opacity: 0.5, Transparency: &typeset.TransparencyGroup{Isolated: true, Space: typeset.SpaceGray}, Content: []typeset.Fragment{solid(10, 10, 30, 30, half)}},
		&typeset.Group{Opacity: 0.5, Transparency: &typeset.TransparencyGroup{Isolated: true, Space: typeset.SpaceRGB}, Content: []typeset.Fragment{solid(60, 10, 30, 30, half)}},
	}}
	return &typeset.Document{Pages: []*typeset.Page{page}}
}

// TestGroupSpace locks the colour space a group composites in (ISO
// 32000-1, 11.6.6, Table 147): red at alpha 0.5 enters a DeviceGray group
// as its grey, 0.3 (10.3.2), and the group at opacity 0.5 shows over white
// as the grey 0.825; in a DeviceRGB group it stays red, (1, 0.75, 0.75).
// Both the content and the group are translucent: litho's renderer
// composites in the parent's space a group whose content paints opaque,
// a leniency it documents, and paints an opaque Normal group of Normal
// content as its content painted into the parent.
func TestGroupSpace(t *testing.T) {
	t.Parallel()
	out, img := writeRender(t, spaceDoc(t))
	checkPixels(t, img, 3, []pixelCheck{
		{25, 25, want8(0.825, 0.825, 0.825), "red in a grey group"},
		{75, 25, want8(1, 0.75, 0.75), "red in an RGB group"},
	})
	for _, want := range []string{"/CS /DeviceGray", "/CS /DeviceRGB"} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("output lacks %s", want)
		}
	}
}

// masksDoc is the document of [TestSoftMasks].
func masksDoc(t *testing.T) *typeset.Document {
	t.Helper()
	ramp := &typeset.Shading{
		Kind: typeset.Axial, From: typeset.Point{X: 0}, To: typeset.Point{X: 100},
		Stops: []typeset.ColorStop{{Color: rgb(0, 0, 0)}, {Offset: 1, Color: rgb(1, 1, 1)}},
	}
	page := &typeset.Page{Width: 200, Height: 100, Content: []typeset.Fragment{
		&typeset.Group{
			Transparency: &typeset.TransparencyGroup{Mask: &typeset.SoftMask{Content: []typeset.Fragment{
				&typeset.Path{Ops: rect(0, 0, 100, 40), Fill: &typeset.Fill{Shading: ramp}},
			}}},
			Content: []typeset.Fragment{solid(0, 0, 100, 60, rgb(1, 0, 0))},
		},
		&typeset.Group{
			Transparency: &typeset.TransparencyGroup{Mask: &typeset.SoftMask{Kind: typeset.MaskAlpha, Content: []typeset.Fragment{
				solid(110, 10, 40, 40, typeset.Color{G: 1, A: 0.25}),
			}}},
			Content: []typeset.Fragment{solid(110, 10, 80, 80, rgb(1, 0, 0))},
		},
	}}
	return &typeset.Document{Pages: []*typeset.Page{page}}
}

// TestSoftMasks locks the two kinds of soft mask (ISO 32000-1, 11.6.5.2)
// against values computed by hand. A luminosity mask of an axial shading
// from black to white across 100 points keeps a red square in proportion
// to x, so over white the red runs from white to red; outside the mask's
// content it masks everything out. An alpha mask of a square at alpha
// 0.25 keeps a quarter of the red, whatever the square's colour.
func TestSoftMasks(t *testing.T) {
	t.Parallel()
	out, img := writeRender(t, masksDoc(t))
	var checks []pixelCheck
	for _, x := range []int{10, 30, 50, 70, 90} {
		k := (float64(x) + 0.5) / 100
		checks = append(checks, pixelCheck{x, 20, want8(1, 1-k, 1-k), "luminosity ramp"})
	}
	checks = append(checks,
		pixelCheck{50, 50, want8(1, 1, 1), "outside the luminosity mask's content"},
		pixelCheck{130, 30, want8(1, 0.75, 0.75), "alpha mask"},
		pixelCheck{170, 70, want8(1, 1, 1), "outside the alpha mask's content"},
	)
	checkPixels(t, img, 3, checks)
	for _, want := range []string{"/S /Luminosity", "/S /Alpha", "/SMask"} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("output lacks %s", want)
		}
	}
}

// TestTransparencyGroupBoxHoldsItsContent locks that a transparency group
// of one fragment draws what a plain group draws: its form's bounding box,
// derived from the content, cuts none of it. The content reaches the
// limits of each kind of fragment: a skewed, scaled and stroked text run,
// a stroked path with a sharp miter, a picture, and a rotated group with a
// clip, all under a scale and a translation.
func TestTransparencyGroupBoxHoldsItsContent(t *testing.T) {
	t.Parallel()
	pic := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for i := range pic.Pix {
		pic.Pix[i] = uint8(40 * (i % 5)) // test values stay under 256
	}
	src := encode(t, pic)
	content := func() []typeset.Fragment {
		txt := run(t, sans, 30, typeset.Point{X: 5, Y: 40}, "Wide text")
		txt.Skew, txt.Stroke, txt.HScale = 0.4, 2, 1.5
		return []typeset.Fragment{
			txt,
			&typeset.Path{
				Ops: []typeset.PathOp{
					{Kind: typeset.MoveTo, P: [3]typeset.Point{{X: 10, Y: 90}}},
					{Kind: typeset.LineTo, P: [3]typeset.Point{{X: 60, Y: 95}}},
					{Kind: typeset.LineTo, P: [3]typeset.Point{{X: 10, Y: 100}}},
				},
				Stroke: &typeset.Stroke{Color: rgb(0, 0, 1), Width: 6},
			},
			&typeset.Image{Rect: typeset.Rect{X: 100, Y: 60, Width: 40, Height: 40}, Source: src},
			&typeset.Group{
				Transform: typeset.Matrix{A: 0.8, B: 0.6, C: -0.6, D: 0.8, E: 180, F: 60},
				Clip:      &typeset.Path{Ops: rect(0, 0, 30, 30)},
				Content:   []typeset.Fragment{solid(-10, -10, 60, 60, rgb(0, 0.6, 0))},
			},
		}
	}
	// Each fragment sits in a group of its own, so each form's box is
	// measured from one kind of fragment alone.
	m := typeset.Matrix{A: 1.5, D: 1.5, E: 10, F: 20}
	doc := func(transparent bool) *typeset.Document {
		var groups []typeset.Fragment
		for _, f := range content() {
			g := &typeset.Group{Opacity: 0.999, Content: []typeset.Fragment{f}}
			if transparent {
				g.Transparency = &typeset.TransparencyGroup{}
			}
			groups = append(groups, g)
		}
		return &typeset.Document{Pages: []*typeset.Page{{Width: 400, Height: 220, Content: []typeset.Fragment{
			&typeset.Group{Transform: m, Content: groups},
		}}}}
	}
	_, plain := writeRender(t, doc(false))
	out, grouped := writeRender(t, doc(true))
	if !bytes.Contains(inflated(t, out), []byte("/Fm4 Do")) {
		t.Fatal("output paints no form")
	}
	b := plain.Bounds()
	ink, differ := 0, 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			p := color.NRGBAModel.Convert(plain.At(x, y)).(color.NRGBA)
			if p != (color.NRGBA{R: 255, G: 255, B: 255, A: 255}) {
				ink++
			}
			if !near(grouped, x, y, p, 1) {
				differ++
			}
		}
	}
	if ink == 0 {
		t.Fatal("the plain group draws nothing")
	}
	if differ != 0 {
		t.Errorf("%d of %d pixels differ between the plain and the transparency group", differ, b.Dx()*b.Dy())
	}
}

// TestTransparencyGroupOffPage locks that a transparency group that shows
// nothing writes no form: one off the page, one whose clip lies apart
// from its content, and one whose mask's content lies apart from it.
func TestTransparencyGroupOffPage(t *testing.T) {
	t.Parallel()
	red := solid(10, 10, 20, 20, rgb(1, 0, 0))
	page := &typeset.Page{Width: 100, Height: 100, Content: []typeset.Fragment{
		&typeset.Group{Transform: typeset.Matrix{A: 1, D: 1, E: 500}, Transparency: &typeset.TransparencyGroup{}, Content: []typeset.Fragment{red}},
		&typeset.Group{Clip: &typeset.Path{Ops: rect(50, 50, 10, 10)}, Transparency: &typeset.TransparencyGroup{}, Content: []typeset.Fragment{red}},
		&typeset.Group{Transparency: &typeset.TransparencyGroup{Mask: &typeset.SoftMask{Content: []typeset.Fragment{solid(60, 60, 10, 10, rgb(1, 1, 1))}}}, Content: []typeset.Fragment{red}},
		&typeset.Group{Transparency: &typeset.TransparencyGroup{}},
	}}
	out, _ := writeRender(t, &typeset.Document{Pages: []*typeset.Page{page}})
	if bytes.Contains(out, []byte("/Subtype /Form")) {
		t.Error("a group that shows nothing writes a form")
	}
}

// transparencyTagged is a tagged document whose content sits in
// transparency groups: a heading on the page, a paragraph that starts on
// the page and goes on in a group, a second paragraph and a decorative
// rule in the group, a figure in a group nested in it, and a closing
// paragraph on the page.
func transparencyTagged(t *testing.T) *typeset.Document {
	t.Helper()
	h := &typeset.StructElem{Role: "H1"}
	p1 := &typeset.StructElem{Role: "P"}
	p2 := &typeset.StructElem{Role: "P"}
	fig := &typeset.StructElem{Role: "Figure", Alt: "A green square"}
	p3 := &typeset.StructElem{Role: "P"}
	root := &typeset.StructElem{Role: "Document", Lang: "en-US", Kids: []*typeset.StructElem{h, p1, p2, fig, p3}}
	tagged := func(f *typeset.Text, e *typeset.StructElem) *typeset.Text {
		f.Parent = e
		return f
	}
	square := solid(40, 300, 50, 50, rgb(0, 0.6, 0))
	square.Parent = fig
	return &typeset.Document{
		Metadata:  typeset.Metadata{Title: "Transparency", Lang: "en-US"},
		Structure: root,
		Pages: []*typeset.Page{letter(
			tagged(run(t, sans, 18, typeset.Point{X: 40, Y: 60}, "Heading"), h),
			tagged(run(t, sans, 12, typeset.Point{X: 40, Y: 100}, "First words"), p1),
			&typeset.Group{Opacity: 0.6, Transparency: &typeset.TransparencyGroup{Isolated: true}, Content: []typeset.Fragment{
				tagged(run(t, sans, 12, typeset.Point{X: 40, Y: 120}, "and more words"), p1),
				tagged(run(t, sans, 12, typeset.Point{X: 40, Y: 160}, "Second paragraph"), p2),
				solid(40, 170, 200, 1, typeset.Black),
				&typeset.Group{Blend: typeset.BlendMultiply, Transparency: &typeset.TransparencyGroup{
					Mask: &typeset.SoftMask{Content: []typeset.Fragment{solid(40, 300, 50, 50, rgb(1, 1, 1))}},
				}, Content: []typeset.Fragment{square}},
			}},
			tagged(run(t, sans, 12, typeset.Point{X: 40, Y: 400}, "Closing words"), p3),
		)},
	}
}

// TestTransparencyGroupTagged locks the tagging of a transparency group's
// content: marked in the form's own stream, which the parent tree reaches
// through the form's /StructParents and the elements through marked-content
// references naming the stream (ISO 32000-1, 14.7.4.3 and 14.7.4.4), with
// the Do outside every sequence. Text extraction reads every run inside
// marked content, and litho's PDF/UA-1 validator finds nothing once the
// remediation adds what the backend leaves to it.
func TestTransparencyGroupTagged(t *testing.T) {
	t.Parallel()
	out := write(t, transparencyTagged(t))
	for _, want := range []string{"/Stm", "/StructParents 1", "/StructParents 2", "/ParentTreeNextKey 3"} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("output lacks %s", want)
		}
	}
	r := open(t, out)
	ctx := context.Background()
	elems, err := r.ExtractTextElements(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractTextElements: %v", err)
	}
	seen := map[string]bool{}
	for _, e := range elems {
		seen[e.Text] = true
		if e.MCID < 0 {
			t.Errorf("%q outside marked content", e.Text)
		}
	}
	text, err := r.ExtractText(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	for _, want := range []string{"Heading", "First words", "and more words", "Second paragraph", "Closing words"} {
		if !strings.Contains(text, want) {
			t.Errorf("ExtractText lacks %q: %q", want, text)
		}
	}
	report, fixed := remediate(t, out, func(ctx context.Context, w *pdf.Writer) (*models.RemediationReport, error) {
		return w.RemediatePDFUA(ctx, models.PDFUA1)
	})
	for _, v := range report.Residual {
		t.Errorf("residual: %v", v)
	}
	violations, err := open(t, fixed).ValidatePDFUA(ctx, models.PDFUA1)
	if err != nil {
		t.Fatalf("ValidatePDFUA: %v", err)
	}
	for _, v := range violations {
		t.Errorf("remediated output: %v", v)
	}
}

// TestTransparencyRemediates locks that a file holding every construct
// passes litho's validators once remediated to the PDF/A levels: PDF/A-1b,
// which forbids transparency (ISO 19005-1, 6.4), flattens it, and the
// later levels keep it.
func TestTransparencyRemediates(t *testing.T) {
	t.Parallel()
	doc := transparencyTagged(t)
	doc.Pages[0].Content = append(doc.Pages[0].Content, blendPage([3]float64{0.8, 0.4, 0.2}, [3]float64{0.3, 0.6, 0.9}).Content...)
	out := write(t, doc)
	for _, profile := range []models.PDFAProfile{models.PDFA1b, models.PDFA2b, models.PDFA4} {
		t.Run(string(profile), func(t *testing.T) {
			t.Parallel()
			report, fixed := remediate(t, out, func(ctx context.Context, w *pdf.Writer) (*models.RemediationReport, error) {
				return w.RemediatePDFA(ctx, profile)
			})
			for _, v := range report.Residual {
				t.Errorf("residual: %v", v)
			}
			violations, err := open(t, fixed).ValidatePDFA(context.Background(), profile)
			if err != nil {
				t.Fatalf("ValidatePDFA: %v", err)
			}
			for _, v := range violations {
				t.Errorf("remediated output: %v", v)
			}
		})
	}
}

// TestTransparencyGroupSpoolKeepsTheBytes locks that a form's
// /StructParents, set once the structure tree is written, reaches the file
// whether the form's stream was spooled or held.
func TestTransparencyGroupSpoolKeepsTheBytes(t *testing.T) {
	t.Parallel()
	doc := transparencyTagged(t)
	dir := t.TempDir()
	var outs [2][]byte
	for i, held := range []int64{-1, 1} {
		var buf bytes.Buffer
		if err := typeset.Write(context.Background(), doc, &buf, typeset.Options{TempDir: dir, Spool: held}); err != nil {
			t.Fatalf("Write with spool %d: %v", held, err)
		}
		outs[i] = buf.Bytes()
	}
	if !bytes.Equal(outs[0], outs[1]) {
		t.Error("the spool changes the output")
	}
	if !bytes.Contains(outs[1], []byte("/StructParents 1")) {
		t.Error("a spooled form lacks its /StructParents")
	}
}

// TestTransparencyEveryVersion locks that every version a conversion
// writes holds the constructs, PDF 1.4 ones (ISO 32000-1, Tables 58 and
// 95), and draws them alike: the blend modes, an isolated and a knockout
// group and both soft masks render the same at 1.4 and 2.0 as at 1.7.
func TestTransparencyEveryVersion(t *testing.T) {
	t.Parallel()
	docs := []*typeset.Document{
		{Pages: []*typeset.Page{blendPage([3]float64{0.8, 0.4, 0.2}, [3]float64{0.3, 0.6, 0.9})}},
		isoKnockoutDoc(t),
		masksDoc(t),
	}
	for i, doc := range docs {
		var renders []image.Image
		for _, v := range []typeset.Version{typeset.Version17, typeset.Version14, typeset.Version20} {
			var buf bytes.Buffer
			if err := typeset.Write(context.Background(), doc, &buf, typeset.Options{Version: v}); err != nil {
				t.Fatalf("Write at %s: %v", v, err)
			}
			if !bytes.HasPrefix(buf.Bytes(), []byte("%PDF-"+string(v))) {
				t.Errorf("document %d at %s: header %q", i, v, buf.Bytes()[:8])
			}
			renders = append(renders, render(t, open(t, buf.Bytes()), 1))
		}
		b := renders[0].Bounds()
		for k, img := range renders[1:] {
			differ := 0
			for y := b.Min.Y; y < b.Max.Y; y++ {
				for x := b.Min.X; x < b.Max.X; x++ {
					if !near(img, x, y, color.NRGBAModel.Convert(renders[0].At(x, y)).(color.NRGBA), 1) {
						differ++
					}
				}
			}
			if differ != 0 {
				t.Errorf("document %d: %d pixels differ from PDF 1.7 in version %d", i, differ, k+1)
			}
		}
	}
}
