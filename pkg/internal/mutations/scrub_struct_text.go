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
// An element whose content went in part needs no rewrite of its own. A
// redaction removes show-text operators and keeps the BDC and EMC around them
// (14.6.2), so each marked-content sequence stays in the stream holding what
// survived of it, under the identifier its element's /K and the /ParentTree
// already name (14.7.4.2, 14.7.4.4): the kept text stays tagged, in its reading
// order, as ISO 14289-1, 7.1 asks, and the removed text, which the tree reads
// from the content, is gone from it. What the content does not give back is
// the text an element holds itself, /ActualText, /Alt and /E (14.9.3 to
// 14.9.5), and those describe the element's whole content, its descendants'
// included. So every element above removed content is read, the one the
// identifier names and each element of the tree above it:
//
//   - One whose whole content went loses those entries.
//   - One that also describes content that stays, on this page or another,
//     in a form XObject or an annotation, reports
//     [ErrStructElementPartlyRemoved]: clearing the entries would take the
//     description of what stayed, keeping them keeps the removed text, and no
//     part of a description can be told to be the removed part's. Entries
//     that are empty strings spell nothing, and stay (see [spellsStructText]).
//   - One written directly into the array, or into its parent's /K, reports
//     [ErrStructElementNotAddressable], having no object number to replace.
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

	scrub := &structTextScrub{
		alloc: alloc, doc: doc, pageNum: page.Ref.ObjectNum,
		removed: removed, surviving: surviving,
		uses:    make(map[int]*contentSides),
		memo:    make(map[structWalkKey]contentSides),
		walking: make(map[int]bool),
	}

	// The elements the /ParentTree names, which a reader that starts from the
	// content reaches whether or not the tree's /K agrees.
	for _, mcid := range sortedMCIDs(removed) {
		for _, elemObj := range elements.of(ctx, mcid) {
			ref, isRef := elemObj.(ast.PdfIndirectRef)
			if !isRef {
				if dict, ok := elemObj.(ast.PdfDict); ok && hasStructText(dict) {
					return fmt.Errorf("%w: MCID %d", ErrStructElementNotAddressable, mcid)
				}
				continue
			}
			use := scrub.use(ref.ObjectNum)
			use.removed = true
			use.kept = use.kept || surviving[mcid]
		}
	}
	for _, mcid := range sortedMCIDs(surviving) {
		for _, elemObj := range elements.of(ctx, mcid) {
			if ref, isRef := elemObj.(ast.PdfIndirectRef); isRef {
				// Recorded whether or not removed content names it yet: the
				// tree's /K may assign it some below.
				scrub.use(ref.ObjectNum).kept = true
			}
		}
	}

	// The tree read from its root, as a viewer reads it: each element's
	// content is the content of its subtree, so an ancestor holding
	// /ActualText for a paragraph a span of which went is found here.
	scrub.kids(ctx, elements.root["K"], 0, 0)
	if scrub.err != nil {
		return scrub.err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// Ascending, so a refusal names the same element every run.
	nums := make([]int, 0, len(scrub.uses))
	for num := range scrub.uses {
		nums = append(nums, num)
	}
	sort.Ints(nums)
	for _, num := range nums {
		use := scrub.uses[num]
		if !use.removed {
			continue
		}
		ref := ast.PdfIndirectRef{ObjectNum: num}
		elem, resolveErr := resolveDictAllocFirst(ctx, alloc, doc, ref)
		if resolveErr != nil || elem == nil {
			// Written as null by the rewrite, which is what makes passing
			// over it safe; the output scan still reads every element the
			// output does hold.
			continue
		}
		if !hasStructText(elem) {
			continue
		}
		if use.kept {
			if !spellsStructText(elem) {
				// Empty entries spell nothing removed, and nothing kept
				// needs them taken away.
				continue
			}
			return fmt.Errorf("%w: element %d holds /ActualText, /Alt or /E for content that stays",
				ErrStructElementPartlyRemoved, num)
		}
		scrubbed, _ := withoutStructText(elem)
		if !alloc.AllocAt(num, scrubbed) {
			return fmt.Errorf("%w: element %d", ErrStructElementNotAddressable, num)
		}
	}
	return ctx.Err()
}

// sortedMCIDs returns the identifiers of set, ascending.
func sortedMCIDs(set map[int]bool) []int {
	out := make([]int, 0, len(set))
	for mcid := range set {
		out = append(out, mcid)
	}
	sort.Ints(out)
	return out
}

// contentSides says what became of the content under a structure element:
// some of it removed, some of it kept, or both.
type contentSides struct {
	removed, kept bool
}

func (c *contentSides) add(o contentSides) {
	c.removed = c.removed || o.removed
	c.kept = c.kept || o.kept
}

// structWalkKey memoises the content of one object of the tree under the page
// it inherits: an element that states no /Pg takes its parent's.
type structWalkKey struct {
	num, page int
}

// structTextScrub reads the structure tree for [scrubStructTreeText].
type structTextScrub struct {
	alloc   *writer.ObjAllocator
	doc     *document.Document
	pageNum int

	removed, surviving map[int]bool

	// uses holds, by object number, what became of the content of each
	// element above removed content.
	uses map[int]*contentSides
	// memo holds the content of each /K array and element written as an
	// object of its own, so one reached twice is read once; walking marks
	// those on the way down, so a /K cycle reads as content kept.
	memo    map[structWalkKey]contentSides
	walking map[int]bool

	// err is the first refusal the walk met, in tree order.
	err error
}

func (s *structTextScrub) use(num int) *contentSides {
	use, ok := s.uses[num]
	if !ok {
		use = &contentSides{}
		s.uses[num] = use
	}
	return use
}

// mcid returns what became of the content an identifier marks, on the page pg
// an element's items inherit, 0 when nothing above states one. Content on
// another page is kept, and so is an identifier the removal took nothing of.
// An identifier whose page nothing states is taken to be this page's, as
// [structElementsByMCID] takes it.
func (s *structTextScrub) mcid(mcid, pg int) contentSides {
	if pg != 0 && pg != s.pageNum {
		return contentSides{kept: true}
	}
	return contentSides{removed: s.removed[mcid], kept: s.surviving[mcid] || !s.removed[mcid]}
}

// kids returns what became of the content a /K entry names, kObj, on the page
// pg its items inherit, at depth in the tree.
//
// The walk stops at [maxStructTreeDepth], and what lies past it reads as kept:
// an element above content the walk cannot see refuses rather than lose a
// description of it. A kid that does not resolve is null, and names nothing.
func (s *structTextScrub) kids(ctx context.Context, kObj ast.PdfObject, pg, depth int) contentSides {
	if kObj == nil {
		return contentSides{}
	}
	if depth > maxStructTreeDepth || ctx.Err() != nil {
		return contentSides{kept: true}
	}
	arr, err := resolveArrayAllocFirst(ctx, s.alloc, s.doc, kObj)
	if err != nil {
		return s.kid(ctx, kObj, pg, depth)
	}
	return s.once(kObj, pg, func() contentSides {
		var sides contentSides
		for _, kid := range arr {
			sides.add(s.kid(ctx, kid, pg, depth))
		}
		return sides
	})
}

// kid returns what became of the content one item of a /K array names (ISO
// 32000-1, 14.7.4.2): a marked-content identifier, a marked-content
// reference, an object reference, or an element.
func (s *structTextScrub) kid(ctx context.Context, kid ast.PdfObject, pg, depth int) contentSides {
	if mcid, isInt := kid.(ast.PdfInt); isInt {
		return s.mcid(int(mcid), pg)
	}
	return s.once(kid, pg, func() contentSides {
		dict, err := resolveDictAllocFirst(ctx, s.alloc, s.doc, kid)
		if err != nil || dict == nil {
			return contentSides{}
		}
		switch kind, _ := dict["Type"].(ast.PdfName); kind {
		case "MCR":
			// Content in a form XObject is not this page's content stream,
			// which is all a page redaction removes from.
			if _, inForm := dict["Stm"]; inForm {
				return contentSides{kept: true}
			}
			mcid, ok := intEntry(ctx, s.doc, dict["MCID"])
			if !ok {
				return contentSides{kept: true}
			}
			return s.mcid(mcid, structItemPage(dict, pg))
		case "OBJR":
			// An annotation or XObject the element describes: it stays as
			// far as the tree can tell.
			return contentSides{kept: true}
		}
		sides := s.kids(ctx, dict["K"], structItemPage(dict, pg), depth+1)
		if !sides.removed {
			return sides
		}
		if ref, isRef := kid.(ast.PdfIndirectRef); isRef {
			s.use(ref.ObjectNum).add(sides)
			return sides
		}
		if s.err == nil && (sides.kept && spellsStructText(dict) || !sides.kept && hasStructText(dict)) {
			if sides.kept {
				s.err = fmt.Errorf("%w: an element written in its parent's /K holds /ActualText, /Alt or /E for content that stays",
					ErrStructElementPartlyRemoved)
			} else {
				s.err = fmt.Errorf("%w: an element written in its parent's /K holds /ActualText, /Alt or /E for removed content",
					ErrStructElementNotAddressable)
			}
		}
		return sides
	})
}

// once reads the content obj names through read, once per object written on
// its own and the page it inherits. One reached again while it is being read
// is a /K cycle, which reads as content kept.
func (s *structTextScrub) once(obj ast.PdfObject, pg int, read func() contentSides) contentSides {
	ref, isRef := obj.(ast.PdfIndirectRef)
	if !isRef {
		return read()
	}
	key := structWalkKey{num: ref.ObjectNum, page: pg}
	if sides, done := s.memo[key]; done {
		return sides
	}
	if s.walking[ref.ObjectNum] {
		return contentSides{kept: true}
	}
	s.walking[ref.ObjectNum] = true
	sides := read()
	delete(s.walking, ref.ObjectNum)
	s.memo[key] = sides
	return sides
}

// structItemPage returns the page an element or marked-content reference
// states in /Pg, or inherited when it states none by reference.
func structItemPage(dict ast.PdfDict, inherited int) int {
	if ref, isRef := dict["Pg"].(ast.PdfIndirectRef); isRef {
		return ref.ObjectNum
	}
	return inherited
}

// hasStructText reports whether elem holds one of [structTextKeys].
func hasStructText(elem ast.PdfDict) bool {
	for _, key := range structTextKeys {
		if _, present := elem[key]; present {
			return true
		}
	}
	return false
}

// spellsStructText reports whether one of the [structTextKeys] elem holds may
// spell text: any value but a string that decodes to none. An empty /Alt
// describes nothing, so it holds no copy of what a redaction removed, and an
// element whose content went in part keeps it. A value that is not a direct
// string, a reference among them, is taken to spell text.
func spellsStructText(elem ast.PdfDict) bool {
	for _, key := range structTextKeys {
		v, present := elem[key]
		if !present {
			continue
		}
		s, isString := v.(ast.PdfString)
		if !isString || document.DecodePdfString(s) != "" {
			return true
		}
	}
	return false
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
