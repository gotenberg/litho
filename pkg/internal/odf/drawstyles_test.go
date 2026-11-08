package odf

import (
	"context"
	"math"
	"slices"
	"testing"
)

// drawNS declares the namespaces of the drawing styles beside ns's.
const drawNS = ns + ` xmlns:draw="urn:oasis:names:tc:opendocument:xmlns:drawing:1.0"` +
	` xmlns:xlink="http://www.w3.org/1999/xlink"` +
	` xmlns:meta="urn:oasis:names:tc:opendocument:xmlns:meta:1.0"` +
	` xmlns:loext="urn:org:documentfoundation:names:experimental:office:xmlns:loext:1.0"`

func TestDrawStyles_Read(t *testing.T) {
	doc := `<office:document ` + drawNS + ` office:version="1.3"><office:styles>` +
		`<draw:gradient draw:name="g" draw:style="axial" draw:cx="20%" draw:start-color="#ff0000" draw:end-color="#0000ff" draw:start-intensity="50%" draw:angle="30deg" draw:border="10%">` +
		`<loext:gradient-stop svg:offset="0" loext:color-value="#000000"/><loext:gradient-stop svg:offset="0.5" loext:color-value="#808080"/><loext:gradient-stop svg:offset="1" loext:color-value="#ffffff"/></draw:gradient>` +
		`<draw:opacity draw:name="o" draw:start="80%" draw:end="20%"/>` +
		`<draw:hatch draw:name="h" draw:style="triple" draw:color="#00ff00" draw:distance="1mm" draw:rotation="45deg"/>` +
		`<draw:marker draw:name="m" svg:viewBox="0 0 20 30" svg:d="M0 0"/>` +
		`<draw:stroke-dash draw:name="d" draw:style="round" draw:dots1="2" draw:dots1-length="150%" draw:dots2="1" draw:distance="50%"/>` +
		`<draw:fill-image draw:name="f" xlink:href="Pictures/a.png"/>` +
		`</office:styles></office:document>`
	p, err := Open(context.Background(), []byte(doc), Options{})
	if err != nil {
		t.Fatal(err)
	}
	d := NewDrawStyles(p)
	g := d.Gradients["g"]
	if g == nil || g.Style != "axial" || g.CX != 0.2 || g.CY != 0.5 || g.Start != (Color{R: 128}) || g.End != (Color{B: 255}) || g.Angle != 30 || g.Border != 0.1 || len(g.Stops) != 3 {
		t.Errorf("gradient = %+v", g)
	}
	if o := d.Opacities["o"]; o == nil || o.StartValue != 0.8 || o.EndValue != 0.2 {
		t.Errorf("opacity = %+v", o)
	}
	if h := d.Hatches["h"]; h == nil || h.Style != "triple" || h.Color != (Color{G: 255}) || math.Abs(h.Distance-72/25.4) > 1e-9 || h.Rotation != 45 {
		t.Errorf("hatch = %+v", h)
	}
	if m := d.Markers["m"]; m == nil || m.ViewBox != [4]float64{0, 0, 20, 30} {
		t.Errorf("marker = %+v", m)
	}
	if s := d.Dashes["d"]; s == nil || !s.Relative || s.Dots1 != 2 || s.Dots2 != 1 || s.Dots1Length != 1.5 || s.Distance != 0.5 {
		t.Errorf("dash = %+v", s)
	}
	if d.FillImages["f"] == nil {
		t.Error("no fill image")
	}
}

func TestStrokeDash_DashArray(t *testing.T) {
	const cm = 72 / 2.54
	tests := []struct {
		name  string
		dash  StrokeDash
		width float64
		want  []float64
	}{
		{"absolute", StrokeDash{Dots1: 1, Dots1Length: 0.5 * cm, Dots2: 1, Dots2Length: 0.1 * cm, Distance: 0.2 * cm}, 0.1 * cm,
			[]float64{0.5 * cm, 0.2 * cm, 0.1 * cm, 0.2 * cm}},
		{"relative", StrokeDash{Dots1: 2, Dots1Length: 2, Distance: 1, Relative: true}, 0.2 * cm,
			[]float64{0.4 * cm, 0.2 * cm, 0.4 * cm, 0.2 * cm}},
		{"zero lengths take the width", StrokeDash{Dots1: 1, Distance: 0.05 * cm}, 0.1 * cm, []float64{0.1 * cm, 0.05 * cm}},
		{"short absolute dashes widen", StrokeDash{Dots1: 1, Dots1Length: 0.1, Distance: 0.1}, 0, []float64{smallestDash, smallestDash}},
		{"hairline", StrokeDash{Dots1: 1, Distance: 1, Relative: true}, 0, []float64{smallestDash, smallestDash}},
		{"no dots", StrokeDash{Distance: 1}, 1, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.dash.DashArray(tt.width)
			if !slices.EqualFunc(got, tt.want, func(a, b float64) bool { return math.Abs(a-b) < 1e-9 }) {
				t.Errorf("DashArray = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseAngle(t *testing.T) {
	tests := []struct {
		in     string
		legacy bool
		want   float64
		ok     bool
	}{
		{"30deg", false, 30, true},
		{"30deg", true, 30, true},
		{"450", false, 90, true},
		{"450", true, 45, true},
		{"100grad", false, 90, true},
		{"-90deg", false, 270, true},
		{"1rad", false, 180 / math.Pi, true},
		{"x", false, 0, false},
	}
	for _, tt := range tests {
		got, ok := ParseAngle(tt.in, tt.legacy)
		if ok != tt.ok || math.Abs(got-tt.want) > 1e-9 {
			t.Errorf("ParseAngle(%q, %v) = %v, %v, want %v, %v", tt.in, tt.legacy, got, ok, tt.want, tt.ok)
		}
	}
}

func TestLegacyAngles(t *testing.T) {
	tests := []struct {
		version, generator string
		want               bool
	}{
		{"1.3", "LibreOffice/26.8", false},
		{"1.2", "LibreOffice/7.1.0.3$Linux", false},
		{"1.2", "LibreOffice/6.4.7.2$Windows", true},
		{"1.2", "OpenOffice/4.1.2$Win32", true},
		{"1.2", "LibreOfficeDev/6.4.0.0.alpha0$Linux_X86_64", true},
		{"1.2", "LibreOfficeDev/7.6.0.0.alpha0$Linux_X86_64", false},
		{"1.2", "", false},
		{"1.1", "", true},
		{"", "", true},
	}
	for _, tt := range tests {
		ver := ""
		if tt.version != "" {
			ver = ` office:version="` + tt.version + `"`
		}
		doc := `<office:document ` + drawNS + ver + `><office:meta><meta:generator>` + tt.generator + `</meta:generator></office:meta></office:document>`
		p, err := Open(context.Background(), []byte(doc), Options{})
		if err != nil {
			t.Fatal(err)
		}
		if got := LegacyAngles(p); got != tt.want {
			t.Errorf("LegacyAngles(%q, %q) = %v, want %v", tt.version, tt.generator, got, tt.want)
		}
	}
}
