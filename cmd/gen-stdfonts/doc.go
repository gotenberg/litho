// Command gen-stdfonts generates the PDF.js glyph tables of
// pkg/internal/typography/stdfonts from PDF.js's src/core/standard_fonts.js.
//
// PDF.js reads text drawn with a standard font that a PDF references without
// embedding through lookup tables from glyph ID to Unicode. The generator
// transcribes those tables into Go maps and leaves their values unchanged. It
// refuses a table body holding anything but "t[gid] = unicode;" assignments,
// so an upstream rewrite cannot emit a partial table.
//
// Usage:
//
//	go run ./cmd/gen-stdfonts                        # download at the pinned PDF.js commit
//	go run ./cmd/gen-stdfonts --src standard_fonts.js # read a local copy of that commit
//	go generate ./pkg/internal/typography/stdfonts/...
package main
