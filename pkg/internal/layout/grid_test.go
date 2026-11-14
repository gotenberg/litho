package layout

import (
	"context"
	"errors"
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	p, _ := placeGrid(t.Context(), c, items, [2]int{})
	runtime.ReadMemStats(&after)
	if got := after.TotalAlloc - before.TotalAlloc; got > 1<<20 {
		t.Errorf("placement allocated %d bytes, want at most 1 MiB", got)
	}
	if p.n != [2]int{MaxTracksPerAxis, MaxTracksPerAxis} {
		t.Errorf("grid = %v tracks, want %d by %d", p.n, MaxTracksPerAxis, MaxTracksPerAxis)
	}
}

// fullGrid returns a dense grid container of MaxTracksPerAxis columns and
// rows of 1px.
func fullGrid() *box {
	ts := make([]css.TrackSize, MaxTracksPerAxis)
	for i := range ts {
		ts[i] = tpx(1)
	}
	return &box{st: gridBox(cols(ts...), rowsT(ts...), flow(css.GridFlowDense))}
}

// checkerItems returns items taking every other cell of every other row
// of a full grid, then n auto-placed items of column spans from 2 to 1000,
// which fit nowhere among them.
func checkerItems(n int) []*box {
	var items []*box
	for r := 1; r <= MaxTracksPerAxis; r += 2 {
		for c := 1; c <= MaxTracksPerAxis; c += 2 {
			items = append(items, &box{st: sty(gridAt(r, r+1, c, c+1))})
		}
	}
	for k := range n {
		span := k%(MaxTracksPerAxis-1) + 2
		items = append(items, &box{st: sty(func(s *css.Style) { s.GridColumnStart = css.GridLine{Span: true, N: span} })})
	}
	return items
}

// checkPlaced fails t unless every item of p has an area inside the
// implicit grid's bounds.
func checkPlaced(t *testing.T, p *gridPlacement) {
	t.Helper()
	for i, it := range p.items {
		for ax := range 2 {
			if it.start[ax] < 0 || it.end[ax] <= it.start[ax] || it.end[ax] > MaxTracksPerAxis {
				t.Fatalf("item %d axis %d at [%d, %d)", i, ax, it.start[ax], it.end[ax])
			}
		}
	}
}

// TestGrid_DensePlacementResumes locks the cost of dense auto-placement:
// items of one span resume the search where the previous one stopped, so
// a hundred thousand of them fill the grid in order, well inside the
// placement's work budget, where a search from the start for each would
// test about 10^8 words.
func TestGrid_DensePlacementResumes(t *testing.T) {
	t.Parallel()
	items := make([]*box, 100_000)
	for i := range items {
		items[i] = &box{st: sty()}
	}
	p, err := placeGrid(t.Context(), fullGrid(), items, [2]int{})
	if err != nil || p.overBudget {
		t.Fatalf("placement: err %v, over budget %v", err, p.overBudget)
	}
	for i, it := range p.items {
		if r, c := it.start[rowAxis], it.start[colAxis]; r != i/MaxTracksPerAxis || c != i%MaxTracksPerAxis {
			t.Fatalf("item %d at row %d column %d", i, r, c)
		}
	}
}

// TestGrid_PlacementBudget checks that a dense placement whose searches
// fit nowhere stops at its work budget, places every item, and gives the
// rest a track of their own after the grid's.
func TestGrid_PlacementBudget(t *testing.T) {
	t.Parallel()
	items := checkerItems(3000)
	p, err := placeGrid(t.Context(), fullGrid(), items, [2]int{})
	if err != nil || !p.overBudget {
		t.Fatalf("placement: err %v, over budget %v, want over budget", err, p.overBudget)
	}
	checkPlaced(t, p)
	if last := p.items[len(p.items)-1]; last.start[colAxis] != 0 {
		t.Errorf("last item at column %d, want 0", last.start[colAxis])
	}
}

// TestGrid_PlacementStopsOnCancel checks that a cancelled context stops
// the auto-placement search and returns its error with every item placed.
func TestGrid_PlacementStopsOnCancel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	items := make([]*box, 100_000)
	for i := range items {
		items[i] = &box{st: sty()}
	}
	p, err := placeGrid(ctx, fullGrid(), items, [2]int{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("placement on a cancelled context = %v, want context.Canceled", err)
	}
	checkPlaced(t, p)
}

// TestSizing_StretchHeightMargins holds a block-axis stretch to CSS
// Sizing 3, 4.2: a margin that would collapse with the parent's, the
// parent having no border or padding on that side and being no
// independent formatting context, counts as 0; else it comes off.
func TestSizing_StretchHeightMargins(t *testing.T) {
	t.Parallel()
	stretchH := func(s *css.Style) { s.Height = css.Dimension{Kind: css.Stretch} }
	tests := []struct {
		name   string
		parent []func(*css.Style)
		want   float64
	}{
		{"a margin that would collapse counts as 0", []func(*css.Style){height(100)}, 100},
		{"padding keeps the margin", []func(*css.Style){height(100), padding(0, 0, 1, 0)}, 50},
		{"a new formatting context keeps the margin", []func(*css.Style){height(100), display(css.FlowRoot)}, 50},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			root := doc(el(sty(tt.parent...), el(sty(stretchH, margin(0, 0, 50, 0)))))
			c := kid(t, boxes(t, root, page(200, 200)), 0, 0, 0)
			if got := rect(c)[3]; got != tt.want {
				t.Errorf("height = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestGrid_SpecifiedSizeSuggestion holds the automatic minimum size of a
// grid item to CSS Grid 1, 6.6: its content-based minimum is its
// specified size suggestion, a fixed width, even past its content, so a
// 0fr column keeps the width of its empty item.
func TestGrid_SpecifiedSizeSuggestion(t *testing.T) {
	t.Parallel()
	got := flexRects(t, doc(el(gridBox(width(100), cols(tfr(0), tfr(1)), rowsT(tpx(10))), cell(width(50)), cell())), 0, 0)
	if want := [][4]float64{{0, 0, 50, 10}, {50, 0, 50, 10}}; !eqRects(got, want) {
		t.Errorf("rects = %v, want %v", got, want)
	}
}

// TestGrid_RowMovesPastCollapsedWhiteSpace moves a grid row to the next
// column when the first content of its item, after an anonymous block of
// collapsed white space, starts there: the item's first child keeps its
// top margin at the item's content edge, where no break truncates it (CSS
// Fragmentation 3, 5.2), as Chromium draws it.
//
// # Corpus evidence
//
//   - wpt, css/css-multicol/multicol-span-all-004.html: Chromium draws the
//     h3 of the grid item at the top of the third column below its margin.
func TestGrid_RowMovesPastCollapsedWhiteSpace(t *testing.T) {
	t.Parallel()
	auto := func(s *css.Style) { s.Columns.Fill = css.FillAuto }
	item := el(sty(), txt(inlineSty(), " "), el(sty(margin(2, 0, 0, 0), width(2), height(1), bg(blue))))
	root := doc(el(sty(columns(2, 1), height(5), auto), strip(lime, height(4)), el(sty(display(css.Grid)), item)))
	res := run(t, root, page(9, 6))
	assertPixels(t, res, 1, `
		GG_______
		GG_______
		GG_BB____
		GG_______
		_________
		_________`)
}

// TestGrid_TableItemStretches holds a table grid item of auto width to CSS
// Grid 1, 6.2: justify-self normal stretches it to its area, within its
// max-width, and start keeps its fit-content width.
func TestGrid_TableItemStretches(t *testing.T) {
	t.Parallel()
	cell := func() *Node { return tr(nil, part(css.TableCell, nil, el(sty(width(10), height(10))))) }
	maxW := func(s *css.Style) { s.MaxWidth = css.Pts(px(50)) }
	start := func(s *css.Style) { s.JustifyItems = css.BoxStart }
	for _, tt := range []struct {
		name string
		grid *css.Style
		tb   *Node
		want float64
	}{
		{"stretched", gridBox(width(150)), table(nil, cell()), 150},
		{"max-width", gridBox(width(150)), table(mods(maxW), cell()), 50},
		{"start", gridBox(width(150), start), table(nil, cell()), 10},
	} {
		w := kid(t, boxes(t, doc(el(tt.grid, tt.tb)), page(200, 200)), 0, 0, 0)
		if got := rect(w); got[2] != tt.want {
			t.Errorf("%s: table item %v wide, want %v", tt.name, got[2], tt.want)
		}
	}
}

// TestGrid_LastBaselineAlignment checks CSS Box Alignment 3, 9.3: items
// aligned on the last baseline of their row share it, the one with the
// largest descent below it at the row's end.
//
// # Corpus evidence
//
//   - wpt: css/css-grid/alignment/grid-self-alignment-baseline-with-grid-001
//     to -004: Chromium 156 lines a 16px Ahem item up with the last
//     baseline of a grid item that is a grid under align-items: last
//     baseline.
func TestGrid_LastBaselineAlignment(t *testing.T) {
	last := css.BoxAlign(css.BoxLastBaseline)
	padded := el(sty(func(s *css.Style) { s.Padding[css.Bottom] = css.Pts(px(20)) }), txt(sty(), "a"))
	big := el(sty(font(20)), txt(sty(font(20)), "b"))
	rb := boxes(t, doc(el(gridBox(cols(tauto(), tauto()), alignItems(last)), padded, big)), page(100, 100))
	c := kid(t, rb, 0, 0)
	// Ahem: the 10px item's baseline is 8px down, 22px above its bottom;
	// the 20px item's 16px down, 4px above its bottom.
	if got := rect(c.kids[0]); got[1] != 8 {
		t.Errorf("padded item at %v, want y 8: at the row's end", got)
	}
	if got := rect(c.kids[1]); got[1] != 0 {
		t.Errorf("20px item at %v, want y 0: its baseline on the other's", got)
	}
	if got := rect(c); got[3] != 38 {
		t.Errorf("grid %v, want 38px tall", got)
	}
}
