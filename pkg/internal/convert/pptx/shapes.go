package pptx

import (
	"fmt"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/shape"
)

// The graphic data URIs of a graphic frame (ECMA-376 Part 1, 20.1.2.2.17
// graphicData; 19.3.2.4 for the OLE object).
const (
	uriTable   = "http://schemas.openxmlformats.org/drawingml/2006/table"
	uriChart   = "http://schemas.openxmlformats.org/drawingml/2006/chart"
	uriDiagram = "http://schemas.openxmlformats.org/drawingml/2006/diagram"
	uriOLE     = "http://schemas.openxmlformats.org/presentationml/2006/ole"
)

// shapeCtx is where a shape tree is read: its layer, the colour scope its
// shapes resolve in, the namespace of its elements (PresentationML, or the
// diagram drawing's), the part its relationships belong to, and the fill
// a member of a group takes for grpFill.
type shapeCtx struct {
	layer *layer
	sc    scope
	ns    string
	part  string
	// slide marks the slide's own tree, whose placeholders print; a
	// layout's or master's placeholders are prompts and never print.
	slide bool
	group *slides.Fill
	// title collects the text of the slide's title placeholder.
	title string
}

// kid returns el's child local in el's own namespace.
func kid(el *ooxml.Element, local string) *ooxml.Element {
	if el == nil {
		return nil
	}
	return el.Child(el.Space, local)
}

// shapeList reads the shapes of the shape tree or group tree into out, in
// stacking order (ECMA-376 Part 1, 19.3.1.45 spTree). It stops with
// errStop past [maxShapes].
func (r *reader) shapeList(out *[]*slides.Shape, c *shapeCtx, tree *ooxml.Element, depth int) error {
	for _, el := range tree.Kids() {
		if el.Space != c.ns {
			continue
		}
		switch el.Local {
		case "sp", "cxnSp", "pic", "graphicFrame", "grpSp":
		case "contentPart":
			r.warn("unsupported-content", "ink drawn on a slide is left out")
			continue
		default:
			continue
		}
		if err := r.ctx.Err(); err != nil {
			return err
		}
		r.shapes++
		if r.shapes > maxShapes {
			r.warn("too-large", fmt.Sprintf("the presentation holds more than %d shapes: the rest are left out", maxShapes))
			return errStop
		}
		if hidden(el) {
			continue
		}
		if _, isPh := phOf(el); isPh && !c.slide {
			continue
		}
		var s *slides.Shape
		var err error
		switch el.Local {
		case "sp", "cxnSp":
			s = r.autoShape(c, el)
		case "pic":
			s = r.picture(c, el)
		case "graphicFrame":
			s, err = r.graphicFrame(c, el, depth)
		case "grpSp":
			s, err = r.group(c, el, depth)
		}
		if err != nil {
			return err
		}
		if s != nil {
			*out = append(*out, s)
		}
	}
	return nil
}

// nvProps returns a shape's non-visual drawing properties, its cNvPr
// (20.1.2.2.8).
func nvProps(el *ooxml.Element) *ooxml.Element {
	for _, nv := range el.Kids() {
		if strings.HasPrefix(nv.Local, "nv") {
			if pr := kid(nv, "cNvPr"); pr != nil {
				return pr
			}
		}
	}
	return nil
}

// hidden reports a shape its cNvPr hides.
func hidden(el *ooxml.Element) bool {
	return boolAttr(nvProps(el), "hidden", false)
}

// spPr returns a shape's properties, p:spPr (or the diagram drawing's).
func spPr(el *ooxml.Element) *ooxml.Element {
	if el.Local == "grpSp" {
		return kid(el, "grpSpPr")
	}
	return kid(el, "spPr")
}

// placement is a shape's transform (20.1.7.6 xfrm): its box in its
// parent's coordinates, its rotation and flips, and for a group the child
// box (20.1.7.5 chOff and chExt).
type placement struct {
	box          compose.Rect
	rot          float64
	flipH, flipV bool
	child        compose.Rect
}

// readXfrm reads an a:xfrm or p:xfrm, false when it lacks off or ext.
func readXfrm(x *ooxml.Element) (placement, bool) {
	var p placement
	off, ext := x.Child(ooxml.NSA, "off"), x.Child(ooxml.NSA, "ext")
	if off == nil || ext == nil {
		return p, false
	}
	p.box = compose.Rect{X: emuAttr(off, "x"), Y: emuAttr(off, "y"), Width: max(0, emuAttr(ext, "cx")), Height: max(0, emuAttr(ext, "cy"))}
	p.rot, p.flipH, p.flipV = ooxml.Transform2D(x)
	if co, ce := x.Child(ooxml.NSA, "chOff"), x.Child(ooxml.NSA, "chExt"); co != nil && ce != nil {
		p.child = compose.Rect{X: emuAttr(co, "x"), Y: emuAttr(co, "y"), Width: max(0, emuAttr(ce, "cx")), Height: max(0, emuAttr(ce, "cy"))}
	}
	return p, true
}

// placeOf returns the transform of the first shape of chain that has one:
// a placeholder without its own xfrm takes its layout's or master's
// (19.3.1.36).
func placeOf(chain []*ooxml.Element) (placement, bool) {
	for _, el := range chain {
		x := spPr(el).Child(ooxml.NSA, "xfrm")
		if el.Local == "graphicFrame" {
			x = kid(el, "xfrm")
		}
		if p, ok := readXfrm(x); ok {
			return p, true
		}
	}
	return placement{}, false
}

// place sets s's box, rotation and flips from chain, false when no shape
// of the chain places it.
func place(s *slides.Shape, chain []*ooxml.Element) bool {
	p, ok := placeOf(chain)
	if !ok {
		return false
	}
	s.Box, s.Rotation, s.FlipH, s.FlipV = p.box, p.rot, p.flipH, p.flipV
	return true
}

// geometryOf returns the geometry of the first shape of chain that sets
// one (20.1.9.18 prstGeom with its 20.1.9.5 avLst, 20.1.9.8 custGeom), nil
// for a rectangle.
func (r *reader) geometryOf(chain []*ooxml.Element) *shape.Geometry {
	for _, el := range chain {
		pr := spPr(el)
		if g := pr.Child(ooxml.NSA, "prstGeom"); g != nil {
			name := attr(g, "prst")
			geo := &shape.Geometry{Preset: name}
			for _, gd := range g.Child(ooxml.NSA, "avLst").Kids() {
				if !gd.Is(ooxml.NSA, "gd") {
					continue
				}
				if v, ok := strings.CutPrefix(strings.TrimSpace(attr(gd, "fmla")), "val "); ok {
					if n, ok := ooxml.Int(v); ok {
						geo.Adjust = append(geo.Adjust, shape.Adjust{Name: attr(gd, "name"), Value: float64(n)})
					}
				}
			}
			if (name == "rect" || name == "") && len(geo.Adjust) == 0 {
				return nil
			}
			return geo
		}
		if g := pr.Child(ooxml.NSA, "custGeom"); g != nil {
			return &shape.Geometry{Custom: r.customGeometry(g)}
		}
	}
	return nil
}

// customGeometry reads a custom geometry (ECMA-376 Part 1, 20.1.9.8
// custGeom: guides, adjust handles, connection sites, the text rectangle
// and the paths) into the geometry engine's form. Until the engine's
// parser is wired it returns nil, which draws the shape's bounding
// rectangle, and warns.
func (r *reader) customGeometry(_ *ooxml.Element) *shape.Custom {
	r.warn("approximated", "a custom geometry is drawn as its bounding rectangle")
	return nil
}

// styleRefs returns the p:style of the first shape of chain that has one
// (20.1.2.2.37 style).
func styleRefs(chain []*ooxml.Element) *ooxml.Element {
	for _, el := range chain {
		if st := kid(el, "style"); st != nil {
			return st
		}
	}
	return nil
}

// href returns the target of a hyperlink element (21.1.2.3.5 hlinkClick)
// of part: an external relationship's URI, "" for an internal jump or
// none.
func (r *reader) href(part string, h *ooxml.Element) string {
	if h == nil {
		return ""
	}
	id, _ := h.AttrNS(ooxml.NSR, "id")
	if id == "" {
		return ""
	}
	if rel, ok := r.pkg.RelationshipByID(part, id); ok && rel.External {
		return rel.Target
	}
	return ""
}

// footerKind reports a date, footer, slide number or header placeholder.
func footerKind(p placeholder) bool {
	switch p.typ {
	case "dt", "ftr", "sldNum", "hdr":
		return true
	}
	return false
}

// autoShape reads a p:sp or p:cxnSp (19.3.1.43 sp, 19.3.1.19 cxnSp): its
// placement, geometry, fill, line and text, each inherited from its
// placeholders as far as the slide leaves them unset. A placeholder
// without text is an empty prompt and prints nothing, as Impress leaves
// empty presentation objects out; a date, footer or slide number
// placeholder prints only when the slide's layout has one of its type,
// as Impress turns them into its master page's header and footer fields.
//
// # Corpus evidence
//
//   - litho: a slide's slide number placeholder prints the slide's number
//     at the slide's own place when its layout holds one, and nothing when
//     only the master does, whatever the master's p:hf says; a placeholder
//     no layer places prints nothing.
func (r *reader) autoShape(c *shapeCtx, el *ooxml.Element) *slides.Shape {
	chain := inherited(c.layer, el)
	p, isPh := phOf(el)
	nv := nvProps(el)
	if isPh && footerKind(p) && c.layer.parent.find(p, false) == nil {
		return nil
	}
	s := &slides.Shape{Kind: slides.KindGeometry, Name: attr(nv, "name"), Href: r.href(c.part, nv.Child(ooxml.NSA, "hlinkClick"))}
	if !place(s, chain) {
		// Impress drops a placeholder neither the slide, its layout nor
		// its master places.
		return nil
	}
	s.Geometry = r.geometryOf(chain)
	if el.Local == "cxnSp" && s.Geometry == nil {
		s.Geometry = &shape.Geometry{Preset: "line"}
	}
	style := styleRefs(chain)
	fc := &fillCtx{sc: c.sc, part: c.part, group: c.group}
	s.Fill = r.shapeFill(fc, chain, style)
	s.Line = r.shapeLine(fc, chain, style)
	r.effects(chain)
	if el.Local == "sp" {
		s.Text = r.textBody(c, chain, p, style.Child(ooxml.NSA, "fontRef"))
	}
	if isPh {
		if s.Text == nil || empty(s.Text) {
			return nil
		}
		if p.class() == "title" && c.title == "" {
			c.title = plain(s.Text)
		}
	}
	if tx := kid(el, "txXfrm"); tx != nil && s.Text != nil {
		return splitText(s, tx)
	}
	return s
}

// splitText returns a diagram drawing's shape whose text sits in a box of
// its own (MS-ODRAWXML, 2.4.2 txXfrm) as a group of the shape without its
// text and a shape of the text alone in that box.
func splitText(s *slides.Shape, tx *ooxml.Element) *slides.Shape {
	p, ok := readXfrm(tx)
	if !ok {
		return s
	}
	text := &slides.Shape{Kind: slides.KindGeometry, Name: s.Name, Box: p.box, Rotation: p.rot, FlipH: p.flipH, FlipV: p.flipV, Text: s.Text}
	shape := *s
	shape.Text = nil
	g := &slides.Shape{Kind: slides.KindGroup, Name: s.Name, Kids: []*slides.Shape{&shape, text}}
	g.Box = union(g.Kids)
	g.Child = g.Box
	return g
}

// effects reports the effects a printout leaves out (20.1.8.26 effectLst,
// 20.1.4.1.26 scene3d, 20.1.5.12 sp3d) of the first shape of chain that
// sets them.
func (r *reader) effects(chain []*ooxml.Element) {
	for _, el := range chain {
		pr := spPr(el)
		if e := pr.Child(ooxml.NSA, "effectLst"); e != nil && len(e.Children) > 0 {
			r.warn("unsupported-content", "shape effects (shadows, glows, reflections, soft edges) are not drawn")
		}
		if pr.Child(ooxml.NSA, "scene3d") != nil || pr.Child(ooxml.NSA, "sp3d") != nil {
			r.warn("unsupported-content", "3D shape properties are not drawn: shapes print flat")
		}
	}
}

// group reads a p:grpSp (19.3.1.22): its box and child space, and its
// members in the child space; members with grpFill take the group's
// fill.
func (r *reader) group(c *shapeCtx, el *ooxml.Element, depth int) (*slides.Shape, error) {
	if depth >= maxGroupDepth {
		r.warn("too-large", fmt.Sprintf("shape groups nested deeper than %d are left out", maxGroupDepth))
		return nil, nil
	}
	nv := nvProps(el)
	s := &slides.Shape{Kind: slides.KindGroup, Name: attr(nv, "name"), Href: r.href(c.part, nv.Child(ooxml.NSA, "hlinkClick"))}
	pr := kid(el, "grpSpPr")
	p, ok := readXfrm(pr.Child(ooxml.NSA, "xfrm"))
	if ok {
		s.Box, s.Rotation, s.FlipH, s.FlipV, s.Child = p.box, p.rot, p.flipH, p.flipV, p.child
		if s.Child.Width <= 0 || s.Child.Height <= 0 {
			s.Child = compose.Rect{}
		}
	}
	inner := *c
	if f, ok := r.fillOf(&fillCtx{sc: c.sc, part: c.part, group: c.group}, pr); ok {
		inner.group = &f
	}
	if err := r.shapeList(&s.Kids, &inner, el, depth+1); err != nil {
		return nil, err
	}
	c.title = inner.title
	if !ok {
		// A group without a transform places its members in its parent's
		// space: its box is their union.
		s.Box = union(s.Kids)
	}
	if len(s.Kids) == 0 {
		return nil, nil
	}
	return s, nil
}

// union returns the bounding box of the shapes' boxes.
func union(kids []*slides.Shape) compose.Rect {
	var u compose.Rect
	for i, k := range kids {
		b := k.Box
		if i == 0 {
			u = b
			continue
		}
		x0, y0 := min(u.X, b.X), min(u.Y, b.Y)
		x1, y1 := max(u.X+u.Width, b.X+b.Width), max(u.Y+u.Height, b.Y+b.Height)
		u = compose.Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
	}
	return u
}
