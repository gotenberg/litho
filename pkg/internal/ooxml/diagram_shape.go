package ooxml

import (
	"encoding/xml"
	"math"
	"strconv"
)

// Element returns the laid out shape s as the shape element a diagram's
// saved drawing holds (MS-ODRAWXML, 2.4 dsp:sp), in the namespace space:
// its spPr with the preset geometry, unless a rectangle, and the fill and
// line of its data when draws is set; its style, each style matrix
// reference carrying the colour the colour definition gives it; and its
// text as [DiagramShape.TextBody] returns it.
func (s *DiagramShape) Element(space, preset string, draws bool) *Element {
	sp := &Element{Space: space, Local: "sp"}
	pr := &Element{Space: space, Local: "spPr"}
	if draws && preset != "rect" {
		pr.Children = append(pr.Children, newA("prstGeom", []string{"prst", preset}, newA("avLst", nil)))
	}
	if draws {
		if f := fillChild(s.FillSpPr); f != nil {
			pr.Children = append(pr.Children, f)
		}
		if ln := s.LineSpPr.Child(NSA, "ln"); ln != nil {
			pr.Children = append(pr.Children, ln)
		}
	}
	sp.Children = append(sp.Children, pr)
	style := &Element{Space: space, Local: "style"}
	for _, ref := range []struct {
		ref, col *Element
		local    string
	}{{s.Style.Line, s.StyleColor.Line, "lnRef"}, {s.Style.Fill, s.StyleColor.Fill, "fillRef"}, {s.Style.Effect, s.StyleColor.Effect, "effectRef"}, {s.Style.Font, s.StyleColor.Font, "fontRef"}} {
		if ref.ref == nil {
			continue
		}
		e := &Element{Space: NSA, Local: ref.local, Attrs: ref.ref.Attrs, Children: ref.ref.Children}
		if ref.col != nil {
			e.Children = []*Element{ref.col}
		}
		style.Children = append(style.Children, e)
	}
	sp.Children = append(sp.Children, style)
	if tb := s.TextBody(space); tb != nil {
		sp.Children = append(sp.Children, tb)
	}
	return sp
}

// fillChild returns the fill among e's children, nil for none.
func fillChild(e *Element) *Element {
	for _, k := range e.Kids() {
		if k.Space != NSA {
			continue
		}
		switch k.Local {
		case "noFill", "solidFill", "gradFill", "blipFill", "pattFill", "grpFill":
			return k
		}
	}
	return nil
}

// newA returns a new DrawingML element with the attributes of the name and
// value pairs kv and the children kids.
func newA(local string, kv []string, kids ...*Element) *Element {
	return &Element{Space: NSA, Local: local, Attrs: attrList(kv...), Children: kids}
}

// attrList returns the attributes of the name and value pairs kv.
func attrList(kv ...string) []xml.Attr {
	var out []xml.Attr
	for i := 0; i+1 < len(kv); i += 2 {
		out = append(out, xml.Attr{Name: xml.Name{Local: kv[i]}, Value: kv[i+1]})
	}
	return out
}

// withAttrSet returns list with the attribute name set to v.
func withAttrSet(list []xml.Attr, name, v string) []xml.Attr {
	for i := range list {
		if list[i].Name.Local == name {
			list[i].Value = v
			return list
		}
	}
	return append(list, attrList(name, v)...)
}

// pPrWith returns a copy of the paragraph properties e, a new a:pPr when
// nil, with the attribute name set to v.
func pPrWith(e *Element, name, v string) *Element {
	out := &Element{Space: NSA, Local: "pPr"}
	if e != nil {
		*out = *e
		out.Attrs = append([]xml.Attr(nil), e.Attrs...)
	}
	out.Attrs = withAttrSet(out.Attrs, name, v)
	return out
}

// TextBody returns the shape's text as a txBody in the namespace space:
// its data's body properties with the anchor, insets and autofit the text
// algorithm set, its data's list style, and its paragraphs with the level,
// margin, indent, alignment, spacing and bullet the algorithm set; nil for
// no text.
func (s *DiagramShape) TextBody(space string) *Element {
	if len(s.Paragraphs) == 0 {
		return nil
	}
	body := &Element{Space: NSA, Local: "bodyPr"}
	// Text the sizing gave a size is set at it, as PowerPoint saves the
	// text it sizes: with noAutofit.
	sized := !s.Autofit && s.FontSize > 0 && s.text
	if s.BodyPr != nil {
		body.Attrs = append([]xml.Attr(nil), s.BodyPr.Attrs...)
		for _, k := range s.BodyPr.Kids() {
			if (s.Autofit || sized) && (k.Local == "normAutofit" || k.Local == "spAutoFit" || k.Local == "noAutofit") {
				continue
			}
			body.Children = append(body.Children, k)
		}
	}
	if s.Anchor != "" {
		body.Attrs = withAttrSet(body.Attrs, "anchor", s.Anchor)
	}
	for i, name := range [4]string{"lIns", "tIns", "rIns", "bIns"} {
		if s.Inset[i] {
			body.Attrs = withAttrSet(body.Attrs, name, strconv.FormatInt(s.Insets[i], 10))
		}
	}
	switch {
	case s.Autofit:
		body.Children = append(body.Children, newA("normAutofit", nil))
	case sized:
		body.Children = append(body.Children, newA("noAutofit", nil))
	}
	tb := &Element{Space: space, Local: "txBody", Children: []*Element{body}}
	if s.ListStyle != nil {
		tb.Children = append(tb.Children, s.ListStyle)
	}
	for _, p := range s.Paragraphs {
		ppr := pPrWith(p.PPr, "lvl", strconv.Itoa(max(0, min(p.Level, 8))))
		if p.HasMarL {
			ppr.Attrs = withAttrSet(ppr.Attrs, "marL", strconv.FormatInt(p.MarL, 10))
		}
		if p.HasIndent {
			ppr.Attrs = withAttrSet(ppr.Attrs, "indent", strconv.FormatInt(p.Indent, 10))
		}
		if p.Align != "" {
			ppr.Attrs = withAttrSet(ppr.Attrs, "algn", p.Align)
		}
		// 21.1.2.2.7 pPr: lnSpc and spcAft come first among its children,
		// before the bullet properties.
		var spacing []*Element
		if p.LineSpacing > 0 {
			spacing = append(spacing, newA("lnSpc", nil, newA("spcPct", []string{"val", strconv.FormatInt(p.LineSpacing, 10)})))
		}
		if p.SpaceAfter > 0 {
			spacing = append(spacing, newA("spcAft", nil, newA("spcPct", []string{"val", strconv.FormatInt(p.SpaceAfter, 10)})))
		}
		if len(spacing) > 0 {
			ppr.Children = append(spacing, ppr.Children...)
		}
		if p.Bullet {
			var kids []*Element
			for _, k := range ppr.Children {
				switch k.Local {
				case "buNone", "buChar", "buAutoNum", "buBlip":
					continue
				}
				kids = append(kids, k)
			}
			kids = append(kids, newA("buChar", []string{"char", "•"}))
			ppr.Children = kids
		} else if ppr.Child(NSA, "buChar") == nil && ppr.Child(NSA, "buAutoNum") == nil && ppr.Child(NSA, "buBlip") == nil {
			ppr.Children = append(append([]*Element(nil), ppr.Children...), newA("buNone", nil))
		}
		out := &Element{Space: NSA, Local: "p", Children: []*Element{ppr}}
		for _, k := range p.Source.Kids() {
			if k.Space != NSA || (k.Local != "r" && k.Local != "br" && k.Local != "fld") {
				continue
			}
			if size := s.FontSize; size > 0 {
				if p.Level > 0 && s.ChildFontSize > 0 {
					size = s.ChildFontSize
				}
				k = sizedRun(k, size)
			}
			out.Children = append(out.Children, k)
		}
		tb.Children = append(tb.Children, out)
	}
	return tb
}

// sizedRun returns a run whose rPr sets no size with the size pt.
func sizedRun(run *Element, pt float64) *Element {
	rpr := run.Child(NSA, "rPr")
	if _, ok := rpr.Attr("sz"); ok {
		return run
	}
	out := *run
	out.Children = nil
	nr := &Element{Space: NSA, Local: "rPr"}
	if rpr != nil {
		*nr = *rpr
		nr.Attrs = append([]xml.Attr(nil), rpr.Attrs...)
	}
	nr.Attrs = withAttrSet(nr.Attrs, "sz", strconv.Itoa(int(math.Round(pt*100))))
	out.Children = append(out.Children, nr)
	for _, k := range run.Children {
		if k != rpr {
			out.Children = append(out.Children, k)
		}
	}
	return &out
}
