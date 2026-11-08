package pptx

import (
	"errors"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// asPicture returns a p:pic (or a diagram drawing's) as the DrawingML
// picture element it mirrors (ECMA-376 Part 1, 19.3.1.37 pic, whose
// blipFill and spPr are 20.2.2.5 pic:pic's), so ooxml.ParsePicture finds
// its parts.
func asPicture(el *ooxml.Element) *ooxml.Element {
	out := &ooxml.Element{Space: ooxml.NSPic, Local: "pic"}
	for _, c := range el.Kids() {
		if c.Space == el.Space {
			alias := *c
			alias.Space = ooxml.NSPic
			if alias.Local == "nvPicPr" {
				alias.Children = nil
				for _, n := range c.Kids() {
					if n.Space == el.Space {
						a := *n
						a.Space = ooxml.NSPic
						n = &a
					}
					alias.Children = append(alias.Children, n)
				}
			}
			c = &alias
		}
		out.Children = append(out.Children, c)
	}
	return out
}

// picture reads a p:pic (19.3.1.37): the picture of its blip, its SVG
// rendition first, cropped by its srcRect, placed by its own or its
// placeholder's transform. A picture with a geometry other than a
// rectangle, or with an outline, is a geometry filled with the picture.
func (r *reader) picture(c *shapeCtx, el *ooxml.Element) *slides.Shape {
	chain := inherited(c.layer, el)
	nv := nvProps(el)
	s := &slides.Shape{Kind: slides.KindPicture, Name: attr(nv, "name"), Href: r.href(c.part, nv.Child(ooxml.NSA, "hlinkClick")), Jump: r.jump(c.part, nv.Child(ooxml.NSA, "hlinkClick"))}
	if !place(s, chain) {
		return nil
	}
	if media := mediaOf(el); media != "" {
		r.warn("unsupported-content", fmt.Sprintf("%s does not play in a printout: its poster frame stands in", media))
	}
	ref := ooxml.ParsePicture(asPicture(el))
	pic := r.pictureOf(c.part, ref)
	if pic == nil {
		return nil
	}
	pic = ooxml.Cropped(pic, ref.Crop)
	style := styleRefs(chain)
	fc := &fillCtx{sc: c.sc, part: c.part, group: c.group}
	s.Line = r.shapeLine(fc, chain, style)
	s.Geometry = r.geometryOf(chain)
	r.effects(chain, s)
	r.effectList(chain, style, fc, s)
	if s.Geometry != nil || s.Line.Width > 0 {
		s.Kind = slides.KindGeometry
		s.Fill = slides.Fill{Kind: slides.FillPicture, Picture: pic}
		return s
	}
	s.Picture = pic
	return s
}

// mediaOf names the media a picture stands in for (19.3.1.x the nvPr's
// a:videoFile, a:audioFile, a:quickTimeFile, a:wavAudioFile, a:audioCd),
// "" for none.
func mediaOf(el *ooxml.Element) string {
	for _, nv := range el.Kids() {
		pr := kid(nv, "nvPr")
		if pr == nil {
			continue
		}
		for _, k := range pr.Kids() {
			switch k.Local {
			case "videoFile", "quickTimeFile":
				return "a video"
			case "audioFile", "wavAudioFile", "audioCd":
				return "a sound"
			}
		}
	}
	return ""
}

// pictureOf returns the picture a picture reference names: its SVG
// rendition when it has one and it decodes, else its raster or vector
// blip; nil with a warning for a linked or undecodable one.
func (r *reader) pictureOf(part string, ref ooxml.PictureRef) *layout.Picture {
	var pic *layout.Picture
	if ref.SVG != "" {
		pic = r.loadPicture(part, ref.SVG)
	}
	if pic == nil && ref.Embed != "" {
		pic = r.loadPicture(part, ref.Embed)
	}
	if pic == nil && ref.Link != "" && ref.Embed == "" {
		r.warn("unsupported-content", "a linked picture is left out: linked files are not read")
	}
	return pic
}

// loadPicture decodes the picture the relationship id of part names, once
// per target, nil with a warning when it cannot be drawn.
func (r *reader) loadPicture(part, id string) *layout.Picture {
	rel, ok := r.pkg.RelationshipByID(part, id)
	if !ok || rel.External {
		if ok {
			r.warn("unsupported-content", "a linked picture is left out: linked files are not read")
		}
		return nil
	}
	if pic, ok := r.pictures[rel.Target]; ok {
		return pic
	}
	r.pictures[rel.Target] = nil
	data, err := r.pkg.Read(rel.Target)
	if err != nil {
		r.warn("missing-picture", fmt.Sprintf("picture %s cannot be read: %v", rel.Target, err))
		return nil
	}
	pic, err := ooxml.DecodePicture(r.ctx, data, r.fonts)
	if err != nil {
		if errors.Is(err, ooxml.ErrUnsupportedPicture) {
			r.warn("unsupported-picture", fmt.Sprintf("picture %s is left out: %v", rel.Target, err))
		} else {
			r.warn("unsupported-picture", fmt.Sprintf("picture %s does not decode: %v", rel.Target, err))
		}
		return nil
	}
	if pic.Source != nil && pic.Source.Image != nil {
		b := pic.Source.Image.Bounds()
		r.pixels += int64(b.Dx()) * int64(b.Dy())
		if r.pixels > maxTotalPixels {
			r.warn("too-large", "pictures past the conversion's pixel budget are left out")
			return nil
		}
	}
	r.pictures[rel.Target] = pic
	return pic
}

// graphicFrame reads a p:graphicFrame (19.3.1.21): a table, a chart, a
// SmartArt diagram drawn from its drawing part, or an OLE object drawn
// from its fallback picture.
func (r *reader) graphicFrame(c *shapeCtx, el *ooxml.Element, depth int) (*slides.Shape, error) {
	chain := inherited(c.layer, el)
	nv := nvProps(el)
	s := &slides.Shape{Name: attr(nv, "name"), Href: r.href(c.part, nv.Child(ooxml.NSA, "hlinkClick")), Jump: r.jump(c.part, nv.Child(ooxml.NSA, "hlinkClick"))}
	if !place(s, chain) {
		return nil, nil
	}
	data := el.Child(ooxml.NSA, "graphic").Child(ooxml.NSA, "graphicData")
	switch attr(data, "uri") {
	case uriTable:
		t := r.table(c, data.Child(ooxml.NSA, "tbl"), s.Box)
		if t == nil {
			return nil, nil
		}
		s.Kind, s.Table = slides.KindTable, t
		r.effectList([]*ooxml.Element{data.Child(ooxml.NSA, "tbl").Child(ooxml.NSA, "tblPr")}, nil, &fillCtx{sc: c.sc, part: c.part, group: c.group}, s)
	case uriChart:
		id, _ := data.Child(ooxml.NSChart, "chart").AttrNS(ooxml.NSR, "id")
		rel, ok := r.pkg.RelationshipByID(c.part, id)
		if !ok || rel.External || !r.pkg.Has(rel.Target) {
			r.warn("malformed-part", "a chart whose part is missing is left out")
			return nil, nil
		}
		ch, err := ooxml.ParseChart(r.ctx, r.pkg, rel.Target)
		if err != nil {
			if ctxErr := r.ctx.Err(); ctxErr != nil {
				return nil, ctxErr
			}
			r.warn("chart-malformed", fmt.Sprintf("chart %s does not parse and is left out: %v", rel.Target, err))
			return nil, nil
		}
		for _, w := range ch.Warnings {
			r.warn(w.Code, w.Message)
		}
		s.Kind, s.Chart = slides.KindChart, ch
	case uriDiagram:
		return r.diagram(c, data, s, depth)
	case uriChartEx:
		// The reader takes the picture PowerPoint saves in the Fallback
		// of the cx1 and cx2 Choice it does not understand; a chart of
		// this kind outside one is left out until a reader of its part
		// exists.
		r.warn("unsupported-content", "an Office 2016 chart (funnel, waterfall, sunburst, treemap, histogram, box and whisker) without a fallback picture is left out")
		return nil, nil
	case uriOLE:
		ole := data.Child(nsP, "oleObj")
		pic := ole.Child(nsP, "pic")
		if pic == nil {
			r.warn("unsupported-content", "an embedded object without a picture of it is left out")
			return nil, nil
		}
		ref := ooxml.ParsePicture(asPicture(pic))
		p := r.pictureOf(c.part, ref)
		if p == nil {
			return nil, nil
		}
		s.Kind, s.Picture = slides.KindPicture, ooxml.Cropped(p, ref.Crop)
	default:
		r.warn("unsupported-content", "a graphic frame of an unknown kind is left out")
		return nil, nil
	}
	return s, nil
}
