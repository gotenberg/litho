// Package type1 parses Adobe Type 1 font programs embedded in PDF
// files as /FontFile streams. [ParseEncoding] returns the
// byte-to-glyph-name encoding vector declared in the font's clear-text
// PostScript preamble.
//
// [ParseCharStringNames] and [ParseCharStringWidths] decrypt the
// eexec-encrypted section to list the glyphs its /CharStrings
// dictionary defines and their advance widths. Glyph outlines are not
// interpreted: the width scan stops at the metrics operator. Callers
// resolve glyph names to Unicode through the main Adobe Glyph List and
// the Symbol/Dingbats auxiliary tables.
package type1
