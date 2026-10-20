// Package truetype provides a lightweight binary parser for
// TrueType / OpenType font files. It reads the cmap
// (character-to-glyph mapping) and post (PostScript glyph names)
// tables for Unicode fallback in PDF text extraction, and the hhea,
// hmtx, head, loca and maxp tables for glyph widths, glyph counts and
// empty glyphs, and cuts a program down to the glyphs a document shows
// ([Subset]). It lists the faces of a TrueType or OpenType Collection
// ([Faces]) and extracts one as a program of its own ([ExtractFace]). The
// subsetters copy glyph outlines without interpreting them, reading a
// composite glyph only for the glyphs it draws; [ParseFont] reads a program
// for rendering and decodes each glyph's quadratic outline, composites
// resolved, into a path ([Font.GlyphOutline]).
package truetype
