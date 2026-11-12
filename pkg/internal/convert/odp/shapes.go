package odp

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/css"
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
// shapes carry; nil for an element that draws nothing printed. An empty
// placeholder (19.411 presentation:placeholder) prints its fill and its
// line, its prompt text being the screen's alone (measured on the pinned
// LibreOffice).
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
	if r.shapes >= maxShapes {
		r.warn("too-many-shapes", fmt.Sprintf("shapes past %d are not drawn", maxShapes))
		return nil
	}
	r.shapes++
	jump := r.eventJump(el, pc)
	if h := eventHref(el); h != "" && jump == 0 {
		href = h
	}
	var s *slides.Shape
	switch el.Name.Local {
	case "frame":
		s = r.frame(el, pc)
	case "g":
		s = r.group(el, pc, depth)
	case "custom-shape", "rect", "ellipse", "circle", "line", "connector", "polygon", "polyline", "path", "regular-polygon", "caption", "measure":
		s = r.drawShape(el, pc)
	case "page-thumbnail":
	default:
		r.warnOnce("shape-"+el.Name.Local, "unsupported-shape", fmt.Sprintf("draw:%s is not drawn", el.Name.Local))
	}
	if s == nil {
		return nil
	}
	s.Name = el.A(odf.NSDraw, "name")
	if s.Alt == "" {
		s.Alt = odf.Describe(el)
	}
	if pc.master && depth == 0 && el.A(odf.NSPresentation, "class") == "" {
		// A master page's own drawing repeats on every slide that shows
		// it, as a page's background does: a pagination artifact (ISO
		// 32000-1, 14.8.2.2.2), as the master page of a text document is.
		s.Decorative = true
	}
	s.Jump = jump
	if s.Href == "" && jump == 0 {
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

// eventJump returns the 1-based position in the document of the slide a
// shape's click action goes to (ODF 1.3, Part 3, 19.414
// presentation:action): next-page and previous-page from the slide pc
// reads, first-page, last-page, and show with an xlink:href "#name"
// naming a page by its draw:name or the name Impress shows ("Slide 2");
// 0 for none, for a master page's shape, or for a target out of the
// document.
func (r *reader) eventJump(el *odf.Element, pc *pageCtx) int {
	if pc.master || pc.number <= 0 {
		return 0
	}
	for _, l := range el.Child(odf.NSOffice, "event-listeners").All(odf.NSPresentation, "event-listener") {
		n := 0
		switch l.A(odf.NSPresentation, "action") {
		case "next-page":
			n = pc.number + 1
		case "previous-page":
			n = pc.number - 1
		case "first-page":
			n = 1
		case "last-page":
			n = r.pageCount
		case "show":
			if name, ok := strings.CutPrefix(l.A(odf.NSXLink, "href"), "#"); ok {
				n = r.pageNames[name]
			}
		}
		if n >= 1 && n <= r.pageCount {
			return n
		}
	}
	return 0
}

// placement reads a shape's svg:x, svg:y, svg:width, svg:height and
// draw:transform (ODF 1.3, Part 3, 19.228) into its unrotated box, its
// clockwise rotation about the box's centre and its mirroring.
type placement struct {
	box          compose.Rect
	rotation     float64
	flipH, flipV bool
	// skew shears the box about its centre before the rotation, in
	// degrees, the top moving right for a positive angle.
	skew float64
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
	m, _, ok := parseTransform(tr)
	if !ok {
		r.warn("transform", "a draw:transform that does not parse is not applied")
		return placement{box: compose.Rect{X: x, Y: y, Width: w, Height: h}}
	}
	m = m.mul(translation(x, y))
	return m.place(w, h)
}

// frame reads a draw:frame (ODF 1.3, Part 3, 10.4.2) from the first of
// its contents it draws: a text box, a picture, a chart object, a table;
// an object it does not draw gives way to its replacement picture, the
// draw:image after it. A picture linked outside the package prints
// nothing, its replacement included; a media object without a
// replacement picture prints as a #434343 box.
//
// # Corpus evidence
//
//   - libreoffice/odp: sd/tdf116266's frame of a linked SVG and a PNG
//     prints nothing.
//   - libreoffice/odp: sd/tdf53970_linked.odp's linked video prints as a
//     #434343 box.
func (r *reader) frame(el *odf.Element, pc *pageCtx) *slides.Shape {
	pl := r.placementOf(el, r.placeholderBox(el, pc))
	ss := r.shapeStyle(el, pc)
	s := &slides.Shape{Box: pl.box, Rotation: pl.rotation, FlipH: pl.flipH, FlipV: pl.flipV, SkewX: pl.skew}
	s.Alt, s.Decorative = odf.Describe(el), odf.Decorative(ss.graphic)
	media := false
	for _, k := range el.Children {
		switch {
		case k.Is(odf.NSDraw, "text-box"):
			s.Kind = slides.KindGeometry
			s.Fill = r.fill(ss.graphic, "solid")
			s.Line = r.line(ss.graphic)
			s.Text = r.textBody(k, ss, pc)
			r.textBodyProps(s.Text, ss.graphic, true)
			r.effects(ss.graphic, s)
			return s
		case k.Is(odf.NSDraw, "image"):
			pic := r.picture(k)
			if pic == nil && linkedOutside(k.A(odf.NSXLink, "href")) {
				// A picture linked outside the package prints nothing, its
				// replacement picture included, as Impress, which does not
				// fetch the link on a conversion, prints its frame.
				return nil
			}
			if pic == nil {
				continue
			}
			s.Kind = slides.KindPicture
			s.Picture = r.cropped(adjusted(r.ctx, pic, ss.graphic), ss.graphic.Get("fo:clip"))
			if v, ok := odf.Percent(ss.graphic.Get("draw:image-opacity")); ok && v < 1 {
				s.Picture = translucent(s.Picture, max(v, 0))
			}
			switch m := ss.graphic.Get("style:mirror"); {
			case strings.Contains(m, "horizontal"):
				s.FlipH = !s.FlipH
				if strings.Contains(m, "vertical") {
					s.FlipV = !s.FlipV
				}
			case m == "vertical":
				s.FlipV = !s.FlipV
			}
			if hasParagraphs(k) && strings.TrimSpace(k.TextContent()) != "" {
				// A picture's text is drawn over it, in the frame's box,
				// as Impress draws it.
				t := &slides.Shape{Kind: slides.KindGeometry, Box: s.Box, Rotation: s.Rotation, Text: r.textBody(k, ss, pc)}
				r.textBodyProps(t.Text, ss.graphic, false)
				return &slides.Shape{Kind: slides.KindGroup, Box: s.Box, Child: s.Box, Kids: []*slides.Shape{s, t}}
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
			media = true
		case k.Is(odf.NSDraw, "applet"), k.Is(odf.NSDraw, "floating-frame"):
			r.warnOnce("frame-"+k.Name.Local, "unsupported-frame", fmt.Sprintf("draw:%s is not drawn", k.Name.Local))
		}
	}
	if media {
		// A media object without a replacement picture prints as a dark
		// grey box, #434343.
		s.Kind = slides.KindGeometry
		s.Fill = slides.Fill{Kind: slides.FillSolid, Color: css.Color{R: 0x43 / 255.0, G: 0x43 / 255.0, B: 0x43 / 255.0, A: 1}}
		return s
	}
	return nil
}

// linkedOutside reports an xlink:href naming a file outside the package:
// one of a scheme, or a relative path leaving it (ODF 1.3, Part 3,
// 19.910: a package-relative href never starts with "../" or "/").
func linkedOutside(href string) bool {
	return strings.Contains(href, "://") || strings.HasPrefix(href, "../") || strings.HasPrefix(href, "/")
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
//
// # Corpus evidence
//
//   - probe, odp: an L of either draw:mirror-horizontal or
//     draw:mirror-vertical and draw:transform rotate (-0.349) leans 20
//     degrees the other way in the pinned LibreOffice; with both mirrors,
//     a half turn, it does not.
func (r *reader) drawShape(el *odf.Element, pc *pageCtx) *slides.Shape {
	ss := r.shapeStyle(el, pc)
	pl := r.placementOf(el, nil)
	s := &slides.Shape{Kind: slides.KindGeometry, Box: pl.box, Rotation: pl.rotation, FlipH: pl.flipH, FlipV: pl.flipV, SkewX: pl.skew}
	s.Alt, s.Decorative = odf.Describe(el), odf.Decorative(ss.graphic)
	s.Fill = r.fill(ss.graphic, "solid")
	s.Line = r.line(ss.graphic)
	switch el.Name.Local {
	case "custom-shape":
		s.Geometry = r.enhancedGeometry(el.Child(odf.NSDraw, "enhanced-geometry"))
		if g := s.Geometry; g != nil && g.MirrorH != g.MirrorV && s.Rotation != 0 {
			// Impress mirrors a rotated custom shape about the page's axes
			// through its centre, after the rotation: the mirror of a
			// shape turned one way is the mirrored shape turned the other.
			s.Rotation = 360 - s.Rotation
		}
	case "rect":
		if rad, ok := odf.Length(el.A(odf.NSDraw, "corner-radius")); ok && rad > 0 {
			if side := min(s.Box.Width, s.Box.Height); side > 0 {
				s.Geometry = &shape.Geometry{Preset: "roundRect", Adjust: []shape.Adjust{{Name: "adj", Value: math.Min(rad/side, 0.5) * 100000}}}
			}
		}
	case "ellipse", "circle":
		s.Geometry = ellipseGeometry(el)
	case "line", "connector", "measure":
		x1, _ := odf.Length(el.A(odf.NSSVG, "x1"))
		y1, _ := odf.Length(el.A(odf.NSSVG, "y1"))
		x2, _ := odf.Length(el.A(odf.NSSVG, "x2"))
		y2, _ := odf.Length(el.A(odf.NSSVG, "y2"))
		s.Box = compose.Rect{X: min(x1, x2), Y: min(y1, y2), Width: math.Abs(x2 - x1), Height: math.Abs(y2 - y1)}
		s.FlipH, s.FlipV = x2 < x1, y2 < y1
		s.Fill = slides.Fill{}
		s.Geometry = &shape.Geometry{Preset: "line"}
		if el.Name.Local == "measure" {
			r.warn("measure", "dimension lines are drawn as their measured line, without their extension lines")
		}
		if el.Name.Local == "connector" {
			s.Geometry = &shape.Geometry{Preset: connectorPreset(el.A(odf.NSDraw, "type"))}
			if box, g := connectorRoute(el); g != nil {
				s.Box, s.FlipH, s.FlipV, s.Geometry = box, false, false, g
			}
		}
	case "polyline":
		s.Fill = slides.Fill{}
		s.Geometry = r.outline(el)
	case "polygon", "path", "regular-polygon":
		s.Geometry = r.outline(el)
	case "caption":
		s.Geometry = captionGeometry(el, s.Box)
	}
	if hasParagraphs(el) {
		s.Text = r.textBody(el, ss, pc)
		r.textBodyProps(s.Text, ss.graphic, false)
	}
	r.effects(ss.graphic, s)
	return s
}

// captionGeometry returns the outline of a draw:caption (ODF 1.3, Part 3,
// 10.3.11): its rectangle, filled and stroked, and its callout line,
// stroked, from the middle of the rectangle's side nearest the caption
// point to that point, draw:caption-point-x and -y (19.120, 19.121),
// which are relative to the shape's corner. The line is straight, a
// caption's simplest kind; nil without a caption point.
func captionGeometry(el *odf.Element, box compose.Rect) *shape.Geometry {
	px, okX := odf.Length(el.A(odf.NSDraw, "caption-point-x"))
	py, okY := odf.Length(el.A(odf.NSDraw, "caption-point-y"))
	if !okX || !okY || box.Width <= 0 || box.Height <= 0 {
		return nil
	}
	w, h := box.Width, box.Height
	// The side whose outward direction points most at the caption point.
	dx, dy := (px-w/2)/w, (py-h/2)/h
	sx, sy := w/2, 0.0
	switch {
	case math.Abs(dx) >= math.Abs(dy) && dx < 0:
		sx, sy = 0, h/2
	case math.Abs(dx) >= math.Abs(dy):
		sx, sy = w, h/2
	case dy > 0:
		sx, sy = w/2, h
	}
	rect := odf.Points(fmt.Sprintf("0,0 %g,0 %g,%g 0,%g", w, w, h, h), compose.Rect{Width: w, Height: h}, true)
	line := odf.Points(fmt.Sprintf("%g,%g %g,%g", sx, sy, px, py), compose.Rect{Width: w, Height: h}, false)
	if rect == nil || line == nil {
		return nil
	}
	rect.Paths = append(rect.Paths, line.Paths...)
	return &shape.Geometry{Custom: rect}
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
// 1.3, Part 3, 10.6.2) through the shared OpenDocument geometry reader:
// its draw:type mapped onto the DrawingML preset of the same shape, or
// its path, equations and modifiers as a custom geometry, with its
// mirroring; nil for none. A freeform of no LibreOffice type fills its
// open subpaths as if closed.
//
// # Corpus evidence
//
//   - libreoffice/odp: the open arc of
//     sd/tdf147978_enhancedPath_viewBox.odp prints filled.
func (r *reader) enhancedGeometry(eg *odf.Element) *shape.Geometry {
	if eg == nil {
		return nil
	}
	g := odf.ParseGeometry(eg)
	if g != nil && g.Custom != nil && eg.A(odf.NSDraw, "type") == "non-primitive" {
		// A freeform of no LibreOffice type fills its open subpaths as if
		// closed.
		g.Custom.OpenUnfilled = false
	}
	if g != nil && !shape.Known(g) {
		r.warn("custom-geometry", "the shape type "+strconv.Quote(g.Preset)+" is drawn as its bounding box")
	}
	return g
}

// outline reads the outline of a draw:path, draw:polygon, draw:polyline
// or draw:regular-polygon (ODF 1.3, Part 3, 10.3.7, 10.3.5, 10.3.4,
// 10.3.6) as a custom geometry: svg:d or draw:points in the coordinate
// space svg:viewBox fitted to the shape's box, or the regular polygon
// draw:corners, draw:concave and draw:sharpness describe. An outline
// that does not parse is drawn as the bounding box, with a warning.
func (r *reader) outline(el *odf.Element) *shape.Geometry {
	var c *shape.Custom
	view := viewBoxOf(el)
	switch el.Name.Local {
	case "path":
		c = odf.PathData(el.A(odf.NSSVG, "d"), view)
	case "polygon", "polyline":
		c = odf.Points(el.A(odf.NSDraw, "points"), view, el.Name.Local == "polygon")
	case "regular-polygon":
		n, _ := strconv.Atoi(el.A(odf.NSDraw, "corners"))
		sharp, _ := strconv.ParseFloat(strings.TrimSuffix(el.A(odf.NSDraw, "sharpness"), "%"), 64)
		c = odf.RegularPolygon(n, el.A(odf.NSDraw, "concave") == "true", sharp)
	}
	if c == nil {
		r.warn("custom-geometry", fmt.Sprintf("the outline of draw:%s is drawn as its bounding box", el.Name.Local))
		return nil
	}
	return &shape.Geometry{Custom: c}
}

// viewBoxOf returns a shape's svg:viewBox (ODF 1.3, Part 3, 19.574), the
// coordinate space of its svg:d or draw:points. A missing one, or one of
// zero width or height, is the empty rectangle, which the geometry engine
// reads as the shape's size in 1/100 mm, LibreOffice's unit.
func viewBoxOf(el *odf.Element) compose.Rect {
	f := strings.FieldsFunc(el.A(odf.NSSVG, "viewBox"), func(r rune) bool { return r == ' ' || r == ',' || r == '\t' || r == '\n' || r == '\r' })
	if len(f) != 4 {
		return compose.Rect{}
	}
	var v [4]float64
	for i, x := range f {
		n, err := strconv.ParseFloat(x, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) || math.Abs(n) > 1e12 {
			return compose.Rect{}
		}
		v[i] = n
	}
	if v[2] <= 0 || v[3] <= 0 {
		return compose.Rect{X: v[0], Y: v[1]}
	}
	return compose.Rect{X: v[0], Y: v[1], Width: v[2], Height: v[3]}
}

// hundredthMM is a point in 1/100 mm, the unit of a connector's route.
const hundredthMM = 2540.0 / 72

// connectorRoute reads the route LibreOffice writes on a connector as
// svg:d (ODF 1.3, Part 3, 19.530 on draw:connector): the path of the
// drawn connector in page coordinates in 1/100 mm, svg:viewBox giving
// only its size. It returns the route's bounding box in points and the
// geometry that draws it there; a zero box and nil without a route.
func connectorRoute(el *odf.Element) (compose.Rect, *shape.Geometry) {
	d := el.A(odf.NSSVG, "d")
	if strings.TrimSpace(d) == "" {
		return compose.Rect{}, nil
	}
	c := odf.PathData(d, compose.Rect{})
	if c == nil {
		return compose.Rect{}, nil
	}
	b, ok := odf.Bounds(c.Paths[0].Cmds)
	if !ok {
		return compose.Rect{}, nil
	}
	*c.View = b
	c.Paths[0].Fill = shape.FillNone
	box := compose.Rect{X: b.X / hundredthMM, Y: b.Y / hundredthMM, Width: b.Width / hundredthMM, Height: b.Height / hundredthMM}
	return box, &shape.Geometry{Custom: c}
}

// ellipseGeometry returns the outline of a draw:ellipse or draw:circle
// (ODF 1.3, Part 3, 10.3.8 and 10.3.9): the whole ellipse, or for a
// draw:kind (19.188) of section, cut or arc the part between
// draw:start-angle and draw:end-angle, counterclockwise from three
// o'clock: a section closed through the centre, a cut closed by its
// chord, an arc open and unfilled, as Impress draws them. The ellipse is
// drawn in a 2 by 2 space fitted to the box.
//
// # Corpus evidence
//
//   - libreoffice/odp: sd/tdf99497_CircleKind.odp's arc prints its curve
//     alone.
func ellipseGeometry(el *odf.Element) *shape.Geometry {
	kind := el.A(odf.NSDraw, "kind")
	if kind != "section" && kind != "cut" && kind != "arc" {
		return &shape.Geometry{Preset: "ellipse"}
	}
	start, _ := odf.ParseAngle(el.A(odf.NSDraw, "start-angle"), false)
	end, ok := odf.ParseAngle(el.A(odf.NSDraw, "end-angle"), false)
	if !ok {
		end = 360
	}
	lit := func(v ...float64) []shape.Arg {
		out := make([]shape.Arg, len(v))
		for i, x := range v {
			out[i] = shape.Arg{Value: x}
		}
		return out
	}
	var cmds []shape.Cmd
	switch kind {
	case "section":
		cmds = []shape.Cmd{{Kind: shape.CmdMove, Args: lit(1, 1)}, {Kind: shape.CmdAngleEllipseTo, Args: lit(1, 1, 1, 1, start, end)}, {Kind: shape.CmdClose}}
	case "cut":
		cmds = []shape.Cmd{{Kind: shape.CmdAngleEllipse, Args: lit(1, 1, 1, 1, start, end)}, {Kind: shape.CmdClose}}
	default:
		cmds = []shape.Cmd{{Kind: shape.CmdAngleEllipse, Args: lit(1, 1, 1, 1, start, end)}}
	}
	pd := shape.PathDef{Stroke: true, Cmds: cmds}
	if kind == "arc" {
		pd.Fill = shape.FillNone
	}
	return &shape.Geometry{Custom: &shape.Custom{View: &compose.Rect{Width: 2, Height: 2}, Paths: []shape.PathDef{pd}}}
}
