package stdfonts

// calibriGIDToUnicode maps the glyph IDs that non-embedded Calibri
// text uses to Unicode codepoints. Calibri places space at GID 3 like
// Arial, which the GID + 29 offset path already covers, but it orders
// the rest differently: each accented Latin capital follows its base
// letter (À at GID 5, right after A at GID 4), several
// Latin-Extended-B letters appear in the first few hundred GIDs, and
// ASCII digits and punctuation sit far above 128, so they do not
// follow the GID + 29 convention.
//
// The GID 1 entry is not the font's own mapping: in the full Calibri
// program GID 1 is a zero-width control glyph. It maps to space because
// non-embedded Calibri text can carry GID 1 where a space belongs, and
// PDF.js renders it as a space for that reason
// (getSupplementalGlyphMapForCalibri).
//
// Like the Arial table, this is intentionally partial, additive and
// safe to extend. GIDs missing from the map fall through to the
// CID-offset path handled by [typography.Font.CIDIdentityFallback].
//
// # Corpus evidence
//
//   - All corpora: the entries were captured by exercising the
//     non-embedded-Calibri fixtures of the corpus against the reference
//     output, so the table is corpus-driven.
//   - All corpora: all 175 embedded Calibri programs that keep the full
//     glyph set carry a zero-width glyph at GID 1 and the space glyph,
//     463 FUnits wide, at GID 3.
//   - pdfjs issue15443.pdf: its non-embedded Calibri text writes GID 1
//     between two words, and the PDF.js reference output reads a space.
var calibriGIDToUnicode = map[uint16]rune{
	// Space, then the ASCII punctuation and digits, which Calibri places
	// far above GID 128.
	0x0001: 0x0020, // space
	0x0358: 0x002E, // . FULL STOP (GID 856)
	0x03EC: 0x0030, // 0 (GID 1004)
	0x03ED: 0x0031, // 1 (GID 1005)
	0x03F0: 0x0034, // 4 (GID 1008)

	// Currency symbols.
	0x03AC: 0x00A3, // £ POUND SIGN (GID 940)

	// Latin-1 Supplement capital letters with diacritics.
	0x0005: 0x00C0, // À LATIN CAPITAL LETTER A WITH GRAVE
	0x001D: 0x00C8, // È LATIN CAPITAL LETTER E WITH GRAVE
	0x001E: 0x00C9, // É LATIN CAPITAL LETTER E WITH ACUTE
	0x0030: 0x00CC, // Ì LATIN CAPITAL LETTER I WITH GRAVE
	0x004C: 0x00D2, // Ò LATIN CAPITAL LETTER O WITH GRAVE

	// Latin-1 Supplement lowercase letters with diacritics.
	0x0103: 0x00E0, // à LATIN SMALL LETTER A WITH GRAVE
	0x011F: 0x00E8, // è LATIN SMALL LETTER E WITH GRAVE
	0x0120: 0x00E9, // é LATIN SMALL LETTER E WITH ACUTE
	0x015E: 0x00EC, // ì LATIN SMALL LETTER I WITH GRAVE
	0x017E: 0x00F2, // ò LATIN SMALL LETTER O WITH GRAVE
}
