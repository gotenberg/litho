package truetype

import (
	"os"
	"testing"
)

// fuzzOutlineGlyphs caps the glyphs one fuzz input draws, so a large glyph
// count keeps each run short.
const fuzzOutlineGlyphs = 64

// FuzzGlyphOutline feeds arbitrary bytes to [ParseFont] and draws the glyphs
// of every program it accepts: the glyf and loca data of an embedded font are
// attacker-controlled. The contract is crash-freedom.
func FuzzGlyphOutline(f *testing.F) {
	valid, err := os.ReadFile("testdata/outlines.ttf")
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add(valid[:len(valid)/2])
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		font, err := ParseFont(data)
		if err != nil {
			return
		}
		for gid := range min(font.NumGlyphs(), fuzzOutlineGlyphs) {
			if _, err := font.GlyphOutline(t.Context(), gid); err != nil {
				continue
			}
			_ = font.Advance(gid)
		}
	})
}
