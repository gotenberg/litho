package ods

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/intconv"
	"github.com/gotenberg/litho/pkg/internal/odf"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// maxShapeTextRunes caps the characters of one shape's text. Anti-DoS
// guard, not spec-mandated: a shape or a text box holds a few paragraphs.
const maxShapeTextRunes = 8192

// The text of a shape whose styles set no font: Liberation Serif at 12
// points.
//
// # Corpus evidence
//
//   - probe, ods: through gotenberg/gotenberg:9.4.1, the text of a custom
//     shape, a rectangle, an ellipse and a text box of a spreadsheet whose
//     styles set no font prints in Liberation Serif at 12 points.
const (
	shapeTextFamily = "Liberation Serif"
	shapeTextSize   = 12.0
)

// shapePara is one paragraph of a shape's text: its runs, and the share
// of a line's free room before it, 0 left, 0.5 centred, 1 right.
type shapePara struct {
	runs  []text.Run
	shift float64
	size  float64
}

// shapeText is the text of a drawing shape or a text box of a sheet's
// drawing layer: its paragraphs, the padding of its text area, top,
// right, bottom, left, and where the text sits in it.
type shapeText struct {
	paras []shapePara
	pad   [4]float64
	// vertical is the draw:textarea-vertical-align: "top", "middle" or
	// "bottom".
	vertical string
	// centred centres the block of lines in the area, its lines aligned
	// within it (draw:textarea-horizontal-align center).
	centred bool
}

// shapeTextOf reads the paragraphs of container, a drawing shape or the
// draw:text-box of a frame, in the graphic style p and the text
// properties of the shape's graphic style styleName (ODF 1.3, Part 3,
// 10.3 and 10.4.3; 20.167 draw:textarea-vertical-align, 20.166
// draw:textarea-horizontal-align, 20.230 fo:padding); nil when they hold
// no text. The lines wrap in the shape's box less its padding, each
// paragraph aligned as its style's fo:text-align says, and sit at the
// top, the middle or the bottom of the area: at the top by default, the
// block of lines centred across a custom shape and wherever the style
// asks.
//
// # Corpus evidence
//
//   - probe, ods: through gotenberg/gotenberg:9.4.1, the text of a
//     rectangle, an ellipse and a text box whose styles set no text area
//     starts at the shape's top left corner, without padding; that of a
//     custom shape is centred across it, its lines starting at one edge;
//     textarea-vertical-align bottom and middle put it at the bottom and
//     in the middle.
func (r *reader) shapeTextOf(container *odf.Element, styleName string, p odf.Props, custom bool) *shapeText {
	if r.fonts == nil || !hasText(container) {
		return nil
	}
	base := newFont()
	base = r.foldFont(base, r.styles.Layers(odf.ScopeContent, odf.FamilyGraphic, styleName, odf.TextProps))
	if len(base.Families) == 0 {
		base.Families = []string{shapeTextFamily}
	}
	if !hasSize(r.styles.Layers(odf.ScopeContent, odf.FamilyGraphic, styleName, odf.TextProps)) {
		base.Size = shapeTextSize
	}
	st := &shapeText{vertical: p.Get("draw:textarea-vertical-align"), centred: custom}
	switch p.Get("draw:textarea-horizontal-align") {
	case "center":
		st.centred = true
	case "left", "right", "justify":
		st.centred = false
	}
	for i, side := range [4]string{"top", "right", "bottom", "left"} {
		if v, ok := odf.Length(p.Get("fo:padding-" + side)); ok {
			st.pad[i] = max(v, 0)
		} else if v, ok := odf.Length(p.Get("fo:padding")); ok {
			st.pad[i] = max(v, 0)
		}
	}
	n := 0
	for _, c := range container.Children {
		if !c.Is(odf.NSText, "p") && !c.Is(odf.NSText, "h") {
			continue
		}
		name := c.A(odf.NSText, "style-name")
		f := r.foldFont(base, r.styles.Layers(odf.ScopeContent, odf.FamilyParagraph, name, odf.TextProps))
		para := shapePara{size: f.Size}
		switch r.styles.Resolve(odf.ScopeContent, odf.FamilyParagraph, name, odf.ParagraphProps).Get("fo:text-align") {
		case "center":
			para.shift = 0.5
		case "end", "right":
			para.shift = 1
		}
		r.shapeRuns(c, f, &para.runs, &n, 0)
		st.paras = append(st.paras, para)
		if n > maxShapeTextRunes {
			break
		}
	}
	return st
}

// hasSize reports text property layers that set a font size.
func hasSize(layers []odf.Props) bool {
	for _, l := range layers {
		if l.Has("fo:font-size") {
			return true
		}
	}
	return false
}

// shapeRuns appends the runs of the text of el in the font f to out: a
// text:span in its own font over f (6.1.7), a text:s as its spaces
// (6.1.3), a tab as a space, a line break as a new line. n counts the
// characters read; reading stops past [maxShapeTextRunes].
func (r *reader) shapeRuns(el *odf.Element, f sheet.Font, out *[]text.Run, n *int, depth int) {
	if depth > maxTextDepth {
		return
	}
	style := runStyle(f)
	add := func(s string) {
		*n += utf8.RuneCountInString(s)
		if *n <= maxShapeTextRunes {
			*out = append(*out, text.Run{Text: s, Style: style})
		}
	}
	for _, c := range el.Children {
		if *n > maxShapeTextRunes {
			return
		}
		switch {
		case c.IsText():
			add(c.Text)
		case c.Is(odf.NSText, "s"):
			k, err := intconv.Atoi(c.A(odf.NSText, "c"))
			if err != nil || k < 1 {
				k = 1
			}
			add(strings.Repeat(" ", min(k, maxShapeTextRunes)))
		case c.Is(odf.NSText, "tab"):
			add(" ")
		case c.Is(odf.NSText, "line-break"):
			add("\n")
		case c.Is(odf.NSText, "span"):
			sf := f
			if s := r.spanFont(f, nil, c.A(odf.NSText, "style-name")); s != nil {
				sf = *s
			}
			r.shapeRuns(c, sf, out, n, depth+1)
		default:
			r.shapeRuns(c, f, out, n, depth+1)
		}
	}
}

// runStyle returns the text style of the sheet font f.
func runStyle(f sheet.Font) text.Style {
	s := text.Style{Families: f.Families, Size: f.Size, Italic: f.Italic, Color: typeset.Color{A: 1}}
	if f.Bold {
		s.Weight = 700
	}
	if f.Color.A > 0 {
		s.Color = typeset.Color{R: f.Color.R, G: f.Color.G, B: f.Color.B, A: f.Color.A}
	}
	return s
}

// draw lays the text out in box, the shape's own box, and returns its
// glyph runs. Text that does not lay out draws nothing.
func (st *shapeText) draw(ctx context.Context, fonts *text.Fonts, box typeset.Rect) []typeset.Fragment {
	avail := box.Width - st.pad[1] - st.pad[3]
	w := avail
	if w <= 0 {
		w = 1 << 16
	}
	type placed struct {
		line  text.Line
		shift float64
	}
	var lines []placed
	height, widest := 0.0, 0.0
	for _, p := range st.paras {
		runs := p.runs
		if len(runs) == 0 {
			runs = []text.Run{{Text: " ", Style: text.Style{Size: p.size, Families: []string{shapeTextFamily}}}}
		}
		ls, _, err := fonts.Layout(ctx, runs, w, text.Options{Whitespace: text.Preserve})
		if err != nil {
			continue
		}
		for _, l := range ls {
			lines = append(lines, placed{l, p.shift})
			height += l.Ascent + l.Descent + l.LineGap
			widest = max(widest, l.Width)
		}
	}
	top := box.Y + st.pad[0]
	inner := box.Height - st.pad[0] - st.pad[2]
	switch st.vertical {
	case "middle":
		top += float64((inner - height) / 2)
	case "bottom":
		top += inner - height
	}
	left, span := box.X+st.pad[3], avail
	if st.centred && widest < avail {
		left += float64((avail - widest) / 2)
		span = widest
	}
	var out []typeset.Fragment
	y := top
	for _, pl := range lines {
		l := pl.line
		y += l.Ascent
		x := left + float64((span-l.Width)*pl.shift)
		for _, run := range l.Runs {
			if run.Color.A <= 0 {
				continue
			}
			t := *run
			t.Origin = typeset.Point{X: x + run.Origin.X, Y: y}
			out = append(out, &t)
		}
		y += l.Descent + l.LineGap
	}
	return out
}

// withText returns art with the text st drawn over it in the shape's
// unturned box, turned with the shape.
func (r *reader) withText(art shapeArt, st *shapeText) shapeArt {
	lb := art.logic
	if st == nil || lb == nil {
		return art
	}
	fonts, shapeLocal := r.fonts, lb.local
	local := func(ctx context.Context, box typeset.Rect) []typeset.Fragment {
		return append(shapeLocal(ctx, box), st.draw(ctx, fonts, box)...)
	}
	nb := *lb
	nb.local = local
	m := lb.lin
	m.E, m.F = lb.x, lb.y
	art.logic = &nb
	art.draw = func(ctx context.Context) []typeset.Fragment {
		return []typeset.Fragment{&typeset.Group{Transform: m, Opacity: 1, Content: local(ctx, typeset.Rect{Width: nb.w, Height: nb.h})}}
	}
	return art
}

// textBox adds the text box tb of the frame el (ODF 1.3, Part 3, 10.4.3)
// as a drawing of its text in the frame's box, with the fill and the line
// its graphic style sets: a frame fills and strokes nothing unless its
// style asks, where a drawing shape takes LibreOffice's pool colours.
func (r *reader) textBox(ts *tableState, el, tb *odf.Element, at *sheet.Anchor) {
	if at != nil && (ts.rowHidden || ts.colHidden(at.Column)) {
		return
	}
	styleName := el.A(odf.NSDraw, "style-name")
	p := r.styles.Resolve(odf.ScopeContent, odf.FamilyGraphic, styleName, odf.GraphicProps)
	st := r.shapeTextOf(tb, styleName, p, false)
	if st == nil {
		return
	}
	paint, line := r.shapePaint(p), r.shapeLine(p)
	if v, _ := p.Lookup("draw:fill"); v == "" || v == "none" {
		paint = nil
	}
	if v, _ := p.Lookup("draw:stroke"); v == "" || v == "none" {
		line = nil
	}
	dx, dy := 0.0, 0.0
	if at != nil {
		dx, dy = ts.colLeft(at.Column), ts.rowY
	}
	art, ok := r.geometryArt(el, paint, line, dx, dy)
	if !ok {
		return
	}
	r.place(ts, el, at, r.withText(art, st))
}
