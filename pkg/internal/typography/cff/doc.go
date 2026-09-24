// Package cff provides a lightweight parser for Compact Font Format (CFF) data.
// It extracts the charset (GID → glyph name mapping), the encoding, the
// units-per-em and the glyph advance widths that PDF text extraction and width
// validation need. Charstrings are scanned only for their width operand;
// outlines are not parsed.
package cff
