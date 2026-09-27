package remediation

import (
	"context"
	"fmt"
	"maps"

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
// it (see [statesResources]).
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
		if pe.ref.ObjectNum <= 0 || statesResources(pe.dict) {
			continue
		}
		var inherited ast.PdfObject = ast.PdfDict{}
		if holder, ok := resourcesHolder(ctx, alloc, doc, pe); ok {
			inherited = holder.dict["Resources"]
		}
		newPage := make(ast.PdfDict, len(pe.dict)+1)
		maps.Copy(newPage, pe.dict)
		newPage["Resources"] = inherited
		if alloc.AllocAt(pe.ref.ObjectNum, newPage) {
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
// Every node with an object number is read again through the allocator, so the
// node carries what an earlier fixer, or this one on an earlier page, wrote to
// it, and a page an earlier fixer gave /Resources of its own takes them. An
// entry built outside the walk carries no ancestor, and resolves only what the
// page states.
func resourcesNode(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pe annotPageEntry) (annotPageEntry, bool) {
	for node := &pe; node != nil; node = node.parent {
		live := *node
		if live.ref.ObjectNum > 0 {
			if d := resolveFromAllocOrDoc(ctx, alloc, doc, live.ref); d != nil {
				live.dict = d
			}
		}
		if statesResources(live.dict) {
			return live, true
		}
	}
	return annotPageEntry{}, false
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

// pageTreeEntries returns the entry the page tree walk ([collectPageRefs])
// makes for every page it reaches that has an object number, keyed by it. A
// fixer that walks [document.ResolvePages] takes a page's entry from it
// ([pageTreeEntry]) to resolve what the page inherits through the node listing
// it ([resourcesNode]).
func pageTreeEntries(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) map[int]annotPageEntry {
	entries := make(map[int]annotPageEntry)
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return entries
	}
	pagesObj, ok := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)["Pages"]
	if !ok {
		return entries
	}
	var pages []annotPageEntry
	collectPageRefs(ctx, alloc, doc, pagesObj, &pages, make(map[int]bool), 0)
	for _, pe := range pages {
		if pe.ref.ObjectNum > 0 {
			entries[pe.ref.ObjectNum] = pe
		}
	}
	return entries
}

// pageTreeEntry returns the entry of page in entries ([pageTreeEntries]), with
// the page's dictionary read through the allocator. A page the walk does not
// reach by an object number, one written directly in its parent's /Kids
// against ISO 32000-1, Table 29, carries no ancestor, so it resolves only the
// /Resources it states.
//
// # Corpus evidence
//
//   - pdfjs issue9105_other.pdf: its one page is written directly in /Kids and
//     inherits /Resources, the only such page, and its PDF/A-1b and PDF/A-2b
//     outputs, default and aggressive, are the same bytes either way.
func pageTreeEntry(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, entries map[int]annotPageEntry, page *document.Page) annotPageEntry {
	pe := entries[page.Ref.ObjectNum]
	pe.ref = page.Ref
	pe.dict = livePageDict(ctx, alloc, doc, page)
	return pe
}
