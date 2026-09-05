package mutations

import (
	"context"
	"log/slog"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// maxDestTreeDepth caps /Names/Dests recursion in [pruneDestNameTree].
// Anti-DoS guard, not spec-mandated: real trees nest two or three levels;
// 64 mirrors maxAttachmentTreeDepth and maxNameTreeDepth in
// internal/core/document.
const maxDestTreeDepth = 64

// maxOutlineDepth caps the outline walk in [pruneOutlineItems]. Anti-DoS guard,
// not spec-mandated: a real outline nests a handful of levels, and a crafted
// /First or /Next cycle must not hang the writer. The walk also tracks visited
// items, so this bounds nesting rather than repetition.
const maxOutlineDepth = 64

// maxDestChainDepth caps how far [destTargetsRemoved] follows a destination
// through indirect references and /D indirection. Anti-DoS guard, not
// spec-mandated: ISO 32000-1, 12.3.2.2 allows one level of each, so 8 leaves
// ample room while bounding a reference cycle.
const maxDestChainDepth = 8

// removedPageSet returns the object numbers of the pages a rebuild drops.
//
// A page kept at one index and dropped at another is not removed: a split may
// repeat a page, and the object still belongs to the output.
func removedPageSet(allPages []*document.Page, keptIndices []int) map[int]bool {
	keptNums := make(map[int]bool, len(keptIndices))
	for _, idx := range keptIndices {
		keptNums[allPages[idx].Ref.ObjectNum] = true
	}
	removed := make(map[int]bool)
	for _, page := range allPages {
		if keptNums[page.Ref.ObjectNum] {
			continue
		}
		removed[page.Ref.ObjectNum] = true
	}
	return removed
}

// unselectedPages returns the object numbers of the source pages an operation
// does not take, the shape [removedPageSet] gives when the selection is a slice
// of pages rather than a slice of indices.
func unselectedPages(allPages, selected []*document.Page) map[int]bool {
	keptNums := make(map[int]bool, len(selected))
	for _, page := range selected {
		keptNums[page.Ref.ObjectNum] = true
	}
	removed := make(map[int]bool)
	for _, page := range allPages {
		if !keptNums[page.Ref.ObjectNum] {
			removed[page.Ref.ObjectNum] = true
		}
	}
	return removed
}

// destTargetsRemoved reports whether dest is an explicit destination whose page
// reference names a page the rebuild dropped.
//
// Named and byte-string destinations resolve through the tables this file
// prunes, so they need no separate check: an entry that survives points at a
// page that survives.
func destTargetsRemoved(ctx context.Context, doc *document.Document, dest ast.PdfObject, removed map[int]bool, depth int) bool {
	if depth > maxDestChainDepth || dest == nil {
		return false
	}
	switch d := dest.(type) {
	case ast.PdfIndirectRef:
		obj, err := document.ResolveObject(ctx, doc, d)
		if err != nil {
			return false
		}
		return destTargetsRemoved(ctx, doc, obj, removed, depth+1)
	case ast.PdfDict:
		return destTargetsRemoved(ctx, doc, d["D"], removed, depth+1)
	case ast.PdfArray:
		if len(d) == 0 {
			return false
		}
		// ISO 32000-1, 12.3.2.2: the first element of an explicit destination is
		// an indirect reference to the page, and the reference follows the page
		// object, so a destination that survives keeps naming what it named.
		//
		// The integer form names a zero-based page number, and the reader
		// accepts it for this document too (see
		// [document.ResolveDestinationPage]) because producers write it that
		// way. It cannot be checked here: this pass knows which page objects
		// went, not which index each one held, so an integer destination is
		// left alone. It is rare enough to leave alone (one file across the
		// 5163-file corpora) and the page it names is still a page; remapping
		// the index would need the original page order threaded through every
		// caller of this pass.
		ref, isRef := d[0].(ast.PdfIndirectRef)
		return isRef && removed[ref.ObjectNum]
	}
	return false
}

// annotTargetsRemoved reports whether an annotation's destination, given either
// directly as /Dest or through a /GoTo action, names a removed page.
func annotTargetsRemoved(ctx context.Context, doc *document.Document, annot ast.PdfDict, removed map[int]bool) bool {
	if destTargetsRemoved(ctx, doc, annot["Dest"], removed, 0) {
		return true
	}
	action, err := document.ResolveDict(ctx, doc, annot["A"])
	if err != nil || action == nil {
		return false
	}
	if subtype, _ := action["S"].(ast.PdfName); subtype != "GoTo" {
		return false
	}
	return destTargetsRemoved(ctx, doc, action["D"], removed, 0)
}

// pruneLinkAnnots clears the destinations on a kept page that name a removed
// page, rewriting /Annots in pageDict.
//
// The removed page keeps the annotation as its own audience: an /Annots entry
// is a reachable reference, so leaving it behind writes the deleted page, and
// its content stream, back into the output.
//
// A link whose destination is gone has nothing left to do and is dropped, which
// is what qpdf and pdfcpu do with one. Any other annotation is kept and only
// loses the destination: a widget is a form control the page still paints (ISO
// 32000-1, 12.5.6.19), and deleting it over a stale /A would take a field off a
// page the caller asked to keep.
func pruneLinkAnnots(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pageDict ast.PdfDict, removed map[int]bool) {
	annotsObj, ok := pageDict["Annots"]
	if !ok {
		return
	}
	arr, err := document.ResolveArray(ctx, doc, annotsObj)
	if err != nil {
		doc.Logger.Debug("mutations: resolving /Annots while pruning destinations", slog.Any("err", err))
		return
	}

	kept := make(ast.PdfArray, 0, len(arr))
	changed := false
	for _, entry := range arr {
		annot, resolveErr := document.ResolveDict(ctx, doc, entry)
		if resolveErr != nil || annot == nil || !annotTargetsRemoved(ctx, doc, annot, removed) {
			kept = append(kept, entry)
			continue
		}
		changed = true
		if subtype, _ := annot["Subtype"].(ast.PdfName); subtype == "Link" {
			continue
		}
		kept = append(kept, stripAnnotDestination(alloc, entry, annot))
	}
	if !changed {
		return
	}

	if ref, isRef := annotsObj.(ast.PdfIndirectRef); isRef {
		alloc.AllocAt(ref.ObjectNum, kept)
		return
	}
	pageDict["Annots"] = kept
}

// stripAnnotDestination returns the annotation without the /Dest and /A that
// named a removed page, queueing the rewrite when the annotation is indirect.
func stripAnnotDestination(alloc *writer.ObjAllocator, entry ast.PdfObject, annot ast.PdfDict) ast.PdfObject {
	newAnnot := make(ast.PdfDict, len(annot))
	maps.Copy(newAnnot, annot)
	delete(newAnnot, "Dest")
	delete(newAnnot, "A")

	if ref, isRef := entry.(ast.PdfIndirectRef); isRef {
		alloc.AllocAt(ref.ObjectNum, newAnnot)
		return entry
	}
	return newAnnot
}

// droppedPageAnnots returns the annotations that live only on pages the
// operation removes.
//
// An annotation listed by both a kept and a removed page belongs to the output,
// so it is not dropped.
func droppedPageAnnots(ctx context.Context, doc *document.Document, allPages []*document.Page, removed map[int]bool) map[int]bool {
	kept := make(map[int]bool)
	dropped := make(map[int]bool)
	for _, page := range allPages {
		target := kept
		if removed[page.Ref.ObjectNum] {
			target = dropped
		}
		annotsObj, hasAnnots := page.Dict["Annots"]
		if !hasAnnots {
			continue
		}
		arr, err := document.ResolveArray(ctx, doc, annotsObj)
		if err != nil {
			continue
		}
		for _, entry := range arr {
			if ref, isRef := entry.(ast.PdfIndirectRef); isRef {
				target[ref.ObjectNum] = true
			}
		}
	}
	for num := range kept {
		delete(dropped, num)
	}
	return dropped
}

// fieldKidOverrides walks the interactive form and filters every field's /Kids
// down to the widgets that survive, returning the patched fields by source
// object number. The catalog's /AcroForm /Fields array is keyed by the catalog's
// own object number when it needs filtering too.
//
// A field groups the widgets that present it on each page (ISO 32000-1,
// 12.7.3.1), and every widget names its page in /P. Keeping a widget whose page
// is gone therefore keeps the page: extracting nine pages of govdocs
// thread_100_100852.pdf dragged 47 unextracted pages and 510 KB of their content
// into the output, reachable as
// /Pages/Kids[0]/Annots[0]/Parent/Kids[4]/P. Only the dead widgets go; the field
// itself stays, because it may carry the value the form was filled with.
func fieldKidOverrides(
	ctx context.Context,
	doc *document.Document,
	catalogDict ast.PdfDict,
	droppedAnnots map[int]bool,
) map[int]ast.PdfObject {
	overrides := make(map[int]ast.PdfObject)
	if len(droppedAnnots) == 0 {
		return overrides
	}
	acroForm, err := document.ResolveDict(ctx, doc, catalogDict["AcroForm"])
	if err != nil || acroForm == nil {
		return overrides
	}
	fields, err := document.ResolveArray(ctx, doc, acroForm["Fields"])
	if err != nil {
		return overrides
	}

	kept := make(ast.PdfArray, 0, len(fields))
	rootsChanged := false
	for _, entry := range fields {
		if ref, isRef := entry.(ast.PdfIndirectRef); isRef && droppedAnnots[ref.ObjectNum] {
			// A field merged with its only widget (ISO 32000-1, 12.5.6.19):
			// the page is gone, so the field goes with it.
			rootsChanged = true
			continue
		}
		pruneFieldKids(ctx, doc, entry, droppedAnnots, overrides, 0)
		kept = append(kept, entry)
	}
	if rootsChanged {
		newForm := make(ast.PdfDict, len(acroForm))
		maps.Copy(newForm, acroForm)
		newForm["Fields"] = kept
		if ref, isRef := catalogDict["AcroForm"].(ast.PdfIndirectRef); isRef {
			overrides[ref.ObjectNum] = newForm
		}
	}
	return overrides
}

// pruneFieldKids filters one field's /Kids and recurses into the survivors.
func pruneFieldKids(
	ctx context.Context,
	doc *document.Document,
	fieldObj ast.PdfObject,
	droppedAnnots map[int]bool,
	overrides map[int]ast.PdfObject,
	depth int,
) {
	if depth > maxFieldParentDepth {
		return
	}
	ref, isRef := fieldObj.(ast.PdfIndirectRef)
	if !isRef {
		return
	}
	field, err := document.ResolveDict(ctx, doc, fieldObj)
	if err != nil || field == nil {
		return
	}
	kidsObj, hasKids := field["Kids"]
	if !hasKids {
		return
	}
	kids, err := document.ResolveArray(ctx, doc, kidsObj)
	if err != nil {
		return
	}

	kept := make(ast.PdfArray, 0, len(kids))
	for _, kid := range kids {
		if kidRef, kidIsRef := kid.(ast.PdfIndirectRef); kidIsRef && droppedAnnots[kidRef.ObjectNum] {
			continue
		}
		pruneFieldKids(ctx, doc, kid, droppedAnnots, overrides, depth+1)
		kept = append(kept, kid)
	}
	if len(kept) == len(kids) {
		return
	}

	if arrRef, arrIsRef := kidsObj.(ast.PdfIndirectRef); arrIsRef {
		overrides[arrRef.ObjectNum] = kept
		return
	}
	newField := make(ast.PdfDict, len(field))
	maps.Copy(newField, field)
	newField["Kids"] = kept
	overrides[ref.ObjectNum] = newField
}

// splitDestOverrides returns pre-patched source objects for [writer.Copier], so
// a split never follows a destination into a page it leaves out.
//
// Pruning after the copy would be too late. The copier walks references, so a
// link naming an unextracted page pulls that page, and its content stream, into
// the output: extracting nine pages of govdocs thread_100_100852.pdf carried
// seventeen unextracted ones along, and the text of page 26 was recoverable
// from a file that was supposed to hold pages 2 to 10.
// It adds to overrides rather than replacing it, so a page already patched
// there (by [parentlessPageOverrides]) keeps that edit.
func splitDestOverrides(ctx context.Context, doc *document.Document, keptPages []*document.Page, removed map[int]bool, overrides map[int]ast.PdfObject) {
	for _, page := range keptPages {
		pageDict := page.Dict
		if patched, alreadyPatched := overrides[page.Ref.ObjectNum].(ast.PdfDict); alreadyPatched {
			pageDict = patched
		}
		annotsObj, hasAnnots := pageDict["Annots"]
		if !hasAnnots {
			continue
		}
		arr, err := document.ResolveArray(ctx, doc, annotsObj)
		if err != nil {
			doc.Logger.Debug("mutations: resolving /Annots for split overrides", slog.Any("err", err))
			continue
		}

		kept := make(ast.PdfArray, 0, len(arr))
		changed := false
		for _, entry := range arr {
			annot, resolveErr := document.ResolveDict(ctx, doc, entry)
			if resolveErr != nil || annot == nil || !annotTargetsRemoved(ctx, doc, annot, removed) {
				kept = append(kept, entry)
				continue
			}
			changed = true
			if subtype, _ := annot["Subtype"].(ast.PdfName); subtype == "Link" {
				continue
			}
			stripped := make(ast.PdfDict, len(annot))
			maps.Copy(stripped, annot)
			delete(stripped, "Dest")
			delete(stripped, "A")
			if ref, isRef := entry.(ast.PdfIndirectRef); isRef {
				overrides[ref.ObjectNum] = stripped
				kept = append(kept, entry)
				continue
			}
			kept = append(kept, stripped)
		}
		if !changed {
			continue
		}

		if ref, isRef := annotsObj.(ast.PdfIndirectRef); isRef {
			overrides[ref.ObjectNum] = kept
			continue
		}
		newPage := make(ast.PdfDict, len(pageDict))
		maps.Copy(newPage, pageDict)
		newPage["Annots"] = kept
		overrides[page.Ref.ObjectNum] = newPage
	}
}

// pruneOutlineDestinations strips the destinations of outline items that name a
// removed page, walking the whole tree.
//
// The item itself stays. Its /Dest and /A are both optional (ISO 32000-1, Table
// 153), so a bookmark that keeps its title and its place in the tree is valid,
// while unlinking it would mean repairing /First, /Last, /Next, /Prev and /Count
// around the hole. The destination has to go regardless: it reaches the deleted
// page, which is enough to write that page into the output, as govdocs
// thread_000_000761.pdf did with 2196 bytes of it.
func pruneOutlineDestinations(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	catalogDict ast.PdfDict,
	removed map[int]bool,
) {
	root, err := document.ResolveDict(ctx, doc, catalogDict["Outlines"])
	if err != nil || root == nil {
		return
	}
	visited := make(map[int]bool)
	pruneOutlineItems(ctx, alloc, doc, root["First"], removed, visited, 0)
}

// pruneOutlineItems walks one sibling chain and recurses into each item's
// children.
func pruneOutlineItems(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	itemObj ast.PdfObject,
	removed map[int]bool,
	visited map[int]bool,
	depth int,
) {
	if depth > maxOutlineDepth {
		return
	}
	for current := itemObj; current != nil; {
		ref, isRef := current.(ast.PdfIndirectRef)
		if !isRef || visited[ref.ObjectNum] {
			return
		}
		visited[ref.ObjectNum] = true

		item, err := document.ResolveDict(ctx, doc, current)
		if err != nil || item == nil {
			return
		}
		if outlineTargetsRemoved(ctx, doc, item, removed) {
			newItem := make(ast.PdfDict, len(item))
			maps.Copy(newItem, item)
			delete(newItem, "Dest")
			delete(newItem, "A")
			alloc.AllocAt(ref.ObjectNum, newItem)
		}
		pruneOutlineItems(ctx, alloc, doc, item["First"], removed, visited, depth+1)
		current = item["Next"]
	}
}

// outlineTargetsRemoved reports whether an outline item's destination, given
// either as /Dest or through a /GoTo action, names a removed page.
func outlineTargetsRemoved(ctx context.Context, doc *document.Document, item ast.PdfDict, removed map[int]bool) bool {
	if destTargetsRemoved(ctx, doc, item["Dest"], removed, 0) {
		return true
	}
	action, err := document.ResolveDict(ctx, doc, item["A"])
	if err != nil || action == nil {
		return false
	}
	if subtype, _ := action["S"].(ast.PdfName); subtype != "GoTo" {
		return false
	}
	return destTargetsRemoved(ctx, doc, action["D"], removed, 0)
}

// pruneDestsDict filters the catalog's /Dests dictionary, the PDF 1.1 form of
// the named destination table (ISO 32000-1, 12.3.2.3).
func pruneDestsDict(ctx context.Context, doc *document.Document, dests ast.PdfDict, removed map[int]bool) (ast.PdfDict, bool) {
	kept := make(ast.PdfDict, len(dests))
	for name, dest := range dests {
		if destTargetsRemoved(ctx, doc, dest, removed, 0) {
			continue
		}
		kept[name] = dest
	}
	return kept, len(kept) != len(dests)
}

// nameTreeDropFunc decides whether one name-tree value goes. It may rewrite
// objects the value reaches, which is how a Web Capture content set keeps its
// entry while losing the pages it named.
type nameTreeDropFunc func(value ast.PdfObject) bool

// pruneDestNameTree filters a name tree in place, returning the object to store
// in the parent and whether anything was dropped.
//
// Nodes keep their shape: only the /Names pairs change. A /Limits left wider
// than the keys that remain still contains every key below it, which is all a
// lookup needs (ISO 32000-1, 7.9.6).
func pruneDestNameTree(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, nodeObj ast.PdfObject, drop nameTreeDropFunc, depth int) (ast.PdfObject, bool) {
	if depth > maxDestTreeDepth {
		return nodeObj, false
	}

	var node ast.PdfDict
	if ref, isRef := nodeObj.(ast.PdfIndirectRef); isRef {
		node = ResolveFromAllocOrDoc(ctx, alloc, doc, ref)
	}
	if node == nil {
		node, _ = document.ResolveDict(ctx, doc, nodeObj) // Best-effort: a nil node ends the branch.
	}
	if node == nil {
		return nodeObj, false
	}

	newNode := make(ast.PdfDict, len(node))
	maps.Copy(newNode, node)
	changed := false

	if kidsObj, hasKids := node["Kids"]; hasKids {
		if kids, err := document.ResolveArray(ctx, doc, kidsObj); err == nil {
			newKids := make(ast.PdfArray, len(kids))
			for i, kid := range kids {
				newKid, kidChanged := pruneDestNameTree(ctx, alloc, doc, kid, drop, depth+1)
				newKids[i] = newKid
				changed = changed || kidChanged
			}
			if _, kidsAreDirect := kidsObj.(ast.PdfArray); kidsAreDirect {
				newNode["Kids"] = newKids
			}
		}
	}

	if namesObj, hasNames := node["Names"]; hasNames {
		if names, err := document.ResolveArray(ctx, doc, namesObj); err == nil {
			keptPairs := make(ast.PdfArray, 0, len(names))
			for i := 0; i+1 < len(names); i += 2 {
				if drop(names[i+1]) {
					continue
				}
				keptPairs = append(keptPairs, names[i], names[i+1])
			}
			if len(keptPairs) != len(names) {
				changed = true
				if ref, isRef := namesObj.(ast.PdfIndirectRef); isRef {
					alloc.AllocAt(ref.ObjectNum, keptPairs)
				} else {
					newNode["Names"] = keptPairs
				}
			}
		}
	}

	if !changed {
		return nodeObj, false
	}
	if ref, isRef := nodeObj.(ast.PdfIndirectRef); isRef {
		alloc.AllocAt(ref.ObjectNum, newNode)
		return nodeObj, true
	}
	return newNode, true
}

// pruneCatalogDestinations drops the named destinations that point at pages a
// rebuild removed, rewriting the catalog when either table changes.
func pruneCatalogDestinations(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	catalogRef ast.PdfIndirectRef,
	catalogDict ast.PdfDict,
	removed map[int]bool,
) {
	newCatalog := make(ast.PdfDict, len(catalogDict))
	maps.Copy(newCatalog, catalogDict)
	changed := false

	// /OpenAction names the view the document opens on (ISO 32000-1, 12.3.2 and
	// Table 28), as a destination or a /GoTo action. Left pointing at a deleted
	// page it opens the file on a page that is not there, and the reference keeps
	// that page in the output: govdocs thread_400_400912.pdf held on to 17204
	// bytes of the page it had just deleted.
	if openAction, hasOpen := catalogDict["OpenAction"]; hasOpen {
		targets := destTargetsRemoved(ctx, doc, openAction, removed, 0)
		if !targets {
			if action, err := document.ResolveDict(ctx, doc, openAction); err == nil && action != nil {
				if subtype, _ := action["S"].(ast.PdfName); subtype == "GoTo" {
					targets = destTargetsRemoved(ctx, doc, action["D"], removed, 0)
				}
			}
		}
		if targets {
			delete(newCatalog, "OpenAction")
			changed = true
		}
	}

	if destsObj, hasDests := catalogDict["Dests"]; hasDests {
		if dests, err := document.ResolveDict(ctx, doc, destsObj); err == nil && dests != nil {
			if kept, dropped := pruneDestsDict(ctx, doc, dests, removed); dropped {
				changed = true
				if ref, isRef := destsObj.(ast.PdfIndirectRef); isRef {
					alloc.AllocAt(ref.ObjectNum, kept)
				} else {
					newCatalog["Dests"] = kept
				}
			}
		}
	}

	if namesObj, hasNames := catalogDict["Names"]; hasNames {
		if names, err := document.ResolveDict(ctx, doc, namesObj); err == nil && names != nil {
			if destsRoot, hasDestsRoot := names["Dests"]; hasDestsRoot {
				newRoot, dropped := pruneDestNameTree(ctx, alloc, doc, destsRoot,
					func(value ast.PdfObject) bool {
						return destTargetsRemoved(ctx, doc, value, removed, 0)
					}, 0)
				if dropped {
					changed = true
					if _, rootIsRef := destsRoot.(ast.PdfIndirectRef); !rootIsRef {
						newNames := make(ast.PdfDict, len(names))
						maps.Copy(newNames, names)
						newNames["Dests"] = newRoot
						if ref, isRef := namesObj.(ast.PdfIndirectRef); isRef {
							alloc.AllocAt(ref.ObjectNum, newNames)
						} else {
							newCatalog["Names"] = newNames
						}
					}
				}
			}
		}
	}

	if changed {
		alloc.AllocAt(catalogRef.ObjectNum, newCatalog)
	}
}
