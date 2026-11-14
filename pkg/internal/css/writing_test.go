package css

import "testing"

func TestProperties_WritingMode(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, doc, author string
		want              WritingMode
	}{
		{"initial", `<div id=t></div>`, ``, HorizontalTB},
		{"vertical-rl", `<div id=t></div>`, `#t { writing-mode: vertical-rl }`, VerticalRL},
		{"sideways-lr", `<div id=t></div>`, `#t { writing-mode: sideways-lr }`, SidewaysLR},
		{"the SVG tb-rl", `<div id=t></div>`, `#t { writing-mode: tb-rl }`, VerticalRL},
		{"inherited", `<div style="writing-mode: sideways-rl"><p id=t></p></div>`, ``, SidewaysRL},
		{"invalid keeps the cascade", `<div id=t></div>`, `#t { writing-mode: vertical-lr; writing-mode: diagonal }`, VerticalLR},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := compute(t, `<body>`+c.doc+`</body>`, c.author).of("t").WritingMode; got != c.want {
				t.Errorf("writing-mode %v, want %v", got, c.want)
			}
		})
	}
}

// TestProperties_TextOrientation parses text-orientation and
// text-combine-upright (CSS Writing Modes 4, 5.1 and 9.1): both inherit,
// sideways-right is the alias Chromium parses, and digits, which
// Chromium refuses, keeps the cascade.
func TestProperties_TextOrientation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, doc, author string
		orient            TextOrientation
		combine           TextCombine
	}{
		{"initial", `<div id=t></div>`, ``, OrientMixed, CombineNone},
		{"upright", `<div id=t></div>`, `#t { text-orientation: upright }`, OrientUpright, CombineNone},
		{"sideways-right", `<div id=t></div>`, `#t { text-orientation: sideways-right }`, OrientSideways, CombineNone},
		{"inherited", `<div style="text-orientation: sideways; text-combine-upright: all"><p id=t></p></div>`, ``, OrientSideways, CombineAll},
		{"digits refused", `<div id=t></div>`, `#t { text-combine-upright: all; text-combine-upright: digits 2 }`, OrientMixed, CombineAll},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			st := compute(t, `<body>`+c.doc+`</body>`, c.author).of("t")
			if st.TextOrientation != c.orient || st.TextCombineUpright != c.combine {
				t.Errorf("text-orientation %v, text-combine-upright %v, want %v and %v", st.TextOrientation, st.TextCombineUpright, c.orient, c.combine)
			}
		})
	}
}
