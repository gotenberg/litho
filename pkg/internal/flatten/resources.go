package flatten

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/mutations"
)

// groupTransparency reports whether dict, a page or form XObject dictionary,
// holds a transparency group (ISO 32000-2, 11.6.6, Table 145).
func groupTransparency(ctx context.Context, doc *document.Document, dict ast.PdfDict) bool {
	g, err := document.ResolveDict(ctx, doc, dict["Group"])
	if err != nil || g == nil {
		return false
	}
	s, err := document.ResolveName(ctx, doc, g["S"])
	return err == nil && s == "Transparency"
}

// neutralStates replaces, in resources, every graphics state dictionary that
// sets transparency with a copy that does not, reporting whether it replaced
// any. Once a page's transparent painting is composited, what still reads
// those states is text, kept as text and drawn opaque, and state that paints
// nothing. The copies are new objects: another page or an annotation
// appearance may read the originals.
func (f *flattener) neutralStates(ctx context.Context, resources ast.PdfDict) (bool, error) {
	states, err := document.ResolveDict(ctx, f.doc, resources["ExtGState"])
	if err != nil || states == nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return false, ctxErr
		}
		// A subdictionary that does not resolve defines no state a viewer
		// applies (ISO 32000-2, 7.3.10).
		return false, nil
	}
	var out ast.PdfDict
	for _, name := range slices.Sorted(maps.Keys(states)) {
		obj := states[name]
		ref, isRef := obj.(ast.PdfIndirectRef)
		copied, known := ast.PdfObject(nil), false
		if isRef {
			copied, known = f.states[ref.ObjectNum]
		}
		if !known {
			gs, err := document.ResolveDict(ctx, f.doc, obj)
			if err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return false, ctxErr
				}
				continue
			}
			if extGStateTransparent(ctx, f.doc, gs) {
				copied = f.alloc.Alloc(neutralState(ctx, f.doc, gs))
			}
			if isRef {
				f.states[ref.ObjectNum] = copied
			}
		}
		if copied != nil {
			if out == nil {
				out = maps.Clone(states)
			}
			out[name] = copied
		}
	}
	if out == nil {
		return false, nil
	}
	resources["ExtGState"] = out
	return true, nil
}

// neutralState returns a copy of gs that sets no transparency: constant
// alphas of 1, the Normal blend mode and no soft mask (ISO 32000-2, 8.4.5,
// Table 57), its other entries kept.
func neutralState(ctx context.Context, doc *document.Document, gs ast.PdfDict) ast.PdfDict {
	out := maps.Clone(gs)
	for _, key := range []ast.PdfName{"CA", "ca"} {
		if _, ok := out[key]; ok {
			out[key] = ast.PdfReal(1)
		}
	}
	if blendModeTransparent(ctx, doc, gs["BM"]) {
		out["BM"] = ast.PdfName("Normal")
	}
	if _, ok := out["SMask"]; ok {
		out["SMask"] = ast.PdfName("None")
	}
	return out
}

// textForm returns a copy of the form XObject the Do op names, against
// resources, that holds only its text: the content a region's image takes
// over is gone, and its text stays text (see [Region]). The copy of a form
// is made once. A form deeper than the form-depth budget, or one that paints
// itself where it recurses, both of which the renderer skips, gives the zero
// reference: nothing to draw.
func (f *flattener) textForm(ctx context.Context, op content.Operation, resources ast.PdfDict, depth int) (ast.PdfIndirectRef, error) {
	obj, stream, subtype, ok := f.scan.xobject(ctx, resources, op)
	if !ok || subtype != "Form" {
		return ast.PdfIndirectRef{}, fmt.Errorf("%w: %v", errNotForm, op.Operands)
	}
	ref, isRef := obj.(ast.PdfIndirectRef)
	if isRef {
		if copied, ok := f.forms[ref.ObjectNum]; ok {
			return copied, nil
		}
	}
	if depth > f.doc.Limits().WithDefaults().MaxFormDepth {
		return ast.PdfIndirectRef{}, nil
	}
	if isRef {
		f.forms[ref.ObjectNum] = ast.PdfIndirectRef{}
	}
	copied, err := f.copyTextForm(ctx, stream, resources, depth)
	if err != nil {
		return ast.PdfIndirectRef{}, err
	}
	if isRef {
		f.forms[ref.ObjectNum] = copied
	}
	return copied, nil
}

// copyTextForm writes the copy [flattener.textForm] returns of stream, a
// form painted against parent: its content without the operators that paint
// anything but text, a path that clips kept as a clip (its painting operator
// becomes n, ISO 32000-2, 8.5.4), and each form it paints that shows text
// replaced by that form's copy. Text objects stay whole, their marked content
// with them. The copy has no transparency group, and its graphics states set
// no transparency.
func (f *flattener) copyTextForm(ctx context.Context, stream ast.PdfStream, parent ast.PdfDict, depth int) (ast.PdfIndirectRef, error) {
	data, err := f.doc.DecodeStream(ctx, stream)
	if err != nil {
		return ast.PdfIndirectRef{}, fmt.Errorf("decoding a form showing text: %w", err)
	}
	ops, ranges, err := content.ParseStreamDataWithOffsets(ctx, data, content.SplitGluedOperators())
	if err != nil {
		return ast.PdfIndirectRef{}, fmt.Errorf("parsing a form showing text: %w", err)
	}
	formRes, err := document.ResolveDict(ctx, f.doc, stream.Dict["Resources"])
	if err != nil || formRes == nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ast.PdfIndirectRef{}, ctxErr
		}
		formRes = parent
	}
	resources := maps.Clone(formRes)
	if resources == nil {
		resources = ast.PdfDict{}
	}
	xobjects, err := f.subdict(ctx, resources, "XObject")
	if err != nil {
		return ast.PdfIndirectRef{}, err
	}

	var edits []mutations.SpanEdit
	remove := func(i int, replacement string) {
		edits = append(edits, mutations.SpanEdit{Span: ranges[i], Replacement: []byte(" " + replacement + " ")})
	}
	inText, clipping := false, false
	var path []int
	for i, op := range ops {
		if i%opsPerContextCheck == 0 {
			if err := ctx.Err(); err != nil {
				return ast.PdfIndirectRef{}, err
			}
		}
		switch {
		case op.Operator == "BT":
			inText = true
		case op.Operator == "ET":
			inText = false
		case inText:
		case op.Operator == "W" || op.Operator == "W*":
			clipping = true
			path = append(path, i)
		case op.Operator == "n" || isPaint(op.Operator):
			if clipping {
				if op.Operator != "n" {
					remove(i, "n")
				}
			} else {
				for _, p := range append(path, i) {
					remove(p, "")
				}
			}
			path, clipping = path[:0], false
		case pathOperator(op.Operator):
			path = append(path, i)
		case op.Operator == "sh" || op.Operator == "BI":
			remove(i, "")
		case op.Operator == "Do":
			innerObj, inner, subtype, ok := f.scan.xobject(ctx, formRes, op)
			if !ok || subtype != "Form" || !f.scan.form(ctx, innerObj, inner, formRes, depth+1, nil).text {
				remove(i, "")
				continue
			}
			copied, err := f.textForm(ctx, op, formRes, depth+1)
			if err != nil {
				return ast.PdfIndirectRef{}, err
			}
			if copied.ObjectNum == 0 {
				remove(i, "")
				continue
			}
			name := f.newName(xobjects, "FlatTx")
			xobjects[name] = copied
			remove(i, "/"+string(name)+" Do")
		}
	}
	out, err := mutations.EditSpans(data, edits)
	if err != nil {
		return ast.PdfIndirectRef{}, fmt.Errorf("editing a form showing text: %w", err)
	}
	if _, err := f.neutralStates(ctx, resources); err != nil {
		return ast.PdfIndirectRef{}, err
	}
	if len(xobjects) > 0 {
		resources["XObject"] = xobjects
	}
	dict := maps.Clone(stream.Dict)
	for _, key := range []ast.PdfName{"Group", "Filter", "DecodeParms", "DL", "Length"} {
		delete(dict, key)
	}
	dict["Resources"] = resources
	dict["Length"] = ast.PdfInt(len(out))
	return f.alloc.Alloc(ast.NewStream(dict, out)), nil
}

// isPaint reports whether operator paints a path (ISO 32000-2, 8.5.3.1).
func isPaint(operator string) bool {
	_, ok := pathPaints(operator)
	return ok
}
