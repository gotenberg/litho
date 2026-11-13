package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestMarginTrim_BlockContainer holds margin-trim on a block container to
// CSS Box 4, 3.3 and 3.3.1: the block-start margin of the first in-flow
// child and the block-end margin of the last, with every margin collapsed
// with them, those of empty children included, are truncated to zero; the
// container's own margins stay.
func TestMarginTrim_BlockContainer(t *testing.T) {
	t.Parallel()
	trim := func(m css.MarginTrim) func(*css.Style) { return func(s *css.Style) { s.MarginTrim = m } }
	both := css.TrimBlockStart | css.TrimBlockEnd
	box10 := func(mods ...func(*css.Style)) *Node {
		return el(sty(append([]func(*css.Style){height(10)}, mods...)...))
	}
	tests := []struct {
		name string
		root *Node
		path []int
		want [4]float64 // border box of the box at path, in px
	}{
		{"start trims the first child's margin", doc(el(sty(trim(css.TrimBlockStart), margin(5, 0)), box10(margin(30, 0)))), []int{0, 0}, [4]float64{0, 5, 100, 10}},
		{"start reaches a nested first child", doc(el(sty(trim(css.TrimBlockStart)), el(sty(margin(20, 0)), box10(margin(30, 0))))), []int{0, 0, 0, 0}, [4]float64{0, 0, 100, 10}},
		{"start trims through an empty child", doc(el(sty(trim(css.TrimBlockStart), border(1, css.Solid, black)), el(sty(margin(20, 0, 40, 0))), box10(margin(30, 0)))), []int{0, 0, 1}, [4]float64{1, 1, 98, 10}},
		{"start spares the second child", doc(el(sty(trim(css.TrimBlockStart), border(1, css.Solid, black)), box10(margin(30, 0)), box10(margin(20, 0)))), []int{0, 0, 1}, [4]float64{1, 41, 98, 10}},
		{"end trims the last child's margin", doc(el(sty(trim(css.TrimBlockEnd), border(1, css.Solid, black)), box10(margin(0, 0, 30, 0)))), []int{0, 0}, [4]float64{0, 0, 100, 12}},
		{"end trims the margins collapsed through an empty last child", doc(el(sty(trim(both), border(1, css.Solid, black)), box10(margin(0, 0, 30, 0)), el(sty(height(0), margin(50, 0))))), []int{0, 0}, [4]float64{0, 0, 100, 12}},
		{"end keeps the container's own margin", doc(el(sty(trim(css.TrimBlockEnd), margin(0, 0, 7, 0)), box10(margin(0, 0, 30, 0))), box10()), []int{0, 1}, [4]float64{0, 17, 100, 10}},
		{"a flex container trims nothing", doc(el(sty(trim(both), border(1, css.Solid, black), func(s *css.Style) { s.Display = css.Flex }), box10(margin(30, 0)))), []int{0, 0}, [4]float64{0, 0, 100, 72}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rb := boxes(t, tt.root, page(100, 200))
			if got := rect(kid(t, rb, tt.path...)); got != tt.want {
				t.Errorf("border box %v, want %v", got, tt.want)
			}
		})
	}
}

// TestMarginTrim_Spanners holds margin-trim on a multi-column container
// to CSS Box 4, 3.3: the block-start margin of a spanner before any
// column content and the block-end margin of the last spanner are
// truncated, those between spanners collapse as usual.
func TestMarginTrim_Spanners(t *testing.T) {
	t.Parallel()
	span := func(s *css.Style) { s.Columns.SpanAll = true }
	mc := el(sty(columns(2, 1), func(s *css.Style) { s.MarginTrim = css.TrimBlockStart | css.TrimBlockEnd }),
		el(sty(span, height(2), margin(3, 0))), el(sty(span, height(2), margin(3, 0))))
	rb := boxes(t, doc(mc), page(100, 200))
	if got := rect(kid(t, rb, 0, 0)); got[3] != 7 {
		t.Errorf("container height %v, want 7: 2 + 3 + 2", got[3])
	}
	if got := rect(kid(t, rb, 0, 0, 0)); got[1] != 0 {
		t.Errorf("first spanner top %v, want 0", got[1])
	}
}
