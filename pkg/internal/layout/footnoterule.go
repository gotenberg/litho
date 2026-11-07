package layout

import (
	"fmt"
	"math"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// FootnoteRule is the separator a page draws between its body and its
// footnote area, as Writer's style:footnote-sep (ODF 1.3 Part 3, 17.19)
// and Word's separator footnote (ECMA-376 Part 1, 17.11, the w:separator
// mark) place it: a horizontal rule of Thickness points in Color, Width
// times the footnote area's width long, at the area's start, centre or
// end per Align, with SpaceAbove points between the body text and the
// rule and SpaceBelow between the rule and the first note. The rule and
// its spaces take room from the page area as the notes do, and are drawn
// only on a page whose footnote area holds a note.
type FootnoteRule struct {
	// Width is the rule's length as a fraction of the footnote area's
	// width, 0 to 1; Writer's style:rel-width, Word's separator being
	// one third of the text width. 0 draws no rule but keeps the spaces.
	Width float64
	// Thickness is the rule's stroke width in points, finite and at least
	// 0; 0 draws a hairline.
	Thickness float64
	// Color is the rule's colour; its Alpha 0 draws it in the body's text
	// colour.
	Color css.Color
	// Align places the rule in the footnote area: [css.AlignLeft],
	// [css.AlignCenter] or [css.AlignRight]; start and end follow the
	// page's direction, and any other value reads as left.
	Align css.TextAlign
	// Line is the rule's line style: [css.Solid], [css.Dotted] or
	// [css.Dashed]; [css.BorderNone] draws none, and any other value
	// draws solid.
	Line css.BorderStyle
	// SpaceAbove and SpaceBelow are the room in points left above and
	// below the rule, finite and at least 0.
	SpaceAbove, SpaceBelow float64
}

// hairline is the stroke width in points of a [FootnoteRule] of Thickness
// 0: the thinnest line LibreOffice strokes a separator with.
const hairline = 0.1

// checkFootnoteRule reports a footnote separator of pb that [Layout] cannot
// draw: a length that is not finite or is negative, or a Width outside 0
// to 1.
func checkFootnoteRule(pb PageBox) error {
	r := pb.FootnoteRule
	if r == nil {
		return nil
	}
	for _, v := range [...]float64{r.Width, r.Thickness, r.SpaceAbove, r.SpaceBelow} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return fmt.Errorf("%w: footnote rule length %v", ErrInvalidOptions, v)
		}
	}
	if r.Width > 1 {
		return fmt.Errorf("%w: footnote rule width %v past the area's", ErrInvalidOptions, r.Width)
	}
	return nil
}

// ruleRoom returns the room the footnote separator of page p takes above
// its footnote area: the spaces above and below it and its thickness, 0
// without one.
func (e *engine) ruleRoom(p int) float64 {
	r := e.box(p).FootnoteRule
	if r == nil {
		return 0
	}
	return r.SpaceAbove + r.Thickness + r.SpaceBelow
}

// paintFootnoteRule paints the footnote separator of pb on pg, top being
// the top of the room the rule takes in page coordinates, as Writer draws
// style:footnote-sep (ODF 1.3 Part 3, 17.19): a horizontal line Width
// times the area's width long at the area's left, centre or right, its
// top SpaceAbove below top, in its colour or the body's text colour.
func (e *engine) paintFootnoteRule(pg *compose.Page, pb PageBox, top float64) {
	r := pb.FootnoteRule
	if r == nil || r.Line == css.BorderNone || r.Line == css.BorderHidden || r.Width <= 0 {
		return
	}
	aw, _ := areaOf(pb)
	w := r.Width * aw
	align := r.Align
	rtl := e.rootBox != nil && e.rootBox.st.Direction == css.RTL
	switch align { //nolint:exhaustive // the other values read as left
	case css.AlignStart:
		align = css.AlignLeft
		if rtl {
			align = css.AlignRight
		}
	case css.AlignEnd:
		align = css.AlignRight
		if rtl {
			align = css.AlignLeft
		}
	}
	x := pb.Margin[css.Left]
	switch align { //nolint:exhaustive // the other values read as left
	case css.AlignCenter:
		x += (aw - w) / 2
	case css.AlignRight:
		x += aw - w
	}
	c := r.Color
	if c.A == 0 {
		c = css.Color{A: 1}
		if e.rootBox != nil {
			c = e.rootBox.st.Color
		}
	}
	line := r.Line
	if line != css.Dotted && line != css.Dashed {
		line = css.Solid
	}
	t := r.Thickness
	if t <= 0 {
		t = hairline
	}
	st := css.Initial()
	st.Border[css.Top] = css.Border{Width: t, Style: line, Color: c}
	fb := &box{st: st, kind: kindBlock}
	p := &painter{e: e, top: &ditem{group: true, opacity: 1}, mono: -1, alpha: 1}
	p.frames = []*frame{{container: p.top, root: fb}}
	p.border(fb, st, compose.Rect{X: x, Y: top + r.SpaceAbove, Width: w, Height: t}, [4][2]float64{}, [4]float64{css.Top: t}, [4]bool{css.Top: true})
	pg.Content = append(pg.Content, e.flatten(p.top, 0, true)...)
}
