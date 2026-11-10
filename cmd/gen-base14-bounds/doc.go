// Command gen-base14-bounds generates the glyph bounding boxes of the standard
// 14 fonts in pkg/internal/typography from Adobe's Core 14 AFM files.
//
// A standard font a PDF does not embed has no outlines to measure, so litho
// bounds its glyphs with the B entries of its Adobe Font Metrics (Adobe
// Technical Note #5004, section 8.2). The generator reads the copies Apache
// PDFBox ships, at a pinned commit, and transcribes each glyph's box unchanged.
// It refuses a file whose FontName is not the one asked for, or whose character
// metrics hold a glyph without a name or a box, so an upstream change cannot
// emit a partial table.
//
// Usage:
//
//	go run ./cmd/gen-base14-bounds              # download at the pinned PDFBox commit
//	go run ./cmd/gen-base14-bounds --src dir/   # read local copies of that commit
//	go generate ./pkg/internal/typography/...
package main
