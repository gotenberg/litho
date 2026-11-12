package docx

import (
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// The graphic data URI and namespace of a drawing canvas (MS-ODRAWXML,
// 2.3.4.2 wpc:wpc).
const (
	uriCanvas = "http://schemas.microsoft.com/office/word/2010/wordprocessingCanvas"
	nsWPC     = uriCanvas
)

// canvas builds a wpc:wpc (MS-ODRAWXML, 2.3.4.2) of w by h points: an
// inline block painted with the canvas's background (wpc:bg) and outline
// (wpc:whole), holding its shapes, pictures and groups at their
// transforms' offsets, which are in the canvas's own coordinates.
//
// # Corpus evidence
//
//   - litho/docx shapes-groups-canvas: Word prints the canvas's F2F2F2
//     background under its grey outline, its two flowchart boxes at their
//     offsets from the canvas's top left, and the elbow connector between
//     them.
func (d *document) canvas(s *story, c *ooxml.Element, st *css.Style, w, h float64, depth int) *layout.Node {
	gs := inlineStyle(st)
	gs.Display = css.InlineBlock
	gs.Width, gs.Height = css.Pts(w), css.Pts(h)
	gs.Position = css.Relative
	gs.LineHeight = css.Dimension{Kind: css.Normal}
	n := &layout.Node{Kind: layout.Element, Style: gs}
	if depth > maxNesting {
		return n
	}
	// The background and the outline: a rectangle of the canvas's size.
	bg := &ooxml.Element{Space: ooxml.NSA, Local: "spPr"}
	if f := c.Child(nsWPC, "bg"); f != nil {
		bg.Children = append(bg.Children, f.Kids()...)
	}
	if ln := c.Child(nsWPC, "whole").Child(ooxml.NSA, "ln"); ln != nil {
		bg.Children = append(bg.Children, ln)
	} else {
		bg.Children = append(bg.Children, &ooxml.Element{Space: ooxml.NSA, Local: "ln", Children: []*ooxml.Element{{Space: ooxml.NSA, Local: "noFill"}}})
	}
	if ss := ooxml.ParseShapeStyle(bg, nil, d.theme); ss.Fill != nil || ss.Gradient != nil || ss.Line != nil {
		n.Children = append(n.Children, absolute(vectorShape(st, nil, ss, w, h), 0, 0))
	}
	for _, k := range c.Kids() {
		var xfrm *ooxml.Element
		switch {
		case k.Is(ooxml.NSWPS, "wsp"):
			xfrm = k.Child(ooxml.NSWPS, "spPr").Child(ooxml.NSA, "xfrm")
		case k.Is(ooxml.NSPic, "pic"):
			xfrm = k.Child(ooxml.NSPic, "spPr").Child(ooxml.NSA, "xfrm")
		case k.Is(ooxml.NSWPG, "wgp"):
			xfrm = k.Child(ooxml.NSWPG, "grpSpPr").Child(ooxml.NSA, "xfrm")
		default:
			continue
		}
		b := xfrmBox(xfrm)
		var kid *layout.Node
		switch {
		case k.Is(ooxml.NSWPS, "wsp"):
			kid = d.shape(s, k, st, b[2], b[3], "", depth+1)
		case k.Is(ooxml.NSPic, "pic"):
			kid = d.picture(s, ooxml.ParsePicture(k), st, b[2], b[3], "")
		default:
			kid = d.group(s, k, st, b[2], b[3], depth+1)
		}
		if kid != nil {
			n.Children = append(n.Children, absolute(kid, b[0], b[1]))
		}
	}
	return n
}
