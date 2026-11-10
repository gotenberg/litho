package pdfgen

import (
	"unicode/utf16"
	"unicode/utf8"
)

// unicodeToPDFDoc mirrors qpdf's QUtil unicode_to_pdf_doc table: the code
// points PDFDocEncoding places outside Latin-1 (ISO 32000-1, Annex D.2).
var unicodeToPDFDoc = map[rune]byte{
	0x02d8: 0x18, 0x02c7: 0x19, 0x02c6: 0x1a, 0x02d9: 0x1b, 0x02dd: 0x1c, 0x02db: 0x1d,
	0x02da: 0x1e, 0x02dc: 0x1f, 0x2022: 0x80, 0x2020: 0x81, 0x2021: 0x82, 0x2026: 0x83,
	0x2014: 0x84, 0x2013: 0x85, 0x0192: 0x86, 0x2044: 0x87, 0x2039: 0x88, 0x203a: 0x89,
	0x2212: 0x8a, 0x2030: 0x8b, 0x201e: 0x8c, 0x201c: 0x8d, 0x201d: 0x8e, 0x2018: 0x8f,
	0x2019: 0x90, 0x201a: 0x91, 0x2122: 0x92, 0xfb01: 0x93, 0xfb02: 0x94, 0x0141: 0x95,
	0x0152: 0x96, 0x0160: 0x97, 0x0178: 0x98, 0x017d: 0x99, 0x0131: 0x9a, 0x0142: 0x9b,
	0x0153: 0x9c, 0x0161: 0x9d, 0x017e: 0x9e, 0xfffd: 0x9f, 0x20ac: 0xa0,
}

// Text returns the PDF text string pikepdf stores for s: PDFDocEncoding
// when every character has a code there, else UTF-16BE behind a U+FEFF byte
// order mark. It follows qpdf's QUtil::utf8_to_pdf_doc, including its
// refusals: U+0018 to U+001F, U+007F and U+00AD have no PDFDocEncoding code,
// and a string whose encoding would start like a byte order mark falls back
// to UTF-16.
func Text(s string) String {
	if b, ok := toPDFDoc(s); ok {
		return String(b)
	}
	out := []byte{0xfe, 0xff}
	for _, r := range s {
		for _, u := range utf16.Encode([]rune{r}) {
			out = append(out, byte(u>>8), byte(u)) //nolint:gosec // splitting a UTF-16 code unit into its two bytes.
		}
	}
	return String(out)
}

func toPDFDoc(s string) ([]byte, bool) {
	if len(s) >= 4 && s[0] == 0xc3 {
		if s[1:4] == "\xbe\xc3\xbf" || s[1:4] == "\xbf\xc3\xbe" || (len(s) >= 6 && s[1:6] == "\xaf\xc2\xbb\xc2\xbf") {
			return nil, false
		}
	}
	out := make([]byte, 0, len(s))
	for i, r := range s {
		switch {
		case r == utf8.RuneError && invalidAt(s, i):
			return nil, false
		case r < 128:
			if (r >= 0x18 && r <= 0x1f) || r == 127 {
				return nil, false
			}
			out = append(out, byte(r)) //nolint:gosec // r < 128, checked by the case.
		case r == 0xad:
			return nil, false
		case r > 160 && r < 256:
			out = append(out, byte(r)) //nolint:gosec // r < 256, checked by the case.
		default:
			c, ok := unicodeToPDFDoc[r]
			if !ok {
				return nil, false
			}
			out = append(out, c)
		}
	}
	return out, true
}

// invalidAt reports whether s holds an invalid UTF-8 sequence at i, as
// opposed to an encoded U+FFFD.
func invalidAt(s string, i int) bool {
	_, size := utf8.DecodeRuneInString(s[i:])
	return size == 1
}
