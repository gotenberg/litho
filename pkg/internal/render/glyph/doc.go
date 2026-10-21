// Package glyph maps the character codes a content stream shows to the glyphs
// a renderer draws (ISO 32000-1, 9.6 and 9.7).
//
// [Cache.Font] loads a font dictionary once per document into a [Font]. A
// [Font] splits a shown string into character codes ([Font.Codes]) and
// resolves each code ([Font.Glyph]) to a [Glyph]: its outline in glyph space,
// the matrix from glyph space to text space, its advance and, for a Type 3
// font, the glyph description to run.
//
// # Code to glyph
//
//   - Type 1 and Type1C fonts select a glyph by the name the code takes
//     through /Encoding, /BaseEncoding and /Differences over the program's
//     built-in encoding (9.6.6.1, 9.6.6.2).
//   - TrueType fonts follow 9.6.6.4: a nonsymbolic font with an encoding looks
//     the glyph name's character up in the (3,1) cmap, or its Mac OS Roman code
//     in the (1,0) cmap, then the name in the post table; a symbolic font or one
//     without an encoding looks the code up in the (3,0) cmap at 0x0000, 0xF000,
//     0xF100 and 0xF200, then in the (1,0) cmap.
//   - Type 0 fonts split the string by their encoding CMap's codespace ranges
//     (9.7.6.2) and map each code to a CID. A CIDFontType0 program selects the
//     CID through its charset, a CIDFontType2 program through /CIDToGIDMap,
//     Identity or a stream (9.7.4.2, Table 117).
//   - Type 3 fonts name a /CharProcs glyph description, drawn through
//     /FontMatrix with the font's /Resources (9.6.5).
//
// A code that selects no glyph the program holds draws the program's .notdef
// glyph (9.6.6.3, 9.7.6.3).
//
// # Advance
//
// The advance is the width the PDF declares, read as text extraction reads it
// ([typography.Font.CodeWidth]), never the program's own advance: ISO
// 32000-1, 9.2.4 makes the font dictionary's widths govern glyph positioning.
//
// # Fonts the document does not embed
//
// litho bundles no font program. A font without an embedded program, or whose
// program will not parse, asks [Options.Substitutes] for a program, first by
// its own name, then by the standard 14 font whose metrics stand in for it,
// chosen by name or failing that by its descriptor's /Flags. A simple font's
// code selects the substitute's glyph by its glyph name or the character it
// stands for. A Type 0 font's code selects it by the character the font's
// /ToUnicode CMap or, failing that, its Adobe collection's UCS2 CMap gives
// it, never by its CID: /CIDToGIDMap indexes the embedded program alone
// (9.7.4.2, Table 117). The advance stays the declared /W or /W2 metric
// (9.7.4.3). A substitute
// glyph whose advance differs from the declared width is scaled horizontally
// to it, as viewers draw it. A font no substitute serves, and a glyph its
// substitute lacks, draws a placeholder: a hollow box as wide as the declared
// width and as tall as the font's bounding box. [Glyph.Source] says which
// path each glyph took.
//
// # Concurrency
//
// A [Cache] is safe for concurrent use. A [Font] is immutable once loaded and
// safe for concurrent use.
package glyph
