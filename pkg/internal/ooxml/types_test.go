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
	if got := Tint(css.Color{A: 1}, 0.5); got != (css.Color{R: 0.5, G: 0.5, B: 0.5, A: 1}) {
		t.Errorf("Tint = %+v", got)
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
