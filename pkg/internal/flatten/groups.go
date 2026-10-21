package flatten

import (
	"context"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// keptForms readies the XObjects of a page whose operations ops are edited
// by edits, against resources, and reports whether it changed xobjects, the
// page's new /XObject subdictionary. A form the page keeps drawing that
// holds a transparency group or a graphics state setting transparency, or
// paints a form that does, is pointed at a copy without either (see
// [flattener.ungroup]). A name only removed operations
// drew is dropped, so the image or form whose painting a composite took over
// leaves the page's resources, unless a form the page keeps has no resources
// of its own and may draw it by that name (ISO 32000-2, 7.8.3).
func (f *flattener) keptForms(ctx context.Context, ops []content.Operation, edits map[int]*opEdit, resources, xobjects ast.PdfDict) (bool, error) {
	used := make(map[ast.PdfName]bool)
	dropped := make(map[ast.PdfName]bool)
	inheriting, changed := false, false
	for i, op := range ops {
		if op.Operator != "Do" || len(op.Operands) != 1 {
			continue
		}
		name, ok := op.Operands[0].(ast.PdfName)
		if !ok {
			continue
		}
		if e := edits[i]; e != nil && e.remove {
			dropped[name] = true
			continue
		}
		used[name] = true
		obj, stream, subtype, ok := f.scan.xobject(ctx, resources, op)
		if !ok || subtype != "Form" {
			continue
		}
		if _, own := stream.Dict["Resources"]; !own {
			inheriting = true
		}
		if info := f.scan.form(ctx, obj, stream, resources, 1, nil); !info.group && !info.states {
			continue
		}
		copied, err := f.ungroup(ctx, obj, stream, 1)
		if err != nil {
			return false, err
		}
		if _, defined := xobjects[name]; defined {
			xobjects[name] = copied
			changed = true
		}
	}
	if inheriting {
		return changed, nil
	}
	for _, name := range slices.Sorted(maps.Keys(dropped)) {
		if _, defined := xobjects[name]; defined && !used[name] {
			delete(xobjects, name)
			changed = true
		}
	}
	return changed, nil
}

// ungroup returns a copy of the form XObject obj without its transparency
// group, whose graphics states set no transparency (see [neutralState]), and
// whose resources name such copies of the forms it paints that need one. The
// form paints no transparency: the group of opaque content paints exactly as
// the content painted without it (ISO 32000-2, 11.4.5, 11.4.6, 11.6.6), and a
// state it sets transparency with paints nothing, or the form would have been
// composited. ISO 19005-1, 6.4 reads every graphics state a resource
// dictionary holds, used or not. The copy is a new object, its
// content the original's bytes: an annotation appearance may still paint the
// original under a constant alpha, where the group does change how it
// composites. A form deeper than the form-depth budget, which the renderer
// skips, is returned as it is.
//
// # Corpus evidence
//
//   - litho paintop_2b.pdf: the appearance of its Square names a state at ca
//     0.5 and paints nothing with it. Drawn into the page by annotation
//     flattening, the form kept the state, and PDF/A-1b validation still found
//     ISO 19005-1, 6.4 test 6.
func (f *flattener) ungroup(ctx context.Context, obj ast.PdfObject, stream ast.PdfStream, depth int) (ast.PdfObject, error) {
	ref, isRef := obj.(ast.PdfIndirectRef)
	if isRef {
		if copied, ok := f.ungrouped[ref.ObjectNum]; ok {
			return copied, nil
		}
		// A form that paints itself is left as it is where it recurses.
		f.ungrouped[ref.ObjectNum] = obj
	}
	if depth > f.doc.Limits().WithDefaults().MaxFormDepth {
		return obj, nil
	}
	dict := maps.Clone(stream.Dict)
	delete(dict, "Group")
	own, err := document.ResolveDict(ctx, f.doc, stream.Dict["Resources"])
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		own = nil
	}
	if own != nil {
		resources := maps.Clone(own)
		xobjects, err := f.subdict(ctx, resources, "XObject")
		if err != nil {
			return nil, err
		}
		for _, name := range slices.Sorted(maps.Keys(xobjects)) {
			inner, err := document.ResolveStream(ctx, f.doc, xobjects[name])
			if err != nil || inner.Dict == nil {
				continue
			}
			if subtype, err := document.ResolveName(ctx, f.doc, inner.Dict["Subtype"]); err != nil || subtype != "Form" {
				continue
			}
			if info := f.scan.form(ctx, xobjects[name], inner, own, depth+1, nil); !info.group && !info.states {
				continue
			}
			copied, err := f.ungroup(ctx, xobjects[name], inner, depth+1)
			if err != nil {
				return nil, err
			}
			xobjects[name] = copied
		}
		resources["XObject"] = xobjects
		if _, err := f.neutralStates(ctx, resources); err != nil {
			return nil, err
		}
		dict["Resources"] = resources
	}
	copied := f.alloc.Alloc(ast.NewLazyStream(dict, stream.Bytes))
	if isRef {
		f.ungrouped[ref.ObjectNum] = copied
	}
	return copied, nil
}

// plainImages replaces, in xobjects, each image whose /SMask is no soft mask
// image, such as the name /None, with a copy without the entry, reporting
// whether it replaced any. Such an entry masks nothing (ISO 32000-2, 8.9.5,
// Table 87 makes /SMask a stream), so the image draws the same, and ISO
// 19005-1, 6.4 still reads the key as transparency. The copies are new
// objects, their samples the original's bytes.
func (f *flattener) plainImages(ctx context.Context, xobjects ast.PdfDict) bool {
	changed := false
	for _, name := range slices.Sorted(maps.Keys(xobjects)) {
		obj := xobjects[name]
		ref, isRef := obj.(ast.PdfIndirectRef)
		if isRef {
			if copied, ok := f.images[ref.ObjectNum]; ok {
				if copied != nil {
					xobjects[name], changed = copied, true
				}
				continue
			}
		}
		var copied ast.PdfObject
		stream, err := document.ResolveStream(ctx, f.doc, obj)
		if err == nil && stream.Dict != nil {
			subtype, _ := document.ResolveName(ctx, f.doc, stream.Dict["Subtype"]) //nolint:errcheck // Deliberate: a subtype that does not read names no image.
			if _, has := stream.Dict["SMask"]; has && subtype == "Image" && !imageTransparent(ctx, f.doc, stream.Dict) {
				dict := maps.Clone(stream.Dict)
				delete(dict, "SMask")
				copied = f.alloc.Alloc(ast.NewLazyStream(dict, stream.Bytes))
			}
		}
		if isRef {
			f.images[ref.ObjectNum] = copied
		}
		if copied != nil {
			xobjects[name], changed = copied, true
		}
	}
	return changed
}

// transparentAnnotations counts the annotations of a page dictionary that
// paint with transparency: a constant opacity /CA below 1 (ISO 32000-2,
// 12.5.2), or a normal appearance that holds a transparency group or paints
// with transparency (12.5.5). [Document] leaves annotations as they are: an
// appearance composites over the page as a viewer draws it, and [Annotations]
// bakes the ones it can into the page first.
func (f *flattener) transparentAnnotations(ctx context.Context, dict ast.PdfDict) int {
	annots, err := document.ResolveArray(ctx, f.doc, dict["Annots"])
	if err != nil {
		return 0
	}
	n := 0
	for _, entry := range annots {
		annot, err := document.ResolveDict(ctx, f.doc, entry)
		if err != nil || annot == nil {
			continue
		}
		if f.annotationTransparent(ctx, annot) {
			n++
		}
	}
	return n
}

// statesSetTransparency reports whether a graphics state dictionary of
// resources sets transparency, painting with it or not: a form or an
// appearance that names one still holds transparency (ISO 19005-1, 6.4 reads
// every ExtGState a resource dictionary holds).
func statesSetTransparency(ctx context.Context, doc *document.Document, resources ast.PdfDict) bool {
	states, err := document.ResolveDict(ctx, doc, resources["ExtGState"])
	if err != nil {
		return false
	}
	for _, name := range slices.Sorted(maps.Keys(states)) {
		if gs, err := document.ResolveDict(ctx, doc, states[name]); err == nil && stateNeedsNeutral(ctx, doc, gs) {
			return true
		}
	}
	return false
}

// appearanceTransparent reports whether a normal appearance of annot, a
// stream or a dictionary of streams by state, holds a transparency group or
// paints with transparency.
func (f *flattener) appearanceTransparent(ctx context.Context, annot ast.PdfDict) bool {
	ap, err := document.ResolveDict(ctx, f.doc, annot["AP"])
	if err != nil || ap == nil {
		return false
	}
	normal, err := document.ResolveObject(ctx, f.doc, ap["N"])
	if err != nil {
		return false
	}
	candidates := []ast.PdfObject{ap["N"]}
	if states, ok := normal.(ast.PdfDict); ok {
		candidates = candidates[:0]
		for _, k := range slices.Sorted(maps.Keys(states)) {
			candidates = append(candidates, states[k])
		}
	}
	for _, obj := range candidates {
		stream, err := document.ResolveStream(ctx, f.doc, obj)
		if err != nil || stream.Dict == nil {
			continue
		}
		info := f.scan.form(ctx, obj, stream, nil, 1, nil)
		if info.transparent || info.group || info.states {
			return true
		}
	}
	return false
}
