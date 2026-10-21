package page

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
)

// The expected pixels below are computed by hand: at 72 dpi a 100-unit page
// is 100 pixels and user point (x, y) lands on device (x, 100 - y), so a
// pixel's column is the user x it starts at and its row the user y it ends
// at, subtracted from 100.

func TestFillRectangle(t *testing.T) {
	t.Parallel()
	img, _ := draw(t, fixture{content: "1 0 0 rg 10 10 20 20 re f"})
	// User x 10..30 is columns 10..29, user y 10..30 rows 70..89.
	expect(t, img, 0, map[[2]int]rgb{
		{10, 70}: red, {29, 89}: red, {15, 80}: red,
		{9, 80}: paper, {30, 80}: paper, {15, 69}: paper, {15, 90}: paper,
	})
}

func TestFillRules(t *testing.T) {
	t.Parallel()
	const rects = "0 g 10 10 80 80 re 30 30 40 40 re "
	img, _ := draw(t, fixture{content: rects + "f*"})
	expect(t, img, 0, map[[2]int]rgb{{50, 50}: paper, {20, 50}: black})
	// Both rectangles wind the same way, so nonzero fills the hole.
	img, _ = draw(t, fixture{content: rects + "f"})
	expect(t, img, 0, map[[2]int]rgb{{50, 50}: black, {20, 50}: black})
}

func TestStroke(t *testing.T) {
	t.Parallel()
	// A 4-unit line along user y 50 covers rows 48 to 51, butt-capped at
	// x 10 and 90.
	img, _ := draw(t, fixture{content: "0 G 4 w 10 50 m 90 50 l S"})
	expect(t, img, 0, map[[2]int]rgb{
		{50, 48}: black, {50, 51}: black, {50, 47}: paper, {50, 52}: paper,
		{10, 50}: black, {89, 50}: black, {9, 50}: paper, {90, 50}: paper,
	})
	// A zero-width line is one device pixel wide (ISO 32000-1, 8.4.3.2):
	// centred on row 49.5, it covers row 49.
	img, _ = draw(t, fixture{content: "0 G 0 w 10 50.5 m 90 50.5 l S"})
	expect(t, img, 0, map[[2]int]rgb{{50, 49}: black, {50, 48}: paper, {50, 50}: paper})
	// s closes the path before stroking: the closing edge from (10, 90) back
	// to (10, 10) paints user x 9 to 11, which S leaves unpainted.
	img, _ = draw(t, fixture{content: "0 G 2 w 10 10 m 90 10 l 90 90 l 10 90 l s"})
	expect(t, img, 0, map[[2]int]rgb{{10, 50}: black, {20, 50}: paper})
	img, _ = draw(t, fixture{content: "0 G 2 w 10 10 m 90 10 l 90 90 l 10 90 l S"})
	expect(t, img, 0, map[[2]int]rgb{{10, 50}: paper, {90, 50}: black})
}

func TestClipping(t *testing.T) {
	t.Parallel()
	img, _ := draw(t, fixture{content: "20 20 40 40 re W n 0 g 0 0 100 100 re f"})
	expect(t, img, 0, map[[2]int]rgb{{30, 70}: black, {20, 40}: black, {59, 79}: black,
		{19, 50}: paper, {60, 50}: paper, {30, 39}: paper, {30, 80}: paper})
	// Q restores the clip of the state q saved (ISO 32000-1, 8.4.2).
	img, _ = draw(t, fixture{content: "q 20 20 40 40 re W n Q 0 g 0 0 100 100 re f"})
	expect(t, img, 0, map[[2]int]rgb{{5, 5}: black, {95, 95}: black})
	// A clip that is no rectangle masks: the lower-left half of the page.
	img, _ = draw(t, fixture{content: "0 0 m 100 0 l 0 100 l h W n 0 g 0 0 100 100 re f"})
	expect(t, img, 0, map[[2]int]rgb{{10, 90}: black, {90, 10}: paper})
	// W* clips with the even-odd rule: the inner square is outside.
	img, _ = draw(t, fixture{content: "10 10 80 80 re 30 30 40 40 re W* n 0 g 0 0 100 100 re f"})
	expect(t, img, 0, map[[2]int]rgb{{50, 50}: paper, {20, 50}: black, {5, 50}: paper})
	// The clip applies after the path paints (8.5.4): the rectangle is
	// filled whole, the later fill only inside it.
	img, _ = draw(t, fixture{content: "1 0 0 rg 0 0 50 100 re W f 0 0 1 rg 0 0 100 100 re f"})
	expect(t, img, 0, map[[2]int]rgb{{25, 50}: blue, {75, 50}: paper})
}

func TestColoursAndAlpha(t *testing.T) {
	t.Parallel()
	img, report := draw(t, fixture{
		page: "/Resources << /ExtGState << /Half << /ca 0.5 >> >> /ColorSpace << /Spot [/Separation /Spot /DeviceCMYK 5 0 R] /Nothing [/Separation /None /DeviceGray 6 0 R] >> >>",
		content: "1 0 0 0 k 0 0 20 20 re f  0.5 g 20 0 20 20 re f  /Spot cs 1 sc 40 0 20 20 re f " +
			"q /Half gs 0 g 60 0 20 20 re f Q  /Nothing cs 1 sc 80 0 20 20 re f",
		objs: []string{
			"<< /FunctionType 2 /Domain [0 1] /C0 [0 0 0 0] /C1 [0 1 1 0] /N 1 >>",
			"<< /FunctionType 2 /Domain [0 1] /C0 [1] /C1 [0] /N 1 >>",
		},
	})
	expect(t, img, 1, map[[2]int]rgb{
		// Uncalibrated DeviceCMYK goes through litho's CMYK profile, so
		// cyan is the printed cyan, not 0 255 255.
		{10, 90}: {0, 174, 239},
		{30, 90}: {128, 128, 128},
		// The tint transform maps the spot to CMYK magenta and yellow.
		{50, 90}: {237, 28, 36},
		// Black at alpha 0.5 over white.
		{70, 90}: {128, 128, 128},
		// The colorant None paints nothing (ISO 32000-1, 8.6.6.4).
		{90, 90}: paper,
	})
	if len(report.Unsupported) != 0 {
		t.Errorf("report %v, want nothing unsupported", report.Unsupported)
	}
}

func TestImages(t *testing.T) {
	t.Parallel()
	img, _ := draw(t, fixture{
		page: "/Resources << /XObject << /Im 5 0 R /Mk 6 0 R /Sm 7 0 R >> >>",
		content: "q 20 0 0 10 10 10 cm /Im Do Q " +
			"0 1 0 rg q 10 0 0 10 40 10 cm /Mk Do Q " +
			"q 10 0 0 10 60 10 cm /Sm Do Q",
		objs: []string{
			stream("/Type /XObject /Subtype /Image /Width 2 /Height 1 /ColorSpace /DeviceRGB /BitsPerComponent 8", "\xff\x00\x00\x00\x00\xff"),
			// A 1-bit image mask whose 0 sample paints (8.9.6.2).
			stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ImageMask true", "\x00"),
			stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /SMask 8 0 R", "\x00"),
			stream("/Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8", "\x80"),
		},
	})
	expect(t, img, 0, map[[2]int]rgb{{15, 85}: red, {25, 85}: blue, {9, 85}: paper, {30, 85}: paper, {45, 85}: {0, 255, 0}})
	// Black at the soft mask's opacity of 128/255 over white.
	expect(t, img, 1, map[[2]int]rgb{{65, 85}: {127, 127, 127}})
}

func TestInlineImage(t *testing.T) {
	t.Parallel()
	img, _ := draw(t, fixture{content: "q 20 0 0 10 10 10 cm BI /W 2 /H 1 /CS /RGB /BPC 8 ID \xff\x00\x00\x00\x00\xff EI Q"})
	expect(t, img, 0, map[[2]int]rgb{{15, 85}: red, {25, 85}: blue})
	// A colour space named through the resources, abbreviated filters.
	img, _ = draw(t, fixture{
		page:    "/Resources << /ColorSpace << /CS0 /DeviceRGB >> >>",
		content: "q 20 0 0 10 10 10 cm BI /W 2 /H 1 /CS /CS0 /BPC 8 /F /AHx ID ff00000000ff> EI Q",
	})
	expect(t, img, 0, map[[2]int]rgb{{15, 85}: red, {25, 85}: blue})
}

// axial is a type 2 shading from black at x 0 to white at x 100.
const axial = "<< /ShadingType 2 /ColorSpace /DeviceGray /Coords [0 0 100 0] " +
	"/Function << /FunctionType 2 /Domain [0 1] /C0 [0] /C1 [1] /N 1 >> >>"

func TestShadings(t *testing.T) {
	t.Parallel()
	img, _ := draw(t, fixture{page: "/Resources << /Shading << /Sh 5 0 R >> >>", content: "/Sh sh", objs: []string{axial}})
	expect(t, img, 2, map[[2]int]rgb{{0, 50}: {1, 1, 1}, {50, 50}: {128, 128, 128}, {99, 50}: {254, 254, 254}})
	// A shading pattern fills the shape; the pattern space is the page's.
	img, _ = draw(t, fixture{
		page:    "/Resources << /Pattern << /P 6 0 R >> >>",
		content: "/Pattern cs /P scn 0 0 50 100 re f",
		objs:    []string{axial, "<< /PatternType 2 /Shading 5 0 R >>"},
	})
	expect(t, img, 2, map[[2]int]rgb{{25, 50}: {65, 65, 65}, {75, 50}: paper})
}

func TestForms(t *testing.T) {
	t.Parallel()
	// The form's matrix moves it to (10, 10) and its /BBox clips the fill.
	img, _ := draw(t, fixture{
		page:    "/Resources << /XObject << /Fm 5 0 R >> >>",
		content: "/Fm Do 1 0 0 rg 90 90 5 5 re f",
		objs: []string{stream("/Type /XObject /Subtype /Form /BBox [0 0 20 20] /Matrix [1 0 0 1 10 10] "+
			"/Resources << /ExtGState << /G << /ca 1 >> >> >>", "q /G gs 0 g -10 -10 100 100 re f Q")},
	})
	expect(t, img, 0, map[[2]int]rgb{{10, 70}: black, {29, 89}: black, {31, 50}: paper, {50, 85}: paper, {92, 7}: red})
}

func TestFormGuards(t *testing.T) {
	t.Parallel()
	// A form that paints itself is caught, not run until the depth budget.
	_, report := draw(t, fixture{
		page:    "/Resources << /XObject << /Fm 5 0 R >> >>",
		content: "/Fm Do",
		objs:    []string{stream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Resources << /XObject << /Fm 5 0 R >> >>", "/Fm Do")},
	})
	if report.Unsupported[Skipped] != 1 {
		t.Errorf("self-painting form: %v, want one skip", report.Unsupported)
	}
	// The form-depth budget stops a chain of forms.
	f := fixture{
		page:    "/Resources << /XObject << /A 5 0 R >> >>",
		content: "/A Do",
		objs: []string{
			stream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /Resources << /XObject << /B 6 0 R >> >>", "/B Do"),
			stream("/Type /XObject /Subtype /Form /BBox [0 0 100 100]", "0 g 0 0 100 100 re f"),
		},
	}
	doc, pg := open(t, f.bytes(), limits.Limits{MaxFormDepth: 1})
	c, report, err := Render(context.Background(), doc, pg, Options{DPI: 72, Background: white})
	if err != nil {
		t.Fatal(err)
	}
	if report.Unsupported[Skipped] != 1 {
		t.Errorf("form past the depth budget: %v, want one skip", report.Unsupported)
	}
	expect(t, c.Image(), 0, map[[2]int]rgb{{50, 50}: paper})
}

func TestReportsUnsupported(t *testing.T) {
	t.Parallel()
	_, report := draw(t, fixture{
		page: "/Resources << /Font << /F 5 0 R >> /Pattern << /T 6 0 R >> /ExtGState << /M << /BM /Multiply >> >> >>",
		content: "BT /F 12 Tf (a) Tj [(b)] TJ ET /Pattern cs /T scn 0 0 10 10 re f " +
			"0 g /M gs 0 0 10 10 re f",
		objs: []string{
			"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
			stream("/PatternType 1 /PaintType 1 /TilingType 1 /BBox [0 0 10 10] /XStep 10 /YStep 10 /Resources << >>", "0 g 0 0 5 5 re f"),
		},
	})
	// Helvetica is not embedded and no provider serves it: a and b draw
	// placeholders.
	want := map[string]int{PlaceholderGlyph: 2, UnsupportedTilingPattern: 1, UnsupportedBlendMode: 1}
	for k, n := range want {
		if report.Unsupported[k] != n {
			t.Errorf("%s: %d, want %d (report %v)", k, report.Unsupported[k], n, report.Unsupported)
		}
	}
}

func TestOptionalContent(t *testing.T) {
	t.Parallel()
	img, _ := draw(t, fixture{
		catalog: "/OCProperties << /OCGs [6 0 R] /D << /OFF [6 0 R] >> >>",
		page:    "/Resources << /Properties << /Off 6 0 R >> /XObject << /Fm 5 0 R >> >>",
		content: "/OC /Off BDC 0 g 0 0 50 50 re f EMC 1 0 0 rg 50 50 50 50 re f " +
			"/Fm Do",
		objs: []string{
			stream("/Type /XObject /Subtype /Form /BBox [0 0 100 100] /OC 6 0 R", "0 0 1 rg 0 50 50 50 re f"),
			"<< /Type /OCG /Name (hidden) >>",
		},
	})
	expect(t, img, 0, map[[2]int]rgb{{25, 75}: paper, {75, 25}: red, {25, 25}: paper})
}

func TestPageGeometry(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		page          string
		dpi           float64
		width, height int
		probe         geom.Point // user point
		want          geom.Point // its device position
	}{
		{"/MediaBox [0 0 100 50]", 72, 100, 50, geom.Point{X: 10, Y: 10}, geom.Point{X: 10, Y: 40}},
		{"/MediaBox [0 0 100 50] /Rotate 90", 72, 50, 100, geom.Point{X: 10, Y: 10}, geom.Point{X: 10, Y: 10}},
		{"/MediaBox [0 0 100 50] /Rotate 180", 72, 100, 50, geom.Point{X: 10, Y: 10}, geom.Point{X: 90, Y: 10}},
		{"/MediaBox [0 0 100 50] /Rotate -90", 72, 50, 100, geom.Point{X: 10, Y: 10}, geom.Point{X: 40, Y: 90}},
		{"/MediaBox [0 0 100 50] /CropBox [10 10 60 40]", 144, 100, 60, geom.Point{X: 10, Y: 40}, geom.Point{X: 0, Y: 0}},
		{"/MediaBox [0 0 100 50] /UserUnit 2", 72, 200, 100, geom.Point{X: 0, Y: 50}, geom.Point{X: 0, Y: 0}},
		{"/MediaBox [0 0 100.4 50]", 72, 101, 50, geom.Point{}, geom.Point{X: 0, Y: 50}},
	} {
		_, pg := open(t, fixture{page: tc.page}.bytes(), limits.Limits{})
		g, err := PageGeometry(pg, tc.dpi)
		if err != nil {
			t.Fatalf("%s: %v", tc.page, err)
		}
		got := g.Matrix.Apply(tc.probe)
		if g.Width != tc.width || g.Height != tc.height || math.Abs(got.X-tc.want.X) > 1e-9 || math.Abs(got.Y-tc.want.Y) > 1e-9 {
			t.Errorf("%s: %dx%d, %v -> %v; want %dx%d, %v", tc.page, g.Width, g.Height, tc.probe, got, tc.width, tc.height, tc.want)
		}
	}
	_, pg := open(t, fixture{}.bytes(), limits.Limits{})
	for _, dpi := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		if _, err := PageGeometry(pg, dpi); !errors.Is(err, ErrInvalidOptions) {
			t.Errorf("dpi %v: %v, want ErrInvalidOptions", dpi, err)
		}
	}
}

func TestRotatedPage(t *testing.T) {
	t.Parallel()
	img, _ := draw(t, fixture{page: "/MediaBox [0 0 100 50] /Rotate 90", content: "0 g 0 0 10 10 re f"})
	if b := img.Bounds(); b.Dx() != 50 || b.Dy() != 100 {
		t.Fatalf("size %v, want 50x100", b)
	}
	// Turned clockwise, the page's lower-left corner is the top-left one.
	expect(t, img, 0, map[[2]int]rgb{{5, 5}: black, {5, 15}: paper, {45, 95}: paper})
}

func TestBudgets(t *testing.T) {
	t.Parallel()
	data := fixture{content: "0 g 0 0 100 100 re f"}.bytes()
	doc, pg := open(t, data, limits.Limits{MaxRenderPixels: 9_999})
	if _, _, err := Render(context.Background(), doc, pg, Options{DPI: 72}); !errors.Is(err, ErrTooLarge) {
		t.Errorf("page past the pixel budget: %v, want ErrTooLarge", err)
	}
	// Nine nested triangular clips keep nine masks, past eight page budgets.
	var nested strings.Builder
	for i := range 9 {
		nested.WriteString("q 0 0 m 100 0 l " + strconv.Itoa(i) + " 100 l h W n ")
	}
	doc, pg = open(t, fixture{content: nested.String()}.bytes(), limits.Limits{MaxRenderPixels: 10_000})
	if _, _, err := Render(context.Background(), doc, pg, Options{DPI: 72}); !errors.Is(err, ErrTooLarge) {
		t.Errorf("nine nested masks: %v, want ErrTooLarge", err)
	}
	// Rectangular clips cost no mask, however deep they nest.
	doc, pg = open(t, fixture{content: strings.Repeat("q 1 1 98 98 re W n ", 50) + "0 g 0 0 100 100 re f"}.bytes(), limits.Limits{MaxRenderPixels: 10_000})
	if _, _, err := Render(context.Background(), doc, pg, Options{DPI: 72}); err != nil {
		t.Errorf("rectangular clips: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	doc, pg = open(t, fixture{content: strings.Repeat("0 0 1 1 re f ", 200)}.bytes(), limits.Limits{})
	if _, _, err := Render(ctx, doc, pg, Options{DPI: 72}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled render: %v, want context.Canceled", err)
	}
}

// busyPage exercises every painting path at once.
var busyPage = fixture{
	page: "/Resources << /XObject << /Im 5 0 R /Fm 6 0 R >> /Shading << /Sh 7 0 R >> >>",
	content: "q 0 0 m 100 0 l 0 100 l h W n /Sh sh Q 0 0 1 RG 3 w 1 J 1 j [4 2] 0 d 10 10 m 90 90 l 90 10 l S " +
		"q 30 0 0 30 60 60 cm /Im Do Q q 0.7 0 0 0.7 5 5 cm /Fm Do Q",
	objs: []string{
		stream("/Type /XObject /Subtype /Image /Width 2 /Height 2 /ColorSpace /DeviceRGB /BitsPerComponent 8 /Interpolate true",
			"\xff\x00\x00\x00\xff\x00\x00\x00\xff\xff\xff\x00"),
		stream("/Type /XObject /Subtype /Form /BBox [0 0 40 40]", "1 0 0 rg 20 20 30 0 360 0 0 0 c f 0 g 5 5 m 35 5 l 20 35 l h f"),
		axial,
	},
}

func TestDeterministic(t *testing.T) {
	t.Parallel()
	a, _ := draw(t, busyPage)
	b, _ := draw(t, busyPage)
	if string(a.Pix) != string(b.Pix) {
		t.Error("two renders of one page differ")
	}
}

func TestConcurrentRenders(t *testing.T) {
	t.Parallel()
	doc, pg := open(t, busyPage.bytes(), limits.Limits{})
	want, _, err := Render(context.Background(), doc, pg, Options{DPI: 72, Background: white})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, _, err := Render(context.Background(), doc, pg, Options{DPI: 72, Background: white})
			if err != nil {
				errs <- err
				return
			}
			if string(got.Image().Pix) != string(want.Image().Pix) {
				errs <- errors.New("a concurrent render differs")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func BenchmarkRenderBusyPage(b *testing.B) {
	doc, pg := open(b, busyPage.bytes(), limits.Limits{})
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := Render(context.Background(), doc, pg, Options{DPI: 300, Background: white}); err != nil {
			b.Fatal(err)
		}
	}
}
