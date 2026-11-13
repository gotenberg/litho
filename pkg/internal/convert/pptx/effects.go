package pptx

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/shape"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
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
// The outer shadow (20.1.8.45 outerShdw) fills [slides.Shape.Shadow]: its
// colour and alpha, its blur radius, its distance and direction resolved
// into offsets, its scale with the alignment it scales about resolved
// into the offsets too, as the builder scales about the shape's centre.
// A shadow without a colour child is white, as Impress prints it. The
// glow (20.1.8.32), none without a colour, and the soft edge (20.1.8.53)
// fill [slides.Shape.Glow] and [slides.Shape.SoftEdge]; a shape of no
// geometry with either takes the rectangle preset, whose effects the
// builder applies as Impress applies a rectangle's. The inner shadow
// (20.1.8.40 innerShdw), none without a colour, fills
// [slides.Shape.InnerShadow], the reflection (20.1.8.50) fills
// [slides.Shape.Reflection], the blur (20.1.8.15) [slides.Shape.Blur] and
// the fill overlay of a solid fill (20.1.8.29) [slides.Shape.FillOverlay].
// Preset shadows (20.1.8.52) and fill overlays of other fills print
// nothing, with a warning; so does an effect container (20.1.8.25 effectDag). A
// shadow's skew (kx, ky) is left out, with a warning.
//
// # Corpus evidence
//
//   - probe, pptx: a rectangle referencing effect style 3 of its theme, whose
//     effect list holds a shadow blurred 4.5 pt, 1.5 pt down at 63%,
//     prints that shadow.
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
				r.warn(warn.ContentUnsupported, warn.Approximated, "effect containers (effectDag) are not drawn")
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
			r.warn(warn.ContentUnsupported, warn.Approximated, "effect containers (effectDag) are not drawn")
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
			if attr(e, "kx") != "" || attr(e, "ky") != "" {
				r.warn(warn.ContentUnsupported, warn.Approximated, "the skew of shadows is not drawn")
			}
			s.Shadow.Color, s.Shadow.Opacity = color(e)
			if len(e.Kids()) == 0 {
				// Impress prints a shadow without a colour white.
				s.Shadow.Color, s.Shadow.Opacity = css.Color{R: 1, G: 1, B: 1, A: 1}, 1
			}
		case "glow":
			if rad := emuAttr(e, "rad"); numeric.PositiveFinite(rad) {
				// A glow without a colour prints nothing.
				if c, a := color(e); a > 0 {
					s.Glow = &slides.Glow{Color: c, Opacity: a, Radius: min(rad, maxEffectRadius)}
				}
			}
		case "softEdge":
			if rad := emuAttr(e, "rad"); numeric.PositiveFinite(rad) && !beveled(chain) {
				s.SoftEdge = min(rad, maxEffectRadius)
			}
		case "innerShdw":
			s.InnerShadow = innerShadow(e)
			s.InnerShadow.Color, s.InnerShadow.Opacity = color(e)
		case "prstShdw":
			r.warn(warn.ContentUnsupported, warn.Approximated, "preset shadows are not drawn")
		case "reflection":
			s.Reflection = reflection(e, s.Box)
		case "blur":
			if rad := emuAttr(e, "rad"); numeric.PositiveFinite(rad) {
				s.Blur = &slides.Blur{Radius: min(rad, maxEffectRadius), Grow: ooxml.OnOff(attr(e, "grow"), true)}
			}
		case "fillOverlay":
			if fo := r.fillOverlay(e, color); fo != nil {
				s.FillOverlay = fo
			}
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

// outerShadow reads the geometry of an outer shadow (20.1.8.45
// outerShdw) of a shape of box: blurRad, dist and dir (60,000ths of a
// degree, clockwise from the x axis, y down), sx and sy (1,000ths of a
// percent, 100,000 for none), and the alignment algn (20.1.10.3
// ST_RectAlignment, "b" by default) the scale keeps fixed, folded into
// the offsets as a scale about box's centre, and rotWithShape, true by
// default (the schema's CT_OuterShadowEffect), whether the offsets turn
// with the shape.
func outerShadow(e *ooxml.Element, box typeset.Rect) *slides.Shadow {
	sh := &slides.Shadow{ScaleX: 1, ScaleY: 1, RotateWithShape: true}
	if v, ok := e.Attr("rotWithShape"); ok {
		sh.RotateWithShape = ooxml.OnOff(v, true)
	}
	if v := emuAttr(e, "blurRad"); numeric.PositiveFinite(v) {
		sh.Blur = min(v, maxEffectRadius)
	}
	dist := emuAttr(e, "dist")
	if !numeric.PositiveFinite(dist) {
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
	ax, ay := alignment(e)
	// Scaling about the alignment point P is scaling about the centre C
	// and moving by (P - C)(1 - s).
	sh.OffsetX += (ax - 0.5) * box.Width * (1 - sh.ScaleX)
	sh.OffsetY += (ay - 0.5) * box.Height * (1 - sh.ScaleY)
	return sh
}

// alignment returns the point of a box an effect's scale keeps fixed,
// its algn (20.1.10.3 ST_RectAlignment, "b" by default) as fractions of
// the box from the left and the top.
func alignment(e *ooxml.Element) (ax, ay float64) {
	ax, ay = 0.5, 1.0
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
	return ax, ay
}

// textEffects reads the effects of a run (21.1.2.3.9 rPr holding
// 20.1.8.26 effectLst), sc resolving the colours: the outer shadow, the
// glow and the reflection, as [reader.effectList] reads a shape's, their
// scales left about the glyphs' bounds, which the builder knows. The
// other effects of a run, and an effect container (20.1.8.25 effectDag),
// print nothing, with a warning. It returns nil for a list of none.
func (r *reader) textEffects(lst *ooxml.Element, sc scope) *slides.TextEffects {
	if lst.Is(ooxml.NSA, "effectDag") {
		if len(lst.Kids()) > 0 {
			r.warn(warn.ContentUnsupported, warn.Approximated, "effect containers (effectDag) on text are not drawn")
		}
		return nil
	}
	fx := &slides.TextEffects{}
	color := func(e *ooxml.Element) (css.Color, float64) {
		c, ok := sc.color(e, nil)
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
			sh := outerShadow(e, typeset.Rect{})
			sh.Color, sh.Opacity = color(e)
			if sh.Opacity > 0 {
				fx.Shadow = sh
				fx.ShadowAlignX, fx.ShadowAlignY = alignment(e)
			}
			if attr(e, "kx") != "" || attr(e, "ky") != "" {
				r.warn(warn.ContentUnsupported, warn.Approximated, "the skew of shadows is not drawn")
			}
		case "glow":
			if rad := emuAttr(e, "rad"); numeric.PositiveFinite(rad) {
				if c, a := color(e); a > 0 {
					fx.Glow = &slides.Glow{Color: c, Opacity: a, Radius: min(rad, maxEffectRadius)}
				}
			}
		case "reflection":
			fx.Reflection = reflection(e, typeset.Rect{})
			fx.ReflectionAlignX, fx.ReflectionAlignY = alignment(e)
		default:
			r.warn(warn.ContentUnsupported, warn.Approximated, "the "+e.Local+" effect of text is not drawn")
		}
	}
	if fx.Shadow == nil && fx.Glow == nil && fx.Reflection == nil {
		return nil
	}
	return fx
}

// innerShadow reads the geometry of an inner shadow (20.1.8.40
// innerShdw): blurRad, and dist and dir (60,000ths of a degree, clockwise
// from the x axis, y down) resolved into offsets, the shadow then falling
// toward the edges on the side dir points to. Its colour is read apart.
func innerShadow(e *ooxml.Element) *slides.Shadow {
	sh := &slides.Shadow{ScaleX: 1, ScaleY: 1}
	if v := emuAttr(e, "blurRad"); numeric.PositiveFinite(v) {
		sh.Blur = min(v, maxEffectRadius)
	}
	dist := emuAttr(e, "dist")
	if !numeric.PositiveFinite(dist) {
		dist = 0
	}
	dist = min(dist, maxEffectRadius)
	dir, _ := intAttr(e, "dir")
	s, c := math.Sincos(float64(dir) / 60000 * math.Pi / 180)
	sh.OffsetX, sh.OffsetY = dist*c, dist*s
	return sh
}

// reflection reads a reflection (20.1.8.50 reflection) of a shape of box:
// its geometry as [outerShadow] reads a shadow's, with the schema's
// defaults (CT_ReflectionEffect): no blur, start alpha 100% at 0, end
// alpha 0% at 100%, faded toward 90 degrees, unscaled, aligned on the
// bottom. The alphas and positions are 1,000ths of a percent.
func reflection(e *ooxml.Element, box typeset.Rect) *slides.Reflection {
	sh := outerShadow(e, box)
	pct := func(name string, def float64) float64 {
		v, ok := intAttr(e, name)
		if !ok {
			return def
		}
		return min(max(float64(v)/100000, 0), 1)
	}
	fade := 90.0
	if v, ok := intAttr(e, "fadeDir"); ok {
		fade = float64(v) / 60000
	}
	return &slides.Reflection{
		ScaleX: sh.ScaleX, ScaleY: sh.ScaleY, OffsetX: sh.OffsetX, OffsetY: sh.OffsetY, Blur: sh.Blur,
		StartOpacity: pct("stA", 1), StartPos: pct("stPos", 0),
		EndOpacity: pct("endA", 0), EndPos: pct("endPos", 1),
		FadeDir: fade,
	}
}

// fillOverlay reads a fill overlay (20.1.8.29 fillOverlay): its blend
// mode (20.1.10.11 ST_BlendMode, required) and its fill, of which a solid
// fill is drawn; another fill warns and gives nil. color resolves the
// colour of the solid fill.
func (r *reader) fillOverlay(e *ooxml.Element, color func(*ooxml.Element) (css.Color, float64)) *slides.FillOverlay {
	solid := e.Child(ooxml.NSA, "solidFill")
	if solid == nil {
		r.warn(warn.ContentUnsupported, warn.Approximated, "fill overlays of a fill other than a solid colour are not drawn")
		return nil
	}
	c, a := color(solid)
	if a <= 0 {
		return nil
	}
	fo := &slides.FillOverlay{Color: c, Opacity: a}
	switch attr(e, "blend") {
	case "mult":
		fo.Mode = slides.OverlayMultiply
	case "screen":
		fo.Mode = slides.OverlayScreen
	case "darken":
		fo.Mode = slides.OverlayDarken
	case "lighten":
		fo.Mode = slides.OverlayLighten
	}
	return fo
}

// beveled reports whether the first shape properties of chain that hold a
// 3-D shape (20.1.5.12 sp3d) give it a top or bottom bevel (20.1.5.3
// bevelT, bevelB): PowerPoint prints such a shape without its soft edge.
//
// # Corpus evidence
//
//   - libreoffice/pptx: oox/tdf170095's picture with a soft edge of 25 pt
//     and a top bevel prints opaque to its edge.
//   - openxmlsdk/pptx: AutoShapes_3D_Bevel_Circle_Soft_Edges_5_Point_'s
//     shapes with a soft edge of 5 pt and a top bevel print opaque to
//     their edges.
func beveled(chain []*ooxml.Element) bool {
	for _, el := range chain {
		pr := spPr(el)
		if pr == nil {
			continue
		}
		if sp := pr.Child(ooxml.NSA, "sp3d"); sp != nil {
			return sp.Child(ooxml.NSA, "bevelT") != nil || sp.Child(ooxml.NSA, "bevelB") != nil
		}
	}
	return false
}
