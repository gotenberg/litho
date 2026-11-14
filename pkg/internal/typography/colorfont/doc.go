// Package colorfont reads the colour glyphs of an OpenType program: the
// colour bitmaps of the CBLC and CBDT tables and of the sbix table, and the
// layered glyphs of the COLR table (version 0) with the palettes of the
// CPAL table (OpenType 1.9.1, the CBLC, CBDT, sbix, COLR and CPAL
// tables).
//
// [Parse] reads the table directory and the headers of the tables a
// program holds; a glyph's bitmap ([Font.Bitmap]) or layers
// ([Font.Layers]) are read when asked, so a font of thousands of colour
// glyphs costs what its used glyphs cost. A bitmap is returned as the PNG
// or JPEG file the font stores, with its placement; decoding it is the
// caller's.
//
// Not supported: the paint graph of COLR version 1 (a version 1 table is
// read for its version 0 records only), the SVG table, and bitmaps stored
// as raw pixels (CBDT formats 1 to 9) or TIFF (sbix): such a glyph reports
// no bitmap.
//
// Every offset, count and length is untrusted: each is checked against
// its table and against named caps before it is used, and a reader never
// panics.
package colorfont
