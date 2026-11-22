package office

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/symbolfont"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// maxSymbolTexts caps the ActualText values a [SymbolCodes] records.
// Anti-DoS guard, not spec-mandated: a document writes a few distinct
// runs of a symbol font, bullets and checkboxes, and a run past the cap
// keeps the drawn characters as its glyphs' text, which its ActualText
// names for a reader all the same.
const maxSymbolTexts = 1 << 12

// SymbolCodes maps the characters symbol runs draw in place of codes back
// to the codes, which [SymbolCodes.Restore] gives the drawn glyphs as
// their text. The character map's keys come from the recoding tables and
// the codes U+0020 to U+00FF, a few hundred at most. Build one with
// [NewSymbolCodes]; [SymbolNode] writes into it.
type SymbolCodes struct {
	codes map[rune]rune
	// actual holds the ActualText of every run recorded, which tells its
	// glyphs from those of other runs.
	actual map[string]bool
	// names maps a private-use character drawn in place of a code to the
	// character the run's ActualText gives the code, itself no
	// private-use character.
	names map[rune]rune
}

// NewSymbolCodes returns an empty [SymbolCodes].
func NewSymbolCodes() *SymbolCodes {
	return &SymbolCodes{codes: map[rune]rune{}, actual: map[string]bool{}, names: map[rune]rune{}}
}

// Restore gives every glyph of pg that draws a recorded character, in a
// run whose structure element carries the ActualText of a recorded run,
// the code the source wrote as its text: the text a symbol font's glyph
// stands for in the printed text, as Word and LibreOffice write it, while
// the ActualText names it for a reader (ISO 19005-2, 6.2.11.7.3).
//
// A run of an artifact, a header's or a footer's, has no structure
// element to carry an ActualText: a glyph of it that draws a recorded
// private-use character takes the character the ActualText would give
// it, so that no private-use value is left without its replacement text
// (ISO 19005-2, 6.2.11.7.3; ISO 14289-2, 8.4.3).
func (c *SymbolCodes) Restore(pg *typeset.Page) {
	if c == nil || len(c.codes) == 0 {
		return
	}
	c.restore(pg.Content)
}

// restore is [SymbolCodes.Restore] for one list of fragments.
func (c *SymbolCodes) restore(frags []typeset.Fragment) {
	for _, f := range frags {
		switch v := f.(type) {
		case *typeset.Text:
			if v.Parent == nil {
				c.name(v)
				continue
			}
			if !c.actual[v.Parent.ActualText] {
				continue
			}
			for i := range v.Glyphs {
				v.Glyphs[i].Text = strings.Map(func(r rune) rune {
					if src, ok := c.codes[r]; ok {
						return src
					}
					return r
				}, v.Glyphs[i].Text)
			}
		case *typeset.Group:
			c.restore(v.Content)
		}
	}
}

// name gives the glyphs of t, a run of an artifact, that draw a recorded
// private-use character the character its ActualText names.
func (c *SymbolCodes) name(t *typeset.Text) {
	if len(c.names) == 0 {
		return
	}
	for i := range t.Glyphs {
		t.Glyphs[i].Text = strings.Map(func(r rune) rune {
			if n, ok := c.names[r]; ok {
				return n
			}
			return r
		}, t.Glyphs[i].Text)
	}
}

// record notes that draw, the text of a run whose ActualText is actual,
// stands for src, character by character, and that each private-use
// character of draw is named by the character of actual in its place,
// when actual names each character of draw with one of its own.
func (c *SymbolCodes) record(actual, src, draw string) {
	if c == nil || actual == "" || len(c.actual) >= maxSymbolTexts {
		return
	}
	s := []rune(src)
	if len(s) != utf8.RuneCountInString(draw) {
		return
	}
	c.actual[actual] = true
	a := []rune(actual)
	i := 0
	for _, d := range draw {
		if _, ok := c.codes[d]; !ok && d != s[i] {
			c.codes[d] = s[i]
		}
		if _, ok := c.names[d]; !ok && len(a) == len(s) && unicode.Is(unicode.Co, d) && !unicode.Is(unicode.Co, a[i]) {
			c.names[d] = a[i]
		}
		i++
	}
}

// SymbolNode sets what tn, a text node, draws when its first family is a
// symbol font [symbolfont.Recodes] knows, after rule ([symbolfont.Draw]),
// and gives it the text its codes stand for as its ActualText: the codes
// of a symbol font name nothing a reader knows (ISO 19005-2, 6.2.11.7.3;
// ISO 14289-2, 8.4.3). It records in codes each character drawn in place
// of a code. A node that draws in OpenSymbol takes a style of its own;
// tn's style is not modified. fonts nil leaves the codes as they are.
func SymbolNode(tn *layout.Node, fonts *text.Fonts, rule symbolfont.Rule, codes *SymbolCodes) {
	if tn.Style == nil || len(tn.Style.FontFamilies) == 0 {
		return
	}
	family := tn.Style.FontFamilies[0]
	if !symbolfont.Recodes(family) {
		return
	}
	tn.ActualText = SymbolText(tn.Text, family)
	if fonts == nil {
		return
	}
	draw, fams, ok := symbolfont.Draw(tn.Text, family, rule, fonts.Covers)
	if !ok {
		return
	}
	if tn.ActualText == "" && draw != tn.Text {
		tn.ActualText = tn.Text
	}
	codes.record(tn.ActualText, tn.Text, draw)
	tn.Text = draw
	if fams != nil {
		st := *tn.Style
		st.FontFamilies = slices.Concat(fams, tn.Style.FontFamilies)
		tn.Style = &st
	}
}

// SymbolDrawn reports whether s, symbol-font codes set in family, draws in
// a face [SymbolNode] gives it after rule: family itself, a clone of it,
// or OpenSymbol through LibreOffice's recoding ([symbolfont.Draw]). It is
// false when a code would fall to a face that holds no glyph for it, or
// fonts is nil.
func SymbolDrawn(s, family string, fonts *text.Fonts, rule symbolfont.Rule) bool {
	if fonts == nil || s == "" {
		return false
	}
	draw, fams, ok := symbolfont.Draw(s, family, rule, fonts.Covers)
	fam, aliases := family, true
	if ok && fams != nil {
		fam, aliases = fams[0], false
	}
	for _, r := range draw {
		if !fonts.Covers(fam, aliases, r) {
			return false
		}
	}
	return true
}

// SymbolText returns the characters text stands for when it is written in
// family, a symbol font LibreOffice recodes (Symbol, Wingdings, Webdings
// and their kin, [symbolfont.Recode]): each code from 0x20 to 0xFF, or
// that code in the private use area at U+F000, as the character
// [SymbolChar] names, else the character LibreOffice's recoding table
// draws it as, U+FFFD when that one is private-use too. Other characters stay. It returns "" when family is no such
// font or text holds no symbol code.
//
// A run's glyphs keep the codes the source wrote, which a reader cannot
// name when they are private-use: the converters give the run this text
// as its ActualText (ISO 19005-2, 6.2.11.7.3; ISO 14289-2, 8.4.3).
func SymbolText(text, family string) string {
	if !symbolfont.Recodes(family) {
		return ""
	}
	fams := []string{family}
	var b strings.Builder
	changed := false
	for _, r := range text {
		code := r
		if code >= 0xf020 && code <= 0xf0ff {
			code -= 0xf000
		}
		if code < 0x20 || code > 0xff {
			b.WriteRune(r)
			continue
		}
		if u, ok := SymbolChar(code, family); ok {
			changed = true
			b.WriteRune(u)
			continue
		}
		draw, _, _ := symbolfont.Recode(string(r), fams)
		if draw == string(r) && !unicode.Is(unicode.Co, r) {
			b.WriteRune(r)
			continue
		}
		changed = true
		for _, d := range draw {
			if unicode.Is(unicode.Co, d) {
				d = unicode.ReplacementChar
			}
			b.WriteRune(d)
		}
	}
	if !changed {
		return ""
	}
	return b.String()
}

// symbolChars maps codes of the Symbol font, the font's byte, to the
// Unicode character they stand for (Adobe's Symbol encoding), for the
// characters list labels and bullets use.
var symbolChars = map[rune]rune{
	0xB7: '•', 0xA7: '♣', 0xA8: '♦', 0xA9: '♥', 0xAA: '♠', 0xD8: '¬', 0xAE: '→', 0xDE: '⇒', 0xB0: '°', 0x2D: '−', 0x2A: '∗',
}

// wingdingsChars maps Wingdings characters list labels use to Unicode,
// as Word writes them where its PDF names one.
//
// # Corpus evidence
//
//   - litho/docx: breaks-symbols writes Wingdings 0x4A as U+263A, 0xE8 as
//     U+2794 and 0xFC as U+2713, numbering-bullets 0xFC as U+2713.
//   - openxmlsdk/docx: v2FxTestFiles/ForTestCase/header.docx, whose list
//     levels draw Wingdings "l", "n", "u" and 0xA7, writes U+25FC and
//     U+25C6 for the two it prints, "n" and "u" (Wingdings "l" is U+26AB
//     in PowerPoint's PDFs).
var wingdingsChars = map[rune]rune{
	0xA7: '▪', 0xA8: '◻', 0x6E: '◼', 0x6F: '□', 0x71: '❑', 0x75: '◆', 0x76: '❖', 0xD8: '➢', 0xFC: '✓', 0xFB: '✘',
	0x9F: '•', 0xA1: '○', 0x77: '⬥', 0xE0: '➔', 0xE8: '➔', 0x4A: '☺', 0x4C: '☹', 0xF0: '⇨',
}

// SymbolChar returns the Unicode character the code, a byte from 0x20 to
// 0xFF, stands for in the symbol font family (Symbol or Wingdings, 2 and
// 3), for the codes bullets and list labels use; false for another code
// or font.
func SymbolChar(code rune, family string) (rune, bool) {
	var table map[rune]rune
	switch strings.ToLower(strings.TrimSpace(family)) {
	case "symbol":
		table = symbolChars
	case "wingdings", "wingdings 2", "wingdings 3":
		table = wingdingsChars
	}
	u, ok := table[code]
	return u, ok
}
