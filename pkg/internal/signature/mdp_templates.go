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

// maxActionCompare bounds how many pairs of objects [sameAcross] compares. It
// is an anti-DoS guard, not spec-mandated: an action and those its /Next entry
// chains to (ISO 32000-1, 12.6.2, Table 193) are a handful of dictionaries, and
// the bound stops a crafted chain from costing more than a few thousand
// comparisons. A comparison that reaches it reports the objects different, so
// the field it judges stays a change.
const maxActionCompare = 1 << 12

// templateWidget is a widget of a page the signed revision names as a
// template, with what [copiedWidget] compares.
type templateWidget struct {
	rect    []float64
	ft      string
	name    string
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
// tree, which a reader does not show, and in its /Pages tree, which it does,
// and a script instantiates either by copying it, its fields among what it
// holds, to a new page (7.7.4, Table 31).
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

// match returns the widget of a template page that the widget of doc copies
// ([copiedWidget]).
func (t *templateInstances) match(ctx context.Context, doc *document.Document, widget ast.PdfDict) (templateWidget, bool) {
	for _, tp := range t.templates {
		if tw, ok := copiedWidget(ctx, doc, widget, tp.widgets); ok {
			return tw, true
		}
	}
	return templateWidget{}, false
}

// copiedWidget returns the widget of widgets that the widget of doc copies:
// the same rectangle, the same field type and the same partial name for its
// field.
func copiedWidget(ctx context.Context, doc *document.Document, widget ast.PdfDict, widgets []templateWidget) (templateWidget, bool) {
	rect := rectOf(ctx, doc, widget)
	ft := fieldTypeIn(ctx, doc, widget)
	name := partialName(ctx, doc, widget)
	for _, tw := range widgets {
		if tw.ft == ft && tw.name == name && sameNumbers(tw.rect, rect) {
			return tw, true
		}
	}
	return templateWidget{}, false
}

// instantiated reports whether the field or widget num, which the update
// added as dict, arrives with a template instantiation, or false when the
// signed revision names no template.
//
// ISO 32000-1, 12.8.2.2, Table 254 permits instantiating page templates at
// P=2, and 12.7.6 has an instantiation copy a template page, with its fields,
// to a new page. A script that renames the copied fields gives each a new
// field of its own, under new nodes that group them. Every widget at or under
// num must be listed on a page the signed revision did not hold and copy a
// widget of a template page ([templateInstances.match]), and the actions that
// run when it is used must be the template's ([sameAcross]). A node that is
// neither such a widget nor its field carries no action at all.
//
// The copy is judged by where it sits, what it is called and what it runs,
// which is what a new field changes: it adds a field the certifier did not
// approve, and its actions run when a reader uses it.
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
		tw, ok := t.match(ctx, f.doc, w.dict)
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
