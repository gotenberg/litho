package odp

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/odf"
	"github.com/gotenberg/litho/pkg/internal/shape"
)

// shapeList reads the drawing shapes among els (ODF 1.3, Part 3, 10.3 to
// 10.6) in stacking order: by draw:z-index (19.231) where a shape gives
// one, by document order otherwise. On a master page, the presentation
// placeholders a slide does not show are left out.
func (r *reader) shapeList(els []*odf.Element, pc *pageCtx, depth int) []*slides.Shape {
	type entry struct {
		key   int
		shape *slides.Shape
	}
	var list []entry
	for i, el := range els {
		if el.IsText() || !r.tick() {
			continue
		}
		if pc.master && depth == 0 && !pc.masterShows(el.A(odf.NSPresentation, "class")) {
			continue
		}
		key := i
		if z, err := strconv.Atoi(el.A(odf.NSDraw, "z-index")); err == nil && z >= 0 {
			key = z
		}
		for _, s := range r.shape(el, pc, depth, "") {
			list = append(list, entry{key: key, shape: s})
		}
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].key < list[j].key })
	out := make([]*slides.Shape, 0, len(list))
	for _, e := range list {
		out = append(out, e.shape)
	}
	return out
}

// shape reads one element of a page, a master page or a group: a shape,
// a group, or a draw:a hyperlink around shapes (10.4.12), whose href the
// shapes carry; nil for an element that draws nothing printed.
func (r *reader) shape(el *odf.Element, pc *pageCtx, depth int, href string) []*slides.Shape {
	if depth > maxGroupDepth {
		r.warn("group-depth", fmt.Sprintf("groups nested past %d levels are not drawn", maxGroupDepth))
		return nil
	}
	switch {
	case el.Is(odf.NSDraw, "a"):
		var out []*slides.Shape
		for _, k := range el.Children {
			if !k.IsText() {
				out = append(out, r.shape(k, pc, depth+1, el.A(odf.NSXLink, "href"))...)
			}
		}
		return out
	case el.Name.Space == odf.NSAnim, el.Is(odf.NSPresentation, "animations"):
		r.warn("animations", "animations are not played: every shape prints in its final place")
		return nil
	case el.Name.Space == odf.NSDr3d:
		r.warn("3d-scene", "3D scenes are not drawn")
		return nil
	case el.Is(odf.NSOffice, "annotation"), el.Is(odf.NSOfficeOOO, "annotation"):
		r.warn("comments", "comments are not printed")
		return nil
	case el.Is(odf.NSDraw, "control"):
		r.warn("form-controls", "form controls are not drawn")
		return nil
	case el.Is(odf.NSOffice, "forms"), el.Is(odf.NSPresentation, "notes"), el.Is(odf.NSDraw, "page-thumbnail"),
		el.Is(odf.NSOffice, "event-listeners"), el.Name.Space == odf.NSLOExt:
		return nil
	case el.Name.Space != odf.NSDraw:
		return nil
	}
	if d := r.layers[el.A(odf.NSDraw, "layer")]; d == "none" || d == "screen" {
		return nil
	}
	if !pc.master && el.A(odf.NSPresentation, "placeholder") == "true" {
		// An empty placeholder shows its prompt on screen and prints
		// nothing (19.411).
		return nil
	}
	if r.shapes >= maxShapes {
		r.warn("too-many-shapes", fmt.Sprintf("shapes past %d are not drawn", maxShapes))
		return nil
	}
	r.shapes++
	if h := eventHref(el); h != "" {
		href = h
	}
	var s *slides.Shape
	switch el.Name.Local {
	case "frame":
		s = r.frame(el, pc)
	case "g":
		s = r.group(el, pc, depth)
	case "custom-shape", "rect", "ellipse", "circle", "line", "connector", "polygon", "polyline", "path", "regular-polygon", "caption":
		s = r.drawShape(el, pc)
	case "measure":
		r.warn("measure", "dimension lines are not drawn")
	case "page-thumbnail":
	default:
		r.warn("shape-"+el.Name.Local, fmt.Sprintf("draw:%s is not drawn", el.Name.Local))
	}
	if s == nil {
		return nil
	}
	s.Name = el.A(odf.NSDraw, "name")
	if s.Href == "" {
		s.Href = href
	}
	return []*slides.Shape{s}
}

// eventHref returns the target of a shape's click action that opens a
// document or a URL (ODF 1.3, Part 3, 10.9.2 presentation:event-listener
// with presentation:action show), "" for none.
func eventHref(el *odf.Element) string {
	for _, l := range el.Child(odf.NSOffice, "event-listeners").All(odf.NSPresentation, "event-listener") {
		if l.A(odf.NSPresentation, "action") == "show" {
			if h := l.A(odf.NSXLink, "href"); h != "" {
				return h
			}
		}
	}
	return ""
}

// placement reads a shape's svg:x, svg:y, svg:width, svg:height and
// draw:transform (ODF 1.3, Part 3, 19.228) into its unrotated box, its
// clockwise rotation about the box's centre and its mirroring.
type placement struct {
	box          compose.Rect
	rotation     float64
	flipH, flipV bool
}

// placementOf returns the placement of el, the box of def standing in for
// a missing size.
func (r *reader) placementOf(el *odf.Element, def *compose.Rect) placement {
	x, _ := odf.Length(el.A(odf.NSSVG, "x"))
	y, _ := odf.Length(el.A(odf.NSSVG, "y"))
	w, okW := odf.Length(el.A(odf.NSSVG, "width"))
	h, okH := odf.Length(el.A(odf.NSSVG, "height"))
	if def != nil && (!okW || !okH) {
		x, y, w, h = def.X, def.Y, def.Width, def.Height
	}
	w, h = max(w, 0), max(h, 0)
	tr := el.A(odf.NSDraw, "transform")
	if tr == "" {
		return placement{box: compose.Rect{X: x, Y: y, Width: w, Height: h}}
	}
	m, skew, ok := parseTransform(tr)
	if !ok {
		r.warn("transform", "a draw:transform that does not parse is not applied")
		return placement{box: compose.Rect{X: x, Y: y, Width: w, Height: h}}
	}
	if skew {
		r.warn("skew", "skewed shapes are drawn unskewed")
	}
	m = m.mul(translation(x, y))
	return m.place(w, h)
}

// frame reads a draw:frame (ODF 1.3, Part 3, 10.4.2) from the first of
// its contents it draws: a text box, a picture, a chart object, a table;
// an object it does not draw gives way to its replacement picture, the
// draw:image after it.
func (r *reader) frame(el *odf.Element, pc *pageCtx) *slides.Shape {
	pl := r.placementOf(el, r.placeholderBox(el, pc))
	ss := r.shapeStyle(el, pc)
	s := &slides.Shape{Box: pl.box, Rotation: pl.rotation, FlipH: pl.flipH, FlipV: pl.flipV}
	for _, k := range el.Children {
		switch {
		case k.Is(odf.NSDraw, "text-box"):
			s.Kind = slides.KindGeometry
			s.Fill = r.fill(ss.graphic, "solid")
			s.Line = r.line(ss.graphic)
			s.Text = r.textBody(k, ss, pc)
			r.textBodyProps(s.Text, ss.graphic, true)
			r.effects(ss.graphic)
			return s
		case k.Is(odf.NSDraw, "image"):
			pic := r.picture(k)
			if pic == nil {
				continue
			}
			s.Kind = slides.KindPicture
			s.Picture = r.cropped(pic, ss.graphic.Get("fo:clip"))
			switch m := ss.graphic.Get("style:mirror"); {
			case strings.Contains(m, "horizontal"):
				s.FlipH = !s.FlipH
				if strings.Contains(m, "vertical") {
					s.FlipV = !s.FlipV
				}
			case m == "vertical":
				s.FlipV = !s.FlipV
			}
			if strings.TrimSpace(k.TextContent()) != "" {
				r.warn("picture-text", "the text of a picture is not drawn")
			}
			return s
		case k.Is(odf.NSDraw, "object"):
			if c := r.object(k); c != nil {
				s.Kind = slides.KindChart
				s.Chart = c
				return s
			}
		case k.Is(odf.NSDraw, "object-ole"):
			r.warn("ole-object", "embedded OLE objects are drawn as their replacement picture")
		case k.Is(odf.NSTable, "table"):
			if t := r.table(k, s.Box, pc); t != nil {
				s.Kind = slides.KindTable
				s.Table = t
				return s
			}
		case k.Is(odf.NSDraw, "plugin"):
			r.warn("media", "audio and video are drawn as their replacement picture")
		case k.Is(odf.NSDraw, "applet"), k.Is(odf.NSDraw, "floating-frame"):
			r.warn("frame-"+k.Name.Local, fmt.Sprintf("draw:%s is not drawn", k.Name.Local))
		}
	}
	return nil
}

// hasParagraphs reports an element holding text:p or text:list children.
func hasParagraphs(el *odf.Element) bool {
	for _, k := range el.Children {
		if k.Is(odf.NSText, "p") || k.Is(odf.NSText, "h") || k.Is(odf.NSText, "list") {
			return true
		}
	}
	return false
}

// placeholderBox returns the box of the master page's placeholder of the
// same presentation class as el, which a slide's placeholder without a
// size of its own takes (ODF 1.3, Part 3, 19.393); nil for none.
func (r *reader) placeholderBox(el *odf.Element, pc *pageCtx) *compose.Rect {
	class := el.A(odf.NSPresentation, "class")
	if class == "" || pc.master || pc.mp == nil {
		return nil
	}
	for _, k := range pc.mp.El.Children {
		if k.Is(odf.NSDraw, "frame") && k.A(odf.NSPresentation, "class") == class {
			x, _ := odf.Length(k.A(odf.NSSVG, "x"))
			y, _ := odf.Length(k.A(odf.NSSVG, "y"))
			w, _ := odf.Length(k.A(odf.NSSVG, "width"))
			h, _ := odf.Length(k.A(odf.NSSVG, "height"))
			return &compose.Rect{X: x, Y: y, Width: w, Height: h}
		}
	}
	return nil
}

// group reads a draw:g (ODF 1.3, Part 3, 10.3.15). An ODF group has no
// coordinate space of its own: its shapes are placed on the page, so the
// group's box and child space are both the bounding box of its shapes.
func (r *reader) group(el *odf.Element, pc *pageCtx, depth int) *slides.Shape {
	kids := r.shapeList(el.Children, pc, depth+1)
	if len(kids) == 0 {
		return nil
	}
	x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	for _, k := range kids {
		b := k.Box
		x0, y0 = min(x0, b.X), min(y0, b.Y)
		x1, y1 = max(x1, b.X+b.Width), max(y1, b.Y+b.Height)
	}
	box := compose.Rect{X: x0, Y: y0, Width: x1 - x0, Height: y1 - y0}
	return &slides.Shape{Kind: slides.KindGroup, Box: box, Child: box, Kids: kids}
}

// drawShape reads a drawing shape (ODF 1.3, Part 3, 10.3 and 10.6): its
// geometry, its fill and stroke, and its text.
func (r *reader) drawShape(el *odf.Element, pc *pageCtx) *slides.Shape {
	ss := r.shapeStyle(el, pc)
	pl := r.placementOf(el, nil)
	s := &slides.Shape{Kind: slides.KindGeometry, Box: pl.box, Rotation: pl.rotation, FlipH: pl.flipH, FlipV: pl.flipV}
	s.Fill = r.fill(ss.graphic, "solid")
	s.Line = r.line(ss.graphic)
	switch el.Name.Local {
	case "custom-shape":
		s.Geometry = r.enhancedGeometry(el.Child(odf.NSDraw, "enhanced-geometry"), s)
	case "rect":
		if rad, ok := odf.Length(el.A(odf.NSDraw, "corner-radius")); ok && rad > 0 {
			if side := min(s.Box.Width, s.Box.Height); side > 0 {
				s.Geometry = &shape.Geometry{Preset: "roundRect", Adjust: []shape.Adjust{{Name: "adj", Value: math.Min(rad/side, 0.5) * 100000}}}
			}
		}
	case "ellipse", "circle":
		s.Geometry = &shape.Geometry{Preset: "ellipse"}
		if k := el.A(odf.NSDraw, "kind"); k != "" && k != "full" {
			r.warn("ellipse-kind", "arcs, sections and cuts of an ellipse are drawn as the whole ellipse")
		}
	case "line", "connector":
		x1, _ := odf.Length(el.A(odf.NSSVG, "x1"))
		y1, _ := odf.Length(el.A(odf.NSSVG, "y1"))
		x2, _ := odf.Length(el.A(odf.NSSVG, "x2"))
		y2, _ := odf.Length(el.A(odf.NSSVG, "y2"))
		s.Box = compose.Rect{X: min(x1, x2), Y: min(y1, y2), Width: math.Abs(x2 - x1), Height: math.Abs(y2 - y1)}
		s.FlipH, s.FlipV = x2 < x1, y2 < y1
		s.Fill = slides.Fill{}
		s.Geometry = &shape.Geometry{Preset: "line"}
		if el.Name.Local == "connector" {
			s.Geometry = &shape.Geometry{Preset: connectorPreset(el.A(odf.NSDraw, "type"))}
		}
	case "polyline":
		s.Fill = slides.Fill{}
		s.Geometry = &shape.Geometry{Custom: r.customGeometry(el)}
	case "polygon", "path", "regular-polygon":
		s.Geometry = &shape.Geometry{Custom: r.customGeometry(el)}
	case "caption":
		r.warn("caption", "the callout line of a caption shape is not drawn")
	}
	if hasParagraphs(el) {
		s.Text = r.textBody(el, ss, pc)
		r.textBodyProps(s.Text, ss.graphic, false)
	}
	r.effects(ss.graphic)
	return s
}

// connectorPreset maps a connector's draw:type (ODF 1.3, Part 3, 19.229)
// onto the DrawingML connector drawn alike: standard, the elbow
// connector, as bentConnector3; curve as curvedConnector3; line and
// lines as a straight connector.
func connectorPreset(typ string) string {
	switch typ {
	case "curve":
		return "curvedConnector3"
	case "line", "lines":
		return "straightConnector1"
	}
	return "bentConnector3"
}

// enhancedGeometry reads a custom shape's draw:enhanced-geometry (ODF
// 1.3, Part 3, 10.6.2): its draw:type as the preset, spelled as the file
// spells it, which the geometry engine maps onto a DrawingML one, and its
// draw:modifiers as the adjustments "$0", "$1"... in their order; a shape
// of no type or "non-primitive" is a custom geometry. draw:mirror-
// horizontal and -vertical (19.192, 19.193) mirror the shape.
func (r *reader) enhancedGeometry(eg *odf.Element, s *slides.Shape) *shape.Geometry {
	if eg == nil {
		return nil
	}
	if eg.A(odf.NSDraw, "mirror-horizontal") == "true" {
		s.FlipH = !s.FlipH
	}
	if eg.A(odf.NSDraw, "mirror-vertical") == "true" {
		s.FlipV = !s.FlipV
	}
	typ := eg.A(odf.NSDraw, "type")
	if typ == "" || typ == "non-primitive" {
		return &shape.Geometry{Custom: r.customGeometry(eg)}
	}
	g := &shape.Geometry{Preset: typ}
	for i, f := range strings.Fields(eg.A(odf.NSDraw, "modifiers")) {
		if i >= maxModifiers {
			break
		}
		v, err := strconv.ParseFloat(f, 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		g.Adjust = append(g.Adjust, shape.Adjust{Name: "$" + strconv.Itoa(i), Value: v})
	}
	return g
}

// maxModifiers bounds the draw:modifiers values read: 32. Anti-DoS
// guard, not spec-mandated: Impress's shapes take eight at most.
const maxModifiers = 32

// customGeometry returns the custom geometry of a draw:enhanced-geometry,
// a draw:path, a draw:polygon, a draw:polyline or a draw:regular-polygon
// (ODF 1.3, Part 3, 10.6.2, 10.3.7, 10.3.5, 10.3.4, 10.3.6). The geometry
// engine reads them; until it is wired here, the shape is drawn as its
// bounding rectangle, with a warning.
func (r *reader) customGeometry(el *odf.Element) *shape.Custom {
	r.warn("custom-geometry", fmt.Sprintf("the custom geometry of draw:%s is drawn as its bounding box", el.Name.Local))
	return nil
}
