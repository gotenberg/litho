package layout

import (
	"math"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// jcluster is one typographic character unit of a line in visual order,
// for justification: the glyphs [g0, g1] of a line item's run, or an atomic
// inline.
type jcluster struct {
	it     *lineItem
	g0, g1 int
	r      rune
	// sep reports a word separator, hang one that hangs at the line end.
	sep, hang bool
}

// justify spreads free points over the justification opportunities of l
// (CSS Text 3, 7.1 and 7.4) and reports whether it found one. The
// opportunities follow text-justify: inter-word puts one after each word
// separator; auto, as Chromium does, adds one on each side of every
// character of the scripts that set no space between words, Han, kana
// and their punctuation (CSS Text 3, 7.4.1); inter-character puts one
// between every two typographic character units. The spaces that hang at
// the logical line end take none, wherever the bidi reordering put them,
// and nothing is added after the line's last unit.
func (s *inlineState) justify(l *lineBox, free float64) bool {
	mode := s.root.st.TextJustify
	if math.Abs(free) <= eps || mode == css.JustifyNone {
		return false
	}
	cls := lineClusters(l)
	if officeTabs(s.root.st) {
		cls = afterLastTab(l, cls, s.root.st.Direction == css.RTL)
	}
	n := len(cls)
	// opp[k] reports an opportunity between clusters k and k+1.
	opp := make([]bool, n)
	count := 0
	for k := 0; k+1 < n; k++ {
		a, b := cls[k], cls[k+1]
		if a.hang || b.hang {
			continue
		}
		switch mode {
		case css.JustifyInterCharacter:
			opp[k] = true
		case css.JustifyInterWord:
			opp[k] = a.sep
		case css.JustifyAuto, css.JustifyNone:
			opp[k] = a.sep || isCJKIdeographOrSymbol(a.r) || isCJKIdeographOrSymbol(b.r)
		}
		if opp[k] {
			count++
		}
	}
	if count == 0 {
		return false
	}
	extra := free / float64(count)
	// at holds where each opportunity lies before justification, for the
	// inline box fragments: an edge moves by the extra of the
	// opportunities before it.
	var at []float64
	for k := range n {
		if opp[k] {
			c := cls[k]
			x := c.it.x + c.it.w
			if c.it.text != nil && c.g1+1 < len(c.it.text.Glyphs) {
				x = c.it.x + c.it.text.Glyphs[c.g1+1].X*hscale(c.it.text)
			}
			at = append(at, x)
		}
	}
	moved := func(x float64) float64 {
		m := 0
		for _, a := range at {
			if a < x+eps {
				m++
			}
		}
		return x + float64(m)*extra
	}
	for _, f := range l.frags {
		f.x0, f.x1 = moved(f.x0), moved(f.x1)
	}
	// after[it][g] is the extra after glyph g of item it.
	after := map[*lineItem]map[int]bool{}
	for k := range n {
		if opp[k] {
			c := cls[k]
			if after[c.it] == nil {
				after[c.it] = map[int]bool{}
			}
			after[c.it][c.g1] = true
		}
	}
	acc := 0.0
	for _, it := range l.items {
		it.x += acc
		local := 0.0
		if it.text != nil {
			// A scaled run keeps its positions unscaled.
			hs := hscale(it.text)
			for gi := range it.text.Glyphs {
				it.text.Glyphs[gi].X += local / hs
				if after[it][gi] {
					local += extra
				}
			}
		} else if after[it][0] {
			local = extra
		}
		it.w += local
		acc += local
	}
	return true
}

// lineClusters returns the typographic character units of l in visual
// order: a run's glyphs grouped by the cluster each starts, an atomic
// inline as one unit.
func lineClusters(l *lineBox) []jcluster {
	var out []jcluster
	for _, it := range l.items {
		if it.tab {
			continue
		}
		if it.text == nil {
			if it.atomic != nil {
				out = append(out, jcluster{it: it, r: utf8.RuneError})
			}
			continue
		}
		first := len(out)
		gs := it.text.Glyphs
		for gi := 0; gi < len(gs); gi++ {
			start := gi
			for gi+1 < len(gs) && gs[gi+1].Text == "" {
				gi++
			}
			r, _ := utf8.DecodeRuneInString(gs[start].Text)
			out = append(out, jcluster{it: it, g0: start, g1: gi, r: r, sep: gs[start].Text == " " || gs[start].Text == " "})
		}
		// The spaces that hang are the logically last separators: the
		// visually first ones of a right-to-left run.
		own := out[first:]
		drop := it.hang
		if it.rtl {
			for k := 0; k < len(own) && drop > 0; k++ {
				if own[k].sep {
					own[k].hang = true
					drop--
				}
			}
		} else {
			for k := len(own) - 1; k >= 0 && drop > 0; k-- {
				if own[k].sep {
					own[k].hang = true
					drop--
				}
			}
		}
	}
	return out
}

// isCJKIdeographOrSymbol reports a character of the scripts that set no
// space between words, around which Chromium justifies: CJK ideographs,
// radicals, strokes, kana, CJK symbols and punctuation, enclosed and
// compatibility forms, and fullwidth forms (Blink's
// Character::IsCJKIdeographOrSymbol).
func isCJKIdeographOrSymbol(r rune) bool {
	switch {
	case r >= 0x2E80 && r <= 0x2FDF, // CJK and Kangxi radicals
		r >= 0x2FF0 && r <= 0x303F,   // ideographic description, CJK symbols and punctuation
		r >= 0x3040 && r <= 0x30FF,   // hiragana, katakana
		r >= 0x3190 && r <= 0x31FF,   // kanbun, strokes, katakana extension
		r >= 0x3200 && r <= 0x33FF,   // enclosed CJK, compatibility
		r >= 0x3400 && r <= 0x4DBF,   // extension A
		r >= 0x4E00 && r <= 0x9FFF,   // unified ideographs
		r >= 0xF900 && r <= 0xFAFF,   // compatibility ideographs
		r >= 0xFE30 && r <= 0xFE4F,   // CJK compatibility forms
		r >= 0xFF00 && r <= 0xFFEF,   // halfwidth and fullwidth forms
		r >= 0x20000 && r <= 0x3FFFF: // supplementary ideographs
		return true
	}
	return false
}

// shrinkWordOverflow is the share of the word a shrinking justified line
// takes, with the space before it, that may reach past the line's end at
// natural spacing: Word moves a word onto a line by narrowing the spaces
// only when at least two thirds of it and its space fit unshrunk, a
// measurement of Word, which ECMA-376 leaves to the application (Part 1,
// 17.18.44 ST_Jc both).
//
// # Corpus evidence
//
//   - litho/docx paper-*: of 345 lines Word justifies in Calibri 11 points
//     across the paper matrix, a rule of at most 22% of each space and at
//     most a third of the word and its space past the end breaks 337 as
//     Word does; a 20% space limit alone breaks 286, LibreOffice's 25%
//     alone 254. Most of the 8 others are within the difference between
//     the advances Word places Calibri's glyphs at, up to 0.2 point a
//     glyph off the font's at 11 points, and Carlito's.
const shrinkWordOverflow = 1.0 / 3

// shrinkBreak returns where the justified line from start breaks when its
// word separators may shrink (css.Style.JustifyShrink), end being where it
// breaks at natural spacing. The line takes the next word when the part
// of it past avail fits in the room the separators give up, that is
// JustifyShrink of each, and is at most shrinkWordOverflow of the width
// of the word and the spaces before it. It takes one word at most, and
// none from a line holding a tab.
func (e *engine) shrinkBreak(p *ifcPara, w []float64, start, end int, x0, avail float64, root *box, org float64) int {
	spaces, space := 0, 0.0
	for i := start; i < end; i++ {
		if p.infos[p.p.RunOf(i)].kind == runTab {
			return end
		}
		if p.p.Rune(i) == ' ' {
			spaces++
			space = w[i]
		}
	}
	for i := end - 1; i >= start && p.p.Rune(i) == ' '; i-- {
		spaces--
	}
	if spaces <= 0 || space <= 0 {
		return end
	}
	more := root.st.JustifyShrink * space * float64(spaces+1)
	end2, fits := e.breakLineIn(p, w, start, x0, avail+more, root, org)
	if !fits || end2 <= end {
		return end
	}
	next := end + 1
	for next < end2 && !p.allowed(next) {
		next++
	}
	for i := start; i < next; i++ {
		if p.infos[p.p.RunOf(i)].kind == runTab {
			return end
		}
	}
	last := next
	for last > end && p.hangs(last-1) {
		last--
	}
	from := end
	for from > start && p.p.Rune(from-1) == ' ' {
		from--
	}
	x, word := x0, 0.0
	for i := start; i < last; i++ {
		x += w[i]
		if i >= from {
			word += w[i]
		}
	}
	if x-avail > shrinkWordOverflow*word+eps {
		return end
	}
	return next
}
