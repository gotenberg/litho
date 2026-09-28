// Package cff provides a lightweight parser for Compact Font Format (CFF) data.
// It extracts the charset (GID → glyph name mapping), the encoding, the
// units-per-em and the glyph advance widths that PDF text extraction and width
// validation need, and cuts a name-keyed program down to the glyphs a
// document shows ([Subset]). Charstrings are scanned only for their width
// operand and for the seac components of an accented glyph; outlines are not
// parsed.
package cff
