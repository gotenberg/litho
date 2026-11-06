package text

import (
	"context"
	"math"
	"testing"
)

func FuzzShape(f *testing.F) {
	f.Add("AB CA\u05d0\u05d1 ABC", 3.5, uint8(0))
	f.Add("\x00\x01\u2028\ufeff\u200dA\u0301\u2067\u05d0\u2069 \u00a0D", -2.0, uint8(7))
	fuzzFonts := newFuzzFonts(f, f.TempDir())
	f.Fuzz(func(t *testing.T, text string, objWidth float64, flags uint8) {
		if len(text) > 1<<12 {
			return
		}
		s := style()
		if flags&1 != 0 {
			s.LetterSpacing = -1
		}
		if flags&2 != 0 {
			s.WordSpacing = 3
		}
		runs := []ParagraphRun{
			{Text: text, Style: s, Override: Direction(flags >> 2 % 3)},
			{Object: &Object{Width: math.Abs(objWidth), Ascent: 2, Descent: 1}},
			{Text: text, Style: Style{Size: 13.7, Italic: true}},
		}
		p, _, err := fuzzFonts.Shape(context.Background(), runs, ParagraphOptions{Direction: Direction(flags >> 4 % 3)})
		if err != nil {
			return
		}
		n := p.Len()
		for i := range n {
			if v := p.Advance(i); math.IsNaN(v) || math.IsInf(v, 0) {
				t.Fatalf("advance %d = %v", i, v)
			}
			_ = p.Break(i)
			_ = p.GraphemeStart(i)
		}
		for _, cut := range [][2]int{{0, n}, {n / 3, 2 * n / 3}, {-1, n + 5}} {
			l := p.Line(cut[0], cut[1], flags&64 != 0)
			for _, sp := range l.Spans {
				if math.IsNaN(sp.X) || math.IsNaN(sp.Width) || sp.Run < 0 || sp.Run >= len(runs) {
					t.Fatalf("span %+v", sp)
				}
				if sp.Text != nil {
					for _, g := range sp.Text.Glyphs {
						if math.IsNaN(g.X) || math.IsInf(g.X, 0) {
							t.Fatalf("glyph at %v", g.X)
						}
					}
				}
			}
		}
		if lo, hi := p.MinContent(), p.MaxContent(); math.IsNaN(lo) || math.IsNaN(hi) {
			t.Fatalf("intrinsic %v %v", lo, hi)
		}
	})
}
