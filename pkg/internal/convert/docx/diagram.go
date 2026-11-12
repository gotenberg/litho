package docx

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/shape"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// The namespaces of a SmartArt diagram (ECMA-376 Part 1, 21.4) and of the
// drawing Word saves for it (MS-ODRAWXML, 2.4).
const (
	nsDgm = "http://schemas.openxmlformats.org/drawingml/2006/diagram"
	nsDSP = "http://schemas.microsoft.com/office/drawing/2008/diagram"
)

// maxDiagramShapes caps the shapes one SmartArt drawing draws. Anti-DoS
// guard, not spec-mandated: a diagram holds a shape or two per data point,
// and Office's diagrams stay under a few hundred points.
const maxDiagramShapes = 4096

// diagram builds a SmartArt graphic frame of w by h points (21.4.2.5
// relIds): an inline block holding the diagram's shapes, each drawn with
// its geometry, fill and outline, and its text. The shapes are those of
// the drawing Word saves beside the diagram's parts (MS-ODRAWXML, 2.4: the
// dsp:drawing the data model's dsp:dataModelExt names), else those the
// layout engine lays out from the data model, layout definition, quick
// style and colours (21.4.2). A diagram with neither keeps its room
// empty, with a warning.
//
// # Corpus evidence
//
//   - litho/docx smartart: Word prints the four boxes and three arrows of
//     the basic process where the frame sits, the text white in the
//     theme's minor font, centred in each box; drawn from the saved
//     drawing, layout rises from 0 to 0.64. Word prints the boxes 88 by
//     100.8 points where the drawing holds 79.2 by 57.6: it lays this
//     diagram out again, from a data model without presentation points,
//     which the layout engine does not lay out.
func (d *document) diagram(s *story, data *ooxml.Element, st *css.Style, w, h float64, depth int) *layout.Node {
	ids := data.Child(nsDgm, "relIds")
	var roots [4]*ooxml.Element
	for i, name := range [4]string{"dm", "lo", "qs", "cs"} {
		roots[i] = d.diagramPart(s, ids, name)
	}
	gs := inlineStyle(st)
	gs.Display = css.InlineBlock
	gs.Width, gs.Height = css.Pts(w), css.Pts(h)
	gs.Position = css.Relative
	gs.LineHeight = css.Dimension{Kind: css.Normal}
	n := &layout.Node{Kind: layout.Element, Style: gs, Role: "Figure"}
	dg := ooxml.ParseDiagram(roots[0], roots[1], roots[2], roots[3])
	count := 0
	if drawing := d.diagramDrawing(s, dg); drawing != nil {
		d.diagramTree(drawing.Child(nsDSP, "spTree"), st, n, &count, depth)
	} else if root := dg.LayOut(emuOf(w), emuOf(h)); root != nil {
		d.diagramLaidOut(root, st, n, 0, 0, &count, depth)
	}
	if len(n.Children) == 0 {
		d.warn(warn.ContentUnsupported, warn.Omitted, "a SmartArt diagram is left out: the document holds neither its layout nor a drawing of it")
		return box(st, w, h)
	}
	n.Alt = office.ShownText(n.Children)
	return n
}

// emuOf returns pt points in EMU (20.1.2.1: 12700 to the point).
func emuOf(pt float64) int64 { return int64(math.Round(pt * 12700)) }

// diagramPart parses the diagram part the relIds attribute name names,
// nil for none or one that does not parse (with a warning).
func (d *document) diagramPart(s *story, ids *ooxml.Element, name string) *ooxml.Element {
	id, ok := ids.AttrNS(ooxml.NSR, name)
	if !ok {
		return nil
	}
	rel, ok := d.pkg.RelationshipByID(d.ctx, s.part, id)
	if !ok || rel.External || !d.pkg.Has(rel.Target) {
		return nil
	}
	root, err := d.pkg.ParseXML(d.ctx, rel.Target, understood)
	if err != nil {
		if !d.stop(err) {
			// The diagram is drawn without the part: from its saved drawing,
			// or with the defaults the layout engine gives what is missing.
			d.warn(warn.PartMalformed, warn.Approximated, fmt.Sprintf("the SmartArt part %s does not parse: %v", rel.Target, err))
		}
		return nil
	}
	return root
}

// diagramDrawing returns the root of the first drawing part the diagram's
// data model names that holds a shape, nil for none.
func (d *document) diagramDrawing(s *story, dg *ooxml.Diagram) *ooxml.Element {
	for _, id := range dg.Drawings {
		rel, ok := d.pkg.RelationshipByID(d.ctx, s.part, id)
		if !ok || rel.External || !d.pkg.Has(rel.Target) {
			continue
		}
		root, err := d.pkg.ParseXML(d.ctx, rel.Target, understood)
		if err != nil {
			if d.stop(err) {
				return nil
			}
			// The next drawing, or the layout engine, draws the diagram.
			d.warn(warn.PartMalformed, warn.Approximated, fmt.Sprintf("the SmartArt drawing %s does not parse: %v", rel.Target, err))
			continue
		}
		if root.Find(nsDSP, "sp") != nil {
			return root
		}
	}
	return nil
}

// diagramLaidOut adds a laid out shape and its kids to n, at ox, oy EMU
// in the frame: a group draws only its kids.
func (d *document) diagramLaidOut(ds *ooxml.DiagramShape, st *css.Style, n *layout.Node, ox, oy int64, count *int, depth int) {
	if depth > maxNesting || *count >= maxDiagramShapes {
		return
	}
	x, y := ox+ds.X, oy+ds.Y
	if !ds.Group {
		*count++
		preset := ds.Preset()
		draws := preset != "" && shape.Known(&shape.Geometry{Preset: preset})
		sp := ds.Element(nsDSP, preset, draws)
		b := box4{float64(x) / 12700, float64(y) / 12700, float64(max(0, ds.W)) / 12700, float64(max(0, ds.H)) / 12700}
		rot := float64(ds.Rot)/60000 + ds.DiagramRot
		n.Children = append(n.Children, d.diagramShapeAt(sp, st, b, rot, b, rot+float64(ds.TextRot), draws)...)
	}
	for _, k := range ds.Kids {
		d.diagramLaidOut(k, st, n, x, y, count, depth+1)
	}
}

// diagramTree adds the shapes of a saved drawing's shape tree or group to
// n, in the frame's coordinates: a drawing's offsets are the frame's own.
func (d *document) diagramTree(tree *ooxml.Element, st *css.Style, n *layout.Node, count *int, depth int) {
	if depth > maxNesting {
		return
	}
	for _, k := range tree.Kids() {
		if *count >= maxDiagramShapes {
			return
		}
		switch {
		case k.Is(nsDSP, "grpSp"):
			d.diagramTree(k, st, n, count, depth+1)
		case k.Is(nsDSP, "sp"):
			*count++
			xfrm := k.Child(nsDSP, "spPr").Child(ooxml.NSA, "xfrm")
			b := xfrmBox(xfrm)
			rot, _, _ := ooxml.Transform2D(xfrm)
			tb, trot := b, rot
			if tx := k.Child(nsDSP, "txXfrm"); tx != nil {
				tb = xfrmBox(tx)
				trot, _, _ = ooxml.Transform2D(tx)
			}
			n.Children = append(n.Children, d.diagramShapeAt(k, st, b, rot, tb, trot, true)...)
		}
	}
}

// box4 is a box in points: x, y, width, height.
type box4 [4]float64

// diagramShapeAt returns the nodes of one dsp:sp drawn in the box b turned
// by rot degrees, when draws: its geometry; then its text in the box tb
// turned by trot degrees.
func (d *document) diagramShapeAt(sp *ooxml.Element, st *css.Style, b box4, rot float64, tb box4, trot float64, draws bool) []*layout.Node {
	spPr := sp.Child(nsDSP, "spPr")
	var out []*layout.Node
	if draws && (b[2] > 0 || b[3] > 0) {
		g := ooxml.ParseGeometry(spPr)
		d.checkGeometry(g)
		v := vectorShape(st, g, ooxml.ParseShapeStyle(spPr, sp.Child(nsDSP, "style"), d.theme), b[2], b[3])
		_, fh, fv := ooxml.Transform2D(spPr.Child(ooxml.NSA, "xfrm"))
		setTransform(v.Style, rot, fh, fv)
		out = append(out, absolute(v, b[0], b[1]))
	}
	body := sp.Child(nsDSP, "txBody")
	if body == nil || body.Find(ooxml.NSA, "t") == nil {
		return out
	}
	t := d.drawingText(body, sp.Child(nsDSP, "style").Child(ooxml.NSA, "fontRef"), tb[2], tb[3])
	setTransform(t.Style, trot, false, false)
	return append(out, absolute(t, tb[0], tb[1]))
}

// xfrmBox returns the offset and size of a DrawingML transform in points
// (20.1.7.6 xfrm: off, ext).
func xfrmBox(xfrm *ooxml.Element) box4 {
	x, _ := ooxml.EMU(attrOr(xfrm.Child(ooxml.NSA, "off"), "x"))
	y, _ := ooxml.EMU(attrOr(xfrm.Child(ooxml.NSA, "off"), "y"))
	w, _ := ooxml.EMU(attrOr(xfrm.Child(ooxml.NSA, "ext"), "cx"))
	h, _ := ooxml.EMU(attrOr(xfrm.Child(ooxml.NSA, "ext"), "cy"))
	return box4{x, y, max(w, 0), max(h, 0)}
}

// absolute positions n at x, y in its containing block.
func absolute(n *layout.Node, x, y float64) *layout.Node {
	n.Style.Position = css.Absolute
	n.Style.Left, n.Style.Top = css.Pts(x), css.Pts(y)
	if n.Style.Display == css.Inline || n.Style.Display == css.InlineBlock {
		n.Style.Display = css.Block
	}
	return n
}

// drawingText builds a DrawingML text body (21.1.2.1.1 txBody) as a block
// of w by h points: its paragraphs inside the body's insets (0.1 inch left
// and right, 0.05 inch top and bottom by default), at the top, the middle
// or the bottom of the box as the body's anchor says (20.1.10.59
// ST_TextAnchoringType). A run's colour is its own fill, else the colour
// of the shape's font reference (20.1.4.2.10 fontRef), else black; its
// face its latin typeface, else the theme font fontRef names, else the
// theme's minor font.
func (d *document) drawingText(body, fontRef *ooxml.Element, w, h float64) *layout.Node {
	bodyPr := body.Child(ooxml.NSA, "bodyPr")
	bs := d.blockStyle()
	bs.Width, bs.Height = css.Pts(max(w, 1)), css.Pts(max(h, 1))
	bs.BoxSizing = css.BorderBox
	bs.Display = css.Flex
	bs.FlexDirection = css.FlexColumn
	switch attrOr(bodyPr, "anchor") {
	case "ctr":
		bs.JustifyContent = css.BoxCenter
	case "b":
		bs.JustifyContent = css.BoxFlexEnd
	}
	for side, v := range bodyInsets(bodyPr) {
		bs.Padding[side] = css.Pts(v)
	}
	face := d.themeFace(attrOr(fontRef, "idx"))
	color := css.Color{A: 1}
	if c, ok := ooxml.SchemeColor(fontRef, d.theme); ok {
		color = c
	}
	scale := 1.0
	if fs, ok := ooxml.Int(attrOr(bodyPr.Child(ooxml.NSA, "normAutofit"), "fontScale")); ok && fs > 0 {
		scale = float64(min(fs, 100000)) / 100000
	}
	inner := d.blockStyle()
	var paras []*layout.Node
	for _, p := range body.Kids() {
		if p.Is(ooxml.NSA, "p") {
			paras = append(paras, d.drawingPara(p, face, color, scale))
		}
	}
	return &layout.Node{Kind: layout.Element, Style: bs, Children: []*layout.Node{{Kind: layout.Element, Style: inner, Children: paras}}}
}

// themeFace returns the theme typeface a font reference's idx names
// (20.1.10.32 ST_FontCollectionIndex: major, minor, none), the minor one
// for any other.
func (d *document) themeFace(idx string) string {
	if d.theme == nil {
		return ""
	}
	if idx == "major" {
		return d.theme.Major.Latin
	}
	return d.theme.Minor.Latin
}

// drawingPara builds one DrawingML paragraph (21.1.2.2.6 p): its runs and
// line breaks, aligned as its pPr algn says (20.1.10.58 ST_TextAlignType),
// its proportional line spacing (21.1.2.2.10 lnSpc spcPct) applied to each
// run, a font size missing from a run 18 points (21.1.2.3.9 rPr sz).
func (d *document) drawingPara(p *ooxml.Element, face string, color css.Color, scale float64) *layout.Node {
	ps := d.blockStyle()
	pPr := p.Child(ooxml.NSA, "pPr")
	switch attrOr(pPr, "algn") {
	case "ctr":
		ps.TextAlign = css.AlignCenter
	case "r":
		ps.TextAlign = css.AlignEnd
	case "just", "dist":
		ps.TextAlign = css.AlignJustify
	}
	mult := 0.0
	if v, ok := ooxml.Int(attrOr(pPr.Child(ooxml.NSA, "lnSpc").Child(ooxml.NSA, "spcPct"), "val")); ok && v > 0 {
		mult = float64(min(v, maxLineSpacingPct)) / 100000
	}
	n := &layout.Node{Kind: layout.Element, Style: ps, Role: "P"}
	size := 18.0
	for _, r := range p.Kids() {
		switch {
		case r.Is(ooxml.NSA, "r"), r.Is(ooxml.NSA, "fld"):
			rp := d.drawingRunProps(r.Child(ooxml.NSA, "rPr"), face, color, scale)
			size = *rp.size
			rs := inlineStyle(ps)
			d.applyRun(rs, rp, mult)
			text := r.Child(ooxml.NSA, "t")
			if text == nil || text.Text == "" {
				continue
			}
			n.Children = append(n.Children, &layout.Node{Kind: layout.Element, Style: rs, Children: []*layout.Node{{Kind: layout.Text, Style: rs, Text: text.Text}}})
		case r.Is(ooxml.NSA, "br"):
			n.Children = append(n.Children, &layout.Node{Kind: layout.LineBreak, Style: ps})
		}
	}
	if len(n.Children) == 0 {
		rp := d.drawingRunProps(p.Child(ooxml.NSA, "endParaRPr"), face, color, scale)
		size = *rp.size
		d.applyRun(ps, rp, mult)
	}
	// 21.1.2.2.11 spcAft and 21.1.2.2.12 spcBef: a percentage of the
	// line, here of 1.2 times the font size.
	for _, sp := range []struct {
		side  css.Side
		local string
	}{{css.Top, "spcBef"}, {css.Bottom, "spcAft"}} {
		side, sp := sp.side, pPr.Child(ooxml.NSA, sp.local)
		if v, ok := ooxml.Int(attrOr(sp.Child(ooxml.NSA, "spcPts"), "val")); ok && v > 0 {
			ps.Margin[side] = css.Pts(float64(min(v, maxSpacingPts)) / 100)
		} else if v, ok := ooxml.Int(attrOr(sp.Child(ooxml.NSA, "spcPct"), "val")); ok && v > 0 {
			ps.Margin[side] = css.Pts(float64(min(v, maxLineSpacingPct)) / 100000 * size * 1.2)
		}
	}
	return n
}

// The bounds of a DrawingML paragraph's spacing read. Anti-DoS guards,
// not spec-mandated: 20.1.10.44 ST_TextSpacingPercentOrPercentString
// allows 13200%, 20.1.10.85 ST_TextSpacingPoint 1584 points, and a larger
// value only pushes text off the shape.
const (
	maxLineSpacingPct = 13200000
	maxSpacingPts     = 158400
)

// drawingRunProps returns the run properties of a DrawingML rPr
// (21.1.2.3.9): size, bold, italic, underline, fill and latin typeface,
// the size scaled by scale, the shape's autofit font scale.
func (d *document) drawingRunProps(rPr *ooxml.Element, face string, color css.Color, scale float64) runProps {
	var rp runProps
	size := 18.0
	if v, err := strconv.Atoi(attrOr(rPr, "sz")); err == nil && v >= 100 && v <= 400000 {
		size = float64(v) / 100
	}
	size *= scale
	rp.size = &size
	if v, ok := rPr.Attr("b"); ok {
		b := ooxml.OnOff(v, true)
		rp.bold = &b
	}
	if v, ok := rPr.Attr("i"); ok {
		i := ooxml.OnOff(v, true)
		rp.italic = &i
	}
	if v := attrOr(rPr, "u"); v != "" && v != "none" {
		rp.underline = &underlineSpec{style: "single"}
	}
	if c, ok := ooxml.SchemeColor(rPr.Child(ooxml.NSA, "solidFill"), d.theme); ok {
		color = c
	}
	rp.color = &colorSpec{c: color}
	if l := attrOr(rPr.Child(ooxml.NSA, "latin"), "typeface"); l != "" && !strings.HasPrefix(l, "+") {
		face = l
	} else if strings.HasPrefix(l, "+mj") {
		face = d.themeFace("major")
	}
	rp.fonts = fontSpec{ascii: face, hAnsi: face}
	return rp
}
