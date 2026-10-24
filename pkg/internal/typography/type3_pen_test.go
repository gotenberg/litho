package typography

import (
	"context"
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// TestType3PenBox pins how a Type 3 glyph's box grows where it is shown by
// the pen of the showing state, for the strokes of its description that do
// not set their own line parameters (ISO 32000-1, 9.6.5): only the
// parameters a stroke inherits come from the showing state, a gs that sets
// one counts as setting it (8.4.5, Table 58), Q restores what the
// description set (8.4.2), the grown stroke is cut to its clip (8.5.4), and
// strokes past [maxType3PenStrokes] merge into one that bounds them all.
func TestType3PenBox(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	p := type3Painter{doc: &document.Document{Logger: document.NopLogger()}, resources: paintTestResources()}
	var many strings.Builder
	for i := range maxType3PenStrokes + 6 {
		many.WriteString(strconv.Itoa(i) + " 0 m " + strconv.Itoa(i+1) + " 0 l S ")
	}
	initial := graphics.State{LineWidth: 1, MiterLimit: initialMiterLimit}
	for _, tc := range []struct {
		name  string
		data  string
		state graphics.State
		noPen bool
		want  [4]float64
	}{
		{"inherited width", "0 500 m 1000 500 l S", graphics.State{LineWidth: 4000, MiterLimit: initialMiterLimit}, false, [4]float64{-2000, -1500, 3000, 2500}},
		{"initial state grows nothing", "0 500 m 1000 500 l S", initial, false, [4]float64{-0.5, 499.5, 1000.5, 500.5}},
		{"width set by the description", "4 w 0 0 m 10 0 l S", graphics.State{LineWidth: 4000}, false, [4]float64{-2, -2, 12, 2}},
		{"every parameter set", "4 w 0 J 1 j 10 M 0 0 m 10 0 l S", graphics.State{LineWidth: 4000, LineCap: 2}, true, [4]float64{-2, -2, 12, 2}},
		{"width set by gs", "/GS1 gs 0 0 m 10 0 l S", graphics.State{LineWidth: 4000, LineCap: 2}, false, [4]float64{-3 * math.Sqrt2, -3 * math.Sqrt2, 10 + 3*math.Sqrt2, 3 * math.Sqrt2}},
		{"Q restores the inherited width", "q 4 w Q 0 0 m 10 0 l S", graphics.State{LineWidth: 100, MiterLimit: initialMiterLimit}, false, [4]float64{-50, -50, 60, 50}},
		{"inherited square cap", "40 w 0 0 m 10 0 l S", graphics.State{LineWidth: 1, LineCap: 2, MiterLimit: initialMiterLimit}, false, [4]float64{-20 * math.Sqrt2, -20 * math.Sqrt2, 10 + 20*math.Sqrt2, 20 * math.Sqrt2}},
		{"inherited miter limit at a join", "4 w 0 j 0 0 m 10 0 l 10 10 l S", graphics.State{LineWidth: 1, MiterLimit: 30}, false, [4]float64{-60, -60, 70, 70}},
		{"stroke cut to its clip", "0 0 10 10 re W n 0 5 m 10 5 l S", graphics.State{LineWidth: 100, MiterLimit: initialMiterLimit}, false, [4]float64{0, 0, 10, 10}},
		{"pen through the description's cm", "q 2 0 0 2 0 0 cm 0 0 m 10 0 l S Q", graphics.State{LineWidth: 10, MiterLimit: initialMiterLimit}, false, [4]float64{-10, -10, 30, 10}},
		{"strokes past the cap merge", many.String(), graphics.State{LineWidth: 10, MiterLimit: initialMiterLimit}, false, [4]float64{-5, -5, maxType3PenStrokes + 11, 5}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ops, err := content.ParseStreamData(ctx, []byte(tc.data), content.SplitGluedOperators())
			if err != nil {
				t.Fatalf("ParseStreamData: %v", err)
			}
			got, unbounded, pen := type3PaintBounds(ctx, p, ops)
			if unbounded || got == nil {
				t.Fatalf("got %v unbounded %v, want a box", got, unbounded)
			}
			if (pen == nil) != tc.noPen {
				t.Fatalf("pen %v, want none: %v", pen, tc.noPen)
			}
			if pen != nil && len(pen.strokes) > maxType3PenStrokes {
				t.Fatalf("%d strokes recorded apart, want at most %d", len(pen.strokes), maxType3PenStrokes)
			}
			box := type3InkBounds{box: *got, pen: pen}.penBox(&tc.state)
			for i := range box {
				if math.Abs(box[i]-tc.want[i]) > 1e-9 {
					t.Fatalf("got %v, want %v", box, tc.want)
				}
			}
		})
	}
}

// TestType3PenStrokeMergeBoundsBoth checks a merged stroke reaches at least
// as far as either stroke it merges, under any showing state.
func TestType3PenStrokeMergeBoundsBoth(t *testing.T) {
	t.Parallel()
	a := newType3PenStroke([4]float64{0, 0, 10, 0}, &[4]float64{-100, -100, 5, 100}, type3PenState{lw: 8, lj: 1, set: penWidth | penJoin}, [6]float64{1, 0, 0, 1, 0, 0}, true)
	b := newType3PenStroke([4]float64{20, 0, 30, 0}, &[4]float64{15, -100, 100, 100}, type3PenState{lc: 2, ml: 4, set: penCap | penMiter}, [6]float64{3, 0, 0, 3, 0, 0}, false)
	clip := *a.clip
	m := a.merge(b)
	if *a.clip != clip {
		t.Fatal("merging grew the clip box of the stroke merged into")
	}
	for _, st := range []graphics.State{
		{LineWidth: 0, MiterLimit: 1},
		{LineWidth: 50, LineJoin: 0, MiterLimit: 10},
		{LineWidth: 2, LineJoin: 1, LineCap: 2, MiterLimit: math.NaN()},
	} {
		if m.reach(&st) < a.reach(&st) || m.reach(&st) < b.reach(&st) {
			t.Fatalf("state %+v: merged reach %v under %v or %v", st, m.reach(&st), a.reach(&st), b.reach(&st))
		}
	}
}
