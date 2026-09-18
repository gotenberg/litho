package mutations

import (
	"context"
	"fmt"
	"maps"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// structTextKeys are the structure element entries that hold a copy of the text
// its marked content draws (ISO 32000-1, Table 323): /ActualText stands in for
// the content when text is extracted, /Alt describes it, and /E expands an
// abbreviation. All three survive a page edit untouched.
var structTextKeys = []ast.PdfName{"ActualText", "Alt", "E"}

// scrubStructTreeText clears the text the structure tree holds for marked
// content a redaction removed from the page.
//
// This is the first vector past the page itself. A tagged PDF keeps a parallel
// copy of its text in the structure tree, and tagged PDFs are the common case,
// so a redaction that stops at the content stream leaves the words sitting in
// /ActualText for any extractor to read back.
//
// The page's /StructParents keys an entry in the structure tree's /ParentTree,
// and for a page that entry is an array indexed by marked-content identifier
// (ISO 32000-1, 14.7.4.4), which is what turns a removed MCID into the element
// describing it.
//
// It refuses rather than guess:
//
//   - An element describing both removed and surviving content reports
//     [ErrStructElementPartlyRemoved], whether because one MCID covers operators
//     on both sides or because two MCIDs name the same element.
//   - An element written directly into the array reports
//     [ErrStructElementNotAddressable], having no object number to replace.
func scrubStructTreeText(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	page *document.Page,
	removed map[int]bool,
	surviving map[int]bool,
) error {
	if len(removed) == 0 {
		return nil
	}

	entries, err := pageParentTreeEntries(ctx, doc, page)
	if err != nil {
		return err
	}
	if entries == nil {
		// An untagged page, or one that names no /ParentTree entry: there is no
		// second copy of its text to clear.
		return nil
	}

	// Sorted, so a refusal names the same MCID every run.
	mcids := make([]int, 0, len(removed))
	for mcid := range removed {
		mcids = append(mcids, mcid)
	}
	sort.Ints(mcids)

	for _, mcid := range mcids {
		if mcid < 0 || mcid >= len(entries) {
			// The tree does not describe this marked content, so it holds no
			// copy of it either.
			continue
		}
		// The same MCID can cover operators that went and operators that
		// stayed: the element describes both, so neither clearing nor keeping
		// it is right.
		if surviving[mcid] {
			return fmt.Errorf("%w: MCID %d", ErrStructElementPartlyRemoved, mcid)
		}

		ref, isRef := entries[mcid].(ast.PdfIndirectRef)
		if !isRef {
			return fmt.Errorf("%w: MCID %d", ErrStructElementNotAddressable, mcid)
		}
		if other, shared := sharedWithSurvivor(entries, ref, mcid, surviving); shared {
			return fmt.Errorf("%w: MCID %d and MCID %d name element %d",
				ErrStructElementPartlyRemoved, mcid, other, ref.ObjectNum)
		}

		elem, resolveErr := document.ResolveDict(ctx, doc, ref)
		if resolveErr != nil || elem == nil {
			// An element litho cannot read is one it cannot prove holds a copy.
			continue
		}
		scrubbed, changed := withoutStructText(elem)
		if !changed {
			continue
		}
		if !alloc.AllocAt(ref.ObjectNum, scrubbed) {
			return fmt.Errorf("%w: MCID %d", ErrStructElementNotAddressable, mcid)
		}
	}
	return nil
}

// pageParentTreeEntries returns the /ParentTree array for this page, indexed by
// marked-content identifier, or nil when the page has no entry.
func pageParentTreeEntries(ctx context.Context, doc *document.Document, page *document.Page) (ast.PdfArray, error) {
	spObj, ok := page.Dict["StructParents"]
	if !ok {
		return nil, nil
	}
	key, err := document.ResolveInt(ctx, doc, spObj)
	if err != nil {
		return nil, fmt.Errorf("resolving /StructParents: %w", err)
	}

	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil || catalog == nil {
		return nil, nil
	}
	root, err := document.ResolveDict(ctx, doc, catalog.Dict["StructTreeRoot"])
	if err != nil || root == nil {
		return nil, nil
	}
	parentTree, ok := root["ParentTree"]
	if !ok {
		return nil, nil
	}

	value, err := document.LookupNumberTree(ctx, doc, parentTree, int(key))
	if err != nil {
		return nil, fmt.Errorf("looking up /ParentTree entry %d: %w", int(key), err)
	}
	entries, err := document.ResolveArray(ctx, doc, value)
	if err != nil {
		return nil, nil
	}
	return entries, nil
}

// sharedWithSurvivor reports whether any surviving marked content names the
// same element, and which. One element commonly spans a whole paragraph, so
// this is the ordinary case rather than a corner one.
func sharedWithSurvivor(entries ast.PdfArray, ref ast.PdfIndirectRef, mcid int, surviving map[int]bool) (int, bool) {
	for other := range entries {
		if other == mcid || !surviving[other] {
			continue
		}
		otherRef, ok := entries[other].(ast.PdfIndirectRef)
		if ok && otherRef.ObjectNum == ref.ObjectNum {
			return other, true
		}
	}
	return 0, false
}

// withoutStructText returns a copy of elem with the text-bearing entries gone,
// and whether it held any.
func withoutStructText(elem ast.PdfDict) (ast.PdfDict, bool) {
	scrubbed := make(ast.PdfDict, len(elem))
	maps.Copy(scrubbed, elem)

	changed := false
	for _, key := range structTextKeys {
		if _, present := scrubbed[key]; present {
			delete(scrubbed, key)
			changed = true
		}
	}
	return scrubbed, changed
}
