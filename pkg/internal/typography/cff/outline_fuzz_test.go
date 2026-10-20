package cff

import (
	"os"
	"testing"
)

// fuzzOutlineGlyphs caps the glyphs one fuzz input interprets, so a large
// CharStrings INDEX keeps each run short.
const fuzzOutlineGlyphs = 64

// FuzzGlyphOutline feeds arbitrary bytes to [ParseFont] and interprets the
// charstrings of every program it accepts: an embedded CFF program is
// attacker-controlled. The contract is crash-freedom.
func FuzzGlyphOutline(f *testing.F) {
	for _, name := range []string{"testdata/type2_ops.cff", "testdata/cid_fdarray_matrix.cff"} {
		valid, err := os.ReadFile(name)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(valid)
		f.Add(valid[:len(valid)/2])
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		font, err := ParseFont(data)
		if err != nil {
			return
		}
		for gid := range min(font.NumGlyphs(), fuzzOutlineGlyphs) {
			if _, err := font.Glyph(t.Context(), gid); err != nil {
				continue
			}
			_ = font.FontMatrix(gid)
		}
	})
}
