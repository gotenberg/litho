package signature

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// maxActionCompare bounds how many pairs of objects [sameAcross] compares, and
// how many [widgetMatch.copied] compares to tell which template widget a new
// widget of the form copies. It is an anti-DoS guard, not spec-mandated: an
// action and those its /Next entry chains to (ISO 32000-1, 12.6.2, Table 193)
// are a handful of dictionaries, and the traits of a field a few values and at
// most a choice list of a few hundred options (12.7.4.4, Table 231), so the
// bound stops a crafted chain or list from costing more than a few thousand
// comparisons. A comparison that reaches it reports the objects different, so
// the field it judges stays a change.
const maxActionCompare = 1 << 12

// fieldTraits are the entries of a field that define what it accepts and how
// it shows its value, beyond the type, name and actions a copy is also judged
// by: its flags and default value (ISO 32000-1, 12.7.3.1, Table 220), the
// options of a check box or radio button (12.7.4.2.3, Table 227) or of a
// choice field (12.7.4.4, Table 231), the maximum length of a text field
// (12.7.4.3, Table 229), and the quadding and default appearance of its
// variable text (12.7.3.3, Table 222).
var fieldTraits = []ast.PdfName{"Ff", "MaxLen", "Opt", "DV", "Q", "DA"}

// widgetTraits are the entries of a widget annotation that define how it is
// shown: its annotation flags (ISO 32000-1, 12.5.2, Table 164; 12.5.3) and its
// appearance characteristics (12.5.6.19, Table 188). They belong to the
// annotation rather than to its field, which is why [widgetMatch.copied]
// compares them at P=2 only.
var widgetTraits = []ast.PdfName{"F", "MK"}

// templateWidget is a widget of a page the signed revision names as a
// template, with what [widgetMatch.copied] compares.
type templateWidget struct {
	rect    []float64
	ft      string
	name    string
	traits  []ast.PdfObject // as [traitsOf] returns them
	actions widgetActions
}

// widgetActions are the actions that run when a widget is used: its own /A
// and /AA (ISO 32000-1, 12.5.6.19, Table 188) and, when the widget is the kid
// of its field rather than merged with it, the field's /AA (12.7.3.1, Table
// 220). terminal is the object number of the field, the widget's own when it
// is merged with it.
type widgetActions struct {
	a, aa, fieldAA ast.PdfObject
	terminal       int
}

// templatePage is a page the signed revision names as a template, with the
// widgets it lists.
type templatePage struct {
	num     int // the page's object number, 0 when the name tree holds it directly
	dict    ast.PdfDict
	widgets []templateWidget
}

// signedTemplates reads the pages the name dictionary of the signed revision
// rev names as templates, each once, and returns nil when it names none.
//
// ISO 32000-1, 12.7.6 lets the name dictionary name pages in its /Templates
// tree, which a reader does not show, and in its /Pages tree, which it does
// (7.7.4, Table 31), and lets a script or an import-data action add either to
// the document as a regular page. The page brings the fields it holds, which
// an instantiation may rename so that they do not clash with the fields
// already there (12.7.7.3.3, Table 249; Annex J).
//
// # Corpus evidence
//
//   - All corpora: no name dictionary holds a /Templates tree, and the one
//     /Pages tree, in pdfjs issue19389.pdf, is in a document with no
//     signature, so the rule rests on the clauses alone.
func signedTemplates(ctx context.Context, rev *document.Document) ([]*templatePage, error) {
	catalog, err := document.ResolveCatalog(ctx, rev)
	if err != nil {
		return nil, fmt.Errorf("reading the signed revision's catalog: %w", err)
	}
	names, err := document.ResolveDict(ctx, rev, catalog.Dict["Names"])
	if err != nil || names == nil {
		// A name dictionary that cannot be read names no template.
		rev.Logger.Debug("resolving the signed /Names for templates", slog.Any("err", err))
		return nil, nil
	}
	var pages []ast.PdfObject
	for _, tree := range []ast.PdfName{"Templates", "Pages"} {
		if _, ok := names[tree]; !ok {
			continue
		}
		err := document.WalkNameTree(ctx, rev, names[tree], func(_ string, value ast.PdfObject) {
			pages = append(pages, value)
		})
		if err != nil {
			return nil, fmt.Errorf("reading the signed /Names /%s: %w", string(tree), err)
		}
	}

	var templates []*templatePage
	seen := make(map[int]bool)
	for i, page := range pages {
		if i%100 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		num := refNum(page)
		if num > 0 && seen[num] {
			continue
		}
		seen[num] = true
		pageDict, err := document.ResolveDict(ctx, rev, page)
		if err != nil || pageDict == nil {
			continue
		}
		t := &templatePage{num: num, dict: pageDict}
		for _, w := range pageWidgets(ctx, rev, pageDict) {
			t.widgets = append(t.widgets, templateWidget{
				rect:    rectOf(ctx, rev, w.dict),
				ft:      fieldTypeIn(ctx, rev, w.dict),
				name:    partialName(ctx, rev, w.dict),
				traits:  traitsOf(ctx, rev, w.dict),
				actions: actionsOf(ctx, rev, w.num, w.dict),
			})
		}
		templates = append(templates, t)
	}
	return templates, nil
}

// templateInstances is what [fieldDiff] reads to tell the fields that a
// template instantiation adds: the widgets of the signed revision's template
// pages, and the widgets the pages the update added list.
type templateInstances struct {
	templates []*templatePage
	added     map[int]bool
}

// newTemplateInstances reads the template pages of the signed revision rev
// ([signedTemplates]) and the pages doc holds that rev does not, as held tells
// ([signedRevision.held]). It returns nil when no template page of rev lists a
// widget.
func newTemplateInstances(ctx context.Context, doc, rev *document.Document, held func(num int) bool) (*templateInstances, error) {
	templates, err := signedTemplates(ctx, rev)
	if err != nil {
		return nil, err
	}
	t := &templateInstances{added: make(map[int]bool)}
	for _, tp := range templates {
		if len(tp.widgets) > 0 {
			t.templates = append(t.templates, tp)
		}
	}
	if len(t.templates) == 0 {
		return nil, nil
	}

	current, err := pagesOf(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("reading the current pages: %w", err)
	}
	for i, page := range current {
		if i%100 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if page.Ref.ObjectNum <= 0 || held(page.Ref.ObjectNum) {
			continue
		}
		for _, w := range pageWidgets(ctx, doc, page.Dict) {
			t.added[w.num] = true
		}
	}
	return t, nil
}

// numberedDict is a dictionary with the number of the object that holds it.
type numberedDict struct {
	num  int
	dict ast.PdfDict
}

// pageWidgets returns the widget annotations the /Annots of page lists by
// reference.
func pageWidgets(ctx context.Context, doc *document.Document, page ast.PdfDict) []numberedDict {
	annots, err := document.ResolveArray(ctx, doc, page["Annots"])
	if err != nil {
		doc.Logger.Debug("resolving /Annots for widgets", slog.Any("err", err))
		return nil
	}
	var widgets []numberedDict
	for _, entry := range annots {
		ref, ok := entry.(ast.PdfIndirectRef)
		if !ok {
			continue
		}
		dict, err := document.ResolveDict(ctx, doc, ref)
		if err == nil && dict != nil && dictName(dict, "Subtype") == "Widget" {
			widgets = append(widgets, numberedDict{ref.ObjectNum, dict})
		}
	}
	return widgets
}

// match returns the widget of a template page that widget copies
// ([widgetMatch.copied]).
func (t *templateInstances) match(ctx context.Context, w widgetMatch, widget ast.PdfDict) (templateWidget, bool) {
	for _, tp := range t.templates {
		if tw, ok := w.copied(ctx, widget, tp.widgets); ok {
			return tw, true
		}
	}
	return templateWidget{}, false
}

// widgetMatch finds the template widget a widget of the current document doc
// copies. rev is the signed revision that holds the templates, policy the
// DocMDP permissions, and budget bounds what comparing traits costs.
type widgetMatch struct {
	doc, rev *document.Document
	policy   int
	budget   *pairBudget
}

// copied returns the widget of widgets that widget copies: the same
// rectangle, the same field type, the same partial name for its field and the
// same [fieldTraits], compared object by object across the two revisions
// ([pairBudget.same]). At P=2 it also has the same [widgetTraits].
//
// At P=3 the [widgetTraits] may differ. ISO 32000-1, 12.8.2.2, Table 254 adds
// "annotation creation, deletion, and modification" to what P=2 permits and
// leaves no annotation type out, a widget is an annotation (12.5.6.19), and
// /F and /MK are entries of the annotation (12.5.2, Table 164; Table 188), not
// of its field (12.7.3.1, Table 220). A template instantiation creates the
// copy's widget, so at P=3 it creates an annotation, whose flags and
// appearance characteristics are then its own, as [annotationEntries] lets
// them change on a signed widget. P=3 would permit the same result in two
// updates, a faithful copy followed by a change to its widget. The
// [fieldTraits] stay compared at every P, since no annotation change reaches a
// field.
//
// A copy whose flags, options, default value or appearance differ from the
// template's adds, under a template field's name, a field that accepts or
// shows something the certifier did not approve.
func (w widgetMatch) copied(ctx context.Context, widget ast.PdfDict, widgets []templateWidget) (templateWidget, bool) {
	rect := rectOf(ctx, w.doc, widget)
	ft := fieldTypeIn(ctx, w.doc, widget)
	name := partialName(ctx, w.doc, widget)
	var traits []ast.PdfObject
	for _, tw := range widgets {
		if tw.ft != ft || tw.name != name || !sameNumbers(tw.rect, rect) {
			continue
		}
		if traits == nil {
			traits = traitsOf(ctx, w.doc, widget)
		}
		if w.sameTraits(ctx, traits, tw.traits) {
			return tw, true
		}
	}
	return templateWidget{}, false
}

// sameTraits reports whether a widget of doc with traits has those of a
// template widget of rev, both as [traitsOf] returns them.
func (w widgetMatch) sameTraits(ctx context.Context, traits, template []ast.PdfObject) bool {
	compared := len(traits)
	if w.policy == 3 {
		// ISO 32000-1, 12.8.2.2, Table 254: P=3 permits annotation creation,
		// and the instantiation creates the copy's widget ([widgetMatch.copied]).
		compared -= len(widgetTraits)
	}
	for i := range compared {
		if !w.budget.same(ctx, w.doc, traits[i], w.rev, template[i]) {
			return false
		}
	}
	return true
}

// traitsOf returns the [fieldTraits] of the field of a widget of doc as a
// reader resolves them, then those its field states itself, then the
// widget's own [widgetTraits]. An absent entry is nil.
//
// A reader resolves an inheritable entry from the widget or the nearest
// ancestor that states it (ISO 32000-1, 12.7.3.1), and Tables 220, 222, 227
// and 229 make every field trait inheritable but a choice field's /Opt (Table
// 231), which a reader may read from the field alone. Comparing both readings
// holds a copy to the template under either. The field is the nearest of the
// widget and its ancestors that carries a partial name, as for [partialName],
// and the walk is bounded by [maxFieldTreeDepth].
func traitsOf(ctx context.Context, doc *document.Document, widget ast.PdfDict) []ast.PdfObject {
	n := len(fieldTraits)
	traits := make([]ast.PdfObject, 2*n+len(widgetTraits))
	stated := make([]bool, n)
	field := false
	seen := make(map[int]bool)
	for depth, dict := 0, widget; dict != nil && depth <= maxFieldTreeDepth; depth++ {
		for i, key := range fieldTraits {
			if value, ok := dict[key]; ok && !stated[i] {
				stated[i] = true
				traits[i] = value
			}
		}
		if _, ok := dict["T"]; ok && !field {
			field = true
			for i, key := range fieldTraits {
				traits[n+i] = dict[key]
			}
		}
		var ok bool
		if _, dict, ok = document.ResolveFieldParent(ctx, doc, dict, seen); !ok {
			break
		}
	}
	for i, key := range widgetTraits {
		traits[2*n+i] = widget[key]
	}
	return traits
}

// instantiated reports whether the field or widget num, which the update
// added as dict, arrives with a template instantiation, or false when the
// signed revision names no template.
//
// ISO 32000-1, 12.8.2.2, Table 254 permits instantiating page templates at
// P=2, and 12.7.6 has an instantiation add a template page, with its fields,
// to the document as a new page. An instantiation that renames the copied
// fields, prefixing each name with the page and the template (12.7.7.3.3,
// Table 249; Annex J), gives each a new field of its own, under new nodes that
// group them. Every widget at or under
// num must be listed on a page the signed revision did not hold and copy a
// widget of a template page ([templateInstances.match]), and the actions that
// run when it is used must be the template's ([sameAcross]). A node that is
// neither such a widget nor its field carries no action at all.
//
// The copy is judged by where it sits, what it is called, what it accepts and
// shows ([fieldTraits], [widgetTraits]) and what it runs, which is what a new
// field changes: it adds a field the certifier did not approve, and its
// actions run when a reader uses it.
func (f *fieldDiff) instantiated(ctx context.Context, num int, dict ast.PdfDict) bool {
	t := f.templateInstances(ctx)
	if t == nil {
		return false
	}
	widgets := f.widgetsUnder(ctx, num, dict)
	if len(widgets) == 0 {
		return false
	}
	isWidget := dictName(dict, "Subtype") == "Widget"
	isField := false
	for _, w := range widgets {
		if !t.added[w.num] || f.held(w.num) {
			return false
		}
		match := widgetMatch{doc: f.doc, rev: f.signed, policy: f.policy, budget: &pairBudget{left: maxActionCompare}}
		tw, ok := t.match(ctx, match, w.dict)
		if !ok {
			return false
		}
		actions := actionsOf(ctx, f.doc, w.num, w.dict)
		if w.num == num && !f.sameActions(ctx, actions, tw.actions) {
			return false
		}
		if actions.terminal == num {
			isField = true
			if !sameAcross(ctx, f.doc, actions.fieldAA, f.signed, tw.actions.fieldAA) {
				return false
			}
		}
	}
	if isWidget {
		return true
	}
	_, a := dict["A"]
	_, aa := dict["AA"]
	return !a && (isField || !aa)
}

// sameActions reports whether a widget of the current document runs the
// actions a template widget of the signed revision runs.
func (f *fieldDiff) sameActions(ctx context.Context, actions, template widgetActions) bool {
	return sameAcross(ctx, f.doc, actions.a, f.signed, template.a) &&
		sameAcross(ctx, f.doc, actions.aa, f.signed, template.aa) &&
		sameAcross(ctx, f.doc, actions.fieldAA, f.signed, template.fieldAA)
}

// templateInstances returns the template instances of the signed revision,
// reading them on the first call, or nil when there are none or they cannot be
// read.
func (f *fieldDiff) templateInstances(ctx context.Context) *templateInstances {
	if !f.templatesRead {
		f.templatesRead = true
		var err error
		if f.templates, err = newTemplateInstances(ctx, f.doc, f.signed, f.held); err != nil {
			// Templates that cannot be read instantiate nothing, so a new
			// field stays a change.
			f.doc.Logger.Debug("reading the signed revision's templates", slog.Any("err", err))
		}
	}
	return f.templates
}

// widgetsUnder returns num when it is a widget and the widgets its /Kids reach
// in the current document, each once. The walk is bounded by
// [maxFieldTreeDepth].
func (f *fieldDiff) widgetsUnder(ctx context.Context, num int, dict ast.PdfDict) []numberedDict {
	type node struct {
		numberedDict
		depth int
	}
	var widgets []numberedDict
	seen := map[int]bool{num: true}
	stack := []node{{numberedDict{num, dict}, 0}}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if dictName(n.dict, "Subtype") == "Widget" {
			widgets = append(widgets, n.numberedDict)
		}
		if n.depth >= maxFieldTreeDepth {
			continue
		}
		kids, err := document.ResolveArray(ctx, f.doc, n.dict["Kids"])
		if err != nil {
			continue
		}
		for _, kid := range kids {
			ref, ok := kid.(ast.PdfIndirectRef)
			if !ok || seen[ref.ObjectNum] {
				continue
			}
			seen[ref.ObjectNum] = true
			kidDict, err := document.ResolveDict(ctx, f.doc, ref)
			if err == nil && kidDict != nil {
				stack = append(stack, node{numberedDict{ref.ObjectNum, kidDict}, n.depth + 1})
			}
		}
	}
	return widgets
}

// actionsOf returns the actions of the widget num of doc ([widgetActions]).
// The field is the nearest of the widget and its ancestors that carries a
// partial name (ISO 32000-1, 12.7.3.1, Table 220), found within
// [maxFieldTreeDepth] steps.
func actionsOf(ctx context.Context, doc *document.Document, num int, widget ast.PdfDict) widgetActions {
	actions := widgetActions{a: widget["A"], aa: widget["AA"], terminal: num}
	if _, ok := widget["T"]; ok {
		return actions
	}
	seen := make(map[int]bool)
	for depth, dict := 0, widget; depth < maxFieldTreeDepth; depth++ {
		parentObj, parent, ok := document.ResolveFieldParent(ctx, doc, dict, seen)
		if !ok {
			break
		}
		if _, ok := parent["T"]; ok {
			actions.fieldAA = parent["AA"]
			actions.terminal = refNum(parentObj)
			break
		}
		dict = parent
	}
	return actions
}

// partialName returns the partial name of the field of a widget of doc: the
// /T of the nearest of the widget and its ancestors that carries one (ISO
// 32000-1, 12.7.3.2), or "" when none does. The walk is bounded by
// [maxFieldTreeDepth].
func partialName(ctx context.Context, doc *document.Document, widget ast.PdfDict) string {
	seen := make(map[int]bool)
	for depth, dict := 0, widget; dict != nil && depth <= maxFieldTreeDepth; depth++ {
		if _, ok := dict["T"]; ok {
			name, err := document.ResolveString(ctx, doc, dict["T"])
			if err != nil {
				return ""
			}
			return document.DecodePdfString(name)
		}
		var ok bool
		if _, dict, ok = document.ResolveFieldParent(ctx, doc, dict, seen); !ok {
			break
		}
	}
	return ""
}

// rectOf returns the /Rect of an annotation of doc as numbers, or nil when it
// is no array of numbers.
func rectOf(ctx context.Context, doc *document.Document, annot ast.PdfDict) []float64 {
	arr, err := document.ResolveArray(ctx, doc, annot["Rect"])
	if err != nil {
		return nil
	}
	rect := make([]float64, 0, len(arr))
	for _, entry := range arr {
		resolved, err := document.ResolveObject(ctx, doc, entry)
		if err != nil {
			return nil
		}
		v, ok := document.Number(resolved)
		if !ok {
			return nil
		}
		rect = append(rect, v)
	}
	return rect
}

// sameNumbers reports whether a and b hold the same numbers, in order, and
// are not empty.
func sameNumbers(a, b []float64) bool {
	if len(a) == 0 || len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// sameAcross reports whether x, an object of the document a, is the same
// object as y of the document b, following references on both sides: a
// dictionary or an array entry by entry, a stream by its dictionary and its
// data, and anything else by value. Two absent objects are the same. It
// compares at most [maxActionCompare] pairs, and a pair of references it
// already compares counts as the same, so a cycle ends the walk.
func sameAcross(ctx context.Context, a *document.Document, x ast.PdfObject, b *document.Document, y ast.PdfObject) bool {
	budget := pairBudget{left: maxActionCompare}
	return budget.same(ctx, a, x, b, y)
}

// pairBudget is how many more pairs of objects the comparisons that share it
// may compare ([pairBudget.same]). exhausted records that one of them ran out.
type pairBudget struct {
	left      int
	exhausted bool
}

// same reports whether x of a is the same object as y of b, as [sameAcross]
// compares them, spending one unit of the budget on each pair. A comparison
// that runs out of budget reports the objects different. Dictionary entries
// are compared in key order, so what a comparison spends does not depend on
// the order of a Go map, and whether a later comparison sharing the budget
// runs out is the same from run to run.
func (p *pairBudget) same(ctx context.Context, a *document.Document, x ast.PdfObject, b *document.Document, y ast.PdfObject) bool {
	type pair struct{ x, y ast.PdfObject }
	stack := []pair{{x, y}}
	compared := make(map[[2]int]bool)
	for len(stack) > 0 {
		if p.left <= 0 {
			p.exhausted = true
			return false
		}
		if ctx.Err() != nil {
			return false
		}
		p.left--
		top := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		xr, xRef := top.x.(ast.PdfIndirectRef)
		yr, yRef := top.y.(ast.PdfIndirectRef)
		if xRef && yRef {
			key := [2]int{xr.ObjectNum, yr.ObjectNum}
			if compared[key] {
				continue
			}
			compared[key] = true
		}
		px, err := resolvedIn(ctx, a, top.x)
		if err != nil {
			return false
		}
		py, err := resolvedIn(ctx, b, top.y)
		if err != nil {
			return false
		}
		switch xv := px.(type) {
		case ast.PdfDict:
			yv, ok := py.(ast.PdfDict)
			if !ok || len(xv) != len(yv) {
				return false
			}
			keys := slices.Sorted(maps.Keys(xv))
			for _, key := range slices.Backward(keys) {
				other, ok := yv[key]
				if !ok {
					return false
				}
				stack = append(stack, pair{xv[key], other})
			}
		case ast.PdfArray:
			yv, ok := py.(ast.PdfArray)
			if !ok || len(xv) != len(yv) {
				return false
			}
			for i := len(xv) - 1; i >= 0; i-- {
				stack = append(stack, pair{xv[i], yv[i]})
			}
		case ast.PdfStream:
			yv, ok := py.(ast.PdfStream)
			if !ok || !sameStreamData(ctx, xv, yv) {
				return false
			}
			stack = append(stack, pair{xv.Dict, yv.Dict})
		default:
			if !sameObject(px, py) {
				return false
			}
		}
	}
	return true
}

// resolvedIn resolves obj in doc when it is a reference, and returns it as
// written otherwise.
func resolvedIn(ctx context.Context, doc *document.Document, obj ast.PdfObject) (ast.PdfObject, error) {
	if _, ok := obj.(ast.PdfIndirectRef); !ok {
		return obj, nil
	}
	return document.ResolveObject(ctx, doc, obj)
}

// sameStreamData reports whether two streams hold the same data. The reads
// are bounded by the document's stream budget.
func sameStreamData(ctx context.Context, x, y ast.PdfStream) bool {
	xData, err := x.Bytes(ctx)
	if err != nil {
		return false
	}
	yData, err := y.Bytes(ctx)
	return err == nil && bytes.Equal(xData, yData)
}
