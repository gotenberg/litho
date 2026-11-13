package pptx

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/css"
)

func nearly(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func colorNear(a, b css.Color) bool {
	return nearly(a.R, b.R) && nearly(a.G, b.G) && nearly(a.B, b.B) && nearly(a.A, b.A)
}

// TestEffects_EffectList checks the effect list read into the shape's
// shadow, glow and soft edge, the shape being 10 by 20 pt.
func TestEffects_EffectList(t *testing.T) {
	t.Parallel()
	style := `<p:style><a:lnRef idx="0"/><a:fillRef idx="0"/><a:effectRef idx="3"><a:srgbClr val="00FF00"/></a:effectRef><a:fontRef idx="minor"/></p:style>`
	for _, tc := range []struct {
		name, spPr, style string
		shadow            *slides.Shadow
		glow              *slides.Glow
		soft              float64
		rect              bool
		warn              string
	}{
		{
			"outer shadow", `<a:effectLst><a:outerShdw blurRad="76200" dist="127000" dir="5400000"><a:srgbClr val="112233"><a:alpha val="40000"/></a:srgbClr></a:outerShdw></a:effectLst>`, "",
			&slides.Shadow{Color: css.Color{R: 0x11 / 255.0, G: 0x22 / 255.0, B: 0x33 / 255.0, A: 1}, Opacity: 0.4, OffsetY: 10, ScaleX: 1, ScaleY: 1, Blur: 6}, nil, 0, false, "",
		},
		{
			"a shadow without a colour is white", `<a:effectLst><a:outerShdw dist="127000"/></a:effectLst>`, "",
			&slides.Shadow{Color: css.Color{R: 1, G: 1, B: 1, A: 1}, Opacity: 1, OffsetX: 10, ScaleX: 1, ScaleY: 1}, nil, 0, false, "",
		},
		{
			"a scale keeps the bottom by default", `<a:effectLst><a:outerShdw sx="50000" sy="50000"><a:prstClr val="black"/></a:outerShdw></a:effectLst>`, "",
			&slides.Shadow{Color: css.Color{A: 1}, Opacity: 1, OffsetY: 5, ScaleX: 0.5, ScaleY: 0.5}, nil, 0, false, "",
		},
		{
			"a scale about the top left corner", `<a:effectLst><a:outerShdw sx="200000" sy="50000" algn="tl"><a:prstClr val="black"/></a:outerShdw></a:effectLst>`, "",
			&slides.Shadow{Color: css.Color{A: 1}, Opacity: 1, OffsetX: 5, OffsetY: -5, ScaleX: 2, ScaleY: 0.5}, nil, 0, false, "",
		},
		{
			"glow", `<a:effectLst><a:glow rad="127000"><a:schemeClr val="accent2"><a:alpha val="50000"/></a:schemeClr></a:glow></a:effectLst>`, "",
			nil, &slides.Glow{Color: css.Color{R: 0xC0 / 255.0, G: 0x50 / 255.0, B: 0x4D / 255.0, A: 1}, Opacity: 0.5, Radius: 10}, 0, true, "",
		},
		{"a glow without a colour is none", `<a:effectLst><a:glow rad="127000"/></a:effectLst>`, "", nil, nil, 0, false, ""},
		{"soft edge", `<a:effectLst><a:softEdge rad="63500"/></a:effectLst>`, "", nil, nil, 5, true, ""},
		{"a bevelled shape prints no soft edge", `<a:effectLst><a:softEdge rad="63500"/></a:effectLst><a:sp3d><a:bevelT/></a:sp3d>`, "", nil, nil, 0, false, ""},
		{
			"the theme's effect style", "", style,
			&slides.Shadow{Color: css.Color{G: 1, A: 1}, Opacity: 1, OffsetX: 2, ScaleX: 1, ScaleY: 1, Blur: 3}, nil, 0, false, "",
		},
		{"an empty list overrides the style", `<a:effectLst/>`, style, nil, nil, 0, false, ""},
		{"preset shadow", `<a:effectLst><a:prstShdw prst="shdw1"><a:prstClr val="black"/></a:prstShdw></a:effectLst>`, "", nil, nil, 0, false, "preset shadows"},
		{"skewed shadow", `<a:effectLst><a:outerShdw kx="-800400"><a:prstClr val="black"/></a:outerShdw></a:effectLst>`, "", &slides.Shadow{Color: css.Color{A: 1}, Opacity: 1, ScaleX: 1, ScaleY: 1}, nil, 0, false, "skew of shadows"},
		{"a fill overlay of a gradient", `<a:effectLst><a:fillOverlay blend="mult"><a:gradFill><a:gsLst><a:gs pos="0"><a:srgbClr val="FF0000"/></a:gs></a:gsLst></a:gradFill></a:fillOverlay></a:effectLst>`, "", nil, nil, 0, false, "fill overlays"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			theme := strings.Replace(testTheme, `<a:effectStyle><a:effectLst/></a:effectStyle></a:effectStyleLst>`,
				`<a:effectStyle><a:effectLst><a:outerShdw blurRad="38100" dist="25400"><a:schemeClr val="phClr"/></a:outerShdw></a:effectLst></a:effectStyle></a:effectStyleLst>`, 1)
			doc, warnings := readDeck(t, testDeck{slides: []testSlide{{tree: sp("S", `<a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:solidFill><a:srgbClr val="FF0000"/></a:solidFill>`+tc.spPr, tc.style)}}, parts: map[string]string{"ppt/theme/theme1.xml": theme}})
			s := shapeNamed(doc.Slides[0].Shapes, "S")
			if s == nil {
				t.Fatal("no shape S")
			}
			switch {
			case (s.Shadow == nil) != (tc.shadow == nil):
				t.Errorf("shadow %+v, want %+v", s.Shadow, tc.shadow)
			case s.Shadow != nil:
				got, want := *s.Shadow, *tc.shadow
				if !colorNear(got.Color, want.Color) || !nearly(got.Opacity, want.Opacity) || !nearly(got.OffsetX, want.OffsetX) || !nearly(got.OffsetY, want.OffsetY) ||
					!nearly(got.ScaleX, want.ScaleX) || !nearly(got.ScaleY, want.ScaleY) || !nearly(got.Blur, want.Blur) {
					t.Errorf("shadow %+v, want %+v", got, want)
				}
			}
			switch {
			case (s.Glow == nil) != (tc.glow == nil):
				t.Errorf("glow %+v, want %+v", s.Glow, tc.glow)
			case s.Glow != nil:
				if !colorNear(s.Glow.Color, tc.glow.Color) || !nearly(s.Glow.Opacity, tc.glow.Opacity) || !nearly(s.Glow.Radius, tc.glow.Radius) {
					t.Errorf("glow %+v, want %+v", *s.Glow, *tc.glow)
				}
			}
			if !nearly(s.SoftEdge, tc.soft) {
				t.Errorf("soft edge %v, want %v", s.SoftEdge, tc.soft)
			}
			if rect := s.Geometry != nil && s.Geometry.Preset == "rect"; rect != tc.rect {
				t.Errorf("geometry %+v, want the rectangle preset %v", s.Geometry, tc.rect)
			}
			if tc.warn != "" && !strings.Contains(fmt.Sprint(warnings), tc.warn) {
				t.Errorf("warnings %v, want one on %q", warnings, tc.warn)
			}
		})
	}
}

// TestEffects_InnerShadow checks the inner shadow read from an effect
// list: its blur, its distance and direction as offsets, its colour and
// alpha; none without a colour.
func TestEffects_InnerShadow(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, spPr string
		want       *slides.Shadow
	}{
		{
			"inner shadow", `<a:effectLst><a:innerShdw blurRad="63500" dist="127000" dir="13500000"><a:srgbClr val="000000"><a:alpha val="50000"/></a:srgbClr></a:innerShdw></a:effectLst>`,
			&slides.Shadow{Color: css.Color{A: 1}, Opacity: 0.5, OffsetX: -10 * math.Sqrt2 / 2, OffsetY: -10 * math.Sqrt2 / 2, Blur: 5, ScaleX: 1, ScaleY: 1},
		},
		{"no colour", `<a:effectLst><a:innerShdw blurRad="63500"/></a:effectLst>`, &slides.Shadow{ScaleX: 1, ScaleY: 1, Blur: 5}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := readDeck(t, testDeck{slides: []testSlide{{tree: sp("S", `<a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:solidFill><a:srgbClr val="FF0000"/></a:solidFill>`+tc.spPr, "")}}})
			s := shapeNamed(doc.Slides[0].Shapes, "S")
			if s == nil || s.InnerShadow == nil {
				t.Fatal("no inner shadow on shape S")
			}
			got, want := *s.InnerShadow, *tc.want
			if !colorNear(got.Color, want.Color) || !nearly(got.Opacity, want.Opacity) || !nearly(got.OffsetX, want.OffsetX) || !nearly(got.OffsetY, want.OffsetY) || !nearly(got.Blur, want.Blur) {
				t.Errorf("inner shadow %+v, want %+v", got, want)
			}
		})
	}
}

// TestEffects_ContentEffects checks the reflection, blur and fill overlay
// read from an effect list, the shape being 10 by 20 pt.
func TestEffects_ContentEffects(t *testing.T) {
	t.Parallel()
	lst := `<a:effectLst><a:blur rad="127000" grow="0"/><a:fillOverlay blend="mult"><a:solidFill><a:srgbClr val="FF0000"><a:alpha val="50000"/></a:srgbClr></a:solidFill></a:fillOverlay>` +
		`<a:reflection blurRad="12700" stA="50000" endPos="90000" dist="50800" dir="5400000" sy="-100000" algn="bl" rotWithShape="0"/></a:effectLst>`
	doc, warnings := readDeck(t, testDeck{slides: []testSlide{{tree: sp("S", `<a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:solidFill><a:srgbClr val="0000FF"/></a:solidFill>`+lst, "")}}})
	s := shapeNamed(doc.Slides[0].Shapes, "S")
	if s == nil {
		t.Fatal("no shape S")
	}
	if s.Blur == nil || !nearly(s.Blur.Radius, 10) || s.Blur.Grow {
		t.Errorf("blur %+v, want 10 pt without growing", s.Blur)
	}
	if fo := s.FillOverlay; fo == nil || fo.Mode != slides.OverlayMultiply || !colorNear(fo.Color, css.Color{R: 1, A: 1}) || !nearly(fo.Opacity, 0.5) {
		t.Errorf("fill overlay %+v, want red at 50%% multiplied", fo)
	}
	rf := s.Reflection
	if rf == nil {
		t.Fatal("no reflection")
	}
	// Mirrored about the bottom of a 20 pt box, then 4 pt down.
	if !nearly(rf.ScaleY, -1) || !nearly(rf.ScaleX, 1) || !nearly(rf.OffsetY, 24) || !nearly(rf.OffsetX, 0) || !nearly(rf.Blur, 1) {
		t.Errorf("reflection geometry %+v, want mirrored 24 pt down, blurred 1 pt", *rf)
	}
	if !nearly(rf.StartOpacity, 0.5) || !nearly(rf.EndOpacity, 0) || !nearly(rf.StartPos, 0) || !nearly(rf.EndPos, 0.9) || !nearly(rf.FadeDir, 90) {
		t.Errorf("reflection fade %+v, want 50%% at 0 to 0%% at 90%% toward 90 degrees", *rf)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings %v, want none", warnings)
	}
}
