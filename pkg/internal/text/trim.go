package text

import (
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
)

// trimAdjacent sets adjacent fullwidth punctuation half width (CSS Text 4,
// 8.3, trim-adjacent, which Chromium's initial text-spacing-trim: normal
// applies): an opening punctuation after an opening or closing
// punctuation, a middle dot or an ideographic space loses the blank half
// before its glyph; a closing punctuation before a closing punctuation or
// a middle dot loses the blank half after it. A glyph takes part only when
// it is one em wide, as a fullwidth glyph of a CJK face is, and its run
// does not ask for the full width (Style.NoSpacingTrim).
//
// The trim at the start and the end of a line, which normal also applies
// to the lines after a paragraph's first, depends on where the line
// breaks and is not applied.
func (p *Paragraph) trimAdjacent() {
	n := len(p.text)
	for k := range p.items {
		it := &p.items[k]
		if it.object || it.style.NoSpacingTrim {
			continue
		}
		size := it.style.Size
		for gi := range it.glyphs {
			g := &it.glyphs[gi]
			i := g.cluster
			if g.len != 1 || i < 0 || i >= n || g.advance < size*0.95 || g.advance > size*1.05 {
				continue
			}
			c := p.text[i]
			switch {
			case strings.ContainsRune(fullwidthOpening, c) && i > 0 && trimsBefore(p.text[i-1]):
				// The glyph moves left over the half it loses.
				half := g.advance / 2
				g.advance -= half
				g.xoff -= half
				p.adv[i] -= half
			case strings.ContainsRune(fullwidthClosing, c) && i+1 < n && trimsAfter(p.text[i+1]):
				half := g.advance / 2
				g.advance -= half
				p.adv[i] -= half
			}
		}
	}
}

// trimsBefore reports a character an opening punctuation after it loses
// its blank half for.
func trimsBefore(r rune) bool {
	return strings.ContainsRune(fullwidthOpening, r) || strings.ContainsRune(fullwidthClosing, r) ||
		strings.ContainsRune(fullwidthMiddle, r) || r == '　'
}

// trimsAfter reports a character a closing punctuation before it loses
// its blank half for.
func trimsAfter(r rune) bool {
	return strings.ContainsRune(fullwidthClosing, r) || strings.ContainsRune(fullwidthMiddle, r)
}

// autospace adds the text-autospace space between an ideograph and a
// letter or digit of another script, after the first of the two in
// logical order (CSS Text 4, 8.2): ideographs are the Han, Hiragana,
// Katakana and Bopomofo characters, letters those of the other scripts
// and digits the decimal digits. Punctuation takes none. The run of the
// first character decides whether and how much.
func (p *Paragraph) autospace() {
	n := len(p.text)
	if n < 2 {
		return
	}
	// last[i] is the last glyph of the cluster that starts at i.
	last := make([]*shapedGlyph, n)
	lastItem := make([]*shapedItem, n)
	for k := range p.items {
		it := &p.items[k]
		for gi := range it.glyphs {
			g := &it.glyphs[gi]
			if c := g.cluster; c >= 0 && c < n {
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
