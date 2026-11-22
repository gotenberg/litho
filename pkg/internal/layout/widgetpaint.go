package layout

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// drawnAsWidget reports whether the form control c styled st paints its
// widget in place of the background and the border its style gives it
// (CSS Basic User Interface 4, 7.2: appearance auto draws the native
// widget). A checkbox, a radio button and a range draw theirs whatever
// their border and background; a list box under appearance none too. Any
// other control draws its widget under appearance auto, or textfield for a
// text field, unless an author styled its border or background, which
// draws its CSS box, as Chromium draws it.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 draws check
//     boxes, radio buttons and ranges as their widget under a border or a
//     background, a list box as its widget under appearance: none, a
//     progress bar and a meter under a border or a background as their
//     CSS box with a #808080 bar and a #008000 value, or a gradient.
func drawnAsWidget(c *Control, st *css.Style) bool {
	switch c.Kind { //nolint:exhaustive // the other controls follow appearance and author styles
	case ControlCheckBox, ControlRadio, ControlRange:
		return st.Appearance != css.AppearanceNone
	case ControlListBox:
		return !st.AuthorBorder && !st.AuthorBackground
	}
	if st.AuthorBorder || st.AuthorBackground {
		return false
	}
	switch st.Appearance {
	case css.AppearanceAuto:
		return true
	case css.AppearanceTextField:
		return c.Kind == ControlTextField
	case css.AppearanceNone, css.AppearanceMenuListButton:
	}
	return false
}

// widgetOf returns the control b's node draws, nil when b is no control
// or is hidden.
func widgetOf(b *box) *Control {
	if b.node == nil || b.node.Control == nil || b.hidden {
		return nil
	}
	return b.node.Control
}

// widgetBase paints, for a control b drawn as its widget, the widget's
// face on its border box r in place of its background and border, and
// reports whether it did: a field's and a list box's filled rounded box,
// a button's, a colour well's. The parts that show the control's value
// paint in [painter.widgetParts].
func (p *painter) widgetBase(b *box, r typeset.Rect) bool {
	c := widgetOf(b)
	if c == nil || !drawnAsWidget(c, b.st) {
		return false
	}
	t := themeOf(b.st)
	switch c.Kind {
	case ControlTextField, ControlTextArea, ControlDropDown, ControlDate, ControlTime:
		fill, edge := t.field, t.fieldEdge
		if c.Disabled {
			fill, edge = disabledField(t, b.st.DarkScheme)
		}
		p.roundBox(b, r, fill, edge)
	case ControlListBox:
		p.roundBox(b, r, t.field, t.listEdge)
	case ControlButton, ControlColor:
		fill, edge := t.button, t.buttonEdge
		if c.Disabled {
			fill, edge = disabledButton(t, b.st.DarkScheme)
		}
		p.roundBox(b, r, fill, edge)
	case ControlCheckBox, ControlRadio, ControlRange, ControlProgress, ControlMeter, ControlFile:
		// Drawn whole by widgetParts, a file control by its button.
	}
	return true
}

// Chromium's disabled widgets, as the light colours composite over a
// white page.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 fills a disabled
//     field with #a9a9a9 at 0.2 then #ffffff at 0.6, a disabled button
//     with #a9a9a9 at 0.2 then #efefef at 0.3, and edges them with
//     #767676 at 0.3 (a field's at 0.75 of that); a disabled check box or
//     radio button as a field, checked #767676 at 0.3 with its tick
//     #ffffff at 0.6; a disabled range's value and thumb #cbcbcb; dark,
//     a disabled field #3b3b3b edged #626262 at 0.75, a disabled button
//     #363636.
var (
	disabledGround = over(hexColor(0xa9a9a9), hexColor(0xffffff), 0.2)
	disabledThumb  = hexColor(0xcbcbcb)
)

// disabledField returns the fill and the edge of a disabled field of
// theme t.
func disabledField(t widgetTheme, dark bool) (typeset.Color, typeset.Color) {
	if dark {
		return t.field, over(hexColor(0x626262), t.field, 0.75)
	}
	fill := over(t.field, disabledGround, 0.6)
	return fill, over(hexColor(0x767676), fill, float64(0.3*0.75))
}

// disabledButton returns the fill and the edge of a disabled button of
// theme t.
func disabledButton(t widgetTheme, dark bool) (typeset.Color, typeset.Color) {
	if dark {
		return hexColor(0x363636), hexColor(0x363636)
	}
	fill := over(t.button, disabledGround, 0.3)
	return fill, over(hexColor(0x767676), fill, 0.3)
}

// roundBox paints the face of a field or a button on the border box r:
// filled with fill and edged by a one-pixel line of edge, its corners
// rounded by 2 pixels, as Chromium's widgets are.
func (p *painter) roundBox(b *box, r typeset.Rect, fill, edge typeset.Color) {
	in := insetBy(r, half)
	radii := cornerRadii(min(widgetRadius, in.Width/2, in.Height/2))
	ops := roundedRect(in, radii)
	p.emit(b, false, &typeset.Path{Ops: ops, Fill: &typeset.Fill{Color: fill}}, r.Y, r.Y+r.Height, -1)
	p.emit(b, false, &typeset.Path{Ops: ops, Stroke: &typeset.Stroke{Color: edge, Width: wpx}}, r.Y, r.Y+r.Height, -1)
}

// The lengths of Chromium's widgets, in points.
const (
	wpx          = css.Pt
	half         = wpx / 2
	widgetRadius = 2 * wpx
)

// insetBy returns r shrunk by d on every side.
func insetBy(r typeset.Rect, d float64) typeset.Rect {
	return typeset.Rect{X: r.X + d, Y: r.Y + d, Width: max(r.Width-float64(2*d), 0), Height: max(r.Height-float64(2*d), 0)}
}

// cornerRadii returns four circular corners of radius rad.
func cornerRadii(rad float64) [4][2]float64 {
	if !(rad > 0) {
		return [4][2]float64{}
	}
	return [4][2]float64{{rad, rad}, {rad, rad}, {rad, rad}, {rad, rad}}
}

// widgetParts paints the parts of the control b that show its value or
// its kind over its face, on its border box r: a check box's box and
// tick, a radio button's ring and dot, a range's track and thumb, a
// progress bar's or a meter's bar, a colour well's swatch, a drop-down
// box's arrow, a textarea's resize grip, a date or time field's icon.
// They paint whether or not the face does, as Chromium paints them,
// except a check box's and a radio button's, which are the face.
func (p *painter) widgetParts(b *box, r typeset.Rect) {
	c := widgetOf(b)
	if c == nil {
		return
	}
	drawn := drawnAsWidget(c, b.st)
	t := themeOf(b.st)
	content := typeset.Rect{X: b.contentX() + p.dx, Y: b.contentY() + p.dy, Width: b.w, Height: b.h}
	switch c.Kind { //nolint:exhaustive // a field, a list box and a button have no part
	case ControlCheckBox:
		if drawn {
			p.checkBox(b, content, c, t)
		}
	case ControlRadio:
		if drawn {
			p.radio(b, content, c, t)
		}
	case ControlRange:
		p.slider(b, content, c, t, drawn)
	case ControlProgress, ControlMeter:
		p.bar(b, content, c, t, drawn)
	case ControlColor:
		p.swatch(b, content, c)
	case ControlDropDown:
		if drawn || b.st.Appearance != css.AppearanceNone {
			p.dropArrow(b, r)
		}
	case ControlTextArea:
		if b.st.Resize != css.ResizeNone {
			p.grip(b, r, t)
		}
	case ControlDate, ControlTime:
		if !c.Disabled {
			p.pickerIcon(b, r, c)
		}
	}
}

// widgetSquare returns the largest square centred in r.
func widgetSquare(r typeset.Rect) typeset.Rect {
	s := min(r.Width, r.Height)
	return typeset.Rect{X: r.X + float64((r.Width-s)/2), Y: r.Y + float64((r.Height-s)/2), Width: s, Height: s}
}

// checkBox paints the check box c on its content box r, in the largest
// square it holds: a box rounded by 2 pixels, edged by a one-pixel line,
// or filled with the accent and ticked when checked. The tick is a stroke
// 0.16 of the box wide through (0.2, 0.5), (0.4, 0.7) and (0.8, 0.2) of
// it.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 draws a 13 pixel
//     check box as a box filled 0.2 pixel in and edged by a one-pixel
//     line, rounded by 2 pixels, a checked one filled whole with its
//     accent and ticked through (2.6, 6.5), (5.2, 9.1) and (10.4, 2.6) by
//     a 2.08 pixel stroke with butt caps and miter joins; at 30 and 40
//     pixels its tick scaled, its corners and its edge not.
func (p *painter) checkBox(b *box, r typeset.Rect, c *Control, t widgetTheme) {
	sq := widgetSquare(r)
	if sq.Width <= 0 {
		return
	}
	radii := cornerRadii(min(widgetRadius, sq.Width/2))
	fill, edge := t.box, t.boxEdge
	if c.Disabled {
		fill, _ = disabledField(t, b.st.DarkScheme)
		edge = over(hexColor(0x767676), fill, 0.3)
	}
	if !c.Checked {
		p.widgetPath(b, &typeset.Path{Ops: roundedRect(insetBy(sq, float64(0.2*wpx)), radii), Fill: &typeset.Fill{Color: fill}})
		p.widgetPath(b, &typeset.Path{Ops: roundedRect(insetBy(sq, half), radii), Stroke: &typeset.Stroke{Color: edge, Width: wpx}})
		return
	}
	mark, tick := t.accent, t.tick
	if c.Disabled {
		mark = over(hexColor(0x767676), fill, 0.3)
		tick = over(hexColor(0xffffff), mark, 0.6)
	}
	p.widgetPath(b, &typeset.Path{Ops: roundedRect(sq, radii), Fill: &typeset.Fill{Color: mark}})
	at := func(x, y float64) typeset.Point {
		return typeset.Point{X: sq.X + float64(x*sq.Width), Y: sq.Y + float64(y*sq.Height)}
	}
	p.widgetPath(b, &typeset.Path{Ops: []typeset.PathOp{
		{Kind: typeset.MoveTo, P: [3]typeset.Point{at(0.2, 0.5)}},
		{Kind: typeset.LineTo, P: [3]typeset.Point{at(0.4, 0.7)}},
		{Kind: typeset.LineTo, P: [3]typeset.Point{at(0.8, 0.2)}},
	}, Stroke: &typeset.Stroke{Color: tick, Width: float64(0.16 * sq.Width), Cap: typeset.ButtCap, Join: typeset.MiterJoin}})
}

// radio paints the radio button c on its content box r, in the largest
// square it holds: a circle edged by a one-pixel line, the edge in the
// accent and a dot 0.6 of the circle across in its middle when checked.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 draws a 13 pixel
//     radio button as a circle filled 6.3 pixels round and edged 6
//     pixels round by a one-pixel line, a checked one edged with its
//     accent around a dot of radius 3.9 pixels; at 30 pixels the dot
//     scaled, the edge not.
func (p *painter) radio(b *box, r typeset.Rect, c *Control, t widgetTheme) {
	sq := widgetSquare(r)
	if sq.Width <= 0 {
		return
	}
	cx, cy, rad := sq.X+float64(sq.Width/2), sq.Y+float64(sq.Height/2), float64(sq.Width/2)
	fill, edge, dot := t.box, t.boxEdge, t.accent
	if c.Disabled {
		fill, _ = disabledField(t, b.st.DarkScheme)
		edge = over(hexColor(0x767676), fill, 0.3)
		dot = edge
	} else if c.Checked {
		edge = t.accent
	}
	p.widgetPath(b, &typeset.Path{Ops: circleOps(cx, cy, max(rad-float64(0.2*wpx), 0)), Fill: &typeset.Fill{Color: fill}})
	p.widgetPath(b, &typeset.Path{Ops: circleOps(cx, cy, max(rad-half, 0)), Stroke: &typeset.Stroke{Color: edge, Width: wpx}})
	if c.Checked {
		p.widgetPath(b, &typeset.Path{Ops: circleOps(cx, cy, float64(0.3*sq.Width)), Fill: &typeset.Fill{Color: dot}})
	}
}

// widgetPath emits the path pa of the control b, a part of its content:
// what shows a Form element's state hangs from it; the bar of a progress
// bar, a meter or a range, which no PrintField role names, is an artifact.
func (p *painter) widgetPath(b *box, pa *typeset.Path) {
	if b.node.Role == "Form" {
		pa.Parent = b.elem
	}
	y0, y1 := pathBounds(pa.Ops)
	p.emit(b, true, pa, y0, y1, -1)
}

// pathBounds returns the top and the bottom of the points of ops.
func pathBounds(ops []typeset.PathOp) (float64, float64) {
	y0, y1 := math.Inf(1), math.Inf(-1)
	for _, op := range ops {
		n := 1
		if op.Kind == typeset.CurveTo {
			n = 3
		}
		if op.Kind == typeset.ClosePath {
			n = 0
		}
		for _, pt := range op.P[:n] {
			y0, y1 = min(y0, pt.Y), max(y1, pt.Y)
		}
	}
	if y0 > y1 {
		return 0, 0
	}
	return y0, y1
}

// slider paints the range c on its content box r: a track 8 pixels tall,
// or the box's height when less, a pixel in from either side, rounded
// whole, filled with the accent up to the thumb, and a thumb 15 pixels
// across whose centre runs from 7.5 pixels in from the left to 7.5 in
// from the right. Under appearance: none the thumb alone, which keeps its
// own appearance.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 draws a range of
//     129 by 16 pixels at value 30 of 0 to 100 as a track 127 by 8
//     pixels, centred, and a thumb of 15 pixels centred 41.7 pixels in; a
//     range 6 pixels tall with a track 6 pixels tall; under
//     appearance: none the thumb alone.
func (p *painter) slider(b *box, r typeset.Rect, c *Control, t widgetTheme, drawn bool) {
	if r.Width <= 0 || r.Height <= 0 {
		return
	}
	value, thumb := t.accent, t.accent
	if c.Disabled {
		value, thumb = disabledThumb, disabledThumb
	}
	thumbR := float64(7.5 * wpx)
	cx := r.X + thumbR + float64(clamp01(c.Value)*max(r.Width-2*thumbR, 0))
	cy := r.Y + float64(r.Height/2)
	if drawn {
		th := min(8*wpx, r.Height)
		track := typeset.Rect{X: r.X + wpx, Y: cy - float64(th/2), Width: max(r.Width-2*wpx, 0), Height: th}
		p.track(b, track, th/2, t.track, t.trackEdge, value, cx-track.X)
	}
	p.widgetPath(b, &typeset.Path{Ops: circleOps(cx, cy, thumbR), Fill: &typeset.Fill{Color: thumb}})
}

// track paints a rounded track r of corner radius rad, filled with fill,
// its first w points with value, and edged with edge.
func (p *painter) track(b *box, r typeset.Rect, rad float64, fill, edge, value typeset.Color, w float64) {
	shape := roundedRect(r, cornerRadii(min(rad, r.Width/2, r.Height/2)))
	p.widgetPath(b, &typeset.Path{Ops: shape, Fill: &typeset.Fill{Color: fill}})
	if w = min(w, r.Width); w > 0 {
		clip := &typeset.Path{Ops: rectOps(typeset.Rect{X: r.X, Y: r.Y, Width: w, Height: r.Height}), Fill: &typeset.Fill{}}
		g := &ditem{group: true, fx: &groupFx{opacity: 1, clip: clip}}
		page := -1
		if p.mono >= 0 {
			page = p.mono
		}
		value := &typeset.Path{Ops: shape, Fill: &typeset.Fill{Color: value}}
		if b.node.Role == "Form" {
			value.Parent = b.elem
		}
		g.kids = append(g.kids, &ditem{frag: value, y0: r.Y, y1: r.Y + r.Height, page: page})
		holder := p.sync(p.chain(b, true))
		holder.kids = append(holder.kids, g)
	}
	in := insetBy(r, half)
	p.widgetPath(b, &typeset.Path{Ops: roundedRect(in, cornerRadii(min(rad-half, in.Width/2, in.Height/2))), Stroke: &typeset.Stroke{Color: edge, Width: wpx}})
}

// bar paints the progress bar or the meter c on its content box r. Drawn
// as its widget, a bar half the box's height, centred and rounded whole:
// a progress bar's filled with the accent up to its value, an
// indeterminate one a fifth of it from a pixel in; a meter's value a
// pixel inside its bar, in its region's colour. Drawn as its CSS box, a
// progress bar fills the box #808080 and its value #008000, a meter fills
// a bar half the box's height with gradients ([meterBar]), square.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 draws progress
//     bars of 160 by 16, 30 and 4 pixels with bars 8, 15 and 2 pixels
//     tall, an indeterminate one with a value 32 pixels long a pixel in;
//     meters of 80 by 16 and 30 pixels with bars 8 and 15 pixels tall
//     whose value is 6 and 13 pixels tall; under a background or
//     appearance: none, a progress bar's #808080 bar filled #008000.
func (p *painter) bar(b *box, r typeset.Rect, c *Control, t widgetTheme, drawn bool) {
	if r.Width <= 0 || r.Height <= 0 {
		return
	}
	if !drawn {
		if c.Kind == ControlMeter {
			bar := typeset.Rect{X: r.X, Y: r.Y + float64(r.Height/4), Width: r.Width, Height: float64(r.Height / 2)}
			p.widgetPath(b, &typeset.Path{Ops: rectOps(bar), Fill: meterGradient(bar, meterBar)})
			if v := clamp01(c.Value); v > 0 {
				val := bar
				val.Width = float64(v * bar.Width)
				p.widgetPath(b, &typeset.Path{Ops: rectOps(val), Fill: meterGradient(val, meterValues[min(int(c.Level), 2)])})
			}
			return
		}
		p.widgetPath(b, &typeset.Path{Ops: rectOps(r), Fill: &typeset.Fill{Color: hexColor(0x808080)}})
		if v := clamp01(c.Value); v > 0 {
			p.widgetPath(b, &typeset.Path{Ops: rectOps(typeset.Rect{X: r.X, Y: r.Y, Width: float64(v * r.Width), Height: r.Height}), Fill: &typeset.Fill{Color: hexColor(0x008000)}})
		}
		return
	}
	h := float64(r.Height / 2)
	track := typeset.Rect{X: r.X, Y: r.Y + float64((r.Height-h)/2), Width: r.Width, Height: h}
	if c.Kind == ControlMeter {
		shape := roundedRect(track, cornerRadii(min(h/2, track.Width/2)))
		p.widgetPath(b, &typeset.Path{Ops: shape, Fill: &typeset.Fill{Color: t.track}})
		if v := clamp01(c.Value); v > 0 && track.Width > 2*wpx && h > 2*wpx {
			fill := t.meter[min(int(c.Level), 2)]
			p.widgetPath(b, &typeset.Path{Ops: rectOps(typeset.Rect{X: track.X + wpx, Y: track.Y + wpx, Width: float64(v * (track.Width - 2*wpx)), Height: h - 2*wpx}), Fill: &typeset.Fill{Color: fill}})
		}
		in := insetBy(track, half)
		p.widgetPath(b, &typeset.Path{Ops: roundedRect(in, cornerRadii(min(float64(h/2)-half, in.Width/2, in.Height/2))), Stroke: &typeset.Stroke{Color: t.meterEdge, Width: wpx}})
		return
	}
	if c.Value < 0 {
		p.track(b, track, h/2, t.track, t.trackEdge, t.accent, 0)
		seg := typeset.Rect{X: track.X + wpx, Y: track.Y, Width: track.Width / 5, Height: h}
		p.widgetPath(b, &typeset.Path{Ops: roundedRect(seg, cornerRadii(min(h/2, seg.Width/2))), Fill: &typeset.Fill{Color: t.accent}})
		return
	}
	p.track(b, track, h/2, t.track, t.trackEdge, t.accent, float64(clamp01(c.Value)*track.Width))
}

// swatch paints the colour of the colour well c in its content box r,
// less 2 pixels across and 4 down either side: the colour, edged inside
// by a one-pixel #777777 line.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 draws a colour
//     well of 50 by 27 pixels with a swatch of 40 by 15 pixels 5 in and 6
//     down, edged by #777777, under appearance: none and a border too.
func (p *painter) swatch(b *box, r typeset.Rect, c *Control) {
	sw := typeset.Rect{X: r.X + 2*wpx, Y: r.Y + 4*wpx, Width: r.Width - 4*wpx, Height: r.Height - 8*wpx}
	if sw.Width <= 0 || sw.Height <= 0 {
		return
	}
	col := c.Swatch
	col.A = 1
	p.widgetPath(b, &typeset.Path{Ops: rectOps(sw), Fill: &typeset.Fill{Color: col}})
	p.widgetPath(b, &typeset.Path{Ops: rectOps(insetBy(sw, half)), Stroke: &typeset.Stroke{Color: hexColor(0x777777), Width: wpx}})
}

// dropArrow paints a drop-down box's arrow on its border box r: a
// chevron 8 pixels wide and 4 tall, 4 pixels in from its right border,
// centred down, stroked 2 pixels wide in the text's colour.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 draws the arrow
//     of a drop-down box through (-13, -2.5), (-9, 1.5) and (-5, -2.5)
//     pixels from its right edge and its middle, 2 pixels wide, in its
//     text colour (#cc0000 for a red select, #ffffff in the dark scheme),
//     at any font size; under a 3 pixel border 4 pixels in from it.
func (p *painter) dropArrow(b *box, r typeset.Rect) {
	col, ok := colorOf(b.st.Color)
	if !ok {
		return
	}
	right := r.X + r.Width - b.bd[css.Right] - 4*wpx
	mid := r.Y + float64(r.Height/2)
	pt := func(x, y float64) typeset.Point { return typeset.Point{X: right + x*wpx, Y: mid + y*wpx} }
	p.widgetPath(b, &typeset.Path{Ops: []typeset.PathOp{
		{Kind: typeset.MoveTo, P: [3]typeset.Point{pt(-8, -2.5)}},
		{Kind: typeset.LineTo, P: [3]typeset.Point{pt(-4, 1.5)}},
		{Kind: typeset.LineTo, P: [3]typeset.Point{pt(0, -2.5)}},
	}, Stroke: &typeset.Stroke{Color: col, Width: 2 * wpx, Cap: typeset.ButtCap, Join: typeset.MiterJoin}})
}

// grip paints the resize grip of a textarea that can be resized (CSS
// Basic User Interface 4, 5.1) in the bottom right corner of its padding
// box: two dark lines and two light ones, at 45 degrees, under
// appearance: none too.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 draws, from the
//     padding box's bottom right corner, hairlines of #000000 at 0.45
//     from (-2, -9) to (-9, -2) and (-2, -5) to (-5, -2) pixels, and of
//     #ffffff at 0.45 from (-2, -8) to (-8, -2) and (-2, -4) to (-4, -2);
//     none under resize: none.
func (p *painter) grip(b *box, r typeset.Rect, t widgetTheme) {
	x := r.X + r.Width - b.bd[css.Right]
	y := r.Y + r.Height - b.bd[css.Bottom]
	// line runs from (-2, -d) to (-d, -2) pixels from the corner.
	line := func(d float64) []typeset.PathOp {
		return []typeset.PathOp{
			{Kind: typeset.MoveTo, P: [3]typeset.Point{{X: x - 2*wpx, Y: y - float64(d*wpx)}}},
			{Kind: typeset.LineTo, P: [3]typeset.Point{{X: x - float64(d*wpx), Y: y - 2*wpx}}},
		}
	}
	dark := append(line(9), line(5)...)
	light := append(line(8), line(4)...)
	p.emit(b, false, &typeset.Path{Ops: dark, Stroke: &typeset.Stroke{Color: over(hexColor(0), t.field, 0.45), Width: wpx}}, y-9*wpx, y, -1)
	p.emit(b, false, &typeset.Path{Ops: light, Stroke: &typeset.Stroke{Color: over(hexColor(0xffffff), t.field, 0.45), Width: wpx}}, y-8*wpx, y, -1)
}

// pickerIcon paints a date field's calendar or a time field's clock, in
// the text's colour, at the right of its border box r: Chromium's icons at
// the control font's 13.333 pixels, scaled with the font. A disabled
// field shows none.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 draws a calendar
//     10 by 11 pixels, its right edge 5.5 pixels in from the field's and
//     its top 4.5 pixels down a field 21 pixels tall, and a clock of
//     radius 5.83 pixels centred 12 pixels in from the right and in the
//     middle of the field; a disabled one neither.
func (p *painter) pickerIcon(b *box, r typeset.Rect, c *Control) {
	col, ok := colorOf(b.st.Color)
	if !ok {
		return
	}
	k := b.st.FontSize / css.Pt / smallControl
	if !(k > 0) {
		return
	}
	u := float64(wpx * k)
	if c.Kind == ControlTime {
		cx, cy := r.X+r.Width-float64(12*u), r.Y+float64(r.Height/2)
		ops := append(circleOps(cx, cy, float64(5.83*u)), circleOps(cx, cy, float64(4.67*u))...)
		hand := []typeset.PathOp{{Kind: typeset.MoveTo, P: [3]typeset.Point{{X: cx + float64(0.29*u), Y: cy - float64(2.92*u)}}}}
		for _, pt := range [][2]float64{{-0.58, -2.92}, {-0.58, 0.58}, {2.48, 2.42}, {2.92, 1.70}, {0.29, 0.15}} {
			hand = append(hand, typeset.PathOp{Kind: typeset.LineTo, P: [3]typeset.Point{{X: cx + float64(pt[0]*u), Y: cy + float64(pt[1]*u)}}})
		}
		hand = append(hand, typeset.PathOp{Kind: typeset.ClosePath})
		p.widgetPath(b, &typeset.Path{Ops: append(ops, hand...), Fill: &typeset.Fill{Color: col, Rule: typeset.EvenOdd}})
		return
	}
	x0 := r.X + r.Width - float64(15.5*u)
	y0 := r.Y + float64(math.Floor(float64(r.Height/u-11)/2)*u) - float64(half*k)
	pt := func(x, y float64) typeset.Point { return typeset.Point{X: x0 + float64(x*u), Y: y0 + float64(y*u)} }
	var ops []typeset.PathOp
	move := func(x, y float64) {
		ops = append(ops, typeset.PathOp{Kind: typeset.MoveTo, P: [3]typeset.Point{pt(x, y)}})
	}
	line := func(x, y float64) {
		ops = append(ops, typeset.PathOp{Kind: typeset.LineTo, P: [3]typeset.Point{pt(x, y)}})
	}
	corner := func(cx, cy, x, y float64) {
		ops = append(ops, typeset.PathOp{Kind: typeset.CurveTo, P: [3]typeset.Point{pt(cx, cy), pt(cx, cy), pt(x, y)}})
	}
	move(9, 1)
	for _, v := range [][2]float64{{8.5, 1}, {8.5, 0}, {7.5, 0}, {7.5, 1}, {2.5, 1}, {2.5, 0}, {1.5, 0}, {1.5, 1}, {1, 1}} {
		line(v[0], v[1])
	}
	corner(0, 1, 0, 2)
	line(0, 10)
	corner(0, 11, 1, 11)
	line(9, 11)
	corner(10, 11, 10, 10)
	line(10, 2)
	corner(10, 1, 9, 1)
	ops = append(ops, typeset.PathOp{Kind: typeset.ClosePath})
	move(9, 10)
	line(1, 10)
	line(1, 3.5)
	line(9, 3.5)
	ops = append(ops, typeset.PathOp{Kind: typeset.ClosePath})
	p.widgetPath(b, &typeset.Path{Ops: ops, Fill: &typeset.Fill{Color: col, Rule: typeset.EvenOdd}})
}

// widgetBaselined reports a control b drawn as its widget whose baseline
// the widget sets, not its content: a check box, a radio button, a range,
// a progress bar, a meter and a colour well; a range and a colour well
// under appearance: none too, which keep their thumb and their swatch.
func widgetBaselined(b *box) bool {
	c := widgetOf(b)
	if c == nil || !drawnAsWidget(c, b.st) && c.Kind != ControlColor && c.Kind != ControlRange {
		return false
	}
	switch c.Kind { //nolint:exhaustive // the other controls take their text's baseline
	case ControlCheckBox, ControlRadio, ControlRange, ControlProgress, ControlMeter, ControlColor:
		return true
	}
	return false
}

// widgetBaseline returns the baseline of the control b, which
// [widgetBaselined] reports, from its border box's top: its border box's
// bottom, where CSS 2.1, 10.8.1 puts an inline-block without lines on its
// bottom margin edge, and a colour well's swatch's bottom, 4 pixels above
// its content box's.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 sets check boxes
//     (margin-bottom 3 pixels), radio buttons, ranges (margin 2 pixels),
//     progress bars and meters with their border box's bottom on the
//     baseline, a range under appearance: none too, and a colour well 27
//     pixels tall 6 pixels above its bottom.
func widgetBaseline(b *box) float64 {
	if widgetOf(b).Kind == ControlColor {
		return b.bd[css.Top] + b.pd[css.Top] + max(b.h-swatchBlock, 0)
	}
	return b.borderH()
}

// swatchBlock is the room below a colour well's swatch in its content
// box, in points.
const swatchBlock = 4 * wpx

// The vertical gradients a meter drawn as its CSS box fills its bar and
// its value with, top to bottom at 0, 20, 45, 55 and 100 per cent: its
// bar's, then its value's in each region.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 fills the bar of
//     a meter under appearance: none, 80 by 8 pixels in the middle of its
//     16 pixel box, from #dddddd down through #e8e8e8, #cecece and back
//     to #d8d8d8, and an optimum value from #aadd77 through #bfe897 and
//     #7cae3a to #9ccf64.
var (
	meterBar    = [5]uint32{0xdddddd, 0xeeeeee, 0xcccccc, 0xcccccc, 0xdddddd}
	meterValues = [3][5]uint32{
		{0xaadd77, 0xcceeaa, 0x77aa33, 0x77aa33, 0xaadd77},
		{0xffee77, 0xffffcc, 0xddbb33, 0xddbb33, 0xffee77},
		{0xff7777, 0xffcccc, 0xdd4444, 0xdd4444, 0xff7777},
	}
)

// meterGradient returns the fill of r with the vertical gradient of the
// five colours stops.
func meterGradient(r typeset.Rect, stops [5]uint32) *typeset.Fill {
	sh := &typeset.Shading{Kind: typeset.Axial, From: typeset.Point{X: r.X, Y: r.Y}, To: typeset.Point{X: r.X, Y: r.Y + max(r.Height, 1e-3)}}
	for i, at := range [5]float64{0, 0.2, 0.45, 0.55, 1} {
		sh.Stops = append(sh.Stops, typeset.ColorStop{Offset: at, Color: hexColor(stops[i])})
	}
	return &typeset.Fill{Shading: sh}
}
