// Command gen-symbol-fonts generates the symbol font recoding tables the
// slides package of pkg/internal/convert/slides draws bullets with.
//
// A presentation names a bullet's character by its code in a symbol font
// (Wingdings, Webdings, Symbol and their kin) that the printing system
// lacks: the character is a byte of the font's own encoding, or that byte
// plus U+F000 in the private use area. LibreOffice draws such a character
// in its own symbol font, OpenSymbol, at the code point its recoding
// tables give the font's byte. The generator downloads LibreOffice core's
// unotools/source/misc/fontcvt.cxx at a pinned commit, refuses a file
// whose SHA-256 differs from the pinned digest, reads the tables of the
// fonts its StarSymbol recoding list names, one entry per code from 0x20
// to 0xFF, and writes them as Go data keyed by the normalized font names
// that select them.
//
// Usage:
//
//	go run ./cmd/gen-symbol-fonts                   # download the source
//	go run ./cmd/gen-symbol-fonts --src fontcvt.cxx # read a local copy
//	go generate ./pkg/internal/convert/slides/...
package main
