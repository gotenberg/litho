package svg

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// maxFontSize is the largest font size the text package shapes at; a
// larger one is drawn at it. Overflow guard, not spec-mandated: see
// text.Style.Size.
const maxFontSize = 1000

// posCtx is the positioning attributes of a text or tspan element (SVG
// 1.1, 10.5): its x, y, dx and dy lists, and how many of its characters
// have been placed.
type posCtx struct {
	lists [4][]length
	n     int
}

// textChar is one character of a text element with the style of the
// element it sits in and the positioning elements it sits under, outermost
// first.
type textChar struct {
	r     rune
	st    *style
	chain []*posCtx
	keep  bool
	// pre marks a character under xml:space="preserve", never collapsed.
	pre bool
}

// textCtx collects the characters of one text element.
type textCtx struct {
	chars []textChar
	over  bool
}

// text draws a text element styled st (SVG 1.1, 10): its characters and
// those of its tspan and a descendants, white space handled as xml:space
// asks (10.15), positioned by the x, y, dx and dy lists of the elements
// they sit in (10.5), shaped run by run with the text package, and
// anchored chunk by chunk by text-anchor (10.9.1). textPath, textLength
// and rotate are not drawn.
func (r *renderer) text(n *html.Node, st *style, s sink) {
	if r.d.opts.Fonts == nil {
		r.d.warn("svg-text", "text is not drawn: no fonts are available")
		return
	}
	if st.clipPath != "" {
		inner, done := r.open(s, n, st, nil)
		defer done()
		s = inner
	} else {
		s.alpha *= st.opacity
	}
	if s.alpha <= 0 {
		return
	}
	for _, p := range r.textPieces(n, st) {
		r.drawPiece(p, s)
	}
}

// textPieces collects, positions and shapes the characters of the text
// element n styled st, and returns its pieces anchored.
func (r *renderer) textPieces(n *html.Node, st *style) []*piece {
	tc := &textCtx{}
	r.collectText(n, st, nil, tc, attrNS(n, "xml", "space") == "preserve", 0)
	if tc.over {
		r.d.warn("svg-limit", fmt.Sprintf("text past %d characters was left out", maxTextRunes))
	}
	collapseSpaces(tc.chars)
	return r.layoutText(tc.chars)
}

// textOutline returns the outlines of the glyphs of the text element n
// styled st, in its user space, to be filled with the nonzero rule: the
// shape text gives a clip path (SVG 1.1, 14.3.5). Characters that are not
// visible add nothing.
func (r *renderer) textOutline(n *html.Node, st *style) []compose.PathOp {
	if r.d.opts.Fonts == nil {
		r.d.warn("svg-text", "text is not drawn: no fonts are available")
		return nil
	}
	var out []compose.PathOp
	for _, p := range r.textPieces(n, st) {
		if !p.st.visible {
			continue
		}
		for _, t := range p.runs {
			c := *t
			c.Origin = compose.Point{X: p.x + t.Origin.X, Y: p.y + t.Origin.Y}
			out = append(out, r.runOutline(&c)...)
		}
	}
	return out
}

// attrNS returns the value of n's attribute key in namespace ns.
func attrNS(n *html.Node, ns, key string) string {
	for _, a := range n.Attr {
		if a.Namespace == ns && a.Key == key {
			return a.Val
		}
	}
	return ""
}

// collectText appends the characters of the element n, styled st, under
// the positioning chain chain.
func (r *renderer) collectText(n *html.Node, st *style, chain []*posCtx, tc *textCtx, preserve bool, depth int) {
	if depth > maxDepth {
		return
	}
	pc := &posCtx{}
	for i, k := range [4]string{"x", "y", "dx", "dy"} {
		if v, ok := attrOK(n, k); ok {
			pc.lists[i], _ = parseLengthList(v, maxTextRunes)
		}
	}
	chain = append(chain[:len(chain):len(chain)], pc)
	if v := attrNS(n, "xml", "space"); v != "" {
		preserve = v == "preserve"
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		switch {
		case c.Type == html.TextNode:
			for _, ch := range c.Data {
				if r.runes >= maxTextRunes {
					tc.over = true
					return
				}
				r.runes++
				switch ch {
				case '\n', '\r':
					if !preserve {
						continue
					}
					ch = ' '
				case '\t':
					ch = ' '
				}
				tc.chars = append(tc.chars, textChar{r: ch, st: st, chain: chain, keep: true, pre: preserve})
			}
		case c.Type == html.ElementNode && c.Namespace == "svg" && (c.Data == "tspan" || c.Data == "a"):
			cs := r.d.computeStyle(c, st)
			if !cs.displayed {
				continue
			}
			r.collectText(c, cs, chain, tc, preserve, depth+1)
		case c.Type == html.ElementNode && c.Namespace == "svg" && c.Data == "textPath":
			r.d.warn("unsupported-svg-element", "text on a path is not drawn")
		}
	}
}

// collapseSpaces applies xml:space="default" (SVG 1.1, 10.15): runs of
// spaces collapse to one, and leading and trailing spaces go. Newlines
// were already removed and tabs turned into spaces as the characters were
// collected.
func collapseSpaces(chars []textChar) {
	prevSpace := true
	for i := range chars {
		if chars[i].pre {
			prevSpace = chars[i].r == ' '
			continue
		}
		if chars[i].r == ' ' {
			chars[i].keep = !prevSpace
			prevSpace = true
			continue
		}
		prevSpace = false
	}
	for i := len(chars) - 1; i >= 0; i-- {
		if !chars[i].keep {
			continue
		}
		if chars[i].r != ' ' || chars[i].pre {
			break
		}
		chars[i].keep = false
	}
}

// piece is a run of characters drawn together: one style, positioned at
// its first character.
type piece struct {
	text   []rune
	st     *style
	x, y   float64
	runs   []*compose.Text
	width  float64
	chunk  int
	anchor anchor
}

// layoutText positions and shapes the kept characters and returns their
// pieces anchored.
func (r *renderer) layoutText(chars []textChar) []*piece {
	var pieces []*piece
	var cur *piece
	x, y := 0.0, 0.0
	chunk := 0
	flush := func() {
		if cur == nil {
			return
		}
		r.shapePiece(cur)
		x += cur.width
		pieces = append(pieces, cur)
		cur = nil
	}
	first := true
	for i := range chars {
		c := &chars[i]
		if !c.keep {
			continue
		}
		var v [4]float64
		var has [4]bool
		for k := range 4 {
			for _, pc := range slices.Backward(c.chain) {
				if pc.n < len(pc.lists[k]) {
					v[k], has[k] = pc.lists[k][pc.n].resolve(r.lc(c.st), [4]axis{axisX, axisY, axisX, axisY}[k]), true
					break
				}
			}
		}
		for _, pc := range c.chain {
			pc.n++
		}
		moved := has[0] || has[1] || has[2] || has[3]
		if moved || cur == nil || !sameText(cur.st, c.st) {
			flush()
		}
		if has[0] || has[1] {
			if !first {
				chunk++
			}
		}
		first = false
		if has[0] {
			x = v[0]
		}
		if has[1] {
			y = v[1]
		}
		x += v[2]
		y += v[3]
		if cur == nil {
			cur = &piece{st: c.st, x: x, y: y, chunk: chunk, anchor: c.st.anchor}
		}
		cur.text = append(cur.text, c.r)
	}
	flush()
	r.anchor(pieces)
	return pieces
}

// sameText reports whether two styles draw text alike, so that their
// characters shape as one run, with the kerning and ligatures across them
// that a split would lose.
func sameText(a, b *style) bool {
	if a == b {
		return true
	}
	return slices.Equal(a.families, b.families) && a.fontSize == b.fontSize && a.weight == b.weight &&
		a.italic == b.italic && a.lang == b.lang && samePaint(a.fill, b.fill) && samePaint(a.stroke, b.stroke) &&
		a.fillOpacity == b.fillOpacity && a.strokeOpacity == b.strokeOpacity && a.strokeWidth == b.strokeWidth &&
		a.visible == b.visible && a.color == b.color && a.opacity == b.opacity
}

// samePaint reports whether two paints are the same.
func samePaint(a, b paint) bool {
	if a.kind != b.kind || a.color != b.color || a.url != b.url || (a.fallback == nil) != (b.fallback == nil) {
		return false
	}
	return a.fallback == nil || *a.fallback == *b.fallback
}

// anchor shifts each text chunk by its text-anchor (SVG 1.1, 10.9.1): the
// anchor of a chunk's first character decides, and the chunk's advance is
// from its first piece's start to its last piece's end.
func (r *renderer) anchor(pieces []*piece) {
	for i := 0; i < len(pieces); {
		j := i
		for j < len(pieces) && pieces[j].chunk == pieces[i].chunk {
			j++
		}
		a := pieces[i].anchor
		if a != anchorStart {
			w := pieces[j-1].x + pieces[j-1].width - pieces[i].x
			if a == anchorMiddle {
				w /= 2
			}
			for _, p := range pieces[i:j] {
				p.x -= w
			}
		}
		i = j
	}
}

// textStyle returns the text package style of an element styled st.
func textStyle(st *style) text.Style {
	return text.Style{
		Families: text.BrowserFamilies(st.families),
		Weight:   st.weight,
		Italic:   st.italic,
		Size:     min(st.fontSize, maxFontSize),
		Lang:     st.lang,
		Color:    compose.Black,
	}
}

// shapePiece shapes p with the text package and records its glyph runs
// and advance.
func (r *renderer) shapePiece(p *piece) {
	if !positive(p.st.fontSize) || len(p.text) == 0 {
		return
	}
	str := string(p.text)
	if !utf8.ValidString(str) {
		str = strings.ToValidUTF8(str, "�")
	}
	para, _, err := r.d.opts.Fonts.Shape(r.ctx, []text.ParagraphRun{{Text: str, Style: textStyle(p.st)}}, text.ParagraphOptions{})
	if err != nil {
		r.d.warn("svg-text", fmt.Sprintf("text was left out: %v", err))
		return
	}
	line := para.Line(0, para.Len(), false)
	for _, sp := range line.Spans {
		if sp.Text != nil {
			t := *sp.Text
			p.runs = append(p.runs, &t)
		}
	}
	p.width = line.Width
}

// drawPiece draws the glyph runs of p with its fill, then its stroke (SVG
// 1.1, 10.10). A stroke of the fill's colour and opacity, solid, strokes
// the glyph runs themselves, which keeps the text one object; any other
// stroke, a gradient's included, strokes the glyphs' outlines as paths.
func (r *renderer) drawPiece(p *piece, s sink) {
	st := p.st
	if !st.visible || len(p.runs) == 0 {
		return
	}
	box := bbox{}
	box.add(compose.Point{X: p.x, Y: p.y - st.fontSize})
	box.add(compose.Point{X: p.x + p.width, Y: p.y + st.fontSize/4})
	fill, ok := r.flatPaint(st.fill, st, box, "text fill")
	if st.fill.kind == paintNone {
		ok = false
	}
	fill.A *= st.fillOpacity * s.alpha
	ok = ok && fill.A > 0
	stroke, strokeSh := r.strokeOf(st, box)
	var outline *compose.Fill
	if strokeSh != nil {
		outline = withAlpha(&compose.Fill{Shading: strokeSh}, s.alpha)
		if outline == nil {
			stroke = nil
		}
	} else {
		stroke = strokeAlpha(stroke, s.alpha)
	}
	strokeW := 0.0
	if ok && stroke != nil && strokeSh == nil && len(stroke.Dash) == 0 && stroke.Color == fill {
		strokeW, stroke = stroke.Width, nil
	}
	if !ok && stroke == nil {
		return
	}
	for _, t := range p.runs {
		c := *t
		c.Origin = compose.Point{X: p.x + t.Origin.X, Y: p.y + t.Origin.Y}
		c.Color = fill
		if strokeW > 0 {
			c.Stroke = min(max(c.Stroke, strokeW), c.Size)
		}
		if ok {
			r.emit(s, &c)
		}
		if stroke == nil {
			continue
		}
		ops := r.runOutline(&c)
		if len(ops) == 0 {
			continue
		}
		if outline != nil {
			if o := strokeOutline(ops, stroke); len(o) > 0 {
				r.emit(s, &compose.Path{Ops: o, Fill: outline})
			}
			continue
		}
		r.emit(s, &compose.Path{Ops: ops, Stroke: stroke})
	}
}
