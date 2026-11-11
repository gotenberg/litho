package docx

import (
	"strings"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/shape"
)

// pxPt is the size of a VML pixel, the unit of a top-level shape's
// unitless lengths (ECMA-376 Part 4, 14.1.2.19).
const pxPt = 0.75

// vml builds the VML drawings of a w:pict or w:object (ECMA-376 Part 4,
// 14.1): groups of shapes, text boxes, pictures, and rectangles, rounded
// rectangles, ovals and lines with their fill and stroke. A shape
// positioned absolutely is anchored by its style's offsets and its
// w10:wrap as a DrawingML anchor is; any other sits in the line.
func (d *document) vml(s *story, c *ooxml.Element, st *css.Style, sink *inlineSink, depth int) {
	d.readShapeTypes(c)
	drawn := false
	for _, k := range c.Kids() {
		if k.Space != ooxml.NSV || k.Local == "shapetype" {
			continue
		}
		vs := ooxml.ParseVMLStyle(attrOr(k, "style"))
		if vs["visibility"] == "hidden" || vs["display"] == "none" {
			continue
		}
		w, h := vmlSize(k, vs, pxPt)
		n := d.vmlShape(s, k, vs, st, w, h, vs["position"] != "absolute", depth)
		if n == nil {
			continue
		}
		drawn = true
		if vs["position"] == "absolute" {
			d.place(vmlPlacement(k, vs, w, h), n, sink)
		}
		sink.add(n)
	}
	if !drawn && c.Local == "object" {
		d.warn("unsupported-content", "an embedded object without a picture is left out")
	}
}

// readShapeTypes records the shape type numbers of the v:shapetype
// children of c, which later shapes name by id (Part 4, 14.1.2.20).
func (d *document) readShapeTypes(c *ooxml.Element) {
	for _, k := range c.Kids() {
		if !k.Is(ooxml.NSV, "shapetype") {
			continue
		}
		spt, ok := k.AttrNS(ooxml.NSO, "spt")
		if !ok {
			continue
		}
		if v, ok := ooxml.Int(spt); ok {
			d.vmlTypes[attrOr(k, "id")] = v
		}
	}
}

// vmlSize returns the size of a VML shape in points: its style's width
// and height in units of unit points each, a line's extent between its
// end points.
func vmlSize(k *ooxml.Element, vs ooxml.VMLStyle, unit float64) (float64, float64) {
	w, _ := vs.Length("width", unit)
	h, _ := vs.Length("height", unit)
	if k.Local == "line" {
		x0, y0 := vmlPoint(attrOr(k, "from"))
		x1, y1 := vmlPoint(attrOr(k, "to"))
		w, h = abs(x1-x0), abs(y1-y0)
	}
	return max(0, w), max(0, h)
}

// vmlPoint parses a VML point ("10pt,20pt", Part 4, 14.1.2.12 from) in
// points.
func vmlPoint(s string) (float64, float64) {
	a, b, _ := strings.Cut(s, ",")
	x, _ := ooxml.Measure(strings.TrimSuffix(strings.TrimSpace(a), "px"), pxPt)
	y, _ := ooxml.Measure(strings.TrimSuffix(strings.TrimSpace(b), "px"), pxPt)
	return x, y
}

// vmlRel maps a VML mso-position-horizontal-relative or
// mso-position-vertical-relative value (MS-OI29500, 2.1.1394) to its
// DrawingML name (20.4.3.4, 20.4.3.5): text is the column horizontally
// and the paragraph vertically.
func vmlRel(v string, vertical bool) string {
	switch v {
	case "page", "margin":
		return v
	case "char":
		return "character"
	case "line":
		return "line"
	case "left-margin-area":
		return "leftMargin"
	case "right-margin-area":
		return "rightMargin"
	case "top-margin-area":
		return "topMargin"
	case "bottom-margin-area":
		return "bottomMargin"
	case "inner-margin-area":
		return "insideMargin"
	case "outer-margin-area":
		return "outsideMargin"
	}
	if vertical {
		return "paragraph"
	}
	return "column"
}

// vmlPlacement returns the placement of an absolutely positioned VML
// shape: its margin-left and margin-top (with left and top) offsets, its
// mso-position alignments and references, a negative z-index behind the
// text, and its w10:wrap (none when absent: in front of the text).
func vmlPlacement(k *ooxml.Element, vs ooxml.VMLStyle, w, h float64) placement {
	p := placement{w: w, h: h, wrap: "wrapNone"}
	ml, _ := vs.Length("margin-left", pxPt)
	l, _ := vs.Length("left", pxPt)
	mt, _ := vs.Length("margin-top", pxPt)
	t, _ := vs.Length("top", pxPt)
	p.posH, p.posV = ml+l, mt+t
	p.relH = vmlRel(vs["mso-position-horizontal-relative"], false)
	p.relV = vmlRel(vs["mso-position-vertical-relative"], true)
	if a := vs["mso-position-horizontal"]; a != "" && a != "absolute" {
		p.alignH = a
	}
	if a := vs["mso-position-vertical"]; a != "" && a != "absolute" {
		p.alignV = a
	}
	p.behind = strings.HasPrefix(vs["z-index"], "-")
	if v, ok := ooxml.Int(vs["z-index"]); ok {
		p.z = max(0, min(v, maxZ))
	}
	for i, name := range []string{"mso-wrap-distance-top", "mso-wrap-distance-right", "mso-wrap-distance-bottom", "mso-wrap-distance-left"} {
		if v, ok := vs.Length(name, pxPt); ok {
			p.dist[i] = max(0, v)
		}
	}
	if wr := k.Child(ooxml.NSW10, "wrap"); wr != nil {
		switch attrOr(wr, "type") {
		case "square":
			p.wrap = "wrapSquare"
		case "tight":
			p.wrap = "wrapTight"
		case "through":
			p.wrap = "wrapThrough"
		case "topAndBottom":
			p.wrap = "wrapTopAndBottom"
		}
	}
	return p
}

// vmlShape builds the VML shape k of w by h points: a group, a text box,
// a picture, or a preset geometry drawn with the shape's fill and stroke;
// a shape of another geometry is left out, with a warning, and keeps its
// room in the line when inLine.
func (d *document) vmlShape(s *story, k *ooxml.Element, vs ooxml.VMLStyle, st *css.Style, w, h float64, inLine bool, depth int) *layout.Node {
	if depth > maxNesting {
		return nil
	}
	if k.Local == "group" {
		return d.vmlGroup(s, k, st, w, h, depth)
	}
	ss := vmlShapeStyle(k)
	if tb := k.Child(ooxml.NSV, "textbox"); tb != nil {
		if content := tb.Child(ooxml.NSW, "txbxContent"); content != nil {
			n := d.textBox(s, content, w, h, ss, vmlInsets(attrOr(tb, "inset")), attrOr(k, "alt"), depth)
			turn(n, vmlFlow(ooxml.ParseVMLStyle(attrOr(tb, "style"))))
			return n
		}
	}
	if img := k.Child(ooxml.NSV, "imagedata"); img != nil {
		id, ok := img.AttrNS(ooxml.NSR, "id")
		if !ok {
			id, _ = img.AttrNS(ooxml.NSO, "relid")
		}
		n := d.picture(s, ooxml.PictureRef{Embed: id}, st, w, h, attrOr(img, "title"))
		if n != nil {
			setTransform(n.Style, vmlRotation(vs), strings.Contains(vs["flip"], "x"), strings.Contains(vs["flip"], "y"))
		}
		return n
	}
	geom := d.vmlGeometry(k)
	if geom == "" {
		d.warn("approximated", "a VML shape of a custom geometry is left out")
		if !inLine || w <= 0 && h <= 0 {
			return nil
		}
		return box(st, w, h)
	}
	g := &shape.Geometry{Preset: geom}
	d.checkGeometry(g)
	n := vectorShape(st, g, ss, w, h)
	setTransform(n.Style, vmlRotation(vs), strings.Contains(vs["flip"], "x"), strings.Contains(vs["flip"], "y"))
	return n
}

// vmlFlow maps a v:textbox's layout-flow (MS-OI29500, 2.1.1394) onto a
// text direction: vertical reads top to bottom, or bottom to top with
// mso-layout-flow-alt bottom-to-top.
func vmlFlow(vs ooxml.VMLStyle) string {
	if !strings.HasPrefix(vs["layout-flow"], "vertical") {
		return ""
	}
	if vs["mso-layout-flow-alt"] == "bottom-to-top" {
		return "btLr"
	}
	return "tbRl"
}

// vmlRotation returns the rotation of a VML shape's style in degrees.
func vmlRotation(vs ooxml.VMLStyle) float64 {
	v, ok := ooxml.Measure(strings.TrimSuffix(vs["rotation"], "fd"), 1)
	if !ok {
		return 0
	}
	if strings.HasSuffix(vs["rotation"], "fd") {
		v /= 65536
	}
	return v
}

// vmlGeometry returns the preset geometry a VML shape draws (Part 4,
// 14.1.2): its element's, else its shape type's o:spt mapped onto the
// DrawingML preset of the same shape ([ooxml.VMLPreset]), "" for a shape
// of its own v:path or a type without a preset.
func (d *document) vmlGeometry(k *ooxml.Element) string {
	switch k.Local {
	case "rect":
		return "rect"
	case "roundrect":
		return "roundRect"
	case "oval":
		return "ellipse"
	case "line":
		return "line"
	case "shape":
	default:
		return ""
	}
	spt, ok := 0, false
	if v, has := k.AttrNS(ooxml.NSO, "spt"); has {
		spt, ok = ooxml.Int(v)
	}
	if !ok {
		spt, ok = d.vmlTypes[strings.TrimPrefix(attrOr(k, "type"), "#")]
	}
	if !ok {
		return ""
	}
	p, _ := ooxml.VMLPreset(spt)
	return p
}

// vmlShapeStyle returns the fill and stroke of a VML shape (Part 4,
// 14.1.2.19 filled, fillcolor, stroked, strokecolor, strokeweight, and
// the v:fill and v:stroke children): a white fill and a black stroke of
// 0.75 point by default, a fill's opacity as its colour's alpha.
func vmlShapeStyle(k *ooxml.Element) ooxml.ShapeStyle {
	var ss ooxml.ShapeStyle
	if vmlOn(k, "filled") {
		c := css.Color{R: 1, G: 1, B: 1, A: 1}
		if v, ok := ooxml.VMLColor(attrOr(k, "fillcolor")); ok {
			c = v
		}
		fill := k.Child(ooxml.NSV, "fill")
		if fill != nil {
			if v, ok := ooxml.VMLColor(attrOr(fill, "color")); ok {
				c = v
			}
			if v, ok := ooxml.VMLFraction(attrOr(fill, "opacity")); ok {
				c.A = v
			}
		}
		if fill == nil || vmlOn(fill, "on") {
			ss.Fill = &c
		}
	}
	if vmlOn(k, "stroked") {
		c := css.Color{A: 1}
		if v, ok := ooxml.VMLColor(attrOr(k, "strokecolor")); ok {
			c = v
		}
		ss.LineWidth = pxPt
		if v, ok := ooxml.Measure(strings.TrimSuffix(attrOr(k, "strokeweight"), "px"), 1); ok {
			ss.LineWidth = max(0, v)
		}
		stroke := k.Child(ooxml.NSV, "stroke")
		if stroke != nil {
			if v, ok := ooxml.VMLColor(attrOr(stroke, "color")); ok {
				c = v
			}
			if v, ok := ooxml.Measure(attrOr(stroke, "weight"), 1); ok {
				ss.LineWidth = max(0, v)
			}
		}
		if stroke == nil || vmlOn(stroke, "on") {
			ss.Line = &c
		}
	}
	return ss
}

// vmlOn reads a VML boolean attribute (Part 4, 14.1.2.19 ST_TrueFalse: t,
// true, f, false), true when absent.
func vmlOn(e *ooxml.Element, name string) bool {
	switch strings.ToLower(strings.TrimSpace(attrOr(e, name))) {
	case "f", "false", "0", "off":
		return false
	}
	return true
}

// vmlInsets returns the insets of a v:textbox (Part 4, 14.1.2.22 inset:
// left, top, right, bottom, 0.1 inch and 0.05 inch by default) as top,
// right, bottom, left.
func vmlInsets(s string) [4]float64 {
	in := [4]float64{3.6, 7.2, 3.6, 7.2}
	order := []css.Side{css.Left, css.Top, css.Right, css.Bottom}
	for i, part := range strings.Split(s, ",") {
		if i >= len(order) {
			break
		}
		if v, ok := ooxml.Measure(strings.TrimSuffix(strings.TrimSpace(part), "px"), pxPt); ok {
			in[order[i]] = max(0, v)
		}
	}
	return in
}

// vmlGroup builds a v:group (Part 4, 14.1.2.7) of w by h points: an
// inline block holding its shapes, each positioned and sized by its style
// in the group's coordinate space (coordorigin, coordsize: 1000 by 1000
// by default).
func (d *document) vmlGroup(s *story, g *ooxml.Element, st *css.Style, w, h float64, depth int) *layout.Node {
	gs := inlineStyle(st)
	gs.Display = css.InlineBlock
	gs.Width, gs.Height = css.Pts(w), css.Pts(h)
	gs.Position = css.Relative
	gs.LineHeight = css.Dimension{Kind: css.Normal}
	n := &layout.Node{Kind: layout.Element, Style: gs}
	ox, oy := ooxml.VMLPair(attrOr(g, "coordorigin"), 0)
	cw, ch := ooxml.VMLPair(attrOr(g, "coordsize"), 1000)
	sx, sy := 1.0, 1.0
	if cw > 0 {
		sx = w / cw
	}
	if ch > 0 {
		sy = h / ch
	}
	d.readShapeTypes(g)
	for _, k := range g.Kids() {
		if k.Space != ooxml.NSV || k.Local == "shapetype" {
			continue
		}
		vs := ooxml.ParseVMLStyle(attrOr(k, "style"))
		if vs["visibility"] == "hidden" {
			continue
		}
		kw, kh := vmlSize(k, vs, 1)
		x, _ := vs.Length("left", 1)
		y, _ := vs.Length("top", 1)
		if k.Local == "line" {
			x0, y0 := vmlPoint(attrOr(k, "from"))
			x1, y1 := vmlPoint(attrOr(k, "to"))
			x, y = min(x0, x1)/pxPt, min(y0, y1)/pxPt
			kw, kh = kw/pxPt, kh/pxPt
		}
		kid := d.vmlShape(s, k, vs, st, kw*sx, kh*sy, false, depth+1)
		if kid == nil {
			continue
		}
		kid.Style.Position = css.Absolute
		kid.Style.Left, kid.Style.Top = css.Pts((x-ox)*sx), css.Pts((y-oy)*sy)
		if kid.Style.Display == css.Inline || kid.Style.Display == css.InlineBlock {
			kid.Style.Display = css.Block
		}
		n.Children = append(n.Children, kid)
	}
	return n
}
