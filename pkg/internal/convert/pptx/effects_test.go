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
		{"preset shadow", `<a:effectLst><a:prstShdw prst="shdw1" dist="127000"><a:prstClr val="black"/></a:prstShdw></a:effectLst>`, "", &slides.Shadow{Color: css.Color{A: 1}, Opacity: 1, OffsetX: 10, ScaleX: 1, ScaleY: 1}, nil, 0, false, ""},
		{"a name no preset carries", `<a:effectLst><a:prstShdw prst="shdw21"><a:prstClr val="black"/></a:prstShdw></a:effectLst>`, "", nil, nil, 0, false, "preset shadow shdw21"},
		// The skew keeps the bottom fixed: the centre, 10 pt above it,
		// moves right by 10 tan 13.34 degrees.
		{"skewed shadow", `<a:effectLst><a:outerShdw kx="-800400"><a:prstClr val="black"/></a:outerShdw></a:effectLst>`, "", &slides.Shadow{Color: css.Color{A: 1}, Opacity: 1, OffsetX: 10 * math.Tan(13.34*math.Pi/180), ScaleX: 1, ScaleY: 1, SkewX: -13.34}, nil, 0, false, ""},
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
					!nearly(got.ScaleX, want.ScaleX) || !nearly(got.ScaleY, want.ScaleY) || !nearly(got.Blur, want.Blur) || !nearly(got.SkewX, want.SkewX) || !nearly(got.SkewY, want.SkewY) {
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
	if fo := s.FillOverlay; fo == nil || fo.Mode != css.OfficeMultiply || !colorNear(fo.Color, css.Color{R: 1, A: 1}) || !nearly(fo.Opacity, 0.5) {
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

// TestEffects_TextEffects checks the effects of a run read into its
// style: the shadow, the glow and the reflection, with the points their
// scales keep fixed; an empty list over the list style's is none; an
// inner shadow and an effect container warn.
func TestEffects_TextEffects(t *testing.T) {
	t.Parallel()
	run := func(rPr string) string {
		return textBox("TB", 0, 0, 12700, 12700, `<a:bodyPr/><a:lstStyle><a:lvl1pPr><a:defRPr><a:effectLst><a:glow rad="25400"><a:srgbClr val="00FF00"/></a:glow></a:effectLst></a:defRPr></a:lvl1pPr></a:lstStyle><a:p><a:r><a:rPr>`+rPr+`</a:rPr><a:t>x</a:t></a:r></a:p>`)
	}
	for _, tc := range []struct {
		name, rPr string
		check     func(t *testing.T, fx *css.OfficeEffects)
		warn      string
	}{
		{"shadow, glow and reflection", `<a:effectLst><a:outerShdw blurRad="38100" dist="38100" dir="2700000" algn="tl"><a:srgbClr val="000000"><a:alpha val="60000"/></a:srgbClr></a:outerShdw>` +
			`<a:glow rad="101600"><a:srgbClr val="FFC000"><a:alpha val="60000"/></a:srgbClr></a:glow><a:reflection blurRad="6350" stA="55000" endPos="45500" dir="5400000" sy="-100000" algn="bl"/></a:effectLst>`,
			func(t *testing.T, fx *css.OfficeEffects) {
				if fx == nil || fx.Shadow == nil || fx.Glow == nil || fx.Reflection == nil {
					t.Fatalf("effects %+v, want a shadow, a glow and a reflection", fx)
				}
				if fx.Rules != css.OfficePowerPoint {
					t.Errorf("rules %v, want PowerPoint's", fx.Rules)
				}
				if sh := fx.Shadow; !nearly(sh.Blur, 3) || !nearly(sh.Dist, 3) || sh.Dir != 45 || !nearly(sh.Color.A, 0.6) || sh.Align != (css.RectAlign{}) {
					t.Errorf("shadow %+v", *sh)
				}
				if g := fx.Glow; !nearly(g.Radius, 8) || !nearly(g.Color.A, 0.6) {
					t.Errorf("glow %+v", *g)
				}
				if r := fx.Reflection; r.SY != -1 || r.Dist != 0 || !nearly(r.StartAlpha, 0.55) || r.Align != (css.RectAlign{Y: 1}) {
					t.Errorf("reflection %+v", *r)
				}
			}, ""},
		{"the list style's", "", func(t *testing.T, fx *css.OfficeEffects) {
			if fx == nil || fx.Glow == nil || !nearly(fx.Glow.Radius, 2) {
				t.Errorf("effects %+v, want the list style's glow", fx)
			}
		}, ""},
		{"an empty list is none", `<a:effectLst/>`, func(t *testing.T, fx *css.OfficeEffects) {
			if fx != nil {
				t.Errorf("effects %+v, want none", fx)
			}
		}, ""},
		{"an inner shadow warns", `<a:effectLst><a:innerShdw blurRad="63500"><a:srgbClr val="000000"/></a:innerShdw></a:effectLst>`, func(t *testing.T, fx *css.OfficeEffects) {
			if fx != nil {
				t.Errorf("effects %+v, want none", fx)
			}
		}, "innerShdw effect of text"},
		{"an effect container warns", `<a:effectDag><a:cont><a:glow rad="25400"><a:srgbClr val="FF0000"/></a:glow></a:cont></a:effectDag>`, func(t *testing.T, fx *css.OfficeEffects) {
			if fx != nil {
				t.Errorf("effects %+v, want none", fx)
			}
		}, "effect containers"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, warnings := readDeck(t, testDeck{slides: []testSlide{{tree: run(tc.rPr)}}})
			shapes := doc.Slides[0].Shapes
			if len(shapes) != 1 || shapes[0].Text == nil || len(shapes[0].Text.Paragraphs[0].Runs) == 0 {
				t.Fatalf("shapes %+v", shapes)
			}
			tc.check(t, shapes[0].Text.Paragraphs[0].Runs[0].Style.Effects)
			if tc.warn != "" && !strings.Contains(fmt.Sprint(warnings), tc.warn) {
				t.Errorf("warnings %v, want one on %q", warnings, tc.warn)
			}
		})
	}
}

// TestEffects_PresetShadowCopies checks the far copy of a preset shadow:
// twice the distance for shdw13, the distance the other way for shdw18,
// in the colour lightened four fifths of the way to white, and none for
// shdw1.
func TestEffects_PresetShadowCopies(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		prst string
		far  float64
	}{{"shdw1", 0}, {"shdw13", 20}, {"shdw18", -10}} {
		t.Run(tc.prst, func(t *testing.T) {
			t.Parallel()
			spPr := `<a:prstGeom prst="rect"><a:avLst/></a:prstGeom><a:effectLst><a:prstShdw prst="` + tc.prst + `" dist="127000"><a:srgbClr val="808080"/></a:prstShdw></a:effectLst>`
			doc, _ := readDeck(t, testDeck{slides: []testSlide{{tree: sp("S", spPr, "")}}})
			s := shapeNamed(doc.Slides[0].Shapes, "S")
			if s == nil || s.Shadow == nil || !nearly(s.Shadow.OffsetX, 10) {
				t.Fatalf("shape %+v, want a shadow 10 pt right", s)
			}
			switch {
			case tc.far == 0 && s.ShadowUnder != nil:
				t.Errorf("far copy %+v, want none", *s.ShadowUnder)
			case tc.far != 0 && (s.ShadowUnder == nil || !nearly(s.ShadowUnder.OffsetX, tc.far) || !nearly(s.ShadowUnder.Color.R, 0x80/255.0+(1-0x80/255.0)*0.8)):
				t.Errorf("far copy %+v, want %v pt right, lightened", s.ShadowUnder, tc.far)
			}
		})
	}
}

// TestEffects_PresetShadowGeometry checks the near copy of preset shadows
// on a shape 10 by 20 pt: the scale and the skew of the preset folded
// into the offsets about the box, grown by half the line when there is
// one, and the text left out of all but shdw1, shdw2, shdw5 and shdw6.
func TestEffects_PresetShadowGeometry(t *testing.T) {
	t.Parallel()
	lean := 10 * math.Sqrt(3) / 2
	for _, tc := range []struct {
		name, prst, line string
		want             slides.Shadow
	}{
		{"offset", "shdw2", "", slides.Shadow{OffsetX: 10, ScaleX: 1, ScaleY: 1}},
		{"half height, leaning left", "shdw3", "", slides.Shadow{OffsetX: 10 - lean, OffsetY: 5, ScaleX: 1, ScaleY: 0.5, SkewX: 40.893394649130904, ShapeOnly: true}},
		{"mirrored below", "shdw20", "", slides.Shadow{OffsetX: 10, OffsetY: 20, ScaleX: 1, ScaleY: -1, ShapeOnly: true}},
		{"mirrored below the line", "shdw20", `<a:ln w="25400"><a:solidFill><a:srgbClr val="000000"/></a:solidFill></a:ln>`, slides.Shadow{OffsetX: 10, OffsetY: 22, ScaleX: 1, ScaleY: -1, ShapeOnly: true}},
		{"three quarters about the top left", "shdw9", "", slides.Shadow{OffsetX: 10 - 1.25, OffsetY: -2.5, ScaleX: 0.75, ScaleY: 0.75, ShapeOnly: true}},
		{"five quarters about the bottom right", "shdw10", "", slides.Shadow{OffsetX: 10 - 1.25, OffsetY: -2.5, ScaleX: 1.25, ScaleY: 1.25, ShapeOnly: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spPr := `<a:prstGeom prst="rect"><a:avLst/></a:prstGeom>` + tc.line + `<a:effectLst><a:prstShdw prst="` + tc.prst + `" dist="127000"><a:srgbClr val="808080"/></a:prstShdw></a:effectLst>`
			doc, _ := readDeck(t, testDeck{slides: []testSlide{{tree: sp("S", spPr, "")}}})
			s := shapeNamed(doc.Slides[0].Shapes, "S")
			if s == nil || s.Shadow == nil {
				t.Fatalf("shape %+v, want a shadow", s)
			}
			got, want := *s.Shadow, tc.want
			if !nearly(got.OffsetX, want.OffsetX) || !nearly(got.OffsetY, want.OffsetY) || !nearly(got.ScaleX, want.ScaleX) || !nearly(got.ScaleY, want.ScaleY) ||
				!nearly(got.SkewX, want.SkewX) || got.ShapeOnly != want.ShapeOnly || got.Blur != 0 || got.RotateWithShape {
				t.Errorf("shadow %+v, want %+v", got, want)
			}
		})
	}
}
