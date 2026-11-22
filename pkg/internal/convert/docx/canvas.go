package docx

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/warn"
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

// uriLockedCanvas is the graphic data URI and the namespace of a locked
// canvas (ECMA-376 Part 1, 20.3.2.1 lockedCanvas).
const uriLockedCanvas = "http://schemas.openxmlformats.org/drawingml/2006/lockedCanvas"

// maxCanvasShapes caps the shapes one locked canvas draws. Anti-DoS
// guard, not spec-mandated: a canvas holds the few shapes of a figure,
// far under it.
const maxCanvasShapes = 4096

// canvasSpace maps a group's child coordinates onto the canvas's, in
// points: a point p of the child space lands at (p - off) * scale + at.
type canvasSpace struct {
	off, scale, at [2]float64
}

// point maps the child point x, y onto the canvas.
func (c canvasSpace) point(x, y float64) (float64, float64) {
	return float64((x-c.off[0])*c.scale[0]) + c.at[0], float64((y-c.off[1])*c.scale[1]) + c.at[1]
}

// childSpace returns the space of the children of a group of transform
// xfrm (20.1.7.5 xfrm of a grpSpPr: off and ext in the space c, chOff and
// chExt its children's) inside c.
func (c canvasSpace) childSpace(xfrm *ooxml.Element) canvasSpace {
	b := xfrmBox(xfrm)
	x, y := c.point(b[0], b[1])
	ch := xfrmBox(&ooxml.Element{Children: []*ooxml.Element{
		{Space: ooxml.NSA, Local: "off", Attrs: xfrm.Child(ooxml.NSA, "chOff").Attrs},
		{Space: ooxml.NSA, Local: "ext", Attrs: xfrm.Child(ooxml.NSA, "chExt").Attrs},
	}})
	out := canvasSpace{off: [2]float64{ch[0], ch[1]}, scale: c.scale, at: [2]float64{x, y}}
	if ch[2] > 0 {
		out.scale[0] = min(float64(c.scale[0]*b[2])/ch[2], maxCanvasScale)
	}
	if ch[3] > 0 {
		out.scale[1] = min(float64(c.scale[1]*b[3])/ch[3], maxCanvasScale)
	}
	return out
}

// maxCanvasScale caps the scale of a group's child space onto its
// canvas. Anti-DoS guard, not spec-mandated: nested groups each a
// billion times their child extent would otherwise multiply into
// coordinates no number holds.
const maxCanvasScale = 1 << 20

// lockedCanvas builds an lc:lockedCanvas (ECMA-376 Part 1, 20.3.2.1), a
// DrawingML group whose shapes Word draws but does not let a reader edit,
// as an inline block of w by h points: its shapes (a:sp, a:cxnSp) drawn
// with their geometries, fills and outlines and their text bodies (a:txSp),
// its pictures (a:pic) and its groups (a:grpSp), each placed by its
// transform in the canvas's child coordinates (20.1.7.5 xfrm chOff,
// chExt), scaled with them, their text too, as a picture would be.
//
// # Corpus evidence
//
//   - libreoffice/docx ooxmlimport/strict-lockedcanvas.docx: Word prints
//     the canvas's rectangle and triangle filled with accent 1, at their
//     offsets from the canvas's child origin.
//   - libreoffice/docx ooxmlimport/fdo43641.docx: a canvas of no extent
//     and no child extent prints its group's rectangle at its own size
//     at the canvas's top left corner, the arrow beside it at its offset
//     from the rectangle.
//   - libreoffice/docx ooxmlexport/fdo76249.docx: Word prints the text of
//     a canvas whose child space is 5.8 times its extent at a 5.8th of
//     its size, the picture beside it shrunk alike.
func (d *document) lockedCanvas(s *story, c *ooxml.Element, st *css.Style, w, h float64, depth int) *layout.Node {
	gs := inlineStyle(st)
	gs.Display = css.InlineBlock
	gs.Width, gs.Height = css.Pts(w), css.Pts(h)
	gs.Position = css.Relative
	gs.LineHeight = css.Dimension{Kind: css.Normal}
	n := &layout.Node{Kind: layout.Element, Style: gs}
	root := canvasSpace{scale: [2]float64{1, 1}}
	if xfrm := c.Child(ooxml.NSA, "grpSpPr").Child(ooxml.NSA, "xfrm"); xfrm != nil && xfrmBox(xfrm)[2] > 0 && xfrmBox(xfrm)[3] > 0 {
		// The canvas's own extent is the frame's: its children's space
		// fills it.
		root = canvasSpace{scale: [2]float64{1, 1}}.childSpace(xfrm)
		b := xfrmBox(xfrm)
		root.at = [2]float64{0, 0}
		if b[2] > 0 && w > 0 {
			root.scale[0] = float64(root.scale[0]*w) / b[2]
		}
		if b[3] > 0 && h > 0 {
			root.scale[1] = float64(root.scale[1]*h) / b[3]
		}
	} else {
		// A canvas of no extent draws its shapes at their own size, the
		// top left corner of their bounds at its origin.
		root.off = canvasOrigin(c)
	}
	count := 0
	d.canvasTree(s, c, st, root, n, &count, depth+1)
	return n
}

// canvasTree adds the shapes, pictures and groups of the DrawingML group
// g, in the space c, to n.
func (d *document) canvasTree(s *story, g *ooxml.Element, st *css.Style, c canvasSpace, n *layout.Node, count *int, depth int) {
	if depth > maxNesting {
		return
	}
	for _, k := range g.Kids() {
		if k.Space != ooxml.NSA || d.ctx.Err() != nil {
			continue
		}
		if *count >= maxCanvasShapes {
			d.warn(warn.LimitReached, warn.Omitted, "shapes of a locked canvas past the limit are left out")
			return
		}
		switch k.Local {
		case "grpSp":
			if xfrm := k.Child(ooxml.NSA, "grpSpPr").Child(ooxml.NSA, "xfrm"); xfrm != nil {
				d.canvasTree(s, k, st, c.childSpace(xfrm), n, count, depth+1)
			}
		case "sp", "cxnSp", "pic":
			*count++
			spPr := k.Child(ooxml.NSA, "spPr")
			xfrm := spPr.Child(ooxml.NSA, "xfrm")
			b := xfrmBox(xfrm)
			x, y := c.point(b[0], b[1])
			bw, bh := float64(b[2]*c.scale[0]), float64(b[3]*c.scale[1])
			rot, fh, fv := ooxml.Transform2D(xfrm)
			if k.Local == "pic" {
				// A picture of a canvas is described by its own non-visual
				// properties, in the DrawingML namespace (20.1.2.2.8 cNvPr).
				alt, decorative := ooxml.Describe(k.Child(ooxml.NSA, "nvPicPr").Child(ooxml.NSA, "cNvPr"))
				if p := d.picture(s, ooxml.ParsePicture(k), st, bw, bh, alt); p != nil {
					if decorative && p.Role == "Figure" {
						p.Role, p.Alt = "", ""
					}
					setTransform(p.Style, rot, fh, fv)
					n.Children = append(n.Children, absolute(p, x, y))
				}
				continue
			}
			g := ooxml.ParseGeometry(spPr)
			d.checkGeometry(g)
			v := vectorShape(st, g, ooxml.ParseShapeStyle(spPr, k.Child(ooxml.NSA, "style"), d.theme), bw, bh)
			setTransform(v.Style, rot, fh, fv)
			n.Children = append(n.Children, absolute(v, x, y))
			if body := k.Child(ooxml.NSA, "txSp").Child(ooxml.NSA, "txBody"); body != nil && body.Find(ooxml.NSA, "t") != nil {
				t := d.drawingText(body, k.Child(ooxml.NSA, "style").Child(ooxml.NSA, "fontRef"), bw, bh, min(c.scale[0], c.scale[1]))
				setTransform(t.Style, rot, false, false)
				n.Children = append(n.Children, absolute(t, x, y))
			}
		}
	}
}

// canvasOrigin returns the top left corner, in points, of the bounds of
// the shapes and pictures of the canvas c, those of its groups included,
// mapped by their groups' transforms.
func canvasOrigin(c *ooxml.Element) [2]float64 {
	o := [2]float64{math.Inf(1), math.Inf(1)}
	leafOrigin(c, canvasSpace{scale: [2]float64{1, 1}}, &o, 0)
	if math.IsInf(o[0], 1) {
		return [2]float64{}
	}
	return o
}

// leafOrigin lowers o to the top left corner of each shape and picture of
// the group g in the space c.
func leafOrigin(g *ooxml.Element, c canvasSpace, o *[2]float64, depth int) {
	if depth > maxNesting {
		return
	}
	for _, k := range g.Kids() {
		switch {
		case k.Is(ooxml.NSA, "grpSp"):
			if xfrm := k.Child(ooxml.NSA, "grpSpPr").Child(ooxml.NSA, "xfrm"); xfrm != nil {
				leafOrigin(k, c.childSpace(xfrm), o, depth+1)
			}
		case k.Is(ooxml.NSA, "sp"), k.Is(ooxml.NSA, "cxnSp"), k.Is(ooxml.NSA, "pic"):
			if xfrm := k.Child(ooxml.NSA, "spPr").Child(ooxml.NSA, "xfrm"); xfrm != nil {
				b := xfrmBox(xfrm)
				x, y := c.point(b[0], b[1])
				o[0], o[1] = min(o[0], x), min(o[1], y)
			}
		}
	}
}
