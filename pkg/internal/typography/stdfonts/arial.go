package stdfonts

// arialGIDToUnicode maps Arial's standard TrueType glyph IDs to the
// Unicode codepoints the Microsoft-shipped Arial renders at those
// positions. Only codepoints outside the ASCII range (GID ≥ 128) are
// listed; the [0x20, 0x7E] range follows the GID + 29 convention
// already handled by typography.CIDIdentityFallback + CIDOffset=29 in
// the per-glyph decoder. Entries are captured by exercising
// non-embedded-Arial fixtures from the PDF.js corpus and comparing
// against the reference output.
//
// The table is intentionally partial: only positions the corpus has
// exercised appear here. Additions are additive — a missing GID
// falls back to the CID offset path and the caller emits the raw
// codepoint, which at worst stays in the corpus diff but never
// regresses correctly resolved files.
var arialGIDToUnicode = map[uint16]rune{
	// Smart quotes (General Punctuation).
	179: 0x201C, // " LEFT DOUBLE QUOTATION MARK
	180: 0x201D, // " RIGHT DOUBLE QUOTATION MARK
	181: 0x2018, // ' LEFT SINGLE QUOTATION MARK
	182: 0x2019, // ' RIGHT SINGLE QUOTATION MARK

	// Latin-1 Supplement.
	207: 0x00D3, // Ó LATIN CAPITAL LETTER O WITH ACUTE
}
