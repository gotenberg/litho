package xlsx

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/gotenberg/litho/pkg/internal/chart"
	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// nsXDR is the spreadsheet drawing namespace (ECMA-376 Part 1, 20.5).
const nsXDR = "http://schemas.openxmlformats.org/drawingml/2006/spreadsheetDrawing"

// The graphic data URIs of a graphic frame (ECMA-376 Part 1, 20.1.2.2.17).
const (
	uriChart   = "http://schemas.openxmlformats.org/drawingml/2006/chart"
	uriDiagram = "http://schemas.openxmlformats.org/drawingml/2006/diagram"
)

// nsC is the DrawingML chart namespace (ECMA-376 Part 1, 21.2).
const nsC = "http://schemas.openxmlformats.org/drawingml/2006/chart"

// maxTotalPixels caps the pixels one conversion decodes, every picture
// together: 2^28, 1 GiB of RGBA. Anti-DoS guard, not spec-mandated.
const maxTotalPixels = 1 << 28

// maxDrawings caps the drawings of one sheet. Anti-DoS guard, not
// spec-mandated: a dashboard holds a few dozen pictures and charts.
const maxDrawings = 4096

// maxGroupDepth caps the nesting of shape groups walked. Anti-DoS guard,
// not spec-mandated: groups in groups rarely pass a few levels.
const maxGroupDepth = 16

// chartOf returns the chart of the chart part named part, the one place
// the DrawingML chart reader plugs into. Until it does, the chart is the
// empty placeholder, reported as not drawn.
func (r *reader) chartOf(part string) *chart.Chart {
	r.warn("unsupported-content", fmt.Sprintf("chart %s is not drawn", part))
	return &chart.Chart{}
}

// drawing reads the drawing part the sheet's relationship id names
// (ECMA-376 Part 1, 20.5.2.35 wsDr): each anchor in drawing order, its
// pictures and shapes as one picture, its chart as a chart. Legacy VML
// drawings, which hold comments, are not read: comments do not print.
func (w *sheetReader) drawing(id string) error {
	r := w.r
	rel, ok := r.pkg.RelationshipByID(w.part, id)
	if !ok || rel.External || rel.Type != relDrawing || !r.pkg.Has(rel.Target) {
		return nil
	}
	root, err := r.pkg.ParseXML(r.ctx, rel.Target, understood)
	if err != nil {
		if isMalformed(err) {
			r.warn("malformed-part", fmt.Sprintf("the drawings of sheet %q do not parse: %v", w.sh.Name, err))
			return nil
		}
		return err
	}
	for _, a := range root.Kids() {
		if len(w.sh.Drawings) >= maxDrawings {
			r.warn("too-large", fmt.Sprintf("sheet %q holds more than %d drawings: the rest are left out", w.sh.Name, maxDrawings))
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
	geom         string
	style        ooxml.ShapeStyle
	text         *shapeText
	// anchored marks a leaf that fills its anchor: its anchor holds the
	// box the rotation leaves, its width and height swapped for a quarter
	// turn (ECMA-376 Part 1, 20.5.2.33, as Excel writes rotated shapes).
	anchored bool
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
			d.Name = attr(obj.Find(nsXDR, "cNvPr"), "name")
			data := obj.Find(ooxml.NSA, "graphicData")
			switch attr(data, "uri") {
			case uriChart:
				id, _ := data.Child(nsC, "chart").AttrNS(ooxml.NSR, "id")
				rel, ok := w.r.pkg.RelationshipByID(part, id)
				if !ok || rel.External || !w.r.pkg.Has(rel.Target) {
					return false
				}
				d.Chart = w.r.chartOf(rel.Target)
				return true
			case uriDiagram:
				w.r.warn("unsupported-content", "a SmartArt diagram is left out: the workbook holds no picture of it")
			default:
				w.r.warn("unsupported-content", "a drawing of an unknown kind is left out")
			}
			return false
		case "pic", "sp", "cxnSp", "grpSp":
			if hidden(obj) {
				return false
			}
			d.Name = attr(obj.Find(nsXDR, "cNvPr"), "name")
			var leaves []leaf
			w.leaves(part, obj, 0, 0, 1, 1, 0, &leaves)
			if len(leaves) == 1 {
				leaves[0].anchored = true
			}
			if len(leaves) == 0 {
				return false
			}
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
			r.warn("unsupported-content", "a linked picture is left out: linked files are not read")
		}
		if pic != nil {
			*out = append(*out, leaf{x: x, y: y, w: bw, h: bh, rot: ref.Rotation, flipH: ref.FlipH, flipV: ref.FlipV, pic: pic, crop: ref.Crop})
		}
	case "sp", "cxnSp":
		spPr := obj.Child(nsXDR, "spPr")
		geom := "rect"
		if g := spPr.Child(ooxml.NSA, "prstGeom"); g != nil {
			geom = attr(g, "prst")
		} else if spPr.Child(ooxml.NSA, "custGeom") != nil {
			geom = "custom"
		}
		if obj.Local == "cxnSp" && geom != "line" {
			if _, ok := ooxml.PresetPath(geom, compose.Rect{Width: 1, Height: 1}); !ok {
				geom = "line"
			}
		}
		if _, ok := ooxml.PresetPath(geom, compose.Rect{Width: 1, Height: 1}); !ok {
			r.warn("approximated", fmt.Sprintf("a %s shape is drawn as its bounding box", geom))
		}
		l := leaf{x: x, y: y, w: bw, h: bh, geom: geom, style: ooxml.ParseShapeStyle(spPr, obj.Child(nsXDR, "style"), r.theme)}
		l.text = r.shapeTextOf(obj.Child(nsXDR, "txBody"), obj.Child(nsXDR, "style"))
		if xfrm := spPr.Child(ooxml.NSA, "xfrm"); xfrm != nil {
			l.rot, l.flipH, l.flipV = ooxml.Transform2D(xfrm)
		}
		*out = append(*out, l)
	case "grpSp":
		if depth >= maxGroupDepth {
			r.warn("too-large", "shape groups nested too deep are left out")
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
				r.warn("unsupported-content", "a chart inside a shape group is left out")
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
			w.leaves(part, k, x+(ox-cx)/cw*bw, y+(oy-cy)/ch*bh, ew/cw*bw, eh/ch*bh, depth+1, out)
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
	if len(leaves) == 1 && leaves[0].pic != nil && leaves[0].rot == 0 && !leaves[0].flipH && !leaves[0].flipV {
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
	out.Vector = func(ctx context.Context, rect compose.Rect) []compose.Fragment {
		var frags []compose.Fragment
		for _, l := range leaves {
			outer := compose.Rect{X: rect.X + l.x*rect.Width, Y: rect.Y + l.y*rect.Height, Width: l.w * rect.Width, Height: l.h * rect.Height}
			box := outer
			if l.anchored && quarterTurn(l.rot) {
				box = compose.Rect{X: outer.X + (outer.Width-outer.Height)/2, Y: outer.Y + (outer.Height-outer.Width)/2, Width: outer.Height, Height: outer.Width}
			}
			var content, upright []compose.Fragment
			if l.pic != nil {
				pic := ooxml.Cropped(l.pic, l.crop)
				if pic.Vector != nil {
					content = pic.Vector(ctx, box)
				} else if pic.Source != nil {
					content = []compose.Fragment{&compose.Image{Rect: box, Source: pic.Source}}
				}
			} else {
				ops, _ := ooxml.PresetPath(l.geom, box)
				content = opaque(l.style.Paint(ops))
				switch {
				case l.text != nil && l.text.upright:
					upright = l.text.draw(ctx, fonts, outer)
				case l.text != nil:
					content = append(content, l.text.draw(ctx, fonts, box)...)
				}
			}
			if m, ok := transform(box, l.rot, l.flipH, l.flipV); ok && len(content) > 0 {
				frags = append(frags, &compose.Group{Transform: m, Content: content})
			} else {
				frags = append(frags, content...)
			}
			frags = append(frags, upright...)
		}
		return []compose.Fragment{&compose.Group{Clip: ooxml.RectPath(rect), Content: frags}}
	}
	return out
}

// quarterTurn reports a rotation of 45 to 135 or 225 to 315 degrees, for
// which a shape's anchor holds its box turned a quarter.
func quarterTurn(rot float64) bool {
	rot = math.Mod(math.Mod(rot, 360)+360, 360)
	return rot >= 45 && rot < 135 || rot >= 225 && rot < 315
}

// opaque returns the paths of frags without their fully transparent fill
// or outline, which compose refuses, and without the paths left painting
// nothing.
func opaque(frags []compose.Fragment) []compose.Fragment {
	out := frags[:0]
	for _, f := range frags {
		if p, ok := f.(*compose.Path); ok {
			if p.Fill != nil && p.Fill.Color.A <= 0 {
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
func transform(box compose.Rect, rot float64, flipH, flipV bool) (compose.Matrix, bool) {
	if rot == 0 && !flipH && !flipV {
		return compose.Matrix{}, false
	}
	cx, cy := box.X+box.Width/2, box.Y+box.Height/2
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
	cos, sin := math.Cos(rad), math.Sin(rad)
	a, b, c, dd := cos*sx, sin*sx, -sin*sy, cos*sy
	return compose.Matrix{A: a, B: b, C: c, D: dd, E: cx - a*cx - c*cy, F: cy - b*cx - dd*cy}, true
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
