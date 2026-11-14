package text

import (
	"slices"
	"strings"
	"unicode"
)

// The fullwidth punctuation classes of CSS Text 4, 8.3 (text-spacing-trim).
const (
	// fullwidthOpening is the fullwidth opening punctuation: its glyph
	// sits on the right half of its em.
	fullwidthOpening = "〈《「『【〔〖〘〚（［｛｟〝‘“"
	// fullwidthClosing is the fullwidth closing punctuation, the
	// ideographic comma and full stop and the fullwidth comma and full
	// stop among it: its glyph sits on the left half of its em.
	fullwidthClosing = "〉》」』】〕〗〙〛）］｝｠〞〟、。，．’”"
	// fullwidthMiddle is the fullwidth middle dot punctuation, centred.
	fullwidthMiddle = "・：；·‧"
	// inkClassed are the punctuation characters whose class the position
	// of their glyph decides, as Chromium reads it: a face draws them on
	// the left half of the em or centred, by its language, and CSS Text 4,
	// 8.3 classes fullwidth punctuation by where its glyph sits.
	inkClassed = "、。，．：；"
)

// punct is the class of a character of fullwidth punctuation for
// text-spacing-trim.
type punct uint8

const (
	punctNone punct = iota
	punctOpen
	punctClose
	punctMiddle
	// punctSpace is the ideographic space.
	punctSpace
	// punctOpenQuote and punctCloseQuote are the quotation marks ‘ “ and
	// ’ ” a face draws narrower than an em, as the Japanese and Korean
	// Noto CJK faces draw them: they are trimmed nothing themselves.
	punctOpenQuote
	punctCloseQuote
)

// cjkPunctuation reports a character of the classes text-spacing-trim
// reads: the quotation marks, the CJK symbols and punctuation block, the
// halfwidth and fullwidth forms up to the halfwidth katakana, and the
// middle dots.
func cjkPunctuation(r rune) bool {
	return r >= '\u2018' && r <= '\u201D' || r >= '\u3000' && r <= '\u303F' ||
		r >= '\uFF00' && r <= '\uFF65' || r == '\u00B7' || r == '\u2027'
}

// inkClass returns the class the ink of a glyph of an ink-classed
// punctuation gives it, from the centre of its ink, at x past the pen
// position, in an advance of adv: the left third is closing, the right
// third opening, the rest centred.
func inkClass(x, adv float64) punct {
	switch {
	case !(adv > 0):
		return punctNone
	case x < adv/3:
		return punctClose
	case x > 2*adv/3:
		return punctOpen
	}
	return punctMiddle
}

// trimAdjacent sets adjacent fullwidth punctuation half width (CSS Text 4,
// 8.3, trim-adjacent, which Chromium's initial text-spacing-trim: normal
// applies). A run under Chromium's fallback rule is trimmed as Chromium
// trims it, any other by the classes alone ([Paragraph.trimByClass]). As
// Chromium does it: an opening punctuation after an opening or
// closing punctuation, a middle dot, an ideographic space or an opening
// quotation mark loses the blank half before its glyph; a closing
// punctuation before a closing punctuation, a middle dot, an ideographic
// space or a closing quotation mark loses the blank half after it. A
// punctuation is fullwidth when its glyph is one em wide, as a CJK face
// draws it; a quotation mark narrower than an em keeps its role above,
// the one Chromium gives it, unless the face of the punctuation trimmed
// draws its own quotation marks fullwidth ([quoteAs]). The ideographic
// comma, full stop, colon and semicolon take their class from where
// their glyph sits ([inkClass]), which the face and its language decide.
//
// A run that asks for the full width (Style.NoSpacingTrim) or turns on
// the palt or halt feature, whose alternate widths Chromium then draws in
// place of any trim, is trimmed nothing.
//
// The trim at the start and the end of a line, which normal also applies
// to the lines after a paragraph's first, depends on where the line
// breaks and is not applied.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1, 40 px sans-serif:
//     in Japanese the colon and semicolon are centred and the comma and
//     full stop closing; in Simplified Chinese all four closing; in
//     Traditional Chinese all four centred. In Japanese, where “ and ”
//     are 19 px wide, 「 after “ is trimmed and after ” not, 」 before ”
//     is trimmed and before “ not; 」 before an ideographic space is
//     trimmed. font-feature-settings: "palt" or "halt" turns every trim
//     off, "palt" 0, "liga" 0, "kern" 0 and "chws" do not.
//   - probe, html: under lang="zh-Hans", in "Liberation Sans", sans-serif,
//     Chromium trims 《 after a ” Liberation Sans draws 13.33 px wide at
//     40 px: Noto Sans CJK SC, which draws 《, draws its quotation marks
//     fullwidth.
func (p *Paragraph) trimAdjacent() {
	n := len(p.text)
	// class[i] is the class of the character at i, drawn by one glyph,
	// before any trim; nil until a paragraph holds a punctuation.
	var class []punct
	for k := range p.items {
		it := &p.items[k]
		if it.object {
			continue
		}
		for gi := range it.glyphs {
			g := &it.glyphs[gi]
			i := g.cluster()
			if g.clusterLen() != 1 || i < 0 || i >= n {
				continue
			}
			c := p.classOf(i, fullwidthGlyph(g.advance, it.style.Size))
			if c == punctNone {
				continue
			}
			if class == nil {
				class = make([]punct, n)
			}
			class[i] = c
		}
	}
	if class == nil {
		return
	}
	for k := range p.items {
		it := &p.items[k]
		if it.object || it.style.NoSpacingTrim {
			continue
		}
		if it.style.Fallback.rule() != FallbackChromium {
			p.trimByClass(it)
			continue
		}
		if alternateWidths(it.style) {
			continue
		}
		for gi := range it.glyphs {
			g := &it.glyphs[gi]
			i := g.cluster()
			if g.clusterLen() != 1 || i < 0 || i >= n {
				continue
			}
			switch class[i] { //nolint:exhaustive // only an opening or closing punctuation is trimmed
			case punctOpen:
				if i == 0 {
					continue
				}
				switch quoteAs(class[i-1], it.fullQuotes) { //nolint:exhaustive // the classes that trim an opening punctuation after them
				case punctOpen, punctClose, punctMiddle, punctSpace, punctOpenQuote:
					// The glyph moves left over the half it loses.
					half := float64(g.advance / 2)
					g.advance -= half
					g.xoff -= half
					p.adv[i] -= half
				}
			case punctClose:
				if i+1 >= n {
					continue
				}
				switch quoteAs(class[i+1], it.fullQuotes) { //nolint:exhaustive // the classes that trim a closing punctuation before them
				case punctClose, punctMiddle, punctSpace, punctCloseQuote:
					half := float64(g.advance / 2)
					g.advance -= half
					p.adv[i] -= half
				}
			}
		}
	}
}

// trimByClass trims the fullwidth punctuation of it by the classes of CSS
// Text 4, 8.3 alone, whatever the width of a neighbour: an opening
// punctuation after an opening or closing punctuation, a middle dot or an
// ideographic space, a closing punctuation before a closing punctuation or
// a middle dot. The runs of the converters that follow no browser take
// this rule.
func (p *Paragraph) trimByClass(it *shapedItem) {
	n := len(p.text)
	for gi := range it.glyphs {
		g := &it.glyphs[gi]
		i := g.cluster()
		if g.clusterLen() != 1 || i < 0 || i >= n || !fullwidthGlyph(g.advance, it.style.Size) {
			continue
		}
		c := p.text[i]
		switch {
		case strings.ContainsRune(fullwidthOpening, c) && i > 0 && trimsBefore(p.text[i-1]):
			// The glyph moves left over the half it loses.
			half := float64(g.advance / 2)
			g.advance -= half
			g.xoff -= half
			p.adv[i] -= half
		case strings.ContainsRune(fullwidthClosing, c) && i+1 < n && trimsAfter(p.text[i+1]):
			half := float64(g.advance / 2)
			g.advance -= half
			p.adv[i] -= half
		}
	}
}

// trimsBefore reports a character an opening punctuation after it loses
// its blank half for, under [Paragraph.trimByClass].
func trimsBefore(r rune) bool {
	return strings.ContainsRune(fullwidthOpening, r) || strings.ContainsRune(fullwidthClosing, r) ||
		strings.ContainsRune(fullwidthMiddle, r) || r == '\u3000'
}

// trimsAfter reports a character a closing punctuation before it loses
// its blank half for, under [Paragraph.trimByClass].
func trimsAfter(r rune) bool {
	return strings.ContainsRune(fullwidthClosing, r) || strings.ContainsRune(fullwidthMiddle, r)
}

// quoteAs returns the class c of a neighbour has for a punctuation of a
// face that draws its quotation marks fullwidth when full is set: a
// narrower quotation mark next to it, which another face draws, is then
// fullwidth too, as Chromium reads the marks' width from the face of the
// punctuation it trims.
func quoteAs(c punct, full bool) punct {
	switch {
	case full && c == punctOpenQuote:
		return punctOpen
	case full && c == punctCloseQuote:
		return punctClose
	}
	return c
}

// classOf returns the class of the character at i, whose glyph is one em
// wide when wide is set: a punctuation narrower than an em is no fullwidth
// punctuation, a quotation mark excepted.
func (p *Paragraph) classOf(i int, wide bool) punct {
	r := p.text[i]
	switch {
	case r == '“' || r == '‘':
		if wide {
			return punctOpen
		}
		return punctOpenQuote
	case r == '”' || r == '’':
		if wide {
			return punctClose
		}
		return punctCloseQuote
	case !wide:
		return punctNone
	case r == '\u3000':
		return punctSpace
	case p.ink != nil && p.ink[i] != punctNone && strings.ContainsRune(inkClassed, r):
		return p.ink[i]
	case strings.ContainsRune(fullwidthOpening, r):
		return punctOpen
	case strings.ContainsRune(fullwidthClosing, r):
		return punctClose
	case strings.ContainsRune(fullwidthMiddle, r):
		return punctMiddle
	}
	return punctNone
}

// fullwidthGlyph reports an advance of one em at size, give or take the
// rounding of a face's units.
func fullwidthGlyph(advance, size float64) bool {
	return advance >= size*0.95 && advance <= size*1.05
}

// alternateTags are the features of the proportional and half-width
// alternates of CJK punctuation (OpenType feature registry: palt, halt,
// vpal, vhal).
var alternateTags = [...]string{"palt", "halt", "vpal", "vhal"}

// alternateWidths reports a style whose font-feature-settings turn one of
// [alternateTags] on, the last setting of a tag deciding.
func alternateWidths(st Style) bool {
	var on [len(alternateTags)]bool
	for _, f := range st.settings().Features {
		if k := slices.Index(alternateTags[:], f.Tag); k >= 0 {
			on[k] = f.Value != 0
		}
	}
	return slices.Contains(on[:], true)
}

// autospace adds the text-autospace space between an ideograph and a
// letter or digit of another script, after the first of the two in
// logical order (CSS Text 4, 8.2): ideographs are the Han, Hiragana,
// Katakana and Bopomofo characters, letters those of the other scripts
// and digits the decimal digits. Punctuation takes none. The run of the
// first character decides whether and how much.
func (p *Paragraph) autospace() {
	n := len(p.text)
	if n < 2 || !slices.ContainsFunc(p.items, func(it shapedItem) bool { return it.style.Autospace != 0 }) {
		return
	}
	// last[i] is the last glyph of the cluster that starts at i.
	last := make([]*shapedGlyph, n)
	lastItem := make([]*shapedItem, n)
	for k := range p.items {
		it := &p.items[k]
		for gi := range it.glyphs {
			g := &it.glyphs[gi]
			if c := g.cluster(); c >= 0 && c < n {
				last[c], lastItem[c] = g, it
			}
		}
	}
	prev := -1
	for i := range n {
		if last[i] == nil {
			continue
		}
		if prev >= 0 {
			st := lastItem[prev].style
			if st.Autospace != 0 && autospaced(p.text[prev], p.text[i], st) {
				last[prev].advance += st.Autospace
				p.adv[prev] += st.Autospace
			}
		}
		prev = i
	}
}

// autospaced reports whether st puts the autospace between a and b.
func autospaced(a, b rune, st Style) bool {
	ia, ib := isIdeograph(a), isIdeograph(b)
	if ia == ib {
		return false
	}
	other := b
	if ib {
		other = a
	}
	switch {
	case unicode.IsDigit(other):
		return st.AutospaceNumeric
	case unicode.IsLetter(other):
		return st.AutospaceAlpha
	}
	return false
}

// isIdeograph reports a character of the scripts text-autospace spaces
// from the others.
func isIdeograph(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Bopomofo)
}
