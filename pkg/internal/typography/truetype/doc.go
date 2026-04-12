// Package truetype provides a lightweight binary parser for
// TrueType / OpenType font files. It extracts only the cmap
// (character-to-glyph mapping) and post (PostScript glyph names)
// tables needed for Unicode fallback in PDF text extraction. Glyph
// outlines are not parsed.
package truetype
