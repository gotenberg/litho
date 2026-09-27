package mutations

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// structSource is what an output built from some of a document's pages carries
// of that document's structure tree (ISO 32000-1, 14.7; ISO 32000-2, 14.7):
// the elements describing content on those pages, their ancestors, and the
// entries of the tree root that name them.
//
// It is read through view, the document's objects as the copy stages them:
// the copier overrides of the pages, with the elements the prune rewrote and
// the ones it dropped (see [newStructSource]).
type structSource struct {
	doc   *document.Document
	view  *writer.ObjAllocator
	prune *structPrune
	// rootRef is the tree root's object, zero when the catalog holds the root
	// directly.
	rootRef ast.PdfIndirectRef
	root    ast.PdfDict
	// top is the root's /K once pruned, in the source's objects.
	top ast.PdfArray
	// document is the root's only kid of standard type Document, zero when
	// the root holds none or several (see [rootDocument]).
	document ast.PdfIndirectRef
	// parentTree maps each /ParentTree key to its value, as the source has it.
	parentTree map[int]ast.PdfObject
	// nextKey is one past the highest key the source's tree uses or reserves.
	nextKey int
	// ids maps each identifier the /IDTree lists for a kept element to it.
	ids map[string]ast.PdfObject
	// lang is the natural language the source's catalog names, and
	// langString the /Lang string naming it.
	lang       string
	langString ast.PdfString
	// names are the structure types and identifiers the kept elements use.
	names structNames

	// Set when the source joins an output (see [structOutput.expect]).
	copier *writer.Copier
	// parent is what the source's top-level elements name as /P in the output.
	parent ast.PdfIndirectRef
	// pageRefs are the pages the copy wrote for the source, in the output.
	pageRefs     []ast.PdfIndirectRef
	roleRenames  map[ast.PdfName]ast.PdfName
	classRenames map[ast.PdfName]ast.PdfName
	idRenames    map[string]string
}

// newStructSource reads doc's structure tree for an output that copies pages
// of doc, and returns the copier overrides under which the copy carries the
// elements describing those pages and no other: overrides, the objects of doc
// the copy stages already, with every element the prune rewrote and every one
// it dropped.
//
// An element is kept when content it describes sits on one of pages: its own
// marked content, a marked-content reference or an object reference there
// (ISO 32000-1, 14.7.4), or a kept descendant. An element that describes
// content only elsewhere goes, and so does one whose every kid went, since it
// describes nothing the output holds. A kept element loses a /Pg naming a page
// the output does not have. The root's single Document element stays whatever
// its kids: it describes the document, not a page, and ISO 14289-2, 8.2.5.2
// asks for it.
//
// Every element the /K hierarchy does not reach but the root's /ParentTree or
// /IDTree, a kept element's /Ref or its /P does is pruned the same way, and a
// dropped element is replaced by null, so no reference the copy follows
// reaches the page it described.
//
// It returns nil and overrides unchanged when doc has no structure tree or one
// the copy cannot carry whole: a /ParentTree it cannot read, or a tree deeper
// than [maxStructTreeDepth]. The output then holds the pages' content without
// its structure.
func newStructSource(ctx context.Context, doc *document.Document, overrides map[int]ast.PdfObject, pages []*document.Page) (*structSource, map[int]ast.PdfObject, error) {
	catalogRef, ok := CatalogRef(ctx, doc)
	if !ok {
		return nil, overrides, nil
	}
	view := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
	// A done ctx can stop the raw-byte augmentation short, and the allocator
	// then starts below objects the file defines.
	if err := ctx.Err(); err != nil {
		return nil, nil, fmt.Errorf("reading the structure tree: %w", err)
	}
	for _, num := range slices.Sorted(maps.Keys(overrides)) {
		view.AllocAt(num, overrides[num])
	}
	catalog, err := LiveCatalogDict(ctx, view, doc, catalogRef)
	if err != nil {
		return nil, overrides, nil
	}
	rootObj, has := catalog["StructTreeRoot"]
	if !has {
		return nil, overrides, nil
	}
	root, err := resolveDictAllocFirst(ctx, view, doc, rootObj)
	if err != nil || root == nil {
		return nil, overrides, nil
	}
	entries, maxKey, ok := readStructParentTree(ctx, view, doc, root["ParentTree"])
	if !ok {
		doc.Logger.Debug("structure copy: the /ParentTree cannot be read whole, copying the pages without their structure")
		return nil, overrides, nil
	}

	kept := make(map[int]bool, len(pages))
	refs := make([]ast.PdfIndirectRef, 0, len(pages))
	for _, page := range pages {
		if page.Ref.ObjectNum > 0 && !kept[page.Ref.ObjectNum] {
			kept[page.Ref.ObjectNum] = true
			refs = append(refs, page.Ref)
		}
	}
	p := newStructPrune(view, doc, func(ref ast.PdfIndirectRef) bool { return !kept[ref.ObjectNum] },
		pageAnnotations(ctx, view, doc, refs), true)
	s := &structSource{doc: doc, view: view, prune: p, root: root, parentTree: entries}
	if ref, isRef := rootObj.(ast.PdfIndirectRef); isRef {
		s.rootRef = ref
	}

	s.document = rootDocument(ctx, view, doc, root)
	if s.document.ObjectNum != 0 {
		p.keepAlways = map[int]bool{s.document.ObjectNum: true}
	}

	top := p.kids(ctx, root["K"], false, 0)
	if top.Value != nil {
		s.top = structKidList(ctx, view, doc, top.Value)
	}

	for _, key := range slices.Sorted(maps.Keys(entries)) {
		if err := ctx.Err(); err != nil {
			return nil, nil, fmt.Errorf("reading the structure tree: %w", err)
		}
		for _, elem := range structKidList(ctx, view, doc, entries[key]) {
			p.subtree(ctx, elem)
		}
	}
	ids, idsOK := readNameTree(ctx, view, doc, root["IDTree"])
	if !idsOK {
		doc.Logger.Debug("structure copy: the /IDTree cannot be read whole, leaving the identifiers out")
	}
	s.ids = make(map[string]ast.PdfObject, len(ids))
	for _, key := range slices.Sorted(maps.Keys(ids)) {
		if p.subtree(ctx, ids[key]) {
			s.ids[key] = ids[key]
		}
	}
	// Each pass can reach elements whose own /Ref and /P reach further.
	for range maxStructTreeDepth {
		before := len(p.memo)
		p.refs(ctx)
		p.climbParents(ctx, s.rootRef)
		if len(p.memo) == before {
			break
		}
	}
	p.repointParents(ctx, s.rootRef)
	if p.truncated {
		doc.Logger.Debug("structure copy: the structure tree is deeper than the walk reads, copying the pages without their structure")
		return nil, overrides, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, fmt.Errorf("reading the structure tree: %w", err)
	}

	// A dropped element stands for nothing in the output. Null in its place
	// keeps a reference no prune reached, a structure destination's (ISO
	// 32000-2, 12.3.2.3), from carrying the element and its page along.
	for _, num := range slices.Sorted(maps.Keys(p.memo)) {
		if p.memo[num].drop {
			view.AllocAt(num, ast.PdfNull{})
		}
	}

	s.nextKey = maxKey + 1
	if next, ok := intEntry(ctx, doc, root["ParentTreeNextKey"]); ok && next > s.nextKey {
		s.nextKey = next
	}
	s.langString, s.lang = catalogLang(ctx, view, doc, catalog)
	s.names = p.keptNames(ctx)
	return s, view.Pending(), nil
}

// readStructParentTree is [readParentTree] for a structure copy, reading a
// /ParentTree that names no object as the absent entry it is: a reference to
// an undefined object is null (ISO 32000-1, 7.3.10), and a null dictionary
// value an absent one (7.3.7). One whose object cannot be parsed reads the
// same, as the copier copies such an object as null (see
// [writer.Copier.CopyRef]) and qpdf reads it. The copy then carries the
// elements with a tree as empty as the source's. A tree whose root resolves
// but whose nodes do not is still one it cannot read whole.
//
// # Corpus evidence
//
//   - pdfjs issue20516.pdf, issue20872.pdf: their /ParentTree names an object
//     whose cross-reference entry has offset 0.
func readStructParentTree(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject) (map[int]ast.PdfObject, int, bool) {
	resolved, err := resolveAllocFirst(ctx, alloc, doc, obj)
	if err != nil && ctx.Err() == nil {
		doc.Logger.Debug("structure copy: the /ParentTree does not resolve, reading it as absent", slog.Any("err", err))
		return make(map[int]ast.PdfObject), -1, true
	}
	if _, isNull := resolved.(ast.PdfNull); err == nil && (resolved == nil || isNull) {
		return make(map[int]ast.PdfObject), -1, true
	}
	return readParentTree(ctx, alloc, doc, obj)
}

// climbParents prunes the parent of each element the prune kept that no walk
// reached. Such a parent is reached only upwards, as in a tree whose
// /ParentTree lists an element its parent never lists in /K, and a copy
// follows /P. Its caller repeats it until no element is new.
func (p *structPrune) climbParents(ctx context.Context, root ast.PdfIndirectRef) {
	for _, num := range slices.Sorted(maps.Keys(p.memo)) {
		if ctx.Err() != nil {
			return
		}
		if p.memo[num].drop {
			continue
		}
		elem, err := resolveDictAllocFirst(ctx, p.alloc, p.doc, ast.PdfIndirectRef{ObjectNum: num})
		if err != nil || elem == nil {
			continue
		}
		parent, isRef := elem["P"].(ast.PdfIndirectRef)
		if !isRef || parent.ObjectNum == root.ObjectNum {
			continue
		}
		if _, decided := p.memo[parent.ObjectNum]; !decided {
			p.subtree(ctx, parent)
		}
	}
}

// repointParents makes each element the prune kept whose /P names neither the
// root nor a kept element name the root instead: a parent the prune dropped,
// or something that is no element at all. A copy follows /P, and would follow
// it to what the prune did not keep.
func (p *structPrune) repointParents(ctx context.Context, root ast.PdfIndirectRef) {
	for _, num := range slices.Sorted(maps.Keys(p.memo)) {
		if p.memo[num].drop {
			continue
		}
		ref := ast.PdfIndirectRef{ObjectNum: num}
		elem, err := resolveDictAllocFirst(ctx, p.alloc, p.doc, ref)
		if err != nil || elem == nil {
			continue
		}
		parent, isRef := elem["P"].(ast.PdfIndirectRef)
		if !isRef || parent.ObjectNum == root.ObjectNum || p.kept(parent.ObjectNum) {
			continue
		}
		updated := maps.Clone(elem)
		if root.ObjectNum != 0 {
			updated["P"] = root
		} else {
			delete(updated, "P")
		}
		p.alloc.AllocAt(num, updated)
	}
}

// structNames are the names the elements of a tree use that another tree's
// may define otherwise: the structure types of the elements that name no
// namespace, which the root's /RoleMap maps (ISO 32000-2, 14.8.6.1), and the
// element identifiers (ISO 32000-1, 14.7.2, Table 323).
type structNames struct {
	types map[ast.PdfName]bool
	ids   map[string]bool
}

// newStructNames returns an empty [structNames].
func newStructNames() structNames {
	return structNames{types: make(map[ast.PdfName]bool), ids: make(map[string]bool)}
}

// add records the names elem uses.
func (n structNames) add(elem ast.PdfDict) {
	if s, isName := elem["S"].(ast.PdfName); isName {
		if _, hasNS := elem["NS"]; !hasNS {
			n.types[s] = true
		}
	}
	if id, isString := elem["ID"].(ast.PdfString); isString {
		n.ids[string(id)] = true
	}
}

// keptNames returns the names the kept elements use, those written directly
// into a kept element's /K included.
func (p *structPrune) keptNames(ctx context.Context) structNames {
	names := newStructNames()
	var direct func(elem ast.PdfDict, depth int)
	direct = func(elem ast.PdfDict, depth int) {
		if depth > maxStructTreeDepth {
			return
		}
		for _, kid := range structKidList(ctx, p.alloc, p.doc, elem["K"]) {
			d, isDict := kid.(ast.PdfDict)
			if !isDict {
				continue
			}
			if kind, _ := d["Type"].(ast.PdfName); kind == "MCR" || kind == "OBJR" {
				continue
			}
			names.add(d)
			direct(d, depth+1)
		}
	}
	for _, num := range slices.Sorted(maps.Keys(p.memo)) {
		if p.memo[num].drop {
			continue
		}
		elem, err := resolveDictAllocFirst(ctx, p.alloc, p.doc, ast.PdfIndirectRef{ObjectNum: num})
		if err != nil || elem == nil {
			continue
		}
		names.add(elem)
		direct(elem, 0)
	}
	return names
}

// kept reports whether the prune decided to keep the indirect element num. An
// element it never reached has no verdict and is not kept.
func (p *structPrune) kept(num int) bool {
	v, decided := p.memo[num]
	return decided && !v.drop
}

// emptyDocument reports whether kid is the Document element of s and the prune
// left it no kid.
func (s *structSource) emptyDocument(ctx context.Context, kid ast.PdfObject) bool {
	ref, isRef := kid.(ast.PdfIndirectRef)
	if !isRef || s.document.ObjectNum == 0 || ref.ObjectNum != s.document.ObjectNum {
		return false
	}
	elem, err := resolveDictAllocFirst(ctx, s.view, s.doc, ref)
	if err != nil || elem == nil {
		return false
	}
	return len(structKidList(ctx, s.view, s.doc, elem["K"])) == 0
}

// carriedValue returns the /ParentTree value the output holds for value, the
// source's: an element kept stays, and in a page's array one dropped becomes
// null so every other marked-content identifier keeps its index (ISO 32000-1,
// 14.7.4.4). It returns nil when nothing of value is kept.
func (s *structSource) carriedValue(ctx context.Context, value ast.PdfObject) ast.PdfObject {
	keep := func(obj ast.PdfObject) bool {
		ref, isRef := obj.(ast.PdfIndirectRef)
		return isRef && s.prune.kept(ref.ObjectNum)
	}
	arr, isArray := value.(ast.PdfArray)
	if !isArray {
		if ref, isRef := value.(ast.PdfIndirectRef); isRef {
			if resolved, err := resolveArrayAllocFirst(ctx, s.view, s.doc, ref); err == nil && resolved != nil {
				arr, isArray = resolved, true
			}
		}
	}
	if !isArray {
		if keep(value) {
			return value
		}
		return nil
	}
	out := make(ast.PdfArray, len(arr))
	anyKept := false
	for i, entry := range arr {
		if keep(entry) {
			out[i] = entry
			anyKept = true
			continue
		}
		out[i] = ast.PdfNull{}
	}
	if !anyKept {
		return nil
	}
	return out
}

// structKeyEntries are the entries that key an object's marked content into the
// /ParentTree: a page's or a form XObject's /StructParents, and the
// /StructParent of an annotation or an XObject an object reference names (ISO
// 32000-1, 14.7.4.4, Tables 30, 95, 164 and 326).
var structKeyEntries = []ast.PdfName{"StructParents", "StructParent"}

// structKeysOf returns the dictionary an object copied into alloc keys its
// marked content in, or nil: a dictionary's own, a stream's dictionary.
func structKeysOf(alloc *writer.ObjAllocator, num int) ast.PdfDict {
	switch v := alloc.Pending()[num].(type) {
	case ast.PdfDict:
		return v
	case ast.PdfStream:
		return v.Dict
	}
	return nil
}

// copiedObjects returns the output objects copier wrote, in order, and extra,
// leaving out the ones it stood an output object in for (see
// [writer.Copier.MapTo]).
func copiedObjects(copier *writer.Copier, mapped map[int]bool, extra []ast.PdfIndirectRef) []int {
	seen := make(map[int]bool)
	var out []int
	for src, dst := range copier.Mapping() {
		if mapped[src] || seen[dst] {
			continue
		}
		seen[dst] = true
		out = append(out, dst)
	}
	for _, ref := range extra {
		if !seen[ref.ObjectNum] {
			seen[ref.ObjectNum] = true
			out = append(out, ref.ObjectNum)
		}
	}
	slices.Sort(out)
	return out
}

// stripStructKeys removes the /StructParents and /StructParent entries of the
// objects copier wrote into alloc and of pages. It is for content copied into
// a tagged output without its structure: its keys index no entry of the
// output's /ParentTree, or one that another document's content owns.
func stripStructKeys(alloc *writer.ObjAllocator, copier *writer.Copier, pages []ast.PdfIndirectRef) {
	for _, num := range copiedObjects(copier, nil, pages) {
		dict := structKeysOf(alloc, num)
		for _, key := range structKeyEntries {
			delete(dict, key)
		}
	}
}

// logStructCopy records at Debug level what a structure copy decided.
func logStructCopy(doc *document.Document, msg string, attrs ...any) {
	doc.Logger.Debug("structure copy: "+msg, slog.Group("structure", attrs...))
}
