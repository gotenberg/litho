package layout

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// ruleOf returns a footnote separator half the area wide, 1 px thick with
// 1 px above and below, in colour c, aligned per a.
func ruleOf(c css.Color, a css.TextAlign, line css.BorderStyle) *FootnoteRule {
	return &FootnoteRule{Width: 0.5, Thickness: px(1), Color: c, Align: a, Line: line, SpaceAbove: px(1), SpaceBelow: px(1)}
}

// TestFootnoteRule_Draws draws the separator above the footnote area of a
// page holding a note, taking its room from the page area, and leaves it
// out on a page without notes.
func TestFootnoteRule_Draws(t *testing.T) {
	t.Parallel()
	red2 := sty(font(2), fg(red))
	pb := func(r *FootnoteRule) PageBox { return PageBox{Width: px(6), Height: px(8), FootnoteRule: r} }
	tests := []struct {
		name string
		root *Node
		pb   PageBox
		want string
	}{
		{"left", doc(el(red2, txt(red2, "X"), call("X", "XX"))), pb(ruleOf(lime, css.AlignLeft, css.Solid)), `
			RRRR__
			RRRR__
			______
			______
			GGG___
			______
			BBBB__
			BBBB__`},
		{"centre", doc(el(red2, txt(red2, "X"), call("X", "XX"))), pb(&FootnoteRule{Width: 1.0 / 3, Thickness: px(1), Color: lime, Align: css.AlignCenter, Line: css.Solid, SpaceAbove: px(1), SpaceBelow: px(1)}), `
			RRRR__
			RRRR__
			______
			______
			__GG__
			______
			BBBB__
			BBBB__`},
		{"end in a left-to-right page", doc(el(red2, txt(red2, "X"), call("X", "XX"))), pb(ruleOf(lime, css.AlignEnd, css.Solid)), `
			RRRR__
			RRRR__
			______
			______
			___GGG
			______
			BBBB__
			BBBB__`},
		{"no alpha takes the body's colour", doc(el(red2, txt(red2, "X"), call("X", "XX"))), pb(ruleOf(css.Color{}, css.AlignLeft, css.Solid)), `
			RRRR__
			RRRR__
			______
			______
			KKK___
			______
			BBBB__
			BBBB__`},
		{"line none keeps the room", doc(el(red2, txt(red2, "X"), call("X", "XX"))), pb(ruleOf(lime, css.AlignLeft, css.BorderNone)), `
			RRRR__
			RRRR__
			______
			______
			______
			______
			BBBB__
			BBBB__`},
		// Without the rule the second line fits beside the note; its
		// room moves it on, and the page after it holds no note and no
		// rule.
		{"the room comes off the page area", doc(el(red2, txt(red2, "X"), call("X", "X")), el(red2, txt(red2, "X")), el(red2, txt(red2, "XX"))), PageBox{Width: px(6), Height: px(6), FootnoteRule: &FootnoteRule{Width: 0.5, Thickness: px(1), Color: lime, Line: css.Solid}}, `
			RRRR__
			RRRR__
			______
			GGG___
			BB____
			BB____
			RR____
			RR____
			RRRR__
			RRRR__
			______
			______`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			res := run(t, tt.root, tt.pb)
			var rows string
			for i := range res.Pages {
				rows += "\n" + strings.Join(grid(render(t, res, i+1)), "\n")
			}
			if want := normalize(tt.want); rows[1:] != want {
				t.Errorf("pixels:\n%s\nwant:\n%s", rows[1:], want)
			}
		})
	}
}

// TestFootnoteRule_Invalid refuses a separator Layout cannot draw.
func TestFootnoteRule_Invalid(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		rule FootnoteRule
	}{
		{"width past 1", FootnoteRule{Width: 1.5}},
		{"negative width", FootnoteRule{Width: -0.1}},
		{"NaN thickness", FootnoteRule{Thickness: math.NaN()}},
		{"negative space above", FootnoteRule{SpaceAbove: -1}},
		{"infinite space below", FootnoteRule{SpaceBelow: math.Inf(1)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := tt.rule
			_, err := Layout(context.Background(), doc(), ahem(t), Options{Page: PageBox{Width: 100, Height: 100, FootnoteRule: &r}})
			if !errors.Is(err, ErrInvalidOptions) {
				t.Errorf("err %v, want ErrInvalidOptions", err)
			}
		})
	}
}
