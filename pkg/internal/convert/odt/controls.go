package odt

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/formfield"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/odf"
)

// controlFontSize is the size of the text a form control shows when the
// document writes its fields: the size of the default appearance
// LibreOffice gives every text and list field it exports, in points.
//
// # Corpus evidence
//
//   - libreoffice/odt odfimport__dateFormFormats.odt: twelve date fields,
//     each /DA (0 0 0 rg /He 8 Tf).
//   - probe, odt: on gotenberg/gotenberg:9.4.1, a text field, a text area,
//     a list box, a drop-down list box, a combo box and a date field of a
//     document that sets no font on its controls export with /DA at 8
//     points, Helvetica for a text field and DejaVu Sans for a list.
const controlFontSize = 8

// controlFamily is the face a form control's text is set in: Liberation
// Sans, Helvetica's metric clone, Helvetica being the face of the default
// appearance LibreOffice writes for a text field (see [controlFontSize]).
const controlFamily = "Liberation Sans"

// controlPad is the room between a control's border and its text, in
// points.
const controlPad = 2

// controlElem returns the office:forms element a draw:control draws (ODF
// 1.3, Part 3, 10.3.13 draw:control and 13.5), the element whose form:id
// or xml:id names it; nil when none does.
func (b *builder) controlElem(c *odf.Element) *odf.Element {
	id := c.A(odf.NSDraw, "control")
	if id == "" {
		return nil
	}
	forms := odf.Section(b.pkg.Content, "body").Child(odf.NSOffice, "text").Child(odf.NSOffice, "forms")
	stack := []*odf.Element{forms}
	for steps := 0; len(stack) > 0 && steps < odf.MaxElements; steps++ {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if cur == nil {
			continue
		}
		if cur.Name.Space == odf.NSForm && (cur.A(odf.NSForm, "id") == id || cur.A(odf.NSXML, "id") == id) {
			return cur
		}
		for _, k := range cur.Children {
			if !k.IsText() {
				stack = append(stack, k)
			}
		}
	}
	return nil
}

// fieldControl builds n, the box of the draw:control c of geometry g, as a
// form field when the conversion writes fields (office.Options
// FormFields): the box shows the control's value, and the control joins
// b.controls, which [formfield.Attach] makes into fields once laid out.
// A control in a header or a footer, a push button, a control that does
// not print (form:printable false) and a kind ODF 1.3, Part 3, 13.5 lists
// that holds no value stay the empty box n is. It reports whether it made
// n a field.
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
func (b *builder) fieldControl(c *odf.Element, g frameGeom, n *layout.Node) bool {
	if !b.formFields || b.hf != nil {
		return false
	}
	el := b.controlElem(c)
	if el == nil || el.A(odf.NSForm, "printable") == "false" {
		return false
	}
	f := compose.Field{
		Name:        el.A(odf.NSForm, "name"),
		Description: el.A(odf.NSForm, "title"),
		FontSize:    controlFontSize,
		ReadOnly:    el.A(odf.NSForm, "readonly") == "true" || el.A(odf.NSForm, "disabled") == "true",
		Required:    el.A(odf.NSForm, "input-required") == "true",
	}
	ctl := &formfield.Control{ID: formfield.NodeID(len(b.controls)), Box: true}
	var kids []*layout.Node
	switch el.Name.Local {
	case "text", "textarea", "formatted-text", "date", "time", "number":
		f.Kind = compose.FieldText
		f.Value = controlText(el)
		f.Multiline = el.Name.Local == "textarea"
		if v, err := strconv.Atoi(el.A(odf.NSForm, "max-length")); err == nil && v > 0 {
			// form:max-length is a non-negative integer (ODF 1.3, Part 3,
			// 19.243); a count past what a field holds is no limit.
			if v <= compose.MaxFieldText {
				f.MaxLen = v
			}
		}
		kids = b.controlLines(f.Value, g, f.Multiline)
	case "combobox":
		f.Kind, f.Editable = compose.FieldCombo, true
		f.Value = controlText(el)
		for _, it := range el.All(odf.NSForm, "item") {
			f.Options = append(f.Options, it.A(odf.NSForm, "label"))
		}
		kids = b.controlLines(f.Value, g, false)
	case "listbox":
		f.Kind = compose.FieldList
		if el.A(odf.NSForm, "dropdown") == "true" {
			f.Kind = compose.FieldCombo
		}
		f.MultiSelect = f.Kind == compose.FieldList && el.A(odf.NSForm, "multiple") == "true"
		var shown []string
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
		shown = []string{f.Value}
		if f.Kind == compose.FieldList {
			shown = f.Options
		}
		kids = b.controlLines(strings.Join(shown, "\n"), g, len(shown) > 1)
	case "checkbox", "radio":
		f.Kind = compose.FieldCheckBox
		if el.Name.Local == "radio" {
			f.Kind = compose.FieldRadio
			f.OnState = radioState(el, len(b.controls))
			f.Checked = selectedOption(el)
		} else {
			f.Checked = checkedBox(el)
		}
		mark := markVector(f.Kind)
		if f.Checked {
			kids = []*layout.Node{b.node(&layout.Node{Kind: layout.Replaced, Style: markStyle(g), Picture: &layout.Picture{Width: max(g.w, 1), Height: max(g.h, 1), Vector: func(_ context.Context, r compose.Rect) []compose.Fragment { return mark(r) }}})}
		} else {
			kids = b.controlLines("", g, false)
			ctl.Other = func(_ []compose.Fragment, r compose.Rect, _ *compose.Measurer) []compose.Fragment { return mark(r) }
		}
	default:
		return false
	}
	ctl.Field = f
	n.ID, n.Children = ctl.ID, kids
	b.controls = append(b.controls, ctl)
	return true
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
		for _, layout := range []string{"2006-01-02", "20060102"} {
			if t, err := time.Parse(layout, v); err == nil {
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

// controlLines returns the text a control of geometry g shows, s set in
// [controlFamily] at [controlFontSize], [controlPad] from the border, a
// single line centred on the box's height unless multi. An empty s shows
// a no-break space, which draws nothing and gives the control content of
// its own, so the Form element of its node holds what the field takes.
func (b *builder) controlLines(s string, g frameGeom, multi bool) []*layout.Node {
	if s == "" {
		s = " "
	}
	t := textState{size: controlFontSize, weight: 400, family: [3]string{controlFamily, controlFamily, controlFamily}}
	st := baseStyle()
	applyText(st, t, false)
	st.Display = css.Block
	st.Padding[css.Left] = css.Pts(controlPad)
	st.Padding[css.Right] = css.Pts(controlPad)
	if multi {
		st.Padding[css.Top] = css.Pts(controlPad)
	} else {
		// One line of the face is about 1.15 times the size (Liberation
		// Sans: hhea ascent 0.905 and descent 0.212 of the em).
		st.Padding[css.Top] = css.Pts(max(0, (g.h-controlFontSize*1.15)/2))
		st.WhiteSpace = css.Pre
	}
	n := b.node(&layout.Node{Kind: layout.Element, Style: st})
	n.Children = []*layout.Node{b.run(s, t)}
	return []*layout.Node{n}
}

// markStyle returns the style of the mark a checked control of geometry g
// shows: a block that fills the box.
func markStyle(g frameGeom) *css.Style {
	st := baseStyle()
	st.Display = css.Block
	st.Width, st.Height = css.Pts(max(g.w, 1)), css.Pts(max(g.h, 1))
	return st
}

// markVector returns the drawing of the on state of a check box, a cross,
// or of a radio button, a dot, in a rectangle.
func markVector(kind compose.FieldKind) func(r compose.Rect) []compose.Fragment {
	return func(r compose.Rect) []compose.Fragment {
		inset := min(r.Width, r.Height) * 0.25
		x0, y0 := r.X+inset, r.Y+inset
		x1, y1 := r.X+r.Width-inset, r.Y+r.Height-inset
		if kind == compose.FieldRadio {
			return []compose.Fragment{&compose.Path{Ops: ellipse(x0, y0, x1, y1), Fill: &compose.Fill{Color: compose.Black}}}
		}
		w := max(0.75, min(r.Width, r.Height)*0.1)
		return []compose.Fragment{&compose.Path{Ops: []compose.PathOp{
			{Kind: compose.MoveTo, P: [3]compose.Point{{X: x0, Y: y0}}},
			{Kind: compose.LineTo, P: [3]compose.Point{{X: x1, Y: y1}}},
			{Kind: compose.MoveTo, P: [3]compose.Point{{X: x1, Y: y0}}},
			{Kind: compose.LineTo, P: [3]compose.Point{{X: x0, Y: y1}}},
		}, Stroke: &compose.Stroke{Color: compose.Black, Width: w}}}
	}
}

// ellipse returns the path of the ellipse inscribed in the rectangle from
// (x0, y0) to (x1, y1), four Bézier arcs.
func ellipse(x0, y0, x1, y1 float64) []compose.PathOp {
	const k = 0.5522847498 // the control distance of a quarter circle, per radius
	cx, cy := (x0+x1)/2, (y0+y1)/2
	rx, ry := (x1-x0)/2, (y1-y0)/2
	pt := func(x, y float64) compose.Point { return compose.Point{X: x, Y: y} }
	return []compose.PathOp{
		{Kind: compose.MoveTo, P: [3]compose.Point{pt(cx+rx, cy)}},
		{Kind: compose.CurveTo, P: [3]compose.Point{pt(cx+rx, cy+k*ry), pt(cx+k*rx, cy+ry), pt(cx, cy+ry)}},
		{Kind: compose.CurveTo, P: [3]compose.Point{pt(cx-k*rx, cy+ry), pt(cx-rx, cy+k*ry), pt(cx-rx, cy)}},
		{Kind: compose.CurveTo, P: [3]compose.Point{pt(cx-rx, cy-k*ry), pt(cx-k*rx, cy-ry), pt(cx, cy-ry)}},
		{Kind: compose.CurveTo, P: [3]compose.Point{pt(cx+k*rx, cy-ry), pt(cx+rx, cy-k*ry), pt(cx+rx, cy)}},
		{Kind: compose.ClosePath},
	}
}

// attachControls makes the controls of b into the form fields of the pages
// of res, warning of those that stay printed without a field.
func (b *builder) attachControls(res *layout.Result) {
	if missed := formfield.Attach(res, b.controls); missed > 0 && len(b.controls) > 0 {
		b.warn("form-field", "a form control that spans pages or shows nothing is printed without its field")
	}
}
