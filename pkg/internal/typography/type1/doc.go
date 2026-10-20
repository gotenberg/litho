// Package type1 parses Adobe Type 1 font programs embedded in PDF
// files as /FontFile streams. [ParseEncoding] returns the
// byte-to-glyph-name encoding vector declared in the font's clear-text
// PostScript preamble.
//
// [ParseCharStringNames] and [ParseCharStringWidths] decrypt the
// eexec-encrypted section to list the glyphs its /CharStrings
// dictionary defines and their advance widths: the width scan stops at the
// metrics operator. Callers resolve glyph names to Unicode through the main
// Adobe Glyph List and the Symbol/Dingbats auxiliary tables.
//
// [ParseFont] reads a program for rendering: [Font.GlyphOutline] runs a
// glyph's charstring, subroutines, flex and seac included, into a path in
// glyph space, which [Font.FontMatrix] maps to text space.
//
// [SubsetInPlace] empties the charstrings of the glyphs a document does
// not show, keeping every glyph name and width, and encrypts the program
// again.
package type1
