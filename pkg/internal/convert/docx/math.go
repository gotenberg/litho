package docx

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/formula"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/omml"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// math builds an Office Math zone e, an m:oMath or m:oMathPara (ECMA-376
// Part 1, 22.1.2.77 and 22.1.2.78), in a paragraph styled ps whose runs
// take rp: a formula laid out by package formula, a replaced box with
// its own baseline. An m:oMath sits on its line by its baseline; an
// m:oMathPara is a line of its own, justified as its m:jc or the
// document's defJc says, between the math margins. Each math run takes
// its size and colour from its w:rPr as a text run does. The box is a
// Formula structure element whose alternate description is the
// equation's tokens in reading order (ISO 32000-1, 14.8.4.5).
func (d *document) math(rp resolvedPara, ps *css.Style, lineMult float64, e *ooxml.Element, sink *inlineSink) {
	styleOf := func(r *ooxml.Element) css.Style {
		props := rp.run
		if r != nil {
			direct := d.parseRunProps(r.Child(w, "rPr"))
			if direct.style != "" {
				props = props.merge(new(d.styles.charStyle(direct.style)))
			}
			props = props.merge(direct)
		}
		rs := inlineOf(ps)
		d.applyRun(&rs, props, lineMult)
		return rs
	}
	q := omml.Read(e, omml.Options{Settings: d.settings.math, Run: func(r *ooxml.Element) omml.RunStyle {
		rs := styleOf(r)
		return omml.RunStyle{Size: rs.FontSize, Color: colorOf(rs.Color)}
	}})
	if q == nil {
		return
	}
	first := styleOf(e.Find(ooxml.NSM, "r"))
	d.equation(q, d.share(&first), ps, sink)
}

// equation adds the equation q, its root styled inner, to the inline
// content of a paragraph styled ps: an inline equation on its line by its
// baseline, a display one as a line of its own.
func (d *document) equation(q *omml.Equation, inner, ps *css.Style, sink *inlineSink) {
	if d.formulas == nil {
		d.formulas = formula.NewSetter(d.fonts, d.opts.Logger)
	}
	st := q.Style()
	box, err := q.Layout(d.ctx, d.formulas, st)
	if err != nil {
		if d.ctx.Err() == nil {
			d.warn(warn.ContentUnsupported, warn.Omitted, fmt.Sprintf("an equation was left out: %v", err))
		}
		return
	}
	for _, w := range box.Warnings {
		d.warn(w.Code, w.Effect, w.Message)
	}
	node := &layout.Node{Kind: layout.Replaced, Style: inner, Role: "Formula", Alt: q.AltText()}
	if box.Width > 0 && box.Ascent+box.Descent > 0 {
		node.Picture = &layout.Picture{
			Width: box.Width, Height: box.Ascent + box.Descent, NoRatio: true,
			HasBaseline: true, Baseline: box.Ascent,
			Vector: func(_ context.Context, r typeset.Rect) []typeset.Fragment {
				return box.Draw(r.X, float64(r.Y+box.Ascent))
			},
		}
	}
	if !q.Display() {
		sink.add(node)
		return
	}
	// A display equation is a line of its own (22.1.2.78), spaced as the
	// paragraph's lines are.
	bst := *ps
	bst.Display = css.Block
	bst.TextIndent = css.Pts(0)
	bst.Margin = [4]css.Dimension{css.Pts(0), css.Pts(0), css.Pts(0), css.Pts(0)}
	bst.Padding = bst.Margin
	bst.Border = [4]css.Border{}
	bst.BackgroundColor = css.Transparent
	left, right := q.Margins()
	bst.Margin[css.Left], bst.Margin[css.Right] = css.Pts(left), css.Pts(right)
	switch q.Justify() {
	case omml.Left:
		bst.TextAlign = css.AlignLeft
	case omml.Right:
		bst.TextAlign = css.AlignRight
	case omml.Center, omml.CenterGroup:
		bst.TextAlign = css.AlignCenter
	}
	bst.TextAlignLast = bst.TextAlign
	sink.add(&layout.Node{Kind: layout.Element, Style: d.share(&bst), Children: []*layout.Node{node}})
}

// colorOf returns the colour c, its channels clamped to [0, 1].
func colorOf(c css.Color) typeset.Color {
	cl := func(v float64) float64 { return min(max(v, 0), 1) }
	return typeset.Color{R: cl(c.R), G: cl(c.G), B: cl(c.B), A: cl(c.A)}
}
