package flatten

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
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
			if stateNeedsNeutral(ctx, f.doc, gs) {
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

// stateNeedsNeutral reports whether gs holds a transparency entry that
// [neutralState] rewrites: one that sets transparency, or one that sets none
// as written but is not the plain value ISO 19005-1, 6.4 requires of a file
// without transparency, a /BM a reader does not recognise, which reads as
// Normal (ISO 32000-2, 11.3.5), or a constant alpha above 1, which reads as 1.
func stateNeedsNeutral(ctx context.Context, doc *document.Document, gs ast.PdfDict) bool {
	if extGStateTransparent(ctx, doc, gs) {
		return true
	}
	for _, key := range []ast.PdfName{"CA", "ca"} {
		if obj, err := document.ResolveObject(ctx, doc, gs[key]); err == nil && obj != nil {
			if v, ok := document.Number(obj); !ok || v != 1 {
				return true
			}
		}
	}
	if _, ok := gs["BM"]; ok && !plainNormal(ctx, doc, gs["BM"]) {
		return true
	}
	return false
}

// plainNormal reports whether a /BM entry is the name Normal or Compatible.
func plainNormal(ctx context.Context, doc *document.Document, obj ast.PdfObject) bool {
	name, err := document.ResolveName(ctx, doc, obj)
	return err == nil && (name == "Normal" || name == "Compatible")
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
	if _, ok := out["BM"]; ok && !plainNormal(ctx, doc, gs["BM"]) {
		out["BM"] = ast.PdfName("Normal")
	}
	if _, ok := out["SMask"]; ok {
		out["SMask"] = ast.PdfName("None")
	}
	return out
}

// neutralPatterns replaces, in resources, every pattern whose graphics
// states set transparency with a copy whose do not, reporting whether it
// replaced any: a tiling pattern's own resources (ISO 32000-2, 8.7.3.1), a
// shading pattern's /ExtGState (8.7.4.1). Painting with such a pattern is a
// transparent unit, which a region's image takes over, so what still names
// the pattern paints nothing with it. The copies are new objects.
func (f *flattener) neutralPatterns(ctx context.Context, resources ast.PdfDict) (bool, error) {
	patterns, err := document.ResolveDict(ctx, f.doc, resources["Pattern"])
	if err != nil || patterns == nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return false, ctxErr
		}
		return false, nil
	}
	var out ast.PdfDict
	for _, name := range slices.Sorted(maps.Keys(patterns)) {
		obj := patterns[name]
		ref, isRef := obj.(ast.PdfIndirectRef)
		copied, known := ast.PdfObject(nil), false
		if isRef {
			copied, known = f.patterns[ref.ObjectNum]
		}
		if !known {
			c, err := f.neutralPattern(ctx, obj)
			if err != nil {
				return false, err
			}
			copied = c
			if isRef {
				f.patterns[ref.ObjectNum] = copied
			}
		}
		if copied != nil {
			if out == nil {
				out = maps.Clone(patterns)
			}
			out[name] = copied
		}
	}
	if out == nil {
		return false, nil
	}
	resources["Pattern"] = out
	return true, nil
}

// neutralPattern returns the copy [flattener.neutralPatterns] puts in place
// of the pattern obj, or nil when it sets no transparency.
func (f *flattener) neutralPattern(ctx context.Context, obj ast.PdfObject) (ast.PdfObject, error) {
	// What does not resolve is null (ISO 32000-2, 7.3.10): no pattern a
	// viewer paints with, so nothing to copy.
	resolved, err := document.ResolveObject(ctx, f.doc, obj)
	if err != nil {
		return nil, ctx.Err()
	}
	switch v := resolved.(type) {
	case ast.PdfStream:
		own, err := document.ResolveDict(ctx, f.doc, v.Dict["Resources"])
		if err != nil || own == nil {
			return nil, ctx.Err()
		}
		resources := maps.Clone(own)
		changed, err := f.neutralStates(ctx, resources)
		if err != nil || !changed {
			return nil, err
		}
		dict := maps.Clone(v.Dict)
		dict["Resources"] = resources
		return f.alloc.Alloc(ast.NewLazyStream(dict, v.Bytes)), nil
	case ast.PdfDict:
		gs, err := document.ResolveDict(ctx, f.doc, v["ExtGState"])
		if err != nil || gs == nil || !stateNeedsNeutral(ctx, f.doc, gs) {
			return nil, ctx.Err()
		}
		dict := maps.Clone(v)
		dict["ExtGState"] = neutralState(ctx, f.doc, gs)
		return f.alloc.Alloc(dict), nil
	}
	return nil, nil
}

// textForm returns a copy of the form XObject the Do op names, against
// resources, that holds only its text: the content a region's image takes
// over is gone, and its text stays text (see [Region]). The copy of a form
// is made once. A form deeper than the form-depth budget, or one that paints
// itself where it recurses, both of which the renderer skips, gives the zero
// reference: nothing to draw.
//
// hides says which colours the Do paints under a constant alpha of 0. A
// transparency group composites whole under the alpha in force at its Do
// (ISO 32000-2, 11.6.6), so a group painted at an alpha of 0 shows nothing
// and gives no copy; any other form's text starts from that alpha.
func (f *flattener) textForm(ctx context.Context, op content.Operation, resources ast.PdfDict, depth int, hides paints) (ast.PdfIndirectRef, error) {
	obj, stream, subtype, ok := f.scan.xobject(ctx, resources, op)
	if !ok || subtype != "Form" {
		return ast.PdfIndirectRef{}, fmt.Errorf("%w: %v", errNotForm, op.Operands)
	}
	if hides.fill && groupTransparency(ctx, f.doc, stream.Dict) {
		return ast.PdfIndirectRef{}, nil
	}
	ref, isRef := obj.(ast.PdfIndirectRef)
	key := copyKey{num: ref.ObjectNum, hides: hides}
	if isRef {
		if copied, ok := f.forms[key]; ok {
			return copied, nil
		}
	}
	if depth > f.doc.Limits().WithDefaults().MaxFormDepth {
		return ast.PdfIndirectRef{}, nil
	}
	if isRef {
		f.forms[key] = ast.PdfIndirectRef{}
	}
	copied, err := f.copyTextForm(ctx, stream, resources, depth, hides)
	if err != nil {
		return ast.PdfIndirectRef{}, err
	}
	if isRef {
		f.forms[key] = copied
	}
	return copied, nil
}

// copyKey names the copy [flattener.textForm] makes of a form: its object
// number and the colours it is painted with nothing showing in.
type copyKey struct {
	num   int
	hides paints
}

// copyTextForm writes the copy [flattener.textForm] returns of stream, a
// form painted against parent: its content without the operators that paint
// anything but text, a path that clips kept as a clip (its painting operator
// becomes n, ISO 32000-2, 8.5.4), and each form it paints that shows text
// replaced by that form's copy. Text objects stay whole, their marked content
// with them. The copy has no transparency group, and its graphics states set
// no transparency.
func (f *flattener) copyTextForm(ctx context.Context, stream ast.PdfStream, parent ast.PdfDict, depth int, hides paints) (ast.PdfIndirectRef, error) {
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
	// The copy draws no image and no form but the copies it makes, so its
	// /XObject names those alone: an image with a soft mask the form drew
	// stays out of what the output's pages reach.
	xobjects := ast.PdfDict{}
	m := graphics.NewMachine([4]float64{})
	m.Doc, m.Resources = f.doc, formRes
	if hides.fill {
		m.Current().FillAlpha = 0
	}
	if hides.stroke {
		m.Current().StrokeAlpha = 0
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
		// The machine runs every operator it models and steps over the rest;
		// an error is a cancelled ctx, checked above.
		_ = m.ExecuteOne(ctx, op) //nolint:errcheck // Deliberate: ctx is checked every opsPerContextCheck operators.
		switch {
		case op.Operator == "BT":
			inText = true
		case op.Operator == "ET":
			inText = false
		case inText:
			if !showText(op.Operator) {
				continue
			}
			if mode, rewrite, _ := f.scan.textMode(ctx, m.Current(), formRes); rewrite {
				edits = append(edits, mutations.SpanEdit{
					Span:        ranges[i],
					Replacement: modeWrapped([2]int{mode, m.Current().Text.Tmode}, data[ranges[i].Start:ranges[i].End]),
				})
			}
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
			st := m.Current()
			copied, err := f.textForm(ctx, op, formRes, depth+1, paints{fill: st.FillAlpha <= 0, stroke: st.StrokeAlpha <= 0})
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
	if _, err := f.neutralPatterns(ctx, resources); err != nil {
		return ast.PdfIndirectRef{}, err
	}
	delete(resources, "XObject")
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
