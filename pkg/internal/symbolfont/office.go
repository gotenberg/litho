package symbolfont

import "strings"

// OfficeAdvance returns the advance, in ems, of the glyph of code in the
// symbol font family as Microsoft Office draws it, from the fonts Windows
// ships: code is a code from 0x20 to 0xFF, or that code in the private use
// area at U+F000. It is false for a family or a code [officeAdvances] does
// not hold. A converter that draws the code in another face, as [Recode]
// draws it in OpenSymbol, gives the glyph this advance to set what follows
// it where Office sets it.
func OfficeAdvance(family string, code rune) (float64, bool) {
	t, ok := officeAdvances[strings.ReplaceAll(strings.ToLower(strings.TrimSpace(family)), " ", "")]
	if !ok {
		return 0, false
	}
	if code >= 0xf020 && code <= 0xf0ff {
		code -= 0xf000
	}
	w, ok := t[code]
	if !ok {
		return 0, false
	}
	return float64(w) / 1000, true
}

// officeAdvances holds, by family, the advances in thousandths of an em
// of the codes of Wingdings, Wingdings 2, Wingdings 3 and Symbol that
// Office's PDFs embed: the /W entry of the glyph each code's text maps
// to in the subset Office writes. The fonts are Microsoft's and none of
// the fixture fonts carries their metrics.
//
// # Corpus evidence
//
//   - libreoffice/pptx, openxmlsdk/pptx, poi/pptx, litho/pptx,
//     libreoffice/docx, openxmlsdk/docx, poi/docx, litho/docx and the xlsx
//     corpora: every Type 0 font of a Microsoft reference named
//     Wingdings-Regular, Wingdings2, Wingdings3 or SymbolMT gives each
//     code the same width wherever it appears. A code written as its
//     private-use character U+F000 plus the code is read so; Wingdings
//     "l", "n", "q", "v", 0xA7, 0xD8 and 0xFC, which Office writes as
//     U+26AB, U+25FC, U+2751, U+2756, U+25AA, U+27A2 and U+2713, are read
//     from the bullets of sd/tdf126234.pptx, sd/tdf123090.pptx,
//     sd/bulletColor.pptx, sd/rightToLeftParagraph.pptx, sd/n83889.pptx and
//     litho/pptx bullets-character, and Symbol 0xB7, written U+2022, from
//     the Symbol bullets of the Word documents.
var officeAdvances = map[string]map[rune]uint16{
	"wingdings": {
		0x28: 1084, 0x2D: 1171, 0x30: 1096, 0x31: 1343, 0x32: 699, 0x33: 699,
		0x3A: 939, 0x49: 892, 0x4B: 843, 0x54: 813, 0x57: 723, 0x62: 941,
		0x63: 933, 0x64: 946, 0x65: 1025, 0x66: 929, 0x67: 1096, 0x68: 1065,
		0x69: 780, 0x6B: 1271, 0x6C: 747, 0x6E: 747, 0x6F: 891, 0x71: 891,
		0x72: 891, 0x76: 891, 0x77: 578, 0x78: 1060, 0x81: 892, 0x84: 892,
		0x8C: 892, 0x8F: 892, 0x95: 892, 0xA7: 458, 0xA8: 891, 0xB2: 891,
		0xD8: 794, 0xDC: 891, 0xE7: 1067, 0xFC: 786,
	},
	"wingdings2": {0x96: 361, 0x97: 554, 0x9E: 891, 0xA4: 891},
	"wingdings3": {0x7D: 530},
	"symbol": {
		0x2A: 500, 0x30: 500, 0x31: 500, 0x32: 500, 0x33: 500, 0x35: 500,
		0x36: 500, 0x3A: 278, 0x53: 592, 0x54: 611, 0x61: 631, 0x62: 549,
		0x63: 549, 0x65: 439, 0x66: 521, 0x67: 411, 0x68: 603, 0x69: 329,
		0x6B: 549, 0x6C: 549, 0x6D: 576, 0x6E: 521, 0x6F: 549, 0x71: 521,
		0x72: 549, 0x75: 576, 0x77: 686, 0x78: 493, 0x79: 686, 0x94: 600,
		0xA5: 713, 0xA8: 753, 0xB7: 460,
	},
}
