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
