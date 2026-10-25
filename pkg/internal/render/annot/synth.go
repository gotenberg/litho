package annot

import (
	"context"
	"log/slog"
	"math"
	"slices"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/render/geom"
)

// maxSynthPoints caps the vertices one synthesised appearance reads from
// /Vertices, /InkList, /QuadPoints or a dash array together. Anti-DoS guard,
// not spec-mandated: each vertex becomes a few bytes of content the renderer
// parses again, so a crafted million-point /InkList would cost memory for no
// visible gain. A freehand stroke samples the pointer some hundred times a
// second, so 1<<17 vertices hold minutes of drawing in one annotation.
const maxSynthPoints = 1 << 17

// maxDashEntries caps the dash array of a synthesised border. Anti-DoS guard,
// not spec-mandated: ISO 32000-1, 12.5.4 shows dash arrays of one to three
// numbers, and poppler 26.09.0 reads at most 10.
const maxDashEntries = 16

// maxSquiggleSteps caps the zig-zag segments of one /Squiggly quadrilateral.
// Anti-DoS guard, not spec-mandated: the zig-zag steps 2 units, so a crafted
// quadrilateral a billion units wide would loop a billion times; past the
// cap the step widens. 1<<12 steps cover a line 8192 units long, wider than
// the largest page ISO 32000-1, Annex C allows (14400 units, where a squiggle
// that long is a texture, not a mark).
const maxSquiggleSteps = 1 << 12

// ctxCheckPoints is how many numbers [readNumbers] reads between context
// checks.
const ctxCheckPoints = 4096

// miterReach is how far past a path's points, in line widths, a stroke can
// reach: half a width, times the default miter limit of 10 at a sharp join
// (ISO 32000-1, 8.4.3.5, Table 52). A synthesised form's /BBox reaches that
// far so its clip never cuts the stroke.
const miterReach = 5

// bezierCircle is the control-point distance, as a fraction of the radius,
// of the four-segment cubic Bézier approximation of a circle: 4/3 tan(π/8).
const bezierCircle = 0.5522847498

// paint is how a synthesised path is painted.
type paint uint8

const (
	paintStroke paint = iota
	paintFill
	paintFillStroke
)

// shape is one synthesised path and how it is painted.
type shape struct {
	path  geom.Path
	paint paint
	// fill, when set, is the colour this shape fills with in place of the
	// drawing's.
	fill []float64
}

// drawing is a synthesised appearance: paths in default user space and the
// graphics state they are painted with. A nil colour paints nothing.
type drawing struct {
	shapes  []shape
	stroke  []float64
	fill    []float64
	width   float64
	dash    []float64
	opacity float64
	// multiply composites the drawing in the Multiply blend mode
	// (ISO 32000-1, 11.3.5.2, Table 136).
	multiply bool
	// clip is the form's /BBox, in default user space.
	clip geom.Rect
}

// border is an annotation's border style: its width and dash array (ISO
// 32000-1, 12.5.4), and whether /BS /S is /U.
type border struct {
	width     float64
	dash      []float64
	underline bool
}

// SynthesizedBox returns two boxes, in default user space, of the appearance
// a viewer synthesises for the annotation dict, what [Collect] draws under
// [Options.Synthesize] when the file holds no appearance stream for it: ink,
// a box the ink of its strokes and fills cannot pass, and path, the box of
// the points its paths pass through. It reads the annotation whatever its
// flags. It returns false when dict has an appearance stream, when its /Rect
// does not read, or when no appearance is synthesised for it.
//
// A line, a polygon, ink or a quadrilateral is drawn from /L, /Vertices,
// /InkList or /QuadPoints (ISO 32000-1, 12.5.6.7, 12.5.6.9, 12.5.6.10,
// 12.5.6.13), so its boxes may lie well apart from /Rect: viewers draw it
// there whatever /Rect says. A Text annotation's icon hangs from the
// upper-left corner of /Rect at its own size (12.5.6.4), so it may reach past
// a small /Rect.
//
// rotate is the /Rotate of the page the annotation lies on, in degrees
// clockwise. An appearance that does not rotate with the page, a Text
// annotation's icon (12.5.6.4) or one with the NoRotate flag (Table 165),
// stays upright on the turned page, turned about the upper-left corner of
// /Rect as the renderer draws it, and its boxes turn with it.
func SynthesizedBox(ctx context.Context, doc *document.Document, dict ast.PdfDict, rotate int) (ink, path geom.Rect, ok bool) {
	if _, _, _, has := normalAppearance(ctx, doc, dict); has {
		return geom.Rect{}, geom.Rect{}, false
	}
	rect, ok := readRect(ctx, doc, dict["Rect"])
	if !ok {
		return geom.Rect{}, geom.Rect{}, false
	}
	subtype, err := document.ResolveName(ctx, doc, dict["Subtype"])
	if err != nil {
		doc.Logger.Debug("unreadable annotation /Subtype", slog.Any("err", err))
	}
	d, ok := synthDrawing(ctx, doc, dict, subtype, rect)
	if !ok {
		return geom.Rect{}, geom.Rect{}, false
	}
	ink, path = d.inkBounds(), d.pathBounds()
	if subtype == "Square" || subtype == "Circle" || subtype == "Text" {
		// [synthesize] clips a Square or a Circle to /Rect, and a Text
		// annotation's icon to its square.
		ink = geom.Rect{
			Min: geom.Point{X: max(ink.Min.X, d.clip.Min.X), Y: max(ink.Min.Y, d.clip.Min.Y)},
			Max: geom.Point{X: min(ink.Max.X, d.clip.Max.X), Y: min(ink.Max.Y, d.clip.Max.Y)},
		}
	}
	if noRotate(ctx, doc, dict, subtype) {
		pivot := geom.Point{X: rect.Min.X, Y: rect.Max.Y}
		ink, path = noRotateBox(ink, pivot, rotate), noRotateBox(path, pivot, rotate)
	}
	return ink, path, true
}

// noRotate reports whether the synthesised appearance of the annotation dict
// stays upright as its page turns: a Text annotation's icon, which behaves as
// if the NoRotate flag were set (ISO 32000-1, 12.5.6.4), or an annotation
// with the flag set (12.5.3, Table 165).
func noRotate(ctx context.Context, doc *document.Document, dict ast.PdfDict, subtype ast.PdfName) bool {
	if subtype == "Text" {
		return true
	}
	f, err := document.ResolveObject(ctx, doc, dict["F"])
	if err != nil {
		doc.Logger.Debug("unreadable annotation /F, reading it as 0", slog.Any("err", err))
		return false
	}
	flags, ok := f.(ast.PdfInt)
	return ok && flags&FlagNoRotate != 0
}

// inkBounds returns the box the drawing's ink reaches, shape by shape. The
// content [drawing.content] writes keeps the default butt caps and miter
// joins with a miter limit of 10 (ISO 32000-1, 8.4.3, Table 52), so a stroke
// reaches half a width past a path of one segment and [miterReach] widths past
// one whose segments join. [drawing.bounds] takes the far reach for every
// shape, which suits a clip that must never cut the stroke but would make a
// straight line reach five widths past its ends.
func (d *drawing) inkBounds() geom.Rect {
	var (
		out   geom.Rect
		found bool
	)
	for _, s := range d.shapes {
		b, ok := s.path.Bounds()
		if !ok {
			continue
		}
		if s.paint != paintFill && d.strokes() {
			reach := d.width / 2
			if joins(s.path) {
				reach = miterReach * d.width
			}
			b.Min.X -= reach
			b.Min.Y -= reach
			b.Max.X += reach
			b.Max.Y += reach
		}
		if !found {
			out, found = b, true
			continue
		}
		out = union(out, b)
	}
	return out
}

// joins reports whether two segments of path meet: a subpath of more than one
// segment, or a closed one.
func joins(path geom.Path) bool {
	segments := 0
	for _, op := range path.Ops {
		switch op {
		case geom.MoveTo:
			segments = 0
		case geom.LineTo, geom.CubicTo:
			segments++
			if segments > 1 {
				return true
			}
		case geom.Close:
			return true
		}
	}
	return false
}

// synthesize builds the appearance a viewer draws for an annotation with no
// appearance stream, for the subtypes whose appearance ISO 32000-1 defines by
// the dictionary alone: Square and Circle (12.5.6.8), Line (12.5.6.7),
// Polygon and PolyLine (12.5.6.9), Ink (12.5.6.13), the text markup types
// (12.5.6.10) and a Link's border (12.5.6.5), and for a Text annotation, the
// predefined icon its /Name names (12.5.6.4, Table 172), drawn as litho's own
// where a viewer draws its own. It returns false for any other subtype, whose
// look needs a font or an icon of no predefined name, and for a dictionary
// whose geometry does not read.
func synthesize(ctx context.Context, doc *document.Document, dict ast.PdfDict, subtype ast.PdfName, rect geom.Rect) (Appearance, bool) {
	d, ok := synthDrawing(ctx, doc, dict, subtype, rect)
	if !ok {
		return Appearance{}, false
	}
	if subtype != "Square" && subtype != "Circle" && subtype != "Text" {
		// A line, a polygon, ink or a quadrilateral may reach past /Rect,
		// which producers often leave stale: poppler 26.09.0 grows the
		// box to cover the drawing, and so does this.
		d.clip = union(rect, d.bounds())
	}
	d.opacity = readOpacity(ctx, doc, dict)
	return d.appearance(rect), true
}

// synthDrawing builds the drawing [synthesize] serialises, false for a
// subtype it does not draw or a dictionary whose geometry does not read.
func synthDrawing(ctx context.Context, doc *document.Document, dict ast.PdfDict, subtype ast.PdfName, rect geom.Rect) (drawing, bool) {
	var (
		d  drawing
		ok bool
	)
	switch subtype {
	case "Square", "Circle":
		d, ok = synthGeometry(ctx, doc, dict, subtype, rect)
	case "Line":
		d, ok = synthLine(ctx, doc, dict)
	case "Polygon", "PolyLine":
		d, ok = synthPoly(ctx, doc, dict, subtype)
	case "Ink":
		d, ok = synthInk(ctx, doc, dict)
	case "Highlight", "Underline", "StrikeOut", "Squiggly":
		d, ok = synthMarkup(ctx, doc, dict, subtype)
	case "Link":
		d, ok = synthLink(ctx, doc, dict, rect)
	case "Text":
		d, ok = synthText(ctx, doc, dict, rect)
	}
	return d, ok && len(d.shapes) > 0
}

// synthGeometry draws a Square or a Circle (ISO 32000-1, 12.5.6.8): the
// rectangle, or the ellipse inscribed in it, that /RD insets from /Rect,
// stroked in /C with the border's width and dash, and filled with /IC.
// The cloudy border effect /BE asks for (12.5.4, Table 167) is drawn as the
// plain border it decorates.
func synthGeometry(ctx context.Context, doc *document.Document, dict ast.PdfDict, subtype ast.PdfName, rect geom.Rect) (drawing, bool) {
	b := readBorder(ctx, doc, dict)
	d := drawing{
		stroke: readColour(ctx, doc, dict["C"], black),
		fill:   readColour(ctx, doc, dict["IC"], nil),
		width:  b.width,
		dash:   b.dash,
		clip:   rect,
	}
	box := insetRD(ctx, doc, dict["RD"], rect)
	half := 0.0
	if d.strokes() {
		// The border is drawn inside the shape's bounds, the stroke centred
		// half a width in, as poppler 26.09.0 and PDF.js draw it.
		half = d.width / 2
	}
	box = geom.Rect{
		Min: geom.Point{X: box.Min.X + half, Y: box.Min.Y + half},
		Max: geom.Point{X: box.Max.X - half, Y: box.Max.Y - half},
	}
	if box.Max.X <= box.Min.X || box.Max.Y <= box.Min.Y {
		return d, false
	}
	var p geom.Path
	if subtype == "Square" {
		p.MoveTo(box.Min)
		p.LineTo(geom.Point{X: box.Max.X, Y: box.Min.Y})
		p.LineTo(box.Max)
		p.LineTo(geom.Point{X: box.Min.X, Y: box.Max.Y})
		p.Close()
	} else {
		c := geom.Point{X: (box.Min.X + box.Max.X) / 2, Y: (box.Min.Y + box.Max.Y) / 2}
		ellipse(&p, c, (box.Max.X-box.Min.X)/2, (box.Max.Y-box.Min.Y)/2)
	}
	d.add(p, paintFillStroke)
	return d, true
}

// synthLine draws a Line (ISO 32000-1, 12.5.6.7, Table 175): the segment
// /L, moved /LL along its normal with leader lines from its endpoints, and
// the endings /LE names. Closed endings are filled with /IC. The caption
// /Cap asks for is not drawn: it needs a font.
func synthLine(ctx context.Context, doc *document.Document, dict ast.PdfDict) (drawing, bool) {
	l, ok := readNumbers(ctx, doc, dict["L"], 4)
	if !ok || len(l) != 4 {
		return drawing{}, false
	}
	b := readBorder(ctx, doc, dict)
	d := drawing{
		stroke: readColour(ctx, doc, dict["C"], black),
		fill:   readColour(ctx, doc, dict["IC"], nil),
		width:  b.width,
		dash:   b.dash,
	}
	if !d.strokes() {
		return d, false
	}
	start, end := geom.Point{X: l[0], Y: l[1]}, geom.Point{X: l[2], Y: l[3]}
	length := math.Hypot(end.X-start.X, end.Y-start.Y)
	if length == 0 {
		return d, false
	}
	ll := readNumber(ctx, doc, dict["LL"], 0)
	lle := max(readNumber(ctx, doc, dict["LLE"], 0), 0)
	llo := max(readNumber(ctx, doc, dict["LLO"], 0), 0)
	startStyle, endStyle := readEndings(ctx, doc, dict["LE"])

	// frame maps (along, across) to user space: along runs from the start
	// to the end, across turns a quarter counterclockwise from it. Table 175
	// calls a positive /LL clockwise from the start to the end, and its
	// Figure 60 draws the line above its points: Acrobat, poppler 26.09.0
	// and PDF.js all read a positive /LL as that side, and so does this.
	frame := segmentFrame(start, end)
	size := min(endingScale*d.width, length/2)
	main := geom.Path{}
	main.MoveTo(frame.Apply(geom.Point{X: endingShorten(startStyle, size), Y: ll}))
	main.LineTo(frame.Apply(geom.Point{X: length - endingShorten(endStyle, size), Y: ll}))
	d.add(main, paintStroke)
	d.ending(frame, startStyle, geom.Point{X: 0, Y: ll}, -size)
	d.ending(frame, endStyle, geom.Point{X: length, Y: ll}, size)

	// Leader lines run from /LLO off each endpoint through the line to /LLE
	// past it (Table 175, PDF 1.6 and 1.7). A line with no /LL has none.
	if ll != 0 {
		sign := math.Copysign(1, ll)
		for _, x := range []float64{0, length} {
			var leader geom.Path
			leader.MoveTo(frame.Apply(geom.Point{X: x, Y: sign * llo}))
			leader.LineTo(frame.Apply(geom.Point{X: x, Y: ll + sign*lle}))
			d.add(leader, paintStroke)
		}
	}
	return d, true
}

// synthPoly draws a Polygon or a PolyLine (ISO 32000-1, 12.5.6.9, Table
// 178): /Vertices joined by straight lines, a polygon closed and filled with
// /IC, a polyline open with the endings /LE names on its first and last
// segments (PDF 1.5).
func synthPoly(ctx context.Context, doc *document.Document, dict ast.PdfDict, subtype ast.PdfName) (drawing, bool) {
	v, ok := readNumbers(ctx, doc, dict["Vertices"], 2*maxSynthPoints)
	if !ok || len(v) < 4 || len(v)%2 != 0 {
		return drawing{}, false
	}
	b := readBorder(ctx, doc, dict)
	d := drawing{
		stroke: readColour(ctx, doc, dict["C"], black),
		fill:   readColour(ctx, doc, dict["IC"], nil),
		width:  b.width,
		dash:   b.dash,
	}
	pts := points(v)
	if subtype == "Polygon" {
		var p geom.Path
		p.MoveTo(pts[0])
		for _, pt := range pts[1:] {
			p.LineTo(pt)
		}
		p.Close()
		d.add(p, paintFillStroke)
		return d, true
	}
	if !d.strokes() {
		return d, false
	}
	startStyle, endStyle := readEndings(ctx, doc, dict["LE"])
	first, last := segmentFrame(pts[0], pts[1]), segmentFrame(pts[len(pts)-2], pts[len(pts)-1])
	firstLen := math.Hypot(pts[1].X-pts[0].X, pts[1].Y-pts[0].Y)
	lastLen := math.Hypot(pts[len(pts)-1].X-pts[len(pts)-2].X, pts[len(pts)-1].Y-pts[len(pts)-2].Y)
	firstSize, lastSize := min(endingScale*d.width, firstLen/2), min(endingScale*d.width, lastLen/2)
	var p geom.Path
	p.MoveTo(first.Apply(geom.Point{X: endingShorten(startStyle, firstSize)}))
	for _, pt := range pts[1 : len(pts)-1] {
		p.LineTo(pt)
	}
	p.LineTo(last.Apply(geom.Point{X: lastLen - endingShorten(endStyle, lastSize)}))
	d.add(p, paintStroke)
	d.ending(first, startStyle, geom.Point{}, -firstSize)
	d.ending(last, endStyle, geom.Point{X: lastLen}, lastSize)
	return d, true
}

// synthInk draws an Ink annotation (ISO 32000-1, 12.5.6.13, Table 182): each
// path of /InkList stroked in /C with the border's width and dash. The table
// leaves how the points connect to the viewer; they are joined by straight
// lines, as poppler 26.09.0 joins them.
func synthInk(ctx context.Context, doc *document.Document, dict ast.PdfDict) (drawing, bool) {
	list, err := document.ResolveArray(ctx, doc, dict["InkList"])
	if err != nil || len(list) == 0 {
		return drawing{}, false
	}
	b := readBorder(ctx, doc, dict)
	d := drawing{stroke: readColour(ctx, doc, dict["C"], black), width: b.width, dash: b.dash}
	if !d.strokes() {
		return d, false
	}
	budget := 2 * maxSynthPoints
	for _, elem := range list {
		v, ok := readNumbers(ctx, doc, elem, budget)
		if !ok {
			return d, false
		}
		budget -= len(v)
		if len(v) < 2 || len(v)%2 != 0 {
			continue
		}
		pts := points(v)
		var p geom.Path
		p.MoveTo(pts[0])
		for _, pt := range pts[1:] {
			p.LineTo(pt)
		}
		d.add(p, paintStroke)
	}
	return d, true
}

// synthMarkup draws a text markup annotation (ISO 32000-1, 12.5.6.10, Table
// 179) over each quadrilateral of /QuadPoints, in /C.
//
// Table 179 orders a quadrilateral's points counterclockwise, but Acrobat
// writes them, and poppler 26.09.0 and PDF.js read them, upper left, upper
// right, lower left, lower right; this reads that order. Table 179 has no
// /BS: Underline, StrikeOut and Squiggly stroke one unit wide, as poppler
// draws them. A Highlight fills each quadrilateral, its ends bulging by a
// quarter of its height, in the Multiply blend mode, as both viewers draw it,
// so the text under it stays legible. With no /C a Highlight is yellow, the
// colour Acrobat and PDF.js default to (poppler fills it black), and the
// other types are black.
func synthMarkup(ctx context.Context, doc *document.Document, dict ast.PdfDict, subtype ast.PdfName) (drawing, bool) {
	q, ok := readNumbers(ctx, doc, dict["QuadPoints"], 2*maxSynthPoints)
	if !ok || len(q) == 0 || len(q)%8 != 0 {
		return drawing{}, false
	}
	d := drawing{width: 1}
	if subtype == "Highlight" {
		d.fill = readColour(ctx, doc, dict["C"], yellow)
		d.multiply = true
	} else {
		d.stroke = readColour(ctx, doc, dict["C"], black)
	}
	for i := 0; i < len(q); i += 8 {
		ul, ur := geom.Point{X: q[i], Y: q[i+1]}, geom.Point{X: q[i+2], Y: q[i+3]}
		ll, lr := geom.Point{X: q[i+4], Y: q[i+5]}, geom.Point{X: q[i+6], Y: q[i+7]}
		var p geom.Path
		switch subtype {
		case "Highlight":
			h := math.Hypot(ul.X-ll.X, ul.Y-ll.Y) / 4
			u := unit(ll, lr)
			n := geom.Point{X: -u.Y, Y: u.X}
			off := func(pt geom.Point, a, c float64) geom.Point {
				return geom.Point{X: pt.X + a*u.X + c*n.X, Y: pt.Y + a*u.Y + c*n.Y}
			}
			p.MoveTo(ll)
			p.CubicTo(off(ll, -h, h), off(ul, -h, -h), ul)
			p.LineTo(ur)
			p.CubicTo(off(ur, h, -h), off(lr, h, h), lr)
			p.Close()
			d.add(p, paintFill)
		case "Underline":
			p.MoveTo(ll)
			p.LineTo(lr)
			d.add(p, paintStroke)
		case "StrikeOut":
			p.MoveTo(mid(ul, ll))
			p.LineTo(mid(ur, lr))
			d.add(p, paintStroke)
		case "Squiggly":
			squiggle(&p, ul, ll, lr)
			d.add(p, paintStroke)
		}
	}
	return d, true
}

// synthLink draws a Link's border (ISO 32000-1, 12.5.6.5, Table 173): the
// /Rect outline, or its bottom edge for the underline style, in /C with the
// width and dash of /BS or /Border. A border of width 0 draws nothing. With
// no /C the border is black, as poppler 26.09.0 draws it.
func synthLink(ctx context.Context, doc *document.Document, dict ast.PdfDict, rect geom.Rect) (drawing, bool) {
	b := readBorder(ctx, doc, dict)
	d := drawing{stroke: readColour(ctx, doc, dict["C"], black), width: b.width, dash: b.dash}
	if !d.strokes() || rect.Max.X <= rect.Min.X || rect.Max.Y <= rect.Min.Y {
		return d, false
	}
	var p geom.Path
	p.MoveTo(rect.Min)
	p.LineTo(geom.Point{X: rect.Max.X, Y: rect.Min.Y})
	if !b.underline {
		p.LineTo(rect.Max)
		p.LineTo(geom.Point{X: rect.Min.X, Y: rect.Max.Y})
		p.Close()
	}
	d.add(p, paintStroke)
	return d, true
}

// endingScale is a line ending's size in line widths. ISO 32000-1, 12.5.6.7,
// Table 176 names the endings without sizing them; poppler 26.09.0 draws them
// six widths long, at most half the segment, and so does this.
const endingScale = 6

// tan30 is the tangent of the half-angle of an arrowhead, 30 degrees.
const tan30 = 0.5773502692

// endingShorten is how far a closed ending of the given size pulls its
// segment's end back, so the line stops at the shape's edge.
func endingShorten(style ast.PdfName, size float64) float64 {
	switch style {
	case "Square", "Circle", "Diamond", "ClosedArrow":
		return size
	}
	return 0
}

// ending adds the line ending style (ISO 32000-1, 12.5.6.7, Table 176) at
// tip, in the frame of its segment, size units long towards the segment's
// inside (negative for the start, whose inside lies along +x). Closed
// endings fill with the interior colour and stroke with the border's.
func (d *drawing) ending(frame geom.Matrix, style ast.PdfName, tip geom.Point, size float64) {
	if size == 0 {
		return
	}
	x, y, h := tip.X, tip.Y, size/2
	at := func(px, py float64) geom.Point { return frame.Apply(geom.Point{X: px, Y: py}) }
	polygon := func(closed bool, pts ...geom.Point) {
		var p geom.Path
		p.MoveTo(pts[0])
		for _, pt := range pts[1:] {
			p.LineTo(pt)
		}
		if closed {
			p.Close()
			d.add(p, paintFillStroke)
			return
		}
		d.add(p, paintStroke)
	}
	switch style {
	case "Square":
		polygon(true, at(x, y+h), at(x-size, y+h), at(x-size, y-h), at(x, y-h))
	case "Circle":
		var p geom.Path
		ellipse(&p, at(x-h, y), math.Abs(h), math.Abs(h))
		// The circle is drawn in user space: the frame is a rotation, so
		// it keeps the circle round.
		d.add(p, paintFillStroke)
	case "Diamond":
		polygon(true, at(x, y), at(x-h, y+h), at(x-size, y), at(x-h, y-h))
	case "OpenArrow", "ClosedArrow":
		polygon(style == "ClosedArrow", at(x-size, y+tan30*size), at(x, y), at(x-size, y-tan30*size))
	case "ROpenArrow", "RClosedArrow":
		polygon(style == "RClosedArrow", at(x+size, y+tan30*size), at(x, y), at(x+size, y-tan30*size))
	case "Butt":
		polygon(false, at(x, y+h), at(x, y-h))
	case "Slash":
		// A short line through the tip, 30 degrees clockwise from the
		// perpendicular (Table 176).
		off := h * math.Cos(math.Pi/3)
		polygon(false, at(x-off, y-h), at(x+off, y+h))
	}
}

// add appends a path painted with p, dropping the operations the drawing's
// colours and width do not allow.
func (d *drawing) add(path geom.Path, p paint) {
	stroke, fill := d.strokes(), d.fill != nil
	switch {
	case p == paintFillStroke && !stroke:
		p = paintFill
	case p == paintFillStroke && !fill:
		p = paintStroke
	}
	if (p == paintFill && !fill) || (p == paintStroke && !stroke) {
		return
	}
	d.shapes = append(d.shapes, shape{path: path, paint: p})
}

// addInk appends a path filled with its own colour, whatever the drawing's
// fill.
func (d *drawing) addInk(path geom.Path, fill []float64) {
	d.shapes = append(d.shapes, shape{path: path, paint: paintFill, fill: fill})
}

// strokes reports whether the drawing strokes: a border of width 0 is not
// drawn (ISO 32000-1, 12.5.4, Table 166), nor one of no colour (Table 164).
func (d *drawing) strokes() bool {
	return d.stroke != nil && d.width > 0
}

// bounds returns the box the drawing's strokes and fills reach.
func (d *drawing) bounds() geom.Rect {
	out := d.pathBounds()
	reach := miterReach*d.width + 1
	out.Min.X -= reach
	out.Min.Y -= reach
	out.Max.X += reach
	out.Max.Y += reach
	return out
}

// pathBounds returns the box of the drawing's paths, the points they pass
// through, without the reach of their strokes.
func (d *drawing) pathBounds() geom.Rect {
	var (
		out   geom.Rect
		found bool
	)
	for _, s := range d.shapes {
		b, ok := s.path.Bounds()
		if !ok {
			continue
		}
		if !found {
			out, found = b, true
			continue
		}
		out = union(out, b)
	}
	return out
}

// appearance serialises the drawing as a form XObject in default user space:
// its /BBox is the clip and Algorithm 8.1 maps it onto itself. An opacity
// below 1 or the Multiply blend mode paints the drawing into a transparency
// group (ISO 32000-1, 11.6.6) composited with them, so a filled and stroked
// shape is not darkened where its paints overlap.
func (d *drawing) appearance(rect geom.Rect) Appearance {
	bbox := ast.PdfArray{
		ast.PdfReal(d.clip.Min.X), ast.PdfReal(d.clip.Min.Y),
		ast.PdfReal(d.clip.Max.X), ast.PdfReal(d.clip.Max.Y),
	}
	body := d.content()
	resources := ast.PdfDict{}
	if d.opacity < 1 || d.multiply {
		bm := ast.PdfName("Normal")
		if d.multiply {
			bm = "Multiply"
		}
		group := ast.NewStream(ast.PdfDict{
			"Type": ast.PdfName("XObject"), "Subtype": ast.PdfName("Form"), "BBox": bbox,
			"Group":     ast.PdfDict{"S": ast.PdfName("Transparency")},
			"Resources": ast.PdfDict{},
		}, body)
		resources = ast.PdfDict{
			"ExtGState": ast.PdfDict{"GS0": ast.PdfDict{
				"Type": ast.PdfName("ExtGState"), "CA": ast.PdfReal(d.opacity), "ca": ast.PdfReal(d.opacity), "BM": bm,
			}},
			"XObject": ast.PdfDict{"Fm0": group},
		}
		body = []byte("/GS0 gs /Fm0 Do")
	}
	stream := ast.NewStream(ast.PdfDict{
		"Type": ast.PdfName("XObject"), "Subtype": ast.PdfName("Form"), "BBox": bbox, "Resources": resources,
	}, body)
	return Appearance{
		Stream:      stream,
		Resources:   resources,
		BBox:        d.clip,
		FormMatrix:  geom.Identity,
		Rect:        rect,
		A:           geom.Identity,
		Synthesized: true,
	}
}

// content writes the drawing's operators (ISO 32000-1, 8.5).
func (d *drawing) content() []byte {
	var c []byte
	c = append(c, "q "...)
	if d.stroke != nil {
		c = appendColour(c, d.stroke, true)
	}
	if d.fill != nil {
		c = appendColour(c, d.fill, false)
	}
	c = appendNum(c, d.width)
	c = append(c, " w ["...)
	for i, v := range d.dash {
		if i > 0 {
			c = append(c, ' ')
		}
		c = appendNum(c, v)
	}
	c = append(c, "] 0 d\n"...)
	fill := d.fill
	for _, s := range d.shapes {
		want := d.fill
		if s.fill != nil {
			want = s.fill
		}
		if want != nil && !slices.Equal(want, fill) {
			c = appendColour(c, want, false)
			fill = want
		}
		pts := s.path.Pts
		for _, op := range s.path.Ops {
			switch op {
			case geom.MoveTo, geom.LineTo:
				c = appendPoint(c, pts[0])
				if op == geom.MoveTo {
					c = append(c, " m "...)
				} else {
					c = append(c, " l "...)
				}
				pts = pts[1:]
			case geom.CubicTo:
				c = appendPoint(c, pts[0])
				c = append(c, ' ')
				c = appendPoint(c, pts[1])
				c = append(c, ' ')
				c = appendPoint(c, pts[2])
				c = append(c, " c "...)
				pts = pts[3:]
			case geom.Close:
				c = append(c, "h "...)
			}
		}
		switch s.paint {
		case paintStroke:
			c = append(c, "S\n"...)
		case paintFill:
			c = append(c, "f\n"...)
		case paintFillStroke:
			c = append(c, "B\n"...)
		}
	}
	return append(c, 'Q')
}

// appendColour writes the operator that sets a colour of 1, 3 or 4
// components in DeviceGray, DeviceRGB or DeviceCMYK (ISO 32000-1, 12.5.2,
// Table 164).
func appendColour(c []byte, comps []float64, stroke bool) []byte {
	for _, v := range comps {
		c = appendNum(c, v)
		c = append(c, ' ')
	}
	var op string
	switch len(comps) {
	case 1:
		op = "g"
	case 3:
		op = "rg"
	default:
		op = "k"
	}
	if stroke {
		op = strings.ToUpper(op)
	}
	return append(append(c, op...), ' ')
}

func appendPoint(c []byte, p geom.Point) []byte {
	c = appendNum(c, p.X)
	c = append(c, ' ')
	return appendNum(c, p.Y)
}

// appendNum writes v with at most four decimals, a hundredth of a pixel at
// 2400 dpi.
func appendNum(c []byte, v float64) []byte {
	return strconv.AppendFloat(c, math.Round(v*1e4)/1e4, 'f', -1, 64)
}

var (
	black  = []float64{0}
	white  = []float64{1}
	yellow = []float64{1, 1, 0}
)

// readColour reads a colour array (ISO 32000-1, 12.5.2, Table 164): no
// components is transparent and paints nothing (nil), 1, 3 and 4 are
// DeviceGray, DeviceRGB and DeviceCMYK. An absent or malformed array gives
// def. Components outside 0 to 1 are clamped to it.
func readColour(ctx context.Context, doc *document.Document, obj ast.PdfObject, def []float64) []float64 {
	if obj == nil {
		return def
	}
	v, ok := readNumbers(ctx, doc, obj, 4)
	if !ok {
		return def
	}
	switch len(v) {
	case 0:
		return nil
	case 1, 3, 4:
		for i := range v {
			v[i] = min(max(v[i], 0), 1)
		}
		return v
	}
	return def
}

// readBorder reads the border of ISO 32000-1, 12.5.4: /BS when present
// (Table 166), which makes /Border ignored, else /Border [h v w dash]
// (Table 164), else a solid border one unit wide. A /BS dashed with no
// usable /D dashes [3]. The beveled and inset styles are drawn as the solid
// border they shade, and an unreadable /Border draws none, as poppler 26.09.0
// reads it.
func readBorder(ctx context.Context, doc *document.Document, dict ast.PdfDict) border {
	bs, err := document.ResolveDict(ctx, doc, dict["BS"])
	if err != nil {
		doc.Logger.Debug("unreadable annotation /BS, reading /Border", slog.Any("err", err))
	}
	if bs != nil {
		b := border{width: max(readNumber(ctx, doc, bs["W"], 1), 0)}
		style, err := document.ResolveName(ctx, doc, bs["S"])
		if err != nil {
			doc.Logger.Debug("unreadable border style /S, reading it as solid", slog.Any("err", err))
		}
		switch style {
		case "D":
			b.dash = readDash(ctx, doc, bs["D"])
			if b.dash == nil {
				b.dash = []float64{3}
			}
		case "U":
			b.underline = true
		}
		return b
	}
	if dict["Border"] == nil {
		return border{width: 1}
	}
	arr, err := document.ResolveArray(ctx, doc, dict["Border"])
	if err != nil || len(arr) < 3 || len(arr) > 4 {
		return border{}
	}
	nums, ok := readNumbers(ctx, doc, arr[:3], 3)
	if !ok {
		return border{}
	}
	b := border{width: max(nums[2], 0)}
	if len(arr) == 4 {
		b.dash = readDash(ctx, doc, arr[3])
	}
	return b
}

// readDash reads a dash array (ISO 32000-1, 8.4.3.6): nil, a solid line,
// when it is not numbers, holds a negative one, or is all zeros.
func readDash(ctx context.Context, doc *document.Document, obj ast.PdfObject) []float64 {
	v, ok := readNumbers(ctx, doc, obj, maxDashEntries)
	if !ok || len(v) == 0 {
		return nil
	}
	sum := 0.0
	for _, x := range v {
		if x < 0 {
			return nil
		}
		sum += x
	}
	if sum == 0 {
		return nil
	}
	return v
}

// readEndings reads /LE (ISO 32000-1, 12.5.6.7, Table 175): two names, the
// start's and the end's line ending, both /None when absent.
func readEndings(ctx context.Context, doc *document.Document, obj ast.PdfObject) (ast.PdfName, ast.PdfName) {
	arr, err := document.ResolveArray(ctx, doc, obj)
	if err != nil || len(arr) != 2 {
		return "None", "None"
	}
	var out [2]ast.PdfName
	for i, e := range arr {
		n, err := document.ResolveName(ctx, doc, e)
		if err != nil || n == "" {
			n = "None"
		}
		out[i] = n
	}
	return out[0], out[1]
}

// readOpacity reads /CA, the opacity the annotation is painted with (ISO
// 32000-1, 12.5.6.2, Table 170; ISO 32000-2, 12.5.2, Table 166 extends it to
// every annotation), 1 when absent, clamped to 0 to 1.
func readOpacity(ctx context.Context, doc *document.Document, dict ast.PdfDict) float64 {
	return min(max(readNumber(ctx, doc, dict["CA"], 1), 0), 1)
}

// insetRD returns rect inset by /RD [left top right bottom] (ISO 32000-1,
// 12.5.6.8, Table 177), or rect itself when /RD is absent, malformed, holds
// a negative number or leaves no area. poppler 26.09.0 parses /RD as left,
// bottom, right, top, and draws a Square or Circle over the whole /Rect
// whatever it says; the table's order is followed here.
func insetRD(ctx context.Context, doc *document.Document, obj ast.PdfObject, rect geom.Rect) geom.Rect {
	if obj == nil {
		return rect
	}
	v, ok := readNumbers(ctx, doc, obj, 4)
	if !ok || len(v) != 4 || min(v[0], v[1], v[2], v[3]) < 0 {
		return rect
	}
	if v[0]+v[2] >= rect.Max.X-rect.Min.X || v[1]+v[3] >= rect.Max.Y-rect.Min.Y {
		return rect
	}
	return geom.Rect{
		Min: geom.Point{X: rect.Min.X + v[0], Y: rect.Min.Y + v[3]},
		Max: geom.Point{X: rect.Max.X - v[2], Y: rect.Max.Y - v[1]},
	}
}

// readNumber reads a finite number, def when it is absent or is not one.
func readNumber(ctx context.Context, doc *document.Document, obj ast.PdfObject, def float64) float64 {
	if obj == nil {
		return def
	}
	r, err := document.ResolveObject(ctx, doc, obj)
	if err != nil {
		doc.Logger.Debug("unreadable annotation number", slog.Any("err", err))
		return def
	}
	f, ok := document.Number(r)
	if !ok || math.IsNaN(f) || math.IsInf(f, 0) {
		return def
	}
	return f
}

// readNumbers reads an array of at most limit finite numbers. It fails on an
// array longer than limit, an element that is no number, and a done ctx.
func readNumbers(ctx context.Context, doc *document.Document, obj ast.PdfObject, limit int) ([]float64, bool) {
	if obj == nil {
		return nil, false
	}
	arr, err := document.ResolveArray(ctx, doc, obj)
	if err != nil || len(arr) > limit {
		return nil, false
	}
	out := make([]float64, len(arr))
	for i, e := range arr {
		if i%ctxCheckPoints == 0 && ctx.Err() != nil {
			return nil, false
		}
		r, err := document.ResolveObject(ctx, doc, e)
		if err != nil {
			return nil, false
		}
		f, ok := document.Number(r)
		if !ok || math.IsNaN(f) || math.IsInf(f, 0) {
			return nil, false
		}
		out[i] = f
	}
	return out, true
}

// points pairs alternate x and y coordinates.
func points(v []float64) []geom.Point {
	out := make([]geom.Point, len(v)/2)
	for i := range out {
		out[i] = geom.Point{X: v[2*i], Y: v[2*i+1]}
	}
	return out
}

// segmentFrame returns the rotation and translation that maps (along,
// across) to user space, along running from a to b and across a quarter turn
// counterclockwise from it.
func segmentFrame(a, b geom.Point) geom.Matrix {
	u := unit(a, b)
	return geom.Matrix{u.X, u.Y, -u.Y, u.X, a.X, a.Y}
}

// unit returns the unit vector from a to b, +x when they coincide.
func unit(a, b geom.Point) geom.Point {
	dx, dy := b.X-a.X, b.Y-a.Y
	l := math.Hypot(dx, dy)
	if l == 0 {
		return geom.Point{X: 1}
	}
	return geom.Point{X: dx / l, Y: dy / l}
}

func mid(a, b geom.Point) geom.Point {
	return geom.Point{X: (a.X + b.X) / 2, Y: (a.Y + b.Y) / 2}
}

// ellipse adds an axis-aligned ellipse of radii rx and ry around c, four
// cubic Béziers.
func ellipse(p *geom.Path, c geom.Point, rx, ry float64) {
	kx, ky := bezierCircle*rx, bezierCircle*ry
	p.MoveTo(geom.Point{X: c.X + rx, Y: c.Y})
	p.CubicTo(geom.Point{X: c.X + rx, Y: c.Y + ky}, geom.Point{X: c.X + kx, Y: c.Y + ry}, geom.Point{X: c.X, Y: c.Y + ry})
	p.CubicTo(geom.Point{X: c.X - kx, Y: c.Y + ry}, geom.Point{X: c.X - rx, Y: c.Y + ky}, geom.Point{X: c.X - rx, Y: c.Y})
	p.CubicTo(geom.Point{X: c.X - rx, Y: c.Y - ky}, geom.Point{X: c.X - kx, Y: c.Y - ry}, geom.Point{X: c.X, Y: c.Y - ry})
	p.CubicTo(geom.Point{X: c.X + kx, Y: c.Y - ry}, geom.Point{X: c.X + rx, Y: c.Y - ky}, geom.Point{X: c.X + rx, Y: c.Y})
	p.Close()
}

// squiggle adds the zig-zag of a Squiggly quadrilateral along its bottom
// edge, from ll to lr, a sixth of its height tall, stepping 2 units, as
// poppler 26.09.0 and PDF.js draw it; Table 179 leaves the shape to the
// viewer.
func squiggle(p *geom.Path, ul, ll, lr geom.Point) {
	u := unit(ll, lr)
	n := geom.Point{X: -u.Y, Y: u.X}
	length := math.Hypot(lr.X-ll.X, lr.Y-ll.Y)
	h := math.Hypot(ul.X-ll.X, ul.Y-ll.Y) / 6
	step := max(2, length/maxSquiggleSteps)
	at := func(a, c float64) geom.Point {
		return geom.Point{X: ll.X + a*u.X + c*n.X, Y: ll.Y + a*u.Y + c*n.Y}
	}
	p.MoveTo(at(0, h))
	up := false
	for x := step; ; x += step {
		c := 0.0
		if up {
			c = h
		}
		p.LineTo(at(x, c))
		up = !up
		if x >= length {
			break
		}
	}
}

// union returns the smallest rectangle holding every r.
func union(first geom.Rect, rest ...geom.Rect) geom.Rect {
	out := first
	for _, r := range rest {
		out.Min.X, out.Min.Y = min(out.Min.X, r.Min.X), min(out.Min.Y, r.Min.Y)
		out.Max.X, out.Max.Y = max(out.Max.X, r.Max.X), max(out.Max.Y, r.Max.Y)
	}
	return out
}
