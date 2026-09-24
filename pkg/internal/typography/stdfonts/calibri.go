package stdfonts

import "maps"

// calibriGIDToUnicode is the glyph ID to Unicode table PDF.js builds for a
// non-embedded Calibri that a CIDFontType2 draws through an Identity CMap
// (fonts.js, fallbackToSystemFont): [pdfjsStandardFontGlyphs], overlaid
// with [pdfjsCalibriGlyphs]. Both are generated from PDF.js, so litho reads
// such text the way PDF.js, the reader its text extraction is measured
// against, does.
//
// Calibri does not follow the Arial glyph order the standard map describes
// (the map agrees with every entry of [arialGIDToUnicode]): each accented
// Latin capital follows its base letter (À at GID 5, right after A at
// GID 4), and ASCII digits and punctuation sit far above GID 128. The
// supplement, which PDF.js calls incomplete, corrects part of them. Every
// other ID keeps the standard map's reading, which is right for the space
// at GID 3, shared by both orders, and a guess elsewhere. litho keeps
// PDF.js's composition as it is, guesses included.
//
// GID 1 maps to space although the full Calibri program draws nothing
// there: non-embedded Calibri text can carry GID 1 where a space belongs,
// and PDF.js maps it to space for that reason.
//
// Glyph IDs missing from the table fall through to the CID-offset path
// handled by [typography.Font.CIDIdentityFallback].
//
// # Corpus evidence
//
//   - All corpora: all 175 embedded Calibri programs that keep the full
//     glyph set carry a zero-width glyph at GID 1 and the space glyph,
//     463 FUnits wide, at GID 3.
//   - pdfjs issue15443.pdf: its non-embedded Calibri text writes GID 1
//     between two words, and the PDF.js reference output reads a space.
//   - pdfjs issue9195.pdf: its non-embedded Calibri text writes every space
//     as GID 3, which only the standard map resolves. The Calibri supplement
//     alone matches 48 of the 151 words of the PDF.js golden, the composed
//     table all 151.
//   - pdfjs issue13916.pdf: the composed table reads "ŠĚŠČŘŽÝÁÍÉ", as the
//     PDF.js golden does, where the 16 entries it replaced, captured from
//     reference output, read "~=~2y" and C1 control characters.
var calibriGIDToUnicode = overlayGlyphMaps(pdfjsStandardFontGlyphs, pdfjsCalibriGlyphs)

// overlayGlyphMaps returns a new table holding the entries of base, replaced
// and extended by those of over. PDF.js applies its glyph maps in that order
// (fonts.js, applyStandardFontGlyphMap).
func overlayGlyphMaps(base, over map[uint16]rune) map[uint16]rune {
	m := make(map[uint16]rune, len(base)+len(over))
	maps.Copy(m, base)
	maps.Copy(m, over)
	return m
}
