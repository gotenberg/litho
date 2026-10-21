package cff

import (
	"os"
	"testing"
)

// FuzzCFF2Outline feeds arbitrary bytes to [ParseCFF2] and interprets the
// charstrings of every table it accepts: the 'CFF2' table of an embedded
// OpenType program is attacker-controlled. The contract is crash-freedom.
func FuzzCFF2Outline(f *testing.F) {
	valid, err := os.ReadFile("testdata/cff2_ops.cff2")
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add(valid[:len(valid)/2])
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		font, err := ParseCFF2(data)
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
