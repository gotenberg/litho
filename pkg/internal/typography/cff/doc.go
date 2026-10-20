// Package cff provides a lightweight parser for Compact Font Format (CFF) data.
// It extracts the charset (GID → glyph name mapping), the encoding, the
// units-per-em and the glyph advance widths that PDF text extraction and width
// validation need, and cuts a name-keyed program down to the glyphs a
// document shows ([Subset]). The width, encoding and subset readers scan
// charstrings only for their width operand and the seac components of an
// accented glyph; [ParseFont] reads a program for rendering, and its Type 2
// interpreter turns each glyph's charstring into an outline ([Font.Glyph]).
package cff
