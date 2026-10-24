package typography

import (
	"context"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// paintTestResources names, as a Type 3 font's /Resources may, a form whose
// box is moved by its matrix, an image, and a shading with and without a
// box of its own.
func paintTestResources() ast.PdfDict {
	nums := func(v ...float64) ast.PdfArray {
		arr := make(ast.PdfArray, len(v))
		for i, x := range v {
			arr[i] = ast.PdfReal(x)
		}
		return arr
	}
	return ast.PdfDict{
		"XObject": ast.PdfDict{
			"Fm1": ast.NewStream(ast.PdfDict{"Subtype": ast.PdfName("Form"), "BBox": nums(0, 0, 2, 2), "Matrix": nums(2, 0, 0, 2, 1, 1)}, nil),
			"Fm2": ast.NewStream(ast.PdfDict{"Subtype": ast.PdfName("Form")}, nil),
			"Im1": ast.NewStream(ast.PdfDict{"Subtype": ast.PdfName("Image")}, nil),
		},
		"Shading": ast.PdfDict{
			"Sh1": ast.PdfDict{"ShadingType": ast.PdfInt(2)},
			"Sh2": ast.PdfDict{"ShadingType": ast.PdfInt(2), "BBox": nums(0, 0, 50, 60)},
		},
		"ExtGState": ast.PdfDict{
			"GS1": ast.PdfDict{"LW": ast.PdfInt(6), "LJ": ast.PdfInt(2)},
		},
	}
}

// TestType3PaintBounds pins the bounds a Type 3 glyph description paints in
// glyph space: through the CTM its cm operators set (ISO 32000-1, 8.4.4), an
// image as the unit square through that CTM (8.9.5), a form inside its box
// (8.10.1), a shading inside its box (8.7.4.3) or the clip (8.7.4.2), a
// stroke widened by the reach of its pen (half its line width, the miter
// limit times that at a miter join, √2 times it at a square cap, 8.4.3.3 to
// 8.4.3.5), its line parameters set by operators or a gs, every mark cut to
// the clip the description set (8.5.4), and nothing for a path that is only
// clipped or ended. A mark the walk cannot place over no clip reports it unbounded.
func TestType3PaintBounds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	p := type3Painter{doc: &document.Document{Logger: document.NopLogger()}, resources: paintTestResources()}
	for _, tc := range []struct {
		name      string
		data      string
		noRes     bool
		want      *[4]float64
		unbounded bool
	}{
		{"filled rectangle", "0 0 500 700 re f", false, &[4]float64{0, 0, 500, 700}, false},
		{"bitmap image under cm", "q 5 0 0 7 1 -2 cm BI /W 1 /H 1 /BPC 1 /IM true ID \x00 EI Q", false, &[4]float64{1, -2, 6, 5}, false},
		{"path under a scaling cm", "q 10 0 0 10 0 0 cm 0 0 m 1 2 l f Q 0 0 m 1 1 l f", false, &[4]float64{0, 0, 10, 20}, false},
		{"rotated curve", "q 0 1 -1 0 0 0 cm 0 0 m 0 10 10 10 10 0 c f Q", false, &[4]float64{-7.5, 0, 0, 10}, false},
		{"stroke widened by half its width", "4 w 0 0 m 10 0 l S", false, &[4]float64{-2, -2, 12, 2}, false},
		{"miter join reaches its limit", "4 w 3 M 0 0 m 10 0 l 10 10 l S", false, &[4]float64{-6, -6, 16, 16}, false},
		{"closed path joins", "4 w 3 M 0 0 m 10 0 l h S", false, &[4]float64{-6, -6, 16, 6}, false},
		{"round join reaches half its width", "4 w 1 j 0 0 m 10 0 l 10 10 l S", false, &[4]float64{-2, -2, 12, 12}, false},
		{"square cap", "4 w 2 J 0 0 m 10 0 l S", false, &[4]float64{-2 * math.Sqrt2, -2 * math.Sqrt2, 10 + 2*math.Sqrt2, 2 * math.Sqrt2}, false},
		{"line width set by gs", "/GS1 gs 0 0 m 10 0 l S", false, &[4]float64{-3, -3, 13, 3}, false},
		{"clip only", "0 0 10 10 re W n", false, nil, false},
		{"fill cut to the clip", "0 0 10 10 re W n 5 5 100 100 re f", false, &[4]float64{5, 5, 10, 10}, false},
		{"fill outside the clip", "0 0 10 10 re W n 50 50 1 1 re f", false, nil, false},
		{"Q restores the clip", "q 0 0 10 10 re W n Q 0 0 100 100 re f", false, &[4]float64{0, 0, 100, 100}, false},
		{"a clipping fill paints before it clips", "0 0 10 10 re W f 0 0 100 100 re f", false, &[4]float64{0, 0, 10, 10}, false},
		{"form inside its moved box", "q 3 0 0 3 0 0 cm /Fm1 Do Q", false, &[4]float64{3, 3, 15, 15}, false},
		{"image XObject", "q 3 0 0 3 0 0 cm /Im1 Do Q", false, &[4]float64{0, 0, 3, 3}, false},
		{"form with no box", "/Fm2 Do", false, nil, true},
		{"XObject the resources do not name", "/Fm9 Do", false, nil, true},
		{"form, the font naming no resources", "/Fm1 Do", true, nil, true},
		{"shading over no clip", "/Sh1 sh", false, nil, true},
		{"shading under a clip", "q 0 0 30 30 re W n /Sh1 sh Q", false, &[4]float64{0, 0, 30, 30}, false},
		{"shading inside its box", "/Sh2 sh", false, &[4]float64{0, 0, 50, 60}, false},
		{"shading box cut to the clip", "0 0 30 30 re W n /Sh2 sh", false, &[4]float64{0, 0, 30, 30}, false},
		{"text in the font showing the glyph", "BT (a) Tj ET", false, nil, true},
		{"text in a font the resources do not name", "BT /F9 1 Tf (a) Tj ET", false, nil, true},
		{"text under a clip", "0 0 7 7 re W n BT /F9 1 Tf (a) Tj ET", false, &[4]float64{0, 0, 7, 7}, false},
		{"an empty string shows nothing", "BT () Tj ET", false, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ops, err := content.ParseStreamData(ctx, []byte(tc.data), content.SplitGluedOperators())
			if err != nil {
				t.Fatalf("ParseStreamData: %v", err)
			}
			painter := p
			if tc.noRes {
				painter.resources = nil
			}
			got, unbounded := type3PaintBounds(ctx, painter, ops)
			if unbounded != tc.unbounded {
				t.Errorf("unbounded = %v, want %v", unbounded, tc.unbounded)
			}
			if tc.unbounded {
				return
			}
			if (got == nil) != (tc.want == nil) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			if got == nil {
				return
			}
			for i := range got {
				if math.Abs(got[i]-tc.want[i]) > 1e-9 {
					t.Fatalf("got %v, want %v", *got, *tc.want)
				}
			}
		})
	}
}

// TestType3GlyphInkBounds pins how a Type 3 glyph's ink combines what its
// description paints with the box its d1 states (ISO 32000-1, 9.6.5), and
// that a description painting past every box is unbounded.
func TestType3GlyphInkBounds(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		g    type3Glyph
		want type3InkBounds
	}{
		{"paint and d1 box", type3Glyph{paintBox: &[4]float64{0, 0, 500, 700}, d1Box: &[4]float64{-5, 0, 400, 750}}, type3InkBounds{box: [4]float64{-5, 0, 500, 750}}},
		{"paint past the d1 box", type3Glyph{paintBox: &[4]float64{0, 0, 3000, 3000}, d1Box: &[4]float64{0, 0, 10, 10}}, type3InkBounds{box: [4]float64{0, 0, 3000, 3000}}},
		{"degenerate d1 box", type3Glyph{paintBox: &[4]float64{0, 0, 500, 700}, d1Box: &[4]float64{0, 0, 0, 0}}, type3InkBounds{box: [4]float64{0, 0, 500, 700}}},
		{"paints nothing", type3Glyph{}, type3InkBounds{}},
		{"unbounded mark", type3Glyph{paintBox: &[4]float64{0, 0, 1, 1}, paintUnbounded: true}, type3InkBounds{unbounded: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := type3GlyphInkBounds(tc.g); got != tc.want {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestStrokeReach pins how far past its path a pen reaches (ISO 32000-1,
// 8.4.3.2 to 8.4.3.5): half the line width through the CTM's largest
// stretch, the miter limit times that at a miter join when asked for,
// √2 times it at a square cap, and nothing for a zero or non-finite width.
func TestStrokeReach(t *testing.T) {
	t.Parallel()
	identity := [6]float64{1, 0, 0, 1, 0, 0}
	for _, tc := range []struct {
		name   string
		lw     float64
		join   int
		cap    int
		ml     float64
		m      [6]float64
		miters bool
		want   float64
	}{
		{"round join", 4, 1, 0, 10, identity, true, 2},
		{"bevel join", 4, 2, 0, 10, identity, true, 2},
		{"miter join", 4, 0, 0, 10, identity, true, 20},
		{"miter join, its tips not asked for", 4, 0, 0, 10, identity, false, 2},
		{"miter limit below one", 4, 0, 0, 0.5, identity, true, 2},
		{"miter limit not a number", 4, 0, 0, math.NaN(), identity, true, 20},
		{"join outside Table 55", 4, 7, 0, 3, identity, true, 6},
		{"square cap", 4, 1, 2, 10, identity, true, 2 * math.Sqrt2},
		{"negative width", -4, 1, 0, 10, identity, true, 2},
		{"CTM stretch", 4, 1, 0, 10, [6]float64{3, 0, 0, 0.5, 9, 9}, true, 6},
		{"zero width", 0, 0, 0, 10, identity, true, 0},
		{"infinite width", math.Inf(1), 1, 0, 10, identity, true, 0},
	} {
		if got := strokeReach(tc.lw, tc.join, tc.cap, tc.ml, tc.m, tc.miters); math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestGlyphInkBoxHoldsTheStroke checks a glyph a stroking text rendering
// mode draws (ISO 32000-1, 9.3.6, Table 106) is bounded by half its pen
// under a round or a miter join alike, and one a filling or invisible mode
// draws is not.
func TestGlyphInkBoxHoldsTheStroke(t *testing.T) {
	t.Parallel()
	font := &Font{Subtype: "Type1", FirstChar: 65, LastChar: 65, Widths: []float64{500}}
	for _, join := range []int{0, 1} {
		for mode, grows := range map[int]bool{0: false, 1: true, 2: true, 3: false, 4: false, 5: true, 6: true, 7: false, 9: false} {
			machine := graphics.NewMachine([4]float64{0, 0, 612, 792})
			state := machine.Current()
			state.Text.Tmode = mode
			state.LineWidth = 2
			state.LineJoin = join
			got := glyphInkBox(context.Background(), machine, font, 10, 65, 0, 0)
			want := [4]float64{0, -2, 5, 10}
			if grows {
				want = [4]float64{-1, -3, 6, 11}
			}
			if got != want {
				t.Errorf("join %d, mode %d: got %v, want %v", join, mode, got, want)
			}
		}
	}
}
