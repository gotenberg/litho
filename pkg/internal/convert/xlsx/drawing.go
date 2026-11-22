package xlsx

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/chart"
	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/detmath"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/shape"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// nsXDR is the spreadsheet drawing namespace (ECMA-376 Part 1, 20.5).
const nsXDR = "http://schemas.openxmlformats.org/drawingml/2006/spreadsheetDrawing"

// The graphic data URIs of a graphic frame (ECMA-376 Part 1, 20.1.2.2.17).
const (
	uriChart   = "http://schemas.openxmlformats.org/drawingml/2006/chart"
	uriDiagram = "http://schemas.openxmlformats.org/drawingml/2006/diagram"
	// uriChartEx is the graphic data of an Office 2016 chart (MS-ODRAWXML,
	// 2.24.3 chartex: histograms, box and whisker, funnel, waterfall,
	// sunburst, treemap and region map charts), which a workbook places in
	// an mc:Choice of the cx1 or cx2 namespaces.
	uriChartEx = "http://schemas.microsoft.com/office/drawing/2014/chartex"
)

// nsC is the DrawingML chart namespace (ECMA-376 Part 1, 21.2).
const nsC = "http://schemas.openxmlformats.org/drawingml/2006/chart"

// maxDrawings caps the drawings of one sheet. Anti-DoS guard, not
// spec-mandated: a dashboard holds a few dozen pictures and charts.
const maxDrawings = 4096

// maxGroupDepth caps the nesting of shape groups walked at
// [limits.MaxSourceGroupDepth], which says why.
const maxGroupDepth = limits.MaxSourceGroupDepth

// chartOf reads the chart part named part through the DrawingML chart
// reader, nil with a warning when it does not parse. The chart's warnings
// become the document's, and its series read the sheets' cells through
// [reader.rangeValues] when their part holds no cache.
func (r *reader) chartOf(part string) *chart.Chart {
	c, err := ooxml.ParseChart(r.ctx, r.pkg, part)
	if err != nil {
		if r.stop(err) {
			return nil
		}
		r.warn(warn.PartMalformed, warn.Omitted, fmt.Sprintf("chart %s does not parse and is left out: %v", part, err))
		return nil
	}
	for _, w := range c.Warnings {
		r.warn(w.Code, w.Effect, w.Message)
	}
	c.Resolver = r.rangeValues
	c.FormatResolver = r.rangeFormat
	c.Locale = r.locale
	return c
}

// rangeFormat returns the number format of the first cell the formula ref
// names, as [reader.rangeValues] reads the range; false when no area
// names a sheet of the workbook.
func (r *reader) rangeFormat(ref string) (string, bool) {
	names, areas := sheetAreas(ref)
	for i, area := range areas {
		for _, sh := range r.doc.Sheets {
			if sh.Name == names[i] && names[i] != "" {
				return sheet.ChartFormat(sh, &r.doc.Default, area), true
			}
		}
	}
	return "", false
}

// rangeValues returns the values of the cells the formula ref names
// ("Sheet1!$B$2:$B$5", "'My sheet'!$B$2:$B$5", or several areas in
// parentheses separated by commas; ECMA-376 Part 1, 21.2.2.115 and
// 18.17.2.3), each area as sheet.ChartValues reads it, in order; nil when
// no area names a sheet of the workbook.
func (r *reader) rangeValues(ref string) []chart.Value {
	names, areas := sheetAreas(ref)
	var out []chart.Value
	for i, area := range areas {
		for _, sh := range r.doc.Sheets {
			if sh.Name != names[i] || names[i] == "" {
				continue
			}
			out = append(out, sheet.ChartValues(sh, area)...)
			break
		}
	}
	return out
}

// drawing reads the drawing part the sheet's relationship id names
// (ECMA-376 Part 1, 20.5.2.35 wsDr): each anchor in drawing order, its
// pictures and shapes as one picture, its chart as a chart. The form
// controls of the legacy VML drawing are read by [sheetReader.controls].
func (w *sheetReader) drawing(id string) error {
	r := w.r
	rel, ok := r.pkg.RelationshipByID(w.r.ctx, w.part, id)
	if !ok || rel.External || rel.Type != relDrawing || !r.pkg.Has(rel.Target) {
		return nil
	}
	root, err := r.pkg.ParseXML(r.ctx, rel.Target, understood)
	if err != nil {
		if isMalformed(err) {
			r.warn(warn.PartMalformed, warn.Omitted, fmt.Sprintf("the drawings of sheet %q do not parse: %v", w.sh.Name, err))
			return nil
		}
		return err
	}
	for _, a := range root.Kids() {
		if len(w.sh.Drawings) >= maxDrawings {
			r.warn(warn.LimitReached, warn.Omitted, fmt.Sprintf("sheet %q holds more than %d drawings: the rest are left out", w.sh.Name, maxDrawings))
			break
		}
		var d sheet.Drawing
		switch {
		case a.Is(nsXDR, "twoCellAnchor"):
			d.From = anchorOf(a.Child(nsXDR, "from"))
			to := anchorOf(a.Child(nsXDR, "to"))
			d.To = &to
		case a.Is(nsXDR, "oneCellAnchor"):
			d.From = anchorOf(a.Child(nsXDR, "from"))
			d.Width, d.Height = extent(a.Child(nsXDR, "ext"))
		case a.Is(nsXDR, "absoluteAnchor"):
			d.Absolute = true
			pos := a.Child(nsXDR, "pos")
			d.X, _ = ooxml.EMU(attr(pos, "x"))
			d.Y, _ = ooxml.EMU(attr(pos, "y"))
			d.X, d.Y = max(0, d.X), max(0, d.Y)
			d.Width, d.Height = extent(a.Child(nsXDR, "ext"))
		default:
			continue
		}
		if w.content(rel.Target, a, &d) {
			w.sh.Drawings = append(w.sh.Drawings, d)
		}
	}
	return nil
}

// drawingLink returns the external target of the hyperlink a drawing's
// cNvPr carries (ECMA-376 Part 1, 20.1.2.2.23 hlinkClick, its r:id naming
// a relationship of the drawing part), as Excel writes it ([linkURL]), ""
// for none or for a place in the workbook.
//
// # Corpus evidence
//
//   - libreoffice/xlsx: sc/image_hyperlink.xlsx: Excel writes a link to
//     https://www.google.com/ over the picture that carries it;
//     sc/chart_hyperlink.xlsx: one over each chart frame, on every page a
//     chart split by a page break prints on.
func (w *sheetReader) drawingLink(part string, nv *ooxml.Element) string {
	id, ok := nv.Child(ooxml.NSA, "hlinkClick").AttrNS(ooxml.NSR, "id")
	if !ok {
		return ""
	}
	if rel, ok := w.r.pkg.RelationshipByID(w.r.ctx, part, id); ok && rel.External {
		return linkURL(rel.Target)
	}
	return ""
}

// anchorOf reads a from or to marker (ECMA-376 Part 1, 20.5.2.15
// CT_Marker): the cell and the offset into it, the offsets in EMU.
func anchorOf(m *ooxml.Element) sheet.Anchor {
	var a sheet.Anchor
	if m == nil {
		return a
	}
	a.Column = max(0, min(childInt(m, "col"), maxColumns-1))
	a.Row = max(0, min(childInt(m, "row"), maxRows-1))
	a.DX, _ = ooxml.EMU(textOf(m.Child(nsXDR, "colOff")))
	a.DY, _ = ooxml.EMU(textOf(m.Child(nsXDR, "rowOff")))
	a.DX, a.DY = max(0, a.DX), max(0, a.DY)
	return a
}

// childInt returns the integer text of m's child local, 0 for none.
func childInt(m *ooxml.Element, local string) int {
	n, _ := ooxml.Int(textOf(m.Child(nsXDR, local)))
	return n
}

// extent reads an ext element's cx and cy (ECMA-376 Part 1, 20.1.7.3) in
// points.
func extent(e *ooxml.Element) (w, h float64) {
	w, _ = ooxml.EMU(attr(e, "cx"))
	h, _ = ooxml.EMU(attr(e, "cy"))
	return max(0, w), max(0, h)
}

// leaf is one picture or shape of an anchor, placed in the anchor's box
// by fractions of its width and height.
type leaf struct {
	x, y, w, h   float64
	rot          float64
	flipH, flipV bool
	pic          *layout.Picture
	crop         [4]float64
	geom         *shape.Geometry
	style        ooxml.ShapeStyle
	text         *shapeText
	// anchored marks a leaf that fills its anchor: its anchor holds the
	// box the rotation leaves, its width and height swapped for a quarter
	// turn (ECMA-376 Part 1, 20.5.2.33, as Excel writes rotated shapes).
	anchored bool
	// fx are the leaf's effects, nil for none.
	fx *css.OfficeEffects
}

// outer returns the box the leaf fills in the anchor's box rect.
func (l *leaf) outer(rect typeset.Rect) typeset.Rect {
	return typeset.Rect{X: rect.X + float64(l.x*rect.Width), Y: rect.Y + float64(l.y*rect.Height), Width: float64(l.w * rect.Width), Height: float64(l.h * rect.Height)}
}

// box returns the leaf's own box in the anchor's box rect: its outer box,
// turned a quarter for an anchored leaf turned a quarter.
func (l *leaf) box(rect typeset.Rect) typeset.Rect {
	outer := l.outer(rect)
	if l.anchored && quarterTurn(l.rot) {
		return typeset.Rect{X: outer.X + float64((outer.Width-outer.Height)/2), Y: outer.Y + float64((outer.Height-outer.Width)/2), Width: outer.Height, Height: outer.Width}
	}
	return outer
}

// content reads the object of anchor a of the drawing part into d: a
// chart frame as a chart, everything else as a picture drawing its leaves.
// It reports false when the anchor holds nothing to draw.
func (w *sheetReader) content(part string, a *ooxml.Element, d *sheet.Drawing) bool {
	for _, obj := range a.Kids() {
		if obj.Space != nsXDR {
			continue
		}
		switch obj.Local {
		case "graphicFrame":
			nv := obj.Find(nsXDR, "cNvPr")
			d.Name = attr(nv, "name")
			d.Alt, d.Decorative = ooxml.Describe(nv)
			d.Href = w.drawingLink(part, nv)
			data := obj.Find(ooxml.NSA, "graphicData")
			switch ooxml.GraphicURI(data) {
			case uriChart:
				id, _ := data.Child(nsC, "chart").AttrNS(ooxml.NSR, "id")
				rel, ok := w.r.pkg.RelationshipByID(w.r.ctx, part, id)
				if !ok || rel.External || !w.r.pkg.Has(rel.Target) {
					return false
				}
				d.Chart = w.r.chartOf(rel.Target)
				return true
			case uriChartEx:
				// An Office 2016 chart (MS-ODRAWXML, 2.24.3 cx:chart):
				// Excel caches no data for it, so the chart reads its
				// ranges through the workbook.
				id, _ := data.Child(ooxml.NSChartEx, "chart").AttrNS(ooxml.NSR, "id")
				rel, ok := w.r.pkg.RelationshipByID(w.r.ctx, part, id)
				if !ok || rel.External || !w.r.pkg.Has(rel.Target) {
					return false
				}
				c, err := ooxml.ParseChartEx(w.r.ctx, w.r.pkg, rel.Target)
				if err != nil {
					if w.r.stop(err) {
						return false
					}
					w.r.warn(warn.ContentUnsupported, warn.Omitted, fmt.Sprintf("the Office 2016 chart %q is left out: %v", d.Name, err))
					return false
				}
				for _, x := range c.Warnings {
					w.r.warn(x.Code, x.Effect, x.Message)
				}
				c.Resolver = w.r.rangeValues
				d.Chart = c
				return true
			case uriDiagram:
				w.r.warn(warn.ContentUnsupported, warn.Omitted, "a SmartArt diagram is left out: the workbook holds no picture of it")
			default:
				w.r.warn(warn.ContentUnsupported, warn.Omitted, "a drawing of an unknown kind is left out")
			}
			return false
		case "pic", "sp", "cxnSp", "grpSp":
			if hidden(obj) {
				return false
			}
			nv := obj.Find(nsXDR, "cNvPr")
			d.Name = attr(nv, "name")
			d.Alt, d.Decorative = ooxml.Describe(nv)
			d.Href = w.drawingLink(part, nv)
			var leaves []leaf
			w.leaves(part, obj, 0, 0, 1, 1, 0, &leaves)
			if len(leaves) == 1 {
				leaves[0].anchored = true
			}
			if len(leaves) == 0 {
				return false
			}
			d.Shape = !slices.ContainsFunc(leaves, func(l leaf) bool { return l.pic != nil })
			d.Picture = w.r.composite(leaves, d)
			return d.Picture != nil
		}
	}
	return false
}

// hidden reports a shape its non-visual properties hide (ECMA-376 Part 1,
// 20.1.2.2.8 cNvPr hidden).
func hidden(obj *ooxml.Element) bool {
	for _, nv := range obj.Kids() {
		if pr := nv.Child(nsXDR, "cNvPr"); pr != nil {
			return boolAttr(pr, "hidden")
		}
	}
	return false
}

// leaves collects the leaves of obj placed in the box (x, y, w, h), in
// fractions of the anchor's box: a top-level object fills it, the
// children of a group are mapped from the group's child coordinates
// (ECMA-376 Part 1, 20.1.7.5 chOff and chExt).
func (w *sheetReader) leaves(part string, obj *ooxml.Element, x, y, bw, bh float64, depth int, out *[]leaf) {
	r := w.r
	switch obj.Local {
	case "pic":
		ref := ooxml.ParsePicture(asPicture(obj))
		var pic *layout.Picture
		if ref.SVG != "" {
			pic = r.loadPicture(part, ref.SVG)
		}
		if pic == nil && ref.Embed != "" {
			pic = r.loadPicture(part, ref.Embed)
		}
		if pic == nil && ref.Link != "" {
			r.warn(warn.PictureMissing, warn.Omitted, "a linked picture is left out: linked files are not read")
		}
		pic = r.colorChange(pic, obj.Child(nsXDR, "blipFill").Child(ooxml.NSA, "blip"))
		if pic != nil {
			*out = append(*out, leaf{x: x, y: y, w: bw, h: bh, rot: ref.Rotation, flipH: ref.FlipH, flipV: ref.FlipV, pic: pic, crop: ref.Crop, fx: r.effectList(obj.Child(nsXDR, "spPr"), obj.Child(nsXDR, "style"))})
		}
	case "sp", "cxnSp":
		spPr := obj.Child(nsXDR, "spPr")
		g := ooxml.ParseGeometry(spPr)
		switch {
		case obj.Local == "cxnSp" && !shape.Known(g):
			// A connector the engine does not know joins its ends.
			g = &shape.Geometry{Preset: "line"}
		case !shape.Known(g):
			r.warn(warn.ContentUnsupported, warn.Approximated, fmt.Sprintf("a %s shape is drawn as its bounding box", g.Preset))
		default:
			if err := shape.Check(g); err != nil {
				r.warn(warn.ContentUnsupported, warn.Approximated, fmt.Sprintf("a shape's geometry is drawn in part: %v", err))
			}
		}
		l := leaf{x: x, y: y, w: bw, h: bh, geom: g, style: ooxml.ParseShapeStyle(spPr, obj.Child(nsXDR, "style"), r.theme), fx: r.effectList(spPr, obj.Child(nsXDR, "style"))}
		l.text = r.shapeTextOf(obj.Child(nsXDR, "txBody"), obj.Child(nsXDR, "style"))
		if xfrm := spPr.Child(ooxml.NSA, "xfrm"); xfrm != nil {
			l.rot, l.flipH, l.flipV = ooxml.Transform2D(xfrm)
		}
		*out = append(*out, l)
	case "grpSp":
		if depth >= maxGroupDepth {
			r.warn(warn.LimitReached, warn.Omitted, "shape groups nested too deep are left out")
			return
		}
		xfrm := obj.Child(nsXDR, "grpSpPr").Child(ooxml.NSA, "xfrm")
		cx, cy := emuAttr(xfrm.Child(ooxml.NSA, "chOff"), "x"), emuAttr(xfrm.Child(ooxml.NSA, "chOff"), "y")
		cw, ch := emuAttr(xfrm.Child(ooxml.NSA, "chExt"), "cx"), emuAttr(xfrm.Child(ooxml.NSA, "chExt"), "cy")
		if cw <= 0 || ch <= 0 {
			cw, ch = emuAttr(xfrm.Child(ooxml.NSA, "ext"), "cx"), emuAttr(xfrm.Child(ooxml.NSA, "ext"), "cy")
			cx, cy = emuAttr(xfrm.Child(ooxml.NSA, "off"), "x"), emuAttr(xfrm.Child(ooxml.NSA, "off"), "y")
		}
		for _, k := range obj.Kids() {
			if k.Space != nsXDR || hidden(k) {
				continue
			}
			switch k.Local {
			case "pic", "sp", "cxnSp", "grpSp":
			case "graphicFrame":
				r.warn(warn.ContentUnsupported, warn.Omitted, "a chart inside a shape group is left out")
				continue
			default:
				continue
			}
			kx := xfrmOf(k)
			if kx == nil || cw <= 0 || ch <= 0 {
				continue
			}
			ox, oy := emuAttr(kx.Child(ooxml.NSA, "off"), "x"), emuAttr(kx.Child(ooxml.NSA, "off"), "y")
			ew, eh := emuAttr(kx.Child(ooxml.NSA, "ext"), "cx"), emuAttr(kx.Child(ooxml.NSA, "ext"), "cy")
			w.leaves(part, k, x+float64((ox-cx)/cw*bw), y+float64((oy-cy)/ch*bh), ew/cw*bw, eh/ch*bh, depth+1, out)
		}
	}
}

// asPicture returns an xdr:pic as the DrawingML picture shape it mirrors
// (ECMA-376 Part 1, 20.5.2.25 pic, whose blipFill and spPr are the
// spreadsheet drawing's), so the shared picture reader finds its parts.
func asPicture(obj *ooxml.Element) *ooxml.Element {
	out := &ooxml.Element{Space: ooxml.NSPic, Local: "pic"}
	for _, c := range obj.Kids() {
		if c.Space == nsXDR {
			alias := *c
			alias.Space = ooxml.NSPic
			c = &alias
		}
		out.Children = append(out.Children, c)
	}
	return out
}

// xfrmOf returns the transform of a group member: its shape properties'
// a:xfrm, or its group properties' for a group.
func xfrmOf(k *ooxml.Element) *ooxml.Element {
	for _, pr := range []string{"spPr", "grpSpPr"} {
		if x := k.Child(nsXDR, pr).Child(ooxml.NSA, "xfrm"); x != nil {
			return x
		}
	}
	return nil
}

// emuAttr returns e's EMU attribute local in points, 0 when absent.
func emuAttr(e *ooxml.Element, local string) float64 {
	v, _ := ooxml.EMU(attr(e, local))
	return v
}

// composite returns the picture of an anchor's leaves: a lone picture
// without rotation or flips as itself, cropped, anything else as a vector
// picture drawing each leaf in its place, a shape's text over its
// outline. The picture's natural size is
// the anchor's when the anchor gives one, else the lone picture's.
func (r *reader) composite(leaves []leaf, d *sheet.Drawing) *layout.Picture {
	if len(leaves) == 1 && leaves[0].pic != nil && leaves[0].rot == 0 && !leaves[0].flipH && !leaves[0].flipV && leaves[0].fx == nil {
		pic := ooxml.Cropped(leaves[0].pic, leaves[0].crop)
		if d.To == nil && (d.Width <= 0 || d.Height <= 0) {
			d.Width, d.Height = pic.Width, pic.Height
		}
		return pic
	}
	w, h := d.Width, d.Height
	if w <= 0 || h <= 0 {
		w, h = 1, 1
	}
	out := &layout.Picture{Width: w, Height: h}
	fonts := r.fonts
	fx := slices.ContainsFunc(leaves, func(l leaf) bool { return l.fx != nil })
	pixels := &r.effectPixels
	late, _ := r.ctx.Value(lateKey{}).(*lateWarnings) // a type assertion, nil when absent
	// An outline is centred on its geometry, and Excel prints its outer
	// half past the anchor's box, a line's whole width along a row.
	grow := 0.0
	for _, l := range leaves {
		if l.geom != nil && (l.style.Line != nil || l.style.LineGradient != nil) {
			grow = max(grow, l.style.LineWidth/2)
		}
	}
	out.Vector = func(ctx context.Context, rect typeset.Rect) []typeset.Fragment {
		// The leaves are clipped to the anchor's box and their outlines'
		// outer half, but a turned one, which Excel draws whole about the
		// centre of the box its anchor holds ([leaf.whole]); each keeps its
		// place in the paint order.
		clip := typeset.Rect{X: rect.X - grow, Y: rect.Y - grow, Width: float64(rect.Width + 2*grow), Height: float64(rect.Height + 2*grow)}
		var drawn, frags []typeset.Fragment
		flush := func() {
			if len(frags) > 0 {
				drawn = append(drawn, &typeset.Group{Clip: ooxml.RectPath(clip), Content: frags})
				frags = nil
			}
		}
		for _, l := range leaves {
			if l.whole() {
				flush()
			}
			outer, box := l.outer(rect), l.box(rect)
			var content, upright []typeset.Fragment
			if l.pic != nil {
				pic := ooxml.Cropped(l.pic, l.crop)
				if pic.Vector != nil {
					content = pic.Vector(ctx, box)
				} else if pic.Source != nil {
					content = []typeset.Fragment{&typeset.Image{Rect: box, Source: pic.Source}}
				}
			} else {
				content = opaque(l.style.Paint(l.geom, box))
				// The text sits in the geometry's text rectangle (ECMA-376
				// Part 1, 20.1.9.22 rect), as LibreOffice lays it out; text
				// kept upright in a turned shape keeps the outer box.
				switch {
				case l.text != nil && l.text.upright:
					upright = l.text.draw(ctx, fonts, outer)
				case l.text != nil:
					content = append(content, l.text.draw(ctx, fonts, shape.TextRect(l.geom, box))...)
				}
			}
			if m, ok := transform(box, l.rot, l.flipH, l.flipV); ok && len(content) > 0 {
				frags = append(frags, &typeset.Group{Transform: m, Content: content})
			} else {
				frags = append(frags, content...)
			}
			frags = append(frags, upright...)
			if l.whole() {
				drawn, frags = append(drawn, frags...), nil
			}
		}
		flush()
		if !fx {
			return drawn
		}
		// The effects reach past the anchor's box, outside its clip.
		return append(underEffects(ctx, leaves, rect, pixels, late), drawn...)
	}
	return out
}

// whole reports a leaf drawn whole, past its anchor's box: a turned shape
// or picture, whose anchor holds its unturned box, the box with its
// width and height swapped for a quarter turn ([leaf.box]), and which
// Excel turns about that box's centre and prints entire, its corners
// past the anchor included (ECMA-376 Part 1, 20.5.2.33 twoCellAnchor;
// 20.1.7.6 xfrm rot).
//
// # Corpus evidence
//
//   - libreoffice/xlsx: sc/tdf142881.xlsx prints two snipped rectangles
//     turned 135 and 315 degrees, 251 by 32 points, whose anchors hold
//     them unturned, as long diagonal bars reaching far past their
//     anchors' boxes, with their text.
//   - libreoffice/xlsx: sc/tdf135828_Shape_Rect.xlsx prints an up arrow
//     turned 77 degrees at its own size, its anchor holding the box a
//     quarter turn swaps.
func (l *leaf) whole() bool {
	return math.Mod(l.rot, 360) != 0
}

// quarterTurn reports a rotation of 45 to 135 or 225 to 315 degrees, for
// which a shape's anchor holds its box turned a quarter.
func quarterTurn(rot float64) bool {
	rot = math.Mod(math.Mod(rot, 360)+360, 360)
	return rot >= 45 && rot < 135 || rot >= 225 && rot < 315
}

// opaque returns the paths of frags without their fully transparent fill
// or outline, which typeset refuses, and without the paths left painting
// nothing.
func opaque(frags []typeset.Fragment) []typeset.Fragment {
	out := frags[:0]
	for _, f := range frags {
		if p, ok := f.(*typeset.Path); ok {
			if p.Fill != nil && p.Fill.Shading == nil && p.Fill.Color.A <= 0 {
				p.Fill = nil
			}
			if p.Stroke != nil && p.Stroke.Color.A <= 0 {
				p.Stroke = nil
			}
			if p.Fill == nil && p.Stroke == nil {
				continue
			}
		}
		out = append(out, f)
	}
	return out
}

// transform returns the matrix rotating box clockwise by rot degrees and
// mirroring it about its centre (ECMA-376 Part 1, 20.1.7.6), false when it
// changes nothing.
func transform(box typeset.Rect, rot float64, flipH, flipV bool) (typeset.Matrix, bool) {
	if rot == 0 && !flipH && !flipV {
		return typeset.Matrix{}, false
	}
	cx, cy := box.X+float64(box.Width/2), box.Y+float64(box.Height/2)
	sx, sy := 1.0, 1.0
	if flipH {
		sx = -1
	}
	if flipV {
		sy = -1
	}
	// Page coordinates grow downwards, so a clockwise turn is a positive
	// angle.
	rad := rot * math.Pi / 180
	cos, sin := detmath.Cos(rad), detmath.Sin(rad)
	a, b, c, dd := cos*sx, sin*sx, -sin*sy, cos*sy
	return typeset.Matrix{A: a, B: b, C: c, D: dd, E: cx - float64(a*cx) - float64(c*cy), F: cy - float64(b*cx) - float64(dd*cy)}, true
}

// loadPicture decodes the picture the relationship id of part names, once
// per target, nil with a warning when it cannot be drawn.
func (r *reader) loadPicture(part, id string) *layout.Picture {
	rel, ok := r.pkg.RelationshipByID(r.ctx, part, id)
	if !ok || rel.External {
		if ok {
			r.warn(warn.PictureMissing, warn.Omitted, "a linked picture is left out: linked files are not read")
		}
		return nil
	}
	if pic, ok := r.pictures[rel.Target]; ok {
		return pic
	}
	if len(r.pictures) >= limits.MaxConvertPictures {
		r.warn(warn.LimitReached, warn.Omitted, fmt.Sprintf("pictures past %d are not drawn", limits.MaxConvertPictures))
		return nil
	}
	r.pictures[rel.Target] = nil
	data, err := r.pkg.Read(rel.Target)
	if err != nil {
		if r.stop(err) {
			return nil
		}
		r.warn(warn.PictureMissing, warn.Omitted, fmt.Sprintf("picture %s cannot be read: %v", rel.Target, err))
		return nil
	}
	pic, err := ooxml.DecodePicture(r.ctx, data, r.fonts, rel.Target)
	if err != nil {
		switch {
		case r.stop(err):
		case errors.Is(err, ooxml.ErrUnsupportedPicture):
			r.warn(warn.PictureFormatUnsupported, warn.Omitted, fmt.Sprintf("picture %s is left out: %v", rel.Target, err))
		default:
			r.warn(warn.PictureUnreadable, warn.Omitted, fmt.Sprintf("picture %s does not decode: %v", rel.Target, err))
		}
		return nil
	}
	r.pictures[rel.Target] = pic
	return pic
}
