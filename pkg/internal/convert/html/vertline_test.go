package html

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// glyphOrigin is where a glyph's origin lands on a page, in CSS pixels
// from the page's top left corner, and the characters it stands for.
type glyphOrigin struct {
	text string
	x, y float64
}

// glyphOrigins returns the glyph origins of page pg in content order,
// group transforms applied, glyphs that stand for no character left out.
func glyphOrigins(pg *typeset.Page) []glyphOrigin {
	var out []glyphOrigin
	var walk func([]typeset.Fragment, typeset.Matrix)
	walk = func(frags []typeset.Fragment, m typeset.Matrix) {
		for _, f := range frags {
			switch v := f.(type) {
			case *typeset.Group:
				t := v.Transform
				if t == (typeset.Matrix{}) {
					t = typeset.Identity
				}
				walk(v.Content, typeset.Matrix{
					A: m.A*t.A + m.C*t.B, B: m.B*t.A + m.D*t.B,
					C: m.A*t.C + m.C*t.D, D: m.B*t.C + m.D*t.D,
					E: m.A*t.E + m.C*t.F + m.E, F: m.B*t.E + m.D*t.F + m.F,
				})
			case *typeset.Text:
				for _, g := range v.Glyphs {
					if g.Text == "" {
						continue
					}
					x, y := v.Origin.X+g.X, v.Origin.Y+g.Y
					k := 4.0 / 3
					out = append(out, glyphOrigin{g.Text, (m.A*x + m.C*y + m.E) * k, (m.B*x + m.D*y + m.F) * k})
				}
			}
		}
	}
	walk(pg.Content, typeset.Identity)
	return out
}

// verticalLine converts one line of html in a vertical box of style css,
// 10 pixels from the page's top left corner, with the fonts of the
// reference image, and returns its glyph origins relative to the box.
func verticalLine(t *testing.T, mode, css, html string) []glyphOrigin {
	t.Helper()
	src := `<!DOCTYPE html><meta charset=utf-8><style>@page { size: 400px 300px; margin: 0 } html, body { margin: 0 }
		.v { writing-mode: ` + mode + `; position: absolute; left: 10px; top: 10px; height: 200px; ` + css + ` }</style><div class=v>` + html + `</div>`
	doc, err := Convert(context.Background(), strings.NewReader(src), gotenbergFonts(t), Options{PreferCSSPageSize: true})
	if err != nil {
		t.Fatal(err)
	}
	gs := glyphOrigins(doc.Pages[0])
	for i := range gs {
		gs[i].x -= 10
	}
	return gs
}

// wantOrigins compares the glyph origins got with want, each "text x,y",
// within half a pixel on both axes.
func wantOrigins(t *testing.T, name string, got []glyphOrigin, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Errorf("%s: %d glyphs %v, want %d", name, len(got), got, len(want))
		return
	}
	for i, w := range want {
		var tx string
		var x, y float64
		if _, err := fmt.Sscanf(w, "%s %g,%g", &tx, &x, &y); err != nil {
			t.Fatal(err)
		}
		g := got[i]
		if g.text != tx || math.Abs(g.x-x) > 0.5 || math.Abs(g.y-y) > 0.5 {
			t.Errorf("%s, glyph %d: %s %.3f,%.3f, want %s", name, i, g.text, g.x, g.y, w)
		}
	}
}

// TestVerticalLine_UprightGlyphsOnTheCentralBaseline locks where the
// glyphs of a vertical line stand across it: an upright glyph centred on
// the middle of its face's ascent and descent, a sideways run on the
// alphabetic baseline the line's central baseline sets, a fallback face's
// content area and a span of another face aligned on the central
// baseline (CSS Inline 3, 4.1 and 4.2.1, departed from as Chromium
// departs: the middle of the face's ascent and descent, not of the em
// box).
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 sets each glyph
//     of the table below where it has it, to the hundredth of a pixel, in
//     vertical-rl.
func TestVerticalLine_UprightGlyphsOnTheCentralBaseline(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, css, html string
		want            []string
	}{
		{"Noto Sans CJK JP, 12px", `font: 12px 'Noto Sans CJK JP'`, `縦書`, []string{"縦 2.5,20.56", "書 2.5,32.56"}},
		{"Noto Sans CJK JP, 16px", `font: 16px 'Noto Sans CJK JP'`, `縦書`, []string{"縦 4,24.08", "書 4,40.08"}},
		{"Noto Sans CJK JP, 23px, line-height 1.7", `font: 23px/1.7 'Noto Sans CJK JP'`, `縦書`, []string{"縦 8.594,30.24", "書 8.594,53.24"}},
		{"Liberation Sans falling back, 12px", `font: 12px 'Liberation Sans'`, `縦Ag`, []string{"縦 3.5,20.56", "A 4,22"}},
		{"Liberation Sans falling back, 16px", `font: 16px 'Liberation Sans'`, `縦Ag`, []string{"縦 6,24.08", "A 7,26"}},
		{"DejaVu Sans falling back, 23px", `font: 23px 'DejaVu Sans'`, `縦Ag`, []string{"縦 7.5,30.24", "A 9,33"}},
		{"a span of another face", `font: 16px 'Noto Sans CJK JP'`, `縦<span style="font: 24px 'Liberation Serif'">Ag縦</span>書`, []string{"縦 9,24.08", "A 9,26", "縦 7.5,76.45", "書 9,93.42"}},
	} {
		for _, mode := range []string{"vertical-rl"} {
			gs := verticalLine(t, mode, c.css, c.html)
			// A sideways run is one glyph a character; keep its first.
			var first []glyphOrigin
			for _, g := range gs {
				if g.text == "g" {
					continue
				}
				first = append(first, g)
			}
			wantOrigins(t, c.name+", "+mode, first, c.want...)
		}
	}
}
