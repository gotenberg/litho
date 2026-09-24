package stdfonts

// calibriGIDToUnicode maps Calibri's standard TrueType glyph IDs to
// Unicode codepoints. Calibri uses a different GID ordering from
// Arial (space is at GID 3, but accented Latin capitals start
// immediately after .notdef and several Latin-Extended-B letters
// appear in the first few hundred GIDs), so ASCII digits and
// punctuation do not follow the GID + 29 convention.
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
var calibriGIDToUnicode = map[uint16]rune{
	// Basic punctuation / digits the Calibri subset places below GID 128.
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
