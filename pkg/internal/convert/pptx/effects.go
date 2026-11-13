package pptx

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/detmath"
	"github.com/gotenberg/litho/pkg/internal/numeric"
	"github.com/gotenberg/litho/pkg/internal/officefx"
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
// A preset shadow (20.1.8.49 prstShdw) fills [slides.Shape.Shadow] and
// [slides.Shape.ShadowUnder] as [presetShadow] reads it. Fill overlays of
// other fills print nothing, with a warning; so does an effect container
// (20.1.8.25 effectDag). A
// shadow's skew (kx, ky, 60,000ths of a degree) fills
// [slides.Shadow.SkewX] and [slides.Shadow.SkewY], about the same
// alignment as its scale.
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
			near, far, ok := presetShadow(e, s, color)
			if !ok {
				r.warn(warn.ContentUnsupported, warn.Approximated, "the preset shadow "+attr(e, "prst")+" is not drawn")
				break
			}
			s.Shadow, s.ShadowUnder = near, far
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
	s, c := detmath.Sincos(float64(dir) / 60000 * math.Pi / 180)
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
	skew := func(name string) float64 {
		v, _ := intAttr(e, name)
		return float64(v) / 60000
	}
	sh.SkewX, sh.SkewY = skew("kx"), skew("ky")
	ax, ay := alignment(e)
	aligned(sh, ax, ay, box)
	return sh
}

// aligned folds into sh's offsets the point of box its scale and skew
// keep fixed, at fractions ax and ay of box from the left and the top, as
// the builder scales and skews about box's centre.
func aligned(sh *slides.Shadow, ax, ay float64, box typeset.Rect) {
	// Scaling and skewing by A about the alignment point P is doing so
	// about the centre C and moving by (I - A)(P - C).
	dx, dy := float64((ax-0.5)*box.Width), float64((ay-0.5)*box.Height)
	kx, ky := detmath.Tan(sh.SkewX*math.Pi/180), detmath.Tan(sh.SkewY*math.Pi/180)
	if !numeric.Finite(kx) || !numeric.Finite(ky) {
		kx, ky = 0, 0
	}
	sh.OffsetX += dx - float64(sh.ScaleX*(dx+float64(kx*dy)))
	sh.OffsetY += dy - float64(sh.ScaleY*(float64(ky*dx)+dy))
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
// scales and skews about the point of the glyphs' ink their alignment
// names, drawn by PowerPoint's rules. The other effects of a run, and an
// effect container (20.1.8.25 effectDag), print nothing, with a warning.
// It returns nil for a list of none.
func (r *reader) textEffects(lst *ooxml.Element, sc scope) *css.OfficeEffects {
	if lst.Is(ooxml.NSA, "effectDag") {
		if len(lst.Kids()) > 0 {
			r.warn(warn.ContentUnsupported, warn.Approximated, "effect containers (effectDag) on text are not drawn")
		}
		return nil
	}
	fx := &css.OfficeEffects{Rules: css.OfficePowerPoint}
	color := func(e *ooxml.Element) css.Color {
		c, ok := sc.color(e, nil)
		if !ok {
			return css.Color{}
		}
		return c
	}
	for _, e := range lst.Kids() {
		if e.Space != ooxml.NSA {
			continue
		}
		switch e.Local {
		case "outerShdw":
			if c := color(e); c.A > 0 {
				g := effectGeometry(e)
				fx.Shadow = &css.OfficeShadow{Color: c, Blur: g.blur, Dist: g.dist, Dir: g.dir, SX: g.sx, SY: g.sy, KX: g.kx, KY: g.ky, Align: g.align}
			}
		case "glow":
			if rad := emuAttr(e, "rad"); numeric.PositiveFinite(rad) {
				if c := color(e); c.A > 0 {
					fx.Glow = &css.OfficeGlow{Color: c, Radius: min(rad, maxEffectRadius)}
				}
			}
		case "reflection":
			g := effectGeometry(e)
			fx.Reflection = &css.OfficeReflection{
				Blur: g.blur, Dist: g.dist, Dir: g.dir, SX: g.sx, SY: g.sy, KX: g.kx, KY: g.ky, Align: g.align,
				StartAlpha: effectPercent(e, "stA", 1), StartPos: effectPercent(e, "stPos", 0),
				EndAlpha: effectPercent(e, "endA", 0), EndPos: effectPercent(e, "endPos", 1),
				FadeDir: fadeDir(e),
			}
		default:
			r.warn(warn.ContentUnsupported, warn.Approximated, "the "+e.Local+" effect of text is not drawn")
		}
	}
	if fx.Shadow == nil && fx.Glow == nil && fx.Reflection == nil {
		return nil
	}
	return fx
}

// geometry is the geometry of an outer shadow or a reflection as its
// attributes give it: its blur radius and distance in points, its
// direction and skews in degrees, its scales as factors and the point
// its scales and skews keep fixed.
type geometry struct {
	blur, dist, dir float64
	sx, sy, kx, ky  float64
	align           css.RectAlign
}

// effectGeometry reads the geometry of e, an outer shadow or a reflection
// (20.1.8.45 outerShdw, 20.1.8.50 reflection), as [outerShadow] reads it.
func effectGeometry(e *ooxml.Element) geometry {
	var g geometry
	if v := emuAttr(e, "blurRad"); numeric.PositiveFinite(v) {
		g.blur = min(v, maxEffectRadius)
	}
	if v := emuAttr(e, "dist"); numeric.PositiveFinite(v) {
		g.dist = min(v, maxEffectRadius)
	}
	dir, _ := intAttr(e, "dir")
	g.dir = float64(dir) / 60000
	scale := func(name string) float64 {
		v, ok := intAttr(e, name)
		if !ok {
			return 1
		}
		return min(max(float64(v)/100000, -maxShadowScale), maxShadowScale)
	}
	g.sx, g.sy = scale("sx"), scale("sy")
	kx, _ := intAttr(e, "kx")
	ky, _ := intAttr(e, "ky")
	g.kx, g.ky = float64(kx)/60000, float64(ky)/60000
	g.align.X, g.align.Y = alignment(e)
	return g
}

// effectPercent returns e's attribute name, 1,000ths of a percent, as a
// fraction within [0, 1], def when it is missing.
func effectPercent(e *ooxml.Element, name string, def float64) float64 {
	v, ok := intAttr(e, name)
	if !ok {
		return def
	}
	return min(max(float64(v)/100000, 0), 1)
}

// fadeDir returns a reflection's fadeDir in degrees, 90 by default
// (CT_ReflectionEffect).
func fadeDir(e *ooxml.Element) float64 {
	if v, ok := intAttr(e, "fadeDir"); ok {
		return float64(v) / 60000
	}
	return 90
}

// presetShadow reads a preset shadow (20.1.8.49 prstShdw) of the shape
// s, its box and line read, as the copies [officefx.PresetCopies] gives on
// PowerPoint's rules: the near copy, and for shdw13, shdw17 and shdw18
// the far one painted under it. color resolves the colour. It returns
// false for a name that is not a preset, and nils for a shadow of no
// colour. The copies scale and skew about the box grown by half the
// line, and keep the slide's directions whatever the shape's turn.
// PowerPoint clips the copies to what lies outside the shape, which an
// opaque shape hides anyway; the copies here are not clipped.
//
// # Corpus evidence
//
//   - probe, pptx: PowerPoint prints the copies of the twenty presets on
//     rectangles with a 1 point line scaled and skewed about the box
//     grown by half a point: shdw20, mirrored about the bottom, starts 1
//     point below the line's outer edge plus the offset.
func presetShadow(e *ooxml.Element, s *slides.Shape, color func(*ooxml.Element) (css.Color, float64)) (near, far *slides.Shadow, ok bool) {
	c, a := color(e)
	dist := emuAttr(e, "dist")
	if !numeric.PositiveFinite(dist) {
		dist = 0
	}
	dir, _ := intAttr(e, "dir")
	c.A = a
	p := &css.OfficePreset{Name: attr(e, "prst"), Color: c, Dist: min(dist, maxEffectRadius), Dir: float64(dir) / 60000}
	copies, text, ok := officefx.PresetCopies(p, css.OfficePowerPoint)
	if !ok || len(copies) == 0 {
		return nil, nil, ok
	}
	box := s.Box
	if s.Line.Width > 0 {
		box = officefx.Grow(box, float64(s.Line.Width/2))
	}
	shadows := make([]*slides.Shadow, len(copies))
	for i, cp := range copies {
		sin, cos := detmath.Sincos(cp.Dir * math.Pi / 180)
		sh := &slides.Shadow{
			Color: css.Color{R: cp.Color.R, G: cp.Color.G, B: cp.Color.B, A: 1}, Opacity: cp.Color.A,
			OffsetX: cp.Dist * cos, OffsetY: cp.Dist * sin,
			ScaleX: cp.SX, ScaleY: cp.SY, SkewX: cp.KX, ShapeOnly: !text,
		}
		aligned(sh, cp.Align.X, cp.Align.Y, box)
		shadows[i] = sh
	}
	near = shadows[len(shadows)-1]
	if len(shadows) > 1 {
		far = shadows[0]
	}
	return near, far, true
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
	s, c := detmath.Sincos(float64(dir) / 60000 * math.Pi / 180)
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
	return &slides.Reflection{
		ScaleX: sh.ScaleX, ScaleY: sh.ScaleY, OffsetX: sh.OffsetX, OffsetY: sh.OffsetY, Blur: sh.Blur,
		StartOpacity: effectPercent(e, "stA", 1), StartPos: effectPercent(e, "stPos", 0),
		EndOpacity: effectPercent(e, "endA", 0), EndPos: effectPercent(e, "endPos", 1),
		FadeDir: fadeDir(e),
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
		fo.Mode = css.OfficeMultiply
	case "screen":
		fo.Mode = css.OfficeScreen
	case "darken":
		fo.Mode = css.OfficeDarken
	case "lighten":
		fo.Mode = css.OfficeLighten
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
