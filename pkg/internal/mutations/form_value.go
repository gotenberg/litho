package mutations

import (
	"context"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// Field flags, bit n of /Ff being 1<<(n-1) (ISO 32000-1, Tables 226 and 230).
const (
	fieldFlagPushbutton  = 1 << 16
	fieldFlagCombo       = 1 << 17
	fieldFlagEdit        = 1 << 18
	fieldFlagMultiSelect = 1 << 21
)

// fieldValue is the value [SetFormField] or [SetFormFieldValues] writes:
// values, and whether they are a list box's selections, which /V holds as an
// array (ISO 32000-1, Table 230).
type fieldValue struct {
	values []string
	list   bool
}

// String returns the value as a caller gave it, for a message.
func (v fieldValue) String() string {
	if v.list {
		return fmt.Sprintf("%q", v.values)
	}
	return fmt.Sprintf("%q", v.values[0])
}

// fieldEntries holds the entries a field node takes from the nearest node,
// itself or an ancestor, that states them: /FT and /Ff (ISO 32000-1, 12.7.3.1,
// Table 220), and /Opt, which a choice or button field states for the widgets
// below it (Tables 227 and 231).
type fieldEntries struct {
	ft  string
	ff  int
	opt ast.PdfObject
	// optFrom is the object number of the node that stated opt, 0 for none
	// or a node written directly, which keys a unit of the search (see
	// [fieldSearchKey]).
	optFrom int
}

// with returns the entries dict, the field node obj, holds: its own where it
// states them, e's otherwise.
func (e fieldEntries) with(ctx context.Context, doc *document.Document, obj ast.PdfObject, dict ast.PdfDict) fieldEntries {
	cur := e
	if ft, err := document.ResolveName(ctx, doc, dict["FT"]); err == nil && ft != "" {
		cur.ft = string(ft)
	}
	if ffObj, ok := dict["Ff"]; ok {
		if ff, err := document.ResolveInt(ctx, doc, ffObj); err == nil {
			cur.ff = int(ff & math.MaxInt32) // fits int: masked to 31 bits, which hold every flag ISO 32000-1 defines
		}
	}
	if opt, ok := dict["Opt"]; ok {
		cur.opt, cur.optFrom = opt, 0
		if ref, isRef := obj.(ast.PdfIndirectRef); isRef {
			cur.optFrom = ref.ObjectNum
		}
	}
	return cur
}

// writeFieldValue checks val against the field dict, the field named name
// whose entries are e, and writes it: /V, the /I a list box's selections take,
// and the appearance state of each widget of a button. The field is written
// at ref, and not at all when ref is zero, a field written directly in its
// parent's /Kids, which no update can address.
//
// A value the field cannot hold returns [ErrFormFieldValue] and writes
// nothing (see [checkFieldValue]).
//
// # Corpus evidence
//
//   - pdfjs checkbox-bad-appearance.pdf: a check box set to a string /V hid
//     the whole form from litho's reader, on entirely well-formed input.
func writeFieldValue(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, ref ast.PdfIndirectRef, dict ast.PdfDict, name string, e fieldEntries, val fieldValue) error {
	if err := checkFieldValue(ctx, doc, alloc, dict, name, e, val); err != nil {
		return err
	}
	newDict := make(ast.PdfDict, len(dict)+2)
	maps.Copy(newDict, dict)
	switch {
	case e.ft == "Btn":
		// A button field's value is a NAME: the on state chosen from its
		// appearance subdictionary, or /Off (ISO 32000-1, 12.7.4.2.3). Writing
		// a string, as this once did for every type, produced a checkbox whose
		// /V was `(LITHOPROBE)` instead of `/LITHOPROBE`, and litho's own form
		// reader then listed no field at all.
		newDict["V"] = ast.PdfName(val.values[0])
	case val.list:
		arr := make(ast.PdfArray, len(val.values))
		for i, v := range val.values {
			arr[i] = document.EncodePdfString(v)
		}
		newDict["V"] = arr
	default:
		newDict["V"] = document.EncodePdfString(val.values[0])
	}
	if e.ft == "Ch" {
		setChoiceIndices(ctx, doc, newDict, e, val)
	}
	if ref.ObjectNum > 0 {
		alloc.AllocAt(ref.ObjectNum, newDict)
	}
	if e.ft == "Btn" {
		setButtonAppearanceStates(ctx, doc, alloc, newDict, val.values[0])
	}
	return nil
}

// checkFieldValue returns [ErrFormFieldValue], wrapped, when the field dict,
// named name with entries e, cannot hold val:
//
//   - a push button or a signature field, which hold no value a caller
//     writes (ISO 32000-1, 12.7.4.2.2 and 12.7.4.5);
//   - a state no widget of a check box or radio button group shows, other
//     than Off (12.7.4.2.3), which would leave every widget drawing nothing;
//   - a value outside the options of a choice field the user cannot type
//     into, one that is not an editable combo box (12.7.4.4);
//   - selections of a field that does not allow several (Table 230).
//
// A check box whose widgets state no appearance, or a choice field with no
// /Opt, gives nothing to check the value against, and takes any.
func checkFieldValue(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, dict ast.PdfDict, name string, e fieldEntries, val fieldValue) error {
	refuse := func(why string, args ...any) error {
		return fmt.Errorf("%w: %s for field %q: %s", ErrFormFieldValue, val, name, fmt.Sprintf(why, args...))
	}
	if val.list && (e.ft != "Ch" || e.ff&fieldFlagMultiSelect == 0) && len(val.values) != 1 {
		return refuse("only a list box that allows several selections takes several")
	}
	switch e.ft {
	case "Sig":
		return refuse("a signature field takes a signature, not a value")
	case "Btn":
		if e.ff&fieldFlagPushbutton != 0 {
			return refuse("a push button holds no value")
		}
		value := val.values[0]
		if value == "Off" {
			return nil
		}
		states := buttonStates(ctx, doc, alloc, dict)
		if len(states) == 0 || slices.Contains(states, value) {
			return nil
		}
		return refuse("no widget shows that state; it takes Off or one of %s", strings.Join(states, ", "))
	case "Ch":
		if e.ff&fieldFlagCombo != 0 && e.ff&fieldFlagEdit != 0 {
			return nil
		}
		exports := choiceExports(ctx, doc, e.opt)
		if len(exports) == 0 {
			return nil
		}
		for _, v := range val.values {
			if !slices.Contains(exports, v) {
				return refuse("%q is none of its options: %s", v, strings.Join(exports, ", "))
			}
		}
	}
	return nil
}

// buttonStates returns the on states the widgets of a button field show, the
// keys of their normal appearance dictionaries other than Off (ISO 32000-1,
// 12.7.4.2.3), in widget order: the field's own when it is its widget
// (12.5.6.19), its kids' otherwise.
func buttonStates(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, dict ast.PdfDict) []string {
	var states []string
	for _, w := range buttonWidgets(ctx, doc, alloc, dict) {
		for _, s := range appearanceStates(ctx, doc, w.dict) {
			if !slices.Contains(states, s) {
				states = append(states, s)
			}
		}
	}
	return states
}

// buttonWidget is a widget of a button field: its object and dictionary.
type buttonWidget struct {
	obj  ast.PdfObject
	dict ast.PdfDict
}

// buttonWidgets returns the widgets of the button field dict that carry an
// appearance: the field itself when it is its widget, its kids otherwise. They
// are read from alloc first.
func buttonWidgets(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, dict ast.PdfDict) []buttonWidget {
	kids, err := resolveArrayAllocFirst(ctx, alloc, doc, dict["Kids"])
	if err != nil || kids == nil {
		if _, hasAP := dict["AP"]; hasAP {
			return []buttonWidget{{dict: dict}}
		}
		return nil
	}
	var out []buttonWidget
	for _, kidObj := range kids {
		kidDict, err := resolveDictAllocFirst(ctx, alloc, doc, kidObj)
		if err != nil || kidDict == nil {
			continue
		}
		if _, hasAP := kidDict["AP"]; hasAP {
			out = append(out, buttonWidget{obj: kidObj, dict: kidDict})
		}
	}
	return out
}

// appearanceStates returns the names of widget's normal appearance states
// other than Off, sorted. A widget whose /N is a stream rather than a
// dictionary of states has none.
func appearanceStates(ctx context.Context, doc *document.Document, widget ast.PdfDict) []string {
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
			out = append(out, string(key))
		}
	}
	slices.Sort(out)
	return out
}

// setButtonAppearanceStates points each widget of the button field dict at the
// appearance of state: /AS selects which appearance in /AP is drawn (ISO
// 32000-1, 12.5.5), and leaving it on the previous state drew the OLD state.
// A widget whose appearance has no such state is set to Off, which is how a
// radio button group shows that one of its buttons is on (12.7.4.2.4): setting
// every kid to the state drew none of the others, since none has it.
func setButtonAppearanceStates(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, dict ast.PdfDict, state string) {
	for _, w := range buttonWidgets(ctx, doc, alloc, dict) {
		as := ast.PdfName("Off")
		if state != "Off" && slices.Contains(appearanceStates(ctx, doc, w.dict), state) {
			as = ast.PdfName(state)
		}
		if w.obj == nil {
			// The field is its own widget, and dict is the copy its caller
			// writes.
			dict["AS"] = as
			continue
		}
		ref, ok := w.obj.(ast.PdfIndirectRef)
		if !ok {
			continue
		}
		kid := make(ast.PdfDict, len(w.dict)+1)
		maps.Copy(kid, w.dict)
		kid["AS"] = as
		alloc.AllocAt(ref.ObjectNum, kid)
	}
}

// choiceExports returns the export values of a choice field's /Opt array opt
// (ISO 32000-1, Table 231): each text string, or the first of each pair.
func choiceExports(ctx context.Context, doc *document.Document, opt ast.PdfObject) []string {
	arr, err := document.ResolveArray(ctx, doc, opt)
	if err != nil {
		return nil
	}
	var out []string
	for _, entry := range arr {
		res, err := document.ResolveObject(ctx, doc, entry)
		if err != nil {
			continue
		}
		switch v := res.(type) {
		case ast.PdfString:
			out = append(out, document.DecodePdfString(v))
		case ast.PdfArray:
			if len(v) == 2 {
				if s, err := document.ResolveString(ctx, doc, v[0]); err == nil {
					out = append(out, document.DecodePdfString(s))
				}
			}
		}
	}
	return out
}

// setChoiceIndices writes the /I of a choice field: the zero-based positions
// in /Opt of the selected items, sorted ascending (ISO 32000-1, Table 231),
// which a list box that allows several selections uses to tell items with the
// same export value apart. A field that allows one keeps an /I only when it
// stated one, now naming the new value, so it does not contradict /V; and a
// value no item holds drops it.
func setChoiceIndices(ctx context.Context, doc *document.Document, dict ast.PdfDict, e fieldEntries, val fieldValue) {
	_, stated := dict["I"]
	if e.ff&fieldFlagMultiSelect == 0 && !stated {
		return
	}
	exports := choiceExports(ctx, doc, e.opt)
	var indices ast.PdfArray
	for i, export := range exports {
		if slices.Contains(val.values, export) {
			indices = append(indices, ast.PdfInt(i))
		}
	}
	if len(indices) == 0 {
		delete(dict, "I")
		return
	}
	dict["I"] = indices
}
