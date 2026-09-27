package mutations

import (
	"context"
	"fmt"
	"maps"
	"math"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// maxStructKey is the highest /StructParents or /StructParent key an output
// holds. ISO 32000-1, Annex C (Table C.1) bounds an integer at 2^31-1, as ISO
// 19005-2, 6.1.13 does for PDF/A, so a key the renumbering of a merge would
// push past it is left out rather than written.
const maxStructKey = math.MaxInt32

// structOutput assembles the structure tree of a document built from the pages
// of others: a split's, a merge's, or that of the target of a page insertion,
// whose own tree the inserted pages' elements join.
//
// The first source whose elements the output carries sets the top of the tree,
// and every later one joins it (see [structOutput.add]). A root holding one
// Document element keeps holding one: ISO/TS 32005, Table 5 asks the root for
// exactly one and ISO 14289-2, 8.2.5.2 for its only child to be one, so a
// joining source whose root holds one too puts its elements into it rather
// than beside it (see [structOutput.joinsContainer]). Any other joining source
// keeps its elements at the root, beside the output's, where its own root held
// them, so the output fails no rule of those clauses its sources passed.
//
// The /ParentTree keys of each source are renumbered past those already in the
// output, and the /StructParents and /StructParent entries of its copied
// content with them, so no two sources claim one key (ISO 32000-1, 14.7.4.4).
// A joining source's /RoleMap, /ClassMap and element identifiers join the
// output's, each renamed where the output already defines the name otherwise
// (see [structOutput.join]).
type structOutput struct {
	alloc *writer.ObjAllocator
	// target is the document a page insertion edits, nil for a split or a merge,
	// which build their output from nothing. The output's objects are then read
	// from alloc alone.
	target     *document.Document
	catalogRef ast.PdfIndirectRef
	// rootRef is the object the tree's root is written at.
	rootRef ast.PdfIndirectRef
	// rootBase holds the target root's entries, kept where the output does not
	// rebuild them.
	rootBase ast.PdfDict
	kids     ast.PdfArray
	// container is the element the joining sources' top-level elements go into,
	// zero for the root, and containerKids its /K.
	container     ast.PdfIndirectRef
	containerKids ast.PdfArray
	hosted        bool
	roleMap       ast.PdfDict
	// types are the structure types the output's elements with no namespace
	// use, which a /RoleMap entry would remap.
	types       map[ast.PdfName]bool
	classMap    ast.PdfDict
	classOrigin map[ast.PdfName]structOrigin
	// ids maps each element identifier the output's /IDTree lists to its
	// element, and idNames every identifier reserved, a joined source's
	// included before its elements are copied.
	ids        map[string]ast.PdfObject
	idNames    map[string]bool
	namespaces ast.PdfArray
	af         ast.PdfArray
	lexicons   ast.PdfArray
	parentTree map[int]ast.PdfObject
	nextKey    int
	// lang is the natural language the output's catalog names.
	lang     string
	sources  []*structSource
	untagged []untaggedCopy
}

// untaggedCopy is content copied into a tagged output without its structure.
type untaggedCopy struct {
	copier *writer.Copier
	pages  []ast.PdfIndirectRef
}

// structOrigin is where a /ClassMap definition came from, so two can be
// compared.
type structOrigin struct {
	doc  *document.Document
	view *writer.ObjAllocator
	obj  ast.PdfObject
}

// newStructOutput returns an empty structure tree for a split or a merge,
// whose root is written at an object reserved in alloc now, so the copied
// elements can name it as their parent. lang is the natural language the
// output's catalog names.
func newStructOutput(alloc *writer.ObjAllocator, lang string) *structOutput {
	t := emptyStructOutput(alloc, lang)
	t.rootRef = alloc.Alloc(ast.PdfNull{})
	return t
}

// emptyStructOutput returns a structure tree holding nothing, with no object
// reserved for its root.
func emptyStructOutput(alloc *writer.ObjAllocator, lang string) *structOutput {
	return &structOutput{
		alloc:       alloc,
		roleMap:     ast.PdfDict{},
		types:       make(map[ast.PdfName]bool),
		classMap:    ast.PdfDict{},
		classOrigin: make(map[ast.PdfName]structOrigin),
		ids:         make(map[string]ast.PdfObject),
		idNames:     make(map[string]bool),
		parentTree:  make(map[int]ast.PdfObject),
		lang:        lang,
	}
}

// newTargetStructOutput returns the structure tree of doc, the target of a page
// insertion, as alloc stages it, for inserted pages' elements to join. It
// returns nil when doc has no structure tree: the output's tree is then the
// inserted pages' alone. usable is false when doc's tree cannot be extended
// whole, its /ParentTree or /IDTree being unreadable; the inserted pages then
// join the document without their structure, and doc's tree stays as it is.
func newTargetStructOutput(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalogRef ast.PdfIndirectRef) (out *structOutput, usable bool, err error) {
	catalog, err := LiveCatalogDict(ctx, alloc, doc, catalogRef)
	if err != nil {
		return nil, false, fmt.Errorf("reading the catalog: %w", err)
	}
	_, lang := catalogLang(ctx, alloc, doc, catalog)
	rootObj, has := catalog["StructTreeRoot"]
	if !has {
		return nil, true, nil
	}
	root, err := resolveDictAllocFirst(ctx, alloc, doc, rootObj)
	if err != nil || root == nil {
		return nil, true, nil
	}
	entries, maxKey, ok := readStructParentTree(ctx, alloc, doc, root["ParentTree"])
	if !ok {
		return nil, false, nil
	}
	ids, ok := readNameTree(ctx, alloc, doc, root["IDTree"])
	if !ok {
		return nil, false, nil
	}

	t := emptyStructOutput(alloc, lang)
	// A root the catalog holds directly has no number its joining elements can
	// name as their parent, so the output writes it as an object of its own.
	if ref, isRef := rootObj.(ast.PdfIndirectRef); isRef {
		t.rootRef = ref
	} else {
		t.rootRef = alloc.Alloc(ast.PdfNull{})
	}
	t.target = doc
	t.catalogRef = catalogRef
	t.rootBase = maps.Clone(root)
	t.hosted = true
	t.parentTree = entries
	t.nextKey = maxKey + 1
	if next, ok := intEntry(ctx, doc, root["ParentTreeNextKey"]); ok && next > t.nextKey {
		t.nextKey = next
	}
	t.ids = ids
	for id := range ids {
		t.idNames[id] = true
	}
	t.kids = structKidList(ctx, alloc, doc, root["K"])
	if ref := rootDocument(ctx, alloc, doc, root); ref.ObjectNum != 0 {
		if elem, err := resolveDictAllocFirst(ctx, alloc, doc, ref); err == nil && elem != nil {
			t.container = ref
			t.containerKids = structKidList(ctx, alloc, doc, elem["K"])
		}
	}
	if roleMap, err := resolveDictAllocFirst(ctx, alloc, doc, root["RoleMap"]); err == nil && roleMap != nil {
		t.roleMap = maps.Clone(roleMap)
	}
	if classMap, err := resolveDictAllocFirst(ctx, alloc, doc, root["ClassMap"]); err == nil && classMap != nil {
		t.classMap = maps.Clone(classMap)
		for name, def := range classMap {
			t.classOrigin[name] = structOrigin{doc: doc, view: alloc, obj: def}
		}
	}
	t.namespaces = structKidList(ctx, alloc, doc, root["Namespaces"])
	t.af = structKidList(ctx, alloc, doc, root["AF"])
	t.lexicons = structKidList(ctx, alloc, doc, root["PronunciationLexicon"])
	names := elementNames(ctx, alloc, doc, root["K"])
	t.types = names.types
	for id := range names.ids {
		t.idNames[id] = true
	}
	return t, true, nil
}

// elementNames returns the names the elements below kObj use, read from alloc
// first.
func elementNames(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, kObj ast.PdfObject) structNames {
	names := newStructNames()
	visited := make(map[int]bool)
	var walk func(obj ast.PdfObject, depth int)
	walk = func(obj ast.PdfObject, depth int) {
		if depth > maxStructTreeDepth || ctx.Err() != nil {
			return
		}
		for _, kid := range structKidListOnce(ctx, alloc, doc, obj, visited) {
			if ref, isRef := kid.(ast.PdfIndirectRef); isRef {
				if visited[ref.ObjectNum] {
					continue
				}
				visited[ref.ObjectNum] = true
			}
			if _, isInt := kid.(ast.PdfInt); isInt {
				continue
			}
			elem, err := resolveDictAllocFirst(ctx, alloc, doc, kid)
			if err != nil || elem == nil {
				continue
			}
			if _, isElement := elem["S"].(ast.PdfName); !isElement {
				continue
			}
			names.add(elem)
			walk(elem["K"], depth+1)
		}
	}
	walk(kObj, 0)
	return names
}

// resolve reads one of the output's objects: from alloc, and for a page
// insertion from the target document when alloc holds none at its number.
func (t *structOutput) resolve(ctx context.Context, obj ast.PdfObject) ast.PdfObject {
	if ref, isRef := obj.(ast.PdfIndirectRef); isRef {
		if pending, queued := t.alloc.Pending()[ref.ObjectNum]; queued {
			return pending
		}
		if t.target == nil {
			return nil
		}
		resolved, err := document.ResolveObject(ctx, t.target, ref)
		if err != nil {
			return nil
		}
		return resolved
	}
	return obj
}

// pendingDict returns obj when it is a dictionary, or the dictionary alloc
// holds at its number when it is a reference: an object the output wrote,
// which it may edit in place. It returns nil for anything else.
func (t *structOutput) pendingDict(obj ast.PdfObject) ast.PdfDict {
	if ref, isRef := obj.(ast.PdfIndirectRef); isRef {
		d, _ := t.alloc.Pending()[ref.ObjectNum].(ast.PdfDict)
		return d
	}
	d, _ := obj.(ast.PdfDict)
	return d
}

// resolveDict is [structOutput.resolve] for a dictionary.
func (t *structOutput) resolveDict(ctx context.Context, obj ast.PdfObject) ast.PdfDict {
	d, _ := t.resolve(ctx, obj).(ast.PdfDict)
	return d
}

// kidList is [structKidList] for one of the output's objects.
func (t *structOutput) kidList(ctx context.Context, kObj ast.PdfObject) ast.PdfArray {
	switch v := t.resolve(ctx, kObj).(type) {
	case nil, ast.PdfNull:
		return nil
	case ast.PdfArray:
		return slices.Clone(v)
	}
	return ast.PdfArray{kObj}
}

// kidListOnce is [structKidListOnce] for one of the output's objects.
func (t *structOutput) kidListOnce(ctx context.Context, kObj ast.PdfObject, visited map[int]bool) ast.PdfArray {
	if _, isArray := t.resolve(ctx, kObj).(ast.PdfArray); isArray && document.VisitedBefore(kObj, visited) {
		return nil
	}
	return t.kidList(ctx, kObj)
}

// expect prepares copier, which copies the pages of s, to carry s's elements
// into the output: the root of s's tree becomes the element the top-level
// elements of s join, so nothing the copy follows reaches s's own root and,
// through it, every element s holds.
func (t *structOutput) expect(s *structSource, copier *writer.Copier) {
	s.copier = copier
	s.parent = t.rootRef
	if t.joinsContainer(s) {
		s.parent = t.container
	}
	if s.rootRef.ObjectNum != 0 {
		copier.MapTo(s.rootRef.ObjectNum, s.parent)
	}
}

// joinsContainer reports whether the top-level elements of s, joining an
// output that already has a tree, go into the output's Document element rather
// than beside it: when the roots of both hold one Document element (see
// [rootDocument]). Each then describes its content as a document's, and the
// output's root keeps holding exactly one Document, as ISO/TS 32005, Table 5
// and ISO 14289-2, 8.2.5.2 ask. The Document of s goes into the output's, as
// ISO/TS 32005 lets a Document hold one. A source whose root holds no single
// Document keeps its elements at the root, where its own held them.
func (t *structOutput) joinsContainer(s *structSource) bool {
	return t.hosted && t.container.ObjectNum != 0 && s.document.ObjectNum != 0
}

// untag records content copied into the output without its structure, whose
// structure keys the output drops (see [stripStructKeys]).
func (t *structOutput) untag(copier *writer.Copier, pages []ast.PdfIndirectRef) {
	t.untagged = append(t.untagged, untaggedCopy{copier: copier, pages: pages})
}

// add carries the elements of s, whose pages the copy wrote as pageRefs, into
// the output. The first source to arrive in an output built from nothing sets
// the top of its tree. Every later one, and every source joining a page
// insertion's target, has its top-level elements put at index at among the
// top-level elements of the output's container, or after the last when at is
// negative: page order is the logical reading order the structure tree gives
// (ISO 32000-1, 14.8.2.3), so an element joins where its pages did.
//
// A source whose catalog names a natural language other than the one its
// top-level elements inherit in the output has it written on each of them that
// names none: an element takes the language of its parent, and the catalog's
// when none names one (ISO 32000-1, 14.9.2.1), so its content would otherwise
// read as another source's. A source whose catalog names none has nothing
// written: its content inherits the output's language, as it inherited none
// in its source. The empty language identifier, which says the language is
// unknown (ISO 32000-1, 14.9.2), is what ISO 14289-1, 7.2 test 29 and ISO
// 14289-2, 8.4.4 test 2 reject on a structure element, so writing it there
// failed merges whose sources pass those tests.
//
// # Corpus evidence
//
//   - All corpora: merging each of 1124 tagged files with the next, 160 merges
//     wrote the unknown language on a source's elements, which ISO 14289-1,
//     7.2 test 29 and ISO 14289-2, 8.4.4 test 2 then reported, as veraPDF
//     does.
func (t *structOutput) add(ctx context.Context, s *structSource, pageRefs []ast.PdfIndirectRef, at int) error {
	s.pageRefs = pageRefs
	if err := t.join(ctx, s); err != nil {
		return err
	}

	inherited := t.parentLang(ctx, s)
	copied := make(ast.PdfArray, 0, len(s.top))
	for _, kid := range s.top {
		// The Document element kept for the root's sake describes nothing a
		// joining source adds when the prune left it empty: the output has a
		// root of its own.
		if t.hosted && s.emptyDocument(ctx, kid) {
			continue
		}
		dst, err := s.copier.CopyObject(ctx, kid)
		if err != nil {
			return fmt.Errorf("copying the structure elements: %w", err)
		}
		if elem := t.pendingDict(dst); elem != nil {
			elem["P"] = s.parent
			if _, has := elem["Lang"]; !has && s.lang != "" && !strings.EqualFold(s.lang, inherited) {
				elem["Lang"] = s.langString
			}
		}
		copied = append(copied, dst)
	}

	for _, key := range []ast.PdfName{"Namespaces", "AF", "PronunciationLexicon"} {
		for _, entry := range structKidList(ctx, s.view, s.doc, s.root[key]) {
			dst, err := s.copier.CopyObject(ctx, entry)
			if err != nil {
				return fmt.Errorf("copying the structure tree's /%s: %w", key, err)
			}
			switch key {
			case "Namespaces":
				t.namespaces = appendNew(t.namespaces, dst)
			case "AF":
				t.af = appendNew(t.af, dst)
			default:
				t.lexicons = appendNew(t.lexicons, dst)
			}
		}
	}

	t.sources = append(t.sources, s)
	if !t.hosted {
		t.hosted = true
		t.kids = copied
		if s.document.ObjectNum != 0 {
			if dst, copiedDoc := s.copier.Mapping()[s.document.ObjectNum]; copiedDoc {
				t.container = ast.PdfIndirectRef{ObjectNum: dst}
				t.containerKids = t.kidList(ctx, t.resolveDict(ctx, t.container)["K"])
			}
		}
		return nil
	}
	if s.parent == t.container && t.container.ObjectNum != 0 {
		t.containerKids = insertKids(t.containerKids, copied, at)
	} else {
		t.kids = insertKids(t.kids, copied, at)
	}
	return nil
}

// parentLang returns the natural language the top-level elements of s inherit
// in the output: the /Lang of the output's Document element when they join it
// and it names one, and the catalog's otherwise (ISO 32000-1, 14.9.2.1).
func (t *structOutput) parentLang(ctx context.Context, s *structSource) string {
	if t.hosted && s.parent == t.container && t.container.ObjectNum != 0 {
		if elem := t.resolveDict(ctx, t.container); elem != nil {
			if lang, named := t.resolve(ctx, elem["Lang"]).(ast.PdfString); named {
				return strings.TrimSpace(document.DecodePdfString(lang))
			}
		}
	}
	return t.lang
}

// appendNew appends obj to arr unless arr already holds it.
func appendNew(arr ast.PdfArray, obj ast.PdfObject) ast.PdfArray {
	if ref, isRef := obj.(ast.PdfIndirectRef); isRef && slices.Contains(arr, ast.PdfObject(ref)) {
		return arr
	}
	return append(arr, obj)
}

// insertKids returns kids with added inserted at index at, or appended when at
// is negative or past the end.
func insertKids(kids, added ast.PdfArray, at int) ast.PdfArray {
	if at < 0 || at > len(kids) {
		at = len(kids)
	}
	out := make(ast.PdfArray, 0, len(kids)+len(added))
	out = append(out, kids[:at]...)
	out = append(out, added...)
	return append(out, kids[at:]...)
}

// insertionIndex returns where, among the elements s joins (see
// [structOutput.expect]), the elements of s describing pages inserted before
// the page at index at of pages go: before the first element whose content
// starts on that page or a later one, and after every element when none does.
// An element describing no page is passed over.
func (t *structOutput) insertionIndex(ctx context.Context, s *structSource, pages []ast.PdfIndirectRef, at int) int {
	index := make(map[int]int, len(pages))
	for i, ref := range pages {
		index[ref.ObjectNum] = i
	}
	kids := t.kids
	if s.parent == t.container && t.container.ObjectNum != 0 {
		kids = t.containerKids
	}
	for i, kid := range kids {
		if first, found := t.firstPage(ctx, kid, index); found && first >= at {
			return i
		}
	}
	return -1
}

// firstPage returns the lowest index a page the subtree at obj names has in
// index: the /Pg of an element, a marked-content reference or an object
// reference (ISO 32000-1, Tables 323 to 325).
func (t *structOutput) firstPage(ctx context.Context, obj ast.PdfObject, index map[int]int) (int, bool) {
	first, found := 0, false
	visited := make(map[int]bool)
	var walk func(obj ast.PdfObject, depth int)
	walk = func(obj ast.PdfObject, depth int) {
		if depth > maxStructTreeDepth || ctx.Err() != nil {
			return
		}
		if ref, isRef := obj.(ast.PdfIndirectRef); isRef {
			if visited[ref.ObjectNum] {
				return
			}
			visited[ref.ObjectNum] = true
		}
		dict := t.resolveDict(ctx, obj)
		if dict == nil {
			return
		}
		if pg, isRef := dict["Pg"].(ast.PdfIndirectRef); isRef {
			if i, held := index[pg.ObjectNum]; held && (!found || i < first) {
				first, found = i, true
			}
		}
		if kind, _ := dict["Type"].(ast.PdfName); kind == "MCR" || kind == "OBJR" {
			return
		}
		for _, kid := range t.kidListOnce(ctx, dict["K"], visited) {
			walk(kid, depth+1)
		}
	}
	walk(obj, 0)
	return first, found
}

// finish settles the keys and identifiers of every source, and writes the
// tree's root at [structOutput.rootRef], which it returns.
func (t *structOutput) finish(ctx context.Context) (ast.PdfIndirectRef, error) {
	for _, s := range t.sources {
		if err := t.settle(ctx, s); err != nil {
			return ast.PdfIndirectRef{}, err
		}
	}
	for _, u := range t.untagged {
		stripStructKeys(t.alloc, u.copier, u.pages)
	}

	if t.container.ObjectNum != 0 {
		if elem := t.resolveDict(ctx, t.container); elem != nil {
			// A /K written as an array object of its own is rewritten in
			// place, so no object is left behind that nothing names.
			if arrayRef, isRef := elem["K"].(ast.PdfIndirectRef); isRef && len(t.containerKids) > 0 {
				if _, isArray := t.resolve(ctx, arrayRef).(ast.PdfArray); isArray {
					t.alloc.AllocAt(arrayRef.ObjectNum, t.containerKids)
					return t.writeRoot(ctx)
				}
			}
			updated := maps.Clone(elem)
			setOrDelete(updated, "K", kidsValue(t.containerKids))
			t.alloc.AllocAt(t.container.ObjectNum, updated)
		}
	}
	return t.writeRoot(ctx)
}

// writeRoot writes the tree's root at [structOutput.rootRef] and returns it.
func (t *structOutput) writeRoot(ctx context.Context) (ast.PdfIndirectRef, error) {
	if err := ctx.Err(); err != nil {
		return ast.PdfIndirectRef{}, fmt.Errorf("writing the structure tree root: %w", err)
	}

	root := ast.PdfDict{}
	if t.rootBase != nil {
		root = maps.Clone(t.rootBase)
	}
	root["Type"] = ast.PdfName("StructTreeRoot")
	setOrDelete(root, "K", kidsValue(t.kids))
	root["ParentTree"] = buildNumberTree(t.alloc, t.parentTree)
	root["ParentTreeNextKey"] = ast.PdfInt(t.nextKey)
	delete(root, "IDTree")
	if len(t.ids) > 0 {
		root["IDTree"] = buildNameTree(t.alloc, t.ids)
	}
	for key, value := range map[ast.PdfName]ast.PdfObject{
		"RoleMap":              t.roleMap,
		"ClassMap":             t.classMap,
		"Namespaces":           t.namespaces,
		"AF":                   t.af,
		"PronunciationLexicon": t.lexicons,
	} {
		delete(root, key)
		switch v := value.(type) {
		case ast.PdfDict:
			if len(v) > 0 {
				root[key] = v
			}
		case ast.PdfArray:
			if len(v) > 0 {
				root[key] = v
			}
		}
	}
	t.alloc.AllocAt(t.rootRef.ObjectNum, root)
	return t.rootRef, nil
}

// kidsValue returns the /K a list of kids is written as, nil for none.
func kidsValue(kids ast.PdfArray) ast.PdfObject {
	if len(kids) == 0 {
		return nil
	}
	return kids
}

// settle carries the /ParentTree entries and identifiers of s into the
// output, renames what [structOutput.join] decided, and renumbers the
// structure keys of the content copied for s past the ones the output holds.
//
// Only the keys the copied content names are carried: an entry for content
// the output does not hold describes nothing in it. Copying an entry's
// elements can copy content naming a key of its own, an object reference's
// annotation or XObject, so the keys are read again until none is new.
func (t *structOutput) settle(ctx context.Context, s *structSource) error {
	base := t.nextKey

	for _, id := range slices.Sorted(maps.Keys(s.ids)) {
		dst, err := s.copier.CopyObject(ctx, s.ids[id])
		if err != nil {
			return fmt.Errorf("copying the elements the /IDTree names: %w", err)
		}
		if renamed, ok := s.idRenames[id]; ok {
			id = renamed
		}
		t.ids[id] = dst
	}

	carried := make(map[int]ast.PdfObject)
	seen := make(map[int]bool)
	maxKey := s.nextKey - 1
	for range maxStructTreeDepth {
		var fresh []int
		for _, num := range copiedObjects(s.copier, s.pageRefs) {
			dict := structKeysOf(t.alloc, num)
			for _, entry := range structKeyEntries {
				if key, isInt := dict[entry].(ast.PdfInt); isInt && key >= 0 && !seen[int(key)] {
					seen[int(key)] = true
					fresh = append(fresh, int(key))
				}
			}
		}
		if len(fresh) == 0 {
			break
		}
		slices.Sort(fresh)
		for _, key := range fresh {
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("carrying the parent tree: %w", err)
			}
			maxKey = max(maxKey, key)
			value, listed := s.parentTree[key]
			if !listed {
				continue
			}
			kept := s.carriedValue(ctx, value)
			if kept == nil {
				continue
			}
			dst, err := s.copier.CopyObject(ctx, kept)
			if err != nil {
				return fmt.Errorf("copying the parent tree: %w", err)
			}
			carried[key] = dst
		}
	}

	t.rename(ctx, s)

	for key, value := range carried {
		if base+key <= maxStructKey {
			t.parentTree[base+key] = value
		}
	}
	for _, num := range copiedObjects(s.copier, s.pageRefs) {
		dict := structKeysOf(t.alloc, num)
		for _, entry := range structKeyEntries {
			key, isInt := dict[entry].(ast.PdfInt)
			if !isInt {
				continue
			}
			if key < 0 || base+int(key) > maxStructKey {
				delete(dict, entry)
				continue
			}
			dict[entry] = ast.PdfInt(base + int(key))
		}
	}
	t.nextKey = min(base+maxKey+1, maxStructKey)
	return nil
}
