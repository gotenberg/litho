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
//   - pptx: sd/tdf123090.pptx, sd/bulletColor.pptx and sd/tdf126234.pptx
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
