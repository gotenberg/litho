// Package truetype provides a lightweight binary parser for
// TrueType / OpenType font files. It reads the cmap
// (character-to-glyph mapping) and post (PostScript glyph names)
// tables for Unicode fallback in PDF text extraction, and the hhea,
// hmtx, head, loca and maxp tables for glyph widths, glyph counts and
// empty glyphs. Glyph outlines are not parsed.
package truetype
