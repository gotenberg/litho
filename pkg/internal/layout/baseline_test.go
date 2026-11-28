package layout

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"golang.org/x/net/html"
)

func TestBaselineSource_PicksTheLine(t *testing.T) {
	// CSS Inline 3, 4.3: an inline-block aligns by its last line under
	// auto and last, by its first under first.
	two := func(src css.BaselineSource) *Node {
		return el(sty(display(css.InlineBlock), width(20), func(s *css.Style) { s.BaselineSource = src }),
			el(sty(height(10)), txt(sty(), "a")), el(sty(height(10)), txt(sty(), "b")))
	}
	for _, tt := range []struct {
		src  css.BaselineSource
		last bool
	}{{css.BaselineAuto, true}, {css.BaselineLast, true}, {css.BaselineFirst, false}} {
		rb := boxes(t, doc(el(sty(), txt(sty(), "x"), two(tt.src))), page(200, 200))
		a := kid(t, rb, 0, 0, 1)
		first, _ := firstBaseline(a)
		last, _ := lastBaseline(a)
		if near(first, last) {
			t.Fatalf("first and last baselines agree at %v", first)
		}
		want := first
		if tt.last {
			want = last
		}
		if !a.hasBaseline || !near(a.baseline+a.y, want) {
			t.Errorf("baseline-source %v: baseline %v, want %v", tt.src, a.baseline+a.y, want)
		}
	}
}

// TestBaseline_ClipKeepsTheLine sets an inline-block that overflow: clip
// clips on its line's baseline, and one under overflow: hidden, a scroll
// container, on its bottom margin edge (CSS Overflow 3, 3.1; CSS 2.1,
// 10.8.1).
func TestBaseline_ClipKeepsTheLine(t *testing.T) {
	for decl, last := range map[string]bool{
		"overflow: clip": true, "overflow-x: clip": true, "overflow: clip visible": true,
		"overflow: hidden": false, "overflow: clip hidden": false,
	} {
		st := computedStyle(t, "display: inline-block; height: 40px; "+decl)
		ib := el(st, txt(sty(), "a"))
		rb := boxes(t, doc(el(sty(), txt(sty(), "x"), ib)), page(200, 200))
		a := ofNode(rb, ib)
		y, _ := lastBaseline(a)
		if got := a.hasBaseline && near(a.baseline+a.y, y); got != last {
			t.Errorf("%s: baseline on the line %v, want %v", decl, got, last)
		}
	}
}

// TestBaseline_ScrollChildEndsTheSearch sets an inline-block on the bottom
// margin edge of a block child that is a scroll container, empty or
// holding a line, and not on that line (CSS Align 3, 9.1): its baseline
// is 30px down, under a 30px child with a 10px bottom margin 40px down.
func TestBaseline_ScrollChildEndsTheSearch(t *testing.T) {
	for _, tc := range []struct {
		name   string
		margin float64
		kids   []*Node
		want   float64
	}{
		{"empty", 0, nil, 30},
		{"a line", 0, []*Node{txt(sty(), "a")}, 30},
		{"a bottom margin", 10, []*Node{txt(sty(), "a")}, 40},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inner := sty(width(20), height(30), margin(0, 0, tc.margin, 0), func(s *css.Style) { s.Overflow = css.OverflowHidden })
			ib := el(sty(display(css.InlineBlock), padding(0, 0, 20, 0)), el(inner, tc.kids...))
			rb := boxes(t, doc(el(sty(), txt(sty(), "x"), ib)), page(200, 200))
			a := ofNode(rb, ib)
			if !a.hasBaseline || !near(a.baseline, px(tc.want)) {
				t.Errorf("baseline %v down the inline-block, want %v", a.baseline, px(tc.want))
			}
		})
	}
}

// TestBaseline_ControlPlaceholderKeepsItsLine keeps a text field on the
// line of its placeholder, a block of overflow: hidden as the user agent
// sheet makes it: a control's own blocks stand for its inner editor.
func TestBaseline_ControlPlaceholderKeepsItsLine(t *testing.T) {
	st := sty(display(css.InlineBlock), width(100), height(30))
	ps := sty(display(css.Block), func(s *css.Style) { s.Overflow = css.OverflowHidden })
	ph := el(ps, txt(ps, "a"))
	n := &Node{Kind: Element, Style: st, Control: &Control{Kind: ControlTextField, Placeholder: true}, Children: []*Node{ph}}
	rb := boxes(t, doc(el(sty(), txt(sty(), "x"), n)), page(200, 100))
	a, p := ofNode(rb, n), ofNode(rb, ph)
	want, _ := lastBaseline(p)
	if !a.hasBaseline || !near(a.baseline+a.y, want) {
		t.Errorf("field baseline at %v, want its placeholder's line at %v", a.baseline+a.y, want)
	}
}

// TestBaseline_OrthogonalScrollChildGivesNone keeps an inline-block on its
// line when a vertical scroll container follows it (CSS Align 3, 9.1): an
// orthogonal flow root gives no baseline in its parent's axis.
func TestBaseline_OrthogonalScrollChildGivesNone(t *testing.T) {
	vert := sty(func(s *css.Style) {
		s.WritingMode = css.VerticalRL
		s.Overflow = css.OverflowHidden
	})
	ib := el(sty(display(css.InlineBlock)), txt(sty(), "a"), el(vert, txt(vert, "v")))
	rb := boxes(t, doc(el(sty(), txt(sty(), "x"), ib)), page(200, 200))
	a := ofNode(rb, ib)
	if !a.hasBaseline || !near(a.baseline, px(8)) {
		t.Errorf("baseline %v down the inline-block, want %v: its line's", a.baseline, px(8))
	}
}

// computedStyle returns the computed style of a div whose style attribute
// is decls.
func computedStyle(t *testing.T, decls string) *css.Style {
	t.Helper()
	root, err := html.Parse(strings.NewReader(`<div style="` + decls + `"></div>`))
	if err != nil {
		t.Fatal(err)
	}
	c, err := css.NewResolver(css.Media{}, nil).Compute(context.Background(), root, css.ComputeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var div *html.Node
	var find func(n *html.Node)
	find = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "div" && div == nil {
			div = n
		}
		for k := n.FirstChild; k != nil; k = k.NextSibling {
			find(k)
		}
	}
	find(root)
	return c.Style(div)
}

// TestBaseline_AtBottom sets an inline-block of css.Office.BaselineAtBottom
// holding a line on its bottom margin edge, as Word sits an inline text
// box, where the same box without it aligns by its line.
func TestBaseline_AtBottom(t *testing.T) {
	for _, bottom := range []bool{false, true} {
		ib := el(sty(display(css.InlineBlock), width(20), height(40), func(s *css.Style) { s.BaselineAtBottom = bottom }), txt(sty(), "a"))
		rb := boxes(t, doc(el(sty(), txt(sty(), "x"), ib)), page(200, 200))
		a := ofNode(rb, ib)
		if a.hasBaseline == bottom {
			t.Errorf("BaselineAtBottom %v: has a baseline %v", bottom, a.hasBaseline)
		}
	}
}

// TestSmallCapsSize draws synthesised small capitals at 0.7 em, and at
// 0.8 of the size rounded to the half point under
// css.Office.WordSmallCaps, as Word draws them: 13, 17 and 23 points
// tell the half point from the whole point.
func TestSmallCapsSize(t *testing.T) {
	word := &css.Style{}
	word.WordSmallCaps = true
	for _, tc := range []struct {
		st         *css.Style
		size, want float64
	}{{&css.Style{}, 20, 14}, {word, 20, 16}, {word, 11, 9}, {word, 12, 9.5}, {word, 13, 10.5}, {word, 17, 13.5}, {word, 23, 18.5}} {
		if got := smallCapsSize(tc.st, tc.size); !near(got, tc.want) {
			t.Errorf("WordSmallCaps %v at %v: %v, want %v", tc.st.WordSmallCaps, tc.size, got, tc.want)
		}
	}
}
