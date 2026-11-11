package pptx

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/shape"
)

// effectList fills the shadow, glow and soft edge of s from the effect
// list of the first shape of chain whose properties hold one (ECMA-376
// Part 1, 20.1.8.26 effectLst; an empty list means none), else from the
// theme's effect style the shape's style references (20.1.4.2.7
// effectRef, idx 1 or more into 20.1.4.1.12 effectStyleLst, phClr
// standing for the reference's colour), as Impress applies them, fc
// resolving the colours. A table's effect list is its a:tblPr's
// (21.1.3.15), passed as chain with a nil style.
//
// The outer shadow (20.1.8.49 outerShdw) fills [slides.Shape.Shadow]: its
// colour and alpha, its blur radius, its distance and direction resolved
// into offsets, its scale with the alignment it scales about resolved
// into the offsets too, as the builder scales about the shape's centre.
// A shadow without a colour child is white, as Impress prints it. The
// glow (20.1.8.32), none without a colour, and the soft edge (20.1.8.53)
// fill [slides.Shape.Glow] and [slides.Shape.SoftEdge]; a shape of no
// geometry with either takes the rectangle preset, whose effects the
// builder applies as Impress applies a rectangle's. Inner shadows, preset
// shadows, reflections, blurs and fill overlays (20.1.8.40, 20.1.8.52,
// 20.1.8.50, 20.1.8.15, 20.1.8.29) print nothing, with a warning, as
// Impress prints none of them; so does an effect container
// (20.1.8.25 effectDag), and a shadow's skew (kx, ky) is left out.
//
// # Corpus evidence
//
//   - probe, pptx: a rectangle referencing effect style 3 of its theme, whose
//     effect list holds a shadow blurred 4.5 pt, 1.5 pt down at 63%,
//     prints that shadow; one with an inner shadow and one with a
//     reflection print without them.
//   - probe, pptx: a rectangle whose outer shadow has no colour prints a white
//     shadow, opaque; an oval whose glow has none prints no glow.
//   - libreoffice/pptx: sd-data/n819614 and sd/bnc862510_5's diagram shapes print the
//     shadows of their own effect lists.
func (r *reader) effectList(chain []*ooxml.Element, style *ooxml.Element, fc *fillCtx, s *slides.Shape) {
	var lst *ooxml.Element
	ph := fc.ph
	for _, el := range chain {
		if el == nil {
			// A table without a:tblPr (ECMA-376 Part 1, 21.1.3.15 makes it
			// optional) has no effect list of its own.
			continue
		}
		pr := spPr(el)
		if pr == nil && el.Is(ooxml.NSA, "tblPr") {
			pr = el
		}
		if dag := pr.Child(ooxml.NSA, "effectDag"); dag != nil {
			if len(dag.Children) > 0 {
				r.warn("unsupported-content", "effect containers (effectDag) are not drawn")
			}
			return
		}
		if e := pr.Child(ooxml.NSA, "effectLst"); e != nil {
			lst = e
			break
		}
	}
	if lst == nil {
		ref := style.Child(ooxml.NSA, "effectRef")
		idx, ok := intAttr(ref, "idx")
		if !ok || idx < 1 || idx > len(fc.sc.theme.effects) {
			return
		}
		st := fc.sc.theme.effects[idx-1]
		if dag := st.Child(ooxml.NSA, "effectDag"); dag != nil && len(dag.Children) > 0 {
			r.warn("unsupported-content", "effect containers (effectDag) are not drawn")
			return
		}
		lst = st.Child(ooxml.NSA, "effectLst")
		if c, ok := fc.sc.color(ref, nil); ok {
			ph = &c
		}
	}
	if lst == nil {
		return
	}
	defer func() {
		if s.Kind == slides.KindGeometry && s.Geometry == nil && (s.Glow != nil || s.SoftEdge > 0) {
			// Impress draws a rectangle's glow and soft edge as the
			// rectangle preset's, which the builder tells apart from a
			// shape of no geometry.
			s.Geometry = &shape.Geometry{Preset: "rect"}
		}
	}()
	color := func(e *ooxml.Element) (css.Color, float64) {
		c, ok := fc.sc.color(e, ph)
		if !ok {
			return css.Color{}, 0
		}
		a := c.A
		c.A = 1
		return c, a
	}
	for _, e := range lst.Kids() {
		if e.Space != ooxml.NSA {
			continue
		}
		switch e.Local {
		case "outerShdw":
			s.Shadow = outerShadow(e, s.Box)
			s.Shadow.Color, s.Shadow.Opacity = color(e)
			if len(e.Kids()) == 0 {
				// Impress prints a shadow without a colour white.
				s.Shadow.Color, s.Shadow.Opacity = css.Color{R: 1, G: 1, B: 1, A: 1}, 1
			}
		case "glow":
			if rad := emuAttr(e, "rad"); rad > 0 && !math.IsInf(rad, 0) {
				// A glow without a colour prints nothing.
				if c, a := color(e); a > 0 {
					s.Glow = &slides.Glow{Color: c, Opacity: a, Radius: min(rad, maxEffectRadius)}
				}
			}
		case "softEdge":
			if rad := emuAttr(e, "rad"); rad > 0 && !math.IsInf(rad, 0) {
				s.SoftEdge = min(rad, maxEffectRadius)
			}
		case "innerShdw", "prstShdw":
			r.warn("unsupported-content", "inner and preset shadows are not drawn")
		case "reflection":
			r.warn("unsupported-content", "reflections are not drawn")
		case "blur", "fillOverlay":
			r.warn("unsupported-content", "blur and fill overlay effects are not drawn")
		}
	}
}

// maxEffectRadius caps a blur, glow or soft edge radius and a shadow's
// distance, in points. Anti-DoS guard, not spec-mandated: the schema
// allows 2,147,483,647 EMU (169,000 pt); 1000 pt is wider than any
// slide, so a larger value draws the same.
const maxEffectRadius = 1000

// maxShadowScale caps a shadow's scale factor either way. Anti-DoS guard,
// not spec-mandated: a shadow ten times the shape's size covers any
// slide.
const maxShadowScale = 10

// outerShadow reads the geometry of an outer shadow (20.1.8.49
// outerShdw) of a shape of box: blurRad, dist and dir (60,000ths of a
// degree, clockwise from the x axis, y down), sx and sy (1,000ths of a
// percent, 100,000 for none), and the alignment algn (20.1.10.3
// ST_RectAlignment, "b" by default) the scale keeps fixed, folded into
// the offsets as a scale about box's centre. rotWithShape is not read:
// Impress keeps a turned shape's shadow at the slide's offsets.
func outerShadow(e *ooxml.Element, box compose.Rect) *slides.Shadow {
	sh := &slides.Shadow{ScaleX: 1, ScaleY: 1}
	if v := emuAttr(e, "blurRad"); v > 0 && !math.IsInf(v, 0) {
		sh.Blur = min(v, maxEffectRadius)
	}
	dist := emuAttr(e, "dist")
	if !(dist > 0) || math.IsInf(dist, 0) {
		dist = 0
	}
	dist = min(dist, maxEffectRadius)
	dir, _ := intAttr(e, "dir")
	s, c := math.Sincos(float64(dir) / 60000 * math.Pi / 180)
	sh.OffsetX, sh.OffsetY = dist*c, dist*s
	scale := func(name string) float64 {
		v, ok := intAttr(e, name)
		if !ok {
			return 1
		}
		f := float64(v) / 100000
		return min(max(f, -maxShadowScale), maxShadowScale)
	}
	sh.ScaleX, sh.ScaleY = scale("sx"), scale("sy")
	ax, ay := 0.5, 1.0
	algn := attr(e, "algn")
	if algn == "" {
		algn = "b"
	}
	switch algn {
	case "tl", "l", "bl":
		ax = 0
	case "tr", "r", "br":
		ax = 1
	}
	switch algn {
	case "tl", "t", "tr":
		ay = 0
	case "l", "ctr", "r":
		ay = 0.5
	}
	// Scaling about the alignment point P is scaling about the centre C
	// and moving by (P - C)(1 - s).
	sh.OffsetX += (ax - 0.5) * box.Width * (1 - sh.ScaleX)
	sh.OffsetY += (ay - 0.5) * box.Height * (1 - sh.ScaleY)
	return sh
}
