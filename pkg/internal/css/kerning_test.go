package css

import "testing"

func TestProperties_FontKerning(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, doc, author string
		want              FontKerning
	}{
		{"initial is auto", `<div id=t></div>`, ``, KerningAuto},
		{"none", `<div id=t></div>`, `#t { font-kerning: none }`, KerningNone},
		{"normal", `<div id=t></div>`, `#t { font-kerning: normal }`, KerningNormal},
		{"inherited", `<div style="font-kerning: none"><p id=t></p></div>`, ``, KerningNone},
		{"invalid keeps the cascade", `<div id=t></div>`, `#t { font-kerning: none; font-kerning: off }`, KerningNone},
		{"the font shorthand resets it", `<div id=t></div>`, `#t { font-kerning: none; font: 10px serif }`, KerningAuto},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := compute(t, `<body>`+c.doc+`</body>`, c.author).of("t").FontKerning; got != c.want {
				t.Errorf("font-kerning %v, want %v", got, c.want)
			}
		})
	}
}
