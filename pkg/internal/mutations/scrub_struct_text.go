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
// describing it. When the entry does not name an element for an identifier,
// because the page has no entry, the entry does not resolve, or its slot is
// null or past its end, the elements are found from the other side: the /K
// entries of the tree itself (see [pageStructElements]). The element is still in
// the tree whatever its /ParentTree entry says, and a viewer reaches it there.
//
// It refuses rather than guess:
//
//   - An element describing both removed and surviving content reports
//     [ErrStructElementPartlyRemoved], whether because one MCID covers operators
//     on both sides or because two MCIDs name the same element.
//   - An element written directly into the array, or into its parent's /K,
//     reports [ErrStructElementNotAddressable], having no object number to
//     replace.
//
// An element that does not resolve is passed over: the rewrite writes null in
// its place (see [FullRewrite]), so nothing of it reaches the output.
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

	elements, err := pageStructElements(ctx, alloc, doc, page)
	if err != nil {
		return err
	}
	if elements == nil {
		// An untagged document: there is no second copy of its text to clear.
		return nil
	}

	// Sorted, so a refusal names the same MCID every run.
	mcids := make([]int, 0, len(removed))
	for mcid := range removed {
		mcids = append(mcids, mcid)
	}
	sort.Ints(mcids)

	for _, mcid := range mcids {
		named := elements.of(ctx, mcid)
		if len(named) == 0 {
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

		for _, elemObj := range named {
			ref, isRef := elemObj.(ast.PdfIndirectRef)
			if !isRef {
				return fmt.Errorf("%w: MCID %d", ErrStructElementNotAddressable, mcid)
			}
			if other, shared := elements.sharedWithSurvivor(ctx, ref, mcid, surviving); shared {
				return fmt.Errorf("%w: MCID %d and MCID %d name element %d",
					ErrStructElementPartlyRemoved, mcid, other, ref.ObjectNum)
			}

			elem, resolveErr := resolveDictAllocFirst(ctx, alloc, doc, ref)
			if resolveErr != nil || elem == nil {
				// Written as null by the rewrite, which is what makes passing
				// over it safe; the output scan still reads every element
				// the output does hold.
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
	}
	return ctx.Err()
}

// structElements answers which structure elements describe a marked-content
// identifier of one page.
type structElements struct {
	alloc *writer.ObjAllocator
	doc   *document.Document
	root  ast.PdfDict
	page  *document.Page

	// entries is the page's /ParentTree array, nil when it has none that
	// resolves.
	entries ast.PdfArray
	// byTree maps each identifier to the elements whose /K names it, read
	// from the tree once, the first time an identifier needs it.
	byTree map[int][]ast.PdfObject
}

// of returns the elements describing mcid: the one the /ParentTree entry names,
// or, when it names none, those the tree's /K entries assign it.
func (s *structElements) of(ctx context.Context, mcid int) []ast.PdfObject {
	if mcid >= 0 && mcid < len(s.entries) {
		if _, isNull := s.entries[mcid].(ast.PdfNull); !isNull && s.entries[mcid] != nil {
			return []ast.PdfObject{s.entries[mcid]}
		}
	}
	if s.byTree == nil {
		s.byTree = structElementsByMCID(ctx, s.alloc, s.doc, s.root, s.page)
	}
	return s.byTree[mcid]
}

// sharedWithSurvivor reports whether any surviving marked content names the
// same element, and which. One element commonly spans a whole paragraph, so
// this is the ordinary case rather than a corner one.
func (s *structElements) sharedWithSurvivor(ctx context.Context, ref ast.PdfIndirectRef, mcid int, surviving map[int]bool) (int, bool) {
	others := make([]int, 0, len(surviving))
	for other := range surviving {
		if other != mcid {
			others = append(others, other)
		}
	}
	// Sorted, so the refusal names the same MCID every run.
	sort.Ints(others)
	for _, other := range others {
		for _, elemObj := range s.of(ctx, other) {
			otherRef, ok := elemObj.(ast.PdfIndirectRef)
			if ok && otherRef.ObjectNum == ref.ObjectNum {
				return other, true
			}
		}
	}
	return 0, false
}

// pageStructElements returns what finds the structure elements of the page's
// marked content, or nil when the document has no structure tree litho can
// read.
//
// An unresolvable /StructTreeRoot is null (ISO 32000-1, 7.3.10), so there is
// no tree, and the rewrite writes none: nothing holds a copy to clear.
func pageStructElements(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, page *document.Page) (*structElements, error) {
	catalogRef, ok := CatalogRef(ctx, doc)
	if !ok {
		return nil, nil
	}
	catalog, err := LiveCatalogDict(ctx, alloc, doc, catalogRef)
	if err != nil || catalog == nil {
		return nil, nil //nolint:nilerr // No readable catalog holds no structure text to scrub.
	}
	root, err := resolveDictAllocFirst(ctx, alloc, doc, catalog["StructTreeRoot"])
	if err != nil || root == nil {
		return nil, nil //nolint:nilerr // An unresolvable /StructTreeRoot is null (ISO 32000-1, 7.3.10): no structure text to scrub.
	}

	entries, err := pageParentTreeEntries(ctx, doc, page, root)
	if err != nil {
		return nil, err
	}
	return &structElements{alloc: alloc, doc: doc, root: root, page: page, entries: entries}, nil
}

// pageParentTreeEntries returns the /ParentTree array for this page, indexed by
// marked-content identifier, or nil when the page has none that resolves.
func pageParentTreeEntries(ctx context.Context, doc *document.Document, page *document.Page, root ast.PdfDict) (ast.PdfArray, error) {
	spObj, ok := page.Dict["StructParents"]
	if !ok {
		return nil, nil
	}
	key, err := document.ResolveInt(ctx, doc, spObj)
	if err != nil {
		return nil, fmt.Errorf("resolving /StructParents: %w", err)
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
		// An unresolvable entry is null (ISO 32000-1, 7.3.10). It names no
		// element, and the tree's /K entries are read instead.
		return nil, nil //nolint:nilerr // A deliberate fallback, see above.
	}
	return entries, nil
}

// structElementsByMCID maps each marked-content identifier the structure tree
// assigns to page to the elements whose /K names it.
//
// A content item names its page through the /Pg of its marked-content
// reference or of its element (ISO 32000-1, 14.7.4.2 and Table 324). Table 323
// does not make /Pg inheritable, but an element that states none describes the
// content of its nearest ancestor's page in practice, and that is the page
// assumed here. An identifier whose page nothing states is taken to be this
// page's: assigning it here can only clear an element's text or refuse, while
// leaving it out could leave a copy behind. A marked-content reference with a
// /Stm names content in a form XObject rather than on the page, and is passed
// over, as are object references.
//
// The walk reads each element and each /K array once, and stops at
// [maxStructTreeDepth]. An element that does not resolve is passed over: the
// rewrite writes null in its place, and with it every element only it reaches.
func structElementsByMCID(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, root ast.PdfDict, page *document.Page) map[int][]ast.PdfObject {
	out := make(map[int][]ast.PdfObject)
	visited := make(map[int]bool)
	pageNum := page.Ref.ObjectNum

	add := func(mcid int, owner ast.PdfObject) {
		for _, known := range out[mcid] {
			if sameStructElement(known, owner) {
				return
			}
		}
		out[mcid] = append(out[mcid], owner)
	}
	onPage := func(pg ast.PdfObject, inherited int) (int, bool) {
		if ref, isRef := pg.(ast.PdfIndirectRef); isRef {
			return ref.ObjectNum, ref.ObjectNum == pageNum
		}
		return inherited, inherited == 0 || inherited == pageNum
	}

	var walk func(kObj, owner ast.PdfObject, pg, depth int)
	walk = func(kObj, owner ast.PdfObject, pg, depth int) {
		if kObj == nil || depth > maxStructTreeDepth || ctx.Err() != nil {
			return
		}
		for _, kid := range structKidListOnce(ctx, alloc, doc, kObj, visited) {
			if mcid, isInt := kid.(ast.PdfInt); isInt {
				if _, here := onPage(nil, pg); here && owner != nil {
					add(int(mcid), owner)
				}
				continue
			}
			if document.VisitedBefore(kid, visited) {
				continue
			}
			dict, err := resolveDictAllocFirst(ctx, alloc, doc, kid)
			if err != nil || dict == nil {
				continue
			}
			switch kind, _ := dict["Type"].(ast.PdfName); kind {
			case "MCR":
				if _, inForm := dict["Stm"]; inForm {
					continue
				}
				mcid, ok := intEntry(ctx, doc, dict["MCID"])
				if _, here := onPage(dict["Pg"], pg); ok && here && owner != nil {
					add(mcid, owner)
				}
			case "OBJR":
			default:
				elemPg, _ := onPage(dict["Pg"], pg)
				walk(dict["K"], kid, elemPg, depth+1)
			}
		}
	}
	walk(root["K"], nil, 0, 0)
	return out
}

// sameStructElement reports whether a and b name the same element: the same
// object number for references, and the same value otherwise.
func sameStructElement(a, b ast.PdfObject) bool {
	ra, aRef := a.(ast.PdfIndirectRef)
	rb, bRef := b.(ast.PdfIndirectRef)
	if aRef || bRef {
		return aRef && bRef && ra.ObjectNum == rb.ObjectNum
	}
	return false
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
