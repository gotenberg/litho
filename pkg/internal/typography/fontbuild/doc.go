// Package fontbuild writes small, structurally complete font programs, a
// TrueType sfnt, a bare CFF and a Type 1 program, from a list of glyph
// names, code points and advance widths. Tests use them to exercise font
// embedding without committing a font program: every glyph is a filled
// rectangle, a TrueType composite of other glyphs or a CFF or Type 1 seac
// accented character, so the programs carry the tables a PDF consumer reads
// and nothing a type designer made.
package fontbuild
