// Package embed writes a font program into a PDF as a Type0 font: one sfnt
// program in, the font objects a content stream draws with out.
//
// [Load] parses a TrueType program, or an OpenType program with CFF
// outlines, far enough to embed it: its metrics, its Unicode cmap, and for
// CFF outlines the charset. A [Subset] then records the glyphs drawn with
// the font, by glyph ID and with the text each one stands for, hands out the
// 2-byte code a string selects each glyph by, and writes the font subset to
// the glyphs recorded:
//
//   - TrueType outlines become a CIDFontType2 over a renumbered subset, with
//     a CIDToGIDMap stream (ISO 32000-1, 9.7.4.2).
//   - CFF outlines become a CIDFontType0 over the CFF table subset in place
//     (ISO 32000-1, 9.9, Table 126), its codes the CIDs of the charset.
//
// Both take the Identity-H encoding and a ToUnicode CMap naming the text of
// each code (ISO 32000-1, 9.10.3), so the drawn text extracts and searches
// as written. [Font.WriteIdentity] writes the older shape the text stamp
// takes, whose codes are glyph IDs.
//
// A program that fails to subset is embedded whole: the output is then
// larger, never wrong. Output depends on the program and the glyphs recorded
// alone, so identical input writes identical objects.
package embed
