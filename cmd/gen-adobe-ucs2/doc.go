// Command gen-adobe-ucs2 generates the Adobe PDF-to-Unicode mapping resources
// that pkg/internal/typography/cmap supplies to the conformance checks.
//
// ISO 32000-1, 9.10.2 obtains a CIDFont's Unicode values from the
// "Registry-Ordering-UCS2" CMap of its character collection, and a /ToUnicode
// CMap may build on one through usecmap or /UseCMap (9.7.5.3, 9.7.5.4). litho
// bundles PDF.js's binary copies of those CMaps for text extraction, which
// stays equal to PDF.js. They date from 2009, while veraPDF judges conformance
// against Adobe's current resources, which add and revise mappings. The
// generator fetches those resources (pdf2unicode/ of Adobe's
// mapping-resources-pdf repository) at a pinned commit, refuses a file whose
// SHA-256 differs from the pinned digest, and writes each one gzip-compressed,
// with Adobe's BSD 3-Clause license, next to a Go file recording the
// provenance and the digests.
//
// Usage:
//
//	go run ./cmd/gen-adobe-ucs2              # download at the pinned commit
//	go run ./cmd/gen-adobe-ucs2 --src DIR    # read local copies of that commit
//	go generate ./pkg/internal/typography/cmap/...
package main
