// Command gen-emoji-data generates the emoji character properties the text
// stack of pkg/internal/text reads: Emoji, Emoji_Presentation,
// Emoji_Modifier, Emoji_Modifier_Base and Extended_Pictographic (Unicode
// Technical Standard #51, Annex A).
//
// They decide which characters an emoji presentation sequence is made of
// and which of them a colour font draws by default (CSS Fonts 4, 6.12,
// font-variant-emoji). The generator downloads emoji-data.txt of Unicode
// 17.0, the version the ICU of the pinned Chromium (156) carries, refuses
// a file whose SHA-256 differs from the pinned digest, and writes each
// property as a [unicode.RangeTable].
//
// The data is under the Unicode License v3, which allows its
// redistribution with its notice: the generated file carries it.
//
// Usage:
//
//	go run ./cmd/gen-emoji-data                    # download the file
//	go run ./cmd/gen-emoji-data --src FILE.txt     # read a local copy
//	go generate ./pkg/internal/text/...
package main
