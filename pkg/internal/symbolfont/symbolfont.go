package symbolfont

import "strings"

//go:generate go run ../../../cmd/gen-symbol-fonts -out symbols_gen.go

// Recode returns the character c of a bullet in the families fams
// as Impress draws it: the character drawn, the text it stands for in the
// printed text, and the families to draw it in. A bullet of a StarSymbol
// font draws in OpenSymbol as it is. A character of a symbol font Impress
// recodes (Wingdings, Webdings, Symbol, Monotype Sorts, StarBats and their
// kin), a code from 0x20 to 0xFF or that code in the private use area at
// U+F000, draws in OpenSymbol at the code point LibreOffice's recoding
// table gives it, U+E12C for a code the table leaves out, and stands for
// its private-use code, U+F000 plus the code (unotools'
// ConvertChar::RecodeChar, fontcvt.cxx). A control character of such a
// font draws OpenSymbol's bullet; any other character keeps its families.
//
// # Corpus evidence
//
//   - libreoffice/pptx: sd/tdf123090.pptx, sd/bulletColor.pptx and sd/tdf126234.pptx
//     print Wingdings bullets "n", "q" and "l" in OpenSymbol, as U+F06E,
//     U+F071 and U+F06C in the text.
func Recode(c string, fams []string) (draw, txt string, out []string) {
	r := []rune(c)
	if len(r) == 0 {
		return "", "", fams
	}
	star := false
	var table *[224]rune
	for _, f := range fams {
		name := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(f)), " ", "")
		switch name {
		case "starsymbol", "opensymbol":
			star = true
		}
		if t, ok := symbolFonts[name]; ok && table == nil {
			table = t
		}
	}
	code := r[0]
	if code >= 0xf020 && code <= 0xf0ff {
		code -= 0xf000
	}
	switch {
	case star:
		return c, c, []string{"OpenSymbol"}
	case table == nil:
		return c, c, fams
	case code >= 0x20 && code <= 0xff:
		to := table[code-0x20]
		if to == 0 {
			to = 0xe12c
		}
		return string(to), string(0xf000 + code), append([]string{"OpenSymbol"}, fams...)
	case code < 0x20:
		return "•", "•", []string{"OpenSymbol"}
	}
	return c, c, append([]string{"OpenSymbol"}, fams...)
}

// Rule names the program whose drawing of a symbol font's codes [Draw]
// follows.
type Rule uint8

const (
	// RuleWord draws the codes as Microsoft Word does where the font is
	// missing: in a metric-compatible clone of the font, at the code
	// itself, when the clone covers every code of the text; else as
	// [RuleLibreOffice] does.
	RuleWord Rule = iota
	// RuleLibreOffice recodes the codes into OpenSymbol, as LibreOffice
	// draws a symbol font the printing system lacks ([Recode]).
	RuleLibreOffice
)

// Draw returns what to draw for s, text set in the symbol font family, by
// rule: the text and the families to draw it in, nil for the run's own,
// and whether either changed. A family [Recodes] does not know, and text
// whose every character a face of family itself covers, draw as they are.
// covers reports whether a face of fam covers r, one of its
// metric-compatible clones too when aliases is set. A code whose recoding
// OpenSymbol lacks keeps its character, which the run's fallback draws.
//
// A code is a character from U+0020 to U+00FF, or that code in the
// private use area at U+F000, where Word and the fonts' own symbol cmaps
// place them.
//
// # Corpus evidence
//
//   - probe, odt: LibreOffice in the gotenberg/gotenberg:9.4.1 image, which
//     holds no Symbol or Wingdings font, prints "abg", U+F061 U+F062 U+F067
//     U+F0B7 in Symbol and U+F04A U+F0FC U+F028 in Wingdings in OpenSymbol.
//   - libreoffice/odt: odfexport/WordTest_edit.odt prints its U+F0B7 bullets
//     in Symbol in OpenSymbol.
//   - litho/docx: run-fonts-fallback and fields-logic-formula print Symbol
//     runs in SymbolMT and Wingdings runs in Wingdings in Word, whose
//     metrics Standard Symbols PS, Symbol's clone, keeps.
func Draw(s, family string, rule Rule, covers func(fam string, aliases bool, r rune) bool) (string, []string, bool) {
	if !Recodes(family) || s == "" {
		return s, nil, false
	}
	own := true
	for _, r := range s {
		if !covers(family, false, r) {
			own = false
			break
		}
	}
	if own {
		return s, nil, false
	}
	if rule == RuleWord {
		var b strings.Builder
		clone := true
		for _, r := range s {
			c := code(r)
			if c < 0 {
				c = r
			}
			if !covers(family, true, c) {
				clone = false
				break
			}
			b.WriteRune(c)
		}
		if clone {
			return b.String(), nil, b.String() != s
		}
	}
	var b strings.Builder
	recoded := false
	for _, r := range s {
		if code(r) < 0 {
			b.WriteRune(r)
			continue
		}
		draw, _, _ := Recode(string(r), []string{family})
		if d := []rune(draw); len(d) != 1 || !covers("OpenSymbol", false, d[0]) {
			b.WriteRune(r)
			continue
		}
		recoded = true
		b.WriteString(draw)
	}
	if !recoded {
		return s, nil, false
	}
	return b.String(), []string{"OpenSymbol"}, true
}

// code returns the code r stands for in a symbol font, a byte from 0x20
// to 0xFF, -1 for a character that is no code.
func code(r rune) rune {
	if r >= 0xf020 && r <= 0xf0ff {
		r -= 0xf000
	}
	if r < 0x20 || r > 0xff {
		return -1
	}
	return r
}
