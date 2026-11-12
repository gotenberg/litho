package docx

import (
	"context"
	"strconv"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/formfield"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/ooxml"
)

// formState is what the document knows of the form controls it makes
// fields (office.Options FormFields).
type formState struct {
	controls []*formfield.Control
	// open counts the controls being read: what a control holds is its
	// content, not a control of its own.
	open int
}

// fieldsOn reports whether the control being read becomes a field: the
// conversion writes fields, the story is not a header or a footer, whose
// content repeats on every page, and no control holds it.
func (d *document) fieldsOn() bool {
	return d.opts.FormFields && !d.inMargin && d.form.open == 0
}

// sdtField returns the field an inline content control (ECMA-376 Part 1,
// 17.5.2) becomes, from its properties pr and its content, false for a
// kind that holds no value a reader types or picks: a picture, a
// building block gallery, a group, a citation, a bibliography, an
// equation or a repeating section.
//
// A plain text control (17.5.2.44 text) and a rich text control, one that
// names no kind, are text fields, multiline when the text control says so
// (multiLine) or for rich text; a date picker (17.5.2.7 date) is a text
// field; a drop-down list (17.5.2.15 dropDownList) is a combo box, and a
// combo box (17.5.2.5 comboBox) an editable one, each with its items'
// display texts; the Office 2010 check box (w14:checkbox, MS-DOCX 2.6.1)
// is a check box. Its name is the control's tag (17.5.2.42), else its
// alias (17.5.2.1), which is also its description; a control whose
// content is locked (17.5.2.23 lock) is read-only. A control that shows
// its placeholder text (17.5.2.39 showingPlcHdr) holds no value.
//
// # Corpus evidence
//
//   - litho/docx content-controls-choice and content-controls-text,
//     converted by LibreOffice on gotenberg/gotenberg:9.4.1: every inline text, rich text and date
//     control becomes a text field, a drop-down list a combo box, a combo
//     box an editable one with its display texts as options, a check box
//     a check box; the alias is the field's /TU. LibreOffice names its
//     fields "Widget", "_2", "_3" and so on, from no source attribute.
//   - litho/docx form-fields-legacy, converted by LibreOffice on
//     gotenberg/gotenberg:9.4.1: no field.
func sdtField(pr *ooxml.Element, content *ooxml.Element) (compose.Field, bool) {
	f := compose.Field{Kind: compose.FieldText}
	tag, alias := attrOr(pr.Child(w, "tag"), "val"), attrOr(pr.Child(w, "alias"), "val")
	f.Name, f.Description = tag, alias
	if f.Name == "" {
		f.Name = alias
	}
	switch attrOr(pr.Child(w, "lock"), "val") {
	case "sdtContentLocked", "contentLocked":
		f.ReadOnly = true
	}
	known := false
	for _, k := range pr.Kids() {
		switch {
		case k.Is(w, "text"):
			known = true
			v := attrOr(k, "multiLine")
			f.Multiline = v == "1" || v == "true" || v == "on"
		case k.Is(w, "date"):
			known = true
		case k.Is(w, "dropDownList"), k.Is(w, "comboBox"):
			known = true
			f.Kind = compose.FieldCombo
			f.Editable = k.Is(w, "comboBox")
			for _, it := range k.Kids() {
				if it.Is(w, "listItem") {
					label := attrOr(it, "displayText")
					if label == "" {
						label = attrOr(it, "value")
					}
					f.Options = append(f.Options, label)
				}
			}
		case k.Is(ooxml.NSW14, "checkbox"):
			known = true
			f.Kind = compose.FieldCheckBox
			v, _ := k.Child(ooxml.NSW14, "checked").AttrNS(ooxml.NSW14, "val")
			f.Checked = v == "1" || v == "true"
		case k.Is(w, "picture"), k.Is(w, "docPartObj"), k.Is(w, "docPartList"), k.Is(w, "group"),
			k.Is(w, "citation"), k.Is(w, "bibliography"), k.Is(w, "equation"),
			k.Local == "repeatingSection" || k.Local == "repeatingSectionItem":
			return f, false
		}
	}
	if !known {
		// A rich text control (17.5.2.31 richText, or no kind at all).
		f.Multiline = true
	}
	if f.Kind != compose.FieldCheckBox && pr.Child(w, "showingPlcHdr") == nil {
		f.Value = runText(content)
	}
	return f, true
}

// maxControlText bounds the characters [runText] gathers from a control.
// Anti-DoS guard, not spec-mandated: a field holds at most
// compose.MaxFieldText bytes.
const maxControlText = compose.MaxFieldText / 4

// runText returns the text the runs under e show: their w:t, a tab for a
// w:tab and a line break for a w:br or w:cr, a paragraph's end a line
// break too; deleted text left out.
func runText(e *ooxml.Element) string {
	var b strings.Builder
	n := 0
	stack := []*ooxml.Element{e}
	for len(stack) > 0 && n < maxControlText {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if cur == nil {
			continue
		}
		switch {
		case cur.Is(w, "t"):
			b.WriteString(cur.Text)
			n += len(cur.Text)
			continue
		case cur.Is(w, "tab"):
			b.WriteByte('\t')
		case cur.Is(w, "br"), cur.Is(w, "cr"):
			b.WriteByte('\n')
		case cur.Is(w, "del"), cur.Is(w, "rPr"), cur.Is(w, "pPr"), cur.Is(w, "instrText"):
			continue
		case cur.Is(w, "p") && cur != e && b.Len() > 0:
			b.WriteByte('\n')
		}
		for i := len(cur.Children) - 1; i >= 0; i-- {
			stack = append(stack, cur.Children[i])
		}
		n++
	}
	return strings.ToValidUTF8(strings.TrimRight(b.String(), "\n"), "�")
}

// inlineControl builds an inline content control (17.5.2.30 sdt in a
// paragraph): its content, and, when the control becomes a field
// ([sdtField]), the node of the Role Form that holds it, which
// [formfield.Attach] turns into the field once laid out.
func (d *document) inlineControl(s *story, rp resolvedPara, ps *css.Style, lineMult float64, e *ooxml.Element, sink *inlineSink, depth int) {
	content := e.Child(w, "sdtContent")
	var f compose.Field
	ok := d.fieldsOn()
	if ok {
		f, ok = sdtField(e.Child(w, "sdtPr"), content)
	}
	if !ok {
		d.inlines(s, rp, ps, lineMult, content, sink, depth+1)
		return
	}
	ctl := &formfield.Control{ID: formfield.NodeID(len(d.form.controls)), Field: f}
	if f.Kind == compose.FieldCheckBox {
		ctl.Other = otherSymbol(e.Child(w, "sdtPr").Child(ooxml.NSW14, "checkbox"), f.Checked)
	}
	d.form.open++
	sink.openLink("")
	d.inlines(s, rp, ps, lineMult, content, sink, depth+1)
	d.form.open--
	d.closeControl(sink, ctl)
}

// closeControl ends the frame the control ctl's content was collected in
// and adds it as one inline element of the Role Form, the control a field
// when it holds content.
func (d *document) closeControl(sink *inlineSink, ctl *formfield.Control) {
	if len(sink.links) == 0 {
		return
	}
	fr := sink.links[len(sink.links)-1]
	sink.links = sink.links[:len(sink.links)-1]
	if len(fr.nodes) == 0 {
		return
	}
	st := *fr.nodes[0].Style
	st.Display = css.Inline
	if ctl.Field.FontSize == 0 {
		ctl.Field.FontSize = st.FontSize
	}
	if c := st.Color; c.A > 0 {
		ctl.Field.Color = compose.Color{R: c.R, G: c.G, B: c.B, A: c.A}
	}
	sink.add(&layout.Node{Kind: layout.Element, Style: inlineStyle(&st), Role: "Form", ID: ctl.ID, Children: fr.nodes})
	d.form.controls = append(d.form.controls, ctl)
}

// otherSymbol returns how a check box content control (MS-DOCX 2.6.1)
// draws the state it does not show: the symbol of w14:checkedState or
// w14:uncheckedState, a hexadecimal character code (MS-DOCX 2.6.3,
// ST_ShortHexNumber), in place of the shown symbol, in its face, when the
// face holds it; nothing when it does not. Word's defaults are U+2612 and
// U+2610.
func otherSymbol(cb *ooxml.Element, checked bool) func([]compose.Fragment, compose.Rect, *compose.Measurer) []compose.Fragment {
	state, def := "checkedState", rune(0x2612)
	if checked {
		state, def = "uncheckedState", 0x2610
	}
	r := def
	if v, ok := cb.Child(ooxml.NSW14, state).AttrNS(ooxml.NSW14, "val"); ok {
		if n, err := strconv.ParseUint(v, 16, 16); err == nil && n > 0 {
			r = rune(n)
		}
	}
	return func(shown []compose.Fragment, _ compose.Rect, m *compose.Measurer) []compose.Fragment {
		return swapGlyph(shown, r, m)
	}
}

// swapGlyph returns a copy of frags, the runs a symbol draws, whose first
// glyph draws r instead, in its run's face; nil when no run holds a glyph
// or the face lacks r.
func swapGlyph(frags []compose.Fragment, r rune, m *compose.Measurer) []compose.Fragment {
	out := make([]compose.Fragment, 0, len(frags))
	done := false
	for _, f := range frags {
		switch v := f.(type) {
		case *compose.Text:
			if !done && v != nil && len(v.Glyphs) > 0 {
				gid, ok := m.Glyph(v.Face, r)
				if !ok {
					return nil
				}
				t := *v
				t.Glyphs = append([]compose.Glyph{{GID: gid, X: v.Glyphs[0].X, Y: v.Glyphs[0].Y, Text: string(r)}}, v.Glyphs[1:]...)
				out = append(out, &t)
				done = true
				continue
			}
		case *compose.Group:
			if !done && v != nil {
				if k := swapGlyph(v.Content, r, m); k != nil {
					g := *v
					g.Content = k
					out = append(out, &g)
					done = true
					continue
				}
			}
		}
		out = append(out, f)
	}
	if !done {
		return nil
	}
	return out
}

// legacyField returns the field a legacy form field (17.16.5.20
// FORMTEXT, 17.16.5.19 FORMDROPDOWN, 17.16.5.18 FORMCHECKBOX) becomes,
// from its instruction and the w:ffData of its begin character (17.16.17),
// false for another field. Its name is ffData's w:name (17.16.27), its
// description the help text (17.16.21 helpText), a field that is not
// enabled (17.16.13 enabled) read-only. A text input's maximum length is
// w:maxLength (17.16.26); a drop-down's items are its w:listEntry
// (17.16.23) and its value the w:result-th item (17.16.30); a check box
// is checked by w:checked, else w:default (17.16.7, 17.16.10).
func legacyField(instr string, ff *ooxml.Element) (compose.Field, bool) {
	var f compose.Field
	switch strings.ToUpper(firstWord(instr)) {
	case "FORMTEXT":
		f.Kind = compose.FieldText
		if v, err := strconv.Atoi(attrOr(ff.Child(w, "textInput").Child(w, "maxLength"), "val")); err == nil && v > 0 && v <= compose.MaxFieldText {
			f.MaxLen = v
		}
	case "FORMDROPDOWN":
		f.Kind = compose.FieldCombo
		dd := ff.Child(w, "ddList")
		for _, e := range dd.Kids() {
			if e.Is(w, "listEntry") {
				f.Options = append(f.Options, attrOr(e, "val"))
			}
		}
		if i, err := strconv.Atoi(attrOr(dd.Child(w, "result"), "val")); err == nil && i >= 0 && i < len(f.Options) {
			f.Value = f.Options[i]
		} else if len(f.Options) > 0 {
			f.Value = f.Options[0]
		}
	case "FORMCHECKBOX":
		f.Kind = compose.FieldCheckBox
		cb := ff.Child(w, "checkBox")
		on := cb.Child(w, "checked")
		if on == nil {
			on = cb.Child(w, "default")
		}
		if on != nil {
			v, ok := on.Attr("val")
			f.Checked = !ok || v == "1" || v == "true" || v == "on"
		}
	default:
		return f, false
	}
	f.Name = attrOr(ff.Child(w, "name"), "val")
	f.Description = attrOr(ff.Child(w, "helpText"), "val")
	if ff != nil && ff.Child(w, "enabled") == nil {
		f.ReadOnly = true
	}
	return f, true
}

// legacyResult starts the result of the legacy form field fl when it
// becomes a field: a text input or a drop-down collects its result as the
// content of a control, which [document.endLegacy] closes; a check box,
// which has no result, adds a box of its own size (17.16.7 size, else the
// text's) that shows a cross when checked. It reports whether the field
// is a form field.
func (d *document) legacyResult(fl *field, st *css.Style, sink *inlineSink) bool {
	if fl.ff == nil || !d.fieldsOn() {
		return false
	}
	f, ok := legacyField(fl.instr.String(), fl.ff)
	if !ok {
		return false
	}
	ctl := &formfield.Control{ID: formfield.NodeID(len(d.form.controls)), Field: f}
	if f.Kind != compose.FieldCheckBox {
		f.Value = ""
		fl.control = ctl
		d.form.open++
		sink.openLink("")
		return true
	}
	size := st.FontSize
	if v, err := strconv.Atoi(attrOr(fl.ff.Child(w, "checkBox").Child(w, "size"), "val")); err == nil && v > 0 && v <= 3276 {
		size = float64(v) / 2
	}
	box := *st
	box.Display = css.InlineBlock
	box.BoxSizing = css.BorderBox
	box.Width, box.Height = css.Pts(size), css.Pts(size)
	bd := css.Border{Width: 0.5, Style: css.Solid, Color: css.Color{A: 1}}
	box.Border = [4]css.Border{bd, bd, bd, bd}
	cross := func(_ context.Context, r compose.Rect) []compose.Fragment { return crossMark(r) }
	var kid *layout.Node
	if f.Checked {
		ks := *st
		ks.Display = css.Block
		ks.Width, ks.Height = css.Pts(size-1), css.Pts(size-1)
		kid = &layout.Node{Kind: layout.Replaced, Style: &ks, Picture: &layout.Picture{Width: size - 1, Height: size - 1, Vector: cross}}
	} else {
		kid = &layout.Node{Kind: layout.Text, Style: st, Text: " "}
		ctl.Other = func(_ []compose.Fragment, r compose.Rect, _ *compose.Measurer) []compose.Fragment {
			return crossMark(r)
		}
	}
	ctl.Box = true
	ctl.Field = f
	sink.add(&layout.Node{Kind: layout.Element, Style: &box, Role: "Form", ID: ctl.ID, Children: []*layout.Node{kid}})
	d.form.controls = append(d.form.controls, ctl)
	return true
}

// endLegacy closes the control the result of the legacy form field fl
// was collected in, its value the text the result shows.
func (d *document) endLegacy(fl *field, sink *inlineSink) {
	ctl := fl.control
	fl.control = nil
	d.form.open--
	if len(sink.links) > 0 && ctl.Field.Kind == compose.FieldText {
		var b strings.Builder
		for _, n := range sink.links[len(sink.links)-1].nodes {
			b.WriteString(office.ShownText([]*layout.Node{n}))
		}
		ctl.Field.Value = strings.TrimRight(b.String(), " ")
	}
	d.closeControl(sink, ctl)
}

// crossMark returns a cross in the rectangle r, a check box's on state.
func crossMark(r compose.Rect) []compose.Fragment {
	inset := min(r.Width, r.Height) * 0.2
	x0, y0 := r.X+inset, r.Y+inset
	x1, y1 := r.X+r.Width-inset, r.Y+r.Height-inset
	return []compose.Fragment{&compose.Path{Ops: []compose.PathOp{
		{Kind: compose.MoveTo, P: [3]compose.Point{{X: x0, Y: y0}}},
		{Kind: compose.LineTo, P: [3]compose.Point{{X: x1, Y: y1}}},
		{Kind: compose.MoveTo, P: [3]compose.Point{{X: x1, Y: y0}}},
		{Kind: compose.LineTo, P: [3]compose.Point{{X: x0, Y: y1}}},
	}, Stroke: &compose.Stroke{Color: compose.Black, Width: max(0.5, min(r.Width, r.Height)*0.08)}}}
}

// attachControls makes the controls of d into the form fields of the
// pages of res, warning of those that stay printed without a field.
func (d *document) attachControls(res *layout.Result) {
	if missed := formfield.Attach(res, d.form.controls); missed > 0 && len(d.form.controls) > 0 {
		d.warn("form-field", "a form control that spans pages or shows nothing is printed without its field")
	}
}
