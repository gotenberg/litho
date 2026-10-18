// Command gen-verapdf-cmaps generates the predefined encoding CMaps that
// pkg/internal/typography/cmap supplies to the conformance checks.
//
// A Type 0 font's /Encoding may name a predefined CMap, and an embedded CMap
// may build on one through usecmap or /UseCMap (ISO 32000-1, 9.7.5.2 and
// 9.7.5.4). litho bundles PDF.js's binary copies of Adobe's CMaps for text
// extraction, which stays equal to PDF.js. veraPDF bundles its own copies,
// and it decodes a font's codes to CIDs, which ISO 19005-2, 6.2.11.4.1 and
// 6.2.11.8 judge, through them. Its set holds the character-collection CMaps
// of the newest supplements (Adobe-Japan1-7, Adobe-KR-9 and the like) and
// UniAKR-UTF16-H, which PDF.js ships none of, and copies of eight Unicode
// CMaps whose mappings differ from PDF.js's. The generator fetches exactly
// those files from veraPDF's parser repository at the commit veraPDF 1.30.2
// was built from, refuses a file whose SHA-256 differs from the pinned
// digest, and writes each one gzip-compressed, with Adobe's BSD 3-Clause
// license, next to a Go file recording the provenance and the digests.
//
// Usage:
//
//	go run ./cmd/gen-verapdf-cmaps              # download at the pinned commits
//	go run ./cmd/gen-verapdf-cmaps --src DIR    # read local copies of those commits
//	go generate ./pkg/internal/typography/cmap/...
package main
