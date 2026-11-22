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
//     vertical-rl and vertical-lr alike.
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
		for _, mode := range []string{"vertical-rl", "vertical-lr"} {
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

// TestVerticalLine_LROverSideOnTheRight locks the over side of a
// vertical-lr line on the right, as on a vertical-rl line (CSS Writing
// Modes 4, 4.1): its lines stack from the left, and each sets its glyphs,
// its decorations and its inline boxes as the vertical-rl line does.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 draws a line of
//     vertical-lr as the same line of vertical-rl, pixel for pixel, and
//     sets the lines of a vertical-lr paragraph of Liberation Serif and
//     Noto Sans CJK JP, 15 px at line-height 1.6, 24 px apart from the
//     left, the first one's upright glyphs at 6 px.
func TestVerticalLine_LROverSideOnTheRight(t *testing.T) {
	t.Parallel()
	const css = `font: 15px/1.6 'Liberation Serif'; line-height: 1.6`
	gs := verticalLine(t, "vertical-lr", css, `縦<br>書<br>き`)
	wantOrigins(t, "lines", gs, "縦 6,23.2", "書 30,23.2", "き 54,23.2")
	const deco = `font: 16px 'Noto Sans CJK JP'`
	for _, html := range []string{
		`<span style="text-decoration: underline red">縦書</span>`,
		`<span style="text-decoration: overline red">縦書</span>`,
		`縦<span style="background: blue; padding-left: 3px; border-right: 4px solid red">書</span>`,
	} {
		rl, lr := verticalPaths(t, "vertical-rl", deco, html), verticalPaths(t, "vertical-lr", deco, html)
		if rl != lr {
			t.Errorf("%s: vertical-lr paints %s, vertical-rl %s", html, lr, rl)
		}
	}
}

// verticalPaths converts one line as verticalLine does and returns the
// boxes of its filled paths, in pixels.
func verticalPaths(t *testing.T, mode, css, html string) string {
	t.Helper()
	src := `<!DOCTYPE html><meta charset=utf-8><style>@page { size: 400px 300px; margin: 0 } html, body { margin: 0 }
		.v { writing-mode: ` + mode + `; position: absolute; left: 10px; top: 10px; height: 200px; ` + css + ` }</style><div class=v>` + html + `</div>`
	doc, err := Convert(context.Background(), strings.NewReader(src), gotenbergFonts(t), Options{PreferCSSPageSize: true})
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprint(pxBoxes(doc)[0])
}

// TestVerticalLine_UprightGlyphsOfAFaceWithoutVerticalMetrics locks the
// upright glyphs of a face without a vmtx table: each advances by the
// face's rounded ascent and descent and sits the rounded ascent below the
// top of its advance, as Chromium sets them (CSS Writing Modes 4, 5.1,
// leaves the metrics of a synthesized vertical glyph to the user agent).
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 sets each glyph
//     of the table below where it has it, in vertical-rl and vertical-lr
//     under text-orientation: upright.
func TestVerticalLine_UprightGlyphsOfAFaceWithoutVerticalMetrics(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		css  string
		want []string
	}{
		{`font: 12px 'Liberation Serif'`, []string{"A 3.667,21", "b 5,35", "g 5,49"}},
		{`font: 16px 'Liberation Serif'`, []string{"A 3.723,24", "b 5.5,41", "g 5.5,58"}},
		{`font: 16px 'DejaVu Sans'`, []string{"A 4.027,25", "b 4.422,44", "g 4.422,63"}},
	} {
		for _, mode := range []string{"vertical-rl", "vertical-lr"} {
			wantOrigins(t, c.css+", "+mode, verticalLine(t, mode, c.css+"; text-orientation: upright", `Abg`), c.want...)
		}
	}
}

// TestVertical_OrthogonalAvailableSpace locks the inline size of an
// orthogonal flow root whose containing block has no definite height: its
// content's fit-content size in the smaller of the containing block's
// max-height, floored by its min-height, and the initial containing
// block's height (CSS Writing Modes 4, 7.3.1).
func TestVertical_OrthogonalAvailableSpace(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name, parent, want string
	}{
		{"min-height over max-height", `max-height: 40px; min-height: 80px`, "10,10+80x80"},
		{"max-height", `max-height: 50px`, "10,10+80x50"},
		{"no bound", ``, "10,10+20x190"},
	} {
		doc := convertDoc(t, `<style>@page { size: 400px 300px; margin: 0 } html, body { margin: 0 }
			.p { margin: 10px; font: 20px/20px "Test Serif"; `+c.parent+` }
			.v { writing-mode: vertical-rl; width: max-content; background: lime }</style>
			<div class=p><div class=v>aaaa aaaa aaaa aaaa</div></div>`, Options{PreferCSSPageSize: true})
		if got := pxBoxes(doc)[0]["g"]; got != c.want {
			t.Errorf("%s: vertical box %q, want %q", c.name, got, c.want)
		}
	}
}

// TestPrincipalDirection_FromTheBody locks the used direction of the root:
// the body's (CSS Writing Modes 4, 8), so the root's own line sets its
// text from the right, while a block the root generates inherits the
// root's computed direction and sets its text from the left.
func TestPrincipalDirection_FromTheBody(t *testing.T) {
	t.Parallel()
	doc := convertDoc(t, `<style>@page { size: 400px 300px; margin: 0 } html { font: 20px "Test Serif" } body { direction: rtl; height: 0; margin: 0 }
		html::before { content: "AB" } html::after { content: "CD"; display: block }</style><body></body>`, Options{PreferCSSPageSize: true})
	at := map[string]float64{}
	for _, g := range glyphOrigins(doc.Pages[0]) {
		at[g.text] = g.x
	}
	if at["A"] != 380 || at["C"] != 0 {
		t.Errorf("root line's A at %g, want 380; generated block's C at %g, want 0", at["A"], at["C"])
	}
}

// TestStaticPosition_InlineInARightToLeftLine locks the static position of
// an absolutely positioned box that was inline-level, both insets auto,
// in a right-to-left line: its hypothetical box, an object resolving to
// the paragraph's direction (UAX #9, N1 and N2), sits on the left of the
// content before it and extends leftward, its right edge the static
// position (CSS 2.1, 10.3.7), whatever its own writing mode.
func TestStaticPosition_InlineInARightToLeftLine(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"horizontal-tb", "vertical-lr", "vertical-rl"} {
		doc := convertDoc(t, `<style>@page { size: 400px 300px; margin: 0 } body { margin: 0 }
			.c { direction: rtl; position: relative; width: 320px; height: 320px; font: 80px/1 "Test Serif" }
			.c span { position: absolute; bottom: 80px; width: 80px; height: 80px; background: lime; writing-mode: `+mode+` }</style>
			<div class=c>12 34 56 78<span></span></div>`, Options{PreferCSSPageSize: true})
		if got := pxBoxes(doc)[0]["g"]; got != "160,160+80x80" {
			t.Errorf("%s: box %q, want 160,160+80x80", mode, got)
		}
	}
}
