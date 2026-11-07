package ooxml

import (
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

func TestParseVMLStyle(t *testing.T) {
	t.Parallel()
	st := ParseVMLStyle("position:absolute; Margin-Left:12pt;width:100;height:2in;left:8px;z-index:-5;bad;width:200")
	for _, tc := range []struct {
		name string
		unit float64
		want float64
		ok   bool
	}{
		{"margin-left", 1, 12, true},
		{"width", 1, 200, true},
		{"height", 1, 144, true},
		{"left", 1, 6, true},
		{"top", 1, 0, false},
	} {
		got, ok := st.Length(tc.name, tc.unit)
		if ok != tc.ok || math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("Length(%q) = %v, %v; want %v, %v", tc.name, got, ok, tc.want, tc.ok)
		}
	}
	if st["position"] != "absolute" || st["z-index"] != "-5" {
		t.Errorf("style %v", st)
	}
	if n := len(ParseVMLStyle(strings.Repeat("a:1;", 2*maxVMLDeclarations))); n != 1 {
		t.Errorf("%d declarations", n)
	}
}

func TestVMLValues(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		s    string
		want css.Color
		ok   bool
	}{
		{"#396", css.Color{R: 0.2, G: 0.6, B: 0.4, A: 1}, true},
		{"#FF0000", css.Color{R: 1, A: 1}, true},
		{"red", css.Color{R: 1, A: 1}, true},
		{"#4f81bd [3204]", css.Color{R: 0x4f / 255.0, G: 0x81 / 255.0, B: 0xbd / 255.0, A: 1}, true},
		{"window", css.Color{}, false},
		{"#12", css.Color{}, false},
	} {
		got, ok := VMLColor(tc.s)
		if ok != tc.ok || math.Abs(got.R-tc.want.R) > 1e-9 || math.Abs(got.G-tc.want.G) > 1e-9 || math.Abs(got.B-tc.want.B) > 1e-9 {
			t.Errorf("VMLColor(%q) = %v, %v; want %v, %v", tc.s, got, ok, tc.want, tc.ok)
		}
	}
	for _, tc := range []struct {
		s    string
		want float64
		ok   bool
	}{
		{"32768f", 0.5, true},
		{"50%", 0.5, true},
		{".25", 0.25, true},
		{"3", 1, true},
		{"x", 0, false},
	} {
		got, ok := VMLFraction(tc.s)
		if ok != tc.ok || math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("VMLFraction(%q) = %v, %v; want %v, %v", tc.s, got, ok, tc.want, tc.ok)
		}
	}
	if x, y := VMLPair("21600,1000", 0); x != 21600 || y != 1000 {
		t.Errorf("VMLPair = %v, %v", x, y)
	}
	if x, y := VMLPair("9201", 0); x != 9201 || y != 0 {
		t.Errorf("VMLPair with one number = %v, %v", x, y)
	}
	if x, y := VMLPair("", 1000); x != 1000 || y != 1000 {
		t.Errorf("VMLPair default = %v, %v", x, y)
	}
}
