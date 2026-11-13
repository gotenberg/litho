// Command gen-counter-styles generates the predefined counter styles the
// CSS package of pkg/internal/css spells counters in.
//
// CSS Counter Styles 3 defines the predefined counter styles of its
// sections 6 and 7 as @counter-style rules in the specification's own
// style sheet blocks, and the longhand East Asian styles of its section
// 7.1.5 by an algorithm over tables of characters. The generator
// downloads the specification's source at a pinned commit of the CSS
// Working Group's drafts, refuses a file whose SHA-256 differs from the
// pinned digest, and writes the style sheet blocks of those sections and
// the character tables of 7.1.5 as Go data. A style the draft names
// otherwise than the Candidate Recommendation, which Chromium parses, is
// written under the Recommendation's name; a style only the draft holds
// is left out.
//
// The specification is under the W3C Software and Document License,
// which allows its redistribution with its notice: the generated file
// carries it.
//
// Usage:
//
//	go run ./cmd/gen-counter-styles                 # download the source
//	go run ./cmd/gen-counter-styles --src FILE.bs   # read a local copy
//	go generate ./pkg/internal/css/...
package main
