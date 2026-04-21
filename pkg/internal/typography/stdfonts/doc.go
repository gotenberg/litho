// Package stdfonts ships partial GID→Unicode tables for the common
// Windows system fonts (Arial, Times New Roman, Calibri, …) used by PDF
// producers that reference those fonts by name without embedding the
// font program. PDF readers render such files by falling back to the
// host OS's installed face; text extractors that have no access to the
// host fonts need an approximate cmap to resolve the CID values in the
// content stream to their Unicode codepoints.
//
// The tables are intentionally partial: the ASCII range follows the
// CID + 29 offset used by CIDIdentityFallback in the typography
// package, so only Latin-Extended and beyond codepoints need to be
// listed. Each entry in a cmap is the CID (GID in the host font) as
// key and the Unicode rune the font renders at that position as value.
// Values were captured from Microsoft's Arial 5.x / Calibri 6.x and
// cross-referenced with PDF.js's reference outputs; expanding coverage
// later is additive and safe.
package stdfonts
