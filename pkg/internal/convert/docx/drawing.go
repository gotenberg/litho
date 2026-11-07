package docx

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
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
		alt := ""
		if dp := k.Child(ooxml.NSWP, "docPr"); dp != nil {
			alt = attrOr(dp, "descr")
			if alt == "" {
				alt = attrOr(dp, "title")
			}
		}
		data := k.Child(ooxml.NSA, "graphic").Child(ooxml.NSA, "graphicData")
		n := d.graphic(s, data, st, max(wpt, 0), max(hpt, 0), alt, depth)
		if n == nil {
			continue
		}
		if !inline {
			d.anchor(k, n, sink)
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
			return d.shape(s, sp, st, w, h, depth)
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
	return &layout.Node{Kind: layout.Replaced, Style: ps, Picture: pic, Role: "Figure", Alt: alt}
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
	if pic.Source != nil && pic.Source.Image != nil {
		b := pic.Source.Image.Bounds()
		d.pixels += int64(b.Dx()) * int64(b.Dy())
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
// of its preset geometry.
func (d *document) shape(s *story, sp *ooxml.Element, st *css.Style, w, h float64, depth int) *layout.Node {
	spPr := sp.Child(ooxml.NSWPS, "spPr")
	ss := ooxml.ParseShapeStyle(spPr, sp.Child(ooxml.NSWPS, "style"), d.theme)
	geom := "rect"
	if g := spPr.Child(ooxml.NSA, "prstGeom"); g != nil {
		geom = attrOr(g, "prst")
	} else if spPr.Child(ooxml.NSA, "custGeom") != nil {
		geom = "custom"
	}
	if ref := ooxml.ParsePicture(sp); ref.Embed != "" && spPr.Child(ooxml.NSA, "blipFill") != nil {
		return d.picture(s, ref, st, w, h, "")
	}
	if tb := sp.Child(ooxml.NSWPS, "txbx"); tb != nil {
		return d.textBox(s, tb.Child(ooxml.NSW, "txbxContent"), w, h, ss, sp.Child(ooxml.NSWPS, "bodyPr"), depth)
	}
	if _, ok := ooxml.PresetPath(geom, compose.Rect{Width: 1, Height: 1}); !ok {
		d.warn("approximated", fmt.Sprintf("a %s shape is drawn as its bounding box", geom))
	}
	ps := inlineStyle(st)
	ps.Width, ps.Height = css.Pts(max(w, 0.1)), css.Pts(max(h, 0.1))
	ps.LineHeight = css.Dimension{Kind: css.Normal}
	if xfrm := spPr.Child(ooxml.NSA, "xfrm"); xfrm != nil {
		rot, fh, fv := ooxml.Transform2D(xfrm)
		setTransform(ps, rot, fh, fv)
	}
	pic := &layout.Picture{Width: max(w, 0.1), Height: max(h, 0.1)}
	pic.Vector = func(_ context.Context, r compose.Rect) []compose.Fragment {
		ops, _ := ooxml.PresetPath(geom, r)
		return ss.Paint(ops)
	}
	return &layout.Node{Kind: layout.Replaced, Style: ps, Picture: pic}
}

// textBox builds a text box of w by h points: an inline block holding the
// content's blocks, with the shape's fill and outline and the insets of
// its body properties (20.1.2.1.1 bodyPr: 0.1 inch left and right, 0.05
// inch top and bottom by default).
func (d *document) textBox(s *story, content *ooxml.Element, w, h float64, ss ooxml.ShapeStyle, bodyPr *ooxml.Element, depth int) *layout.Node {
	bs := d.blockStyle()
	bs.Display = css.InlineBlock
	bs.Width = css.Pts(max(w, 1))
	bs.MinHeight = css.Pts(max(h, 0))
	bs.BoxSizing = css.BorderBox
	bs.LineHeight = css.Dimension{Kind: css.Normal}
	bs.VerticalAlign = css.VerticalAlign{Kind: css.AlignTop}
	insets := [4]float64{3.6, 7.2, 3.6, 7.2}
	for side, name := range map[css.Side]string{css.Top: "tIns", css.Right: "rIns", css.Bottom: "bIns", css.Left: "lIns"} {
		if v, ok := ooxml.EMU(attrOr(bodyPr, name)); ok {
			insets[side] = max(0, v)
		}
	}
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
	}
	var kids []*layout.Node
	if content != nil {
		ts := &story{part: s.part, fields: s.fields, note: s.note}
		kids = finish(d.blocks(ts, nil, content, depth+1))
	}
	return &layout.Node{Kind: layout.Element, Style: bs, Children: kids}
}
