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

// fixPageResources materialises an explicit /Resources dict on every
// page that inherits one from /Pages. ISO 19005-2/4 §6.2.2 test 2
// forbids reliance on inheritance: each page must declare its own
// Resources. The copy walks up the /Parent chain once, finds the first
// defined /Resources, and pins it to the page dict directly.
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
		if _, ok := pe.dict["Resources"]; ok {
			continue
		}
		inherited := findInheritedResources(ctx, alloc, doc, pe.dict)
		newPage := make(ast.PdfDict, len(pe.dict)+1)
		maps.Copy(newPage, pe.dict)
		if inherited == nil {
			newPage["Resources"] = ast.PdfDict{}
		} else {
			newPage["Resources"] = inherited
		}
		if pe.ref.ObjectNum > 0 {
			alloc.AllocAt(pe.ref.ObjectNum, newPage)
			count++
		}
	}
	if count == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("materialised /Resources on %d page(s)", count)}, nil
}

// resourcesHolder returns the node of the page tree whose /Resources the page's
// content resolves its names against: the page itself when its /Resources is
// a dictionary, else the nearest ancestor along /Parent whose /Resources is one
// (ISO 32000-1, 7.7.3.4 and Table 30). ok is false when no node holds one.
//
// A fixer that repairs the resources hands the holder where it would hand the
// page, and writes an inline /Resources it rebuilt back into the holder: the
// repair then reaches every page inheriting the dictionary, and never shadows
// it with a copy on one page. Every node is read through the allocator, so the
// holder carries what an earlier fixer wrote to it, and a page an earlier
// fixer gave /Resources of its own holds them.
//
// Compliance (pageObject.inheritedHolder) stops at the nearest node that
// states /Resources at all and follows the node the page tree walk came
// through, so the two differ only on a /Resources that is not a dictionary or
// a /Parent that names another node. The walk stops at a /Parent cycle and at
// maxPageTreeDepth.
//
// # Corpus evidence
//
//   - All corpora: 19 pages, on 12 files, hold no /Resources dictionary of
//     their own and inherit one. On two files they inherit a colour space a
//     colour repair reads, a DeviceN in pdfjs issue17065.pdf and Separations
//     that agree with each other in litho resinherit_2b.pdf, and on none an
//     ICCBased space.
//   - pdfjs issue17065.pdf: devicen completed the DeviceN its page inherits
//     only in a second round, once page-resources had pinned the dictionary
//     on the page, at PDF/A-2b and PDF/A-4, default and aggressive. One round
//     now does.
func resourcesHolder(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pe annotPageEntry) (annotPageEntry, bool) {
	holder := pe
	seen := make(map[int]bool)
	for depth := 0; liveDictValue(ctx, alloc, doc, holder.dict["Resources"]) == nil; depth++ {
		parentRef, ok := holder.dict["Parent"].(ast.PdfIndirectRef)
		if !ok || parentRef.ObjectNum <= 0 || seen[parentRef.ObjectNum] || depth >= maxPageTreeDepth {
			return annotPageEntry{}, false
		}
		seen[parentRef.ObjectNum] = true
		parent := resolveFromAllocOrDoc(ctx, alloc, doc, parentRef)
		if parent == nil {
			return annotPageEntry{}, false
		}
		holder = annotPageEntry{ref: parentRef, dict: parent}
	}
	return holder, true
}

// findInheritedResources walks /Parent upward and returns the first
// /Resources dict it finds. Returns nil when the chain ends without one.
//
// The bound reads maxPageTreeDepth, which is named for the /Kids DESCENT in
// fixer_annotations.go, while this walk ascends /Parent. That is a name
// mismatch, not a wrong limit: a page tree is a handful of levels deep in
// either direction, so the same number bounds both. The loop also terminates
// on the first node without a /Parent, and a cyclic chain is capped rather than
// detected, which is why the bound is what stops it.
//
// # Corpus evidence
//
//   - All corpora: the deepest page tree is 4 levels.
func findInheritedResources(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pageDict ast.PdfDict) ast.PdfObject {
	current := pageDict
	for range maxPageTreeDepth {
		parentObj, ok := current["Parent"]
		if !ok {
			return nil
		}
		ref, isRef := parentObj.(ast.PdfIndirectRef)
		if !isRef || ref.ObjectNum == 0 {
			return nil
		}
		parentDict := resolveFromAllocOrDoc(ctx, alloc, doc, ref)
		if parentDict == nil {
			if d, err := document.ResolveDict(ctx, doc, parentObj); err == nil {
				parentDict = d
			}
		}
		if parentDict == nil {
			return nil
		}
		if res, ok := parentDict["Resources"]; ok {
			return res
		}
		current = parentDict
	}
	return nil
}
