package css

import "testing"

func TestFontStyle_ObliqueAngle(t *testing.T) {
	tests := []struct {
		decl  string
		style FontStyle
		angle float64
	}{
		{"font-style: oblique", FontOblique, 14},
		{"font-style: oblique 30deg", FontOblique, 30},
		{"font-style: oblique -0.25turn", FontOblique, -90},
		{"font-style: oblique 91deg", FontNormal, 0},
		{"font-style: oblique 10", FontNormal, 0},
		{"font-style: italic", FontItalic, 0},
		{"font: oblique 12px serif", FontOblique, 14},
	}
	for _, tt := range tests {
		s := styleOf(t, tt.decl)
		if s.FontStyle != tt.style || s.FontObliqueAngle != tt.angle {
			t.Errorf("%s: %v %v, want %v %v", tt.decl, s.FontStyle, s.FontObliqueAngle, tt.style, tt.angle)
		}
	}
}

func TestFontStretchAndSynthesis(t *testing.T) {
	tests := []struct {
		decl            string
		stretch         float64
		noBold, noSlant bool
	}{
		{"font-stretch: condensed", 75, false, false},
		{"font-stretch: 130%", 130, false, false},
		{"font-stretch: -5%", 0, false, false},
		{"font: expanded bold 12px serif", 125, false, false},
		{"font-stretch: 80%; font: 12px serif", 0, false, false},
		{"font-synthesis: none", 0, true, true},
		{"font-synthesis: style", 0, true, false},
		{"font-synthesis: weight small-caps", 0, false, true},
		{"font-synthesis: weight weight", 0, false, false},
		{"font-synthesis-weight: none", 0, true, false},
	}
	for _, tt := range tests {
		s := styleOf(t, tt.decl)
		if s.FontStretch != tt.stretch || s.NoSynthesizeWeight != tt.noBold || s.NoSynthesizeStyle != tt.noSlant {
			t.Errorf("%s: stretch %v synthesis %v %v", tt.decl, s.FontStretch, s.NoSynthesizeWeight, s.NoSynthesizeStyle)
		}
	}
}

func TestTextJustifyAndAutospace(t *testing.T) {
	tests := []struct {
		decl    string
		justify TextJustify
		space   TextAutospace
	}{
		{"text-justify: inter-character", JustifyInterCharacter, 0},
		{"text-justify: distribute", JustifyInterCharacter, 0},
		{"text-justify: none", JustifyNone, 0},
		{"text-autospace: normal", JustifyAuto, AutospaceIdeographAlpha | AutospaceIdeographNumeric},
		{"text-autospace: ideograph-numeric insert", JustifyAuto, AutospaceIdeographNumeric},
		{"text-autospace: ideograph-alpha punctuation", JustifyAuto, AutospaceIdeographAlpha},
		{"text-autospace: insert replace", JustifyAuto, 0},
		{"text-autospace: bogus", JustifyAuto, 0},
	}
	for _, tt := range tests {
		s := styleOf(t, tt.decl)
		if s.TextJustify != tt.justify || s.TextAutospace != tt.space {
			t.Errorf("%s: justify %v autospace %v", tt.decl, s.TextJustify, s.TextAutospace)
		}
	}
}

func TestTextShadow_Parse(t *testing.T) {
	s := styleOf(t, "color: red; text-shadow: 1px 2px blue, 3px 4px 5px")
	if len(s.TextShadow) != 2 || s.TextShadow[0].X != 0.75 || s.TextShadow[1].Blur != 3.75 || !colorEq(s.TextShadow[1].Color, rgb(255, 0, 0)) {
		t.Errorf("text-shadow = %+v", s.TextShadow)
	}
	for _, bad := range []string{"text-shadow: 1px 2px 3px 4px", "text-shadow: inset 1px 2px", "text-shadow: 1px"} {
		if s := styleOf(t, bad); s.TextShadow != nil {
			t.Errorf("%s = %+v, want none", bad, s.TextShadow)
		}
	}
	c := compute(t, `<div id=a><p id=b>x</p></div>`, `#a { text-shadow: 1px 1px currentColor } #b { color: lime }`)
	if sh := c.of("b").TextShadow; len(sh) != 1 || !colorEq(sh[0].Color, c.of("b").Color) {
		t.Errorf("inherited shadow = %+v", sh)
	}
}
