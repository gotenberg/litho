package css

import (
	"math"
	"testing"
)

func TestBorderWidth_Snapped(t *testing.T) {
	t.Parallel()
	tests := []struct {
		decl string
		want float64
	}{
		{"border: 0.25px solid", 0.75},
		{"border: 0.9px solid", 0.75},
		{"border: 1.5px solid", 0.75},
		{"border: 2.99px solid", 1.5},
		{"border: 3pt solid", 3},
		{"border: 0 solid", 0},
		{"border: thin solid", 0.75},
		{"border: 2px none", 1.5},
		{"border-width: 4px; border-style: hidden", 3},
	}
	for _, tt := range tests {
		if got := styleOf(t, tt.decl).Border[Top].Width; math.Abs(got-tt.want) > 1e-9 {
			t.Errorf("%s: width %v, want %v", tt.decl, got, tt.want)
		}
	}
}

func TestBorderWidth_InheritedUnderNone(t *testing.T) {
	t.Parallel()
	// CSS2/borders/border-width-011: the body's width, 2em under style
	// none, is what the paragraph inherits.
	c := compute(t, `<div id=p><p id=c>x</p></div>`, `
#p { font-size: 10px; border-width: 2em }
#c { font-size: 20px; border-width: inherit; border-style: solid }`)
	if got := c.of("c").Border[Left].Width; got != 15 {
		t.Errorf("inherited width %v, want 15pt", got)
	}
}

func TestCurrentColor_InheritedAsKeyword(t *testing.T) {
	t.Parallel()
	c := compute(t, `<div id=p><p id=c>x</p></div>`, `
#p { color: red; background: currentColor; outline-color: currentColor; box-shadow: 1px 1px }
#c { color: lime; background: inherit; outline: 1px solid; outline-color: inherit; box-shadow: inherit }`)
	p, ch := c.of("p"), c.of("c")
	if !colorEq(p.BackgroundColor, rgb(255, 0, 0)) || !colorEq(ch.BackgroundColor, rgb(0, 255, 0)) {
		t.Errorf("backgrounds %+v and %+v, want red and lime", p.BackgroundColor, ch.BackgroundColor)
	}
	if !colorEq(ch.Outline.Color, rgb(0, 255, 0)) {
		t.Errorf("outline colour %+v, want lime", ch.Outline.Color)
	}
	if len(ch.BoxShadow) != 1 || !colorEq(ch.BoxShadow[0].Color, rgb(0, 255, 0)) || !colorEq(p.BoxShadow[0].Color, rgb(255, 0, 0)) {
		t.Errorf("shadows %+v and %+v, want red then lime", p.BoxShadow, ch.BoxShadow)
	}
}

func TestBoxShadow_Parse(t *testing.T) {
	t.Parallel()
	s := styleOf(t, "color: blue; box-shadow: 3px 4px red, inset 1px 2px 4px -1px, 2px 2px 2px 2px green inset")
	want := []Shadow{
		{X: 2.25, Y: 3, Color: rgb(255, 0, 0)},
		{X: 0.75, Y: 1.5, Blur: 3, Spread: -0.75, Color: rgb(0, 0, 255), Inset: true, current: true},
		{X: 1.5, Y: 1.5, Blur: 1.5, Spread: 1.5, Color: rgb(0, 128, 0), Inset: true},
	}
	if len(s.BoxShadow) != len(want) {
		t.Fatalf("shadows %+v", s.BoxShadow)
	}
	for i := range want {
		if s.BoxShadow[i] != want[i] {
			t.Errorf("shadow %d = %+v, want %+v", i, s.BoxShadow[i], want[i])
		}
	}
	for _, bad := range []string{"box-shadow: 1px", "box-shadow: 1px 1px -1px", "box-shadow: 1px red 1px", "box-shadow: inset inset 1px 1px", "box-shadow: 1px 1px 1px 1px 1px", "box-shadow: 10% 1px"} {
		if s := styleOf(t, bad); s.BoxShadow != nil {
			t.Errorf("%s: parsed as %+v", bad, s.BoxShadow)
		}
	}
	if s := styleOf(t, "box-shadow: 1px 1px; box-shadow: none"); s.BoxShadow != nil {
		t.Errorf("none = %+v", s.BoxShadow)
	}
}

func TestOutline_Shorthand(t *testing.T) {
	t.Parallel()
	s := styleOf(t, "color: blue; outline: auto 2px; outline-offset: -3px")
	if s.Outline != (Border{Width: 1.5, Style: Solid, Color: rgb(0, 0, 255)}) || s.OutlineOffset != -2.25 {
		t.Errorf("outline %+v offset %v", s.Outline, s.OutlineOffset)
	}
	if s := styleOf(t, "outline: hidden"); s.Outline.Style != BorderNone {
		t.Errorf("outline: hidden is invalid, got %+v", s.Outline)
	}
	if s := styleOf(t, ""); s.Outline.Width != 2.25 || s.Outline.Style != BorderNone {
		t.Errorf("initial outline %+v, want medium none", s.Outline)
	}
}

func TestObjectFit_Parse(t *testing.T) {
	t.Parallel()
	s := styleOf(t, "object-fit: scale-down; object-position: right 25% top")
	if s.ObjectFit != FitScaleDown || s.ObjectPosition != [2]Dimension{{Kind: Percent, Value: 0.75}, {Kind: Percent}} {
		t.Errorf("object-fit %v, position %+v", s.ObjectFit, s.ObjectPosition)
	}
	if s := styleOf(t, ""); s.ObjectFit != FitFill || s.ObjectPosition != [2]Dimension{{Kind: Percent, Value: 0.5}, {Kind: Percent, Value: 0.5}} {
		t.Errorf("initial object-fit %v, position %+v", s.ObjectFit, s.ObjectPosition)
	}
}

func TestContain_Parse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		decl string
		want Containment
	}{
		{"contain: paint", ContainPaint},
		{"contain: content", ContainLayout | ContainStyle | ContainPaint},
		{"contain: size paint", ContainSize | ContainPaint},
		{"contain: paint paint", 0},
		{"contain: size inline-size", 0},
		{"contain: none", 0},
	}
	for _, tt := range tests {
		if got := styleOf(t, tt.decl).Contain; got != tt.want {
			t.Errorf("%s = %b, want %b", tt.decl, got, tt.want)
		}
	}
}

func TestCalc_Constants(t *testing.T) {
	t.Parallel()
	s := styleOf(t, "border-top-left-radius: calc(infinity * 1px); width: calc(2px * pi)")
	if s.BorderRadius[TopLeft].H.Value != maxLength {
		t.Errorf("infinite radius = %+v, want clamped to %v", s.BorderRadius[TopLeft].H, maxLength)
	}
	if math.Abs(s.Width.Value-2*math.Pi*Pt) > 1e-9 {
		t.Errorf("width %+v", s.Width)
	}
}
