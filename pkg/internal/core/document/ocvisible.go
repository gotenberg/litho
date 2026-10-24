package document

import (
	"context"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// maxVisibilityExpressionDepth caps the nesting of an optional content
// membership dictionary's /VE that [OCVisible] evaluates. Anti-DoS guard, not
// spec-mandated: ISO 32000-1, 8.11.2.2 sets no bound on the nesting, and a
// file controls it, while each level costs a Go stack frame. Real
// expressions nest a few levels; past 64 the expression is read as malformed
// and the dictionary falls back to its /OCGs and /P.
const maxVisibilityExpressionDepth = 64

// OCVisible reports whether optional content oc, the value of an /OC entry
// or of a /Properties entry that marked content tags /OC, is shown under the
// document's default configuration (ISO 32000-1, 8.11.4.3). It reads an
// optional content group (8.11.2.1, Table 98) by its state, and an optional
// content membership dictionary (8.11.2.2, Table 99) by its visibility
// expression /VE when it has a valid one, else by its policy /P (AnyOn by
// default) over the groups its /OCGs names.
//
// Anything else is shown, as viewers show it: an absent or unresolvable
// entry, a dictionary of another type, and a membership dictionary naming no
// group, which Table 99 says has no effect.
func OCVisible(ctx context.Context, doc *Document, oc ast.PdfObject) bool {
	if oc == nil {
		return true
	}
	dict, err := ResolveDict(ctx, doc, oc)
	if err != nil || dict == nil {
		return true
	}
	switch t, _ := ResolveName(ctx, doc, dict["Type"]); t {
	case "OCG":
		return groupVisible(oc, doc.OCConfig)
	case "OCMD":
		return membershipVisible(ctx, doc, dict)
	}
	return true
}

// PropertiesOCs maps the names the /Properties of resources gives optional
// content groups and membership dictionaries to their visibility
// ([OCVisible]), which a content walker reads when BDC marks content /OC
// (ISO 32000-1, 8.11.3.2). An entry of another type is left out: the
// content it marks is shown.
//
// # Corpus evidence
//
//   - pdfjs issue269_2: marks all its layers through membership
//     dictionaries and shows two of them. Read as shown, every layer
//     painted in litho's renderer, and the clips a redaction wraps hidden
//     fills in changed what the stack of hidden layers composed: 610 and
//     357 pixels outside the region on litho's rendering of the p1-quarter
//     and p1-band redactions, none on poppler's or PDFium's.
//   - All corpora, the 168 PDFs with /OCProperties, pages 1 to 3: 5 pages
//     change, pdfjs issue269_2, issue12007_reduced, issue12716 page 2,
//     visibility_expressions and pdf20 pdf20-utf8-test, each from apart
//     from poppler 26.09.0 to matching it by the render cell's bars.
func PropertiesOCs(ctx context.Context, doc *Document, resources ast.PdfDict) map[string]bool {
	out := make(map[string]bool)
	props, err := ResolveDict(ctx, doc, resources["Properties"])
	if err != nil || props == nil {
		return out
	}
	for k, v := range props {
		dict, err := ResolveDict(ctx, doc, v)
		if err != nil || dict == nil {
			continue
		}
		if t, _ := ResolveName(ctx, doc, dict["Type"]); t == "OCG" || t == "OCMD" {
			out[string(k)] = OCVisible(ctx, doc, v)
		}
	}
	return out
}

// groupVisible reads the state of the optional content group oc under cfg
// (ISO 32000-1, 8.11.4.3, Table 101): /OFF, then /ON, then /BaseState. A
// group written as a direct object, which no /ON or /OFF array can name,
// takes the base state.
func groupVisible(oc ast.PdfObject, cfg *OCConfig) bool {
	if cfg == nil {
		return true
	}
	if ref, ok := oc.(ast.PdfIndirectRef); ok {
		switch {
		case cfg.Off[ref.ObjectNum]:
			return false
		case cfg.On[ref.ObjectNum]:
			return true
		}
	}
	return cfg.BaseState != "OFF"
}

// membershipVisible evaluates the optional content membership dictionary
// dict (ISO 32000-1, 8.11.2.2, Table 99). /VE, when present and valid, takes
// precedence over /OCGs and /P.
func membershipVisible(ctx context.Context, doc *Document, dict ast.PdfDict) bool {
	if ve, ok := dict["VE"]; ok {
		ev := veEvaluator{ctx: ctx, doc: doc, done: make(map[int]bool), open: make(map[int]bool)}
		if visible, valid := ev.eval(ve, 0); valid {
			return visible
		}
	}
	var states []bool
	ocgs, err := ResolveObject(ctx, doc, dict["OCGs"])
	if err != nil {
		return true
	}
	switch v := ocgs.(type) {
	case ast.PdfDict:
		if state, ok := groupState(ctx, doc, dict["OCGs"]); ok {
			states = append(states, state)
		}
	case ast.PdfArray:
		// Table 99: a null entry, or one that is no group, is ignored.
		for _, item := range v {
			if state, ok := groupState(ctx, doc, item); ok {
				states = append(states, state)
			}
		}
	}
	if len(states) == 0 {
		return true
	}
	policy, _ := ResolveName(ctx, doc, dict["P"])
	switch policy {
	case "AllOn":
		return !slices.Contains(states, false)
	case "AnyOff":
		return slices.Contains(states, false)
	case "AllOff":
		return !slices.Contains(states, true)
	}
	// AnyOn is the default, and Table 99 names no other policy.
	return slices.Contains(states, true)
}

// groupState returns the state of obj when it is an optional content group.
func groupState(ctx context.Context, doc *Document, obj ast.PdfObject) (bool, bool) {
	d, err := ResolveDict(ctx, doc, obj)
	if err != nil || d == nil {
		return false, false
	}
	if t, _ := ResolveName(ctx, doc, d["Type"]); t != "OCG" {
		return false, false
	}
	return groupVisible(obj, doc.OCConfig), true
}

// veEvaluator evaluates a visibility expression (ISO 32000-1, 8.11.2.2,
// Table 99, /VE): an array whose first element is /And, /Or or /Not and
// whose operands are optional content groups or expressions. An expression
// written as an object of its own may be shared: done keeps its value, so
// each is evaluated once however often it is named, and open holds those
// being evaluated, so one that names itself is read as malformed.
type veEvaluator struct {
	ctx  context.Context
	doc  *Document
	done map[int]bool
	open map[int]bool
}

// eval returns the value of the expression or operand obj, and false as its
// second result when obj is malformed.
func (e *veEvaluator) eval(obj ast.PdfObject, depth int) (bool, bool) {
	if depth > maxVisibilityExpressionDepth || e.ctx.Err() != nil {
		return false, false
	}
	ref, isRef := obj.(ast.PdfIndirectRef)
	if isRef {
		if v, ok := e.done[ref.ObjectNum]; ok {
			return v, true
		}
		if e.open[ref.ObjectNum] {
			return false, false
		}
	}
	resolved, err := ResolveObject(e.ctx, e.doc, obj)
	if err != nil {
		return false, false
	}
	switch v := resolved.(type) {
	case ast.PdfDict:
		return groupState(e.ctx, e.doc, obj)
	case ast.PdfArray:
		if isRef {
			e.open[ref.ObjectNum] = true
		}
		value, valid := e.expression(v, depth)
		if isRef {
			delete(e.open, ref.ObjectNum)
			if valid {
				e.done[ref.ObjectNum] = value
			}
		}
		return value, valid
	}
	return false, false
}

// expression evaluates the array arr, [op operand ...].
func (e *veEvaluator) expression(arr ast.PdfArray, depth int) (bool, bool) {
	if len(arr) < 2 {
		return false, false
	}
	op, _ := ResolveName(e.ctx, e.doc, arr[0])
	operands := arr[1:]
	switch op {
	case "Not":
		if len(operands) != 1 {
			return false, false
		}
		v, ok := e.eval(operands[0], depth+1)
		return !v, ok
	case "And", "Or":
		result := op == "And"
		for _, operand := range operands {
			v, ok := e.eval(operand, depth+1)
			if !ok {
				return false, false
			}
			if op == "And" {
				result = result && v
			} else {
				result = result || v
			}
		}
		return result, true
	}
	return false, false
}
