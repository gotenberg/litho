package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

func subCols(names ...[]string) func(*css.Style) {
	return func(s *css.Style) { s.GridTemplateColumns = css.TrackList{Subgrid: true, Names: names} }
}

func subRows(s *css.Style) { s.GridTemplateRows = css.TrackList{Subgrid: true} }

func sides(top, right, bottom, left float64) [4]css.Dimension {
	return [4]css.Dimension{css.Pts(px(top)), css.Pts(px(right)), css.Pts(px(bottom)), css.Pts(px(left))}
}

func colLines(a, b css.GridLine) func(*css.Style) {
	return func(s *css.Style) { s.GridColumnStart, s.GridColumnEnd = a, b }
}

// subgridRects lays root out on a 500 by 500 px page and returns the
// border boxes of the kids of the box at path.
func subgridRects(t *testing.T, root *Node, path ...int) [][4]float64 {
	t.Helper()
	c := kid(t, boxes(t, root, page(500, 500)), path...)
	var out [][4]float64
	for _, k := range c.kids {
		out = append(out, rect(k))
	}
	return out
}

// TestSubgrid_ItemsSizeTheParentColumns checks CSS Grid 2, 9: the items of
// a column subgrid size the parent's auto columns, those at the
// subgrid's edges with its margin, border and padding as an extra margin,
// those at an inner line with half the difference of the gaps, and lie
// on the parent's lines with the subgrid's gap centred in the parent's
// gutters.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (gotenberg/gotenberg:9.4.1) sizes the
//     columns of a 400px grid of auto 1fr auto, gaps 10px, holding a
//     subgrid of column-gap 2px, margin 0 5px 0 2px, a 3px left border
//     and padding 0 7px, whose items are 71.38 and 54px wide, 79.38,
//     238.63 and 62px, its items at 12, 85.38 and 334px.
func TestSubgrid_ItemsSizeTheParentColumns(t *testing.T) {
	t.Parallel()
	sub := el(gridBox(subCols(), gridAt(2, 3, 1, 4), func(s *css.Style) {
		s.ColumnGap = css.Pts(px(2))
		s.Margin = sides(0, 5, 0, 2)
		s.Padding = sides(0, 7, 0, 7)
		s.Border[css.Left] = css.Border{Width: px(3), Style: css.Solid, Color: red}
	}), item(71, 14), item(20, 14), item(54, 14))
	root := doc(el(gridBox(cols(tauto(), tfr(1), tauto()), width(400), gaps(10, 10)),
		item(10, 14), item(20, 14), item(10, 14), sub))
	got := subgridRects(t, root, 0, 0)
	want := [][4]float64{{0, 0, 10, 14}, {89, 0, 20, 14}, {338, 0, 10, 14}, {2, 24, 393, 14}}
	if !eqRects(got, want) {
		t.Errorf("grid items = %v, want %v", got, want)
	}
	got = subgridRects(t, root, 0, 0, 3)
	want = [][4]float64{{12, 24, 71, 14}, {85, 24, 20, 14}, {334, 24, 54, 14}}
	if !eqRects(got, want) {
		t.Errorf("subgrid items = %v, want %v", got, want)
	}
}

// TestSubgrid_ItemsSizeTheParentRows checks CSS Grid 2, 9 in the block
// axis: the items of a row subgrid of row-gap 0 in a parent of row-gap
// 10px size the parent's rows less half the gap difference at each inner
// line, and lie on its lines.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (gotenberg/gotenberg:9.4.1) makes the rows
//     45, 10 and 39px of a grid whose own items are 30, 10 and 14px tall
//     beside a row subgrid holding items of 5 and 50, 20 and 20, and 44px,
//     at 0, 50 and 70px in it.
func TestSubgrid_ItemsSizeTheParentRows(t *testing.T) {
	t.Parallel()
	sub := el(gridBox(subRows, cols(tfr(1), tfr(1)), gridAt(1, 4, 2, 3), func(s *css.Style) { s.RowGap = css.Pts(0) }),
		item(auto, 5), item(auto, 50), item(auto, 20), item(auto, 20), item(auto, 44, gridAt(0, 0, 2, 3)))
	root := doc(el(gridBox(cols(tpx(100), tpx(200)), rowsT(tauto(), tauto(), tauto()), flow(css.GridFlowColumn), gaps(10, 10)),
		item(100, 30), item(100, 10), item(100, 14), sub))
	got := subgridRects(t, root, 0, 0)
	want := [][4]float64{{0, 0, 100, 30}, {0, 55, 100, 10}, {0, 75, 100, 14}, {110, 0, 200, 114}}
	if !eqRects(got, want) {
		t.Errorf("grid items = %v, want %v", got, want)
	}
	got = subgridRects(t, root, 0, 0, 3)
	want = [][4]float64{{110, 0, 100, 5}, {210, 0, 100, 50}, {110, 50, 100, 20}, {210, 50, 100, 20}, {210, 70, 100, 44}}
	if !eqRects(got, want) {
		t.Errorf("subgrid items = %v, want %v", got, want)
	}
}

// TestSubgrid_LineNames checks CSS Grid 2, 9: a subgrid's lines carry its
// own names and the parent's; an item is placed on hypothetical implicit
// lines as in any grid, then clamped into the subgrid's tracks, and an
// auto span is no wider than they are.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (gotenberg/gotenberg:9.4.1) places, in a
//     subgrid [s1] [s2] [s3] [s4] over the lines p2 to p5 of its parent,
//     p3 at 130px, s2 / s4 at 130px 160px wide in its third row (the line
//     p1, which the subgrid lacks, having placed p1 / p5 past it), p2 / s3
//     at 60px 140px wide, p1 / p5 at 210px, span 5 over the three tracks,
//     6 and -1 / -2 in the last track.
func TestSubgrid_LineNames(t *testing.T) {
	t.Parallel()
	ln := func(name string) css.GridLine { return css.GridLine{Name: name} }
	n := func(v int) css.GridLine { return css.GridLine{N: v} }
	row := func(r int) func(*css.Style) {
		return func(s *css.Style) { s.GridRowStart = n(r) }
	}
	sub := el(gridBox(subCols([]string{"s1"}, []string{"s2"}, []string{"s3"}, []string{"s4"}), gridAt(0, 0, 2, 5)),
		item(auto, 14, colLines(ln("p3"), css.GridLine{})),
		item(auto, 14, colLines(ln("s2"), ln("s4"))),
		item(auto, 14, colLines(ln("p2"), ln("s3")), row(2)),
		item(auto, 14, colLines(ln("p1"), ln("p5")), row(3)),
		item(auto, 14, colLines(css.GridLine{Span: true, N: 5}, css.GridLine{}), row(4)),
		item(auto, 14, colLines(n(6), css.GridLine{}), row(5)),
		item(auto, 14, colLines(n(-1), n(-2)), row(6)))
	parent := gridBox(width(400), gaps(10, 10), func(s *css.Style) {
		s.GridTemplateColumns = css.TrackList{
			Sizes: []css.TrackSize{tpx(50), tpx(60), tpx(70), tpx(80)},
			Names: [][]string{{"p1"}, {"p2"}, {"p3"}, {"p4"}, {"p5"}},
		}
	})
	got := subgridRects(t, doc(el(parent, sub)), 0, 0, 0)
	want := [][4]float64{
		{130, 0, 70, 14}, {130, 28, 160, 14}, {60, 14, 140, 14}, {210, 28, 80, 14},
		{60, 42, 230, 14}, {210, 56, 80, 14}, {210, 70, 80, 14},
	}
	if !eqRects(got, want) {
		t.Errorf("subgrid items = %v, want %v", got, want)
	}
}

// TestSubgrid_EdgesHoldTheirTracks checks the empty items of CSS Grid 2,
// 9: a subgrid's padding sizes its edge tracks even where no item stands
// alone in them.
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (gotenberg/gotenberg:9.4.1) sizes the auto
//     columns of a grid holding a subgrid of padding 0 30px over three
//     columns, whose one item spans the first two, 64.34, 47.03 and 30px:
//     the 30px of the empty last track, and the first track holding 30px
//     before the item's span is shared.
func TestSubgrid_EdgesHoldTheirTracks(t *testing.T) {
	t.Parallel()
	sub := el(gridBox(subCols(), gridAt(0, 0, 1, 4), padding(0, 30)), item(91, 14, gridAt(0, 0, 1, 3)))
	root := doc(el(gridBox(cols(tauto(), tauto(), tauto()), width(400), gaps(10, 10), func(s *css.Style) { s.JustifyContent = css.BoxStart }),
		sub, item(10, 14), item(12, 14), item(10, 14)))
	got := subgridRects(t, root, 0, 0)
	want := [][4]float64{{0, 0, 161, 14}, {0, 24, 10, 14}, {74.5, 24, 12, 14}, {131, 24, 10, 14}}
	if !eqRects(got, want) {
		t.Errorf("grid items = %v, want %v", got, want)
	}
}

// TestSubgrid_Nested checks that extra margins add up through nested
// subgrids (CSS Grid 2, 9).
//
// # Corpus evidence
//
//   - probe, html: Chromium 156 (gotenberg/gotenberg:9.4.1) sizes two auto
//     columns 28.03px each for 20.03px items in a subgrid of padding 5px
//     inside a subgrid of a 3px border, the items at 8 and 38.03px.
func TestSubgrid_Nested(t *testing.T) {
	t.Parallel()
	inner := el(gridBox(subCols(), gridAt(0, 0, 1, 3), padding(5)), item(20, 14), item(20, 14))
	outer := el(gridBox(subCols(), gridAt(0, 0, 1, 3), border(3, css.Solid, red)), inner)
	root := doc(el(gridBox(cols(tauto(), tauto()), width(400), gaps(10, 10), func(s *css.Style) { s.JustifyContent = css.BoxStart }), outer))
	got := subgridRects(t, root, 0, 0, 0, 0)
	want := [][4]float64{{8, 8, 20, 14}, {38, 8, 20, 14}}
	if !eqRects(got, want) {
		t.Errorf("nested subgrid items = %v, want %v", got, want)
	}
}

// TestSubgrid_OnlyInAGrid checks CSS Grid 2, 9.1: a grid that is no grid
// item takes subgrid as none.
func TestSubgrid_OnlyInAGrid(t *testing.T) {
	t.Parallel()
	root := doc(el(gridBox(subCols(), width(300)), item(auto, 14), item(auto, 14)))
	got := subgridRects(t, root, 0, 0)
	want := [][4]float64{{0, 0, 300, 14}, {0, 14, 300, 14}}
	if !eqRects(got, want) {
		t.Errorf("items = %v, want %v", got, want)
	}
}
