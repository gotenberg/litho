package shade

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/render/surface"
)

// rgb8 returns pixel (x, y) of c as 8-bit sRGB channels.
func rgb8(c *surface.Canvas, x, y int) [3]float64 {
	p := c.Image().NRGBAAt(x, y)
	return [3]float64{float64(p.R), float64(p.G), float64(p.B)}
}

// sample is an expected 8-bit colour at a device pixel.
type sample struct {
	x, y int
	want [3]float64
}

func checkSamples(t *testing.T, c *surface.Canvas, tol float64, samples []sample) {
	t.Helper()
	for _, s := range samples {
		got := rgb8(c, s.x, s.y)
		for k := range 3 {
			if math.Abs(got[k]-s.want[k]) > tol {
				t.Errorf("pixel (%d,%d) = %v, want %v (tolerance %v)", s.x, s.y, got, s.want, tol)
				break
			}
		}
	}
}

var white = [3]float64{255, 255, 255}

// TestHandComputed pins each type against colours computed from the
// equations of ISO 32000-1, 8.7.4.5 at the pixel centre, independently of
// this implementation. Device pixel (x, y) has its centre at page point
// (x+0.5, 99.5-y). A tolerance of 1 covers 8-bit rounding and the ramp's
// interpolation.
func TestHandComputed(t *testing.T) {
	t.Parallel()

	axial := func(x int) [3]float64 {
		s := (float64(x) + 0.5 - 10) / 80
		return [3]float64{255 * (1 - s), 0, 255 * s}
	}
	cases := []struct {
		name    string
		f       fixture
		samples []sample
	}{
		{"axial", fixtureAxial, []sample{
			{5, 50, [3]float64{255, 0, 0}}, // before the start: extended
			{9, 10, [3]float64{255, 0, 0}},
			{30, 50, axial(30)},
			{50, 20, axial(50)},
			{80, 70, axial(80)},
			{90, 50, white}, // centre past the end, which is not extended
			{99, 50, white},
		}},
		// s is the larger root of the circle equation with r(s) >= 0.
		{"radial", fixtureRadial, []sample{
			{20, 50, [3]float64{215.05, 235.03, 39.95}}, // inside the start circle, a larger circle wins
			{40, 40, [3]float64{95.3, 175.15, 159.7}},
			{53, 36, [3]float64{15.3, 135.15, 239.7}},
			{5, 50, [3]float64{255, 255, 0}}, // extended before the start, inside the cone
			{95, 50, [3]float64{0, 127.5, 255}},
			{5, 10, white}, // outside the cone
		}},
		// Barycentric weights of red (10,10), green (90,10), blue (50,90).
		{"gouraud", fixtureGouraud, []sample{
			{50, 50, [3]float64{62.95, 66.14, 125.91}},
			{30, 80, [3]float64{174.52, 50.2, 30.28}},
			{70, 80, [3]float64{47.02, 177.7, 30.28}},
			{50, 20, [3]float64{15.14, 18.33, 221.53}},
			{10, 50, white},
		}},
		// t interpolated across each triangle, then grey = 0.2 + 0.8 t.
		{"strip", fixtureStrip, []sample{
			{69, 45, [3]float64{117.45, 117.45, 117.45}}, // third triangle, from flag 2
			{20, 80, [3]float64{90.04, 90.04, 90.04}},    // first triangle
			{45, 65, [3]float64{198.44, 198.44, 198.44}}, // second triangle, from flag 1
			{90, 50, white},
		}},
		// grey = (u + v) / 2 with u = (x-10)/80, v = (y-10)/80.
		{"function-based", fixtureFunctionBased, []sample{
			{20, 79, [3]float64{255 * 10.5 / 80, 255 * 10.5 / 80, 255 * 10.5 / 80}},
			{50, 50, [3]float64{127.5, 127.5, 127.5}},
			{80, 30, [3]float64{255 * (70.5 + 59.5) / 160, 255 * (70.5 + 59.5) / 160, 255 * (70.5 + 59.5) / 160}},
			{5, 50, white}, // outside /Domain
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			checkSamples(t, renderFixture(t, tc.f, nil), 1, tc.samples)
		})
	}
}

// TestCoonsBilinear checks a Coons patch whose boundary is the straight
// square (10,10)-(91,91) with control points at the thirds of each side. Each
// edge is then linear in its parameter, the surface is the identity map of
// the square scaled, so (u, v) at a point is its position along each side and
// the colour is the bilinear blend of the corners (8.7.4.5.7).
func TestCoonsBilinear(t *testing.T) {
	t.Parallel()
	f := fixture{
		content: "/Sh sh",
		shading: streamObj("/ShadingType 6 /ColorSpace /DeviceRGB /BitsPerCoordinate 8 /BitsPerComponent 8"+
			" /BitsPerFlag 8 /Decode [0 255 0 255 0 1 0 1 0 1]",
			packBits(0, 8,
				10, 8, 10, 8, 10, 8, 37, 8, 10, 8, 64, 8, 10, 8, 91, 8,
				37, 8, 91, 8, 64, 8, 91, 8, 91, 8, 91, 8,
				91, 8, 64, 8, 91, 8, 37, 8, 91, 8, 10, 8,
				64, 8, 10, 8, 37, 8, 10, 8,
				255, 8, 0, 8, 0, 8, 0, 8, 255, 8, 0, 8, 0, 8, 0, 8, 255, 8, 0, 8, 0, 8, 0, 8)),
	}
	c := renderFixture(t, f, nil)
	// u runs along x (p00 to p30), v along y (p00 to p03); the corners are
	// c00 red, c03 green, c33 blue, c30 black.
	want := func(x, y int) [3]float64 {
		u := (float64(x) + 0.5 - 10) / 81
		v := (99.5 - float64(y) - 10) / 81
		return [3]float64{255 * (1 - u) * (1 - v), 255 * (1 - u) * v, 255 * u * v}
	}
	var samples []sample
	for _, p := range [][2]int{{15, 15}, {50, 50}, {20, 70}, {75, 25}, {85, 85}} {
		samples = append(samples, sample{p[0], p[1], want(p[0], p[1])})
	}
	checkSamples(t, c, 1, samples)
}

// TestPatternFill checks a shading pattern: the pattern matrix [2 0 0 2 30 0]
// maps the axis 0..20 onto page x 30..70, and /Background fills the shape
// where the unextended axis does not reach (Table 78).
func TestPatternFill(t *testing.T) {
	t.Parallel()
	clip := squareMask(t, 20, 20, 80, 80)
	c := renderFixture(t, fixturePattern, clip)
	at := func(x int) [3]float64 {
		s := (float64(x) + 0.5 - 30) / 40
		return [3]float64{255 * s, 255 * (1 - s), 255 * s}
	}
	checkSamples(t, c, 1, []sample{
		{25, 50, [3]float64{127.5, 127.5, 127.5}}, // background before the axis
		{40, 50, at(40)},
		{60, 30, at(60)},
		{75, 50, [3]float64{127.5, 127.5, 127.5}}, // background past it
		{10, 50, white}, // outside the shape
		{50, 85, white},
	})
}

// squareMask returns a pageSide mask covering [x0, x1) x [y0, y1).
func squareMask(t testing.TB, x0, y0, x1, y1 int) *surface.Mask {
	t.Helper()
	m, err := surface.NewMask(pageSide, pageSide, pageSide*pageSide)
	if err != nil {
		t.Fatal(err)
	}
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			m.Set(x, y, 1)
		}
	}
	return m
}

// TestAgainstPoppler compares interior pixels with the pinned poppler
// (POPPLER_VERSION in test/integration/corpus/versions.env), whose renders of
// these fixtures at 72 dpi gave the expected values. Edge pixels are left
// out: poppler paints a shading's own boundary pixels where any part of them
// is covered, while this package samples pixel centres.
//
// The radial tolerance is wider because poppler samples a radial shading
// about half a pixel off the centre: at (40,40) it paints 104,179,151, the
// exact centre value is 95.3,175.15,159.7 (see [TestHandComputed]), and the
// neighbouring centre gives poppler's value.
func TestAgainstPoppler(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		f       fixture
		clip    bool
		tol     float64
		samples []sample
	}{
		{"axial", fixtureAxial, false, 3, []sample{
			{5, 50, [3]float64{255, 0, 0}}, {30, 50, [3]float64{191, 0, 64}},
			{50, 50, [3]float64{127, 0, 128}}, {80, 50, [3]float64{32, 0, 223}},
		}},
		{"radial", fixtureRadial, false, 10, []sample{
			{20, 50, [3]float64{219, 237, 36}}, {40, 40, [3]float64{104, 179, 151}},
			{70, 50, [3]float64{0, 128, 255}}, {30, 55, [3]float64{155, 205, 100}},
		}},
		{"gouraud", fixtureGouraud, false, 4, []sample{
			{50, 50, [3]float64{64, 64, 128}}, {30, 80, [3]float64{175, 48, 32}},
			{70, 80, [3]float64{48, 175, 32}}, {50, 30, [3]float64{32, 32, 191}},
		}},
		{"coons", fixtureCoons, false, 4, []sample{
			{20, 50, [3]float64{128, 128, 52}}, {50, 50, [3]float64{128, 128, 139}},
			{80, 50, [3]float64{128, 128, 227}}, {50, 20, [3]float64{32, 122, 135}},
			{50, 80, [3]float64{227, 134, 135}}, {30, 30, [3]float64{64, 153, 76}},
		}},
		{"coons-shared-edge", fixtureCoonsShared, false, 5, []sample{
			{50, 30, [3]float64{96, 159, 159}}, {50, 70, [3]float64{64, 64, 64}},
			{20, 60, [3]float64{59, 164, 23}}, {80, 40, [3]float64{60, 84, 227}},
		}},
		{"lattice", fixtureLattice, false, 3, []sample{
			{30, 30, [3]float64{127, 127, 127}}, {70, 70, [3]float64{212, 212, 212}},
			{30, 70, [3]float64{85, 85, 85}}, {70, 30, [3]float64{170, 170, 170}},
		}},
		{"tensor", fixtureTensor, false, 5, []sample{
			{30, 30, [3]float64{48, 152, 88}}, {50, 50, [3]float64{96, 119, 163}},
			{70, 70, [3]float64{175, 159, 211}}, {30, 70, [3]float64{155, 122, 104}},
			{70, 30, [3]float64{52, 80, 207}},
		}},
		{"function-based", fixtureFunctionBased, false, 2, []sample{
			{20, 20, [3]float64{128, 128, 128}}, {50, 50, [3]float64{128, 128, 128}},
			{80, 30, [3]float64{207, 207, 207}},
		}},
		{"pattern", fixturePattern, true, 4, []sample{
			{25, 50, [3]float64{128, 128, 128}}, {40, 50, [3]float64{64, 191, 64}},
			{60, 50, [3]float64{191, 64, 191}}, {10, 50, [3]float64{255, 255, 255}},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var clip *surface.Mask
			if tc.clip {
				clip = squareMask(t, 20, 20, 80, 80)
			}
			checkSamples(t, renderFixture(t, tc.f, clip), tc.tol, tc.samples)
		})
	}
}

// TestPaintTarget covers the target's alpha, clip and validation.
func TestPaintTarget(t *testing.T) {
	t.Parallel()
	ctx, doc := openFixture(t, fixtureAxial.bytes())
	sh, err := New(ctx, doc, ast.PdfIndirectRef{ObjectNum: 5}, deviceComponents)
	if err != nil {
		t.Fatal(err)
	}
	newCanvas := func() *surface.Canvas {
		c, err := surface.NewCanvas(pageSide, pageSide, pageSide*pageSide)
		if err != nil {
			t.Fatal(err)
		}
		c.Fill(1, 1, 1, 1)
		return c
	}

	t.Run("alpha", func(t *testing.T) {
		c := newCanvas()
		if err := sh.Paint(ctx, Target{Canvas: c, Opacity: surface.NewOpacity(0.5), Convert: deviceRGB{}}, pageToDevice); err != nil {
			t.Fatal(err)
		}
		// Red at half alpha over white.
		checkSamples(t, c, 1, []sample{{5, 50, [3]float64{255, 127.5, 127.5}}})
	})

	t.Run("clip", func(t *testing.T) {
		c := newCanvas()
		clip := squareMask(t, 0, 0, 50, 100)
		clip.Set(60, 50, 0.25)
		if err := sh.Paint(ctx, Target{Canvas: c, Clip: clip, Convert: deviceRGB{}}, pageToDevice); err != nil {
			t.Fatal(err)
		}
		s := (60.5 - 10) / 80
		checkSamples(t, c, 1, []sample{
			{5, 50, [3]float64{255, 0, 0}},
			{70, 50, white},
			{60, 50, [3]float64{255 * (0.75 + 0.25*(1-s)), 255 * 0.75, 255 * (0.75 + 0.25*s)}},
		})
	})

	t.Run("bbox", func(t *testing.T) {
		f := fixtureAxial
		f.shading = "<< /ShadingType 2 /ColorSpace /DeviceRGB /BBox [0 0 50 100] /Coords [10 0 90 0]" +
			" /Extend [true true] /Function " + rgbFunc("1 0 0", "0 0 1") + " >>"
		c := renderFixture(t, f, nil)
		checkSamples(t, c, 1, []sample{{5, 50, [3]float64{255, 0, 0}}, {60, 50, white}})
	})

	t.Run("singular matrix", func(t *testing.T) {
		c := newCanvas()
		if err := sh.Paint(ctx, Target{Canvas: c, Convert: deviceRGB{}}, geom.Matrix{}); err != nil {
			t.Fatal(err)
		}
		checkSamples(t, c, 0, []sample{{5, 50, white}})
	})

	t.Run("invalid", func(t *testing.T) {
		small, err := surface.NewMask(10, 10, 100)
		if err != nil {
			t.Fatal(err)
		}
		for _, target := range []Target{
			{Convert: deviceRGB{}},
			{Canvas: newCanvas()},
			{Canvas: newCanvas(), Clip: small, Convert: deviceRGB{}},
		} {
			if err := sh.Paint(ctx, target, pageToDevice); !errors.Is(err, ErrTarget) {
				t.Errorf("Paint(%+v) = %v, want ErrTarget", target, err)
			}
		}
	})

	t.Run("cancelled", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		cancel()
		err := sh.Paint(cctx, Target{Canvas: newCanvas(), Convert: deviceRGB{}}, pageToDevice)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Paint with a cancelled context = %v, want context.Canceled", err)
		}
	})
}

// TestConcurrentPaint paints one compiled shading of each kind from several
// goroutines; -race checks that painting writes nothing into the shading.
func TestConcurrentPaint(t *testing.T) {
	t.Parallel()
	for _, f := range []fixture{fixtureRadial, fixtureGouraud, fixtureTensor, fixtureFunctionBased} {
		ctx, doc := openFixture(t, f.bytes())
		sh, err := New(ctx, doc, ast.PdfIndirectRef{ObjectNum: 5}, deviceComponents)
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		errs := make([]error, 4)
		for i := range errs {
			wg.Go(func() {
				c, err := surface.NewCanvas(pageSide, pageSide, pageSide*pageSide)
				if err != nil {
					errs[i] = err
					return
				}
				errs[i] = sh.Paint(ctx, Target{Canvas: c, Convert: deviceRGB{}}, pageToDevice)
			})
		}
		wg.Wait()
		for _, err := range errs {
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}

// TestMalformed checks that each broken shading is refused by name.
func TestMalformed(t *testing.T) {
	t.Parallel()
	gouraud := func(dict string) string {
		return streamObj("/ShadingType 4 /ColorSpace /DeviceRGB "+dict, "")
	}
	cases := []struct {
		name    string
		shading string
		want    error
	}{
		{"unknown type", "<< /ShadingType 9 /ColorSpace /DeviceRGB >>", ErrUnsupportedType},
		{"no colour space", "<< /ShadingType 2 /Coords [0 0 1 0] /Function " + rgbFunc("0 0 0", "1 1 1") + " >>", ErrMalformed},
		{"axial without function", "<< /ShadingType 2 /ColorSpace /DeviceRGB /Coords [0 0 1 0] >>", ErrMalformed},
		{"axial short coords", "<< /ShadingType 2 /ColorSpace /DeviceRGB /Coords [0 0 1] /Function " + rgbFunc("0 0 0", "1 1 1") + " >>", ErrMalformed},
		{"radial negative radius", "<< /ShadingType 3 /ColorSpace /DeviceRGB /Coords [0 0 -1 0 0 5] /Function " + rgbFunc("0 0 0", "1 1 1") + " >>", ErrMalformed},
		{"function outputs", "<< /ShadingType 2 /ColorSpace /DeviceRGB /Coords [0 0 1 0] /Function " + rgbFunc("0", "1") + " >>", ErrMalformed},
		{"function array length", "<< /ShadingType 2 /ColorSpace /DeviceRGB /Coords [0 0 1 0] /Function [" + rgbFunc("0", "1") + "] >>", ErrMalformed},
		{"background length", "<< /ShadingType 2 /ColorSpace /DeviceRGB /Background [1] /Coords [0 0 1 0] /Function " + rgbFunc("0 0 0", "1 1 1") + " >>", ErrMalformed},
		{"mesh as dictionary", "<< /ShadingType 4 /ColorSpace /DeviceRGB /BitsPerCoordinate 8 /BitsPerComponent 8 /BitsPerFlag 8 /Decode [0 1 0 1 0 1 0 1 0 1] >>", ErrMalformed},
		{"bad coordinate depth", gouraud("/BitsPerCoordinate 7 /BitsPerComponent 8 /BitsPerFlag 8 /Decode [0 1 0 1 0 1 0 1 0 1]"), ErrMalformed},
		{"bad flag depth", gouraud("/BitsPerCoordinate 8 /BitsPerComponent 8 /BitsPerFlag 1 /Decode [0 1 0 1 0 1 0 1 0 1]"), ErrMalformed},
		{"short decode", gouraud("/BitsPerCoordinate 8 /BitsPerComponent 8 /BitsPerFlag 8 /Decode [0 1 0 1 0 1]"), ErrMalformed},
		{"lattice row", streamObj("/ShadingType 5 /ColorSpace /DeviceGray /BitsPerCoordinate 8 /BitsPerComponent 8 /VerticesPerRow 1 /Decode [0 1 0 1 0 1]", ""), ErrMalformed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx, doc := openFixture(t, fixture{content: "/Sh sh", shading: tc.shading}.bytes())
			_, err := New(ctx, doc, ast.PdfIndirectRef{ObjectNum: 5}, deviceComponents)
			if !errors.Is(err, tc.want) {
				t.Errorf("New = %v, want %v", err, tc.want)
			}
		})
	}

	t.Run("tiling pattern", func(t *testing.T) {
		t.Parallel()
		f := fixturePattern
		f.pattern = "<< /PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 1 1] /XStep 1 /YStep 1 /Resources << >> >>"
		ctx, doc := openFixture(t, f.bytes())
		if _, err := NewPattern(ctx, doc, ast.PdfIndirectRef{ObjectNum: 6}, deviceComponents); !errors.Is(err, ErrUnsupportedType) {
			t.Errorf("NewPattern = %v, want ErrUnsupportedType", err)
		}
	})

	t.Run("colour space", func(t *testing.T) {
		t.Parallel()
		ctx, doc := openFixture(t, fixtureAxial.bytes())
		failing := func(context.Context, ast.PdfObject) (int, error) { return 0, fmt.Errorf("no space") }
		if _, err := New(ctx, doc, ast.PdfIndirectRef{ObjectNum: 5}, failing); err == nil {
			t.Error("New with a failing colour space resolver succeeded")
		}
		tooWide := func(context.Context, ast.PdfObject) (int, error) { return maxComponents + 1, nil }
		if _, err := New(ctx, doc, ast.PdfIndirectRef{ObjectNum: 5}, tooWide); !errors.Is(err, ErrMalformed) {
			t.Errorf("New with %d components = %v, want ErrMalformed", maxComponents+1, err)
		}
	})
}

// TestAccessors checks what a compiled shading and pattern report.
func TestAccessors(t *testing.T) {
	t.Parallel()
	ctx, doc := openFixture(t, fixturePattern.bytes())
	pat, err := NewPattern(ctx, doc, ast.PdfIndirectRef{ObjectNum: 6}, deviceComponents)
	if err != nil {
		t.Fatal(err)
	}
	if pat.Matrix() != (geom.Matrix{2, 0, 0, 2, 30, 0}) {
		t.Errorf("Matrix() = %v", pat.Matrix())
	}
	if pat.ExtGState() != nil {
		t.Errorf("ExtGState() = %v, want nil", pat.ExtGState())
	}
	sh := pat.Shading()
	if sh.Type() != 2 || sh.Components() != 3 || sh.ColorSpace() != ast.PdfName("DeviceRGB") {
		t.Errorf("Type, Components, ColorSpace = %d, %d, %v", sh.Type(), sh.Components(), sh.ColorSpace())
	}
	if bg, ok := sh.Background(); !ok || len(bg) != 3 || bg[0] != 0.5 {
		t.Errorf("Background() = %v, %v", bg, ok)
	}
	if _, ok := sh.BBox(); ok {
		t.Error("BBox() reports a box the shading does not have")
	}
}
