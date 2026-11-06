package svg

import (
	"fmt"
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
	tc := &textCtx{}
	r.collectText(n, st, nil, tc, attrNS(n, "xml", "space") == "preserve", 0)
	if tc.over {
		r.d.warn("svg-limit", fmt.Sprintf("text past %d characters was left out", maxTextRunes))
	}
	collapseSpaces(tc.chars)
	r.layoutText(tc.chars, st, s)
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
			prevSpace = false
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

// layoutText positions and shapes the kept characters and draws them.
func (r *renderer) layoutText(chars []textChar, st *style, s sink) {
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
			for j := len(c.chain) - 1; j >= 0; j-- {
				pc := c.chain[j]
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
		if moved || cur == nil || cur.st != c.st {
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
	for _, p := range pieces {
		r.drawPiece(p, s)
	}
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
	if !(p.st.fontSize > 0) || len(p.text) == 0 {
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

// drawPiece draws the glyph runs of p with its fill (SVG 1.1, 10.10). A
// stroke the same colour as the fill strokes the glyphs; any other stroke
// is left out with a warning, since a glyph run strokes in its fill
// colour.
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
	strokeW := 0.0
	if st.stroke.kind != paintNone && st.strokeOpacity > 0 {
		w := st.strokeWidth.resolve(r.lc(st), axisDiag)
		sc, sok := r.flatPaint(st.stroke, st, box, "text stroke")
		switch {
		case !(w > 0) || !sok:
		case ok && sc.R == fill.R && sc.G == fill.G && sc.B == fill.B:
			strokeW = w
		default:
			r.d.warn("unsupported-svg-paint", "a text stroke unlike its fill is not drawn")
		}
	}
	if !ok || fill.A <= 0 {
		return
	}
	for _, t := range p.runs {
		c := *t
		c.Origin = compose.Point{X: p.x + t.Origin.X, Y: p.y + t.Origin.Y}
		c.Color = fill
		if strokeW > 0 {
			c.Stroke = min(max(c.Stroke, strokeW), c.Size)
		}
		r.emit(s, &c)
	}
}
