package annotation

import (
	"context"
	"math"
	"slices"
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// Field flags, bit n of /Ff being 1<<(n-1) (ISO 32000-1, Tables 221, 226 and
// 230).
const (
	fieldFlagReadOnly    = 1 << 0
	fieldFlagRequired    = 1 << 1
	fieldFlagRadio       = 1 << 15
	fieldFlagPushbutton  = 1 << 16
	fieldFlagCombo       = 1 << 17
	fieldFlagEdit        = 1 << 18
	fieldFlagMultiSelect = 1 << 21
)

// widgetPlace is where a widget annotation shows its field: the 1-based page
// whose /Annots lists it, and its normalized rectangle.
type widgetPlace struct {
	page int
	rect [4]float64
}

// fieldCollector gathers what one reading of the interactive form records, the
// walk of the pages' /Annots and of the field tree alike.
type fieldCollector struct {
	// formData maps a field's fully qualified name to its value as text, and
	// calcScripts to the script of its calculate action.
	formData, calcScripts map[string]string
	// order lists the names of the terminal fields in document order, each
	// once, which seen records.
	order []string
	seen  map[string]bool
	// details describes each terminal field, by name.
	details map[string]*fieldDetail
	// widgets maps the object number of a widget a page's /Annots lists to
	// where it shows, and pages the object number of a page to its 1-based
	// number, for a widget only its /P places.
	widgets map[int]widgetPlace
	pages   map[int]int
}

// fieldDetail is what a [fieldCollector] learns of one field: its description,
// and the export values the /Opt array of a button field holds, which name its
// items only once its widgets have given its states (see [buttonOptions]).
type fieldDetail struct {
	models.FormField
	buttonOpt []string
}

// newFieldCollector returns an empty [fieldCollector].
func newFieldCollector() *fieldCollector {
	return &fieldCollector{
		formData:    make(map[string]string),
		calcScripts: make(map[string]string),
		seen:        make(map[string]bool),
		details:     make(map[string]*fieldDetail),
		widgets:     make(map[int]widgetPlace),
		pages:       make(map[int]int),
	}
}

// fieldInheritance holds the entries a field node takes from the nearest
// ancestor that states them, its own first (ISO 32000-1, 12.7.3.1, Table 220:
// /FT, /Ff and /V; Table 229: /MaxLen). /Opt (Tables 227 and 231) is read
// the same way, since the widgets of a choice or button field are its kids and
// the field states it.
type fieldInheritance struct {
	ft     string
	ff     int
	maxLen int
	opt    ast.PdfObject
	v      ast.PdfObject
	// optFrom and vFrom are the object numbers of the nodes that stated opt
	// and v, 0 for none or a node written directly, which key a unit of the
	// walk (see [fieldVisit]).
	optFrom, vFrom int
}

// inheritedKey is the part of a [fieldInheritance] that keys a unit of the
// walk: the objects it inherits are named by the node that stated them.
type inheritedKey struct {
	ft                         string
	ff, maxLen, optFrom, vFrom int
}

// key returns the [inheritedKey] of inh.
func (inh fieldInheritance) key() inheritedKey {
	return inheritedKey{ft: inh.ft, ff: inh.ff, maxLen: inh.maxLen, optFrom: inh.optFrom, vFrom: inh.vFrom}
}

// with returns the entries obj, the field node dict, holds: its own where it
// states them, inh's otherwise.
func (inh fieldInheritance) with(ctx context.Context, doc *document.Document, obj ast.PdfObject, dict ast.PdfDict) fieldInheritance {
	cur := inh
	from := 0
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		from = ref.ObjectNum
	}
	if ft, err := document.ResolveName(ctx, doc, dict["FT"]); err == nil && ft != "" {
		cur.ft = string(ft)
	}
	if ffObj, ok := dict["Ff"]; ok {
		if ff, err := document.ResolveInt(ctx, doc, ffObj); err == nil {
			cur.ff = int(ff & math.MaxInt32) // fits int: masked to 31 bits, which hold every flag ISO 32000-1 defines
		}
	}
	if mlObj, ok := dict["MaxLen"]; ok {
		if ml, err := document.ResolveInt(ctx, doc, mlObj); err == nil && ml >= 0 && int64(ml) <= int64(maxFieldMaxLen) {
			cur.maxLen = int(ml) // fits int: at most maxFieldMaxLen, checked above
		}
	}
	if opt, ok := dict["Opt"]; ok {
		cur.opt, cur.optFrom = opt, from
	}
	if v, ok := dict["V"]; ok {
		cur.v, cur.vFrom = v, from
	}
	return cur
}

// maxFieldMaxLen bounds the /MaxLen a field reports. It is an anti-DoS guard,
// not spec-mandated: Table 229 sets no bound, and a caller sizing an input from
// the value must not be handed one past what 32-bit platforms hold. No real
// form limits a text field to anywhere near a billion characters.
const maxFieldMaxLen = 1 << 30

// describe records what dict, the terminal node obj of the field name whose
// own and inherited entries are inh, tells of the field: its type, flags and
// options when it is the field's first terminal node, and in every case the
// widget it is and the states it shows.
func (col *fieldCollector) describe(ctx context.Context, doc *document.Document, obj ast.PdfObject, dict ast.PdfDict, name string, inh fieldInheritance) {
	d := col.details[name]
	if d == nil {
		d = &fieldDetail{Name: name}
		describeField(ctx, doc, d, inh)
		col.details[name] = d
	}
	if d.Type == models.FormFieldCheckbox || d.Type == models.FormFieldRadio {
		for _, state := range onStates(ctx, doc, dict) {
			if !slices.Contains(d.States, state) {
				d.States = append(d.States, state)
			}
		}
	}
	if place, ok := col.widgetPlace(ctx, doc, obj, dict); ok {
		d.Widgets = append(d.Widgets, models.FormWidget{PageNumber: place.page, Rect: place.rect})
	}
}

// widgetPlace returns where obj, a terminal field node with dictionary dict,
// shows as a widget: on the page whose /Annots lists it, else on the page its
// /P names. A node that is no widget, or that no page shows, has none.
func (col *fieldCollector) widgetPlace(ctx context.Context, doc *document.Document, obj ast.PdfObject, dict ast.PdfDict) (widgetPlace, bool) {
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		if place, listed := col.widgets[ref.ObjectNum]; listed {
			return place, true
		}
	}
	// A widget carries /Subtype /Widget (ISO 32000-1, 12.5.6.19). A node with
	// neither that nor a /Rect is a field that no annotation presents.
	subtype, err := document.ResolveName(ctx, doc, dict["Subtype"])
	if err != nil {
		// A /Subtype that is not a name names no widget.
		return widgetPlace{}, false
	}
	if _, hasRect := dict["Rect"]; subtype != "Widget" && (subtype != "" || !hasRect) {
		return widgetPlace{}, false
	}
	// /P is optional (Table 164), and a viewer shows only the annotations a
	// page's /Annots lists. A widget no page lists, whose /P names a page, is
	// placed there all the same: that is the page it was made for.
	p, ok := dict["P"].(ast.PdfIndirectRef)
	if !ok {
		return widgetPlace{}, false
	}
	page, ok := col.pages[p.ObjectNum]
	if !ok {
		return widgetPlace{}, false
	}
	return widgetPlace{page: page, rect: normalizedRect(ctx, doc, dict["Rect"])}, true
}

// field returns the description of the field name, whose value is value when
// stated is true.
func (col *fieldCollector) field(name, value string, stated bool) models.FormField {
	d := col.details[name]
	if d == nil {
		return models.FormField{Name: name, Value: value}
	}
	f := d.FormField
	f.Value = value
	// A button that states no value shows its off appearance (ISO 32000-1,
	// 12.7.4.2.3), which is what its value reads as.
	if !stated && (f.Type == models.FormFieldCheckbox || f.Type == models.FormFieldRadio) {
		f.Value = "Off"
	}
	f.Options = append(f.Options, buttonOptions(d.buttonOpt, f.States)...)
	return f
}

// describeField sets the type, flags, options and selections of d from inh,
// the entries its first terminal node holds and inherits.
func describeField(ctx context.Context, doc *document.Document, d *fieldDetail, inh fieldInheritance) {
	d.Type = fieldType(inh.ft, inh.ff)
	d.ReadOnly = inh.ff&fieldFlagReadOnly != 0
	d.Required = inh.ff&fieldFlagRequired != 0
	switch d.Type {
	case models.FormFieldText:
		d.MaxLen = inh.maxLen
	case models.FormFieldCombo, models.FormFieldList:
		d.Editable = d.Type == models.FormFieldCombo && inh.ff&fieldFlagEdit != 0
		d.MultiSelect = inh.ff&fieldFlagMultiSelect != 0
		d.Options = choiceOptions(ctx, doc, inh.opt)
		if d.MultiSelect {
			d.Values = choiceSelections(ctx, doc, inh.v)
		}
	case models.FormFieldCheckbox, models.FormFieldRadio:
		// Each entry is a text string (ISO 32000-1, 12.7.4.2.4). One of
		// another shape keeps its position, so the entries after it still
		// line up with their widgets' states.
		if opt, err := document.ResolveArray(ctx, doc, inh.opt); err == nil {
			for _, entry := range opt {
				export := ""
				if s, err := document.ResolveString(ctx, doc, entry); err == nil {
					export = document.DecodePdfString(s)
				}
				d.buttonOpt = append(d.buttonOpt, export)
			}
		}
	case models.FormFieldButton, models.FormFieldSignature, models.FormFieldXFA:
		// No option, selection or length applies to these.
	}
}

// fieldType returns the kind of a field whose /FT is ft and whose field flags
// are ff (ISO 32000-1, 12.7.4), "" for a field that states no type litho
// knows.
func fieldType(ft string, ff int) models.FormFieldType {
	switch ft {
	case "Tx":
		return models.FormFieldText
	case "Btn":
		switch {
		case ff&fieldFlagPushbutton != 0:
			return models.FormFieldButton
		case ff&fieldFlagRadio != 0:
			return models.FormFieldRadio
		default:
			return models.FormFieldCheckbox
		}
	case "Ch":
		if ff&fieldFlagCombo != 0 {
			return models.FormFieldCombo
		}
		return models.FormFieldList
	case "Sig":
		return models.FormFieldSignature
	}
	return ""
}

// choiceOptions returns the items of a choice field's /Opt array opt: a text
// string is an export value the field also shows, and an array of two holds
// the export value and the text shown (ISO 32000-1, Table 231). An entry of
// another shape is skipped.
func choiceOptions(ctx context.Context, doc *document.Document, opt ast.PdfObject) []models.FormFieldOption {
	arr, err := document.ResolveArray(ctx, doc, opt)
	if err != nil {
		return nil
	}
	var out []models.FormFieldOption
	for _, entry := range arr {
		res, err := document.ResolveObject(ctx, doc, entry)
		if err != nil {
			continue
		}
		switch v := res.(type) {
		case ast.PdfString:
			out = append(out, models.FormFieldOption{Value: document.DecodePdfString(v)})
		case ast.PdfArray:
			if len(v) != 2 {
				continue
			}
			export, errExport := document.ResolveString(ctx, doc, v[0])
			shown, errShown := document.ResolveString(ctx, doc, v[1])
			if errExport != nil || errShown != nil {
				continue
			}
			o := models.FormFieldOption{Value: document.DecodePdfString(export), Label: document.DecodePdfString(shown)}
			if o.Label == o.Value {
				o.Label = ""
			}
			out = append(out, o)
		}
	}
	return out
}

// choiceSelections returns the selections of a choice field whose value is v:
// each text string of an array, in its order, or the one string v is (ISO
// 32000-1, Table 230).
func choiceSelections(ctx context.Context, doc *document.Document, v ast.PdfObject) []string {
	res, err := document.ResolveObject(ctx, doc, v)
	if err != nil {
		return nil
	}
	switch val := res.(type) {
	case ast.PdfString:
		return []string{document.DecodePdfString(val)}
	case ast.PdfArray:
		var out []string
		for _, item := range val {
			if s, err := document.ResolveString(ctx, doc, item); err == nil {
				out = append(out, document.DecodePdfString(s))
			}
		}
		return out
	}
	return nil
}

// buttonOptions returns the items of a check box or radio button group whose
// /Opt is opt and whose on states are states. Each widget's state names the
// export value opt holds at its position, by that position or by the value
// itself (ISO 32000-1, 12.7.4.2.4): an item's Value is the state a widget
// shows, the one the field's value holds, and its Label the export value when
// the two differ.
func buttonOptions(opt, states []string) []models.FormFieldOption {
	var out []models.FormFieldOption
	for i, export := range opt {
		state := strconv.Itoa(i)
		switch {
		case slices.Contains(states, state):
		case export != "" && slices.Contains(states, export):
			state = export
		default:
			continue
		}
		if slices.ContainsFunc(out, func(o models.FormFieldOption) bool { return o.Value == state }) {
			continue
		}
		o := models.FormFieldOption{Value: state, Label: export}
		if o.Label == o.Value {
			o.Label = ""
		}
		out = append(out, o)
	}
	return out
}

// onStates returns the names of the appearance states of widget other than
// Off: the keys of its normal appearance dictionary (ISO 32000-1, 12.7.4.2.3),
// sorted.
func onStates(ctx context.Context, doc *document.Document, widget ast.PdfDict) []string {
	ap, err := document.ResolveDict(ctx, doc, widget["AP"])
	if err != nil || ap == nil {
		return nil
	}
	normal, err := document.ResolveDict(ctx, doc, ap["N"])
	if err != nil || normal == nil {
		return nil
	}
	var out []string
	for key := range normal {
		if key != "Off" {
			out = append(out, nameValue(key))
		}
	}
	slices.Sort(out)
	return out
}

// normalizedRect returns the rectangle obj holds, its lower-left corner first
// (ISO 32000-1, 7.9.5), or the zero rectangle when obj is not one.
func normalizedRect(ctx context.Context, doc *document.Document, obj ast.PdfObject) [4]float64 {
	arr, err := document.ResolveArray(ctx, doc, obj)
	if err != nil || len(arr) != 4 {
		return [4]float64{}
	}
	arr = document.ResolveRectangle(ctx, doc, arr)
	var r [4]float64
	for i := range r {
		r[i] = document.Float(arr[i])
	}
	return [4]float64{min(r[0], r[2]), min(r[1], r[3]), max(r[0], r[2]), max(r[1], r[3])}
}
