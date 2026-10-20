package type1

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/render/geom"
)

// FuzzGlyphOutline parses arbitrary bytes as a Type 1 program and draws
// every glyph it defines. It must never panic.
func FuzzGlyphOutline(f *testing.F) {
	valid := testProgram{lenIV: 4, subrs: standardSubrs, glyphs: []testGlyph{
		{"A", square},
		{"acute", square},
		{"Aacute", cs(0, 500, tHsbw, 0, 10, 10, 65, 194, tSeac)},
	}}.build()
	f.Add(valid)
	f.Add(valid[:len(valid)/2])
	f.Add(testProgram{lenIV: -1, glyphs: []testGlyph{{"A", square}}}.build())
	f.Fuzz(func(t *testing.T, data []byte) {
		font, err := ParseFont(context.Background(), data)
		if err != nil {
			return
		}
		for name := range font.glyphs {
			g, err := font.GlyphOutline(name)
			if err != nil {
				continue
			}
			want := 0
			for _, op := range g.Path.Ops {
				switch op {
				case geom.MoveTo, geom.LineTo:
					want++
				case geom.CubicTo:
					want += 3
				}
			}
			if want != len(g.Path.Pts) {
				t.Fatalf("glyph /%s: %d points for ops wanting %d", name, len(g.Path.Pts), want)
			}
		}
	})
}
