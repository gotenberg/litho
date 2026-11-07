package text

import "strings"

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
