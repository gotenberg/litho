package pptx

import (
	"encoding/xml"
	"fmt"
	"math"
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/slides"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/shape"
)

// emuPerPoint is the EMUs in a point (ECMA-376 Part 1, 20.1.2.1).
const emuPerPoint = 12700

// diagramParts are the parts of a SmartArt diagram a graphic frame names
// (21.4.2.5 relIds: the data model, layout, quick style and colours).
type diagramParts struct {
	data, layout, style, colors string
}

// diagram reads a SmartArt graphic frame (ECMA-376 Part 1, 21.4 dgm:relIds)
// as Impress draws it: the group of shapes its drawing part holds when
// PowerPoint saved one with shapes (MS-ODRAWXML, 2.4: the dsp:drawing the
// data part's dsp:dataModelExt names), else the shapes the layout engine
// lays out from the data model, layout definition, quick style and
// colours; the data model's background first. A saved drawing's font
// references take the last text colour of the colour definition's node0
// label, as Impress gives them.
//
// # Corpus evidence
//
//   - pptx: sd/bnc862510_6's drawing, whose font reference names lt1 on
//     a white box, prints its text in node0's dk1.
//   - pptx: sd/smartart-cycle's five ellipses on a circle with an arrow
//     between each two, laid out from the layout definition, its drawing
//     part empty.
func (r *reader) diagram(c *shapeCtx, data *ooxml.Element, s *slides.Shape, depth int) (*slides.Shape, error) {
	if depth >= maxGroupDepth {
		return nil, nil
	}
	ids := data.Child(nsDgm, "relIds")
	var parts diagramParts
	for _, p := range []struct {
		name string
		dst  *string
	}{{"dm", &parts.data}, {"lo", &parts.layout}, {"qs", &parts.style}, {"cs", &parts.colors}} {
		if id, ok := ids.AttrNS(ooxml.NSR, p.name); ok {
			if rel, ok := r.pkg.RelationshipByID(c.part, id); ok && !rel.External && r.pkg.Has(rel.Target) {
				*p.dst = rel.Target
			}
		}
	}
	dataRoot, err := r.diagramPart(parts.data)
	if err != nil {
		return nil, err
	}
	d := ooxml.ParseDiagram(dataRoot, nil, nil, nil)
	s.Kind = slides.KindGroup
	// The diagram's shapes are placed in the frame's coordinates, from its
	// top left corner.
	s.Child = compose.Rect{Width: s.Box.Width, Height: s.Box.Height}
	inner := &shapeCtx{layer: c.layer, sc: c.sc, ns: nsDSP, part: parts.data}
	if bg := r.diagramShape(inner, &ooxml.DiagramShape{W: emu(s.Box.Width), H: emu(s.Box.Height), FillSpPr: d.Background, LineSpPr: d.Whole}, "rect", 0, 0); bg != nil && (bg.Fill.Kind != slides.FillNone || bg.Line.Width > 0) {
		s.Kids = append(s.Kids, bg)
	}
	if part := r.diagramDrawing(c, d); part != "" {
		colors, err := r.diagramPart(parts.colors)
		if err != nil {
			return nil, err
		}
		root, err := r.pkg.ParseXML(r.ctx, part, understood)
		if err != nil {
			if isMalformed(err) {
				r.warn("malformed-part", fmt.Sprintf("the SmartArt drawing %s does not parse and is left out: %v", part, err))
				return nil, nil
			}
			return nil, err
		}
		drawn := &shapeCtx{layer: c.layer, sc: c.sc, ns: nsDSP, part: part}
		// Impress gives the drawing's font references the last text colour
		// of the colour definition's node0 label.
		if cd := ooxml.ParseDiagram(nil, nil, nil, colors).Colors["node0"]; cd != nil && len(cd.TextFill) > 0 {
			drawn.fontColor = cd.TextFill[len(cd.TextFill)-1]
		}
		if err := r.shapeList(&s.Kids, drawn, root.Child(nsDSP, "spTree"), depth+1); err != nil {
			return nil, err
		}
	} else {
		var roots [3]*ooxml.Element
		for i, p := range []string{parts.layout, parts.style, parts.colors} {
			if roots[i], err = r.diagramPart(p); err != nil {
				return nil, err
			}
		}
		d = ooxml.ParseDiagram(dataRoot, roots[0], roots[1], roots[2])
		if d.Layout == nil {
			r.warn("unsupported-content", "a SmartArt diagram without a saved drawing or a layout definition is left out")
			return nil, nil
		}
		root := d.LayOut(emu(s.Box.Width), emu(s.Box.Height))
		for _, k := range root.Kids {
			if k.Name == "background" && k.FillSpPr == d.Background {
				continue
			}
			r.diagramShapes(&s.Kids, inner, k, 0, 0, 0)
		}
		r.shrinkGroups(s.Kids, roots[0])
	}
	if len(s.Kids) == 0 {
		return nil, nil
	}
	return s, nil
}

// shrinkGroups ties the text bodies of a laid out diagram's shapes into
// shrink groups, one per layout node name whose primary font size a
// layout node's own constraints make equal (21.4.2.8 constr type
// primFontSz, op equ, forName), so the builder shrinks the text of all
// of them by one scale, the smallest any needs, as Impress syncs a
// SmartArt diagram's font heights (oox Shape::syncDiagramFontHeights);
// a constraint inside a choose, and any other shape, fits alone.
//
// # Corpus evidence
//
//   - pptx: sd/smartart-maxdepth's "parTxOnly" nodes, made equal by a
//     constraint of a dgm:else branch, print "first" broken as "fi rs t"
//     and "second" as "se co nd", each at its own size;
//     sd/smartart-autofit-sync, sd/smartart-vertical-block-list and
//     sd/smartart-font-size hold theirs in their layout nodes' own lists.
func (r *reader) shrinkGroups(shapes []*slides.Shape, layoutDef *ooxml.Element) {
	equal := map[string]bool{}
	var walk func(e *ooxml.Element, depth int)
	walk = func(e *ooxml.Element, depth int) {
		if depth > maxGroupDepth*4 {
			return
		}
		for _, k := range e.Kids() {
			if k.Local == "constrLst" && e.Local == "layoutNode" {
				for _, c := range k.Kids() {
					if c.Local == "constr" && attr(c, "type") == "primFontSz" && attr(c, "op") == "equ" && attr(c, "forName") != "" {
						equal[attr(c, "forName")] = true
					}
				}
			}
			walk(k, depth+1)
		}
	}
	walk(layoutDef, 0)
	ids := map[string]int{}
	var tie func(list []*slides.Shape)
	tie = func(list []*slides.Shape) {
		for _, k := range list {
			if k.Text != nil && equal[k.Name] {
				id, ok := ids[k.Name]
				if !ok {
					r.shrinkGroupCount++
					id = r.shrinkGroupCount
					ids[k.Name] = id
				}
				k.Text.ShrinkGroup = id
			}
			tie(k.Kids)
		}
	}
	tie(shapes)
}

// emu converts points to whole EMUs.
func emu(pt float64) int64 {
	return int64(math.Round(pt * emuPerPoint))
}

// diagramPart parses a diagram part, nil for none or one that does not
// parse (with a warning).
func (r *reader) diagramPart(part string) (*ooxml.Element, error) {
	if part == "" {
		return nil, nil
	}
	root, err := r.pkg.ParseXML(r.ctx, part, understood)
	if err != nil {
		if isMalformed(err) {
			r.warn("malformed-part", fmt.Sprintf("the SmartArt part %s does not parse: %v", part, err))
			return nil, nil
		}
		return nil, err
	}
	return root, nil
}

// diagramDrawing returns the drawing part PowerPoint saved for a diagram
// when it holds shapes, as Impress takes it over its own layout: the
// first dsp:dataModelExt relationship of the slide whose drawing holds a
// dsp:sp; "" for none.
func (r *reader) diagramDrawing(c *shapeCtx, d *ooxml.Diagram) string {
	for _, id := range d.Drawings {
		rel, ok := r.pkg.RelationshipByID(c.part, id)
		if !ok || rel.External || !r.pkg.Has(rel.Target) {
			continue
		}
		root, err := r.pkg.ParseXML(r.ctx, rel.Target, understood)
		if err != nil {
			// A drawing that does not parse is no drawing: the layout
			// engine draws the diagram.
			continue
		}
		if root.Find(nsDSP, "sp") != nil {
			return rel.Target
		}
	}
	return ""
}

// diagramShapes appends a laid out shape and its kids to out, flattened
// into the frame's coordinates: a group draws only its kids, a shape
// draws itself under its kids.
func (r *reader) diagramShapes(out *[]*slides.Shape, c *shapeCtx, ds *ooxml.DiagramShape, ox, oy int64, depth int) {
	if depth >= maxGroupDepth {
		return
	}
	x, y := ox+ds.X, oy+ds.Y
	if !ds.Group {
		if s := r.diagramShape(c, ds, ds.Preset(), x, y); s != nil {
			*out = append(*out, s)
		}
	}
	for _, k := range ds.Kids {
		r.diagramShapes(out, c, k, x, y, depth+1)
	}
}

// diagramShape returns a laid out shape at x, y as a slide shape, read
// through the drawing's shape reader: a dsp:sp made of its preset, the
// fill and line of its data, its style references with the colour
// definition's colours, and its text. A shape without a geometry Impress
// draws (no preset, a hidden geometry, an unconverted connector) prints
// its text alone; nil when nothing prints.
func (r *reader) diagramShape(c *shapeCtx, ds *ooxml.DiagramShape, preset string, x, y int64) *slides.Shape {
	box := compose.Rect{X: float64(x) / emuPerPoint, Y: float64(y) / emuPerPoint, Width: float64(max(0, ds.W)) / emuPerPoint, Height: float64(max(0, ds.H)) / emuPerPoint}
	draws := preset != "" && shape.Known(&shape.Geometry{Preset: preset})
	sp := &ooxml.Element{Space: nsDSP, Local: "sp"}
	pr := &ooxml.Element{Space: nsDSP, Local: "spPr"}
	if draws && preset != "rect" {
		pr.Children = append(pr.Children, el("prstGeom", []string{"prst", preset}, el("avLst", nil)))
	}
	if draws {
		if f := fillChild(ds.FillSpPr); f != nil {
			pr.Children = append(pr.Children, f)
		}
		if ln := ds.LineSpPr.Child(ooxml.NSA, "ln"); ln != nil {
			pr.Children = append(pr.Children, ln)
		}
	}
	sp.Children = append(sp.Children, pr)
	style := &ooxml.Element{Space: nsDSP, Local: "style"}
	for _, ref := range []struct {
		ref, col *ooxml.Element
		local    string
	}{{ds.Style.Line, ds.StyleColor.Line, "lnRef"}, {ds.Style.Fill, ds.StyleColor.Fill, "fillRef"}, {ds.Style.Effect, ds.StyleColor.Effect, "effectRef"}, {ds.Style.Font, ds.StyleColor.Font, "fontRef"}} {
		if ref.ref == nil {
			continue
		}
		e := &ooxml.Element{Space: ooxml.NSA, Local: ref.local, Attrs: ref.ref.Attrs, Children: ref.ref.Children}
		if ref.col != nil {
			e.Children = []*ooxml.Element{ref.col}
		}
		style.Children = append(style.Children, e)
	}
	sp.Children = append(sp.Children, style)
	if tb := diagramText(ds); tb != nil {
		sp.Children = append(sp.Children, tb)
	}
	s := &slides.Shape{Kind: slides.KindGeometry, Name: ds.Name, Box: box, Rotation: float64(ds.Rot) / 60000}
	chain := []*ooxml.Element{sp}
	fc := &fillCtx{sc: c.sc, part: c.part}
	if draws {
		s.Geometry = r.geometryOf(chain)
		s.Fill = r.shapeFill(fc, chain, style)
		s.Line = r.shapeLine(fc, chain, style)
	}
	s.Text = r.textBody(c, chain, placeholder{}, style.Child(ooxml.NSA, "fontRef"))
	if s.Text != nil && empty(s.Text) {
		s.Text = nil
	}
	if rot := math.Mod(ds.DiagramRot, 360); rot != 0 {
		// The layout shape's rotation turns the geometry inside its box:
		// a quarter turn swaps the box's sides about its centre.
		if q := math.Mod(math.Abs(rot), 180); q == 90 {
			cx, cy := s.Box.X+s.Box.Width/2, s.Box.Y+s.Box.Height/2
			s.Box = compose.Rect{X: cx - s.Box.Height/2, Y: cy - s.Box.Width/2, Width: s.Box.Height, Height: s.Box.Width}
		}
		s.Rotation += rot
	}
	if !draws && s.Text == nil {
		return nil
	}
	if ds.TextRot != 0 && s.Text != nil {
		return turnText(s, float64(ds.TextRot))
	}
	return s
}

// turnText returns s as a group of the shape without its text and the
// text alone, turned by rot degrees against the shape (21.4.2.1 tx
// autoTxRot, as Impress turns a diagram text upright).
func turnText(s *slides.Shape, rot float64) *slides.Shape {
	text := &slides.Shape{Kind: slides.KindGeometry, Name: s.Name, Box: s.Box, Rotation: s.Rotation + rot, Text: s.Text}
	if math.Mod(math.Abs(rot), 180) == 90 {
		cx, cy := s.Box.X+s.Box.Width/2, s.Box.Y+s.Box.Height/2
		text.Box = compose.Rect{X: cx - s.Box.Height/2, Y: cy - s.Box.Width/2, Width: s.Box.Height, Height: s.Box.Width}
	}
	body := *s
	body.Text = nil
	g := &slides.Shape{Kind: slides.KindGroup, Name: s.Name, Kids: []*slides.Shape{&body, text}}
	g.Box = union(g.Kids)
	g.Child = g.Box
	return g
}

// fillChild returns the fill among e's children, nil for none.
func fillChild(e *ooxml.Element) *ooxml.Element {
	for _, k := range e.Kids() {
		if k.Space != ooxml.NSA {
			continue
		}
		switch k.Local {
		case "noFill", "solidFill", "gradFill", "blipFill", "pattFill", "grpFill":
			return k
		}
	}
	return nil
}

// el returns a new DrawingML element with the attributes of the name and
// value pairs kv and the children kids.
func el(local string, kv []string, kids ...*ooxml.Element) *ooxml.Element {
	return &ooxml.Element{Space: ooxml.NSA, Local: local, Attrs: attrs(kv...), Children: kids}
}

// withAttr returns a copy of e with the attribute name set to v.
func withAttr(e *ooxml.Element, name, v string) *ooxml.Element {
	out := &ooxml.Element{Space: ooxml.NSA, Local: "pPr"}
	if e != nil {
		*out = *e
		out.Attrs = append([]xml.Attr(nil), e.Attrs...)
	}
	out.Attrs = setAttr(out.Attrs, name, v)
	return out
}

// diagramText returns a laid out shape's text as a dsp:txBody: its data's
// body properties with the anchor, insets and autofit the text algorithm
// set, its data's list style, and its paragraphs with the level, margin,
// indent, alignment and bullet the algorithm set; nil for no text.
func diagramText(ds *ooxml.DiagramShape) *ooxml.Element {
	if len(ds.Paragraphs) == 0 {
		return nil
	}
	body := &ooxml.Element{Space: ooxml.NSA, Local: "bodyPr"}
	if ds.BodyPr != nil {
		body.Attrs = append([]xml.Attr(nil), ds.BodyPr.Attrs...)
		for _, k := range ds.BodyPr.Kids() {
			if ds.Autofit && (k.Local == "normAutofit" || k.Local == "spAutoFit" || k.Local == "noAutofit") {
				continue
			}
			body.Children = append(body.Children, k)
		}
	}
	if ds.Anchor != "" {
		body.Attrs = setAttr(body.Attrs, "anchor", ds.Anchor)
	}
	for i, name := range [4]string{"lIns", "tIns", "rIns", "bIns"} {
		if ds.Inset[i] {
			body.Attrs = setAttr(body.Attrs, name, strconv.FormatInt(ds.Insets[i], 10))
		}
	}
	if ds.Autofit {
		body.Children = append(body.Children, el("normAutofit", nil))
	}
	tb := &ooxml.Element{Space: nsDSP, Local: "txBody", Children: []*ooxml.Element{body}}
	if ds.ListStyle != nil {
		tb.Children = append(tb.Children, ds.ListStyle)
	}
	for _, p := range ds.Paragraphs {
		ppr := withAttr(p.PPr, "lvl", strconv.Itoa(max(0, min(p.Level, 8))))
		if p.HasMarL {
			ppr.Attrs = setAttr(ppr.Attrs, "marL", strconv.FormatInt(p.MarL, 10))
		}
		if p.HasIndent {
			ppr.Attrs = setAttr(ppr.Attrs, "indent", strconv.FormatInt(p.Indent, 10))
		}
		if p.Align != "" {
			ppr.Attrs = setAttr(ppr.Attrs, "algn", p.Align)
		}
		if p.Bullet {
			var kids []*ooxml.Element
			for _, k := range ppr.Children {
				switch k.Local {
				case "buNone", "buChar", "buAutoNum", "buBlip":
					continue
				}
				kids = append(kids, k)
			}
			kids = append(kids, el("buChar", []string{"char", "•"}))
			ppr.Children = kids
		} else if ppr.Child(ooxml.NSA, "buChar") == nil && ppr.Child(ooxml.NSA, "buAutoNum") == nil && ppr.Child(ooxml.NSA, "buBlip") == nil {
			ppr.Children = append(append([]*ooxml.Element(nil), ppr.Children...), el("buNone", nil))
		}
		out := &ooxml.Element{Space: ooxml.NSA, Local: "p", Children: []*ooxml.Element{ppr}}
		for _, k := range p.Source.Kids() {
			if k.Space != ooxml.NSA || (k.Local != "r" && k.Local != "br" && k.Local != "fld") {
				continue
			}
			if ds.FontSize > 0 {
				k = sizedRun(k, ds.FontSize)
			}
			out.Children = append(out.Children, k)
		}
		tb.Children = append(tb.Children, out)
	}
	return tb
}

// sizedRun returns a run whose rPr sets no size with the size pt.
func sizedRun(run *ooxml.Element, pt float64) *ooxml.Element {
	rpr := run.Child(ooxml.NSA, "rPr")
	if _, ok := rpr.Attr("sz"); ok {
		return run
	}
	out := *run
	out.Children = nil
	nr := &ooxml.Element{Space: ooxml.NSA, Local: "rPr"}
	if rpr != nil {
		*nr = *rpr
		nr.Attrs = append([]xml.Attr(nil), rpr.Attrs...)
	}
	nr.Attrs = setAttr(nr.Attrs, "sz", strconv.Itoa(int(math.Round(pt*100))))
	out.Children = append(out.Children, nr)
	for _, k := range run.Children {
		if k != rpr {
			out.Children = append(out.Children, k)
		}
	}
	return &out
}
