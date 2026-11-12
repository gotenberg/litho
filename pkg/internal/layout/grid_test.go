package layout

import (
	"image/color"
	"runtime"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// Style modifiers and track sizes of grid layout, lengths in CSS pixels.

func gridBox(mods ...func(*css.Style)) *css.Style {
	return sty(append([]func(*css.Style){display(css.Grid)}, mods...)...)
}

func tpx(v float64) css.TrackSize {
	b := css.TrackBreadth{Kind: css.TrackLength, Value: px(v)}
	return css.TrackSize{Min: b, Max: b}
}

func tfr(v float64) css.TrackSize {
	return css.TrackSize{Max: css.TrackBreadth{Kind: css.TrackFlex, Value: v}}
}

func tauto() css.TrackSize { return css.TrackSize{} }

func tpct(v float64) css.TrackSize {
	b := css.TrackBreadth{Kind: css.TrackPercent, Value: v}
	return css.TrackSize{Min: b, Max: b}
}

func tminmax(lo, hi css.TrackBreadth) css.TrackSize { return css.TrackSize{Min: lo, Max: hi} }

func tracks(sizes ...css.TrackSize) css.TrackList {
	return css.TrackList{Sizes: sizes, Names: make([][]string, len(sizes)+1)}
}

func cols(sizes ...css.TrackSize) func(*css.Style) {
	return func(s *css.Style) { s.GridTemplateColumns = tracks(sizes...) }
}

func rowsT(sizes ...css.TrackSize) func(*css.Style) {
	return func(s *css.Style) { s.GridTemplateRows = tracks(sizes...) }
}

func gridAt(r0, r1, c0, c1 int) func(*css.Style) {
	return func(s *css.Style) {
		line := func(n int) css.GridLine {
			if n == 0 {
				return css.GridLine{}
			}
			return css.GridLine{N: n}
		}
		s.GridRowStart, s.GridRowEnd = line(r0), line(r1)
		s.GridColumnStart, s.GridColumnEnd = line(c0), line(c1)
	}
}

func spanCols(n int) func(*css.Style) {
	return func(s *css.Style) { s.GridColumnEnd = css.GridLine{Span: true, N: n} }
}

func flow(f css.GridAutoFlow) func(*css.Style) { return func(s *css.Style) { s.GridAutoFlow = f } }

func cell(mods ...func(*css.Style)) *Node { return el(sty(mods...)) }

func TestGrid_TrackSizing(t *testing.T) {
	tests := []struct {
		name  string
		cont  []func(*css.Style)
		items []*Node
		want  [][4]float64
	}{
		{
			"fixed and fr columns share the width",
			[]func(*css.Style){cols(tpx(20), tfr(1), tfr(3)), rowsT(tpx(10))},
			[]*Node{cell(), cell(), cell()},
			[][4]float64{{0, 0, 20, 10}, {20, 0, 20, 10}, {40, 0, 60, 10}},
		},
		{
			"gaps come off the free space",
			[]func(*css.Style){cols(tfr(1), tfr(1)), rowsT(tpx(10), tpx(10)), gaps(5, 10)},
			[]*Node{cell(), cell(), cell(), cell()},
			[][4]float64{{0, 0, 45, 10}, {55, 0, 45, 10}, {0, 15, 45, 10}, {55, 15, 45, 10}},
		},
		{
			"percentage columns resolve against the width",
			[]func(*css.Style){cols(tpct(0.25), tpct(0.5)), rowsT(tpx(10))},
			[]*Node{cell(), cell()},
			[][4]float64{{0, 0, 25, 10}, {25, 0, 50, 10}},
		},
		{
			"auto columns stretch over the free space",
			[]func(*css.Style){cols(tauto(), tpx(40))},
			[]*Node{cell(width(10), height(5)), cell(height(5))},
			[][4]float64{{0, 0, 10, 5}, {60, 0, 40, 5}},
		},
		{
			"auto rows take their tallest item's height",
			[]func(*css.Style){cols(tpx(50), tpx(50))},
			[]*Node{cell(height(30)), cell(height(10))},
			[][4]float64{{0, 0, 50, 30}, {50, 0, 50, 10}},
		},
		{
			"minmax() caps the growth",
			[]func(*css.Style){cols(tminmax(css.TrackBreadth{Kind: css.TrackLength, Value: px(10)}, css.TrackBreadth{Kind: css.TrackLength, Value: px(30)}), tpx(10)), rowsT(tpx(5))},
			[]*Node{cell(), cell()},
			[][4]float64{{0, 0, 30, 5}, {30, 0, 10, 5}},
		},
		{
			"a spanning item grows the tracks it spans",
			[]func(*css.Style){float(css.FloatLeft), cols(tauto(), tauto()), rowsT(tpx(5), tpx(5))},
			[]*Node{cell(spanCols(2), width(40)), cell(width(10)), cell(width(10))},
			[][4]float64{{0, 0, 40, 5}, {0, 5, 10, 5}, {20, 5, 10, 5}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := flexRects(t, doc(el(gridBox(tt.cont...), tt.items...)), 0, 0)
			if !eqRects(got, tt.want) {
				t.Errorf("rects = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGrid_Placement(t *testing.T) {
	areas := &css.GridAreas{Rows: 2, Cols: 2, Areas: []css.GridArea{
		{Name: "head", Row0: 0, Row1: 1, Col0: 0, Col1: 2},
		{Name: "main", Row0: 1, Row1: 2, Col0: 1, Col1: 2},
	}}
	named := func(n string) func(*css.Style) {
		return func(s *css.Style) {
			s.GridRowStart, s.GridColumnStart = css.GridLine{Name: n}, css.GridLine{Name: n}
			s.GridRowEnd, s.GridColumnEnd = css.GridLine{Name: n}, css.GridLine{Name: n}
		}
	}
	tests := []struct {
		name  string
		cont  []func(*css.Style)
		items []*Node
		want  [][4]float64
	}{
		{
			"auto placement fills rows",
			[]func(*css.Style){cols(tpx(10), tpx(10)), func(s *css.Style) { s.GridAutoRows = []css.TrackSize{tpx(5)} }},
			[]*Node{cell(), cell(), cell()},
			[][4]float64{{0, 0, 10, 5}, {10, 0, 10, 5}, {0, 5, 10, 5}},
		},
		{
			"definite items are placed first",
			[]func(*css.Style){cols(tpx(10), tpx(10)), func(s *css.Style) { s.GridAutoRows = []css.TrackSize{tpx(5)} }},
			[]*Node{cell(), cell(gridAt(1, 2, 1, 2))},
			[][4]float64{{10, 0, 10, 5}, {0, 0, 10, 5}},
		},
		{
			"negative lines count from the end",
			[]func(*css.Style){cols(tpx(10), tpx(10), tpx(10)), rowsT(tpx(5))},
			[]*Node{cell(func(s *css.Style) { s.GridColumnStart = css.GridLine{N: -2} })},
			[][4]float64{{20, 0, 10, 5}},
		},
		{
			"column flow fills columns",
			[]func(*css.Style){rowsT(tpx(5), tpx(5)), flow(css.GridFlowColumn), func(s *css.Style) { s.GridAutoColumns = []css.TrackSize{tpx(10)} }},
			[]*Node{cell(), cell(), cell()},
			[][4]float64{{0, 0, 10, 5}, {0, 5, 10, 5}, {10, 0, 10, 5}},
		},
		{
			"dense packing fills holes",
			[]func(*css.Style){cols(tpx(10), tpx(10)), flow(css.GridFlowDense), func(s *css.Style) { s.GridAutoRows = []css.TrackSize{tpx(5)} }},
			[]*Node{cell(), cell(spanCols(2)), cell()},
			[][4]float64{{0, 0, 10, 5}, {0, 5, 20, 5}, {10, 0, 10, 5}},
		},
		{
			"named areas place items",
			[]func(*css.Style){cols(tpx(10), tpx(20)), rowsT(tpx(5), tpx(5)), func(s *css.Style) { s.GridTemplateAreas = areas }},
			[]*Node{cell(named("main")), cell(named("head"))},
			[][4]float64{{10, 5, 20, 5}, {0, 0, 30, 5}},
		},
		{
			"order changes the auto placement",
			[]func(*css.Style){cols(tpx(10), tpx(10)), rowsT(tpx(5))},
			[]*Node{cell(order(2)), cell(order(1))},
			[][4]float64{{10, 0, 10, 5}, {0, 0, 10, 5}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := kid(t, boxes(t, doc(el(gridBox(tt.cont...), tt.items...)), page(100, 100)), 0, 0)
			got := make([][4]float64, 0, len(c.kids))
			// The kids are sorted in order-modified document order: map
			// them back to source order for the expectation.
			for _, n := range tt.items {
				for _, k := range c.kids {
					if k.node == n {
						got = append(got, rect(k))
					}
				}
			}
			if !eqRects(got, tt.want) {
				t.Errorf("rects = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGrid_AutoRepeat(t *testing.T) {
	rep := func(fit bool) func(*css.Style) {
		return func(s *css.Style) {
			s.GridTemplateColumns = css.TrackList{
				Names: make([][]string, 2),
				Auto: &css.TrackRepeat{Fit: fit, Names: make([][]string, 2), Sizes: []css.TrackSize{
					tminmax(css.TrackBreadth{Kind: css.TrackLength, Value: px(30)}, css.TrackBreadth{Kind: css.TrackFlex, Value: 1}),
				}},
			}
			s.GridTemplateRows = tracks(tpx(5))
		}
	}
	tests := []struct {
		name string
		fit  bool
		want [][4]float64
	}{
		{"auto-fill keeps the empty tracks", false, [][4]float64{{0, 0, 33.333, 5}}},
		{"auto-fit collapses the empty tracks", true, [][4]float64{{0, 0, 100, 5}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := flexRects(t, doc(el(gridBox(rep(tt.fit)), cell())), 0, 0)
			if !eqRects(got, tt.want) {
				t.Errorf("rects = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGrid_Alignment(t *testing.T) {
	tests := []struct {
		name  string
		cont  []func(*css.Style)
		items []*Node
		want  [][4]float64
	}{
		{
			"justify-self and align-self center",
			[]func(*css.Style){cols(tpx(40)), rowsT(tpx(20))},
			[]*Node{cell(width(10), height(10), func(s *css.Style) { s.JustifySelf, s.AlignSelf = css.BoxCenter, css.BoxCenter })},
			[][4]float64{{15, 5, 10, 10}},
		},
		{
			"justify-items: end applies to auto items",
			[]func(*css.Style){cols(tpx(40)), rowsT(tpx(20)), func(s *css.Style) { s.JustifyItems = css.BoxEnd }},
			[]*Node{cell(width(10), height(10))},
			[][4]float64{{30, 0, 10, 10}},
		},
		{
			"justify-content: space-between spreads the tracks",
			[]func(*css.Style){cols(tpx(10), tpx(10)), rowsT(tpx(5)), justify(css.BoxSpaceBetween)},
			[]*Node{cell(), cell()},
			[][4]float64{{0, 0, 10, 5}, {90, 0, 10, 5}},
		},
		{
			"auto margins center an item",
			[]func(*css.Style){cols(tpx(40)), rowsT(tpx(20))},
			[]*Node{cell(width(10), height(10), margin(auto))},
			[][4]float64{{15, 5, 10, 10}},
		},
		{
			"rtl puts the first column on the right",
			[]func(*css.Style){cols(tpx(10), tpx(20)), rowsT(tpx(5)), rtl},
			[]*Node{cell(), cell()},
			[][4]float64{{90, 0, 10, 5}, {70, 0, 20, 5}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := flexRects(t, doc(el(gridBox(tt.cont...), tt.items...)), 0, 0)
			if !eqRects(got, tt.want) {
				t.Errorf("rects = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGrid_AbsposArea(t *testing.T) {
	t.Parallel()
	abs := cell(func(s *css.Style) {
		s.Position = css.Absolute
		s.Top, s.Left, s.Bottom, s.Right = css.Pts(0), css.Pts(0), css.Pts(0), css.Pts(0)
	}, gridAt(2, 3, 2, 3))
	root := doc(el(gridBox(cols(tpx(10), tpx(20)), rowsT(tpx(5), tpx(15)), func(s *css.Style) { s.Position = css.Relative }), cell(), abs))
	c := kid(t, boxes(t, root, page(100, 100)), 0, 0)
	var got [4]float64
	for _, k := range c.kids {
		if k.abs {
			got = rect(k)
		}
	}
	if want := [4]float64{10, 5, 20, 15}; got != want {
		t.Errorf("abspos rect = %v, want %v", got, want)
	}
}

func TestGrid_IntrinsicWidth(t *testing.T) {
	t.Parallel()
	// A floated grid shrinks to its columns: a fixed one and an auto one
	// holding a 30px item.
	root := doc(el(gridBox(float(css.FloatLeft), cols(tpx(10), tauto()), rowsT(tpx(5))), cell(), cell(width(30))))
	c := kid(t, boxes(t, root, page(100, 100)), 0, 0)
	if got := rect(c); got[2] != 40 {
		t.Errorf("grid width = %v, want 40", got[2])
	}
}

func TestGrid_RowsMoveToNextPage(t *testing.T) {
	t.Parallel()
	// Rows of 40px on 100px pages: the third row starts at 80 and would
	// straddle the cut; its monolithic items move it to the next page.
	img := func() *Node {
		return &Node{Kind: Replaced, Style: sty(width(10), height(40)), Picture: pic(color.NRGBA{G: 255, A: 255}, 10, 40)}
	}
	root := doc(el(gridBox(cols(tpx(10), tpx(10))), img(), img(), img(), img(), img(), img()))
	c := kid(t, boxes(t, root, page(100, 100)), 0, 0)
	got := rect(c.kids[4])
	if got[1] != 100 {
		t.Errorf("third row item top = %v, want 100", got[1])
	}
	if h := rect(c)[3]; h != 140 {
		t.Errorf("grid height = %v, want 140", h)
	}
}

func TestGrid_Draw(t *testing.T) {
	t.Parallel()
	root := doc(el(gridBox(cols(tpx(2), tfr(1)), rowsT(tpx(2), tpx(2)), width(6), gaps(0, 0)),
		cell(bg(red)), cell(bg(blue)), cell(gridAt(2, 3, 1, 3), bg(lime))))
	assertPixels(t, run(t, root, page(8, 5)), 1, `
		RRBBBB__
		RRBBBB__
		GGGGGG__
		GGGGGG__
		________
	`)
}

func TestSizing_Keywords(t *testing.T) {
	inline := func(w float64) *Node {
		return el(sty(display(css.InlineBlock), width(w), height(10)))
	}
	kw := func(k css.DimKind) func(*css.Style) { return func(s *css.Style) { s.Width = css.Dimension{Kind: k} } }
	tests := []struct {
		name string
		mod  func(*css.Style)
		want float64
	}{
		{"min-content takes the widest piece", kw(css.MinContent), 30},
		{"max-content takes the whole line", kw(css.MaxContent), 60},
		{"fit-content fits the available width", kw(css.FitContent), 60},
		{"stretch fills the containing block", kw(css.Stretch), 100},
		{"min-width: max-content floors a fixed width", func(s *css.Style) {
			s.Width = css.Pts(px(10))
			s.MinWidth = css.Dimension{Kind: css.MaxContent}
		}, 60},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := doc(el(sty(tt.mod), inline(20), txt(sty(), " "), inline(30)))
			c := kid(t, boxes(t, root, page(100, 100)), 0, 0)
			if got := rect(c)[2]; got != tt.want {
				t.Errorf("width = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSizing_MinHeightContent(t *testing.T) {
	t.Parallel()
	root := doc(el(sty(height(0), func(s *css.Style) { s.MinHeight = css.Dimension{Kind: css.MaxContent} }), cell(height(25))))
	c := kid(t, boxes(t, root, page(100, 100)), 0, 0)
	if got := rect(c)[3]; got != 25 {
		t.Errorf("height = %v, want 25", got)
	}
}

// TestGrid_PlacementOccupancyStaysSmall locks the memory the placement
// takes to record the cells its items cover: items spanning a grid of
// MaxTracksPerAxis tracks per axis cover a million cells each, which the
// placement records in a few bits apiece. It runs alone, so the
// allocations it measures are its own.
func TestGrid_PlacementOccupancyStaysSmall(t *testing.T) {
	ts := make([]css.TrackSize, MaxTracksPerAxis)
	for i := range ts {
		ts[i] = tpx(1)
	}
	c := &box{st: gridBox(cols(ts...), rowsT(ts...))}
	items := make([]*box, 20)
	for i := range items {
		items[i] = &box{st: sty(gridAt(1, MaxTracksPerAxis+1, 1, MaxTracksPerAxis+1))}
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	p := placeGrid(c, items, [2]int{})
	runtime.ReadMemStats(&after)
	if got := after.TotalAlloc - before.TotalAlloc; got > 1<<20 {
		t.Errorf("placement allocated %d bytes, want at most 1 MiB", got)
	}
	if p.n != [2]int{MaxTracksPerAxis, MaxTracksPerAxis} {
		t.Errorf("grid = %v tracks, want %d by %d", p.n, MaxTracksPerAxis, MaxTracksPerAxis)
	}
}
