// Package stdfonts ships partial GID→Unicode tables for common Windows
// system fonts used by PDF producers that reference those fonts by name
// without embedding the font program. PDF readers render such files by
// falling back to the host OS's installed face. Text extractors that
// have no access to the host fonts need an approximate cmap to
// resolve the CID values in the content stream to Unicode.
//
// Two families are bundled today: Arial, which [CMap] also answers for
// Helvetica, and Calibri. Every other name resolves to nil, and the caller
// takes its usual fallback path.
//
// The tables are intentionally partial. Each entry keys on the CID (GID in
// the host font) and maps to the Unicode rune the font renders at that
// position. The Arial table lists only codepoints beyond ASCII, whose range
// follows the CID + 29 offset used by [typography.Font.CIDIdentityFallback];
// its values were captured from Microsoft's Arial 5.x. The Calibri table is
// the one PDF.js builds for a non-embedded Calibri, composed from two PDF.js
// lookup tables that cmd/gen-stdfonts generates into pdfjs_glyphs.go from
// PDF.js's src/core/standard_fonts.js at a pinned commit. Those tables are
// Copyright Mozilla Foundation and distributed under the Apache License,
// Version 2.0, reproduced in LICENSE-pdfjs. Regenerate them with go
// generate rather than editing them.
package stdfonts
