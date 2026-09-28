// Package truetype provides a lightweight binary parser for
// TrueType / OpenType font files. It reads the cmap
// (character-to-glyph mapping) and post (PostScript glyph names)
// tables for Unicode fallback in PDF text extraction, and the hhea,
// hmtx, head, loca and maxp tables for glyph widths, glyph counts and
// empty glyphs, and cuts a program down to the glyphs a document shows
// ([Subset]). It lists the faces of a TrueType or OpenType Collection
// ([Faces]) and extracts one as a program of its own ([ExtractFace]). Glyph
// outlines are copied, never interpreted: a composite
// glyph is read only for the glyphs it draws.
package truetype
