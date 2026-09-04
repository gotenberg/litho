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
// The tables are intentionally partial. The ASCII range follows the
// CID + 29 offset used by [typography.Font.CIDIdentityFallback], so
// only Latin-Extended and beyond codepoints need to be listed. Each
// entry keys on the CID (GID in the host font) and maps to the
// Unicode rune the font renders at that position. Values were
// captured from Microsoft's Arial 5.x and Calibri 6.x. Expanding
// coverage later is additive and safe.
package stdfonts
