package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// TestLineQuirks_NoStrut lays out lines by the line height calculation
// quirk and the blocks ignore line-height quirk (Quirks Mode Standard, 3.3
// and 3.4) in quirks and limited-quirks mode: no strut, an empty inline
// box without inline-axis border or padding adds nothing, a line holding a
// line break alone is as tall as the break's line height; no-quirks mode
// keeps the strut.
func TestLineQuirks_NoStrut(t *testing.T) {
	lh := func(v float64) func(*css.Style) {
		return func(s *css.Style) { s.LineHeight = css.Pts(px(v)) }
	}
	block := sty(lh(40))
	for _, tc := range []struct {
		name         string
		kids         func() []*Node
		quirks, none float64
	}{
		{"small span", func() []*Node { return []*Node{el(inlineSty(lh(10)), txt(inlineSty(lh(10)), "x"))} }, 10, 40},
		{"root text", func() []*Node {
			return []*Node{txt(sty(lh(40)), "x"), el(inlineSty(lh(10)), txt(inlineSty(lh(10)), "y"))}
		}, 40, 40},
		{"empty span", func() []*Node {
			return []*Node{el(inlineSty(lh(80))), el(inlineSty(lh(10)), txt(inlineSty(lh(10)), "y"))}
		}, 10, 80},
		{"padded empty span", func() []*Node {
			return []*Node{el(inlineSty(lh(80), padding(0, 1, 0, 0))), el(inlineSty(lh(10)), txt(inlineSty(lh(10)), "y"))}
		}, 80, 80},
		{"break alone", func() []*Node { return []*Node{br(inlineSty(lh(5)))} }, 5, 40},
		{"break then text", func() []*Node { return []*Node{br(inlineSty(lh(50))), txt(sty(lh(40)), "x")} }, 90, 80},
	} {
		for _, mode := range []css.DocumentMode{css.Quirks, css.LimitedQuirks, css.NoQuirks} {
			root := doc(el(block, tc.kids()...))
			b := kid(t, boxesWith(t, root, Options{Page: page(200, 200), Mode: mode}), 0)
			want := tc.quirks
			if mode == css.NoQuirks {
				want = tc.none
			}
			if got := rect(b)[3]; got != want {
				t.Errorf("%s, mode %d: height %v, want %v", tc.name, mode, got, want)
			}
		}
	}
}

// TestMarginQuirks_DroppedAtContainerEdges drops the quirky margins that
// adjoin the start or the end of a quirky container's content (HTML
// Standard, 15.3.3): through a block, after an empty quirky box, and
// collapsed with a margin that is not quirky, which stays; elsewhere a
// quirky margin collapses as any other.
func TestMarginQuirks_DroppedAtContainerEdges(t *testing.T) {
	quirky := func(q MarginQuirks, n *Node) *Node {
		n.MarginQuirks = q
		return n
	}
	para := func(text string) *Node {
		st := sty(margin(16, 0, 16, 0), height(10))
		if text == "" {
			st = sty(margin(16, 0, 16, 0))
		}
		n := el(st)
		if text != "" {
			n.Children = []*Node{txt(inlineSty(), text)}
		}
		return quirky(QuirkyTop|QuirkyBottom, n)
	}
	for _, tc := range []struct {
		name     string
		kids     func() []*Node
		quirks   [2]float64
		standard [2]float64
	}{
		// The container's height, then its first child's top.
		{"alone", func() []*Node { return []*Node{para("a")} }, [2]float64{10, 0}, [2]float64{42, 16}},
		{"through a block", func() []*Node { return []*Node{el(sty(margin(4, 0, 0, 0)), para("a"))} }, [2]float64{14, 4}, [2]float64{42, 16}},
		{"after an empty one", func() []*Node { return []*Node{para(""), para("a")} }, [2]float64{10, 0}, [2]float64{42, 16}},
		{"between two", func() []*Node { return []*Node{para("a"), para("b")} }, [2]float64{36, 0}, [2]float64{68, 16}},
	} {
		for _, quirks := range []bool{true, false} {
			q := MarginQuirks(0)
			kids := tc.kids()
			if quirks {
				q = QuirkyContainer
			} else {
				var clear func(*Node)
				clear = func(n *Node) {
					n.MarginQuirks = 0
					for _, k := range n.Children {
						clear(k)
					}
				}
				for _, k := range kids {
					clear(k)
				}
			}
			cell := quirky(q, el(sty(float(css.FloatLeft), width(50)), kids...))
			b := kid(t, boxesWith(t, doc(cell), Options{Page: page(200, 200), Mode: css.Quirks}), 0, 0)
			first := kid(t, b, 0)
			got := [2]float64{rect(b)[3], rect(first)[1] - rect(b)[1]}
			want := tc.standard
			if quirks {
				want = tc.quirks
			}
			if got != want {
				t.Errorf("%s, quirky %v: height and first top %v, want %v", tc.name, quirks, got, want)
			}
		}
	}
}
