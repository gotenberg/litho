// Package cff provides a lightweight parser for Compact Font Format (CFF) data,
// extracting only the charset (GID → glyph name mapping) needed for Unicode
// fallback in PDF text extraction. CharString outlines are not parsed.
package cff
