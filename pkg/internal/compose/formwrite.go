package compose

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// The field flags this package writes (ISO 32000-1, 12.7.3.1, Table 221;
// 12.7.4.2, Table 226; 12.7.4.3, Table 228; 12.7.4.4, Table 230), by bit
// position from 1.
const (
	flagReadOnly      = 1 << 0
	flagRequired      = 1 << 1
	flagMultiline     = 1 << 12
	flagNoToggleToOff = 1 << 14
	flagRadio         = 1 << 15
	flagCombo         = 1 << 17
	flagEdit          = 1 << 18
	flagMultiSelect   = 1 << 21
)

// formFontName is the resource name of the font of the interactive form's
// default resources: Helvetica, one of the standard 14 fonts every
// conforming reader holds (ISO 32000-1, 9.6.2.2), which sets the text a
// reader types into a field.
const formFontName = "Helv"

// formState is what the interactive form needs at the end of the file: the
// root fields in order, the names taken, the radio button groups and the
// widgets whose /StructParent waits on the page count.
type formState struct {
	// roots are the fields /AcroForm /Fields lists, in the order met.
	roots ast.PdfArray
	// names maps a name written to its use.
	names map[string]*nameUse
	// groups are the radio button groups, in the order met.
	groups []*radioGroup
	// widgets are the widgets written, finished at the end.
	widgets []pendingWidget
	// count is the fields met, radio buttons included.
	count int
	// font is the default resources' font, allocated with the first field
	// that sets text; zero before.
	font ast.PdfIndirectRef
	// charged is what the widgets held until the end cost the
	// conversion's budget.
	charged int64
}

// widgetCost is what one widget held until the end of the file costs the
// conversion's budget: its dictionary of a dozen entries, as Go maps and
// values, and its place in the lists of the form. Memory guard, not
// spec-mandated: a widget's dictionary holds a few hundred bytes, and
// a field's name, value and options are bounded by [MaxFieldText] and
// [maxFieldOptions] each.
const widgetCost = 2 << 10

// nameUse is the field a name was given to.
type nameUse struct {
	// group is the radio button group of the name, nil for another field.
	group *radioGroup
	// next is the next suffix tried for another field of the name.
	next int
}

// radioGroup is one radio button group: the field its buttons are the
// widgets of (ISO 32000-1, 12.7.4.2.4).
type radioGroup struct {
	ref         ast.PdfIndirectRef
	name        string
	description string
	// value is the on state of the first button that is on, "" for none.
	value string
	flags int
	kids  ast.PdfArray
}

// pendingWidget is a widget whose dictionary is finished at the end: its
// /StructParent key is the page count plus its index among the
// annotations of the structure tree.
type pendingWidget struct {
	ref   ast.PdfIndirectRef
	dict  ast.PdfDict
	annot int
}

// fieldName returns the partial name the field f is written with: its
// name made unique against the fields written, a radio button joining the
// group of its name. It returns the group of a radio button, nil for
// another kind. The suffixes count from 2, as LibreOffice names a second
// control of one name.
func (fs *formState) fieldName(f *Field) (string, *radioGroup) {
	base := fieldName(f.Name)
	if fs.names == nil {
		fs.names = make(map[string]*nameUse)
	}
	if u, ok := fs.names[base]; ok && f.Kind == FieldRadio && u.group != nil {
		return base, u.group
	}
	name := base
	if u, ok := fs.names[base]; ok {
		// The fields of a document are at most maxFields, so the loop
		// ends within that many tries past the suffixes already taken.
		if u.next < 2 {
			u.next = 2
		}
		for {
			name = base + "_" + strconv.Itoa(u.next)
			u.next++
			if _, taken := fs.names[name]; !taken {
				break
			}
		}
	}
	use := &nameUse{}
	fs.names[name] = use
	if f.Kind == FieldRadio {
		use.group = &radioGroup{name: name, flags: flagRadio | flagNoToggleToOff}
		fs.groups = append(fs.groups, use.group)
	}
	return name, use.group
}

// fields writes the fields of the page at index page, each a widget
// annotation with its appearance streams, and returns the references of
// their widgets in order, for the page's /Annots.
func (w *docWriter) fields(ctx context.Context, page int, height float64, fields []Field) (ast.PdfArray, error) {
	if len(fields) == 0 {
		return nil, nil
	}
	fs := &w.form
	if fs.count+len(fields) > maxFields {
		return nil, fmt.Errorf("%w: more than %d form fields", ErrLimitExceeded, maxFields)
	}
	refs := make(ast.PdfArray, 0, len(fields))
	for i := range fields {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("writing page %d: %w", page+1, err)
		}
		ref, err := w.widget(ctx, page, height, &fields[i])
		if err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}
	return refs, nil
}

// widget writes the field f of the page at index page, height points
// tall: its appearance
// streams now, its widget annotation (ISO 32000-1, 12.5.6.19, Table 188)
// at the end. A field of any kind but a radio button is one dictionary,
// the field merged with its widget (12.7.3.1); a radio button is a widget
// whose /Parent is its group's field.
func (w *docWriter) widget(ctx context.Context, page int, height float64, f *Field) (ast.PdfIndirectRef, error) {
	fs := &w.form
	cost := int64(widgetCost) + int64(len(f.Name)+len(f.Description)+len(f.Value))
	for _, o := range f.Options {
		cost += int64(len(o))
	}
	if err := w.mem.Charge(cost, "the form fields"); err != nil {
		return ast.PdfIndirectRef{}, fmt.Errorf("writing page %d: %w", page+1, err)
	}
	fs.charged += cost
	fs.count++
	name, group := fs.fieldName(f)
	ref := w.alloc.Alloc(ast.PdfNull{})
	r := f.Rect
	// ISO 32000-1, 12.5.2, Table 164: /Rect in default user space,
	// [llx lly urx ury]; the model's y runs down from the page's top.
	dict := ast.PdfDict{
		"Type":    ast.PdfName("Annot"),
		"Subtype": ast.PdfName("Widget"),
		"Rect":    ast.PdfArray{pdfReal(r.X), pdfReal(height - r.Y - r.Height), pdfReal(r.X + r.Width), pdfReal(height - r.Y)},
		// The print flag (Table 165): the field prints as it shows, as
		// PDF/A asks of every annotation (ISO 19005-2, 6.3.2).
		"F": ast.PdfInt(4),
		"P": pageRef(page),
	}
	description := f.Description
	if description == "" && f.Parent != nil {
		description = f.Parent.Alt
	}
	if description == "" {
		// ISO 14289-1, 7.18.1: a widget carries an alternate
		// description; the name stands in for a missing one.
		description = name
	}
	// ISO 14289-2, 8.10.2.3: a widget whose Form element holds no Lbl
	// carries /Contents (ISO 32000-1, Table 164), its description again.
	dict["Contents"] = textString(description)
	w.hashBytes([]byte(name))
	w.hashBytes([]byte(f.Value))
	switch f.Kind {
	case FieldCheckBox, FieldRadio:
		on := ast.PdfName(f.onState())
		state := ast.PdfName("Off")
		if f.Checked {
			state = on
		}
		onRef, err := w.appearance(ctx, page, f.Rect, f.Appearance, false)
		if err != nil {
			return ast.PdfIndirectRef{}, err
		}
		offRef, err := w.appearance(ctx, page, f.Rect, f.OffAppearance, false)
		if err != nil {
			return ast.PdfIndirectRef{}, err
		}
		// 12.7.4.2.3: the normal appearance of a button is a dictionary of
		// its states, Off and the on state; /AS selects the one shown.
		dict["AP"] = ast.PdfDict{"N": ast.PdfDict{on: onRef, "Off": offRef}}
		dict["AS"] = state
		if group != nil {
			if group.ref.ObjectNum == 0 {
				group.ref = w.alloc.Alloc(ast.PdfNull{})
				fs.roots = append(fs.roots, group.ref)
				group.description = description
			}
			dict["Parent"] = group.ref
			if f.Checked && group.value == "" {
				group.value = string(on)
			}
			if f.ReadOnly {
				group.flags |= flagReadOnly
			}
			if f.Required {
				group.flags |= flagRequired
			}
			group.kids = append(group.kids, ref)
			break
		}
		dict["FT"] = ast.PdfName("Btn")
		dict["V"] = state
		dict["DV"] = state
		addFlags(dict, buttonFlags(f))
	case FieldText, FieldList, FieldCombo:
		app, err := w.appearance(ctx, page, f.Rect, f.Appearance, true)
		if err != nil {
			return ast.PdfIndirectRef{}, err
		}
		dict["AP"] = ast.PdfDict{"N": app}
		w.variableText(dict, f)
	}
	if group == nil {
		dict["T"] = textString(name)
		dict["TU"] = textString(description)
		fs.roots = append(fs.roots, ref)
	}
	pw := pendingWidget{ref: ref, dict: dict, annot: -1}
	if w.st != nil && f.Parent != nil {
		pw.annot = w.st.addAnnot(page, ref, f.Parent, nil)
	}
	fs.widgets = append(fs.widgets, pw)
	return ref, nil
}

// buttonFlags returns the field flags of a check box.
func buttonFlags(f *Field) int {
	flags := 0
	if f.ReadOnly {
		flags |= flagReadOnly
	}
	if f.Required {
		flags |= flagRequired
	}
	return flags
}

// addFlags sets /Ff to flags when any is set (Table 220: 0 by default).
func addFlags(dict ast.PdfDict, flags int) {
	if flags != 0 {
		dict["Ff"] = ast.PdfInt(flags)
	}
}

// variableText sets the entries of a text field or a choice field f, those
// whose value a reader sets in type (ISO 32000-1, 12.7.3.3): the type, the
// value and the default value, the options, the flags, the maximum length
// and the default appearance, which names the form's Helvetica at the
// field's size and colour.
func (w *docWriter) variableText(dict ast.PdfDict, f *Field) {
	fs := &w.form
	if fs.font.ObjectNum == 0 {
		fs.font = w.alloc.Alloc(ast.PdfDict{
			"Type":     ast.PdfName("Font"),
			"Subtype":  ast.PdfName("Type1"),
			"BaseFont": ast.PdfName("Helvetica"),
			"Encoding": ast.PdfName("WinAnsiEncoding"),
		})
	}
	flags := 0
	if f.ReadOnly {
		flags |= flagReadOnly
	}
	if f.Required {
		flags |= flagRequired
	}
	switch f.Kind {
	case FieldText:
		dict["FT"] = ast.PdfName("Tx")
		if f.Multiline {
			flags |= flagMultiline
		}
		if f.MaxLen > 0 {
			dict["MaxLen"] = ast.PdfInt(f.MaxLen)
		}
		dict["V"] = textString(f.Value)
		dict["DV"] = textString(f.Value)
	case FieldList, FieldCombo:
		dict["FT"] = ast.PdfName("Ch")
		if f.Kind == FieldCombo {
			flags |= flagCombo
			if f.Editable {
				flags |= flagEdit
			}
		}
		opt := make(ast.PdfArray, len(f.Options))
		for i, o := range f.Options {
			opt[i] = textString(o)
		}
		dict["Opt"] = opt
		if f.Kind == FieldList && f.MultiSelect {
			flags |= flagMultiSelect
			// Table 231: the value of a box that selects several items is
			// the array of their texts, /I their indices in ascending
			// order.
			values := make(ast.PdfArray, len(f.Selected))
			idx := make(ast.PdfArray, len(f.Selected))
			for i, k := range f.Selected {
				values[i] = textString(f.Options[k])
				idx[i] = ast.PdfInt(k)
			}
			if len(values) > 0 {
				dict["V"] = values
				dict["DV"] = values
				dict["I"] = idx
			}
		} else if f.Value != "" {
			dict["V"] = textString(f.Value)
			dict["DV"] = textString(f.Value)
		}
	case FieldCheckBox, FieldRadio:
	}
	addFlags(dict, flags)
	c := f.Color
	if c.A == 0 {
		c = Black
	}
	dict["DA"] = ast.PdfString(fmt.Sprintf("/%s %s Tf %s %s %s rg", formFontName, num(f.FontSize), num(c.R), num(c.G), num(c.B)))
}

// appearance writes frags, a field's appearance in page coordinates, as a
// form XObject whose bounding box is rect (ISO 32000-1, 12.5.5 and
// 8.10): rect's top-left corner is the stream's origin, under the same
// flip as a page, and nothing outside it shows. The stream carries no
// marked content of the structure tree: a widget's content is the
// annotation's, which the Form element names (14.8.4.5). A text or choice
// field's content sits in /Tx BMC ... EMC, the variable text a reader
// replaces when it sets a new value (12.7.3.3).
func (w *docWriter) appearance(ctx context.Context, page int, rect Rect, frags []Fragment, variable bool) (ast.PdfIndirectRef, error) {
	pw := newPageWriter(w, page, &Page{Width: rect.Width, Height: rect.Height})
	pw.bare = true
	if variable {
		pw.buf.WriteString("/Tx BMC\nq\n")
	}
	if len(frags) > 0 {
		content := []Fragment{&Group{Transform: Matrix{A: 1, D: 1, E: -rect.X, F: -rect.Y}, Content: frags}}
		if err := pw.content(ctx, content); err != nil {
			return ast.PdfIndirectRef{}, err
		}
	}
	if variable {
		pw.buf.WriteString("Q\nEMC\n")
	}
	data := pw.buf.Bytes()
	var box [16]byte
	binary.BigEndian.PutUint64(box[:8], math.Float64bits(rect.Width))
	binary.BigEndian.PutUint64(box[8:], math.Float64bits(rect.Height))
	w.hashBytes(box[:])
	w.hashBytes(data)
	stream, err := flateStream(ast.PdfDict{
		"Type":      ast.PdfName("XObject"),
		"Subtype":   ast.PdfName("Form"),
		"BBox":      ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), pdfReal(rect.Width), pdfReal(rect.Height)},
		"Resources": pw.resources(),
	}, data)
	if err != nil {
		return ast.PdfIndirectRef{}, fmt.Errorf("writing page %d: %w", page+1, err)
	}
	return w.alloc.Alloc(stream), nil
}

// finishForm queues the widgets and the radio button groups of a file of
// n pages and returns the interactive form dictionary (ISO 32000-1,
// 12.7.2, Table 218), nil for a file without fields. The form names its
// root fields, and, when a field sets text, the default resources and the
// default appearance its fields' own appearances override. It sets no
// /NeedAppearances: every widget carries its appearance, which PDF/A
// requires (ISO 19005-2, 6.4.1).
func (w *docWriter) finishForm(n int) ast.PdfDict {
	fs := &w.form
	if len(fs.roots) == 0 {
		return nil
	}
	for _, pw := range fs.widgets {
		if pw.annot >= 0 {
			pw.dict["StructParent"] = ast.PdfInt(n + pw.annot)
		}
		w.alloc.AllocAt(pw.ref.ObjectNum, pw.dict)
	}
	fs.widgets = nil
	w.mem.Release(fs.charged)
	fs.charged = 0
	for _, g := range fs.groups {
		value := ast.PdfName("Off")
		if g.value != "" {
			value = ast.PdfName(g.value)
		}
		dict := ast.PdfDict{
			"FT":   ast.PdfName("Btn"),
			"Ff":   ast.PdfInt(g.flags),
			"T":    textString(g.name),
			"TU":   textString(g.description),
			"V":    value,
			"DV":   value,
			"Kids": g.kids,
		}
		w.alloc.AllocAt(g.ref.ObjectNum, dict)
	}
	form := ast.PdfDict{"Fields": fs.roots}
	if fs.font.ObjectNum != 0 {
		form["DR"] = ast.PdfDict{"Font": ast.PdfDict{formFontName: fs.font}}
		form["DA"] = ast.PdfString("/" + formFontName + " 0 Tf 0 g")
	}
	return form
}
