package ods

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/convert/sheet"
	"github.com/gotenberg/litho/pkg/internal/odf"
	"github.com/gotenberg/litho/pkg/internal/shape"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// What Calc prints of a push button and a check box of a sheet's drawing
// layer: the label in DejaVu Sans at [controlSize] points, one line
// centred on the frame's height and clipped to it; a push button's frame
// filled grey (#c0c0c0), its label centred across it; a check box's frame
// filled white, a square of [checkSide] outlined [checkLine] wide
// [checkLine] from its left edge and centred on its height, the label
// [checkGap] right of the square, and, when checked, the ✘ of OpenSymbol
// at [checkSide] points [checkMarkX] and [checkMarkY] from the square's
// bottom-left corner.
//
// # Corpus evidence
//
//   - libreoffice/ods: sc/move-cell-anchored-shapes.ods prints its 59.4 by
//     18.8 point push button as a #c0c0c0 box of its frame, "Push Button"
//     in DejaVu Sans 8 points centred across it, the baseline 2.7 points
//     below the frame's middle.
//   - libreoffice/ods: sc/tdf106181.ods prints its 124.3 by 45.9 point
//     check box as a white box of its frame, a square of 8.022 points
//     stroked 0.794 wide at 0.794 from the frame's left edge, centred on
//     its height, and "Check Box" in DejaVu Sans 8 points 11.991 from the
//     frame's left edge, clipped to the frame; its checked state is the ✘
//     of OpenSymbol at 8.022 points the field's widget draws at (0.85,
//     1.276) in the square.
//   - probe, ods: through gotenberg/gotenberg:9.4.1 with exportFormFields
//     false, the two print otherwise: a bevelled button, a check box with
//     neither label nor cross. The rig's references keep the default,
//     true.
const (
	controlFamily = "DejaVu Sans"
	controlSize   = 8.0
	checkSide     = 8.022
	checkLine     = 0.794
	checkGap      = 3.175
	checkMarkX    = 0.85
	checkMarkY    = 1.276
)

// The fills of a push button and a check box.
var (
	buttonFill = typeset.Color{R: 0xc0 / 255.0, G: 0xc0 / 255.0, B: 0xc0 / 255.0, A: 1}
	checkFill  = typeset.Color{R: 1, G: 1, B: 1, A: 1}
)

// control adds the draw:control el of the sheet (ODF 1.3, Part 3,
// 10.3.13) as a drawing of what Calc prints for the form element it
// names (13.5): a form:button or a form:checkbox (see [controlFamily]); at
// is its anchor cell, nil for a control of the sheet. A control of another
// kind, or naming no element of the sheet's office:forms, prints nothing,
// with a warning; one that does not print (form:printable false) prints
// nothing.
func (r *reader) control(ts *tableState, el *odf.Element, at *sheet.Anchor) {
	if at != nil && (ts.rowHidden || ts.colHidden(at.Column)) {
		return
	}
	if ts.formIDs == nil {
		ts.formIDs = odf.FormIndex(ts.forms)
	}
	fe := ts.formIDs[el.A(odf.NSDraw, "control")]
	if fe == nil || (fe.Name.Local != "button" && fe.Name.Local != "checkbox") {
		r.warn("form-controls", warn.FormWidgetOmitted, warn.Omitted, "form controls other than push buttons and check boxes are not drawn")
		return
	}
	if fe.A(odf.NSForm, "printable") == "false" {
		return
	}
	button := fe.Name.Local == "button"
	fill := checkFill
	if button {
		fill = buttonFill
	}
	dx, dy := 0.0, 0.0
	if at != nil {
		dx, dy = ts.colLeft(at.Column), ts.rowY
	}
	art, ok := r.geometryArt(el, &shape.Paint{Kind: shape.PaintSolid, Color: fill}, nil, dx, dy)
	if !ok {
		return
	}
	label := fe.A(odf.NSForm, "label")
	checked := !button && odf.CheckedBox(fe)
	fonts, frameLocal := r.fonts, art.logic.local
	local := func(ctx context.Context, box typeset.Rect) []typeset.Fragment {
		out := frameLocal(ctx, box)
		textLeft := box.X
		if !button {
			y := box.Y + float64((box.Height-checkSide)/2)
			x := box.X + checkLine
			out = append(out, &typeset.Path{Ops: rectPath(x, y, checkSide, checkSide), Stroke: &typeset.Stroke{Color: typeset.Black, Width: checkLine}})
			if mark, ok := oneLine(ctx, fonts, "\u2718", "OpenSymbol", checkSide); checked && ok {
				out = append(out, placeLine(mark, x+checkMarkX, y+checkSide-checkMarkY)...)
			}
			textLeft = x + checkSide + checkGap
		}
		l, ok := oneLine(ctx, fonts, label, controlFamily, controlSize)
		if !ok {
			return out
		}
		// One line, its ascent and descent centred on the frame's height,
		// a push button's centred across it too.
		x := textLeft
		if button {
			x += float64((box.Width - l.Width) / 2)
		}
		base := box.Y + float64((box.Height-l.Ascent-l.Descent)/2) + l.Ascent
		clip := &typeset.Path{Ops: rectPath(textLeft, box.Y, box.X+box.Width-textLeft, box.Height)}
		return append(out, &typeset.Group{Opacity: 1, Clip: clip, Content: placeLine(l, x, base)})
	}
	nb := *art.logic
	nb.local = local
	m := nb.lin
	m.E, m.F = nb.x, nb.y
	art.logic = &nb
	art.text = label
	art.draw = func(ctx context.Context) []typeset.Fragment {
		return []typeset.Fragment{&typeset.Group{Transform: m, Opacity: 1, Content: local(ctx, typeset.Rect{Width: nb.w, Height: nb.h})}}
	}
	r.place(ts, el, at, art)
}

// oneLine lays s out on one line in family at size; false when s is
// empty or does not lay out.
func oneLine(ctx context.Context, fonts *text.Fonts, s, family string, size float64) (text.Line, bool) {
	if fonts == nil || s == "" {
		return text.Line{}, false
	}
	ls, _, err := fonts.Layout(ctx, []text.Run{{Text: s, Style: text.Style{Families: []string{family}, Size: size, Color: typeset.Black}}}, 1<<16, text.Options{Whitespace: text.Preserve})
	if err != nil || len(ls) == 0 {
		return text.Line{}, false
	}
	return ls[0], true
}

// placeLine returns the glyph runs of l, its baseline starting at (x, y).
func placeLine(l text.Line, x, y float64) []typeset.Fragment {
	out := make([]typeset.Fragment, 0, len(l.Runs))
	for _, run := range l.Runs {
		t := *run
		t.Origin = typeset.Point{X: x + run.Origin.X, Y: y}
		out = append(out, &t)
	}
	return out
}

// rectPath returns the path of the rectangle at (x, y) of size w by h.
func rectPath(x, y, w, h float64) []typeset.PathOp {
	return []typeset.PathOp{
		{Kind: typeset.MoveTo, P: [3]typeset.Point{{X: x, Y: y}}},
		{Kind: typeset.LineTo, P: [3]typeset.Point{{X: x + w, Y: y}}},
		{Kind: typeset.LineTo, P: [3]typeset.Point{{X: x + w, Y: y + h}}},
		{Kind: typeset.LineTo, P: [3]typeset.Point{{X: x, Y: y + h}}},
		{Kind: typeset.ClosePath},
	}
}
