package layout

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/cases"
	"golang.org/x/text/language"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// runKind says what one run of an inline paragraph stands for.
type runKind uint8

const (
	runText runKind = iota
	// runAtomic is an inline block or inline replaced box.
	runAtomic
	// runTab is a preserved tab, whose width depends on where it lands.
	runTab
	// runEmpty is an inline box with no content, kept for its edges.
	runEmpty
)

// runInfo is where one run of an inline paragraph comes from.
type runInfo struct {
	kind runKind
	// box is the text box, atomic box or empty inline box of the run.
	box *box
	ws  css.WhiteSpace
}

// posBox is an inline box opening or closing at a character position, or
// a float anchored there.
type posBox struct {
	pos int
	box *box
}

// ifcPara is one paragraph of an inline formatting context: the inline
// content between two forced breaks, shaped as one bidirectional
// paragraph.
type ifcPara struct {
	p     *text.Paragraph
	runs  []text.ParagraphRun
	infos []runInfo
	n     int
	// opens lists the inline boxes opening before a character in tree
	// order, closes the ones closing after one, innermost first, and
	// floats the floats anchored before one.
	opens, closes, floats []posBox
	// abs lists the absolutely positioned boxes anchored before a
	// character, for their static position.
	abs []posBox
	// forced reports a paragraph ended by a forced break, which makes a
	// line even when empty.
	forced bool
	// clear is the clear value of the line break that ends the paragraph:
	// the next line moves below the floats it names.
	clear css.Clear
	// openAt and closeAt index opens and closes by position, openPos and
	// closePos by box.
	openAt, closeAt   map[int][]*box
	openPos, closePos map[*box]int
}

// index builds the position indexes of p.
func (p *ifcPara) index() {
	p.openAt, p.closeAt = map[int][]*box{}, map[int][]*box{}
	p.openPos, p.closePos = map[*box]int{}, map[*box]int{}
	for _, o := range p.opens {
		p.openAt[o.pos] = append(p.openAt[o.pos], o.box)
		p.openPos[o.box] = o.pos
	}
	for _, c := range p.closes {
		p.closeAt[c.pos] = append(p.closeAt[c.pos], c.box)
		p.closePos[c.box] = c.pos
	}
}

// ifcData is the inline content of a block container, collected and shaped
// once.
type ifcData struct {
	paras []*ifcPara
	// boxes lists the boxes of the context in tree order, order their
	// indices in it.
	boxes []*box
	order map[*box]int
}

// collector walks an inline formatting context in tree order, applying the
// white space processing of CSS Text 3, 4.1.1 across inline box boundaries
// and text-transform (CSS Text 3, 2.1).
type collector struct {
	e    *engine
	root *box
	d    *ifcData
	cur  *ifcPara
	// space reports that the last character kept is a collapsible space,
	// or that the paragraph has kept none: a collapsible space there is
	// removed.
	space bool
	// prev is the last character kept, for capitalize.
	prev rune
	// bidi lists the inline boxes open whose unicode-bidi opens explicit
	// formatting characters, outermost first.
	bidi []*bidiScope
}

// ifcOf returns the inline content of b, collected and shaped on first
// use.
func (e *engine) ifcOf(b *box) *ifcData {
	if b.ifc != nil {
		return b.ifc
	}
	c := &collector{e: e, root: b, d: &ifcData{}, space: true}
	c.cur = &ifcPara{}
	for _, k := range b.kids {
		c.walk(k)
	}
	c.d.paras = append(c.d.paras, c.cur)
	b.ifc = c.d
	c.d.order = make(map[*box]int, len(c.d.boxes))
	for i, k := range c.d.boxes {
		c.d.order[k] = i
	}
	for _, p := range c.d.paras {
		if e.err != nil {
			break
		}
		p.index()
		p.transformSpaces()
		e.shapePara(b, p)
	}
	return b.ifc
}

// walk collects the box k and its descendants.
func (c *collector) walk(k *box) {
	if c.e.err != nil {
		return
	}
	c.d.boxes = append(c.d.boxes, k)
	switch {
	case k.float != css.FloatNone:
		c.cur.floats = append(c.cur.floats, posBox{pos: c.cur.n, box: k})
	case k.abs:
		c.cur.abs = append(c.cur.abs, posBox{pos: c.cur.n, box: k})
	case k.kind == kindText:
		c.text(k)
	case k.kind == kindBreak:
		c.cur.clear = k.st.Clear
		c.breakPara()
	case k.kind == kindInline:
		start := c.cur.n
		startPara := c.cur
		c.cur.opens = append(c.cur.opens, posBox{pos: c.cur.n, box: k})
		c.openBidi(k)
		for _, kid := range k.kids {
			c.walk(kid)
		}
		if c.cur == startPara && c.cur.n == start && hasInlineEdges(k) {
			// An empty inline box with no margin, border or padding makes
			// no line (CSS 2.1, 9.4.2).
			c.object(runEmpty, k)
		}
		c.closeBidi(k)
		c.cur.closes = append(c.cur.closes, posBox{pos: c.cur.n, box: k})
	default:
		c.object(runAtomic, k)
		c.space = false
		c.prev = 0
	}
}

// hasInlineEdges reports whether the inline box b takes room at its
// start or end: a margin, border or padding on an edge it draws. A piece
// of an inline box split around a block draws its start edge only when
// it starts the box and its end edge only when it ends it (CSS 2.1,
// 9.2.1.1).
func hasInlineEdges(b *box) bool {
	st := b.st
	start, end := css.Left, css.Right
	if st.Direction == css.RTL {
		start, end = end, start
	}
	for _, s := range []css.Side{start, end} {
		if s == start && !b.first || s == end && !b.last {
			continue
		}
		if st.Margin[s].Kind != css.Auto && st.Margin[s].Value != 0 || st.Padding[s].Value != 0 {
			return true
		}
		if bs := st.Border[s].Style; bs != css.BorderNone && bs != css.BorderHidden && st.Border[s].Width > 0 {
			return true
		}
	}
	return false
}

// object adds an object run for box b.
func (c *collector) object(kind runKind, b *box) {
	c.addRun(text.ParagraphRun{Object: &text.Object{}}, runInfo{kind: kind, box: b, ws: b.st.WhiteSpace})
	c.cur.n++
}

// breakPara ends the current paragraph at a forced break.
func (c *collector) breakPara() {
	c.cur.forced = true
	c.d.paras = append(c.d.paras, c.cur)
	c.cur = &ifcPara{}
	c.reopenBidi()
	c.space = true
}

// collapses reports a white-space value whose spaces collapse.
func collapses(ws css.WhiteSpace) bool {
	return ws == css.WhiteSpaceNormal || ws == css.Nowrap || ws == css.PreLine
}

// wraps reports a white-space value that allows soft wrapping.
func wraps(ws css.WhiteSpace) bool {
	return ws == css.WhiteSpaceNormal || ws == css.PreWrap || ws == css.PreLine || ws == css.BreakSpaces
}

// keepsSpaces reports a white-space value whose spaces never hang at a line
// end: pre does not wrap, and break-spaces wraps them like any character
// (CSS Text 3, 4.1.3).
func keepsSpaces(ws css.WhiteSpace) bool {
	return ws == css.Pre || ws == css.BreakSpaces
}

// text collects a text box: its characters after white space processing
// and text-transform.
func (c *collector) text(k *box) {
	if c.e.fontSize(k.st) <= 0 {
		return
	}
	ws := k.st.WhiteSpace
	if k.marker {
		ws = css.Pre
	}
	s := transform(k.text, k.st.TextTransform, k.lang, &c.prev)
	style := c.e.textStyle(k.st, k.lang)
	var buf strings.Builder
	flush := func() {
		if buf.Len() == 0 {
			return
		}
		for _, pc := range smallCaps(buf.String(), k.st.FontVariant == css.SmallCaps) {
			st := style
			if pc.small {
				st.Size *= smallCapsScale
			}
			c.addRun(text.ParagraphRun{Text: pc.text, Style: st}, runInfo{kind: runText, box: k, ws: ws})
			c.cur.n += utf8.RuneCountInString(pc.text)
		}
		buf.Reset()
	}
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		if collapses(ws) {
			switch {
			case r == '\n' && ws == css.PreLine:
				flush()
				c.trimSpace()
				c.breakPara()
			case r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f':
				if !c.space {
					buf.WriteRune(' ')
					c.space = true
				}
			case unicode.IsControl(r):
				buf.WriteRune(visibleControl(r))
				c.space = false
			default:
				buf.WriteRune(r)
				c.space = false
			}
			continue
		}
		switch {
		case r == '\r' || r == '\n':
			if r == '\r' && i+1 < len(rs) && rs[i+1] == '\n' {
				i++
			}
			flush()
			c.breakPara()
			c.space = false
		case r == '\t':
			flush()
			c.object(runTab, k)
			c.space = false
		case r == '\f':
		case unicode.IsControl(r):
			buf.WriteRune(visibleControl(r))
			c.space = false
		default:
			buf.WriteRune(r)
			c.space = false
		}
	}
	flush()
}

// smallCapsScale is the size synthesised small capitals are drawn at, a
// fraction of the font size: Chromium's kSmallCapsFontSizeMultiplier, the
// 0.7 em browsers use for a face without small capitals.
const smallCapsScale = 0.7

// smallCapsPiece is a piece of text drawn at the font size, or, small, in
// synthesised small capitals.
type smallCapsPiece struct {
	text  string
	small bool
}

// smallCaps splits s for font-variant: small-caps when on, synthesising
// small capitals as CSS Fonts 4, 6.6 allows for a face without them: each
// lowercase letter with an uppercase mapping becomes that uppercase letter,
// drawn at [smallCapsScale] of the size, and every other character keeps
// the size. A rune maps to one rune, so positions in the paragraph keep
// their meaning. Off, s is one piece.
func smallCaps(s string, on bool) []smallCapsPiece {
	if !on {
		return []smallCapsPiece{{text: s}}
	}
	var out []smallCapsPiece
	var b strings.Builder
	small := false
	for _, r := range s {
		up := unicode.ToUpper(r)
		lower := up != r && unicode.IsLower(r)
		if lower != small && b.Len() > 0 {
			out = append(out, smallCapsPiece{text: b.String(), small: small})
			b.Reset()
		}
		small = lower
		if lower {
			r = up
		}
		b.WriteRune(r)
	}
	if b.Len() > 0 {
		out = append(out, smallCapsPiece{text: b.String(), small: small})
	}
	return out
}

// visibleControl returns the visible glyph a control character other than
// tab, line feed, carriage return and form feed draws as (CSS Text 3, 4.1:
// it must be rendered as a visible glyph): its Control Pictures symbol for
// the C0 controls and DEL, the replacement character for the C1 controls.
func visibleControl(r rune) rune {
	switch {
	case r < 0x20:
		return 0x2400 + r
	case r == 0x7f:
		return 0x2421
	}
	return unicode.ReplacementChar
}

// transformSpaces applies word-space-transform to the text runs of p (CSS
// Text 4, 4.3): under space, ideographic spaces and the zero width spaces
// between two characters become spaces; under ideographic-space, spaces
// and those zero width spaces become ideographic spaces. A zero width
// space at either end of the paragraph or beside an atomic inline marks no
// word boundary and stays. Each replacement keeps the character count.
func (p *ifcPara) transformSpaces() {
	found := false
	for i, r := range p.runs {
		if r.Object == nil && p.infos[i].box.st.WordSpaceTransform != css.WordSpaceNone {
			found = true
		}
	}
	if !found {
		return
	}
	// flat holds every character of p, 0 for an object, with its run.
	type char struct {
		r   rune
		run int
	}
	var flat []char
	for i, r := range p.runs {
		if r.Object != nil {
			flat = append(flat, char{0, i})
			continue
		}
		for _, c := range r.Text {
			flat = append(flat, char{c, i})
		}
	}
	boundary := func(k int) bool {
		return k > 0 && k < len(flat)-1 && flat[k-1].r != 0 && flat[k+1].r != 0 && flat[k-1].r != '\u200b' && flat[k+1].r != '\u200b'
	}
	out := make([][]rune, len(p.runs))
	for k, c := range flat {
		if p.runs[c.run].Object != nil {
			continue
		}
		r := c.r
		switch p.infos[c.run].box.st.WordSpaceTransform {
		case css.WordSpaceSpace:
			if r == '\u3000' || r == '\u200b' && boundary(k) {
				r = ' '
			}
		case css.WordSpaceIdeographic:
			if r == ' ' || r == '\u200b' && boundary(k) {
				r = '\u3000'
			}
		case css.WordSpaceNone:
		}
		out[c.run] = append(out[c.run], r)
	}
	for i := range p.runs {
		if p.runs[i].Object == nil {
			p.runs[i].Text = string(out[i])
		}
	}
}

// trimSpace removes the collapsible space that ends the current paragraph,
// before a preserved segment break (CSS Text 3, 4.1.1).
func (c *collector) trimSpace() {
	p := c.cur
	if len(p.runs) == 0 {
		return
	}
	last := len(p.runs) - 1
	r := &p.runs[last]
	if r.Object != nil || !collapses(p.infos[last].ws) || !strings.HasSuffix(r.Text, " ") {
		return
	}
	r.Text = strings.TrimSuffix(r.Text, " ")
	p.n--
	for _, list := range []*[]posBox{&p.opens, &p.closes, &p.floats, &p.abs} {
		for i := range *list {
			(*list)[i].pos = min((*list)[i].pos, p.n)
		}
	}
}

// transform applies text-transform t in language lang to s; prev is the
// character before s, for capitalize, and is updated.
func transform(s string, t css.TextTransform, lang string, prev *rune) string {
	tag := language.Und
	if lang != "" {
		if tg, err := language.Parse(lang); err == nil {
			tag = tg
		}
	}
	switch t {
	case css.Uppercase:
		s = cases.Upper(tag).String(s)
	case css.Lowercase:
		s = cases.Lower(tag).String(s)
	case css.TransformNone:
	case css.Capitalize:
		// The first typographic letter unit of each word is titlecased;
		// letters, digits and apostrophes continue a word (CSS Text 3,
		// 2.1.1).
		var b strings.Builder
		for _, r := range s {
			p := *prev
			inWord := unicode.IsLetter(p) || unicode.IsDigit(p) || p == '\'' || p == '’' || unicode.Is(unicode.Mn, p)
			if unicode.IsLetter(r) && !inWord {
				b.WriteRune(unicode.ToTitle(r))
			} else {
				b.WriteRune(r)
			}
			*prev = r
		}
		return b.String()
	}
	if r, size := utf8.DecodeLastRuneInString(s); size > 0 {
		*prev = r
	}
	return s
}

// shapePara shapes paragraph p of the inline formatting context of b.
func (e *engine) shapePara(b *box, p *ifcPara) {
	if len(p.runs) == 0 {
		return
	}
	opts := text.ParagraphOptions{Direction: text.LeftToRight}
	if b.st.Direction == css.RTL {
		opts.Direction = text.RightToLeft
	}
	if b.st.UnicodeBidi == css.Plaintext {
		opts.Direction = text.DirectionAuto
	}
	para, rep, err := e.fonts.Shape(e.ctx, p.runs, opts)
	if err != nil {
		e.fail(fmt.Errorf("shaping text: %w", err))
		return
	}
	p.p = para
	var fresh []rune
	for _, r := range rep.MissingGlyphs {
		if !e.missing[r] {
			e.missing[r] = true
			fresh = append(fresh, r)
		}
	}
	if len(fresh) > 0 {
		e.warn(codeMissingGlyph, fmt.Sprintf("no font covers %q: drawn as the font's missing-glyph box", string(fresh)), -1)
	}
	if len(rep.MissingEmoji) > 0 {
		e.warnOnce(codeMissingGlyph, "colour emoji fonts are not supported: emoji no text font covers are drawn blank", -1)
	}
}
