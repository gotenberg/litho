// Package type1 parses Adobe Type 1 font programs embedded in PDF
// files as /FontFile streams. The public entry point is [ParseEncoding],
// which returns the byte-to-glyph-name encoding vector declared in the
// font's clear-text PostScript preamble.
//
// Only the /Encoding vector is extracted. The eexec-encrypted binary
// section (which holds glyph outlines and CharStrings) is left alone
// because text extraction only needs the byte-to-glyph-name mapping.
// Callers resolve glyph names to Unicode through the main Adobe Glyph
// List and the Symbol/Dingbats auxiliary tables.
package type1
