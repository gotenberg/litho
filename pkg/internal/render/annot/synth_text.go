package annot

import (
	"context"
	"log/slog"
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
)

// noteIconSize is the side, in default user space units, of the square icon
// [synthText] draws for a Text annotation. ISO 32000-1, 12.5.6.4 leaves the
// icon's look and size to the conforming reader; poppler 26.09.0 draws its
// icons 24 units square, PDFium 20 and MuPDF 18. The render gate's oracle is
// poppler, so the icon takes its size.
const noteIconSize = 24

// noteIconStroke is the width of the icon's frame and of the strokes of its
// symbol.
const noteIconStroke = 1.5

// noteIconRadius is the radius of the icon frame's rounded corners.
const noteIconRadius = 3

// darkBackground is the luminance under which an icon's background takes a
// white symbol rather than a black one, so the symbol stays legible on the
// colour /C names.
const darkBackground = 0.5

// synthText draws the icon a closed Text annotation shows (ISO 32000-1,
// 12.5.6.4, Table 172) for /Name: Comment, Key, Note, Help, NewParagraph,
// Paragraph or Insert. Table 172 makes Note the default, and a name outside
// the table, which a viewer has no icon for, draws as Note too.
//
// The table names the icons and leaves their look to the viewer, so these are
// litho's own: a symbol in a rounded frame noteIconSize units square. Text
// annotations do not scale or rotate with the page (12.5.6.4: they behave as
// if the NoZoom and NoRotate flags of Table 165 were set), and Table 165 keeps
// such an annotation's upper-left corner where /Rect puts it: the icon hangs
// from that corner at its own size whatever the size of /Rect, as poppler
// 26.09.0 places it. PDFium draws its icon from the lower-left corner and
// MuPDF centres it in /Rect; the table is followed here.
//
// /C is the colour of the icon's background (Table 164); without it, or with
// an empty array, the background is not painted. The frame and the symbol are
// black, or white over a dark background.
func synthText(ctx context.Context, doc *document.Document, dict ast.PdfDict, rect geom.Rect) (drawing, bool) {
	name, err := document.ResolveName(ctx, doc, dict["Name"])
	if err != nil {
		doc.Logger.Debug("unreadable Text annotation /Name, drawing the Note icon", slog.Any("err", err))
	}
	background := readColour(ctx, doc, dict["C"], nil)
	ink := black
	if background != nil && luminance(background) < darkBackground {
		ink = white
	}
	origin := geom.Point{X: rect.Min.X, Y: rect.Max.Y - noteIconSize}
	d := drawing{
		stroke: ink, fill: background, width: noteIconStroke,
		clip: geom.Rect{Min: origin, Max: geom.Point{X: origin.X + noteIconSize, Y: origin.Y + noteIconSize}},
	}
	ic := icon{d: &d, origin: origin, ink: ink}

	var frame geom.Path
	h := noteIconStroke / 2
	ic.roundRect(&frame, h, h, noteIconSize-h, noteIconSize-h, noteIconRadius)
	d.add(frame, paintFillStroke)

	switch name {
	case "Comment":
		ic.comment()
	case "Key":
		ic.key()
	case "Help":
		ic.help()
	case "NewParagraph":
		ic.pilcrow(-3)
		ic.plus()
	case "Paragraph":
		ic.pilcrow(0)
	case "Insert":
		ic.caret()
	default:
		ic.note()
	}
	return d, true
}

// luminance approximates the relative luminance of a colour of 1, 3 or 4
// components in DeviceGray, DeviceRGB or DeviceCMYK, with the Rec. 709
// weights and CMYK taken through its naive complement.
func luminance(c []float64) float64 {
	switch len(c) {
	case 1:
		return c[0]
	case 3:
		return 0.2126*c[0] + 0.7152*c[1] + 0.0722*c[2]
	case 4:
		k := 1 - c[3]
		return 0.2126*(1-c[0])*k + 0.7152*(1-c[1])*k + 0.0722*(1-c[2])*k
	}
	return 1
}

// icon draws the symbol of a Text annotation's icon into d, in a square of
// noteIconSize units whose lower-left corner is origin.
type icon struct {
	d      *drawing
	origin geom.Point
	ink    []float64
}

// at returns the point (x, y) of the icon's square in default user space.
func (ic icon) at(x, y float64) geom.Point {
	return geom.Point{X: ic.origin.X + x, Y: ic.origin.Y + y}
}

// polyline strokes the points joined by straight lines, closed when asked.
func (ic icon) polyline(closed bool, pts ...[2]float64) {
	var p geom.Path
	p.MoveTo(ic.at(pts[0][0], pts[0][1]))
	for _, pt := range pts[1:] {
		p.LineTo(ic.at(pt[0], pt[1]))
	}
	if closed {
		p.Close()
	}
	ic.d.add(p, paintStroke)
}

// dot fills a disc of radius r around (x, y) in the symbol's colour.
func (ic icon) dot(x, y, r float64) {
	var p geom.Path
	ellipse(&p, ic.at(x, y), r, r)
	ic.d.addInk(p, ic.ink)
}

// roundRect adds the rectangle from (x0, y0) to (x1, y1) with corners rounded
// to radius r.
func (ic icon) roundRect(p *geom.Path, x0, y0, x1, y1, r float64) {
	k := r * (1 - bezierCircle)
	p.MoveTo(ic.at(x0+r, y0))
	p.LineTo(ic.at(x1-r, y0))
	p.CubicTo(ic.at(x1-k, y0), ic.at(x1, y0+k), ic.at(x1, y0+r))
	p.LineTo(ic.at(x1, y1-r))
	p.CubicTo(ic.at(x1, y1-k), ic.at(x1-k, y1), ic.at(x1-r, y1))
	p.LineTo(ic.at(x0+r, y1))
	p.CubicTo(ic.at(x0+k, y1), ic.at(x0, y1-k), ic.at(x0, y1-r))
	p.LineTo(ic.at(x0, y0+r))
	p.CubicTo(ic.at(x0, y0+k), ic.at(x0+k, y0), ic.at(x0+r, y0))
	p.Close()
}

// note draws a sheet with three lines of text on it.
func (ic icon) note() {
	ic.polyline(true, [2]float64{7, 4.5}, [2]float64{17, 4.5}, [2]float64{17, 19.5}, [2]float64{7, 19.5})
	for _, y := range []float64{15.5, 12, 8.5} {
		ic.polyline(false, [2]float64{9.5, y}, [2]float64{14.5, y})
	}
}

// comment draws a speech bubble, its tail at the lower left.
func (ic icon) comment() {
	ic.polyline(true,
		[2]float64{5, 18.5}, [2]float64{19, 18.5}, [2]float64{19, 9.5},
		[2]float64{11.5, 9.5}, [2]float64{7.5, 5.5}, [2]float64{8.5, 9.5}, [2]float64{5, 9.5})
}

// key draws a key: a ring at the upper left, a shaft down to the right and
// two teeth.
func (ic icon) key() {
	var ring geom.Path
	ellipse(&ring, ic.at(8.5, 15.5), 3.5, 3.5)
	ic.d.add(ring, paintStroke)
	ic.polyline(false, [2]float64{11, 13}, [2]float64{19, 5})
	ic.polyline(false, [2]float64{16.5, 7.5}, [2]float64{18.5, 9.5})
	ic.polyline(false, [2]float64{14, 10}, [2]float64{15.75, 11.75})
}

// help draws a question mark.
func (ic icon) help() {
	var p geom.Path
	p.MoveTo(ic.at(8.5, 15))
	p.CubicTo(ic.at(8.5, 20), ic.at(15.5, 20), ic.at(15.5, 15.5))
	p.CubicTo(ic.at(15.5, 12.5), ic.at(12, 12.5), ic.at(12, 9.5))
	ic.d.add(p, paintStroke)
	ic.dot(12, 6, 1.25)
}

// pilcrow draws a paragraph mark moved dx units along x: a filled bowl
// hanging from a bar, and two stems.
func (ic icon) pilcrow(dx float64) {
	var bowl geom.Path
	bowl.MoveTo(ic.at(dx+12.5, 19.5))
	bowl.LineTo(ic.at(dx+11, 19.5))
	bowl.CubicTo(ic.at(dx+9.067, 19.5), ic.at(dx+7.5, 17.933), ic.at(dx+7.5, 16))
	bowl.CubicTo(ic.at(dx+7.5, 14.067), ic.at(dx+9.067, 12.5), ic.at(dx+11, 12.5))
	bowl.LineTo(ic.at(dx+12.5, 12.5))
	bowl.Close()
	ic.d.addInk(bowl, ic.ink)
	ic.polyline(false, [2]float64{dx + 11, 19.5}, [2]float64{dx + 17, 19.5})
	ic.polyline(false, [2]float64{dx + 12.5, 19.5}, [2]float64{dx + 12.5, 4.5})
	ic.polyline(false, [2]float64{dx + 15.5, 19.5}, [2]float64{dx + 15.5, 4.5})
}

// plus draws a plus sign at the upper right, the mark of a new paragraph.
func (ic icon) plus() {
	ic.polyline(false, [2]float64{16.5, 15}, [2]float64{21, 15})
	ic.polyline(false, [2]float64{18.75, 12.75}, [2]float64{18.75, 17.25})
}

// caret draws the proofreader's insertion mark.
func (ic icon) caret() {
	ic.polyline(false, [2]float64{6, 7}, [2]float64{12, 17}, [2]float64{18, 7})
}

// noRotateBox returns box turned about pivot as a page turned by rotate
// degrees clockwise draws a NoRotate annotation (ISO 32000-1, 12.5.3, Table
// 165): upright on the turned page, which in default user space is the box
// turned rotate degrees counterclockwise. rotate is a multiple of 90.
func noRotateBox(box geom.Rect, pivot geom.Point, rotate int) geom.Rect {
	rotate = ((rotate % 360) + 360) % 360
	if rotate == 0 {
		return box
	}
	sin, cos := math.Sincos(float64(rotate) * math.Pi / 180)
	sin, cos = math.Round(sin), math.Round(cos)
	turn := func(p geom.Point) geom.Point {
		dx, dy := p.X-pivot.X, p.Y-pivot.Y
		return geom.Point{X: pivot.X + dx*cos - dy*sin, Y: pivot.Y + dx*sin + dy*cos}
	}
	a, b := turn(box.Min), turn(box.Max)
	return geom.Rect{
		Min: geom.Point{X: min(a.X, b.X), Y: min(a.Y, b.Y)},
		Max: geom.Point{X: max(a.X, b.X), Y: max(a.Y, b.Y)},
	}
}
