package xlsx

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/intconv"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// relVMLDrawing is the relationship of a sheet's legacy VML drawing
// (ECMA-376 Part 1, 18.3.1.49 legacyDrawing).
const relVMLDrawing = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/vmlDrawing"

// nsExcelVML is the namespace of the Excel data of a VML shape (ECMA-376
// Part 4, 14.4 x:ClientData).
const nsExcelVML = "urn:schemas-microsoft-com:office:excel"

// maxControls caps the form controls read from one sheet. Anti-DoS
// guard, not spec-mandated: a form holds a few dozen controls.
const maxControls = 1024

// maxCaptionRunes caps the characters of a control's caption. Anti-DoS
// guard, not spec-mandated: a caption is a short label.
const maxCaptionRunes = 512

// controlKind is a kind of form control (ECMA-376 Part 4, 14.4.2.3
// ObjectType, ST_ObjectType).
type controlKind uint8

// The form controls Excel prints.
const (
	ctrlCheckbox controlKind = iota
	ctrlRadio
	ctrlButton
	ctrlLabel
	ctrlGroup
	ctrlDrop
	ctrlList
	ctrlSpin
	ctrlScroll
)

// controlKinds maps the ObjectType names of 14.4.2.3 that are form
// controls to their kinds; a comment (Note) and the other types are not.
var controlKinds = map[string]controlKind{ // a read-only spec table, ECMA-376 Part 4, 14.4.2.3.
	"Checkbox": ctrlCheckbox, "Radio": ctrlRadio, "Button": ctrlButton, "Label": ctrlLabel,
	"GBox": ctrlGroup, "Drop": ctrlDrop, "List": ctrlList, "Spin": ctrlSpin, "Scroll": ctrlScroll,
}

// control is one form control as Excel prints it.
type control struct {
	kind    controlKind
	caption string
	family  string
	size    float64
	color   typeset.Color
	checked bool
	// center centres the caption across the control (TextHAlign Center).
	center bool
	// horiz lays a scroll bar or a spinner along x (14.4.2.43 Horiz).
	horiz bool
	// pos is the thumb's place along a scroll bar, 0 to 1.
	pos float64
}

// The measures of a control's face as Excel prints it, in points.
//
// # Corpus evidence
//
//   - litho/xlsx: form_controls prints each control as a picture of its
//     anchor's box, the caption as Segoe UI text at the size of its
//     font (size 160, 8 points): a check box's square and an option
//     button's circle of 9.5 points 3.75 points from the left edge, white
//     with a thin dark grey outline, a thin check mark when checked,
//     centred on the box's height, the caption 16.75 points from the left
//     edge, its line centred; a button's rounded grey outline round the
//     whole box, the caption centred; a label's caption 2.2 points from
//     the left and 1.5 points from the top; a group box's black frame,
//     its caption 9.8 points from the left across the frame's top edge,
//     its baseline 0.6 em below the box's top;
//     a drop-down's frame and arrow box of the box's height, its selected
//     item 3 points from the left; a spinner's grey halves with an arrow
//     each; a horizontal scroll bar's arrows in squares at its ends over a
//     grey track.
const (
	markSide       = 9.5
	markInset      = 3.75
	captionInset   = 16.75
	labelInsetX    = 2.2
	labelInsetY    = 1.5
	groupCaptionX  = 9.8
	groupBaseline  = 0.6
	dropTextInset  = 3
	controlLine    = 0.75
	defaultCtrlPt  = 8
	defaultCtrlFam = "Segoe UI"
)

// controls reads the form controls of the sheet's legacy VML drawing
// (ECMA-376 Part 1, 18.3.1.49 legacyDrawing; Part 4, 14.4 x:ClientData):
// each shape whose ObjectType is a control becomes a drawing at the cells
// and pixel offsets of its x:Anchor (14.4.2.2: left column, offset, top
// row, offset, right column, offset, bottom row, offset, the offsets in
// pixels of 0.75 point). A control whose x:PrintObject is False, or a
// hidden shape, does not print, as Excel prints the controls of a sheet.
// Comments (ObjectType Note) do not print and are not read.
func (w *sheetReader) controls(id string) error {
	r := w.r
	rel, ok := r.pkg.RelationshipByID(r.ctx, w.part, id)
	if !ok || rel.External || rel.Type != relVMLDrawing || !r.pkg.Has(rel.Target) {
		return nil
	}
	root, err := r.pkg.ParseXML(r.ctx, rel.Target, understood)
	if err != nil {
		if isMalformed(err) {
			r.warn(warn.PartMalformed, warn.Omitted, fmt.Sprintf("the form controls of sheet %q do not parse: %v", w.sh.Name, err))
			return nil
		}
		return err
	}
	n := 0
	for _, sp := range root.Kids() {
		cd := sp.Child(nsExcelVML, "ClientData")
		if !sp.Is(ooxml.NSV, "shape") || cd == nil {
			continue
		}
		kind, ok := controlKinds[attr(cd, "ObjectType")]
		if !ok {
			continue
		}
		vs := ooxml.ParseVMLStyle(attr(sp, "style"))
		if vs["visibility"] == "hidden" || strings.EqualFold(clientText(cd, "PrintObject"), "false") {
			continue
		}
		from, to, ok := clientAnchor(clientText(cd, "Anchor"))
		if !ok {
			continue
		}
		if n >= maxControls || len(w.sh.Drawings) >= maxDrawings {
			r.warn(warn.LimitReached, warn.Omitted, fmt.Sprintf("sheet %q holds more than %d form controls: the rest are left out", w.sh.Name, maxControls))
			break
		}
		n++
		c := w.control(kind, sp, cd)
		d := sheet.Drawing{From: from, To: &to, Shape: true, Name: c.caption, Alt: c.caption}
		d.Picture = c.picture(r.fonts)
		w.sh.Drawings = append(w.sh.Drawings, d)
	}
	return nil
}

// clientText returns the trimmed text of the x:ClientData child local,
// "" when absent.
func clientText(cd *ooxml.Element, local string) string {
	if c := cd.Child(nsExcelVML, local); c != nil {
		return strings.TrimSpace(c.Text)
	}
	return ""
}

// clientAnchor reads an x:Anchor (14.4.2.2): eight integers, the offsets
// in pixels of 0.75 point.
func clientAnchor(s string) (sheet.Anchor, sheet.Anchor, bool) {
	f := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\r' || r == '\t' })
	if len(f) != 8 {
		return sheet.Anchor{}, sheet.Anchor{}, false
	}
	var v [8]int
	for i, s := range f {
		n, err := intconv.Atoi32(s)
		if err != nil {
			return sheet.Anchor{}, sheet.Anchor{}, false
		}
		v[i] = max(0, n)
	}
	at := func(col, dx, row, dy int) sheet.Anchor {
		return sheet.Anchor{Column: min(col, maxColumns-1), DX: float64(dx) * 0.75, Row: min(row, maxRows-1), DY: float64(dy) * 0.75}
	}
	return at(v[0], v[1], v[2], v[3]), at(v[4], v[5], v[6], v[7]), true
}

// control reads what a control shows: its caption, the text of its
// v:textbox in the face, size (in twentieths of a point) and colour of
// its font element, whether it is checked (14.4.2.12 Checked), a
// drop-down's selected item (14.4.2.70 Sel, an index into the cells of
// 14.4.2.33 FmlaRange), a scroll bar's place (Val between Min and Max).
func (w *sheetReader) control(kind controlKind, sp, cd *ooxml.Element) *control {
	c := &control{kind: kind, family: defaultCtrlFam, size: defaultCtrlPt, color: typeset.Color{A: 1}}
	if tb := sp.Child(ooxml.NSV, "textbox"); tb != nil {
		var b strings.Builder
		collectText(tb, &b, 0)
		c.caption = strings.Join(strings.Fields(b.String()), " ")
		if r := []rune(c.caption); len(r) > maxCaptionRunes {
			c.caption = string(r[:maxCaptionRunes])
		}
		if f := tb.Find("", "font"); f != nil {
			if v := strings.TrimSpace(attr(f, "face")); v != "" {
				c.family = v
			}
			if v, err := strconv.ParseFloat(attr(f, "size"), 64); err == nil && v > 0 {
				c.size = min(v/20, maxShapeTextPt)
			}
			if col, ok := ooxml.VMLColor(attr(f, "color")); ok {
				c.color = typesetColor(col)
			}
		}
	}
	child := func(local string) string { return clientText(cd, local) }
	c.checked = child("Checked") == "1"
	c.center = strings.EqualFold(child("TextHAlign"), "center")
	c.horiz = cd.Child(nsExcelVML, "Horiz") != nil
	if kind == ctrlScroll {
		lo, _ := strconv.ParseFloat(child("Min"), 64)
		hi, err := strconv.ParseFloat(child("Max"), 64)
		if err != nil {
			hi = 100
		}
		v, _ := strconv.ParseFloat(child("Val"), 64)
		if hi > lo {
			c.pos = max(0, min(1, (v-lo)/(hi-lo)))
		}
	}
	if kind == ctrlDrop {
		c.caption = w.listItem(child("FmlaRange"), child("Sel"))
	}
	return c
}

// maxTextDepth caps the depth of a caption's markup walked. Anti-DoS
// guard, not spec-mandated: a caption's div holds a font or two.
const maxTextDepth = 16

// collectText appends the character data under e.
func collectText(e *ooxml.Element, b *strings.Builder, depth int) {
	if depth > maxTextDepth || b.Len() > 4*maxCaptionRunes {
		return
	}
	b.WriteString(e.Text)
	for _, k := range e.Kids() {
		if k.Local == "br" {
			b.WriteByte(' ')
		}
		collectText(k, b, depth+1)
	}
}

// listItem returns the text of item sel, counted from 1, of the range ref
// of the sheet, "" for none.
func (w *sheetReader) listItem(ref, sel string) string {
	rg, ok := rangeRef(ref)
	n, err := intconv.Atoi32(sel)
	if !ok || err != nil || n < 1 {
		return ""
	}
	cols := rg.LastColumn - rg.FirstColumn + 1
	if cols <= 0 {
		return ""
	}
	i := n - 1
	row, col := rg.FirstRow+i/cols, rg.FirstColumn+i%cols
	if row > rg.LastRow {
		return ""
	}
	c, ok := w.cellAt(row, col)
	if !ok {
		return ""
	}
	if c.Value.Text != "" || c.Value.Kind != sheet.Number {
		return c.Value.Text
	}
	return strconv.FormatFloat(c.Value.Number, 'g', -1, 64)
}

// picture returns the vector picture of the control.
func (c *control) picture(fonts *text.Fonts) *layout.Picture {
	pic := &layout.Picture{Width: 1, Height: 1}
	pic.Vector = func(ctx context.Context, box typeset.Rect) []typeset.Fragment {
		return c.draw(ctx, fonts, box)
	}
	return pic
}

// The colours of a control's face.
var (
	ctrlBlack  = typeset.Color{A: 1}
	ctrlBorder = typeset.Color{R: 0xAD / 255.0, G: 0xAD / 255.0, B: 0xAD / 255.0, A: 1}
	ctrlFace   = typeset.Color{R: 0xF0 / 255.0, G: 0xF0 / 255.0, B: 0xF0 / 255.0, A: 1}
	ctrlTrack  = typeset.Color{R: 0xCD / 255.0, G: 0xCD / 255.0, B: 0xCD / 255.0, A: 1}
	ctrlWhite  = typeset.Color{R: 1, G: 1, B: 1, A: 1}
	ctrlMark   = typeset.Color{R: 0x33 / 255.0, G: 0x33 / 255.0, B: 0x33 / 255.0, A: 1}
)

// markLine is the width of a check box's or an option button's outline
// and check mark.
const markLine = 0.5

// markFace returns the white face of a check box's square or an option
// button's circle with its dark outline.
func markFace(ops []typeset.PathOp) *typeset.Path {
	return &typeset.Path{Ops: ops, Fill: &typeset.Fill{Color: ctrlWhite}, Stroke: &typeset.Stroke{Color: ctrlMark, Width: markLine}}
}

// draw paints the control in box.
func (c *control) draw(ctx context.Context, fonts *text.Fonts, box typeset.Rect) []typeset.Fragment {
	var out []typeset.Fragment
	cy := box.Y + float64(box.Height/2)
	switch c.kind {
	case ctrlCheckbox, ctrlRadio:
		m := typeset.Rect{X: box.X + markInset, Y: cy - markSide/2, Width: markSide, Height: markSide}
		if c.kind == ctrlCheckbox {
			out = append(out, markFace(rectOps(m)))
			if c.checked {
				out = append(out, &typeset.Path{Ops: []typeset.PathOp{
					moveOp(m.X+float64(m.Width*0.3), m.Y+float64(m.Height*0.52)),
					lineOp(m.X+float64(m.Width*0.45), m.Y+float64(m.Height*0.68)),
					lineOp(m.X+float64(m.Width*0.72), m.Y+float64(m.Height*0.32)),
				}, Stroke: &typeset.Stroke{Color: ctrlMark, Width: markLine}})
			}
		} else {
			out = append(out, markFace(ellipseOps(m)))
			if c.checked {
				in := typeset.Rect{X: m.X + float64(m.Width*0.25), Y: m.Y + float64(m.Height*0.25), Width: m.Width / 2, Height: m.Height / 2}
				out = append(out, filled(ellipseOps(in), ctrlBlack))
			}
		}
		out = append(out, c.text(ctx, fonts, box.X+captionInset, cy, 0, false)...)
	case ctrlButton:
		out = append(out, &typeset.Path{Ops: roundRectOps(box, 2.5), Fill: &typeset.Fill{Color: ctrlWhite}, Stroke: &typeset.Stroke{Color: ctrlBorder, Width: controlLine}})
		out = append(out, c.text(ctx, fonts, box.X, cy, box.Width, true)...)
	case ctrlLabel:
		out = append(out, c.textTop(ctx, fonts, box.X+labelInsetX, box.Y+labelInsetY)...)
	case ctrlGroup:
		out = append(out, c.group(ctx, fonts, box)...)
	case ctrlDrop:
		btn := typeset.Rect{X: box.X + box.Width - min(box.Height, box.Width), Y: box.Y, Width: min(box.Height, box.Width), Height: box.Height}
		out = append(out, &typeset.Path{Ops: rectOps(box), Fill: &typeset.Fill{Color: ctrlWhite}, Stroke: &typeset.Stroke{Color: ctrlBorder, Width: controlLine}})
		out = append(out, stroked([]typeset.PathOp{moveOp(btn.X, btn.Y), lineOp(btn.X, btn.Y+btn.Height)}, ctrlBorder))
		out = append(out, arrow(btn, 'd', 0.45))
		out = append(out, c.text(ctx, fonts, box.X+dropTextInset, cy, 0, false)...)
	case ctrlList:
		col := typeset.Rect{X: box.X + box.Width - min(box.Width, 12), Y: box.Y, Width: min(box.Width, 12), Height: box.Height}
		out = append(out, &typeset.Path{Ops: rectOps(box), Fill: &typeset.Fill{Color: ctrlWhite}, Stroke: &typeset.Stroke{Color: ctrlBlack, Width: controlLine}})
		out = append(out, stroked(rectOps(col), ctrlBorder))
	case ctrlSpin, ctrlScroll:
		out = append(out, c.bar(box)...)
	}
	return []typeset.Fragment{&typeset.Group{Clip: ooxml.RectPath(box), Content: out}}
}

// bar paints a spinner or a scroll bar: arrows at its two ends, along x
// when it is horizontal or wider than tall, a scroll bar's grey track
// between them with its thumb at its place.
func (c *control) bar(box typeset.Rect) []typeset.Fragment {
	horiz := c.horiz
	if c.kind == ctrlScroll && !horiz {
		horiz = box.Width > box.Height
	}
	if c.kind == ctrlSpin {
		var a, b typeset.Rect
		if horiz {
			a = typeset.Rect{X: box.X, Y: box.Y, Width: box.Width / 2, Height: box.Height}
			b = typeset.Rect{X: box.X + float64(box.Width/2), Y: box.Y, Width: box.Width / 2, Height: box.Height}
		} else {
			a = typeset.Rect{X: box.X, Y: box.Y, Width: box.Width, Height: box.Height / 2}
			b = typeset.Rect{X: box.X, Y: box.Y + float64(box.Height/2), Width: box.Width, Height: box.Height / 2}
		}
		out := []typeset.Fragment{
			&typeset.Path{Ops: rectOps(a), Fill: &typeset.Fill{Color: ctrlFace}, Stroke: &typeset.Stroke{Color: ctrlBorder, Width: controlLine}},
			&typeset.Path{Ops: rectOps(b), Fill: &typeset.Fill{Color: ctrlFace}, Stroke: &typeset.Stroke{Color: ctrlBorder, Width: controlLine}},
		}
		if horiz {
			return append(out, arrow(a, 'l', 0.4), arrow(b, 'r', 0.4))
		}
		return append(out, arrow(a, 'u', 0.4), arrow(b, 'd', 0.4))
	}
	var a, b, track typeset.Rect
	if horiz {
		s := min(box.Height, box.Width/2)
		a = typeset.Rect{X: box.X, Y: box.Y, Width: s, Height: box.Height}
		b = typeset.Rect{X: box.X + box.Width - s, Y: box.Y, Width: s, Height: box.Height}
		track = typeset.Rect{X: box.X + s, Y: box.Y, Width: box.Width - 2*s, Height: box.Height}
	} else {
		s := min(box.Width, box.Height/2)
		a = typeset.Rect{X: box.X, Y: box.Y, Width: box.Width, Height: s}
		b = typeset.Rect{X: box.X, Y: box.Y + box.Height - s, Width: box.Width, Height: s}
		track = typeset.Rect{X: box.X, Y: box.Y + s, Width: box.Width, Height: box.Height - 2*s}
	}
	out := []typeset.Fragment{filled(rectOps(box), ctrlWhite), filled(rectOps(track), ctrlTrack)}
	if horiz {
		x := track.X + float64(track.Width*c.pos)
		out = append(out, stroked([]typeset.PathOp{moveOp(x, track.Y), lineOp(x, track.Y+track.Height)}, ctrlBorder), arrow(a, 'l', 0.45), arrow(b, 'r', 0.45))
	} else {
		y := track.Y + float64(track.Height*c.pos)
		out = append(out, stroked([]typeset.PathOp{moveOp(track.X, y), lineOp(track.X+track.Width, y)}, ctrlBorder), arrow(a, 'u', 0.45), arrow(b, 'd', 0.45))
	}
	return out
}

// group paints a group box: a black frame whose top edge its caption
// interrupts.
func (c *control) group(ctx context.Context, fonts *text.Fonts, box typeset.Rect) []typeset.Fragment {
	l, t, r, b := box.X+0.5, box.Y+0.5, box.X+box.Width-0.5, box.Y+box.Height-0.5
	lines, _ := c.layout(ctx, fonts)
	x0, x1 := l+groupCaptionX-2, l+groupCaptionX-2
	var txt []typeset.Fragment
	if lines != nil {
		x1 = l + groupCaptionX + lines.Width + 2
		base := box.Y + float64(c.size*groupBaseline)
		txt = placeRuns(lines, l+groupCaptionX, base)
	}
	ops := []typeset.PathOp{moveOp(x0, t), lineOp(l, t), lineOp(l, b), lineOp(r, b), lineOp(r, t), lineOp(min(x1, r), t)}
	return append([]typeset.Fragment{&typeset.Path{Ops: ops, Stroke: &typeset.Stroke{Color: ctrlBlack, Width: 1}}}, txt...)
}

// lineMetrics are the ascent and descent of a caption's line.
type lineMetrics struct{ ascent, descent float64 }

// layout lays the caption out on one line, nil for none.
func (c *control) layout(ctx context.Context, fonts *text.Fonts) (*text.Line, lineMetrics) {
	if fonts == nil || strings.TrimSpace(c.caption) == "" {
		return nil, lineMetrics{}
	}
	st := text.Style{Families: []string{c.family}, Size: c.size, Color: c.color}
	lines, _, err := fonts.Layout(ctx, []text.Run{{Text: c.caption, Style: st}}, 1<<16, text.Options{Whitespace: text.Preserve})
	if err != nil || len(lines) == 0 {
		return nil, lineMetrics{}
	}
	return &lines[0], lineMetrics{lines[0].Ascent, lines[0].Descent}
}

// text lays the caption out on one line centred on cy, from x, or
// centred across width when center is set.
func (c *control) text(ctx context.Context, fonts *text.Fonts, x, cy, width float64, center bool) []typeset.Fragment {
	l, m := c.layout(ctx, fonts)
	if l == nil {
		return nil
	}
	if center {
		x += float64((width - l.Width) / 2)
	}
	return placeRuns(l, x, cy+float64((m.ascent-m.descent)/2))
}

// textTop lays the caption out on one line whose top is y.
func (c *control) textTop(ctx context.Context, fonts *text.Fonts, x, y float64) []typeset.Fragment {
	l, m := c.layout(ctx, fonts)
	if l == nil {
		return nil
	}
	return placeRuns(l, x, y+m.ascent)
}

// placeRuns returns the glyph runs of l on the baseline y from x.
func placeRuns(l *text.Line, x, y float64) []typeset.Fragment {
	var out []typeset.Fragment
	for _, run := range l.Runs {
		if run.Color.A <= 0 {
			continue
		}
		t := *run
		t.Origin = typeset.Point{X: x + run.Origin.X, Y: y}
		out = append(out, &t)
	}
	return out
}

// moveOp and lineOp are path operations.
func moveOp(x, y float64) typeset.PathOp {
	return typeset.PathOp{Kind: typeset.MoveTo, P: [3]typeset.Point{{X: x, Y: y}}}
}

func lineOp(x, y float64) typeset.PathOp {
	return typeset.PathOp{Kind: typeset.LineTo, P: [3]typeset.Point{{X: x, Y: y}}}
}

// rectOps returns the outline of r.
func rectOps(r typeset.Rect) []typeset.PathOp {
	return []typeset.PathOp{moveOp(r.X, r.Y), lineOp(r.X+r.Width, r.Y), lineOp(r.X+r.Width, r.Y+r.Height), lineOp(r.X, r.Y+r.Height), {Kind: typeset.ClosePath}}
}

// kappa places the control points of a quarter ellipse.
const kappa = 0.5522847498

// ellipseOps returns the ellipse inscribed in r.
func ellipseOps(r typeset.Rect) []typeset.PathOp {
	rx, ry := float64(r.Width/2), float64(r.Height/2)
	cx, cy := r.X+rx, r.Y+ry
	kx, ky := float64(kappa*rx), float64(kappa*ry)
	curve := func(x1, y1, x2, y2, x, y float64) typeset.PathOp {
		return typeset.PathOp{Kind: typeset.CurveTo, P: [3]typeset.Point{{X: x1, Y: y1}, {X: x2, Y: y2}, {X: x, Y: y}}}
	}
	return []typeset.PathOp{
		moveOp(cx+rx, cy),
		curve(cx+rx, cy+ky, cx+kx, cy+ry, cx, cy+ry),
		curve(cx-kx, cy+ry, cx-rx, cy+ky, cx-rx, cy),
		curve(cx-rx, cy-ky, cx-kx, cy-ry, cx, cy-ry),
		curve(cx+kx, cy-ry, cx+rx, cy-ky, cx+rx, cy),
		{Kind: typeset.ClosePath},
	}
}

// roundRectOps returns r with corners of radius rad.
func roundRectOps(r typeset.Rect, rad float64) []typeset.PathOp {
	rad = max(0, min(rad, float64(r.Width/2), float64(r.Height/2)))
	k := float64(kappa * rad)
	x0, y0, x1, y1 := r.X, r.Y, r.X+r.Width, r.Y+r.Height
	curve := func(x1, y1, x2, y2, x, y float64) typeset.PathOp {
		return typeset.PathOp{Kind: typeset.CurveTo, P: [3]typeset.Point{{X: x1, Y: y1}, {X: x2, Y: y2}, {X: x, Y: y}}}
	}
	return []typeset.PathOp{
		moveOp(x0+rad, y0), lineOp(x1-rad, y0),
		curve(x1-rad+k, y0, x1, y0+rad-k, x1, y0+rad), lineOp(x1, y1-rad),
		curve(x1, y1-rad+k, x1-rad+k, y1, x1-rad, y1), lineOp(x0+rad, y1),
		curve(x0+rad-k, y1, x0, y1-rad+k, x0, y1-rad), lineOp(x0, y0+rad),
		curve(x0, y0+rad-k, x0+rad-k, y0, x0+rad, y0),
		{Kind: typeset.ClosePath},
	}
}

// arrow returns a black triangle pointing dir ('u', 'd', 'l', 'r') in
// the middle of r, of frac of r's shorter side.
func arrow(r typeset.Rect, dir byte, frac float64) *typeset.Path {
	s := float64(min(r.Width, r.Height) * frac)
	cx, cy := r.X+float64(r.Width/2), r.Y+float64(r.Height/2)
	s2, h2 := float64(s/2), float64(s/4)
	ops := make([]typeset.PathOp, 0, 4)
	switch dir {
	case 'u':
		ops = append(ops, moveOp(cx-s2, cy+h2), lineOp(cx+s2, cy+h2), lineOp(cx, cy-h2))
	case 'd':
		ops = append(ops, moveOp(cx-s2, cy-h2), lineOp(cx+s2, cy-h2), lineOp(cx, cy+h2))
	case 'l':
		ops = append(ops, moveOp(cx+h2, cy-s2), lineOp(cx+h2, cy+s2), lineOp(cx-h2, cy))
	default:
		ops = append(ops, moveOp(cx-h2, cy-s2), lineOp(cx-h2, cy+s2), lineOp(cx+h2, cy))
	}
	ops = append(ops, typeset.PathOp{Kind: typeset.ClosePath})
	return filled(ops, ctrlBlack)
}

// stroked and filled return a path stroked with a line of controlLine or
// filled in c.
func stroked(ops []typeset.PathOp, c typeset.Color) *typeset.Path {
	return &typeset.Path{Ops: ops, Stroke: &typeset.Stroke{Color: c, Width: controlLine}}
}

func filled(ops []typeset.PathOp, c typeset.Color) *typeset.Path {
	return &typeset.Path{Ops: ops, Fill: &typeset.Fill{Color: c}}
}
