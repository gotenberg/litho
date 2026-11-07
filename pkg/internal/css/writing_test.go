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
