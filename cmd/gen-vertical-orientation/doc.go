// Command gen-vertical-orientation generates the Vertical_Orientation
// property the text stack of pkg/internal/text reads (Unicode Standard
// Annex #50, 5): which characters a vertical line sets upright and which
// it turns a quarter turn.
//
// It decides the orientation of each character of a vertical line under
// CSS text-orientation: mixed (CSS Writing Modes 4, 5.1). The generator
// downloads VerticalOrientation.txt of Unicode 17.0, the version the ICU
// of the pinned Chromium (156) carries, refuses a file whose SHA-256
// differs from the pinned digest, and writes the U, Tu and Tr values as
// [unicode.RangeTable] values: every other code point is R.
//
// The data is under the Unicode License v3, which allows its
// redistribution with its notice: the generated file carries it.
//
// Usage:
//
//	go run ./cmd/gen-vertical-orientation                  # download the file
//	go run ./cmd/gen-vertical-orientation --src FILE.txt   # read a local copy
//	go generate ./pkg/internal/text/...
package main
