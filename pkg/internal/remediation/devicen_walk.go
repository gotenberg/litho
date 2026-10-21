package remediation

import (
	"context"
	"log/slog"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// deviceNWalk carries one walk of the colour spaces the pages of a document
// reach, which [trimDeviceNColorants] runs to trim DeviceN spaces and
// [fixDeviceNColorants] to complete their /Colorants. space rewrites one
// colour space; the walk finds each and writes every level it changes back
// where it lives.
//
// It starts at each page's effective /Resources, inherited from an ancestor
// when the page states none (ISO 32000-1, 7.7.3.4), and at the appearance
// streams of its annotations. Below a /Resources it reaches the /ColorSpace
// entries, the /ColorSpace of each shading, the shading of each shading
// pattern, and the /Resources of each coloured tiling pattern, form XObject
// and Type 3 font, recursively: the streams compliance's colour-space link
// walk parses.
//
// Every level is read through the allocator and written back where it lives:
// an indirect object at its own object number, an inline one into its parent,
// up to the page or ancestor holding the /Resources. A space counts only when
// its write lands. visited holds the object number of every resource
// dictionary, form, pattern, font and shading already walked, so a shared
// object is rewritten once and a cyclic /Resources chain cannot loop. The
// recursion depth through forms, tiling patterns and Type 3 fonts is bounded
// by the document's form-nesting budget, the bound compliance's formTooDeep
// applies to the walk this one mirrors.
type deviceNWalk struct {
	alloc   *writer.ObjAllocator
	doc     *document.Document
	visited map[int]bool
	count   int
	// space returns the colour space entry names rewritten, read through the
	// allocator, and false when it needs no change.
	space func(ctx context.Context, entry ast.PdfObject) (ast.PdfArray, bool)
	// samples reports that space keeps a space's component count, so the walk
	// also reaches the spaces something is laid out for: the colour space of
	// an image XObject, whose samples carry one component per colorant (ISO
	// 32000-1, 8.9.5.2), and the base of an /Indexed space, whose lookup table
	// holds one byte per base component (8.6.6.3). It then also enters an
	// uncoloured tiling pattern, whose cell processes none of its colour
	// operators (8.7.3.3) but paints its images.
	samples bool
}

// rewritten is what one level of a [deviceNWalk] hands its parent: the value
// the parent holds, whether that value changed (an inline level the parent
// must write into itself), and how many rewritten spaces the change carries
// that no write has landed yet.
type rewritten struct {
	value   ast.PdfObject
	changed bool
	pending int
}

// unchanged is the [rewritten] of a level that needs no write.
func unchanged(obj ast.PdfObject) rewritten {
	return rewritten{value: obj}
}

// walkPages walks the effective /Resources and the annotation appearances of
// every page of the document's page tree.
func (w *deviceNWalk) walkPages(ctx context.Context) error {
	catalogRef, ok := catalogRefForFixer(ctx, w.doc)
	if !ok {
		return nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, w.alloc, w.doc, catalogRef)
	if catalogDict == nil {
		return nil
	}
	pagesObj, ok := catalogDict["Pages"]
	if !ok {
		return nil
	}
	var pageEntries []annotPageEntry
	collectPageRefs(ctx, w.alloc, w.doc, pagesObj, &pageEntries, make(map[int]bool))

	for _, pe := range pageEntries {
		if err := ctx.Err(); err != nil {
			return err
		}
		w.pageResources(ctx, pe)
		w.annotationAppearances(ctx, pe)
	}
	return ctx.Err()
}

// land writes value at obj's object number when obj is an indirect reference,
// counting the pending spaces it carries, and otherwise hands value back for
// the parent to hold inline.
func (w *deviceNWalk) land(obj, value ast.PdfObject, pending int) rewritten {
	if ref, ok := obj.(ast.PdfIndirectRef); ok && ref.ObjectNum > 0 {
		w.alloc.AllocAt(ref.ObjectNum, value)
		w.count += pending
		return unchanged(obj)
	}
	return rewritten{value: value, changed: true, pending: pending}
}

// firstVisit reports whether obj is an inline value or an indirect object the
// walk has not reached yet, marking it.
func (w *deviceNWalk) firstVisit(obj ast.PdfObject) bool {
	ref, ok := obj.(ast.PdfIndirectRef)
	if !ok {
		return true
	}
	if w.visited[ref.ObjectNum] {
		return false
	}
	w.visited[ref.ObjectNum] = true
	return true
}

// pageResources walks the page's effective /Resources and writes an inline
// one back into the page or ancestor that holds it (see [resourcesHolder]).
// The page is read again through the allocator: an earlier page's walk, or
// an earlier fixer, may have written it since the page tree was collected.
func (w *deviceNWalk) pageResources(ctx context.Context, pe annotPageEntry) {
	pe.dict = liveTreeNode(ctx, w.alloc, w.doc, pe)
	holder, ok := resourcesHolder(ctx, w.alloc, w.doc, pe)
	if !ok {
		return
	}
	t := w.resources(ctx, holder.dict["Resources"], 0)
	if !t.changed {
		return
	}
	newHolder := maps.Clone(holder.dict)
	newHolder["Resources"] = t.value
	if writeTreeNode(ctx, w.alloc, w.doc, holder, newHolder) {
		w.count += t.pending
	}
}

// annotationAppearances walks the /Resources of every appearance stream the
// page's annotations carry under /N, /R and /D, as compliance's colour-space
// link walk reaches them. An appearance subdictionary is walked state by
// state: its streams are indirect, so the annotation itself is never
// rewritten.
func (w *deviceNWalk) annotationAppearances(ctx context.Context, pe annotPageEntry) {
	for _, annotObj := range liveArray(ctx, w.alloc, w.doc, pe.dict["Annots"]) {
		if ctx.Err() != nil {
			return
		}
		annot := liveDictValue(ctx, w.alloc, w.doc, annotObj)
		ap := liveDictValue(ctx, w.alloc, w.doc, annot["AP"])
		for _, key := range []ast.PdfName{"N", "R", "D"} {
			entry, ok := ap[key]
			if !ok {
				continue
			}
			if _, isStream := w.dictOrStream(ctx, entry); isStream {
				w.formStream(ctx, entry, 0, false)
				continue
			}
			states := liveDictValue(ctx, w.alloc, w.doc, entry)
			for _, state := range slices.Sorted(maps.Keys(states)) {
				w.formStream(ctx, states[state], 0, false)
			}
		}
	}
}

// resources walks one /Resources dictionary: its /ColorSpace entries, and the
// shadings, patterns, XObjects and Type 3 fonts it names.
func (w *deviceNWalk) resources(ctx context.Context, obj ast.PdfObject, depth int) rewritten {
	if ctx.Err() != nil || depth > w.doc.Limits().MaxFormDepth || !w.firstVisit(obj) {
		return unchanged(obj)
	}
	res := liveDictValue(ctx, w.alloc, w.doc, obj)
	if res == nil {
		return unchanged(obj)
	}

	walks := []struct {
		key  ast.PdfName
		walk func(ast.PdfObject) rewritten
	}{
		{"ColorSpace", func(e ast.PdfObject) rewritten { return w.colorSpace(ctx, e) }},
		{"Shading", func(e ast.PdfObject) rewritten { return w.shading(ctx, e) }},
		{"Pattern", func(e ast.PdfObject) rewritten { return w.pattern(ctx, e, depth) }},
		{"XObject", func(e ast.PdfObject) rewritten { return w.formStream(ctx, e, depth+1, true) }},
		{"Font", func(e ast.PdfObject) rewritten { return w.type3Font(ctx, e, depth+1) }},
	}
	var updates ast.PdfDict
	pending := 0
	for _, walk := range walks {
		sub, ok := res[walk.key]
		if !ok {
			continue
		}
		t := w.entries(ctx, sub, walk.walk)
		if !t.changed {
			continue
		}
		if updates == nil {
			updates = ast.PdfDict{}
		}
		updates[walk.key] = t.value
		pending += t.pending
	}
	if updates == nil {
		return unchanged(obj)
	}
	newRes := maps.Clone(res)
	maps.Copy(newRes, updates)
	return w.land(obj, newRes, pending)
}

// entries applies walk to every value of one resource category dictionary, in
// sorted name order. Inline results accumulate before the dictionary is
// rebuilt once: rebuilding it per entry would keep only the last, the trap
// fixExtGStateDict records.
func (w *deviceNWalk) entries(ctx context.Context, obj ast.PdfObject, walk func(ast.PdfObject) rewritten) rewritten {
	d := liveDictValue(ctx, w.alloc, w.doc, obj)
	var updates ast.PdfDict
	pending := 0
	for _, name := range slices.Sorted(maps.Keys(d)) {
		if ctx.Err() != nil {
			break
		}
		t := walk(d[name])
		if !t.changed {
			continue
		}
		if updates == nil {
			updates = ast.PdfDict{}
		}
		updates[name] = t.value
		pending += t.pending
	}
	if updates == nil {
		return unchanged(obj)
	}
	nd := maps.Clone(d)
	maps.Copy(nd, updates)
	return w.land(obj, nd, pending)
}

// colorSpace rewrites one colour space value with space. When the walk reaches
// what samples are laid out for, the base of an /Indexed space is rewritten in
// its place; otherwise it is left alone, though compliance judges it, and its
// violation stays. The base is rewritten, never walked again: ISO 32000-1,
// 8.6.6.3 forbids an /Indexed space as the base of another, so a chain of them
// is malformed and would otherwise cycle through indirect arrays.
func (w *deviceNWalk) colorSpace(ctx context.Context, entry ast.PdfObject) rewritten {
	if w.samples {
		arr := liveArray(ctx, w.alloc, w.doc, entry)
		if name, _ := document.ResolveName(ctx, w.doc, firstOf(arr)); name == "Indexed" && len(arr) >= 2 {
			newBase, ok := w.space(ctx, arr[1])
			if !ok {
				return unchanged(entry)
			}
			base := w.land(arr[1], newBase, 1)
			if !base.changed {
				return unchanged(entry)
			}
			newArr := slices.Clone(arr)
			newArr[1] = base.value
			return w.land(entry, newArr, base.pending)
		}
	}
	newArr, ok := w.space(ctx, entry)
	if !ok {
		return unchanged(entry)
	}
	return w.land(entry, newArr, 1)
}

// firstOf returns the first element of arr, or nil when it is empty.
func firstOf(arr ast.PdfArray) ast.PdfObject {
	if len(arr) == 0 {
		return nil
	}
	return arr[0]
}

// shading rewrites the /ColorSpace of one shading, a dictionary or, for types
// 4 to 7, a stream (ISO 32000-1, 8.7.4.5).
func (w *deviceNWalk) shading(ctx context.Context, entry ast.PdfObject) rewritten {
	if !w.firstVisit(entry) {
		return unchanged(entry)
	}
	obj, isStream := w.dictOrStream(ctx, entry)
	dict := dictOf(obj)
	cs, ok := dict["ColorSpace"]
	if !ok {
		return unchanged(entry)
	}
	t := w.colorSpace(ctx, cs)
	if !t.changed {
		return unchanged(entry)
	}
	return w.rewrite(ctx, entry, obj, isStream, "ColorSpace", t)
}

// pattern walks one pattern. A shading pattern names its colour space on its
// /Shading; a tiling pattern (ISO 32000-1, 8.7.3.3) paints its cell from its
// own /Resources, walked one form level deeper. An uncoloured tiling pattern
// (PaintType 2) specifies no colour of its own and processes none of its
// colour operators, so compliance judges no space they select there, only the
// images the cell paints: it is entered only when the walk reaches image
// colour spaces.
func (w *deviceNWalk) pattern(ctx context.Context, entry ast.PdfObject, depth int) rewritten {
	if !w.firstVisit(entry) {
		return unchanged(entry)
	}
	obj, isStream := w.dictOrStream(ctx, entry)
	dict := dictOf(obj)
	if dict == nil {
		return unchanged(entry)
	}
	if !isStream {
		sh, ok := dict["Shading"]
		if !ok {
			return unchanged(entry)
		}
		t := w.shading(ctx, sh)
		if !t.changed {
			return unchanged(entry)
		}
		return w.rewrite(ctx, entry, obj, false, "Shading", t)
	}
	patternType, _ := document.ResolveInt(ctx, w.doc, dict["PatternType"])
	paintType, _ := document.ResolveInt(ctx, w.doc, dict["PaintType"])
	if patternType != tilingPatternType || (paintType == uncolouredPaintType && !w.samples) {
		return unchanged(entry)
	}
	t := w.resources(ctx, dict["Resources"], depth+1)
	if !t.changed {
		return unchanged(entry)
	}
	return w.rewrite(ctx, entry, obj, true, "Resources", t)
}

// formStream walks the /Resources of one form: a form XObject when
// requireForm is set, or an annotation appearance stream. Of any other
// XObject, only an image's colour space is rewritten, and only when the walk
// reaches what samples are laid out for (see [deviceNWalk]).
func (w *deviceNWalk) formStream(ctx context.Context, entry ast.PdfObject, depth int, requireForm bool) rewritten {
	if !w.firstVisit(entry) {
		return unchanged(entry)
	}
	obj, isStream := w.dictOrStream(ctx, entry)
	if !isStream {
		return unchanged(entry)
	}
	dict := dictOf(obj)
	if requireForm {
		subtype, _ := document.ResolveName(ctx, w.doc, dict["Subtype"])
		if subtype == "Image" && w.samples {
			return w.image(ctx, entry, obj)
		}
		if subtype != "Form" {
			return unchanged(entry)
		}
	}
	t := w.resources(ctx, dict["Resources"], depth)
	if !t.changed {
		return unchanged(entry)
	}
	return w.rewrite(ctx, entry, obj, true, "Resources", t)
}

// image rewrites the colour space of the image XObject obj that entry names.
func (w *deviceNWalk) image(ctx context.Context, entry, obj ast.PdfObject) rewritten {
	cs, ok := dictOf(obj)["ColorSpace"]
	if !ok {
		return unchanged(entry)
	}
	t := w.colorSpace(ctx, cs)
	if !t.changed {
		return unchanged(entry)
	}
	return w.rewrite(ctx, entry, obj, true, "ColorSpace", t)
}

// type3Font walks the /Resources of one Type 3 font, which its glyph
// procedures paint from (ISO 32000-1, 9.6.5), one form level deeper. Any other
// font is left alone.
func (w *deviceNWalk) type3Font(ctx context.Context, entry ast.PdfObject, depth int) rewritten {
	if !w.firstVisit(entry) {
		return unchanged(entry)
	}
	obj, isStream := w.dictOrStream(ctx, entry)
	dict := dictOf(obj)
	if isStream || dict == nil {
		return unchanged(entry)
	}
	if subtype, _ := document.ResolveName(ctx, w.doc, dict["Subtype"]); subtype != "Type3" {
		return unchanged(entry)
	}
	t := w.resources(ctx, dict["Resources"], depth)
	if !t.changed {
		return unchanged(entry)
	}
	return w.rewrite(ctx, entry, obj, false, "Resources", t)
}

// rewrite replaces key in the dictionary or stream obj that entry holds with
// the changed child t and lands the copy. A stream keeps its raw body; one
// whose body cannot be read is left as it is rather than written empty.
func (w *deviceNWalk) rewrite(ctx context.Context, entry, obj ast.PdfObject, isStream bool, key ast.PdfName, t rewritten) rewritten {
	nd := maps.Clone(dictOf(obj))
	nd[key] = t.value
	if !isStream {
		return w.land(entry, nd, t.pending)
	}
	ref, isRef := entry.(ast.PdfIndirectRef)
	if !isRef || ref.ObjectNum <= 0 {
		return unchanged(entry)
	}
	stream, _ := obj.(ast.PdfStream)
	raw, err := stream.Bytes(ctx)
	if err != nil {
		w.doc.Logger.Debug("remediation: skipping DeviceN rewrite, stream body read failed",
			slog.Int("obj", ref.ObjectNum), slog.Any("err", err))
		return unchanged(entry)
	}
	return w.land(entry, ast.NewStream(nd, raw), t.pending)
}

// dictOrStream returns the dictionary or stream entry names, read through the
// allocator, and whether it is a stream. It returns nil for anything else.
func (w *deviceNWalk) dictOrStream(ctx context.Context, entry ast.PdfObject) (ast.PdfObject, bool) {
	obj := entry
	if ref, ok := entry.(ast.PdfIndirectRef); ok && ref.ObjectNum > 0 {
		pending, isPending := w.alloc.Pending()[ref.ObjectNum]
		if !isPending {
			resolved, err := w.doc.Resolve(ctx, ref)
			if err != nil {
				return nil, false
			}
			pending = resolved
		}
		obj = pending
	}
	switch v := obj.(type) {
	case ast.PdfStream:
		return v, v.Dict != nil
	case ast.PdfDict:
		return v, false
	}
	return nil, false
}

// dictOf returns the dictionary of a dictionary or stream, or nil.
func dictOf(obj ast.PdfObject) ast.PdfDict {
	switch v := obj.(type) {
	case ast.PdfStream:
		return v.Dict
	case ast.PdfDict:
		return v
	}
	return nil
}
