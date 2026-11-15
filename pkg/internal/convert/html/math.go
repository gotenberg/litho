package html

import (
	"context"
	"fmt"
	"strings"

	dom "golang.org/x/net/html"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/formula"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/typography/mathtable"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// mathElement builds the nodes of the math element n, styled st, whose
// parent is styled parent (MathML Core, 2.1.1 and 4.1): a replaced box
// holding the formula laid out by [formula.Setter], on its line by the
// formula's own baseline under display: inline math, and centred in a
// block of the element's style under display: block math. Each MathML
// element of the formula takes the style the cascade computed for it,
// the user agent stylesheet of MathML Core among the sheets. The box is
// a Formula structure element whose alternate description is the alttext
// attribute, or the tokens in reading order (ISO 32000-1, 14.8.4.5).
func (b *builder) mathElement(ctx context.Context, n *dom.Node, st *css.Style) []*layout.Node {
	if st == nil || st.Display == css.DisplayNone {
		return nil
	}
	lang := b.langOf(n)
	count := 0
	root := b.formulaTree(ctx, n, lang, 0, &count)
	if root == nil {
		return nil
	}
	// The math element's own padding, border and background belong to
	// the box the layout gives it.
	rootStyle := *root.Style
	rootStyle.Padding, rootStyle.Border, rootStyle.Margin = [4]float64{}, [4]float64{}, [4]float64{}
	rootStyle.Background = typeset.Color{}
	rootStyle.HasWidth, rootStyle.HasHeight = false, false
	root.Style = &rootStyle
	box, err := b.res.formulas.Layout(ctx, root, rootStyle)
	if err != nil {
		if ctx.Err() == nil {
			b.warnings = append(b.warnings, typeset.Warning{Code: warn.ContentUnsupported, Effect: warn.Omitted, Message: fmt.Sprintf("a formula was left out: %v", err)})
		}
		return nil
	}
	b.warnings = append(b.warnings, box.Warnings...)
	block := st.Display != css.Inline
	inner := st
	if block {
		inner = anonInline(st)
	}
	node := &layout.Node{Kind: layout.Replaced, Style: inner, Role: "Formula", Alt: formula.AltText(root)}
	switch h := box.Ascent + box.Descent; {
	case box.Width > 0 && h > 0:
		node.Picture = &layout.Picture{
			Width: box.Width, Height: box.Ascent + box.Descent, NoRatio: true,
			HasBaseline: true, Baseline: box.Ascent,
			Vector: func(_ context.Context, r typeset.Rect) []typeset.Fragment {
				return box.Draw(r.X, float64(r.Y+box.Ascent))
			},
		}
	case box.Width > 0 || h > 0:
		// The layout draws no picture of no area. A formula of no height,
		// an mspace of a width alone, still takes its width on the line,
		// and one of no width its height (MathML Core, 3.2.5): the size
		// of a replaced box without a picture, where CSS sizes nothing.
		sized := *inner
		if sized.Width.Kind == css.Auto {
			sized.Width = css.Pts(box.Width)
		}
		if sized.Height.Kind == css.Auto {
			sized.Height = css.Pts(max(h, 0))
		}
		sized.BoxSizing = css.ContentBox
		node.Style = &sized
	}
	if !block {
		node.ID, _ = attr(n, "id")
		return []*layout.Node{node}
	}
	// A block formula is centred in its block (MathML Core, 2.1.1). The
	// block's line holds the formula alone: a line height of 0 leaves
	// the strut of the block's font no height, so the line is as tall as
	// the formula, as Chromium's block math box is.
	bst := *st
	bst.TextAlign, bst.TextAlignLast = css.AlignCenter, css.AlignCenter
	bst.LineHeight = css.Pts(0)
	bst.TextIndent = css.Dimension{}
	wrapper := &layout.Node{Kind: layout.Element, Style: &bst, Children: []*layout.Node{node}}
	wrapper.ID, _ = attr(n, "id")
	return []*layout.Node{wrapper}
}

// formulaTree returns the formula element of the MathML element n, its
// style the cascade's, nil when CSS displays it none. Text and elements
// outside MathML count as the text of a token, the content of an
// element of another kind is left out; depth is n's nesting under the
// math element and count the elements met, against the bounds of
// [formula.MaxDepth] and [formula.MaxElements].
func (b *builder) formulaTree(ctx context.Context, n *dom.Node, lang string, depth int, count *int) *formula.Element {
	st := b.computed.Style(n)
	if st == nil || st.Display == css.DisplayNone || depth > 0 && (st.Position == css.Absolute || st.Position == css.Fixed) {
		// A child CSS displays none, or positions out of the flow, is no
		// in-flow child of its parent (MathML Core, 3.1.2); litho draws
		// the absolutely positioned children of a formula nowhere.
		return nil
	}
	*count++
	if *count%1024 == 0 && ctx.Err() != nil {
		return nil
	}
	if *count > formula.MaxElements || depth > formula.MaxDepth {
		// The formula layout warns of what lies past its bounds; the tree
		// stops just past them.
		return &formula.Element{Name: "mrow"}
	}
	if l, ok := attr(n, "lang"); ok && strings.TrimSpace(l) != "" {
		lang = strings.TrimSpace(l)
	}
	e := &formula.Element{Name: strings.ToLower(n.Data), Style: formulaStyle(st, lang)}
	for _, a := range n.Attr {
		if a.Namespace == "" {
			e.Attrs = append(e.Attrs, formula.Attr{Name: strings.ToLower(a.Key), Value: a.Val})
		}
	}
	switch e.Name {
	case "mi", "mn", "mo", "mtext", "ms", "annotation", "annotation-xml":
		e.Text = textContent(n)
		return e
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != dom.ElementNode || c.Namespace != "math" {
			continue
		}
		if k := b.formulaTree(ctx, c, lang, depth+1, count); k != nil {
			e.Children = append(e.Children, k)
		}
	}
	return e
}

// formulaStyle returns the formula style of the computed style st of a
// MathML element in language lang.
func formulaStyle(st *css.Style, lang string) *formula.Style {
	color := func(c css.Color) typeset.Color {
		cl := func(v float64) float64 { return min(max(v, 0), 1) }
		return typeset.Color{R: cl(c.R), G: cl(c.G), B: cl(c.B), A: cl(c.A)}
	}
	fs := &formula.Style{
		Families: text.BrowserFamilies(st.FontFamilies, lang),
		Size:     min(max(st.FontSize, 0.01), 1000),
		Weight:   st.FontWeight,
		Italic:   st.FontStyle != css.FontNormal,
		Lang:     lang,
		Fallback: text.FallbackChromium,
		Color:    color(st.Color),
		Display:  st.MathStyle == css.MathNormal,
		Cramped:  st.MathShift == css.MathShiftCompact,
		Depth:    int(st.MathDepth),
		MathAuto: st.TextTransform == css.MathAuto,
		Hidden:   st.Visibility != css.Visible,
		RTL:      st.Direction == css.RTL,
	}
	if bg := color(st.BackgroundColor); bg.A > 0 {
		fs.Background = bg
	}
	if d := st.Width; d.Kind == css.Points {
		fs.Width, fs.HasWidth = max(d.Value, 0), true
		if st.BoxSizing == css.BorderBox {
			fs.Width = max(float64(fs.Width-edgeSum(st, css.Left, css.Right)), 0)
		}
	}
	if d := st.Height; d.Kind == css.Points {
		fs.Height, fs.HasHeight = max(d.Value, 0), true
		if st.BoxSizing == css.BorderBox {
			fs.Height = max(float64(fs.Height-edgeSum(st, css.Top, css.Bottom)), 0)
		}
	}
	for side := range 4 {
		if d := st.Padding[side]; d.Kind == css.Points {
			fs.Padding[side] = max(d.Value, 0)
		}
		if d := st.Margin[side]; d.Kind == css.Points {
			fs.Margin[side] = d.Value
		}
		if bd := st.Border[side]; bd.Style != css.BorderNone && bd.Style != css.BorderHidden && bd.Width > 0 {
			fs.Border[side] = bd.Width
			fs.BorderColor[side] = color(bd.Color)
		}
	}
	return fs
}

// mathScaleDowns returns whether the face fc has an OpenType MATH table
// and its ScriptPercentScaleDown and ScriptScriptPercentScaleDown as
// fractions, which font-size: math reads (MathML Core, 4.5).
func mathScaleDowns(ctx context.Context, fc *text.Face) (has bool, script, scriptScript float64) {
	ts, err := fc.Typeset(ctx)
	if err != nil {
		return false, 0, 0
	}
	t, err := mathtable.Parse(ts.Program)
	if err != nil {
		return false, 0, 0
	}
	a, _ := t.Constant(mathtable.ScriptPercentScaleDown)       // 0 without MathConstants: the fallback
	c, _ := t.Constant(mathtable.ScriptScriptPercentScaleDown) // likewise
	return true, float64(a) / 100, float64(c) / 100
}

// edgeSum returns the padding and border widths of sides a and b of st,
// for a border-box size.
func edgeSum(st *css.Style, a, b css.Side) float64 {
	sum := 0.0
	for _, side := range [2]css.Side{a, b} {
		if d := st.Padding[side]; d.Kind == css.Points {
			sum += d.Value
		}
		if bd := st.Border[side]; bd.Style != css.BorderNone && bd.Style != css.BorderHidden {
			sum += bd.Width
		}
	}
	return sum
}
