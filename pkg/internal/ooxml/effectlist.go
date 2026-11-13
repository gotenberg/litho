package ooxml

import (
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// EffectList is an effect list read for drawing: the effects the
// converters draw, and the names of the members they do not, for their
// warnings.
type EffectList struct {
	Effects css.OfficeEffects
	// Undrawn names each member left out, in document order, at most once:
	// "inner shadow", "preset shadow", "blur", "fill overlay", "effect
	// container".
	Undrawn []string
}

// Empty reports whether the list draws nothing and leaves nothing out.
func (l *EffectList) Empty() bool {
	return l.Effects == (css.OfficeEffects{}) && len(l.Undrawn) == 0
}

// ParseEffectList reads the effects of a shape or a picture: the effect
// list of its properties spPr (ECMA-376 Part 1, 20.1.8.26 effectLst; an
// empty one means none), else the theme's effect style that its style's
// effectRef names (20.1.4.2.7, idx 1 or more into 20.1.4.1.12
// effectStyleLst), phClr standing for the reference's colour. An effect
// container (20.1.8.25 effectDag) is left out whole.
func ParseEffectList(spPr, style *Element, theme *Theme) EffectList {
	var l EffectList
	resolve := themeResolver(theme, nil)
	lst := spPr.Child(NSA, "effectLst")
	dag := spPr.Child(NSA, "effectDag")
	if lst == nil && dag == nil {
		ref := style.Child(NSA, "effectRef")
		idx, ok := Int(attrOr(ref, "idx"))
		if !ok || theme == nil || idx < 1 || idx > len(theme.EffectStyles) {
			return l
		}
		st := theme.EffectStyles[idx-1]
		lst, dag = st.Child(NSA, "effectLst"), st.Child(NSA, "effectDag")
		if c, ok := DrawingColor(ref, resolve); ok {
			resolve = themeResolver(theme, &c)
		}
	}
	if dag != nil {
		if len(dag.Kids()) > 0 {
			l.Undrawn = append(l.Undrawn, "effect container")
		}
		return l
	}
	for _, e := range lst.Kids() {
		if e.Space != NSA {
			continue
		}
		l.read(e, e.Local, func(e *Element) (css.Color, bool) { return DrawingColor(e, resolve) }, e.Attr)
	}
	return l
}

// ParseTextEffects reads the effects of a WordprocessingML run's
// properties rPr: w14:glow, w14:shadow and w14:reflection (MS-DOCX,
// 2.6.1.4, 2.6.1.27, 2.6.1.24), their attributes and colours those of
// the DrawingML effects they mirror. A w14 colour's alpha is the
// colour's transparency, the complement of a DrawingML alpha.
//
// # Corpus evidence
//
//   - litho/docx run-w14-effects: Word prints a glow and a shadow of
//     w14:alpha 60000 at a peak opacity of 0.4 (soft mask 102 of 255).
//   - libreoffice/docx ooxmlexport/TextEffects_Glow_Shadow_Reflection: a
//     red shadow of w14:alpha 10000 blurred 15 points peaks at 98 of 255
//     over 11 point glyphs, which an opacity of 0.1 cannot reach.
func ParseTextEffects(rPr *Element, theme *Theme) EffectList {
	var l EffectList
	resolve := themeResolver(theme, nil)
	color := func(e *Element) (css.Color, bool) {
		for _, c := range e.Kids() {
			if c.Space != NSW14 {
				continue
			}
			if col, ok := DrawingColorChoice(w14Color(c), resolve); ok {
				return col, true
			}
		}
		return css.Color{}, false
	}
	for _, e := range rPr.Kids() {
		if e.Space != NSW14 {
			continue
		}
		switch e.Local {
		case "glow", "shadow", "reflection":
			local := e.Local
			if local == "shadow" {
				local = "outerShdw"
			}
			l.read(e, local, color, func(name string) (string, bool) { return e.AttrNS(NSW14, name) })
		}
	}
	return l
}

// w14Color returns a w14 colour choice as the DrawingML one it mirrors,
// its alpha turned from a transparency into an opacity.
func w14Color(c *Element) *Element {
	out := &Element{Space: NSA, Local: c.Local}
	for _, a := range c.Attrs {
		a.Name.Space = ""
		out.Attrs = append(out.Attrs, a)
	}
	for _, m := range c.Kids() {
		if m.Space != NSW14 {
			continue
		}
		k := &Element{Space: NSA, Local: m.Local}
		for _, a := range m.Attrs {
			a.Name.Space = ""
			if m.Local == "alpha" && a.Name.Local == "val" {
				if v, ok := Int(a.Value); ok {
					a.Value = strconv.Itoa(100000 - min(max(v, 0), 100000))
				}
			}
			k.Attrs = append(k.Attrs, a)
		}
		out.Children = append(out.Children, k)
	}
	return out
}

// themeResolver resolves a scheme colour name in theme, phClr to ph.
func themeResolver(theme *Theme, ph *css.Color) SchemeResolver {
	return func(name string) (css.Color, bool) {
		if name == "phClr" {
			if ph == nil {
				return css.Color{}, false
			}
			return *ph, true
		}
		return theme.Color(name)
	}
}

// read reads the member e of local name local into l, colours by color
// and attributes by attr.
func (l *EffectList) read(e *Element, local string, color func(*Element) (css.Color, bool), attr func(string) (string, bool)) {
	emu := func(name string) float64 {
		v, _ := attr(name)
		f, ok := EMU(v)
		if !ok {
			return 0
		}
		return f
	}
	num := func(name string, def float64) float64 {
		v, ok := attr(name)
		if !ok {
			return def
		}
		n, ok := Int(v)
		if !ok {
			return def
		}
		return float64(n)
	}
	// ST_PositiveFixedPercentage and ST_Percentage in 1,000ths of a
	// percent, ST_PositiveFixedAngle and ST_FixedAngle in 60,000ths of a
	// degree (20.1.10.40, 20.1.10.45, 20.1.10.4, 20.1.10.23).
	pct := func(name string, def float64) float64 { return num(name, def*100000) / 100000 }
	deg := func(name string) float64 { return num(name, 0) / 60000 }
	align := func(def string) css.RectAlign {
		v, ok := attr("algn")
		if !ok {
			v = def
		}
		return rectAlign(v)
	}
	switch local {
	case "outerShdw":
		c, ok := color(e)
		if !ok {
			return
		}
		l.Effects.Shadow = &css.OfficeShadow{
			Color: c, Blur: emu("blurRad"), Dist: emu("dist"), Dir: deg("dir"),
			SX: pct("sx", 1), SY: pct("sy", 1), KX: deg("kx"), KY: deg("ky"),
			// 20.1.8.45 gives algn the default b.
			Align: align("b"),
		}
	case "glow":
		c, ok := color(e)
		if r := emu("rad"); ok && r > 0 {
			l.Effects.Glow = &css.OfficeGlow{Color: c, Radius: r}
		}
	case "reflection":
		l.Effects.Reflection = &css.OfficeReflection{
			Blur: emu("blurRad"), StartAlpha: pct("stA", 1), StartPos: pct("stPos", 0),
			EndAlpha: pct("endA", 0), EndPos: pct("endPos", 1),
			Dist: emu("dist"), Dir: deg("dir"), FadeDir: num("fadeDir", 5400000) / 60000,
			SX: pct("sx", 1), SY: pct("sy", 1), KX: deg("kx"), KY: deg("ky"),
			// 20.1.8.50 gives algn the default b.
			Align: align("b"),
		}
	case "softEdge":
		l.Effects.SoftEdge = emu("rad")
	case "innerShdw":
		l.undrawn("inner shadow")
	case "prstShdw":
		l.undrawn("preset shadow")
	case "blur":
		l.undrawn("blur")
	case "fillOverlay":
		l.undrawn("fill overlay")
	}
}

// undrawn records the member name once.
func (l *EffectList) undrawn(name string) {
	for _, n := range l.Undrawn {
		if n == name {
			return
		}
	}
	l.Undrawn = append(l.Undrawn, name)
}

// rectAlign returns the point an ST_RectAlignment names (20.1.10.53), the
// centre for an unknown value.
func rectAlign(v string) css.RectAlign {
	a := css.RectAlign{X: 0.5, Y: 0.5}
	switch v {
	case "tl", "l", "bl":
		a.X = 0
	case "tr", "r", "br":
		a.X = 1
	}
	switch v {
	case "tl", "t", "tr":
		a.Y = 0
	case "bl", "b", "br":
		a.Y = 1
	}
	return a
}
