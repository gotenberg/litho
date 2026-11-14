package ooxml

import (
	"context"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

func TestMeasure(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		s    string
		unit float64
		want float64
		ok   bool
	}{
		{"1440", TwipPt, 72, true},
		{"-720", TwipPt, -36, true},
		{"2.54cm", TwipPt, 72, true},
		{"12pt", TwipPt, 12, true},
		{"914400", EMUPt, 72, true},
		{"1e400", TwipPt, 0, false},
		{"x", TwipPt, 0, false},
		{"", TwipPt, 0, false},
		{"1e30", TwipPt, maxLength, true},
	} {
		got, ok := Measure(tc.s, tc.unit)
		if ok != tc.ok || math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("Measure(%q) = %v, %v; want %v, %v", tc.s, got, ok, tc.want, tc.ok)
		}
	}
}

func TestOnOffAndColors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		s       string
		present bool
		want    bool
	}{{"", false, true}, {"", true, true}, {"0", true, false}, {"false", true, false}, {"off", true, false}, {"on", true, true}, {"1", true, true}} {
		if got := OnOff(tc.s, tc.present); got != tc.want {
			t.Errorf("OnOff(%q, %v) = %v", tc.s, tc.present, got)
		}
	}
	c, ok := HexColor("FF8000")
	if !ok || c != (css.Color{R: 1, G: 128.0 / 255, B: 0, A: 1}) {
		t.Errorf("HexColor = %+v, %v", c, ok)
	}
	if _, ok := HexColor("auto"); ok {
		t.Error("HexColor(auto) parsed")
	}
	if got := Tint(css.Color{A: 1}, 0.5); got != (css.Color{R: 127.0 / 255, G: 127.0 / 255, B: 127.0 / 255, A: 1}) {
		t.Errorf("Tint = %+v", got)
	}
}

// TestTintShade_WordThemeColours holds Word's colours of litho/docx
// run-theme-fonts-colors: each theme colour of the Office theme under
// themeTint 99, themeShade BF and themeShade 80.
func TestTintShade_WordThemeColours(t *testing.T) {
	t.Parallel()
	type row struct{ base, tint, shadeBF, shade80 [3]int }
	rows := []row{
		{[3]int{0, 0, 0}, [3]int{102, 102, 102}, [3]int{-1}, [3]int{-1}},
		{[3]int{255, 255, 255}, [3]int{-1}, [3]int{191, 191, 191}, [3]int{128, 128, 128}},
		{[3]int{68, 84, 106}, [3]int{132, 150, 176}, [3]int{50, 62, 79}, [3]int{34, 42, 53}},
		{[3]int{231, 230, 230}, [3]int{-1}, [3]int{174, 170, 170}, [3]int{118, 113, 113}},
		{[3]int{68, 114, 196}, [3]int{142, 170, 219}, [3]int{47, 84, 150}, [3]int{31, 56, 100}},
		{[3]int{237, 125, 49}, [3]int{244, 176, 131}, [3]int{196, 89, 17}, [3]int{131, 60, 11}},
		{[3]int{165, 165, 165}, [3]int{201, 201, 201}, [3]int{123, 123, 123}, [3]int{82, 82, 82}},
		{[3]int{255, 192, 0}, [3]int{255, 217, 102}, [3]int{191, 143, 0}, [3]int{128, 96, 0}},
		{[3]int{91, 155, 213}, [3]int{156, 194, 229}, [3]int{46, 116, 181}, [3]int{31, 78, 121}},
		{[3]int{112, 173, 71}, [3]int{168, 208, 141}, [3]int{83, 129, 53}, [3]int{56, 86, 35}},
		{[3]int{5, 99, 193}, [3]int{72, 160, 250}, [3]int{-1}, [3]int{-1}},
	}
	rgb := func(v [3]int) css.Color {
		return css.Color{R: float64(v[0]) / 255, G: float64(v[1]) / 255, B: float64(v[2]) / 255, A: 1}
	}
	exact, total := 0, 0
	check := func(name string, got css.Color, want [3]int) {
		if want[0] < 0 {
			return
		}
		total++
		g := [3]int{int(math.Round(got.R * 255)), int(math.Round(got.G * 255)), int(math.Round(got.B * 255))}
		if g == want {
			exact++
			return
		}
		for i := range g {
			if d := g[i] - want[i]; d < -1 || d > 1 {
				t.Errorf("%s: %v, want %v", name, g, want)
				return
			}
		}
	}
	for _, r := range rows {
		check("tint 99", Tint(rgb(r.base), 0x99/255.0), r.tint)
		check("shade BF", Shade(rgb(r.base), 0xBF/255.0), r.shadeBF)
		check("shade 80", Shade(rgb(r.base), 0x80/255.0), r.shade80)
	}
	t.Logf("%d of %d exact", exact, total)
	if exact < 22 {
		t.Errorf("%d of %d exact, want at least 22", exact, total)
	}
}

func TestParseTheme(t *testing.T) {
	t.Parallel()
	const theme = `<a:theme xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><a:themeElements>` +
		`<a:clrScheme name="Office"><a:dk1><a:sysClr val="windowText" lastClr="000000"/></a:dk1><a:lt1><a:sysClr val="window" lastClr="FFFFFF"/></a:lt1>` +
		`<a:accent1><a:srgbClr val="4472C4"/></a:accent1></a:clrScheme>` +
		`<a:fontScheme name="Office"><a:majorFont><a:latin typeface="Calibri Light"/><a:ea typeface=""/><a:cs typeface=""/><a:font script="Jpan" typeface="Yu Gothic Light"/></a:majorFont>` +
		`<a:minorFont><a:latin typeface="Calibri"/><a:ea typeface=""/><a:cs typeface="Arial"/></a:minorFont></a:fontScheme></a:themeElements></a:theme>`
	root, err := Parse(context.Background(), []byte(theme), nil)
	if err != nil {
		t.Fatal(err)
	}
	th := ParseTheme(root)
	if c, ok := th.Color("text1"); !ok || c != (css.Color{A: 1}) {
		t.Errorf("text1 = %+v, %v", c, ok)
	}
	if c, ok := th.Color("accent1"); !ok || c.B < 0.7 {
		t.Errorf("accent1 = %+v, %v", c, ok)
	}
	for name, want := range map[string]string{"majorHAnsi": "Calibri Light", "minorAscii": "Calibri", "minorBidi": "Arial", "majorEastAsia": ""} {
		if got := th.Font(name); got != want {
			t.Errorf("Font(%s) = %q, want %q", name, got, want)
		}
	}
	if th.Major.Scripts["Jpan"] != "Yu Gothic Light" {
		t.Errorf("script font = %q", th.Major.Scripts["Jpan"])
	}
}
