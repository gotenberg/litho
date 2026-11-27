package pptx

import (
	"sort"

	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/shape"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// fillCtx is where a fill or line is resolved: the colour scope, the part
// whose relationships a picture fill names, the enclosing group's fill
// for grpFill, and the colour phClr stands for in a theme style.
type fillCtx struct {
	sc    scope
	part  string
	group *slides.Fill
	ph    *css.Color
}

// withPh returns fc with phClr standing for c, its alpha included: a
// style reference's colour stands for phClr with its transforms (ECMA-376
// Part 1, 20.1.10.54 ST_SchemeColorVal phClr; 20.1.4.2.10 fillRef), as
// PowerPoint prints it. Impress takes the colour opaque.
//
// # Corpus evidence
//
//   - libreoffice/pptx: sd/smartart-center-cycle's ellipses, whose colour
//     definition gives accent1 at half alpha, print in Office at half
//     alpha, the overlaps darker.
func (fc *fillCtx) withPh(c css.Color) *fillCtx {
	out := *fc
	out.ph = &c
	return &out
}

// colorChoice resolves one colour choice element (a srgbClr, a
// schemeClr...) with its transforms in fc's scope, phClr standing for
// fc's colour.
func (fc *fillCtx) colorChoice(e *ooxml.Element) (css.Color, bool) {
	return ooxml.DrawingColorChoice(e, fc.sc.resolver(fc.ph))
}

// fillOf returns the fill among e's children (an spPr, a bgPr, a tcPr, a
// grpSpPr...), false when e holds none.
func (r *reader) fillOf(fc *fillCtx, e *ooxml.Element) (slides.Fill, bool) {
	for _, k := range e.Kids() {
		if f, ok := r.fillChoice(fc, k); ok {
			return f, true
		}
	}
	return slides.Fill{}, false
}

// fillChoice reads one fill element (ECMA-376 Part 1, 20.1.8: noFill,
// solidFill, gradFill, blipFill, pattFill, grpFill), false for anything
// else.
func (r *reader) fillChoice(fc *fillCtx, k *ooxml.Element) (slides.Fill, bool) {
	if k.Space != ooxml.NSA {
		return slides.Fill{}, false
	}
	switch k.Local {
	case "noFill":
		return slides.Fill{}, true
	case "solidFill":
		c, ok := fc.sc.color(k, fc.ph)
		if !ok {
			return slides.Fill{}, true
		}
		return slides.Fill{Kind: slides.FillSolid, Color: c}, true
	case "gradFill":
		g := r.gradient(fc, k)
		if g == nil {
			return slides.Fill{}, true
		}
		return slides.Fill{Kind: slides.FillGradient, Gradient: g}, true
	case "blipFill":
		ref := ooxml.ParsePicture(&ooxml.Element{Space: ooxml.NSPic, Local: "pic", Children: []*ooxml.Element{k}})
		pic := blipEffects(r.ctx, r.pictureOf(fc.part, ref), k.Child(ooxml.NSA, "blip"), fc.colorChoice)
		if pic == nil {
			return slides.Fill{}, true
		}
		tile := k.Child(ooxml.NSA, "tile") != nil
		pic = ooxml.Cropped(pic, ref.Crop)
		if !tile {
			pic = ooxml.Cropped(pic, fillRectCrop(k.Child(ooxml.NSA, "stretch").Child(ooxml.NSA, "fillRect")))
		}
		return slides.Fill{Kind: slides.FillPicture, Picture: pic, Tile: tile}, true
	case "pattFill":
		// PowerPoint prints the preset's tile, not Impress's hatch.
		f := slides.Fill{Kind: slides.FillPattern, Pattern: attr(k, "prst"), PatternTile: true, Color: css.Color{A: 1}, Background: css.Color{R: 1, G: 1, B: 1, A: 1}}
		if f.Pattern == "" {
			f.Pattern = "pct5"
		}
		if c, ok := fc.sc.color(k.Child(ooxml.NSA, "fgClr"), fc.ph); ok {
			f.Color = c
		}
		if c, ok := fc.sc.color(k.Child(ooxml.NSA, "bgClr"), fc.ph); ok {
			f.Background = c
		}
		return f, true
	case "grpFill":
		if fc.group != nil {
			return *fc.group, true
		}
		return slides.Fill{}, true
	}
	return slides.Fill{}, false
}

// fillRectCrop returns the crop that draws a stretched picture into the
// fill rectangle of a blip fill (ECMA-376 Part 1, 20.1.8.30 fillRect: the
// insets of the rectangle from the shape's box, in thousandths of a
// percent): a negative inset puts part of the picture past the box, cut
// off; a positive one leaves blank room.
func fillRectCrop(fr *ooxml.Element) [4]float64 {
	var in [4]float64
	for side, name := range [4]string{css.Top: "t", css.Right: "r", css.Bottom: "b", css.Left: "l"} {
		if v, ok := intAttr(fr, name); ok {
			in[side] = max(-10, min(0.99, float64(v)/100000))
		}
	}
	fw, fh := 1-in[css.Left]-in[css.Right], 1-in[css.Top]-in[css.Bottom]
	if fw <= 0.001 || fh <= 0.001 {
		return [4]float64{}
	}
	return [4]float64{css.Top: -in[css.Top] / fh, css.Right: -in[css.Right] / fw, css.Bottom: -in[css.Bottom] / fh, css.Left: -in[css.Left] / fw}
}

// gradient reads a gradFill (20.1.8.33): its stops by position, and
// blended as PowerPoint blends them ([officeRamp]), its
// linear angle (20.1.8.41 lin ang, 60,000ths of a degree clockwise from
// pointing right) or its path, drawn radial (20.1.8.46 path). A gradient
// of one stop is that colour at both ends; nil for none.
func (r *reader) gradient(fc *fillCtx, k *ooxml.Element) *slides.Gradient {
	g := &slides.Gradient{}
	for _, gs := range k.Child(ooxml.NSA, "gsLst").Kids() {
		if !gs.Is(ooxml.NSA, "gs") {
			continue
		}
		c, ok := fc.sc.color(gs, fc.ph)
		if !ok {
			continue
		}
		pos, _ := intAttr(gs, "pos")
		g.Stops = append(g.Stops, slides.Stop{Position: max(0, min(1, float64(pos)/100000)), Color: c})
		if len(g.Stops) >= maxGradientStops {
			break
		}
	}
	if len(g.Stops) == 0 {
		return nil
	}
	sort.SliceStable(g.Stops, func(i, j int) bool { return g.Stops[i].Position < g.Stops[j].Position })
	if len(g.Stops) == 1 {
		g.Stops = []slides.Stop{{Position: 0, Color: g.Stops[0].Color}, {Position: 1, Color: g.Stops[0].Color}}
	}
	g.Blend = officeRamp(g.Stops)
	if lin := k.Child(ooxml.NSA, "lin"); lin != nil {
		a, _ := intAttr(lin, "ang")
		g.Angle = float64(a) / 60000
	} else if path := k.Child(ooxml.NSA, "path"); path != nil {
		// The centre is the middle of the focus rectangle (20.1.8.31
		// fillToRect: its insets from the box, in thousandths of a
		// percent); none centres the gradient.
		g.Radial, g.CenterX, g.CenterY = true, 0.5, 0.5
		if fr := path.Child(ooxml.NSA, "fillToRect"); fr != nil {
			in := func(name string) float64 {
				v, _ := intAttr(fr, name)
				return float64(v) / 100000
			}
			g.CenterX = max(0, min(1, (1+in("l")-in("r"))/2))
			g.CenterY = max(0, min(1, (1+in("t")-in("b"))/2))
		}
	}
	return g
}

// officeRamp returns stops that paint a gradient as PowerPoint blends
// its stops ([ooxml.Ramp]).
func officeRamp(stops []slides.Stop) []slides.Stop {
	in := make([]ooxml.RampStop, len(stops))
	for i, st := range stops {
		in[i] = ooxml.RampStop{Pos: st.Position, Color: st.Color}
	}
	out := ooxml.Ramp(in)
	res := make([]slides.Stop, len(out))
	for i, st := range out {
		res[i] = slides.Stop{Position: st.Pos, Color: st.Color}
	}
	return res
}

// maxGradientStops caps the stops of a gradient. Anti-DoS guard, not
// spec-mandated: Office writes at most ten.
const maxGradientStops = 64

// firstColor returns the colour a fill paints most of: a solid fill's, a
// gradient's first stop, a pattern's foreground; false for none.
func firstColor(f slides.Fill) (css.Color, bool) {
	switch f.Kind {
	case slides.FillSolid, slides.FillPattern:
		return f.Color, true
	case slides.FillGradient:
		return f.Gradient.Stops[0].Color, true
	case slides.FillNone, slides.FillPicture:
	}
	return css.Color{}, false
}

// styleFill returns the fill a style matrix reference names (20.1.4.2.10
// fillRef, 19.3.1.3 bgRef): idx 0 none, 1 to 999 the theme's fill styles,
// 1001 and up its background fill styles, phClr standing for the
// reference's colour. A picture of the theme's styles is the theme part's.
//
// # Corpus evidence
//
//   - libreoffice/pptx: sd/tdf123684's master background, the theme's third
//     background style, is a blipFill of the theme's own image.
func (r *reader) styleFill(fc *fillCtx, ref *ooxml.Element) slides.Fill {
	el, tfc, f := r.styleFillOf(fc, ref)
	if el == nil {
		return f
	}
	f, _ = r.fillChoice(tfc, el)
	return f
}

// styleFillOf returns the theme's fill element a style matrix reference
// names, with the context it is read in; nil and the fill the reference
// stands for alone when the theme has no such style ([reader.styleFill]).
func (r *reader) styleFillOf(fc *fillCtx, ref *ooxml.Element) (*ooxml.Element, *fillCtx, slides.Fill) {
	idx, ok := intAttr(ref, "idx")
	if !ok || idx == 0 {
		return nil, nil, slides.Fill{}
	}
	list := fc.sc.theme.fills
	if idx > 1000 {
		list, idx = fc.sc.theme.bgFills, idx-1000
	}
	ph, has := fc.sc.color(ref, nil)
	if idx < 1 || idx > len(list) {
		// A theme without the style: the reference's colour, solid.
		if has {
			return nil, nil, slides.Fill{Kind: slides.FillSolid, Color: ph}
		}
		return nil, nil, slides.Fill{}
	}
	// The theme part's relationships name a picture of its styles.
	tfc := fc.withPh(ph)
	if fc.sc.theme.part != "" {
		tfc.part = fc.sc.theme.part
	}
	return list[idx-1], tfc, slides.Fill{}
}

// stoplessGradient returns the fill of a shape's gradFill g that states
// no stops (20.1.8.33 gradFill: gsLst is optional): the stops of the
// gradient its style's fill reference ref names, along g's own direction,
// as PowerPoint paints it; false when g states stops or the reference
// names no gradient.
//
// # Corpus evidence
//
//   - poi/pptx: slideshow/63200.pptx's ellipse, a gradFill of a 45-degree
//     lin and no gsLst under fillRef 3, prints in the theme's third fill's
//     stops, diagonally.
func (r *reader) stoplessGradient(fc *fillCtx, g, ref *ooxml.Element) (slides.Fill, bool) {
	if g == nil || g.Child(ooxml.NSA, "gsLst") != nil || ref == nil {
		return slides.Fill{}, false
	}
	el, tfc, _ := r.styleFillOf(fc, ref)
	stops := el.Child(ooxml.NSA, "gsLst")
	if !el.Is(ooxml.NSA, "gradFill") || stops == nil {
		return slides.Fill{}, false
	}
	merged := &ooxml.Element{Space: g.Space, Local: g.Local, Attrs: g.Attrs, Children: append([]*ooxml.Element{stops}, g.Children...)}
	f, _ := r.fillChoice(tfc, merged)
	return f, f.Kind == slides.FillGradient
}

// shapeFill returns the fill of the first shape of chain whose spPr sets
// one, else the fill its style's fillRef names. The style counts as the
// formatting of the shape of chain that carries it, before the shapes it
// inherits from: a slide's placeholder with a p:style of its own takes
// the style's fill over a fill its layout's or master's placeholder
// states, as PowerPoint draws it (ECMA-376 Part 1, 19.3.1.36 ph: the
// placeholder inherits the properties the shape does not set).
//
// # Corpus evidence
//
//   - libreoffice/pptx: sd/bnc904423.pptx, a body placeholder whose
//     p:style names fillRef 1 in accent2 and whose master's placeholder
//     states noFill, prints filled in accent2.
func (r *reader) shapeFill(fc *fillCtx, chain []*ooxml.Element, style *ooxml.Element) slides.Fill {
	ref := style.Child(ooxml.NSA, "fillRef")
	for _, el := range chain {
		efc := *fc
		efc.part = r.partOf(el, fc.part)
		if f, ok := r.fillOf(&efc, spPr(el)); ok {
			if f.Kind != slides.FillNone {
				return f
			}
			if sf, ok := r.stoplessGradient(fc, spPr(el).Child(ooxml.NSA, "gradFill"), ref); ok {
				return sf
			}
			return f
		}
		if ref != nil && kid(el, "style") == style {
			return r.styleFill(fc, ref)
		}
	}
	if ref != nil {
		return r.styleFill(fc, ref)
	}
	return slides.Fill{}
}

// lineSource is one a:ln a line's properties are looked up in, with the
// colour phClr stands for in it.
type lineSource struct {
	ln *ooxml.Element
	fc *fillCtx
}

// shapeLine returns the line of a shape: each property from the first of
// the chain's a:ln elements that sets it, then from the theme line style
// the style's lnRef names (20.1.2.2.24 ln; 20.1.4.2.19 lnRef).
//
// The style's line counts as the formatting of the shape that carries it,
// before the shapes it inherits from, as the fill does ([reader.shapeFill]).
func (r *reader) shapeLine(fc *fillCtx, chain []*ooxml.Element, style *ooxml.Element) slides.Line {
	var srcs []lineSource
	var styled *lineSource
	if ref := style.Child(ooxml.NSA, "lnRef"); ref != nil {
		if idx, ok := intAttr(ref, "idx"); ok && idx >= 1 && idx <= len(fc.sc.theme.lines) {
			ph, _ := fc.sc.color(ref, nil)
			styled = &lineSource{fc.sc.theme.lines[idx-1], fc.withPh(ph)}
		}
	}
	for _, el := range chain {
		if ln := spPr(el).Child(ooxml.NSA, "ln"); ln != nil {
			srcs = append(srcs, lineSource{ln, fc})
		}
		if styled != nil && kid(el, "style") == style {
			srcs = append(srcs, *styled)
			styled = nil
		}
	}
	if styled != nil {
		srcs = append(srcs, *styled)
	}
	return r.lineOf(srcs)
}

// defaultLineWidth is the width of a line whose a:ln sets none: 9525 EMU,
// three quarters of a point, what PowerPoint writes for its thinnest line.
const defaultLineWidth = 0.75

// maxDashes caps the dash and gap lengths of a custom dash. Anti-DoS
// guard, not spec-mandated: real dashes have a handful.
const maxDashes = 32

// lineOf resolves a line from its sources, each property from the first
// that sets it; no source with a fill strokes nothing.
func (r *reader) lineOf(srcs []lineSource) slides.Line {
	var l slides.Line
	filled := false
	for _, s := range srcs {
		f, ok := r.fillOf(s.fc, s.ln)
		if !ok {
			continue
		}
		c, has := firstColor(f)
		if f.Kind == slides.FillNone || !has {
			return slides.Line{}
		}
		l.Color, filled = c, true
		break
	}
	if !filled {
		return slides.Line{}
	}
	l.Width = defaultLineWidth
	for _, s := range srcs {
		if w, ok := s.ln.Attr("w"); ok {
			if v, ok := ooxml.EMU(w); ok && v >= 0 {
				l.Width = v
			}
			break
		}
	}
	if l.Width == 0 {
		// A zero width is a hairline, which LibreOffice draws one device
		// pixel wide; the width left is the dashes' unit.
		l.Width, l.Hairline = defaultLineWidth/3, true
	}
	unit := max(l.Width, 0.25)
	for _, s := range srcs {
		if d := s.ln.Child(ooxml.NSA, "prstDash"); d != nil {
			l.Dash = shape.PresetDash(attr(d, "val"), unit)
			break
		}
		if d := s.ln.Child(ooxml.NSA, "custDash"); d != nil {
			for _, ds := range d.Kids() {
				if !ds.Is(ooxml.NSA, "ds") || len(l.Dash) >= maxDashes {
					continue
				}
				dv, _ := intAttr(ds, "d")
				sv, _ := intAttr(ds, "sp")
				l.Dash = append(l.Dash, max(0, float64(dv))/100000*unit, max(0, float64(sv))/100000*unit)
			}
			break
		}
	}
	for _, s := range srcs {
		if v, ok := s.ln.Attr("cap"); ok {
			switch v {
			case "rnd":
				l.Cap = slides.CapRound
			case "sq":
				l.Cap = slides.CapSquare
			}
			break
		}
	}
	for _, s := range srcs {
		switch {
		case s.ln.Child(ooxml.NSA, "round") != nil:
			l.Join = slides.JoinRound
		case s.ln.Child(ooxml.NSA, "bevel") != nil:
			l.Join = slides.JoinBevel
		case s.ln.Child(ooxml.NSA, "miter") != nil:
			l.Join = slides.JoinMiter
		default:
			continue
		}
		break
	}
	l.Dash, l.Cap = officeDash(l.Dash, unit, l.Cap)
	l.Head = arrowOf(srcs, "headEnd")
	l.Tail = arrowOf(srcs, "tailEnd")
	if !l.Hairline && (l.Head.Kind != slides.ArrowNone || l.Tail.Kind != slides.ArrowNone) {
		l.Width = max(l.Width, minArrowedWidth)
	}
	return l
}

// minArrowedWidth is the width PowerPoint strokes a line with an
// arrowhead at least, in points: a thinner line with a head or a tail
// prints a point wide, its arrowheads with it (ECMA-376 Part 1,
// 20.1.2.2.24 ln w leaves the printed width to the application). A line
// without an arrowhead keeps its width.
//
// # Corpus evidence
//
//   - probe, pptx: lines of 0.25, 0.5, 0.75 and 1 point with an open or a
//     triangle tail print their shafts 1 point wide, one of 1.5 points 1.5
//     wide; the same lines without arrowheads print at their widths.
//   - libreoffice/pptx: sd-data/fdo47434.pptx's arrowed connectors of
//     0.75 point print 1 point wide.
const minArrowedWidth = 1.0

// officeDash returns the dash array dash of a line width wide and the cap
// c as PowerPoint strokes them, by the rule Word and Excel share
// ([ooxml.OfficeDash]): under a round cap each dash a width shorter and
// each gap a width longer, under a square cap the lengths as they are
// with flat caps.
func officeDash(dash []float64, width float64, c slides.Cap) ([]float64, slides.Cap) {
	if len(dash) == 0 {
		return dash, c
	}
	tc := typeset.ButtCap
	switch c {
	case slides.CapRound:
		tc = typeset.RoundCap
	case slides.CapSquare:
		tc = typeset.SquareCap
	case slides.CapFlat:
	}
	dash, tc = ooxml.OfficeDash(dash, width, tc)
	switch tc {
	case typeset.RoundCap:
		return dash, slides.CapRound
	case typeset.SquareCap:
		return dash, slides.CapSquare
	case typeset.ButtCap:
	}
	return dash, slides.CapFlat
}

// arrowOf reads the first headEnd or tailEnd of the sources (20.1.8.37,
// 20.1.8.57): its kind and its width and length, 0.5 for sm, 1 for med, 2
// for lg.
func arrowOf(srcs []lineSource, local string) slides.Arrow {
	for _, s := range srcs {
		e := s.ln.Child(ooxml.NSA, local)
		if e == nil {
			continue
		}
		a := slides.Arrow{Width: 1, Length: 1}
		switch attr(e, "type") {
		case "triangle":
			a.Kind = slides.ArrowTriangle
		case "stealth":
			a.Kind = slides.ArrowStealth
		case "diamond":
			a.Kind = slides.ArrowDiamond
		case "oval":
			a.Kind = slides.ArrowOval
		case "arrow":
			a.Kind = slides.ArrowOpen
		default:
			return slides.Arrow{}
		}
		a.Width, a.Length = arrowSize(attr(e, "w")), arrowSize(attr(e, "len"))
		return a
	}
	return slides.Arrow{}
}

// arrowSize maps ST_LineEndWidth and ST_LineEndLength (20.1.10.32,
// 20.1.10.34) to a multiple of the medium size.
func arrowSize(v string) float64 {
	switch v {
	case "sm":
		return 0.5
	case "lg":
		return 2
	}
	return 1
}
