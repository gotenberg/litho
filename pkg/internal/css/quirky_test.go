package css

import (
	"context"
	"testing"
)

// computeQuirks computes doc under the user agent sheet and the author
// sheet, both the sheet and the style attributes read in mode.
func computeQuirks(t *testing.T, doc, author string, mode DocumentMode) computed {
	t.Helper()
	root := parseHTML(t, doc)
	sheet, err := Parse(context.Background(), []byte(author), ParseOptions{Quirks: mode == Quirks})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	r := NewResolver(a4, []Sheet{{Sheet: HTMLUserAgent(), Origin: UserAgent}, {Sheet: sheet, Origin: Author}})
	c, err := r.Compute(context.Background(), root, ComputeOptions{Hints: HTMLHints, Mode: mode})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	return computed{t: t, doc: root, c: c}
}

// TestQuirkyValues_ReadInQuirksMode reads a number as a length in pixels
// in the properties of CSS Values 4, Appendix C, and a hex colour without
// its "#" in those of CSS Color 4, Appendix B, in a sheet and a style
// attribute of a document in quirks mode alone; a shorthand that includes
// them, a function and another property still refuse them.
func TestQuirkyValues_ReadInQuirksMode(t *testing.T) {
	const author = `#a { width: 100; margin: 10 20; color: ff0000; background-color: 123abc; border-width: 7; border-style: solid; border-color: abc }
#b { border: 4 solid; background: 00ff00; height: calc(100 + 10px); outline-width: 5; border-top-color: 1e3; color: 1234567 }
#c { clip: rect(1, 2, 3, 4); position: absolute; font-size: 20; vertical-align: 3 }
#v { --w: 50; width: var(--w) }`
	const doc = `<div id=a></div><div id=b></div><div id=c></div><div id=s style="width: 30; color: 0f0"></div><div id=v></div>`
	for _, mode := range []DocumentMode{Quirks, LimitedQuirks, NoQuirks} {
		c := computeQuirks(t, doc, author, mode)
		quirks := mode == Quirks
		a, b, cl, s, v := c.of("a"), c.of("b"), c.of("c"), c.of("s"), c.of("v")
		if got := dimEq(a.Width, pts(75)) && dimEq(a.Margin[Top], pts(7.5)) && dimEq(a.Margin[Left], pts(15)); got != quirks {
			t.Errorf("mode %d: width %v, margins %v", mode, a.Width, a.Margin)
		}
		if got := colorEq(a.Color, rgb(255, 0, 0)) && colorEq(a.BackgroundColor, rgb(0x12, 0x3a, 0xbc)) && colorEq(a.Border[Top].Color, rgb(0xaa, 0xbb, 0xcc)) && near(a.Border[Top].Width, 5.25); got != quirks {
			t.Errorf("mode %d: colour %v, background %v, border %v", mode, a.Color, a.BackgroundColor, a.Border[Top])
		}
		if b.Border[Top].Style != BorderNone || b.Height.Kind != Auto || !colorEq(b.Border[Top].Color, b.Color) || !colorEq(b.Color, rgb(0, 0, 0)) {
			t.Errorf("mode %d: refused values set %v, height %v, colour %v", mode, b.Border[Top], b.Height, b.Color)
		}
		if got := cl.Clip.Set && dimEq(cl.Clip.Edges[0], pts(0.75)) && near(cl.FontSize, 15); got != quirks {
			t.Errorf("mode %d: clip %v, font size %v", mode, cl.Clip, cl.FontSize)
		}
		if got := dimEq(s.Width, pts(22.5)) && colorEq(s.Color, rgb(0, 0, 0xf0)); got != quirks {
			t.Errorf("mode %d: style attribute width %v, colour %v", mode, s.Width, s.Color)
		}
		if got := dimEq(v.Width, pts(37.5)); got != quirks {
			t.Errorf("mode %d: substituted width %v", mode, v.Width)
		}
	}
}
