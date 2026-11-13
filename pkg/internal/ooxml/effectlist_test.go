package ooxml

import (
	"math"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

const (
	effectNS = `xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"`
	w14NS    = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:w14="http://schemas.microsoft.com/office/word/2010/wordml"`
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestParseEffectList_ReadsTheMembers(t *testing.T) {
	spPr := mustParse(t, `<a:spPr `+effectNS+`><a:effectLst>`+
		`<a:glow rad="139700"><a:srgbClr val="FF0000"><a:alpha val="40000"/></a:srgbClr></a:glow>`+
		`<a:outerShdw blurRad="76200" dist="38100" dir="2700000" sx="90000" sy="-30000" kx="-800400" algn="bl"><a:prstClr val="black"/></a:outerShdw>`+
		`<a:reflection blurRad="6350" stA="52000" endPos="35000" dist="12700" dir="5400000" sy="-100000" algn="bl" rotWithShape="0"/>`+
		`<a:softEdge rad="63500"/><a:innerShdw blurRad="63500"/><a:prstShdw prst="shdw1"/><a:blur rad="12700"/></a:effectLst></a:spPr>`)
	l := ParseEffectList(spPr, nil, nil)
	fx := l.Effects
	if g := fx.Glow; g == nil || !near(g.Radius, 11) || g.Color != (css.Color{R: 1, A: 0.4}) {
		t.Errorf("glow %+v, want 11 points of red at 0.4", fx.Glow)
	}
	if s := fx.Shadow; s == nil || !near(s.Blur, 6) || !near(s.Dist, 3) || !near(s.Dir, 45) || !near(s.SX, 0.9) || !near(s.SY, -0.3) || !near(s.KX, -13.34) || s.Align != (css.RectAlign{X: 0, Y: 1}) || s.Color.A != 1 {
		t.Errorf("shadow %+v", fx.Shadow)
	}
	if r := fx.Reflection; r == nil || !near(r.Blur, 0.5) || !near(r.StartAlpha, 0.52) || !near(r.EndAlpha, 0) || !near(r.StartPos, 0) || !near(r.EndPos, 0.35) || !near(r.Dist, 1) || !near(r.FadeDir, 90) || !near(r.SX, 1) || !near(r.SY, -1) {
		t.Errorf("reflection %+v", fx.Reflection)
	}
	if !near(fx.SoftEdge, 5) {
		t.Errorf("soft edge %v, want 5", fx.SoftEdge)
	}
	if want := []string{"inner shadow", "preset shadow", "blur"}; !slices.Equal(l.Undrawn, want) {
		t.Errorf("undrawn %v, want %v", l.Undrawn, want)
	}
}

func TestParseEffectList_ThemeReference(t *testing.T) {
	theme := ParseTheme(mustParse(t, `<a:theme `+effectNS+`><a:themeElements><a:clrScheme name="c"><a:accent1><a:srgbClr val="4472C4"/></a:accent1></a:clrScheme>`+
		`<a:fmtScheme name="f"><a:effectStyleLst><a:effectStyle><a:effectLst/></a:effectStyle>`+
		`<a:effectStyle><a:effectLst><a:outerShdw blurRad="57150" dist="19050" dir="5400000" algn="ctr"><a:schemeClr val="phClr"><a:alpha val="63000"/></a:schemeClr></a:outerShdw></a:effectLst></a:effectStyle>`+
		`</a:effectStyleLst></a:fmtScheme></a:themeElements></a:theme>`))
	style := mustParse(t, `<wps:style `+effectNS+` xmlns:wps="http://schemas.microsoft.com/office/word/2010/wordprocessingShape"><a:effectRef idx="2"><a:schemeClr val="accent1"/></a:effectRef></wps:style>`)
	l := ParseEffectList(mustParse(t, `<a:spPr `+effectNS+`/>`), style, theme)
	s := l.Effects.Shadow
	if s == nil || !near(s.Blur, 4.5) || !near(s.Color.A, 0.63) || !near(s.Color.B, 0xC4/255.0) {
		t.Fatalf("shadow %+v, want the theme's in accent 1 at 0.63", s)
	}
	// The shape's own empty list overrides the theme's.
	l = ParseEffectList(mustParse(t, `<a:spPr `+effectNS+`><a:effectLst/></a:spPr>`), style, theme)
	if !l.Empty() {
		t.Errorf("effects %+v, want none", l)
	}
	l = ParseEffectList(mustParse(t, `<a:spPr `+effectNS+`><a:effectDag><a:cont/></a:effectDag></a:spPr>`), style, theme)
	if l.Effects != (css.OfficeEffects{}) || !slices.Equal(l.Undrawn, []string{"effect container"}) {
		t.Errorf("effects %+v, want an undrawn container", l)
	}
}

func TestParseTextEffects_AlphaIsTransparency(t *testing.T) {
	theme := &Theme{Colors: map[string]css.Color{"accent2": {R: 0xED / 255.0, G: 0x7D / 255.0, B: 0x31 / 255.0, A: 1}}}
	rPr := mustParse(t, `<w:rPr `+w14NS+`><w:b/>`+
		`<w14:glow w14:rad="101600"><w14:schemeClr w14:val="accent2"><w14:alpha w14:val="60000"/><w14:satMod w14:val="175000"/></w14:schemeClr></w14:glow>`+
		`<w14:shadow w14:blurRad="50800" w14:dist="38100" w14:dir="2700000" w14:sx="100000" w14:sy="100000" w14:kx="0" w14:ky="0" w14:algn="tl"><w14:srgbClr w14:val="000000"><w14:alpha w14:val="60000"/></w14:srgbClr></w14:shadow>`+
		`<w14:reflection w14:blurRad="6350" w14:stA="55000" w14:stPos="0" w14:endA="300" w14:endPos="45500" w14:dist="0" w14:dir="5400000" w14:fadeDir="5400000" w14:sx="100000" w14:sy="-100000" w14:kx="0" w14:ky="0" w14:algn="bl"/>`+
		`</w:rPr>`)
	fx := ParseTextEffects(rPr, theme).Effects
	if g := fx.Glow; g == nil || !near(g.Radius, 8) || !near(g.Color.A, 0.4) || g.Color.R != 1 {
		t.Errorf("glow %+v, want 8 points at 0.4, saturated", fx.Glow)
	}
	if s := fx.Shadow; s == nil || !near(s.Blur, 4) || !near(s.Dist, 3) || !near(s.Color.A, 0.4) || s.Align != (css.RectAlign{}) {
		t.Errorf("shadow %+v, want 4 points blurred at 0.4 aligned top left", fx.Shadow)
	}
	if r := fx.Reflection; r == nil || !near(r.StartAlpha, 0.55) || !near(r.EndAlpha, 0.003) || !near(r.EndPos, 0.455) || !near(r.SY, -1) {
		t.Errorf("reflection %+v", fx.Reflection)
	}
}

func TestParseTextPaint(t *testing.T) {
	rPr := mustParse(t, `<w:rPr `+w14NS+`>`+
		`<w14:textOutline w14:w="9525"><w14:solidFill><w14:srgbClr w14:val="C00000"/></w14:solidFill></w14:textOutline>`+
		`<w14:textFill><w14:gradFill><w14:gsLst><w14:gs w14:pos="100000"><w14:srgbClr w14:val="ED7D31"/></w14:gs><w14:gs w14:pos="0"><w14:srgbClr w14:val="4472C4"/></w14:gs></w14:gsLst><w14:lin w14:ang="5400000" w14:scaled="0"/></w14:gradFill></w14:textFill>`+
		`</w:rPr>`)
	p, solid := ParseTextPaint(rPr, nil)
	if solid != nil || p == nil || p.NoFill || len(p.Gradient) != 2 || p.Gradient[0].Pos != 0 || !near(p.Angle, 90) {
		t.Fatalf("paint %+v, solid %v, want a vertical gradient of two stops in order", p, solid)
	}
	if o := p.Outline; o == nil || !near(o.Width, 0.75) || !near(o.Color.R, 0xC0/255.0) {
		t.Errorf("outline %+v, want 0.75 point of C00000", p.Outline)
	}
	p, _ = ParseTextPaint(mustParse(t, `<w:rPr `+w14NS+`><w14:textFill><w14:noFill/></w14:textFill></w:rPr>`), nil)
	if p == nil || !p.NoFill || p.Outline != nil {
		t.Errorf("paint %+v, want unfilled", p)
	}
	p, solid = ParseTextPaint(mustParse(t, `<w:rPr `+w14NS+`><w14:textFill><w14:solidFill><w14:srgbClr w14:val="FF0000"><w14:alpha w14:val="25000"/></w14:srgbClr></w14:solidFill></w14:textFill></w:rPr>`), nil)
	if p != nil || solid == nil || solid.R != 1 || !near(solid.A, 0.75) {
		t.Errorf("paint %+v, solid %+v, want red at 0.75 and no paint", p, solid)
	}
}
