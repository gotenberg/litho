package typography

import (
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// isNormalizable reports whether r belongs to the narrow character set
// that wants NFKC folding when surfaced as extracted text: Latin
// ligatures, Arabic presentation forms, Ohm and Micro signs, and the
// Unicode spaces, the CJK ideographic space U+3000 among them. Everything
// outside this set passes through [normalizeExtractionUnicode] unchanged.
//
// # Corpus evidence
//
//   - pdfjs bug1905623.pdf, issue15139.pdf: U+3000 indents paragraphs and
//     pads 請 求 書; kept as U+3000, their text differed from PDF.js's
//     even with every whitespace run stripped.
func isNormalizable(r rune) bool {
	switch {
	case r == 0x00A0, r == 0x00B5, r == 0x037E, r == 0x0EB3:
		return true
	case r >= 0x2000 && r <= 0x200A:
		return true
	case r == 0x202F, r == 0x2126, r == ideographicSpace:
		return true
	case r >= 0xFB00 && r <= 0xFB04, r == 0xFB05, r == 0xFB06:
		return true
	case r >= 0xFB20 && r <= 0xFB36:
		return true
	case r >= 0xFB38 && r <= 0xFB3C:
		return true
	case r == 0xFB3E:
		return true
	case r >= 0xFB40 && r <= 0xFB41:
		return true
	case r >= 0xFB43 && r <= 0xFB44:
		return true
	case r >= 0xFB46 && r <= 0xFBA1:
		return true
	case r >= 0xFBA4 && r <= 0xFBA9:
		return true
	case r >= 0xFBAE && r <= 0xFBB1:
		return true
	case r >= 0xFBD3 && r <= 0xFBDC:
		return true
	case r >= 0xFBDE && r <= 0xFBE7:
		return true
	case r >= 0xFBEA && r <= 0xFBF8:
		return true
	case r >= 0xFBFC && r <= 0xFBFD:
		return true
	case r >= 0xFC00 && r <= 0xFC5D:
		return true
	case r >= 0xFC64 && r <= 0xFCF1:
		return true
	case r >= 0xFCF5 && r <= 0xFD3D:
		return true
	case r == 0xFD88, r == 0xFDF4:
		return true
	case r >= 0xFDFA && r <= 0xFDFB:
		return true
	case r == 0xFE71, r == 0xFE77, r == 0xFE79, r == 0xFE7B, r == 0xFE7D:
		return true
	}
	return false
}

// normalizeExtractionUnicode applies NFKC to contiguous runs of
// [isNormalizable] characters and leaves every other rune untouched.
// Scoping the fold this tightly avoids altering CJK glyphs that NFKC
// would otherwise decompose, while still surfacing the compatibility
// equivalence on Latin ligatures and presentation forms.
func normalizeExtractionUnicode(s string) string {
	hasTarget := false
	for _, r := range s {
		if isNormalizable(r) {
			hasTarget = true
			break
		}
	}
	if !hasTarget {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		r, size := utf8.DecodeRuneInString(s[i:])
		if !isNormalizable(r) {
			b.WriteRune(r)
			i += size
			continue
		}
		j := i + size
		for j < len(s) {
			r2, sz2 := utf8.DecodeRuneInString(s[j:])
			if !isNormalizable(r2) {
				break
			}
			j += sz2
		}
		b.WriteString(norm.NFKC.String(s[i:j]))
		i = j
	}
	return b.String()
}
