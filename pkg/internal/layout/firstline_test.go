package layout

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// lineStyler is a [FirstLineStyler] that gives the nodes in pseudo a
// ::first-line changed by their mod, and restyles the nodes in restyle by
// theirs, each inheriting from the parent style it is given.
type lineStyler struct {
	pseudo  map[*Node]func(*css.Style)
	restyle map[*Node]func(*css.Style)
}

func (l lineStyler) Pseudo(n *Node, parent *css.Style) *css.Style {
	mod, ok := l.pseudo[n]
	if !ok {
		return nil
	}
	st := *parent
	st.Display = css.Inline
	st.BackgroundColor = css.Color{}
	mod(&st)
	return &st
}

func (l lineStyler) Restyle(n *Node, parent *css.Style) *css.Style {
	mod, ok := l.restyle[n]
	if !ok {
		return nil
	}
	st := *n.Style
	st.Color, st.FontSize, st.TextTransform = parent.Color, parent.FontSize, parent.TextTransform
	mod(&st)
	return &st
}

func size(v float64) func(*css.Style) { return func(s *css.Style) { s.FontSize = px(v) } }

// TestFirstLine_BreaksWithItsStyle checks that the first line is broken
// with the font it is drawn in and the lines after it with the block's:
// 20px Ahem fits two letters in 60px, 10px Ahem five (CSS Pseudo 4, 2.1).
func TestFirstLine_BreaksWithItsStyle(t *testing.T) {
	p := el(sty(), txt(sty(), "aa bb cc dd ee"))
	o := Options{Page: page(60, 200), FirstLine: lineStyler{pseudo: map[*Node]func(*css.Style){p: size(20)}}}
	got, pos := lineTexts(runWith(t, doc(p), o), 1)
	if want := []string{"aa ", "bb cc ", "dd ee"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("lines %q, want %q", got, want)
	}
	// The first line is 20px tall, its baseline 16px down; the next ones
	// are 10px.
	if !near(pos[0][1], 16) || !near(pos[1][1], 28) || !near(pos[2][1], 38) {
		t.Errorf("baselines %v", pos)
	}
}

// TestFirstLine_TransformChangesTheCount checks that a first line whose
// text-transform changes the count of characters ends where its text
// goes on: "ß" is "SS" in capitals.
func TestFirstLine_TransformChangesTheCount(t *testing.T) {
	p := el(sty(), txt(sty(), "ß ß ß ß"))
	up := func(s *css.Style) { s.TextTransform = css.Uppercase }
	o := Options{Page: page(60, 200), FirstLine: lineStyler{pseudo: map[*Node]func(*css.Style){p: up}}}
	got, _ := lineTexts(runWith(t, doc(p), o), 1)
	if want := []string{"SS SS ", "ß ß"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("lines %q, want %q", got, want)
	}
}

// TestFirstLine_EndsAtAForcedBreak checks that the line after a forced
// break is no first formatted line (CSS Pseudo 4, 2.1.1) and that an
// inline box on the first line takes the style the styler gives it.
func TestFirstLine_EndsAtAForcedBreak(t *testing.T) {
	span := el(inlineSty(), txt(inlineSty(), "b"))
	p := el(sty(), txt(sty(), "a"), span, br(sty()), txt(sty(), "c"))
	o := Options{Page: page(200, 200), FirstLine: lineStyler{
		pseudo:  map[*Node]func(*css.Style){p: size(20)},
		restyle: map[*Node]func(*css.Style){span: func(*css.Style) {}},
	}}
	res := runWith(t, doc(p), o)
	sizes := map[string]float64{}
	for _, r := range texts(res, 1) {
		sizes[r.text] = r.run.Size
	}
	if !near(sizes["a"], px(20)) || !near(sizes["b"], px(20)) || !near(sizes["c"], px(10)) {
		t.Errorf("sizes %v, want a and b at 20px and c at 10px", sizes)
	}
}

// TestFirstLine_LineHeight checks that the line-height of the ::first-line
// sets the first line box's height, smaller than the block's too (CSS
// Pseudo 4, 2.1.2).
func TestFirstLine_LineHeight(t *testing.T) {
	lh := func(v float64) func(*css.Style) { return func(s *css.Style) { s.LineHeight = css.Pts(px(v)) } }
	p := el(sty(lh(60)), txt(sty(lh(60)), "a"), br(sty(lh(60))), txt(sty(lh(60)), "b"))
	o := Options{Page: page(200, 200), FirstLine: lineStyler{pseudo: map[*Node]func(*css.Style){p: lh(10)}}}
	_, pos := lineTexts(runWith(t, doc(p), o), 1)
	// The first line is 10px tall, baseline 8px; the second 60px, its
	// baseline 25px under its half-leading.
	if len(pos) != 2 || !near(pos[0][1], 8) || !near(pos[1][1], 10+25+8) {
		t.Errorf("baselines %v", pos)
	}
}

// TestFirstLine_ReachesTheFirstChildBlock checks that a block's
// ::first-line styles the first line of its first in-flow block child,
// past white space and a float, and not past an empty block (CSS Pseudo 4,
// 2.1.1).
func TestFirstLine_ReachesTheFirstChildBlock(t *testing.T) {
	float := func(s *css.Style) { s.Float = css.FloatLeft; s.Width = css.Pts(px(10)) }
	tests := []struct {
		name string
		kids func() []*Node
		big  bool
	}{
		{"first child", func() []*Node { return []*Node{el(sty(), txt(sty(), "a"))} }, true},
		{"after white space and a float", func() []*Node {
			return []*Node{txt(sty(), " "), el(sty(float), txt(sty(), "f")), txt(sty(), "\n"), el(sty(), txt(sty(), "a"))}
		}, true},
		{"after an empty block", func() []*Node { return []*Node{el(sty()), el(sty(), txt(sty(), "a"))} }, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outer := el(sty(), tt.kids()...)
			o := Options{Page: page(200, 200), FirstLine: lineStyler{pseudo: map[*Node]func(*css.Style){outer: size(20)}}}
			res := runWith(t, doc(outer), o)
			for _, r := range texts(res, 1) {
				if r.text == "a" && near(r.run.Size, px(20)) != tt.big {
					t.Errorf("a drawn at %v", r.run.Size)
				}
			}
		})
	}
}

// TestFirstLine_PaintsItsBackground checks that the ::first-line paints its
// background under the first line's content, as an inline box does: in
// the space between the Ahem squares, and not on the second line.
func TestFirstLine_PaintsItsBackground(t *testing.T) {
	p := el(sty(), txt(sty(), "a a b"))
	bg := func(s *css.Style) { s.BackgroundColor = red }
	o := Options{Page: page(30, 20), FirstLine: lineStyler{pseudo: map[*Node]func(*css.Style){p: bg}}}
	assertPixels(t, runWith(t, doc(p), o), 1, strings.Repeat("KKKKKKKKKKRRRRRRRRRRKKKKKKKKKK\n", 10)+strings.Repeat("KKKKKKKKKK____________________\n", 10))
}

// TestFirstLine_Anchors checks that an element with an ID on the first line
// records where it lands.
func TestFirstLine_Anchors(t *testing.T) {
	span := el(inlineSty(), txt(inlineSty(), "b"))
	span.ID = "s"
	p := el(sty(), txt(sty(), "a "), span)
	o := Options{Page: page(200, 200), FirstLine: lineStyler{
		pseudo:  map[*Node]func(*css.Style){p: size(20)},
		restyle: map[*Node]func(*css.Style){span: func(*css.Style) {}},
	}}
	res := runWith(t, doc(p), o)
	d, ok := res.Anchors["s"]
	if !ok || d.Page != 1 || !near(d.X, px(40)) {
		t.Errorf("anchor %+v, %v; want page 1 at 40px", d, ok)
	}
}

// TestFirstLine_ThroughContents checks that text whose style inherits as
// its box's parent does takes the first line's style, and text whose own
// inherited style differs, as under an element of display: contents that
// sets one, keeps it.
func TestFirstLine_ThroughContents(t *testing.T) {
	blueSt := sty(func(s *css.Style) { s.Color = blue })
	p := el(sty(), txt(sty(), "a "), txt(blueSt, "b"))
	o := Options{Page: page(200, 200), FirstLine: lineStyler{pseudo: map[*Node]func(*css.Style){p: size(20)}}}
	sizes := map[string]float64{}
	for _, r := range texts(runWith(t, doc(p), o), 1) {
		sizes[strings.TrimSpace(r.text)] = r.run.Size
	}
	if !near(sizes["a"], px(20)) || !near(sizes["b"], px(10)) {
		t.Errorf("sizes %v, want a at 20px and b at 10px", sizes)
	}
}
