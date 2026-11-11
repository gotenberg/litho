package docx

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/gotenberg/litho/pkg/internal/chart"
	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/shape"
)

// The graphic data URIs (ECMA-376 Part 1, 20.1.2.2.17) the converter
// tells apart.
const (
	uriPicture = "http://schemas.openxmlformats.org/drawingml/2006/picture"
	uriShape   = "http://schemas.microsoft.com/office/word/2010/wordprocessingShape"
	uriGroup   = "http://schemas.microsoft.com/office/word/2010/wordprocessingGroup"
	uriChart   = "http://schemas.openxmlformats.org/drawingml/2006/chart"
	uriDiagram = "http://schemas.openxmlformats.org/drawingml/2006/diagram"
	// uriChartEx is an Office 2016 chart (MS-ODRAWXML, 2.24 cx:chart).
	uriChartEx = ooxml.NSChartEx
)

// chart builds the chart a graphic frame's c:chart or cx:chart element
// names through its r:id (ECMA-376 Part 1, 21.2.2.27; MS-ODRAWXML, 2.24.3):
// a Figure replaced box of w by h points that draws the chart part's
// model, read by [ooxml.ParseChart], or [ooxml.ParseChartEx] when ex is
// set. It returns nil with a warning when the part is missing or does not
// parse, the frame then keeping its room empty.
//
// An Office 2016 chart sits in an mc:AlternateContent whose Choice
// requires the cx1 or cx2 namespace; the converter takes the Choice, as
// Word draws the chart and not the Fallback picture ([understood]).
//
// # Corpus evidence
//
//   - litho/docx charts-bar-column, charts-line-area,
//     charts-scatter-bubble-radar, charts-stock-combo: Word draws each
//     chart part; drawn, they pass, their layout from 0 to 0.76 to 0.87.
//   - litho/docx charts-chartex: Word prints the titles, legends and axis
//     labels of the Office 2016 charts and not their fallback pictures;
//     the Choice raises text F1 from 0.11 to 0.50.
func (d *document) chart(s *story, ref *ooxml.Element, st *css.Style, w, h float64, alt string, ex bool) *layout.Node {
	id, _ := ref.AttrNS(ooxml.NSR, "id")
	rel, ok := d.pkg.RelationshipByID(s.part, id)
	if !ok || rel.External || !d.pkg.Has(rel.Target) {
		d.warn("unsupported-content", "a chart whose part is missing is left out")
		return nil
	}
	if w <= 0 || h <= 0 {
		return nil
	}
	parse := ooxml.ParseChart
	if ex {
		parse = ooxml.ParseChartEx
	}
	c, err := parse(d.ctx, d.pkg, rel.Target)
	if err != nil {
		d.warn("chart-malformed", fmt.Sprintf("chart %s does not parse and is left out: %v", rel.Target, err))
		return nil
	}
	for _, cw := range c.Warnings {
		d.warn(cw.Code, cw.Message)
	}
	ps := inlineStyle(st)
	ps.Display = css.Inline
	ps.Width, ps.Height = css.Pts(w), css.Pts(h)
	ps.VerticalAlign = css.VerticalAlign{}
	ps.LineHeight = css.Dimension{Kind: css.Normal}
	ps.MinLineHeight = 0
	fonts := d.fonts
	pic := &layout.Picture{Width: w, Height: h}
	pic.Vector = func(ctx context.Context, r compose.Rect) []compose.Fragment {
		return chart.Render(ctx, fonts, c, r)
	}
	return &layout.Node{Kind: layout.Replaced, Style: ps, Picture: pic, Role: "Figure", Alt: alt}
}

// drawing builds a w:drawing (17.3.3.9): each wp:inline as an inline box,
// each wp:anchor placed by its wrapping (20.4.2.3).
func (d *document) drawing(s *story, c *ooxml.Element, st *css.Style, sink *inlineSink, depth int) {
	for _, k := range c.Kids() {
		inline := k.Is(ooxml.NSWP, "inline")
		if !inline && !k.Is(ooxml.NSWP, "anchor") {
			continue
		}
		var wpt, hpt float64
		if ext := k.Child(ooxml.NSWP, "extent"); ext != nil {
			wpt, _ = ooxml.EMU(attrOr(ext, "cx"))
			hpt, _ = ooxml.EMU(attrOr(ext, "cy"))
		}
		alt, decorative := ooxml.Describe(k.Child(ooxml.NSWP, "docPr"))
		data := k.Child(ooxml.NSA, "graphic").Child(ooxml.NSA, "graphicData")
		n := d.graphic(s, data, st, max(wpt, 0), max(hpt, 0), alt, depth)
		if n == nil {
			continue
		}
		if decorative && n.Role == "Figure" {
			// A drawing its author marked decorative is an artifact
			// (ISO 32000-1, 14.8.2.2.2; ISO 14289-1, 7.1).
			n.Role, n.Alt = "", ""
		}
		if !inline {
			d.anchor(k, n, wpt, hpt, sink)
		}
		sink.add(n)
	}
}

// graphic builds the content of a graphic frame of size w by h points.
func (d *document) graphic(s *story, data *ooxml.Element, st *css.Style, w, h float64, alt string, depth int) *layout.Node {
	if data == nil {
		return nil
	}
	switch attrOr(data, "uri") {
	case uriPicture:
		if pic := data.Child(ooxml.NSPic, "pic"); pic != nil {
			return d.picture(s, ooxml.ParsePicture(pic), st, w, h, alt)
		}
	case uriShape:
		if sp := data.Child(ooxml.NSWPS, "wsp"); sp != nil {
			return d.shape(s, sp, st, w, h, alt, depth)
		}
	case uriGroup:
		if g := data.Child(ooxml.NSWPG, "wgp"); g != nil {
			return d.group(s, g, st, w, h, depth)
		}
	case uriChart:
		if n := d.chart(s, data.Child(ooxml.NSChart, "chart"), st, w, h, alt, false); n != nil {
			return n
		}
	case uriChartEx:
		if n := d.chart(s, data.Child(ooxml.NSChartEx, "chart"), st, w, h, alt, true); n != nil {
			return n
		}
	case uriDiagram:
		d.warn("unsupported-content", "a SmartArt diagram is left out: the document holds no picture of it")
	default:
		d.warn("unsupported-content", "a drawing of an unknown kind is left out")
	}
	return box(st, w, h)
}

// box returns an empty inline box of w by h points, which keeps the room a
// drawing takes.
func box(st *css.Style, w, h float64) *layout.Node {
	bs := inlineStyle(st)
	bs.Display = css.InlineBlock
	bs.Width, bs.Height = css.Pts(w), css.Pts(h)
	return &layout.Node{Kind: layout.Element, Style: bs}
}

// picture builds a picture of size w by h points (0 for its natural size)
// from its reference: a Figure replaced box, cropped, rotated and flipped
// as the reference says.
//
// A picture is a Figure, its alt the description its drawing or its own
// properties carry; one its properties mark decorative is an artifact
// (ISO 32000-1, 14.8.2.2.2). A picture described nowhere stays a Figure
// without one: the source holds nothing to say about it.
func (d *document) picture(s *story, ref ooxml.PictureRef, st *css.Style, w, h float64, alt string) *layout.Node {
	if alt == "" {
		alt = ref.Descr
	}
	var pic *layout.Picture
	if ref.SVG != "" {
		pic = d.loadPicture(s.part, ref.SVG)
	}
	if pic == nil && ref.Embed != "" {
		pic = d.loadPicture(s.part, ref.Embed)
	}
	if pic == nil && ref.Link != "" {
		d.warn("unsupported-content", "a linked picture is left out: linked files are not read")
	}
	if pic == nil {
		if w > 0 && h > 0 {
			return box(st, w, h)
		}
		return nil
	}
	pic = ooxml.Cropped(pic, ref.Crop)
	ps := inlineStyle(st)
	ps.Display = css.Inline
	if w <= 0 || h <= 0 {
		w, h = pic.Width, pic.Height
	}
	ps.Width, ps.Height = css.Pts(w), css.Pts(h)
	ps.VerticalAlign = css.VerticalAlign{}
	ps.LineHeight = css.Dimension{Kind: css.Normal}
	ps.MinLineHeight = 0
	setTransform(ps, ref.Rotation, ref.FlipH, ref.FlipV)
	n := &layout.Node{Kind: layout.Replaced, Style: ps, Picture: pic, Role: "Figure", Alt: alt}
	if ref.Decorative {
		n.Role, n.Alt = "", ""
	}
	return n
}

// setTransform rotates and mirrors the box of st about its centre.
func setTransform(st *css.Style, rot float64, flipH, flipV bool) {
	if rot != 0 {
		st.Transform = append(st.Transform, css.TransformFunc{Kind: css.TransformRotate, V: [6]float64{rot * math.Pi / 180}})
	}
	if flipH || flipV {
		sx, sy := 1.0, 1.0
		if flipH {
			sx = -1
		}
		if flipV {
			sy = -1
		}
		st.Transform = append(st.Transform, css.TransformFunc{Kind: css.TransformScale, V: [6]float64{sx, sy}})
	}
}

// loadPicture decodes the picture the relationship id of part names,
// once per target, nil with a warning when it cannot be drawn.
func (d *document) loadPicture(part, id string) *layout.Picture {
	rel, ok := d.pkg.RelationshipByID(part, id)
	if !ok || rel.External {
		if ok {
			d.warn("unsupported-content", "a linked picture is left out: linked files are not read")
		}
		return nil
	}
	if pic, ok := d.pictures[rel.Target]; ok {
		return pic
	}
	if len(d.pictures) >= limits.MaxConvertPictures {
		d.warn("too-many-pictures", fmt.Sprintf("pictures past %d are not drawn", limits.MaxConvertPictures))
		return nil
	}
	d.pictures[rel.Target] = nil
	data, err := d.pkg.Read(rel.Target)
	if err != nil {
		d.warn("missing-picture", fmt.Sprintf("picture %s cannot be read: %v", rel.Target, err))
		return nil
	}
	pic, err := ooxml.DecodePicture(d.ctx, data, d.fonts)
	if err != nil {
		if errors.Is(err, ooxml.ErrUnsupportedPicture) {
			d.warn("unsupported-picture", fmt.Sprintf("picture %s is left out: %v", rel.Target, err))
		} else {
			d.warn("unsupported-picture", fmt.Sprintf("picture %s does not decode: %v", rel.Target, err))
		}
		return nil
	}
	d.pictures[rel.Target] = pic
	return pic
}

// shape builds a wps:wsp (MS-ODRAWXML, 2.3.4): a text box as a block of
// its content over the shape's fill, any other shape as a vector picture
// of its preset or custom geometry. alt describes the shape, "" to read
// its own non-visual properties.
func (d *document) shape(s *story, sp *ooxml.Element, st *css.Style, w, h float64, alt string, depth int) *layout.Node {
	if alt == "" {
		alt, _ = ooxml.Describe(sp.Child(ooxml.NSWPS, "cNvPr"))
	}
	spPr := sp.Child(ooxml.NSWPS, "spPr")
	ss := ooxml.ParseShapeStyle(spPr, sp.Child(ooxml.NSWPS, "style"), d.theme)
	if ref := ooxml.ParsePicture(sp); ref.Embed != "" && spPr.Child(ooxml.NSA, "blipFill") != nil {
		return d.picture(s, ref, st, w, h, "")
	}
	if tb := sp.Child(ooxml.NSWPS, "txbx"); tb != nil {
		bodyPr := sp.Child(ooxml.NSWPS, "bodyPr")
		n := d.textBox(s, tb.Child(ooxml.NSW, "txbxContent"), w, h, ss, bodyInsets(bodyPr), alt, depth)
		if id := attrOr(tb, "id"); id != "" {
			setChain(n.Style, id, 0, h)
		} else if vert := bodyVert(attrOr(bodyPr, "vert")); vert == "" {
			g := ooxml.ParseGeometry(spPr)
			d.checkGeometry(g)
			d.shapeTextBox(n, g, ss, w, h, attrOr(bodyPr, "anchor"), bodyPr.Child(ooxml.NSA, "spAutoFit") != nil)
		}
		turn(n, bodyVert(attrOr(bodyPr, "vert")))
		return n
	}
	if l := sp.Child(ooxml.NSWPS, "linkedTxbx"); l != nil && attrOr(l, "id") != "" {
		// A later link of a chain of text boxes holds none of the
		// chain's content, which flows into it from the link before.
		bodyPr := sp.Child(ooxml.NSWPS, "bodyPr")
		n := d.textBox(s, nil, w, h, ss, bodyInsets(bodyPr), "", depth)
		seq, _ := ooxml.Int(attrOr(l, "seq"))
		setChain(n.Style, attrOr(l, "id"), seq, h)
		turn(n, bodyVert(attrOr(bodyPr, "vert")))
		return n
	}
	g := ooxml.ParseGeometry(spPr)
	d.checkGeometry(g)
	n := vectorShape(st, g, ss, w, h)
	if xfrm := spPr.Child(ooxml.NSA, "xfrm"); xfrm != nil {
		rot, fh, fv := ooxml.Transform2D(xfrm)
		setTransform(n.Style, rot, fh, fv)
	}
	return n
}

// checkGeometry warns of a geometry the engine draws as its bounding box
// or whose guides do not all resolve.
func (d *document) checkGeometry(g *shape.Geometry) {
	if !shape.Known(g) {
		d.warn("approximated", fmt.Sprintf("a %s shape is drawn as its bounding box", g.Preset))
	} else if err := shape.Check(g); err != nil {
		d.warn("approximated", fmt.Sprintf("a shape's geometry is drawn in part: %v", err))
	}
}

// vectorShape returns the replaced box of w by h points drawing the
// geometry g, nil for a rectangle, with the fill and outline ss.
func vectorShape(st *css.Style, g *shape.Geometry, ss ooxml.ShapeStyle, w, h float64) *layout.Node {
	ps := inlineStyle(st)
	ps.Width, ps.Height = css.Pts(max(w, 0.1)), css.Pts(max(h, 0.1))
	ps.LineHeight = css.Dimension{Kind: css.Normal}
	pic := &layout.Picture{Width: max(w, 0.1), Height: max(h, 0.1)}
	pic.Vector = func(_ context.Context, r compose.Rect) []compose.Fragment {
		return ss.Paint(g, r)
	}
	return &layout.Node{Kind: layout.Replaced, Style: ps, Picture: pic}
}

// bodyVert maps a text box's bodyPr vert (20.1.10.83 ST_TextVerticalType)
// onto a text direction: vert270 reads bottom to top, the vertical types
// top to bottom, horz none.
func bodyVert(v string) string {
	switch v {
	case "", "horz":
		return ""
	case "vert270":
		return "btLr"
	}
	return "tbRl"
}

// textDirection maps a w:textDirection value (17.18.93) onto the two
// directions [setWritingMode] reads: the transitional names btLr and
// tbRl, and the strict ones, tbV and lrV among them, as Word writes them.
func textDirection(v string) string {
	switch v {
	case "btLr", "lr", "lrV":
		return "btLr"
	}
	return "tbRl"
}

// turn sets the writing mode of a text box's block n for the text
// direction dir, "" for horizontal text.
func turn(n *layout.Node, dir string) {
	if dir == "" || n == nil {
		return
	}
	setWritingMode(n.Style, dir)
}

// bodyInsets returns the insets of a text box's body properties, top,
// right, bottom, left (20.1.2.1.1 bodyPr: 0.1 inch left and right, 0.05
// inch top and bottom by default).
func bodyInsets(bodyPr *ooxml.Element) [4]float64 {
	insets := [4]float64{3.6, 7.2, 3.6, 7.2}
	for side, name := range map[css.Side]string{css.Top: "tIns", css.Right: "rIns", css.Bottom: "bIns", css.Left: "lIns"} {
		if v, ok := ooxml.EMU(attrOr(bodyPr, name)); ok {
			insets[side] = max(0, v)
		}
	}
	return insets
}

// shapeTextBox gives the text box n, built by [document.textBox] at w by
// h points, the geometry g of its shape and the vertical anchor of its
// text (ECMA-376 Part 1, 20.1.2.1.1 bodyPr anchor; 20.1.10.59
// ST_TextAnchoringType t, ctr, b): a shape other than a rectangle draws
// behind the text with the fill and outline ss, which the box no longer
// paints, and the text sits at the top, the middle or the bottom of the
// box's inset area, as Word places it. A box that grows to fit its text
// (a:spAutoFit) keeps its text at the top.
//
// # Corpus evidence
//
//   - libreoffice/docx ooxmlexport/ColorOverwritten: Word draws a text box
//     of preset rightArrow as the arrow, its text centred in it; drawn so,
//     layout rises from 0.55 to 0.78.
//   - libreoffice/docx ooxmlexport/fdo77719: layout rises from 0.26 to 0.75.
func (d *document) shapeTextBox(n *layout.Node, g *shape.Geometry, ss ooxml.ShapeStyle, w, h float64, anchor string, autoFit bool) {
	rect := g == nil || (g.Custom == nil && (g.Preset == "" || g.Preset == "rect"))
	if autoFit || (anchor != "ctr" && anchor != "b") {
		anchor = ""
	}
	if (rect || !shape.Known(g)) && anchor == "" {
		return
	}
	bs := n.Style
	bs.Position = css.Relative
	kids := n.Children
	if anchor != "" {
		bs.MinHeight = css.Pts(0)
		bs.Height = css.Pts(max(h, 1))
		area := d.blockStyle()
		area.Display = css.Flex
		area.FlexDirection = css.FlexColumn
		area.Height = css.Dimension{Kind: css.Percent, Value: 1}
		// Positioned, the text paints over the shape before it (CSS 2.1,
		// Appendix E).
		area.Position = css.Relative
		area.JustifyContent = css.BoxCenter
		if anchor == "b" {
			area.JustifyContent = css.BoxFlexEnd
		}
		inner := d.blockStyle()
		kids = []*layout.Node{{Kind: layout.Element, Style: area, Children: []*layout.Node{{Kind: layout.Element, Style: inner, Children: kids}}}}
	}
	if !rect && shape.Known(g) {
		bs.BackgroundColor = css.Transparent
		for i := range bs.Border {
			bs.Border[i] = css.Border{}
		}
		bs.StrokeBorders = false
		pic := vectorShape(bs, g, ss, w, h)
		ps := pic.Style
		ps.Position = css.Absolute
		ps.Display = css.Block
		ps.MinHeight = css.Pts(0)
		ps.Left, ps.Top = css.Pts(0), css.Pts(0)
		ps.Right, ps.Bottom = css.Dimension{Kind: css.Auto}, css.Dimension{Kind: css.Auto}
		if anchor == "" {
			// Positioned, the text paints over the shape before it.
			inner := d.blockStyle()
			inner.Position = css.Relative
			kids = []*layout.Node{{Kind: layout.Element, Style: inner, Children: kids}}
		}
		kids = append([]*layout.Node{pic}, kids...)
	}
	n.Children = kids
}

// textBox builds a text box of w by h points: an inline block holding the
// content's blocks, with the shape's fill and outline and the insets,
// top, right, bottom and left.
//
// A text box with content is a Figure: it is anchored in a paragraph and
// lies inside that paragraph's P, which ISO 32005, Table 5 lets hold a
// Figure but not the P and Div elements of the box's content, while a
// Figure holds them. Its alt is alt, the description of its drawing,
// else the text it shows, so that a reader hears the box either way
// (ISO 14289-1, 7.3).
func (d *document) textBox(s *story, content *ooxml.Element, w, h float64, ss ooxml.ShapeStyle, insets [4]float64, alt string, depth int) *layout.Node {
	bs := d.blockStyle()
	bs.Display = css.InlineBlock
	bs.Width = css.Pts(max(w, 1))
	bs.MinHeight = css.Pts(max(h, 0))
	bs.BoxSizing = css.BorderBox
	bs.LineHeight = css.Dimension{Kind: css.Normal}
	bs.VerticalAlign = css.VerticalAlign{Kind: css.AlignTop}
	for i := range insets {
		bs.Padding[i] = css.Pts(insets[i])
	}
	if ss.Fill != nil {
		bs.BackgroundColor = *ss.Fill
	}
	if ss.Line != nil && ss.LineWidth > 0 {
		for i := range bs.Border {
			bs.Border[i] = css.Border{Width: ss.LineWidth, Style: css.Solid, Color: *ss.Line}
		}
		strokeBorders(bs)
	}
	var kids []*layout.Node
	if content != nil {
		ts := &story{part: s.part, fields: s.fields, note: s.note}
		d.nested++
		kids = finish(d.blocks(ts, nil, content, depth+1))
		d.nested--
	}
	n := &layout.Node{Kind: layout.Element, Style: bs, Children: kids}
	if len(kids) > 0 {
		n.Role = "Figure"
		if n.Alt = alt; alt == "" {
			n.Alt = office.ShownText(kids)
		}
	}
	return n
}
