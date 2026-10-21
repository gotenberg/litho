package page

import (
	"bytes"
	"context"
	"maps"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/render/annot"
)

// square writes a /Square annotation over rect with flags f, its appearance
// object ap.
func square(rect string, f, ap int) string {
	return "<< /Type /Annot /Subtype /Square /Rect [" + rect + "] /F " + strconv.Itoa(f) + " /AP << /N " + strconv.Itoa(ap) + " 0 R >> >>"
}

// annotPopplerFixtures join popplerFixtures: annotation flags under the view
// pdftoppm draws, an appearance matrix (Algorithm 8.1), optional content,
// and /NoRotate on a turned page.
var annotPopplerFixtures = map[string]fixture{
	// Objects 5, 6, 7: a printed annotation, a printed one hidden on
	// screen (/NoView), one not printed; 8, 9, 10 their red, green, blue
	// appearances.
	"annot_flags": {
		page:    "/Annots [5 0 R 6 0 R 7 0 R]",
		content: "0.8 g 0 0 100 100 re f",
		objs: []string{
			square("10 10 30 30", annot.FlagPrint, 8),
			square("40 40 60 60", annot.FlagPrint|annot.FlagNoView, 9),
			square("70 70 90 90", 0, 10),
			stream("/Type /XObject /Subtype /Form /BBox [0 0 20 20]", "1 0 0 rg 0 0 20 20 re f"),
			stream("/Type /XObject /Subtype /Form /BBox [0 0 20 20]", "0 1 0 rg 0 0 20 20 re f"),
			stream("/Type /XObject /Subtype /Form /BBox [0 0 20 20]", "0 0 1 rg 0 0 20 20 re f"),
		},
	},
	// An appearance turned a quarter by its /Matrix and stretched onto a
	// wide /Rect, clipped to its /BBox; object 7 is hidden by an OFF group.
	"annot_matrix": {
		catalog: "/OCProperties << /OCGs [8 0 R] /D << /OFF [8 0 R] >> >>",
		page:    "/Annots [5 0 R 7 0 R]",
		objs: []string{
			"<< /Type /Annot /Subtype /Stamp /Rect [10 20 90 60] /F 4 /AP << /N 6 0 R >> >>",
			stream("/Type /XObject /Subtype /Form /BBox [0 0 20 40] /Matrix [0 1 -1 0 0 0]",
				"1 0 0 rg -10 -10 15 60 re f 0 0 1 rg 15 0 5 40 re f"),
			"<< /Type /Annot /Subtype /Square /Rect [0 0 100 100] /F 4 /OC 8 0 R /AP << /N 9 0 R >> >>",
			"<< /Type /OCG /Name (off) >>",
			stream("/Type /XObject /Subtype /Form /BBox [0 0 100 100]", "0 g 0 0 100 100 re f"),
		},
	},
	// /Rotate 90 with a /NoRotate annotation (object 5) beside a plain one
	// (6), each an arrow pointing up its appearance.
	"annot_norotate": {
		page: "/MediaBox [0 0 120 80] /Rotate 90 /Annots [5 0 R 6 0 R]",
		objs: []string{
			"<< /Type /Annot /Subtype /Text /Rect [10 40 30 70] /F 28 /AP << /N 7 0 R >> >>",
			"<< /Type /Annot /Subtype /Square /Rect [60 40 80 70] /F 4 /AP << /N 7 0 R >> >>",
			stream("/Type /XObject /Subtype /Form /BBox [0 0 20 30]",
				"1 0 0 rg 0 0 20 15 re f 0 0 1 rg 0 15 m 20 15 l 10 30 l h f"),
		},
	},
}

// synthPopplerFixtures join popplerFixtures: annotations with no appearance
// stream, drawn from their dictionaries (annot.Options.Synthesize) as
// poppler builds them. Each stays clear of the places litho reads ISO
// 32000-1 where poppler does not: /RD, a transparent /C, a grey or CMYK
// Link /C and /LLO are checked by hand in TestSynthesizedAnnotations.
var synthPopplerFixtures = map[string]fixture{
	// A filled square with a 4-unit border, a half-opaque circle over a
	// blue bar, a dashed CMYK square and a circle filled in CMYK cyan
	// with no /C (a black border one unit wide).
	"annot_synth_geometry": {
		page:    "/Annots [5 0 R 6 0 R 7 0 R 8 0 R]",
		content: "0 0 1 rg 60 50 10 50 re f",
		objs: []string{
			"<< /Type /Annot /Subtype /Square /Rect [5 55 45 95] /F 4 /C [1 0 0] /IC [0 0 1] /BS << /W 4 >> >>",
			"<< /Type /Annot /Subtype /Circle /Rect [55 55 95 95] /F 4 /C [0 0.5 0] /IC [1 1 0] /Border [0 0 3] /CA 0.5 >>",
			"<< /Type /Annot /Subtype /Square /Rect [5 5 45 45] /F 4 /C [0 0 0 1] /BS << /W 2 /S /D /D [4 2] >> >>",
			"<< /Type /Annot /Subtype /Circle /Rect [55 5 95 45] /F 4 /IC [1 0 0 0] >>",
		},
	},
	// Lines with endings and leader lines, a polyline with endings and a
	// filled polygon.
	"annot_synth_lines": {
		page: "/Annots [5 0 R 6 0 R 7 0 R 8 0 R]",
		objs: []string{
			"<< /Type /Annot /Subtype /Line /Rect [5 70 95 90] /F 4 /L [10 80 90 80] /LE [/ClosedArrow /Circle] " +
				"/IC [1 0 0] /C [0 0 1] /BS << /W 2 >> >>",
			"<< /Type /Annot /Subtype /Line /Rect [5 45 95 70] /F 4 /L [10 50 90 55] /LL 8 /LLE 3 /LE [/Square /OpenArrow] >>",
			"<< /Type /Annot /Subtype /PolyLine /Rect [5 5 75 40] /F 4 /Vertices [10 10 30 35 50 15 70 35] " +
				"/LE [/Diamond /RClosedArrow] /C [0 0.6 0] /IC [1 1 0] /BS << /W 1.5 >> >>",
			"<< /Type /Annot /Subtype /Polygon /Rect [70 5 100 45] /F 4 /Vertices [75 10 95 10 85 40] /C [1 0 0] /IC [0 0 1] /Border [0 0 2] >>",
		},
	},
	// Ink, the four text markup types (a highlight multiplied over a cyan
	// bar) and Link borders: solid, dashed, width 0 and underlined.
	"annot_synth_markup": {
		page:    "/Annots [5 0 R 6 0 R 7 0 R 8 0 R 9 0 R 10 0 R 11 0 R 12 0 R 13 0 R]",
		content: "0 1 1 rg 50 80 45 10 re f",
		objs: []string{
			"<< /Type /Annot /Subtype /Ink /Rect [5 55 45 95] /F 4 /InkList [[10 90 20 70 30 90 40 70] [10 60 40 60]] /C [1 0 0] /BS << /W 3 >> >>",
			"<< /Type /Annot /Subtype /Highlight /Rect [50 80 95 90] /F 4 /QuadPoints [50 90 95 90 50 80 95 80] /C [1 1 0] >>",
			"<< /Type /Annot /Subtype /Underline /Rect [50 62 95 70] /F 4 /QuadPoints [50 70 95 70 50 62 95 62] /C [1 0 0] >>",
			"<< /Type /Annot /Subtype /StrikeOut /Rect [50 45 95 55] /F 4 /QuadPoints [50 55 95 55 50 45 95 45] /C [0 0 1] >>",
			"<< /Type /Annot /Subtype /Squiggly /Rect [50 28 95 40] /F 4 /QuadPoints [50 40 95 40 50 28 95 28] /C [0 0.5 0] >>",
			"<< /Type /Annot /Subtype /Link /Rect [5 5 45 25] /Border [0 0 2] /C [1 0 0] >>",
			"<< /Type /Annot /Subtype /Link /Rect [50 5 95 20] /BS << /W 1 /S /D /D [3 2] >> /C [0 0 1] >>",
			"<< /Type /Annot /Subtype /Link /Rect [5 30 45 40] /Border [0 0 0] /C [0 1 0] >>",
			"<< /Type /Annot /Subtype /Link /Rect [5 42 45 50] /BS << /S /U /W 2 >> /C [1 0 1] >>",
		},
	},
}

func init() {
	maps.Copy(popplerFixtures, annotPopplerFixtures)
	maps.Copy(popplerFixtures, synthPopplerFixtures)
}

func drawMode(t *testing.T, f fixture, mode annot.Mode) (*Report, map[[2]int]rgb) {
	t.Helper()
	doc, pg := open(t, f.bytes(), limits.Limits{})
	c, report, err := Render(context.Background(), doc, pg, Options{DPI: 72, Background: white, Annotations: mode})
	if err != nil {
		t.Fatal(err)
	}
	img := c.Image()
	out := map[[2]int]rgb{}
	for _, p := range [][2]int{{20, 80}, {50, 50}, {80, 20}} {
		out[p] = at(t, img, p[0], p[1])
	}
	return &report, out
}

func TestAnnotationModes(t *testing.T) {
	t.Parallel()
	grey := rgb{204, 204, 204}
	green := rgb{0, 255, 0}
	for _, tc := range []struct {
		mode annot.Mode
		want map[[2]int]rgb
	}{
		{annot.View, map[[2]int]rgb{{20, 80}: red, {50, 50}: grey, {80, 20}: blue}},
		{annot.Print, map[[2]int]rgb{{20, 80}: red, {50, 50}: green, {80, 20}: grey}},
	} {
		_, got := drawMode(t, annotPopplerFixtures["annot_flags"], tc.mode)
		for p, w := range tc.want {
			if !close8(got[p], w, 2) {
				t.Errorf("mode %d: pixel %v = %v, want %v", tc.mode, p, got[p], w)
			}
		}
	}
}

func TestAnnotationMatrix(t *testing.T) {
	t.Parallel()
	img, _ := draw(t, annotPopplerFixtures["annot_matrix"])
	// The /Matrix turns form (x, y) to (-y, x), and Algorithm 8.1 maps the
	// turned box onto the /Rect at twice its size: form x is user y 20 + 2x.
	// The blue strip (form x 15 to 20) lies at user y 50 to 60, the red one
	// cut by the /BBox (form x 0 to 5) at user y 20 to 30, nothing between;
	// the annotation an OFF group hides draws nothing.
	expect(t, img, 8, map[[2]int]rgb{{50, 45}: blue, {50, 75}: red, {50, 60}: paper, {5, 45}: paper})
}

func TestAnnotationNoRotate(t *testing.T) {
	t.Parallel()
	img, _ := draw(t, annotPopplerFixtures["annot_norotate"])
	// /Rotate 90 maps user (x, y) to device (y, x) on an 80 by 120 raster.
	// The plain annotation turns with the page: its red base (user y 40 to
	// 55) lies at device x 40 to 55. The /NoRotate one stays upright with
	// its upper-left corner, user (10, 70), at device (70, 10): its red base
	// spans device x 70 to 90 (clipped at 80), y 25 to 40.
	expect(t, img, 8, map[[2]int]rgb{
		{45, 70}: red, {60, 70}: blue,
		{75, 35}: red, {77, 20}: blue,
	})
}

func TestAnnotationWithoutAppearance(t *testing.T) {
	t.Parallel()
	// A text annotation's icon is the viewer's (ISO 32000-1, 12.5.6.4):
	// with no appearance stream nothing is drawn, and it is counted. The
	// square beside it is drawn from its dictionary and is not.
	img, report := draw(t, fixture{
		page: "/Annots [5 0 R 6 0 R]",
		objs: []string{
			"<< /Type /Annot /Subtype /Text /Rect [60 60 80 80] /F 4 /C [1 0 0] >>",
			"<< /Type /Annot /Subtype /Square /Rect [10 10 30 30] /F 4 /IC [1 0 0] /Border [0 0 0] >>",
		},
	})
	if report.Unsupported[UnsupportedAnnotation] != 1 {
		t.Errorf("report %v, want one annotation without appearance", report.Unsupported)
	}
	expect(t, img, 2, map[[2]int]rgb{{20, 80}: red, {70, 30}: paper})
}

// TestSynthesizedAnnotations checks, by hand, the readings of ISO 32000-1
// the poppler fixtures avoid because poppler departs from them.
func TestSynthesizedAnnotations(t *testing.T) {
	t.Parallel()
	img, _ := draw(t, fixture{
		page: "/Annots [5 0 R 6 0 R 7 0 R 8 0 R]",
		objs: []string{
			// /RD [left top right bottom] insets the square to x 30 to 50,
			// y 60 to 90 (12.5.6.8); poppler ignores /RD.
			"<< /Type /Annot /Subtype /Square /Rect [10 50 60 95] /F 4 /C [] /IC [0 0 1] /BS << /W 4 >> /RD [20 5 10 10] >>",
			// A transparent /C strokes nothing (Table 164); poppler
			// strokes black.
			"<< /Type /Annot /Subtype /Square /Rect [70 50 95 95] /F 4 /C [] /IC [1 0 0] /BS << /W 4 >> >>",
			// A grey /C (Table 164); poppler strokes a grey Link black.
			"<< /Type /Annot /Subtype /Link /Rect [5 5 45 25] /Border [0 0 4] /C [0.5] >>",
			// Leader lines start /LLO off the points (Table 175): at
			// x 60 from y 15 up to y 30.
			"<< /Type /Annot /Subtype /Line /Rect [55 5 95 45] /F 4 /L [60 10 90 10] /LL 15 /LLO 5 /BS << /W 2 >> >>",
		},
	})
	grey := rgb{128, 128, 128}
	expect(t, img, 2, map[[2]int]rgb{
		{20, 20}: paper, {40, 20}: blue, {40, 45}: paper,
		{71, 20}: red,
		{5, 85}:  grey, {25, 85}: paper,
		{60, 88}: paper, {60, 78}: black, {75, 75}: black,
	})
}

func TestAnnotationOverUnreadableContent(t *testing.T) {
	t.Parallel()
	// /Contents naming a dictionary, no stream: the page content does not
	// parse, and its annotations still draw.
	data := bytes.Replace(annotPopplerFixtures["annot_flags"].bytes(), []byte("/Contents 4 0 R"), []byte("/Contents 5 0 R"), 1)
	doc, pg := open(t, data, limits.Limits{})
	c, report, err := Render(context.Background(), doc, pg, Options{DPI: 72, Background: white})
	if err != nil {
		t.Fatal(err)
	}
	if report.Unsupported[Skipped] != 1 {
		t.Fatalf("report %v, want the content skipped", report.Unsupported)
	}
	expect(t, c.Image(), 2, map[[2]int]rgb{{20, 80}: red, {50, 20}: paper})
}

func TestAnnotationNeedAppearances(t *testing.T) {
	t.Parallel()
	// ISO 32000-1, 12.7.2: /NeedAppearances true marks the widget's stream
	// stale; litho builds none, so it draws the stored stream and counts it.
	img, report := draw(t, fixture{
		catalog: "/AcroForm << /Fields [5 0 R] /NeedAppearances true >>",
		page:    "/Annots [5 0 R]",
		objs: []string{
			"<< /Type /Annot /Subtype /Widget /FT /Tx /T (f) /Rect [10 10 30 30] /F 4 /AP << /N 6 0 R >> >>",
			stream("/Type /XObject /Subtype /Form /BBox [0 0 20 20]", "0 g 0 0 20 20 re f"),
		},
	})
	if report.Unsupported[StaleAppearance] != 1 {
		t.Errorf("report %v, want the stale widget counted", report.Unsupported)
	}
	expect(t, img, 0, map[[2]int]rgb{{20, 80}: {0, 0, 0}})
}
