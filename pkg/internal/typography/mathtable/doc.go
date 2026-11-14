// Package mathtable reads the OpenType MATH table (OpenType 1.9.1, MATH,
// the mathematical typesetting table): the layout constants of
// MathConstants, the italics corrections, top accent attachments,
// extended shapes and math kerning of MathGlyphInfo, and the glyph
// variants and glyph assemblies of MathVariants.
//
// [Parse] reads the table directory of a program and the header of its
// MATH table; each subtable is read when asked, from the table's bytes,
// so reading a glyph's variants costs that glyph's records alone.
//
// The table comes from a font file, which is untrusted: every offset and
// count is checked against the table before it is used, every loop runs
// over a count so checked or a named cap, and a reader never panics. A
// subtable whose offset or size does not fit is read as absent, which is
// what MathML Core, 5 asks of a layout ("such parameters may not be
// available (e.g. if the font lacks one of the subtable, has an invalid
// offset, etc) and so fallback options are provided").
//
// Not read: device tables, whose corrections MathValueRecord leaves to
// the layout engine and which a layout at any resolution ignores, as the
// MATH table recommends against accumulating them.
package mathtable
