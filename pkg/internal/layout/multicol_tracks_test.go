package layout

import (
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// colTracks returns a multi-column container style whose columns take the
// widths and gaps of ts, gaps in px, filled in turn 4 px tall.
func colTracks(ts ...css.ColumnTrack) func(*css.Style) {
	for i := range ts {
		ts[i].Gap = px(ts[i].Gap)
	}
	return func(s *css.Style) {
		s.Columns.Tracks = ts
		s.Columns.Fill = css.FillAuto
		s.Height = css.Pts(px(4))
	}
}

// TestMulticol_Tracks lays columns of different widths out: each its
// share of the room the gaps leave, the text breaking its lines at the
// width of the column it lands in, the rule in the middle of each gap,
// the columns from the end edge in a right-to-left container.
func TestMulticol_Tracks(t *testing.T) {
	t.Parallel()
	text := func(mods ...func(*css.Style)) *Node {
		st := sty(append([]func(*css.Style){font(2), fg(red)}, mods...)...)
		return el(st, txt(st, "AAAAA BB CC DD EE"))
	}
	two := colTracks(css.ColumnTrack{Width: 2, Gap: 2}, css.ColumnTrack{Width: 1, Gap: 5})
	rule := func(s *css.Style) {
		s.Columns.RuleWidth, s.Columns.RuleStyle, s.Columns.RuleColor = px(2), css.Solid, blue
	}
	rtl := func(s *css.Style) { s.Direction = css.RTL; s.TextAlign = css.AlignLeft }
	tests := []struct {
		name string
		root *Node
		want string
	}{
		{"two to one", doc(el(sty(two), text())), `
			RRRRRRRRRR____RRRR__
			RRRRRRRRRR____RRRR__
			RRRR__RRRR____RRRR__
			RRRR__RRRR____RRRR__
			____________________`},
		{"rule in the gap", doc(el(sty(two, rule), text())), `
			RRRRRRRRRR__BBRRRR__
			RRRRRRRRRR__BBRRRR__
			RRRR__RRRR__BBRRRR__
			RRRR__RRRR__BBRRRR__
			____________________`},
		{"right to left", doc(el(sty(two, rtl), text(rtl))), `
			RRRR____RRRRRRRRRR__
			RRRR____RRRRRRRRRR__
			RRRR____RRRR__RRRR__
			RRRR____RRRR__RRRR__
			____________________`},
		{"an invalid track lays equal columns", doc(el(sty(colTracks(css.ColumnTrack{Width: 2, Gap: 1}, css.ColumnTrack{Width: math.NaN()}), func(s *css.Style) { s.ColumnGap = css.Pts(0) }), text())), `
			RRRRRRRRRRRRRR__RRRR
			RRRRRRRRRRRRRR__RRRR
			RRRR__RRRR__________
			RRRR__RRRR__________
			____________________`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assertPixels(t, run(t, tt.root, page(20, 5)), 1, tt.want)
		})
	}
}

func TestColumnTracks(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		tracks []css.ColumnTrack
		u      float64
		xs, ws []float64
	}{
		{"none", nil, 100, nil, nil},
		{"shares the room the gaps leave", []css.ColumnTrack{{Width: 3, Gap: 10}, {Width: 1, Gap: 10}, {Width: 1, Gap: 99}}, 120, []float64{0, 70, 100}, []float64{60, 20, 20}},
		{"a negative gap", []css.ColumnTrack{{Width: 1, Gap: -1}, {Width: 1}}, 100, nil, nil},
		{"a zero width", []css.ColumnTrack{{Width: 0}, {Width: 1}}, 100, nil, nil},
		{"an infinite width", []css.ColumnTrack{{Width: math.Inf(1)}}, 100, nil, nil},
		{"gaps wider than the box", []css.ColumnTrack{{Width: 1, Gap: 200}, {Width: 1}}, 100, []float64{0, 200}, []float64{0, 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			xs, ws, _ := columnTracks(tt.tracks, tt.u)
			if len(xs) != len(tt.xs) || len(ws) != len(tt.ws) {
				t.Fatalf("xs %v ws %v, want %v %v", xs, ws, tt.xs, tt.ws)
			}
			for i := range xs {
				if !near(xs[i], tt.xs[i]) || !near(ws[i], tt.ws[i]) {
					t.Errorf("xs %v ws %v, want %v %v", xs, ws, tt.xs, tt.ws)
				}
			}
		})
	}
}
