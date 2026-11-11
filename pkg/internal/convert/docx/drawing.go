package docx

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
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
)

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
		d.warn("unsupported-content", "a chart is left out: the document holds no picture of it")
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
	if pic.Source != nil && len(pic.Source.JPEG) == 0 {
		d.pixels += int64(pic.Source.Width) * int64(pic.Source.Height)
		if d.pixels > maxTotalPixels {
			d.warn("too-large", "pictures past the conversion's pixel budget are left out")
			return nil
		}
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
			n.Alt = shownText(kids)
		}
	}
	return n
}

// maxAltText caps the characters of the text a text box's alt repeats.
// Anti-DoS guard, not spec-mandated: a text box shows a few paragraphs,
// and a reader is told the first few thousand characters of a longer
// one.
const maxAltText = 4096

// shownText returns the text of nodes, a space between blocks, at most
// [maxAltText] bytes cut at a character.
func shownText(nodes []*layout.Node) string {
	var sb strings.Builder
	stack := slices.Clone(nodes)
	slices.Reverse(stack)
	for len(stack) > 0 && sb.Len() < maxAltText {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		switch {
		case n == nil:
		case n.Kind == layout.Text:
			sb.WriteString(n.Text)
		case n.Kind == layout.LineBreak:
			sb.WriteByte(' ')
		default:
			if n.Style != nil && n.Style.Display != css.Inline && sb.Len() > 0 {
				sb.WriteByte(' ')
			}
			for i := len(n.Children) - 1; i >= 0; i-- {
				stack = append(stack, n.Children[i])
			}
		}
	}
	out := strings.Join(strings.Fields(sb.String()), " ")
	for len(out) > maxAltText {
		_, size := utf8.DecodeLastRuneInString(out)
		out = out[:len(out)-size]
	}
	return out
}
