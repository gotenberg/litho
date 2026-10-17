// Package stdfonts ships the glyph ID to Unicode tables PDF.js reads a
// non-embedded standard font with, when a TrueType CIDFont draws it through an
// Identity CMap. PDF readers render such files with the host's installed face,
// whose glyph order the codes index. Text extractors that have no access to
// the host fonts need that order to resolve the codes to Unicode.
//
// [IdentityGlyphMap] answers for every font PDF.js treats as standard or maps
// to a standard font, by the name [PDFJSFontName] picks: PDF.js's standard
// map, which follows the Arial glyph order, with PDF.js's supplements for
// Arial Black and Calibri. Every other name resolves to nil, and the caller
// takes its usual fallback path.
//
// cmd/gen-stdfonts generates the tables, and the font name tables that decide
// which fonts qualify, into pdfjs_glyphs.go from PDF.js's
// src/core/standard_fonts.js at the revision the text goldens are generated
// with. Those tables are Copyright Mozilla Foundation and distributed under
// the Apache License, Version 2.0, reproduced in LICENSE-pdfjs. Regenerate
// them with go generate rather than editing them.
package stdfonts
