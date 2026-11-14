// Command gen-thai-dictionary generates the Thai word list the line
// breaker of pkg/internal/dictbreak divides Thai text with.
//
// It downloads thaidict.txt of ICU 78.1, the word list ICU's Thai
// dictionary break engine reads and the ICU the pinned Chromium (156)
// carries, refuses a file whose SHA-256 differs from the pinned digest,
// and writes its words as a minimal acyclic automaton, packed as
// pkg/internal/dictbreak reads it, in a Go string.
//
// The word list is under the Unicode License v3, which allows its
// redistribution with its notice: the generated file carries the notice
// and LICENSE-icu beside it the licence.
//
// Usage:
//
//	go run ./cmd/gen-thai-dictionary                  # download the file
//	go run ./cmd/gen-thai-dictionary --src FILE.txt   # read a local copy
//	go generate ./pkg/internal/dictbreak/...
package main
