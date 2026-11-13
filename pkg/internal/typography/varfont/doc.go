// Package varfont reads the variation tables of an OpenType variable font
// (OpenType Font Variations Overview) and writes a static instance of it:
// the font at one position of its design space, as a TrueType program the
// rest of litho subsets and embeds like any other.
//
// [ReadAxes] lists a font's axes and named instances from its 'fvar' table.
// [Normalize] maps user coordinates to the normalized coordinates the
// variation data is keyed by, through 'avar' when the font has one.
// [Instance] applies the 'gvar' deltas to the 'glyf' outlines, the 'HVAR'
// and 'VVAR' deltas (or the phantom points) to the glyph metrics, and the
// 'MVAR' deltas to the font-wide metrics, and drops the variation tables.
//
// Every table is read from bytes a document supplies: counts and offsets
// are checked against the data before any allocation or loop, and the work
// one instance may do is bounded.
//
// CFF2 outlines are written as CFF outlines at the instance;
// [DefaultCFF] writes any CFF2 program, a static one without 'fvar'
// included, at its default instance with CFF outlines. 'avar'
// version 2 is refused with [ErrUnsupported].
// Hinting is dropped from an instance ('fpgm', 'prep', 'cvt ', 'cvar' and
// glyph instructions), since the default instance's instructions do not
// fit the moved outlines.
package varfont
