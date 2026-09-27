package remediation

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// matchPageResources matches ISO 19005-2/4 §6.2.2 test 2 violations
// where a page's content stream references resources but the page dict
// lacks an explicit /Resources entry.
func matchPageResources(v compliance.Violation) bool {
	if !clausePrefix(v, "6.2.2") {
		return false
	}
	// The detector reports the inherited case as "resolves %q only through
	// inherited Resources/...". Matching only the description's wording left
	// the fixer idle on exactly the documents it exists to repair: a page with
	// no /Resources of its own, which then hid every resource from the other
	// fixers too.
	return contextContains(v, "only through inherited Resources") ||
		contextContains(v, "no explicitly associated Resources dictionary") ||
		contextContains(v, "not in Resources")
}

// fixPageResources materialises an explicit /Resources on every page that
// states none. ISO 19005-2/4 §6.2.2 test 2 forbids reliance on inheritance:
// each page must declare its own Resources. A page stating none gets the entry
// its nearest ancestor in the page tree states, whole ([resourcesHolder]), so
// it keeps resolving exactly the names it resolved through inheritance (ISO
// 32000-1, 7.7.3.4). It gets an empty dictionary when that entry is no
// dictionary or no node states one: the page then resolves no name either
// way, and ISO 32000-1, Table 30 requires /Resources of a page.
//
// A page stating /Resources null states none (ISO 32000-1, 7.3.7), so it gets
// the entry it inherits too. A page stating any other value keeps it: a
// reference to an undefined object is the page's own entry, as veraPDF reads
// it (see [statesResources]). A page written directly in its parent's /Kids,
// against ISO 32000-1, Table 29, has no object number to write at, and is
// written back into that array ([writeTreeNode]).
//
// # Corpus evidence
//
//   - litho parentreach_2b.pdf: four pages whose /Parent names the root were
//     given the root's /Resources, so their PDF/A-2b outputs passed litho and
//     veraPDF with every name the pages draw undefined, and their PDF/A-4
//     outputs failed 6.2.2 t3 on both. Given the /Resources of the node
//     listing each, repaired, the outputs pass both tools at PDF/A-2a, 2b,
//     2u, 3b and 4, default and aggressive.
func fixPageResources(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}
	pagesObj, ok := catalogDict["Pages"]
	if !ok {
		return nil, nil
	}

	var pageEntries []annotPageEntry
	collectPageRefs(ctx, alloc, doc, pagesObj, &pageEntries, make(map[int]bool), 0)

	count := 0
	for _, pe := range pageEntries {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		pe.dict = liveTreeNode(ctx, alloc, doc, pe)
		if statesResources(pe.dict) {
			continue
		}
		var inherited ast.PdfObject = ast.PdfDict{}
		if holder, ok := resourcesHolder(ctx, alloc, doc, pe); ok {
			inherited = holder.dict["Resources"]
		}
		newPage := make(ast.PdfDict, len(pe.dict)+1)
		maps.Copy(newPage, pe.dict)
		newPage["Resources"] = inherited
		if writeTreeNode(ctx, alloc, doc, pe, newPage) {
			count++
		}
	}
	if count == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("materialised /Resources on %d page(s)", count)}, nil
}

// statesResources reports whether the page tree node dict states /Resources:
// holds the entry with a value other than null, which ISO 32000-1, 7.3.7 makes
// the same as an absent entry. Any other value states one, a reference to an
// undefined object included, as veraPDF's COSObject.knownKey reads the key and
// compliance with it (knownKey, pageObject.inheritedHolder).
func statesResources(dict ast.PdfDict) bool {
	v, ok := dict["Resources"]
	if !ok || v == nil {
		return false
	}
	_, isNull := v.(ast.PdfNull)
	return !isNull
}

// resourcesNode returns the node of the page tree whose /Resources entry the
// page takes: the page itself when it states one ([statesResources]), else
// its nearest ancestor on the page tree walk that does (ISO 32000-1, 7.7.3.4
// and Table 30). ok is false when no node states one.
//
// It is the inheritance compliance judges (pageObject.inheritedHolder) and the
// mutations write (document.Page.InheritedResources): the ancestors are the
// nodes whose /Kids the walk came through ([annotPageEntry.parent]), which
// veraPDF's PDPageTreeBranch writes over a child's /Parent, so a page whose
// /Parent names another node inherits from the node listing it. The first
// node stating the entry ends the search whatever its value, so a page stating
// a reference to no object inherits nothing.
//
// Every node is read again through the allocator ([liveTreeNode]), so the
// node carries what an earlier fixer, or this one on an earlier page, wrote to
// it, and a page an earlier fixer gave /Resources of its own takes them. An
// entry built outside the walk carries no ancestor, and resolves only what the
// page states.
func resourcesNode(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pe annotPageEntry) (annotPageEntry, bool) {
	for node := &pe; node != nil; node = node.parent {
		live := *node
		live.dict = liveTreeNode(ctx, alloc, doc, *node)
		if statesResources(live.dict) {
			return live, true
		}
	}
	return annotPageEntry{}, false
}

// liveTreeNode returns the dictionary of the page tree node the walk reached
// as node, as the output will carry it: at its object number, read through the
// allocator, or, for a node written directly in its parent's /Kids (against
// ISO 32000-1, Table 29), at its place in that array, read through the
// parent's. It falls back to the dictionary the walk read when neither holds
// one any more.
func liveTreeNode(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, node annotPageEntry) ast.PdfDict {
	if node.ref.ObjectNum > 0 {
		if d := resolveFromAllocOrDoc(ctx, alloc, doc, node.ref); d != nil {
			return d
		}
		return node.dict
	}
	if node.parent == nil {
		return node.dict
	}
	kids := liveArray(ctx, alloc, doc, liveTreeNode(ctx, alloc, doc, *node.parent)["Kids"])
	if node.kid < 0 || node.kid >= len(kids) {
		return node.dict
	}
	if d, ok := kids[node.kid].(ast.PdfDict); ok {
		return d
	}
	return node.dict
}

// writeTreeNode queues dict as the dictionary of the page tree node the walk
// reached as node, and reports whether the write landed. A node with an object
// number is written there. One written directly in its parent's /Kids, which
// no object number addresses, is written back into that array: into the array
// at its own object number when it has one, or else into the parent, which is
// written the same way. The parent is read through the allocator, so the write
// keeps what was queued in it before, a sibling written back the same way
// among them.
//
// ISO 32000-1, Table 29 makes each element of /Kids an indirect reference, so
// a direct node breaks it, and an earlier write was refused for want of an
// object number, dropping the repair. It fails when the array no longer holds
// a direct dictionary at the node's place, which the walk found there.
func writeTreeNode(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, node annotPageEntry, dict ast.PdfDict) bool {
	if node.ref.ObjectNum > 0 {
		return alloc.AllocAt(node.ref.ObjectNum, dict)
	}
	if node.parent == nil {
		return false
	}
	parent := liveTreeNode(ctx, alloc, doc, *node.parent)
	kidsObj := parent["Kids"]
	kids := liveArray(ctx, alloc, doc, kidsObj)
	if node.kid < 0 || node.kid >= len(kids) {
		return false
	}
	if _, direct := kids[node.kid].(ast.PdfDict); !direct {
		return false
	}
	newKids := slices.Clone(kids)
	newKids[node.kid] = dict
	if ref, ok := kidsObj.(ast.PdfIndirectRef); ok && ref.ObjectNum > 0 {
		return alloc.AllocAt(ref.ObjectNum, newKids)
	}
	newParent := maps.Clone(parent)
	newParent["Kids"] = newKids
	return writeTreeNode(ctx, alloc, doc, *node.parent, newParent)
}

// resourcesHolder returns the node of the page tree whose /Resources the page's
// content resolves its names against ([resourcesNode]), and false when that
// entry is no dictionary or no node states one: the page then resolves no name
// at all, and a repair has nothing to reach.
//
// A fixer that repairs the resources hands the holder where it would hand the
// page, and writes an inline /Resources it rebuilt back into the holder: the
// repair then reaches every page inheriting the dictionary, and never shadows
// it with a copy on one page.
//
// # Corpus evidence
//
//   - All corpora: 26 pages, on 16 files, state no /Resources and inherit a
//     dictionary. On three files they inherit a colour space a colour repair
//     reads, a DeviceN in pdfjs issue17065.pdf and litho parentreach_2b.pdf
//     and Separations that agree with each other in litho resinherit_2b.pdf,
//     and on none an ICCBased space.
//   - All corpora: 7 pages have a /Parent naming another node than the one
//     listing them. The one in litho resinherit_2b.pdf and the four in litho
//     parentreach_2b.pdf inherit, and the two in pdfjs filled-background.pdf
//     state /Resources of their own.
//   - litho parentreach_2b.pdf: graphics, devicen, aggressive-limits and
//     marked-content-lang repaired the root's /Resources in place of the
//     node's, so PDF/A-1b outputs kept 6.2.8 t1 and 6.1.12 t9 in both modes,
//     and PDF/UA-1 and PDF/UA-2 outputs the /Lang (7.2 t29, 8.4.4 t2), on
//     litho and veraPDF. Each repair now lands, and only the default PDF/A-1b
//     output keeps 6.1.12 t9, which the aggressive remediation alone trims.
//   - pdfjs issue17065.pdf: devicen completed the DeviceN its page inherits
//     only in a second round, once page-resources had pinned the dictionary
//     on the page, at PDF/A-2b and PDF/A-4, default and aggressive. One round
//     now does.
func resourcesHolder(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pe annotPageEntry) (annotPageEntry, bool) {
	holder, ok := resourcesNode(ctx, alloc, doc, pe)
	if !ok || liveDictValue(ctx, alloc, doc, holder.dict["Resources"]) == nil {
		return annotPageEntry{}, false
	}
	return holder, true
}

// pageResourceCategory returns the category dictionary, /Font or /XObject
// among them, of the /Resources the page of pe resolves its names against
// ([pageResourcesFor]), read through the allocator, or nil when there is none.
//
// A fixer repairing what a page's content names reads it here, not in the
// /Resources the parser merges key by key with every ancestor's
// ([document.Page]): that merge names a resource of another node's dictionary
// the page cannot reach (ISO 32000-1, 7.7.3.4 hands a page its ancestor's
// dictionary whole), and misses what an earlier fixer queued in the one it
// does.
//
// # Corpus evidence
//
//   - All corpora: on 74 files, 63 of them verapdf, 7 govdocs, 3 litho and 1
//     pdfjs, the merged /Resources of a page name a resource the dictionary
//     it resolves its names against does not, mostly an ancestor's font
//     beside the page's own /Resources, and on none the other way round.
//     Remediated at PDF/A-1b, PDF/A-2b, PDF/A-4 and PDF/UA-1, default and
//     aggressive, with the 31 files whose pages inherit /Resources, state none
//     that resolve, or sit directly in /Kids, only litho resinherit_2b.pdf and
//     the two files holding a page written directly in /Kids change output,
//     and no finding moves on litho or veraPDF.
//   - litho resinherit_2b.pdf: at PDF/UA-1 the fonts repair gave a /ToUnicode
//     to the three fonts no page resolves, and now gives none; the findings
//     of litho and veraPDF on the outputs are the same either way.
func pageResourceCategory(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pe annotPageEntry, category ast.PdfName) ast.PdfDict {
	obj, ok := pageResourcesFor(ctx, alloc, doc, pe)[category]
	if !ok {
		return nil
	}
	return liveDictValue(ctx, alloc, doc, obj)
}

// pageTreeEntries returns the entry the page tree walk ([collectPageRefs])
// makes for each of pages, the pages [document.ResolvePages] returns, at the
// page's index. A fixer that walks those pages takes a page's entry from it
// ([pageTreeEntry]) to resolve what the page inherits through the node listing
// it ([resourcesNode]), and to write back the node holding what it repairs
// ([writeTreeNode]).
//
// Both walks descend /Kids in order, so they reach the same pages in the same
// order, and each page takes the entry at its own index, a page written
// directly in its parent's /Kids against ISO 32000-1, Table 29 included. Where
// they part, on a damaged tree one of them prunes differently, a page takes
// the entry of its object number, and one the walk does not reach by an object
// number carries no ancestor: it resolves only the /Resources it states.
//
// # Corpus evidence
//
//   - All corpora: two pages are written directly in /Kids, in pdfjs
//     issue9105_other.pdf, whose catalog is written in the trailer, and
//     issue9540.pdf. Both walks reach each at the same index, so it inherits
//     through the node listing it.
//   - pdfjs issue18986.pdf, poppler-742-0-fuzzed.pdf and
//     poppler-937-0-fuzzed.pdf: the page tree walk reaches no page where the
//     parser reaches one, the only files where the walks part.
//   - litho kidsrepair_1b.pdf: two sibling pages written directly in one
//     /Kids, one inheriting its node's /Resources and one stating its own, kept
//     6.2.2 t2 and 6.2.5 t1 after a PDF/A-2 or PDF/A-4 remediation, and 6.4 and
//     6.2.8 t1 after a PDF/A-1b one, on litho and veraPDF. Written back into the
//     array, every output passes both tools.
func pageTreeEntries(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pages []*document.Page) []annotPageEntry {
	var catalog ast.PdfDict
	if catalogRef, ok := catalogRefForFixer(ctx, doc); ok {
		catalog = resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	} else if parsed, err := document.ResolveCatalog(ctx, doc); err == nil {
		// A catalog written directly in the trailer has no object number,
		// and its page tree is walked all the same.
		catalog = parsed.Dict
	}
	var walked []annotPageEntry
	if pagesObj, ok := catalog["Pages"]; ok {
		collectPageRefs(ctx, alloc, doc, pagesObj, &walked, make(map[int]bool), 0)
	}
	if sameWalk(walked, pages) {
		return walked
	}
	byObj := make(map[int]annotPageEntry, len(walked))
	for _, pe := range walked {
		if pe.ref.ObjectNum > 0 {
			byObj[pe.ref.ObjectNum] = pe
		}
	}
	entries := make([]annotPageEntry, len(pages))
	for i, page := range pages {
		pe, ok := byObj[page.Ref.ObjectNum]
		if !ok {
			pe = annotPageEntry{ref: page.Ref, dict: page.Dict}
		}
		entries[i] = pe
	}
	return entries
}

// sameWalk reports whether walked, the entries of the page tree walk, lists
// the pages of pages in the same order: the same object number at every
// index, 0 for a page written directly in /Kids.
func sameWalk(walked []annotPageEntry, pages []*document.Page) bool {
	if len(walked) != len(pages) {
		return false
	}
	for i, page := range pages {
		if walked[i].ref.ObjectNum != page.Ref.ObjectNum {
			return false
		}
	}
	return true
}

// pageTreeEntry returns pe, an entry of [pageTreeEntries], with the page's
// dictionary read through the allocator ([liveTreeNode]), so a page an earlier
// fixer rewrote is read as it now stands.
func pageTreeEntry(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pe annotPageEntry) annotPageEntry {
	pe.dict = liveTreeNode(ctx, alloc, doc, pe)
	return pe
}
