package odt

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/gotenberg/litho/pkg/internal/convert/formfield"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/intconv"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/odf"
	"github.com/gotenberg/litho/pkg/internal/typeset"
	"github.com/gotenberg/litho/pkg/internal/warn"
)

// The faces, sizes and places LibreOffice prints a form control's content
// in, as it prints a document without its form fields (exportFormFields
// false) and as the appearance of each field litho writes draws it: a
// text, combo box or list box line in Liberation Sans at 8 points, a
// drop-down list box's line in DejaVu Sans at 12 points, each [controlPad]
// from the frame's left edge, one line centred on the frame's height, a
// list box's items [listPitch] apart from its top.
//
// # Corpus evidence
//
//   - probe, odt: on gotenberg/gotenberg:9.4.1 with exportFormFields
//     false, a 141.7 by 17.1 point text field prints its value in
//     Liberation Sans 8 points 2.1 points right of its frame, its baseline
//     5.8 points above the frame's bottom, the face's ascent and descent
//     centred; a text area prints its value on one line, its line breaks
//     left out, centred alike; a combo box, a date and a formatted field
//     print alike; a drop-down list box prints its selected item in DejaVu
//     Sans 12 points, 4.4 points above the bottom of a 17.1 point frame; a
//     list box prints its items in Liberation Sans 8 points, the first
//     baseline 7.3 points below its top and the next 9.3 points apart,
//     each selected item white on a black bar; the frames are 0.2 point
//     black lines.
//   - libreoffice/odt: odfimport/dateFormFormats.odt: twelve date fields,
//     each /DA (0 0 0 rg /He 8 Tf) when LibreOffice writes its fields.
const (
	controlFontSize = 8
	dropFontSize    = 12
	controlPad      = 2.1
	listPitch       = 9.3
	controlFrame    = 0.2
)

// The faces of [controlFontSize] and [dropFontSize], and the ascent plus
// descent of each, as fractions of the em (hhea of Liberation Sans 2.1.5:
// 0.905 and 0.212; of DejaVu Sans 2.37: 0.928 and 0.236).
const (
	controlFamily = "Liberation Sans"
	dropFamily    = "DejaVu Sans"
	controlLine   = 1.117
	dropLine      = 1.164
)

// markScale is the side of the square, or the diameter of the circle, a
// check box or a radio button prints at the top-left corner of its frame,
// as a fraction of the frame's shorter side.
//
// # Corpus evidence
//
//   - probe, odt: on gotenberg/gotenberg:9.4.1 with exportFormFields
//     false, a 0.4 cm (11.3 point) check box prints a black square of 8.5
//     points with a 0.6 point border at its frame's top-left corner, a
//     cross 1.5 points inside it when checked, 0.6 point thick; a radio
//     button prints a ring of 8.4 points, 0.6 point thick, and a dot of 6
//     points when selected; the control's form:label is drawn under an
//     empty clip, so nothing of it shows. A push button prints a grey (#c0c0c0) box
//     with its label centred. A control in a page header, and one whose
//     form:printable is false, print nothing.
const markScale = 0.75

// controlElem returns the office:forms element a draw:control draws (ODF
// 1.3, Part 3, 10.3.13 draw:control and 13.5), the element whose form:id
// or xml:id names it; nil when none does.
func (b *builder) controlElem(c *odf.Element) *odf.Element {
	id := c.A(odf.NSDraw, "control")
	if id == "" {
		return nil
	}
	if b.formIDs == nil {
		b.formIDs = formIndex(odf.Section(b.pkg.Content, "body").Child(odf.NSOffice, "text").Child(odf.NSOffice, "forms"))
	}
	return b.formIDs[id]
}

// formIndex maps each form:id and xml:id of the form elements under
// forms to the first element that carries it, in the order a depth-first
// walk from the last child meets them. One walk serves every control: a
// walk per control would cost the controls times the forms.
func formIndex(forms *odf.Element) map[string]*odf.Element {
	out := map[string]*odf.Element{}
	stack := []*odf.Element{forms}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if cur == nil {
			continue
		}
		if cur.Name.Space == odf.NSForm {
			for _, id := range []string{cur.A(odf.NSForm, "id"), cur.A(odf.NSXML, "id")} {
				if _, seen := out[id]; id != "" && !seen {
					out[id] = cur
				}
			}
		}
		for _, k := range cur.Children {
			if !k.IsText() {
				stack = append(stack, k)
			}
		}
	}
	return out
}

// formControl builds the box of the draw:control c of geometry g (ODF 1.3,
// Part 3, 10.3.13): the frame, in its size, and what the control shows,
// as LibreOffice prints it (see [controlFontSize] and [markScale]). A
// control in a
// header or a footer, or one that does not print (form:printable false),
// keeps its room and prints nothing. When the conversion writes fields
// (office.Options FormFields), a control of the kinds [controlField]
// lists joins b.controls, which [formfield.Attach] makes into fields once
// laid out, its box the node of the Role Form that holds what it shows.
func (b *builder) formControl(c *odf.Element, g frameGeom) *layout.Node {
	st := b.frameBox(g)
	st.Display = css.InlineBlock
	for i := range st.Padding {
		st.Padding[i] = css.Pts(0)
	}
	st.Border = [4]css.Border{}
	n := b.node(&layout.Node{Kind: layout.Element, Style: st, Role: "Form"})
	el := b.controlElem(c)
	if el == nil {
		b.warn("form-control", warn.FormWidgetOmitted, warn.Omitted, "a form control whose element the forms do not hold prints as its empty frame")
		frame(st)
		n.PrintField = typeset.PrintField{Role: typeset.PrintText}
		return n
	}
	if b.hf != nil || el.A(odf.NSForm, "printable") == "false" {
		return n
	}
	f, ok := controlField(el, len(b.controls))
	n.PrintField = printedControl(el, &f)
	var other func([]typeset.Fragment, typeset.Rect, *typeset.Measurer) []typeset.Fragment
	switch el.Name.Local {
	case "text", "formatted-text", "date", "time", "number", "combobox":
		frame(st)
		n.Children = b.controlLine(controlText(el), g, controlFamily, controlFontSize, controlLine)
	case "textarea":
		frame(st)
		n.Children = b.controlLine(strings.ReplaceAll(controlText(el), "\n", ""), g, controlFamily, controlFontSize, controlLine)
	case "listbox":
		frame(st)
		if el.A(odf.NSForm, "dropdown") == "true" {
			n.Children = b.controlLine(f.Value, g, dropFamily, dropFontSize, dropLine)
			break
		}
		n.Children = b.listRows(el)
	case "checkbox", "radio":
		mark := markVector(f.Kind)
		on, off := mark(true), mark(false)
		if !f.Checked {
			on, off = off, on
		}
		n.Children = []*layout.Node{b.node(&layout.Node{Kind: layout.Replaced, Style: markStyle(g), Picture: &layout.Picture{
			Width: max(g.w, 1), Height: max(g.h, 1),
			Vector: func(_ context.Context, r typeset.Rect) []typeset.Fragment { return on(r) },
		}})}
		other = func(_ []typeset.Fragment, r typeset.Rect, _ *typeset.Measurer) []typeset.Fragment { return off(r) }
	case "button":
		st.BackgroundColor = css.Color{R: 0.753, G: 0.753, B: 0.753, A: 1}
		n.Children = b.controlLine(el.A(odf.NSForm, "label"), g, controlFamily, controlFontSize, controlLine)
		n.Children[0].Style.TextAlign = css.AlignCenter
	default:
		frame(st)
		b.warn("form-control", warn.FormWidgetOmitted, warn.Omitted, "a form control of a kind litho does not draw prints as its empty frame")
	}
	if ok && b.formFields {
		ctl := &formfield.Control{ID: formfield.NodeID(len(b.controls)), Box: true, Field: f, Other: other}
		n.ID = ctl.ID
		b.controls = append(b.controls, ctl)
	}
	return n
}

// printedControl returns the PrintField attributes of the Form element of
// the control el, f the field [controlField] reads from it: the field's
// own (formfield.Printed), pb for a push button or an image button
// (form:button and form:image, ODF 1.3, Part 3, 13.5), and tv, the role of a value shown as
// text, for a kind no field takes, which prints as its frame. A Form
// element that holds content and no widget needs a role (ISO 14289-1,
// 7.18.4), and these attributes are written only where the control
// becomes no widget.
//
// # Corpus evidence
//
//   - litho/odt form-controls, converted by LibreOffice on
//     gotenberg/gotenberg:9.4.1 with exportFormFields false and
//     nativePdfua PDF/UA-1: each of the 21 printed controls is a Figure
//     with a Layout BBox and Placement and neither Alt nor ActualText,
//     which veraPDF refuses under ISO 14289-1, 7.3; no Form element, no
//     PrintField attribute.
func printedControl(el *odf.Element, f *typeset.Field) typeset.PrintField {
	pf := formfield.Printed(f)
	switch el.Name.Local {
	case "button", "image":
		pf.Role, pf.Checked = typeset.PrintPushButton, typeset.CheckUnset
	case "text", "textarea", "formatted-text", "date", "time", "number",
		"combobox", "listbox", "checkbox", "radio":
	default:
		pf.Role, pf.Checked = typeset.PrintText, typeset.CheckUnset
	}
	return pf
}

// frame gives st the frame LibreOffice prints around a text or list
// control: a black line of [controlFrame] points.
func frame(st *css.Style) {
	bd := css.Border{Width: controlFrame, Style: css.Solid, Color: css.Color{A: 1}}
	st.Border = [4]css.Border{bd, bd, bd, bd}
}

// controlField returns the field the form control el becomes, the n-th
// control made a field, false for a kind no field takes: a push button, a
// hidden, image, grid, file, password or fixed-text control.
//
// # Corpus evidence
//
//   - probe, odt: on gotenberg/gotenberg:9.4.1, LibreOffice exports a
//     form:text, form:textarea, form:formatted-text and form:date as a
//     text field, the text area multiline; a form:checkbox as a check
//     box; the form:radio of one form:name as one radio button group with
//     the Radio and NoToggleToOff flags; a form:listbox as a list box, a
//     drop-down one as a combo box, a form:multiple one with MultiSelect;
//     a form:combobox as an editable combo box; form:readonly as ReadOnly.
//     The options are written by their labels. A second control of one
//     name takes the suffix _2. A control with form:printable false is
//     neither printed nor exported, a form:button is a push button, and a
//     control in a page header is neither printed nor exported. The /TU
//     of every field is its name, form:title set or not. The widget of a
//     control sits about 2 points inside its frame.
func controlField(el *odf.Element, n int) (typeset.Field, bool) {
	f := typeset.Field{
		Name:        el.A(odf.NSForm, "name"),
		Description: el.A(odf.NSForm, "title"),
		FontSize:    controlFontSize,
		ReadOnly:    el.A(odf.NSForm, "readonly") == "true" || el.A(odf.NSForm, "disabled") == "true",
		Required:    el.A(odf.NSForm, "input-required") == "true",
	}
	switch el.Name.Local {
	case "text", "textarea", "formatted-text", "date", "time", "number":
		f.Kind = typeset.FieldText
		f.Value = controlText(el)
		f.Multiline = el.Name.Local == "textarea"
		if v, err := intconv.Atoi(el.A(odf.NSForm, "max-length")); err == nil && v > 0 && v <= typeset.MaxFieldText {
			// form:max-length is a non-negative integer (ODF 1.3, Part 3,
			// 19.243); a count past what a field holds is no limit.
			f.MaxLen = v
		}
	case "combobox":
		f.Kind, f.Editable = typeset.FieldCombo, true
		f.Value = controlText(el)
		for _, it := range el.All(odf.NSForm, "item") {
			f.Options = append(f.Options, it.A(odf.NSForm, "label"))
		}
	case "listbox":
		f.Kind = typeset.FieldList
		if el.A(odf.NSForm, "dropdown") == "true" {
			f.Kind = typeset.FieldCombo
			f.FontSize = dropFontSize
		}
		f.MultiSelect = f.Kind == typeset.FieldList && el.A(odf.NSForm, "multiple") == "true"
		for i, o := range el.All(odf.NSForm, "option") {
			label := o.A(odf.NSForm, "label")
			f.Options = append(f.Options, label)
			if selectedOption(o) {
				f.Selected = append(f.Selected, i)
				if f.Value == "" {
					f.Value = label
				}
			}
		}
		if !f.MultiSelect {
			f.Selected = nil
		}
	case "checkbox":
		f.Kind, f.Checked = typeset.FieldCheckBox, checkedBox(el)
	case "radio":
		f.Kind, f.Checked = typeset.FieldRadio, selectedOption(el)
		f.OnState = radioState(el, n)
	default:
		return f, false
	}
	return f, true
}

// controlText returns the value a text-like control shows: its
// form:current-value, else its form:value; a date (form:date, ODF 1.3,
// Part 3, 19.230) as LibreOffice's default short date of the en-US
// locale, MM/DD/YY, unless the control's Text property says how it shows.
func controlText(el *odf.Element) string {
	if el.Name.Local == "date" {
		for _, p := range el.Child(odf.NSForm, "properties").All(odf.NSForm, "property") {
			if p.A(odf.NSForm, "property-name") == "Text" {
				return p.A(odf.NSOffice, "string-value")
			}
		}
	}
	v, ok := el.Lookup(odf.NSForm, "current-value")
	if !ok {
		v = el.A(odf.NSForm, "value")
	}
	if el.Name.Local == "date" && v != "" {
		for _, pattern := range []string{"2006-01-02", "20060102"} {
			if t, err := time.Parse(pattern, v); err == nil {
				return t.Format("01/02/06")
			}
		}
	}
	return v
}

// selectedOption reports a form:option or form:radio that is selected:
// form:current-selected, else form:selected (ODF 1.3, Part 3, 19.254 and
// 19.267).
func selectedOption(el *odf.Element) bool {
	if v, ok := el.Lookup(odf.NSForm, "current-selected"); ok {
		return v == "true"
	}
	return el.A(odf.NSForm, "selected") == "true"
}

// checkedBox reports a form:checkbox that is checked: form:current-state,
// else form:state (19.255 and 19.268).
func checkedBox(el *odf.Element) bool {
	if v, ok := el.Lookup(odf.NSForm, "current-state"); ok {
		return v == "checked"
	}
	return el.A(odf.NSForm, "state") == "checked"
}

// radioState returns the on state of a radio button, which names it in
// its group: its form:value, else its form:label, else its position among
// the controls.
func radioState(el *odf.Element, n int) string {
	for _, v := range []string{el.A(odf.NSForm, "value"), el.A(odf.NSForm, "label")} {
		if v != "" && v != "Off" {
			return v
		}
	}
	return "Choice_" + strconv.Itoa(n+1)
}

// controlLine returns the text a control of geometry g shows on one
// line: s set in family at size, [controlPad] from the frame's left edge,
// the face's ascent and descent, line of the em, centred on the frame's
// height. An empty s shows a no-break space, which draws nothing and gives
// the control content of its own, so the Form element of its node holds
// what a field takes.
func (b *builder) controlLine(s string, g frameGeom, family string, size, line float64) []*layout.Node {
	if s == "" {
		s = "\u00a0"
	}
	t := textState{size: size, weight: 400, family: [3]string{family, family, family}}
	st := baseStyle()
	applyText(st, t, false)
	st.Display = css.Block
	st.WhiteSpace = css.Pre
	st.LineHeight = css.Pts(size * line)
	st.Padding[css.Left] = css.Pts(controlPad)
	st.Padding[css.Right] = css.Pts(controlPad)
	st.Padding[css.Top] = css.Pts(max(0, (g.h-float64(size*line))/2))
	n := b.node(&layout.Node{Kind: layout.Element, Style: st})
	n.Children = []*layout.Node{b.run(s, t)}
	return []*layout.Node{n}
}

// listRows returns the items of the list box el, one a row of
// [listPitch], each selected one white on black.
func (b *builder) listRows(el *odf.Element) []*layout.Node {
	opts := el.All(odf.NSForm, "option")
	rows := make([]*layout.Node, 0, len(opts))
	for _, o := range opts {
		label := o.A(odf.NSForm, "label")
		if label == "" {
			label = "\u00a0"
		}
		t := textState{size: controlFontSize, weight: 400, family: [3]string{controlFamily, controlFamily, controlFamily}}
		if selectedOption(o) {
			t.color = &css.Color{R: 1, G: 1, B: 1, A: 1}
		}
		st := baseStyle()
		applyText(st, t, false)
		st.Display = css.Block
		st.WhiteSpace = css.Pre
		st.LineHeight = css.Pts(listPitch)
		st.Padding[css.Left] = css.Pts(controlPad)
		if selectedOption(o) {
			st.BackgroundColor = css.Color{A: 1}
		}
		row := b.node(&layout.Node{Kind: layout.Element, Style: st})
		row.Children = []*layout.Node{b.run(label, t)}
		rows = append(rows, row)
	}
	if len(rows) == 0 {
		return b.controlLine("", frameGeom{}, controlFamily, controlFontSize, controlLine)
	}
	return rows
}

// markStyle returns the style of the drawing of a check box or a radio
// button of geometry g: a block that fills its frame.
func markStyle(g frameGeom) *css.Style {
	st := baseStyle()
	st.Display = css.Block
	st.Width, st.Height = css.Pts(max(g.w, 1)), css.Pts(max(g.h, 1))
	return st
}

// markVector returns, for a check box or a radio button, the drawing of
// its on state (on) or its off state in a rectangle, the control's frame:
// a square with a cross, or a ring with a dot, of [markScale] of the
// frame's shorter side at its top-left corner.
func markVector(kind typeset.FieldKind) func(on bool) func(r typeset.Rect) []typeset.Fragment {
	const line = 0.6
	return func(on bool) func(r typeset.Rect) []typeset.Fragment {
		return func(r typeset.Rect) []typeset.Fragment {
			s := float64(min(r.Width, r.Height) * markScale)
			x0, y0 := r.X, r.Y
			black, white := typeset.Black, typeset.Color{R: 1, G: 1, B: 1, A: 1}
			if kind == typeset.FieldRadio {
				out := []typeset.Fragment{
					&typeset.Path{Ops: ellipse(x0, y0, x0+s, y0+s), Fill: &typeset.Fill{Color: black}},
					&typeset.Path{Ops: ellipse(x0+line, y0+line, x0+s-line, y0+s-line), Fill: &typeset.Fill{Color: white}},
				}
				if on {
					d := float64(s * 0.15)
					out = append(out, &typeset.Path{Ops: ellipse(x0+d, y0+d, x0+s-d, y0+s-d), Fill: &typeset.Fill{Color: black}})
				}
				return out
			}
			out := []typeset.Fragment{
				&typeset.Path{Ops: box(x0, y0, s, s), Fill: &typeset.Fill{Color: black}},
				&typeset.Path{Ops: box(x0+line, y0+line, s-2*line, s-2*line), Fill: &typeset.Fill{Color: white}},
			}
			if on {
				in := 1.5
				out = append(out, &typeset.Path{Ops: []typeset.PathOp{
					{Kind: typeset.MoveTo, P: [3]typeset.Point{{X: x0 + in, Y: y0 + in}}},
					{Kind: typeset.LineTo, P: [3]typeset.Point{{X: x0 + s - in, Y: y0 + s - in}}},
					{Kind: typeset.MoveTo, P: [3]typeset.Point{{X: x0 + s - in, Y: y0 + in}}},
					{Kind: typeset.LineTo, P: [3]typeset.Point{{X: x0 + in, Y: y0 + s - in}}},
				}, Stroke: &typeset.Stroke{Color: black, Width: line}})
			}
			return out
		}
	}
}

// box returns the path of the rectangle at (x, y) of size w by h.
func box(x, y, w, h float64) []typeset.PathOp {
	return []typeset.PathOp{
		{Kind: typeset.MoveTo, P: [3]typeset.Point{{X: x, Y: y}}},
		{Kind: typeset.LineTo, P: [3]typeset.Point{{X: x + w, Y: y}}},
		{Kind: typeset.LineTo, P: [3]typeset.Point{{X: x + w, Y: y + h}}},
		{Kind: typeset.LineTo, P: [3]typeset.Point{{X: x, Y: y + h}}},
		{Kind: typeset.ClosePath},
	}
}

// ellipse returns the path of the ellipse inscribed in the rectangle from
// (x0, y0) to (x1, y1), four Bézier arcs.
func ellipse(x0, y0, x1, y1 float64) []typeset.PathOp {
	const k = 0.5522847498 // the control distance of a quarter circle, per radius
	cx, cy := float64((x0+x1)/2), float64((y0+y1)/2)
	rx, ry := float64((x1-x0)/2), float64((y1-y0)/2)
	pt := func(x, y float64) typeset.Point { return typeset.Point{X: x, Y: y} }
	return []typeset.PathOp{
		{Kind: typeset.MoveTo, P: [3]typeset.Point{pt(cx+rx, cy)}},
		{Kind: typeset.CurveTo, P: [3]typeset.Point{pt(cx+rx, cy+float64(k*ry)), pt(cx+float64(k*rx), cy+ry), pt(cx, cy+ry)}},
		{Kind: typeset.CurveTo, P: [3]typeset.Point{pt(cx-float64(k*rx), cy+ry), pt(cx-rx, cy+float64(k*ry)), pt(cx-rx, cy)}},
		{Kind: typeset.CurveTo, P: [3]typeset.Point{pt(cx-rx, cy-float64(k*ry)), pt(cx-float64(k*rx), cy-ry), pt(cx, cy-ry)}},
		{Kind: typeset.CurveTo, P: [3]typeset.Point{pt(cx+float64(k*rx), cy-ry), pt(cx+rx, cy-float64(k*ry)), pt(cx+rx, cy)}},
		{Kind: typeset.ClosePath},
	}
}

// attachControls makes the controls of b into the form fields of the pages
// of res, warning of those that stay printed without a field. It fails as
// formfield.Attach does.
func (b *builder) attachControls(res *layout.Result) error {
	missed, err := formfield.Attach(b.ctx, res, b.controls)
	if err != nil {
		return err
	}
	if missed > 0 && len(b.controls) > 0 {
		b.warn("form-field", warn.FormFieldOmitted, warn.Approximated, "a form control that spans pages or shows nothing is printed without its field")
	}
	return nil
}
