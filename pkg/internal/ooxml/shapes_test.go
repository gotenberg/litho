package ooxml

import (
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/shape"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

func TestParseShapeStyle_Line(t *testing.T) {
	sp := spPrOf(t, `<a:ln w="25400" cap="rnd"><a:solidFill><a:srgbClr val="FF0000"/></a:solidFill><a:prstDash val="dash"/><a:miter/>`+
		`<a:headEnd type="oval" w="sm" len="lg"/><a:tailEnd type="arrow"/></a:ln>`)
	s := ParseShapeStyle(sp, nil, nil)
	if s.Line == nil || s.Line.R != 1 || s.LineWidth != 2 || s.Dash != "dash" || s.Cap != typeset.RoundCap || s.Join != typeset.MiterJoin {
		t.Fatalf("%+v", s)
	}
	if s.Head != (shape.Arrowhead{Kind: shape.ArrowOval, Width: shape.ArrowSmall, Length: shape.ArrowLarge}) || s.Tail != (shape.Arrowhead{Kind: shape.ArrowOpen}) {
		t.Errorf("arrowheads %+v %+v", s.Head, s.Tail)
	}
	// The line, its oval at the start and its open arrow at the end.
	if got := s.Paint(&shape.Geometry{Preset: "line"}, typeset.Rect{Width: 100}); len(got) != 3 {
		t.Errorf("%d fragments", len(got))
	}
}

// TestParseShapeStyle_ThemeLine locks ECMA-376 Part 1, 20.1.4.2.19 lnRef:
// each line property the shape's spPr leaves unset comes from the theme
// line style the reference's idx names, its fill painted with phClr
// standing for the reference's colour.
func TestParseShapeStyle_ThemeLine(t *testing.T) {
	theme := ParseTheme(mustParse(t, `<a:theme `+effectNS+`><a:themeElements><a:clrScheme name="c"><a:accent1><a:srgbClr val="4F81BD"/></a:accent1></a:clrScheme>`+
		`<a:fmtScheme name="f"><a:lnStyleLst>`+
		`<a:ln w="9525"><a:solidFill><a:schemeClr val="phClr"><a:shade val="50000"/></a:schemeClr></a:solidFill></a:ln>`+
		`<a:ln w="25400" cap="sq"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill><a:prstDash val="dash"/><a:miter lim="800000"/></a:ln>`+
		`<a:ln w="38100"><a:noFill/></a:ln>`+
		`</a:lnStyleLst></a:fmtScheme></a:themeElements></a:theme>`))
	style := func(idx string) *Element {
		return mustParse(t, `<wps:style `+effectNS+` xmlns:wps="http://schemas.microsoft.com/office/word/2010/wordprocessingShape"><a:lnRef idx="`+idx+`"><a:srgbClr val="FF0000"/></a:lnRef></wps:style>`)
	}
	s := ParseShapeStyle(spPrOf(t, ``), style("2"), theme)
	if s.Line == nil || *s.Line != (css.Color{R: 1, A: 1}) || s.LineWidth != 2 || s.Cap != typeset.SquareCap || s.Join != typeset.MiterJoin || s.Dash != "dash" {
		t.Errorf("idx 2: %+v, want the theme's 2-point square-capped mitred dash in the reference's red", s)
	}
	s = ParseShapeStyle(spPrOf(t, `<a:ln w="12700"><a:round/><a:prstDash val="solid"/></a:ln>`), style("2"), theme)
	if s.LineWidth != 1 || s.Join != typeset.RoundJoin || s.Dash != "" || s.Cap != typeset.SquareCap {
		t.Errorf("own width, join and dash: %+v, want 1 point, round, solid, the theme's cap", s)
	}
	s = ParseShapeStyle(spPrOf(t, ``), style("1"), theme)
	if s.Line == nil || s.LineWidth != 0.75 || s.Line.G != 0 || s.Line.R <= 0 || s.Line.R >= 1 {
		t.Errorf("idx 1: %+v, want the reference's red shaded by the theme line", s.Line)
	}
	if s = ParseShapeStyle(spPrOf(t, ``), style("3"), theme); s.Line != nil {
		t.Errorf("idx 3: %+v, want no line: the theme line has none", s.Line)
	}
	if s = ParseShapeStyle(spPrOf(t, ``), style("4"), theme); s.Line == nil || s.LineWidth != 0.75 || s.Join != typeset.RoundJoin {
		t.Errorf("idx past the list: %+v, want the reference's colour at 0.75 point, round", s)
	}
}

func TestParseShapeStyle_CustomDash(t *testing.T) {
	tests := []struct {
		name, ds string
		want     []float64
	}{
		{"pairs in line widths", `<a:ds d="800000" sp="300000"/><a:ds d="100000" sp="300000"/>`, []float64{16, 6, 2, 6}},
		{"all zero is solid", `<a:ds d="0" sp="0"/>`, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sp := spPrOf(t, `<a:ln w="25400"><a:solidFill><a:srgbClr val="FF0000"/></a:solidFill><a:custDash>`+tt.ds+`</a:custDash></a:ln>`)
			s := ParseShapeStyle(sp, nil, nil)
			got := s.dash()
			if len(got) != len(tt.want) {
				t.Fatalf("dash %v, want %v", got, tt.want)
			}
			for i := range got {
				if math.Abs(got[i]-tt.want[i]) > 1e-9 {
					t.Errorf("dash %v, want %v", got, tt.want)
				}
			}
		})
	}
}

func TestParseShapeStyle_Gradient(t *testing.T) {
	sp := spPrOf(t, `<a:gradFill><a:gsLst><a:gs pos="0"><a:srgbClr val="FF0000"/></a:gs><a:gs pos="100000"><a:srgbClr val="0000FF"/></a:gs></a:gsLst><a:lin ang="5400000"/></a:gradFill>`)
	s := ParseShapeStyle(sp, nil, nil)
	if s.Gradient == nil || len(s.Gradient.Stops) < 3 || s.Gradient.Stops[0].Color.R != 1 || s.Gradient.Stops[len(s.Gradient.Stops)-1].Color.B != 1 || s.Gradient.Angle != 90 || s.Fill == nil || s.Fill.R != 1 {
		t.Fatalf("%+v", s.Gradient)
	}
	frags := s.Paint(nil, typeset.Rect{Width: 10, Height: 10})
	if p, ok := frags[0].(*typeset.Path); !ok || p.Fill.Shading == nil {
		t.Errorf("%+v", frags)
	}
	radial := ParseShapeStyle(spPrOf(t, `<a:gradFill><a:gsLst><a:gs pos="50000"><a:srgbClr val="00FF00"/></a:gs></a:gsLst><a:path path="circle"/></a:gradFill>`), nil, nil)
	if radial.Gradient == nil || !radial.Gradient.Radial || math.Abs(radial.Gradient.Stops[0].Offset-0.5) > 1e-9 {
		t.Errorf("%+v", radial.Gradient)
	}
}

// TestShapeStylePaint_Shades checks a 3D-looking preset's darkened face.
func TestShapeStylePaint_Shades(t *testing.T) {
	s := ParseShapeStyle(spPrOf(t, `<a:solidFill><a:srgbClr val="808080"/></a:solidFill><a:ln><a:noFill/></a:ln>`), nil, nil)
	frags := s.Paint(&shape.Geometry{Preset: "cube"}, typeset.Rect{Width: 100, Height: 100})
	var colors []float64
	for _, f := range frags {
		if p, ok := f.(*typeset.Path); ok && p.Fill != nil {
			colors = append(colors, p.Fill.Color.R)
		}
	}
	if len(colors) < 3 || colors[0] == colors[1] {
		t.Errorf("cube faces %v", colors)
	}
}

func TestVMLPreset(t *testing.T) {
	for spt, want := range map[int]string{1: "rect", 2: "roundRect", 3: "ellipse", 5: "triangle", 13: "rightArrow", 20: "line", 202: "rect", 187: "star4"} {
		if got, ok := VMLPreset(spt); !ok || got != want {
			t.Errorf("spt %d: %q, want %q", spt, got, want)
		}
	}
	for spt, p := range vmlPresets {
		if !shape.Known(&shape.Geometry{Preset: p}) || p == "" {
			t.Errorf("spt %d maps onto %q, which the engine does not draw", spt, p)
		}
	}
	for _, spt := range []int{136, 24} {
		if _, ok := VMLPreset(spt); ok {
			t.Errorf("WordArt type %d has a preset", spt)
		}
	}
	if _, ok := VMLPreset(14); ok {
		t.Error("the thick arrow has a preset")
	}
}

// TestParseShapeStyle_HSLAndLinearRGB checks the hslClr and scrgbClr
// colour choices of a shape's fill (ECMA-376 Part 1, 20.1.2.3.13 and
// 20.1.2.3.30).
//
// # Corpus evidence
//
//   - litho/docx shapes-fills-lines: Word fills the hslClr hue 120, sat
//     100%, lum 50% rounded rectangle pure green.
func TestParseShapeStyle_HSLAndLinearRGB(t *testing.T) {
	hsl := ParseShapeStyle(spPrOf(t, `<a:solidFill><a:hslClr hue="7200000" sat="100000" lum="50000"/></a:solidFill>`), nil, nil)
	if hsl.Fill == nil || hsl.Fill.R != 0 || hsl.Fill.G != 1 || hsl.Fill.B != 0 {
		t.Errorf("hslClr fill %+v, want green", hsl.Fill)
	}
	lin := ParseShapeStyle(spPrOf(t, `<a:solidFill><a:scrgbClr r="100000" g="0" b="0"><a:lumMod val="50000"/></a:scrgbClr></a:solidFill>`), nil, nil)
	if lin.Fill == nil || math.Abs(lin.Fill.R-0.5) > 1e-9 || lin.Fill.G != 0 {
		t.Errorf("scrgbClr fill %+v, want red at half its lightness", lin.Fill)
	}
}

// TestParseShapeStyle_PatternPictureLineGradient checks a pattern fill, a
// picture fill and a gradient line (20.1.8.47 pattFill, 20.1.8.14
// blipFill, 20.1.2.2.24 ln).
func TestParseShapeStyle_PatternPictureLineGradient(t *testing.T) {
	patt := ParseShapeStyle(spPrOf(t, `<a:pattFill prst="smCheck"><a:fgClr><a:srgbClr val="C00000"/></a:fgClr><a:bgClr><a:srgbClr val="FFFFFF"/></a:bgClr></a:pattFill>`), nil, nil)
	if p := patt.paint(shape.FillNorm); p.Kind != shape.PaintPattern || p.Pattern != "smCheck" || p.Background.G != 1 || p.Color.G != 0 {
		t.Errorf("pattern paint %+v", p)
	}
	tile := ParseShapeStyle(spPrOf(t, `<a:blipFill><a:blip r:embed="rId6" xmlns:r="`+NSR+`"/><a:tile sx="50000" sy="100000"/></a:blipFill>`), nil, nil)
	if tile.Blip == nil || tile.Blip.Embed != "rId6" || !tile.Blip.Tile || tile.Blip.ScaleX != 0.5 || tile.Blip.ScaleY != 1 {
		t.Fatalf("blip %+v", tile.Blip)
	}
	if p := tile.paint(shape.FillNorm); p.Kind != shape.PaintNone {
		t.Errorf("an unresolved picture paints %v", p.Kind)
	}
	tile.Blip.Picture, tile.Blip.Width, tile.Blip.Height = &typeset.ImageSource{}, 12, 12
	if p := tile.paint(shape.FillNorm); p.Kind != shape.PaintPicture || p.TileWidth != 6 || p.TileHeight != 12 {
		t.Errorf("tile paint %+v", p)
	}
	line := ParseShapeStyle(spPrOf(t, `<a:noFill/><a:ln w="76200"><a:gradFill><a:gsLst><a:gs pos="0"><a:srgbClr val="FF0000"/></a:gs><a:gs pos="100000"><a:srgbClr val="0000FF"/></a:gs></a:gsLst><a:lin ang="0"/></a:gradFill></a:ln>`), nil, nil)
	frags := line.Paint(&shape.Geometry{Preset: "ellipse"}, typeset.Rect{Width: 280, Height: 64})
	if p, ok := frags[0].(*typeset.Path); len(frags) != 1 || !ok || p.Fill == nil || p.Fill.Shading == nil {
		t.Errorf("gradient line drew %+v", frags)
	}
	rect := ParseShapeStyle(spPrOf(t, `<a:gradFill><a:gsLst><a:gs pos="0"><a:srgbClr val="FFC000"/></a:gs></a:gsLst><a:path path="rect"/></a:gradFill>`), nil, nil)
	if rect.Gradient == nil || !rect.Gradient.Rect || rect.Gradient.Radial {
		t.Errorf("path rect %+v", rect.Gradient)
	}
}
