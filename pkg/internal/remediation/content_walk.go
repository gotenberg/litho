package remediation

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// contentRepair repairs the decoded bytes of one content stream for
// [walkContentStreams]. It returns the bytes to write in their place and a
// function the walk calls once the write lands, which records what the repair
// changed. A nil function leaves the stream as it is.
type contentRepair func(ctx context.Context, data []byte) ([]byte, func())

// streamRepair is a [contentRepair] that is also told whether the stream
// starts with the colour operators it holds ignored (see
// [compliance.ColourState]).
type streamRepair func(ctx context.Context, data []byte, colourOff bool) ([]byte, func())

// walkContentStreams repairs with repair every content stream the pages of doc
// draw, as [contentStreamWalk] reaches them, and returns how many it rewrote.
// fixer names the caller in the log of a write that cannot land.
func walkContentStreams(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, fixer string, repair contentRepair) (int, error) {
	return walkStreams(ctx, alloc, doc, fixer, nil, func(ctx context.Context, data []byte, _ bool) ([]byte, func()) {
		return repair(ctx, data)
	})
}

// resourceRepair is a [contentRepair] that may also name new image XObjects
// in the resources the stream's names resolve in, through res. The walk writes
// what res gathered before the stream, and drops it when the stream's write
// does not land.
type resourceRepair func(ctx context.Context, data []byte, res *resourceEdit) ([]byte, func())

// walkContentStreamsWithResources is [walkContentStreams] with a repair that
// may add image XObjects to the resources of the stream it repairs.
func walkContentStreamsWithResources(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, fixer string, repair resourceRepair) (int, error) {
	w := &contentStreamWalk{alloc: alloc, doc: doc, fixer: fixer, resRepair: repair, visited: make(map[int]bool)}
	return w.run(ctx)
}

// walkStreams is [walkContentStreams] with a repair told which streams start
// with their colour operators off: the ones colourOff holds by object number,
// and the cell of every uncoloured tiling pattern (ISO 32000-1, 8.7.3.3),
// drawn or not.
func walkStreams(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, fixer string, colourOff map[int]bool, repair streamRepair) (int, error) {
	w := &contentStreamWalk{alloc: alloc, doc: doc, fixer: fixer, repair: repair, colourOff: colourOff, visited: make(map[int]bool)}
	return w.run(ctx)
}

// run walks every page of the document and returns how many streams it
// rewrote.
func (w *contentStreamWalk) run(ctx context.Context) (int, error) {
	alloc, doc := w.alloc, w.doc
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return 0, fmt.Errorf("resolving catalog: %w", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return 0, fmt.Errorf("resolving pages: %w", err)
	}

	for i, pe := range pageTreeEntries(ctx, alloc, doc, pages) {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		w.page(ctx, pe, i+1)
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return w.streams, nil
}

// contentStreamWalk carries the state of one [walkContentStreams] run over the
// content streams a document's pages draw: a page's own content, and the form
// XObjects, tiling patterns, Type 3 glyphs and annotation appearances it draws.
//
// The compliance walk that states the content-stream tests for those streams
// is unexported in its package and reads the parsed document, while a repair
// must read every object through the allocator, so this walk mirrors it rather
// than calling it. It reaches a superset of what compliance judges: every form
// XObject, tiling pattern and Type 3 font a resource dictionary names, drawn or
// not, and the ones they name in turn, which veraPDF judges when drawn and
// compliance does not reach. A repair keeps what a viewer draws, so a stream no
// page draws loses nothing by it.
//
// visited holds the object number of every stream, resource dictionary and
// font already walked, so a shared object is repaired once and a cyclic
// /Resources chain cannot loop. The nesting below the page is bounded by the
// document's form-nesting budget, the bound compliance applies to the forms a
// page draws.
type contentStreamWalk struct {
	alloc  *writer.ObjAllocator
	doc    *document.Document
	fixer  string
	repair streamRepair
	// resRepair, when set, repairs in place of repair, and may add image
	// XObjects to the stream's resources ([walkContentStreamsWithResources]).
	resRepair resourceRepair
	// colourOff holds the object numbers of the streams that start with their
	// colour operators off (see [walkStreams]).
	colourOff map[int]bool
	visited   map[int]bool
	// streams counts the content streams rewritten.
	streams int
}

// firstVisit reports whether obj is an inline value or an indirect object the
// walk has not reached yet, marking it.
func (w *contentStreamWalk) firstVisit(obj ast.PdfObject) bool {
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

// page repairs one page's own content, then the streams its resources and its
// annotations reach. The page and its resources are read alloc-first, so a
// page an earlier fixer rewrote is walked as it now stands. The resources are
// the ones the page resolves its names against, its own or the ones it
// inherits through the page tree ([resourcesHolder]), so a stream the page
// can name is walked and one only another node's dictionary names is not.
func (w *contentStreamWalk) page(ctx context.Context, pe annotPageEntry, pageNum int) {
	pe = pageTreeEntry(ctx, w.alloc, w.doc, pe)
	var owner resourceOwner
	if holder, ok := resourcesHolder(ctx, w.alloc, w.doc, pe); ok {
		owner = w.nodeOwner(ctx, holder)
	}
	w.pageContent(ctx, pe.dict, pageNum, owner)
	if holder, ok := resourcesHolder(ctx, w.alloc, w.doc, pe); ok {
		w.resources(ctx, holder.dict["Resources"], 0)
	}
	w.annotations(ctx, pe.dict)
}

// nodeOwner returns the owner of the /Resources a page tree node states, read
// and written through the allocator, so a page's content names its images in
// the dictionary the page resolves its names in, its own or the one it
// inherits whole (ISO 32000-1, 7.7.3.4).
func (w *contentStreamWalk) nodeOwner(ctx context.Context, node annotPageEntry) resourceOwner {
	return resourceOwner{
		load: func() ast.PdfObject {
			return liveTreeNode(ctx, w.alloc, w.doc, node)["Resources"]
		},
		store: func(res ast.PdfDict) bool {
			dict := maps.Clone(liveTreeNode(ctx, w.alloc, w.doc, node))
			dict["Resources"] = res
			return writeTreeNode(ctx, w.alloc, w.doc, node, dict)
		},
	}
}

// repairWith runs the walk's repair over one stream's data: resRepair with an
// edit of the resources owner describes, or repair. The edit is nil when the
// walk runs repair.
func (w *contentStreamWalk) repairWith(ctx context.Context, data []byte, colourOff bool, owner resourceOwner) ([]byte, func(), *resourceEdit) {
	if w.resRepair == nil {
		newData, landed := w.repair(ctx, data, colourOff)
		return newData, landed, nil
	}
	edit := newResourceEdit(w.alloc, w.doc, owner)
	newData, landed := w.resRepair(ctx, data, edit)
	if landed == nil {
		edit.abandon()
	}
	return newData, landed, edit
}

// pageContent repairs a page's own content, which ISO 32000-1, 7.7.3.3 makes
// one stream however many parts its /Contents array holds, and writes it back
// through [replaceContentStream].
//
// A page the repair cannot write back is left as it was and logged, its
// findings left as residuals, rather than failing the fixer: a fixer's error
// aborts the whole remediation.
func (w *contentStreamWalk) pageContent(ctx context.Context, pageDict ast.PdfDict, pageNum int, owner resourceOwner) {
	contentsObj, ok := pageDict["Contents"]
	if !ok {
		return
	}
	data, err := liveContentBytes(ctx, w.alloc, w.doc, contentsObj)
	if err != nil || len(data) == 0 {
		return
	}
	newData, landed, edit := w.repairWith(ctx, data, false, owner)
	if landed == nil {
		return
	}
	if !edit.commit(ctx) {
		edit.abandon()
		w.doc.Logger.Debug("remediation: cannot name a page's new images in its resources",
			"fixer", w.fixer, "page", pageNum)
		return
	}
	if err := replaceContentStream(ctx, w.alloc, w.doc, contentsObj, newData, pageNum); err != nil {
		edit.abandon()
		w.doc.Logger.Debug("remediation: cannot rewrite a page's content stream",
			"fixer", w.fixer, "page", pageNum, "error", err)
		return
	}
	landed()
	w.streams++
}

// resources walks one resource dictionary: the form XObjects, tiling patterns
// and Type 3 fonts it names, each in sorted name order.
func (w *contentStreamWalk) resources(ctx context.Context, obj ast.PdfObject, depth int) {
	if ctx.Err() != nil || depth > w.doc.Limits().MaxFormDepth || !w.firstVisit(obj) {
		return
	}
	res := liveDictValue(ctx, w.alloc, w.doc, obj)
	if res == nil {
		return
	}
	for _, entry := range w.entries(ctx, res["XObject"]) {
		if dict, ok := w.stream(ctx, entry, isFormXObject, resourceOwner{}); ok {
			w.resources(ctx, dict["Resources"], depth+1)
		}
	}
	for _, entry := range w.entries(ctx, res["Pattern"]) {
		if dict, ok := w.stream(ctx, entry, isTilingPattern, resourceOwner{}); ok {
			w.resources(ctx, dict["Resources"], depth+1)
		}
	}
	for _, entry := range w.entries(ctx, res["Font"]) {
		w.type3Font(ctx, entry, depth+1)
	}
}

// entries returns the values of one resource category dictionary in sorted
// name order, or nil when obj is no dictionary.
func (w *contentStreamWalk) entries(ctx context.Context, obj ast.PdfObject) []ast.PdfObject {
	if obj == nil {
		return nil
	}
	d := liveDictValue(ctx, w.alloc, w.doc, obj)
	out := make([]ast.PdfObject, 0, len(d))
	for _, name := range slices.Sorted(maps.Keys(d)) {
		out = append(out, d[name])
	}
	return out
}

// type3Font repairs every glyph of a Type 3 font (ISO 32000-1, 9.6.5), each a
// content stream of its own, and walks the font's /Resources. compliance
// judges every glyph of a font a page shows text with, so the glyphs are
// repaired whether or not a string selects them.
func (w *contentStreamWalk) type3Font(ctx context.Context, entry ast.PdfObject, depth int) {
	if ctx.Err() != nil || !w.firstVisit(entry) {
		return
	}
	font := liveDictValue(ctx, w.alloc, w.doc, entry)
	if subtype, _ := document.ResolveName(ctx, w.doc, font["Subtype"]); subtype != "Type3" {
		return
	}
	owner := w.fontOwner(ctx, entry)
	for _, glyph := range w.entries(ctx, font["CharProcs"]) {
		w.stream(ctx, glyph, nil, owner)
	}
	w.resources(ctx, font["Resources"], depth)
}

// fontOwner returns the owner of a Type 3 font's /Resources, in which its
// glyph descriptions resolve their names (ISO 32000-1, 9.6.5), read and
// written through the allocator. A font with no /Resources of its own leaves
// its glyphs the page's, so it has none to load; one written directly in a
// font dictionary, with no object number, has none to store.
func (w *contentStreamWalk) fontOwner(ctx context.Context, entry ast.PdfObject) resourceOwner {
	owner := resourceOwner{
		load: func() ast.PdfObject {
			return liveDictValue(ctx, w.alloc, w.doc, entry)["Resources"]
		},
	}
	if ref, ok := entry.(ast.PdfIndirectRef); ok && ref.ObjectNum > 0 {
		owner.store = func(res ast.PdfDict) bool {
			font := maps.Clone(liveDictValue(ctx, w.alloc, w.doc, entry))
			font["Resources"] = res
			return w.alloc.AllocAt(ref.ObjectNum, font)
		}
	}
	return owner
}

// annotations repairs the appearance streams of a page's annotations, under
// /N, /R and /D, each a stream or a subdictionary of appearance states (ISO
// 32000-1, 12.5.5), and walks their resources.
func (w *contentStreamWalk) annotations(ctx context.Context, pageDict ast.PdfDict) {
	for _, annotObj := range liveArray(ctx, w.alloc, w.doc, pageDict["Annots"]) {
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
			if ref, isRef := entry.(ast.PdfIndirectRef); isRef {
				if _, isStream := liveStream(ctx, w.alloc, w.doc, ref); isStream {
					w.appearance(ctx, entry)
					continue
				}
			}
			for _, state := range w.entries(ctx, entry) {
				w.appearance(ctx, state)
			}
		}
	}
}

// appearance repairs one appearance stream, a form XObject (ISO 32000-1,
// 12.5.5), and walks its resources one level below the page.
func (w *contentStreamWalk) appearance(ctx context.Context, entry ast.PdfObject) {
	if dict, ok := w.stream(ctx, entry, nil, resourceOwner{}); ok {
		w.resources(ctx, dict["Resources"], 1)
	}
}

// stream repairs the content stream entry names, when it is one the walk has
// not reached yet and keep, when set, accepts its dictionary. It returns the
// stream's live dictionary, for the walk to descend into its /Resources, and
// whether it was reached. owner says where the stream's names resolve when
// the stream does not resolve them in its own /Resources, as a Type 3 glyph
// does not; its zero value names the stream's own.
//
// The stream is read alloc-first, so the write builds on what an earlier fixer
// queued at that object number: transparency, for one, writes these same forms,
// patterns, glyphs and appearances ahead of the content fixers. The write
// replaces the body with decoded bytes ([replaceDecodedStream]), so the stream
// stays a stream with every other key of its dictionary. A stream whose body
// cannot be decoded is walked below but not rewritten.
func (w *contentStreamWalk) stream(ctx context.Context, entry ast.PdfObject, keep func(context.Context, *document.Document, ast.PdfDict) bool, owner resourceOwner) (ast.PdfDict, bool) {
	// A stream is always an indirect object (ISO 32000-1, 7.3.8.1), so a
	// direct value names none.
	ref, isRef := entry.(ast.PdfIndirectRef)
	if !isRef || ref.ObjectNum <= 0 || ctx.Err() != nil || w.visited[ref.ObjectNum] {
		return nil, false
	}
	stream, ok := liveStream(ctx, w.alloc, w.doc, ref)
	if !ok || (keep != nil && !keep(ctx, w.doc, stream.Dict)) {
		return nil, false
	}
	w.visited[ref.ObjectNum] = true

	data, err := w.doc.DecodeStream(ctx, stream)
	if err != nil {
		w.doc.Logger.Debug("remediation: cannot decode a content stream to repair it",
			"fixer", w.fixer, "obj", ref.ObjectNum, "error", err)
		return stream.Dict, true
	}
	off := w.colourOff[ref.ObjectNum] || isUncolouredPattern(ctx, w.doc, stream.Dict)
	dict := stream.Dict
	if owner.load == nil {
		owner = resourceOwner{
			load: func() ast.PdfObject { return dict["Resources"] },
			store: func(res ast.PdfDict) bool {
				dict = maps.Clone(dict)
				dict["Resources"] = res
				return true
			},
		}
	}
	newData, landed, edit := w.repairWith(ctx, data, off, owner)
	if landed == nil {
		return stream.Dict, true
	}
	if edit.commit(ctx) && replaceDecodedStream(w.alloc, ref, dict, newData) {
		landed()
		w.streams++
		return stream.Dict, true
	}
	edit.abandon()
	return stream.Dict, true
}

// isFormXObject reports whether an XObject dictionary is a form (ISO 32000-1,
// 8.10.2), whose body is a content stream, rather than an image, whose body is
// samples the walk must not decode.
func isFormXObject(ctx context.Context, doc *document.Document, dict ast.PdfDict) bool {
	subtype, _ := document.ResolveName(ctx, doc, dict["Subtype"])
	return subtype == "Form"
}

// isTilingPattern reports whether a pattern dictionary is a tiling pattern
// (ISO 32000-1, 8.7.3.1), whose cell is a content stream. A shading pattern is
// a dictionary with no content of its own.
func isTilingPattern(ctx context.Context, doc *document.Document, dict ast.PdfDict) bool {
	patternType, _ := document.ResolveInt(ctx, doc, dict["PatternType"])
	return patternType == tilingPatternType
}

// isUncolouredPattern reports whether a stream dictionary is an uncoloured
// tiling pattern (ISO 32000-1, 8.7.3.3), whose /PaintType is 2.
func isUncolouredPattern(ctx context.Context, doc *document.Document, dict ast.PdfDict) bool {
	if !isTilingPattern(ctx, doc, dict) {
		return false
	}
	// A /PaintType that resolves to no integer makes no uncoloured pattern,
	// so its error only leaves the cell coloured.
	paintType, _ := document.ResolveInt(ctx, doc, dict["PaintType"])
	return paintType == uncolouredPaintType
}
