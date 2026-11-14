package docx

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/gotenberg/litho/pkg/internal/chart"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/shape"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
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
	rel, ok := d.pkg.RelationshipByID(d.ctx, s.part, id)
	if !ok || rel.External || !d.pkg.Has(rel.Target) {
		d.warn(warn.PartMalformed, warn.Omitted, "a chart whose part is missing is left out")
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
		if d.stop(err) {
			return nil
		}
		d.warn(warn.PartMalformed, warn.Omitted, fmt.Sprintf("chart %s does not parse and is left out: %v", rel.Target, err))
		return nil
	}
	for _, cw := range c.Warnings {
		d.warn(cw.Code, cw.Effect, cw.Message)
	}
	ps := inlineStyle(st)
	ps.Display = css.Inline
	ps.Width, ps.Height = css.Pts(w), css.Pts(h)
	ps.VerticalAlign = css.VerticalAlign{}
	ps.LineHeight = css.Dimension{Kind: css.Normal}
	ps.MinLineHeight = 0
	fonts := d.fonts
	pic := &layout.Picture{Width: w, Height: h}
	pic.Vector = func(ctx context.Context, r typeset.Rect) []typeset.Fragment {
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
	switch ooxml.GraphicURI(data) {
	case uriPicture:
		if pic := data.Child(ooxml.NSPic, "pic"); pic != nil {
			n := d.picture(s, ooxml.ParsePicture(pic), st, w, h, alt)
			if n != nil {
				n.Picture = d.withEffects(n.Picture, d.effectList(pic.Child(ooxml.NSPic, "spPr"), nil))
			}
			return n
		}
	case uriShape:
		if sp := data.Child(ooxml.NSWPS, "wsp"); sp != nil {
			return d.shape(s, sp, st, w, h, alt, depth)
		}
	case uriGroup:
		if g := data.Child(ooxml.NSWPG, "wgp"); g != nil {
			return d.group(s, g, st, w, h, depth)
		}
	case uriCanvas:
		if c := data.Child(nsWPC, "wpc"); c != nil {
			return d.canvas(s, c, st, w, h, depth)
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
		return d.diagram(s, data, st, w, h, depth)
	default:
		d.warn(warn.ContentUnsupported, warn.Omitted, "a drawing of an unknown kind is left out")
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
		d.warn(warn.PictureMissing, warn.Omitted, "a linked picture is left out: linked files are not read")
	}
	if pic == nil {
		if w > 0 && h > 0 {
			return box(st, w, h)
		}
		return nil
	}
	pic = ooxml.Cropped(d.adjustLum(pic, ref.Bright, ref.Contrast), ref.Crop)
	if pic == nil {
		return nil
	}
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
	rel, ok := d.pkg.RelationshipByID(d.ctx, part, id)
	if !ok || rel.External {
		if ok {
			d.warn(warn.PictureMissing, warn.Omitted, "a linked picture is left out: linked files are not read")
		}
		return nil
	}
	if pic, ok := d.pictures[rel.Target]; ok {
		return pic
	}
	if len(d.pictures) >= limits.MaxConvertPictures {
		d.warn(warn.LimitReached, warn.Omitted, fmt.Sprintf("pictures past %d are not drawn", limits.MaxConvertPictures))
		return nil
	}
	d.pictures[rel.Target] = nil
	data, err := d.pkg.Read(rel.Target)
	if err != nil {
		if d.stop(err) {
			return nil
		}
		d.warn(warn.PictureMissing, warn.Omitted, fmt.Sprintf("picture %s cannot be read: %v", rel.Target, err))
		return nil
	}
	pic, err := ooxml.DecodePicture(d.ctx, data, d.fonts, rel.Target)
	if err != nil {
		switch {
		case d.stop(err):
		case errors.Is(err, ooxml.ErrUnsupportedPicture):
			d.warn(warn.PictureFormatUnsupported, warn.Omitted, fmt.Sprintf("picture %s is left out: %v", rel.Target, err))
		default:
			d.warn(warn.PictureUnreadable, warn.Omitted, fmt.Sprintf("picture %s does not decode: %v", rel.Target, err))
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
	fx := d.effectList(spPr, sp.Child(ooxml.NSWPS, "style"))
	if ref := ooxml.ParsePicture(sp); ref.Embed != "" && spPr.Child(ooxml.NSA, "blipFill") != nil && pictureShape(spPr, ss) {
		n := d.picture(s, ref, st, w, h, "")
		if n != nil {
			n.Picture = d.withEffects(n.Picture, fx)
		}
		return n
	}
	d.resolveBlip(s, &ss)
	if tb := sp.Child(ooxml.NSWPS, "txbx"); tb != nil {
		bodyPr := sp.Child(ooxml.NSWPS, "bodyPr")
		n := d.textBox(s, tb.Child(ooxml.NSW, "txbxContent"), w, h, ss, bodyInsets(bodyPr), alt, depth)
		if id := attrOr(tb, "id"); id != "" {
			setChain(n.Style, id, 0, h)
			d.textBoxEffectsOmitted(fx)
		} else if vert := bodyVert(attrOr(bodyPr, "vert")); vert == "" {
			g := ooxml.ParseGeometry(spPr)
			d.checkGeometry(g)
			d.shapeTextBox(n, g, ss, fx, w, h, attrOr(bodyPr, "anchor"), bodyPr.Child(ooxml.NSA, "spAutoFit") != nil)
		} else {
			d.textBoxEffectsOmitted(fx)
		}
		d.turn(n, bodyVert(attrOr(bodyPr, "vert")))
		return n
	}
	if l := sp.Child(ooxml.NSWPS, "linkedTxbx"); l != nil && attrOr(l, "id") != "" {
		d.textBoxEffectsOmitted(fx)
		// A later link of a chain of text boxes holds none of the
		// chain's content, which flows into it from the link before.
		bodyPr := sp.Child(ooxml.NSWPS, "bodyPr")
		n := d.textBox(s, nil, w, h, ss, bodyInsets(bodyPr), "", depth)
		seq, _ := ooxml.Int(attrOr(l, "seq"))
		setChain(n.Style, attrOr(l, "id"), seq, h)
		d.turn(n, bodyVert(attrOr(bodyPr, "vert")))
		return n
	}
	g := ooxml.ParseGeometry(spPr)
	d.checkGeometry(g)
	n := vectorShape(st, g, ss, w, h)
	n.Picture = d.withEffects(n.Picture, fx)
	if xfrm := spPr.Child(ooxml.NSA, "xfrm"); xfrm != nil {
		rot, fh, fv := ooxml.Transform2D(xfrm)
		setTransform(n.Style, rot, fh, fv)
	}
	return n
}

// pictureShape reports whether a shape filled with a picture draws as the
// picture itself: a rectangle without an outline whose picture stretches
// over it. Any other shape draws its geometry filled with the picture
// (20.1.8.14 blipFill), stretched or tiled, under its outline.
//
// # Corpus evidence
//
//   - litho/docx shapes-fills-lines: Word tiles a 16-pixel texture at 12
//     points from the top left of a rectangle with a dotted outline, and
//     stretches a picture over a rounded rectangle clipped to its corners,
//     both under their outlines.
func pictureShape(spPr *ooxml.Element, ss ooxml.ShapeStyle) bool {
	if ss.Line != nil || (ss.Blip != nil && ss.Blip.Tile) {
		return false
	}
	g := ooxml.ParseGeometry(spPr)
	return g == nil || (g.Custom == nil && (g.Preset == "" || g.Preset == "rect"))
}

// resolveBlip loads the picture of a shape's picture fill, when it has
// one, into ss.
func (d *document) resolveBlip(s *story, ss *ooxml.ShapeStyle) {
	if ss.Blip == nil || ss.Blip.Embed == "" {
		return
	}
	if pic := d.loadPicture(s.part, ss.Blip.Embed); pic != nil && pic.Source != nil {
		b := *ss.Blip
		b.Picture, b.Width, b.Height = pic.Source, pic.Width, pic.Height
		ss.Blip = &b
	}
}

// checkGeometry warns of a geometry the engine draws as its bounding box
// or whose guides do not all resolve.
func (d *document) checkGeometry(g *shape.Geometry) {
	if !shape.Known(g) {
		d.warn(warn.ContentUnsupported, warn.Approximated, fmt.Sprintf("a %s shape is drawn as its bounding box", g.Preset))
	} else if err := shape.Check(g); err != nil {
		d.warn(warn.ContentUnsupported, warn.Approximated, fmt.Sprintf("a shape's geometry is drawn in part: %v", err))
	}
}

// vectorShape returns the replaced box of w by h points drawing the
// geometry g with the fill and outline ss.
func vectorShape(st *css.Style, g *shape.Geometry, ss ooxml.ShapeStyle, w, h float64) *layout.Node {
	ps := inlineStyle(st)
	ps.Width, ps.Height = css.Pts(max(w, 0.1)), css.Pts(max(h, 0.1))
	ps.LineHeight = css.Dimension{Kind: css.Normal}
	pic := &layout.Picture{Width: max(w, 0.1), Height: max(h, 0.1)}
	pic.Vector = func(_ context.Context, r typeset.Rect) []typeset.Fragment {
		return ss.Paint(g, r)
	}
	return &layout.Node{Kind: layout.Replaced, Style: ps, Picture: pic}
}

// bodyVert maps a text box's bodyPr vert (20.1.10.83 ST_TextVerticalType)
// onto a text direction: vert270 reads bottom to top, mongolianVert top
// to bottom with its lines flowing left to right, the other vertical
// types top to bottom with their lines flowing right to left, horz none.
func bodyVert(v string) string {
	switch v {
	case "", "horz":
		return ""
	case "vert270":
		return "btLr"
	case "mongolianVert":
		return "tbLrV"
	}
	return "tbRl"
}

// textDirection maps a w:textDirection value (ECMA-376 Part 1, 17.18.93
// ST_TextDirection, and Part 4, 14.11.7 for the transitional names) onto
// the directions [setWritingMode] reads, "" for horizontal lines: lr and
// btLr read bottom to top; rl, rlV, tbRl and tbRlV top to bottom, lines
// flowing right to left; lrV and tbLrV top to bottom, lines flowing left
// to right; tb, tbV, lrTb and lrTbV are horizontal, tbV and lrTbV only
// turning East Asian characters.
//
// # Corpus evidence
//
//   - litho/docx: tables-text-direction-valign prints in Word its lrTbV
//     cell horizontal and its tbLrV cell top to bottom at the cell's left
//     edge, its tbRl cell's line at the right edge.
func textDirection(v string) string {
	switch v {
	case "btLr", "lr":
		return "btLr"
	case "tbLrV", "lrV":
		return "tbLrV"
	case "", "lrTb", "tb", "lrTbV", "tbV":
		return ""
	}
	return "tbRl"
}

// warnTextDirection warns of what a text direction of 17.18.93 draws
// approximately: the turned East Asian characters of tbV and lrTbV are
// drawn upright, and the lines of lrV and tbLrV flow right to left as
// the layout stacks every vertical flow.
func (d *document) warnTextDirection(v string) {
	switch v {
	case "tbV", "lrTbV":
		d.warn(warn.ContentUnsupported, warn.Approximated, "East Asian characters of text direction "+v+" are drawn upright")
	case "lrV", "tbLrV":
		d.warn(warn.ContentUnsupported, warn.Approximated, "vertical text with lines from left to right is laid out as from right to left")
	}
}

// turn sets the writing mode of a text box's block n for the text
// direction dir, "" for horizontal text.
func (d *document) turn(n *layout.Node, dir string) {
	d.warnTextDirection(dir)
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

// textBoxEffectsOmitted warns that the effects fx of a text box are not
// drawn: those of a box of a chain of linked boxes or of vertical text,
// whose layout leaves no shape behind the text to draw them around.
func (d *document) textBoxEffectsOmitted(fx *css.OfficeEffects) {
	if fx != nil {
		d.warn(warn.ContentUnsupported, warn.Omitted, "the effects of a linked or vertical text box are not drawn")
	}
}

// shapeTextBox gives the text box n, built by [document.textBox] at w by
// h points, the geometry g of its shape and the vertical anchor of its
// text (ECMA-376 Part 1, 20.1.2.1.1 bodyPr anchor; 20.1.10.59
// ST_TextAnchoringType t, ctr, b): a shape other than a rectangle, or one
// with the effects fx (20.1.8.26 effectLst), draws behind the text with
// the fill and outline ss and its effects around it, which the box then
// leaves unpainted, and the text sits at the top, the middle or the
// bottom of the box's inset area, as Word places it. A box that grows to
// fit its text (a:spAutoFit) keeps its text at the top; the shape behind
// it keeps the size the file gives.
//
// # Corpus evidence
//
//   - libreoffice/docx ooxmlexport/ColorOverwritten: Word draws a text box
//     of preset rightArrow as the arrow, its text centred in it; drawn so,
//     layout rises from 0.55 to 0.78.
//   - libreoffice/docx ooxmlexport/fdo77719: layout rises from 0.26 to 0.75.
func (d *document) shapeTextBox(n *layout.Node, g *shape.Geometry, ss ooxml.ShapeStyle, fx *css.OfficeEffects, w, h float64, anchor string, autoFit bool) {
	rect := g == nil || (g.Custom == nil && (g.Preset == "" || g.Preset == "rect"))
	if autoFit || (anchor != "ctr" && anchor != "b") {
		anchor = ""
	}
	if fx != nil && (rect || !shape.Known(g)) {
		// The box's own fill and outline give way to a rectangle drawn
		// behind the text, which carries the effects.
		g, rect = &shape.Geometry{Preset: "rect"}, false
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
		// The area's height in points rather than a percentage, which an
		// absolutely positioned box, a group's shape, leaves unresolved.
		inner := h - bs.Padding[css.Top].Value - bs.Padding[css.Bottom].Value
		if rect || !shape.Known(g) {
			inner -= bs.Border[css.Top].Width + bs.Border[css.Bottom].Width
		}
		area.Height = css.Pts(max(inner, 0))
		// Positioned, the text paints over the shape before it (CSS 2.1,
		// Appendix E).
		area.Position = css.Relative
		area.JustifyContent = css.BoxCenter
		if anchor == "b" {
			area.JustifyContent = css.BoxFlexEnd
		}
		kids = []*layout.Node{{Kind: layout.Element, Style: area, Children: []*layout.Node{{Kind: layout.Element, Style: d.blockStyle(), Children: kids}}}}
	}
	if !rect && shape.Known(g) {
		bs.BackgroundColor = css.Transparent
		for i := range bs.Border {
			bs.Border[i] = css.Border{}
		}
		bs.StrokeBorders = false
		pic := vectorShape(bs, g, ss, w, h)
		pic.Picture = d.withEffects(pic.Picture, fx)
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
		kids = d.finish(d.blocks(ts, nil, content, depth+1))
		d.nested--
	}
	n := &layout.Node{Kind: layout.Element, Style: bs, Children: kids}
	if len(kids) > 0 {
		n.Role, n.Alt = "Figure", alt
		if alt == "" {
			n.Alt = office.ShownText(kids)
		}
	}
	return n
}
