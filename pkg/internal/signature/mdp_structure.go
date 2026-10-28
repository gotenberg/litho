package signature

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/intconv"
)

// annotationTagType returns the standard structure type that tags an
// annotation of the given subtype. ISO 14289-1, 7.18.4 nests a widget within
// a Form element (ISO 32000-1, 14.8.4.5), 7.18.5 a link within a Link element
// and 7.18.1 any other annotation within an Annot element (14.8.4.4).
func annotationTagType(subtype string) string {
	switch subtype {
	case "Widget":
		return "Form"
	case "Link":
		return "Link"
	}
	return "Annot"
}

// annotationTagEntries are the entries of a structure element dictionary (ISO
// 32000-1, 14.7.2, Table 323) that a new element tagging an annotation may
// carry: its type, structure type, parent, kids and page, the alternate
// description, replacement text, expansion, language and title that describe
// the annotation to assistive technology, and the namespace ISO 32000-2,
// 14.7.4 adds. An /ID, which the /IDTree would have to gain, attributes,
// classes and a revision number are not among them.
var annotationTagEntries = map[ast.PdfName]bool{
	"Type": true, "S": true, "P": true, "K": true, "Pg": true,
	"Alt": true, "ActualText": true, "E": true, "Lang": true, "T": true, "NS": true,
}

// standardNamespaces are the namespaces of the standard structure types of
// PDF 1.7 and PDF 2.0 (ISO 32000-2, 14.8.6), in which the structure types of
// [annotationTagType] keep their meaning.
var standardNamespaces = map[string]bool{
	"http://iso.org/pdf/ssn":  true,
	"http://iso.org/pdf2/ssn": true,
}

// withoutAnnotationTags returns objects without those that add to the
// structure tree the annotations an update wrote within policy, and a note for
// MDPInfo when they could not be told apart. policy is 2 or 3.
//
// ISO 32000-1, 12.8.2.2, Table 254 permits signing and filling in forms from
// P=2 up, and P=3 adds annotation creation. In a tagged document the
// annotation that such a change adds is a structural content item (14.7.4.4):
// ISO 14289-1, 7.18.1 requires each annotation to be tagged, and 7.18.4 a
// widget to be nested within a Form element. A PDF/UA signer that adds a
// signature widget therefore also writes a structure element for it, the
// parent tree entry its /StructParent keys, and the element and structure tree
// root whose kids gain it. Those objects are part of the permitted change, and
// only these are:
//
//   - a new structure element whose kids are object references (14.7.4.3,
//     Table 325) to annotations the update wrote within policy, each with the
//     structure type [annotationTagType] names for it, whose entries are among
//     [annotationTagEntries], and whose /P names an element the signed
//     revision holds;
//   - that parent, and the /K array it holds as an object of its own, when its
//     kids are the signed ones with those elements inserted, and nothing else
//     of it changed but, on the structure tree root (Table 322), its
//     /ParentTree and /ParentTreeNextKey;
//   - the nodes of the parent tree (7.9.7), when every signed entry keeps its
//     value and every new one keys an annotation's /StructParent to the new
//     element that tags it.
//
// Any other change to the structure tree stays a change: an existing
// element's other kids, attributes, /Alt or /ActualText, the role map, or a
// new element that claims page content. The classifier reports a typed
// structure object, and [changedUntyped] one without /Type the structure tree
// root reaches.
func withoutAnnotationTags(
	ctx context.Context,
	doc *document.Document,
	signed *signedRevision,
	objects []appendedObject,
	policy int,
) (_ []appendedObject, note string) {
	tagged := make(map[int]string)
	candidates := false
	for _, obj := range objects {
		switch {
		case obj.IsAnnot && permitted(obj, policy):
			tagged[obj.ObjNum] = annotationTagType(obj.Subtype)
		case !obj.IsXRef && !obj.IsAnnot && (obj.Type == "" || obj.Type == "StructElem"):
			candidates = true
		}
	}
	if len(tagged) == 0 || !candidates {
		return objects, ""
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return objects, fmt.Sprintf("; annotation tags not recognized: %v", err)
	}
	root, ok := catalog.Dict["StructTreeRoot"].(ast.PdfIndirectRef)
	if !ok {
		return objects, ""
	}
	rev, err := signed.revision(ctx)
	if err != nil {
		return objects, fmt.Sprintf("; annotation tags not recognized: %v", err)
	}

	t := &tagDiff{
		doc: doc, rev: rev,
		held:     func(num int) bool { return signed.held(ctx, rev, num) },
		root:     root.ObjectNum,
		tagged:   tagged,
		appended: make(map[int]bool, len(objects)),
	}
	for _, obj := range objects {
		if !obj.IsXRef && obj.Unread == "" {
			t.appended[obj.ObjNum] = true
		}
	}
	exempt, err := t.exemptions(ctx, objects)
	if err != nil {
		return objects, fmt.Sprintf("; annotation tags not recognized: %v", err)
	}
	if len(exempt) == 0 {
		return objects, ""
	}
	kept := make([]appendedObject, 0, len(objects))
	for _, obj := range objects {
		if !exempt[obj.ObjNum] {
			kept = append(kept, obj)
		}
	}
	return kept, ""
}

// tagDiff compares the structure tree of the signed revision rev with the
// current one of doc for [withoutAnnotationTags].
type tagDiff struct {
	doc, rev *document.Document
	held     func(num int) bool // reports whether the signed revision held a number ([signedRevision.held])
	root     int                // the structure tree root, the same object in both revisions
	tagged   map[int]string     // annotations the update wrote within policy, to the structure type that tags them
	appended map[int]bool       // the object numbers the update defines
	pages    map[int]bool       // the current pages
}

// annotationTag is a new structure element that tags annotations.
type annotationTag struct {
	parent  int   // the element its /P names
	annots  []int // the annotations its object references name
	objects []int // its /K array and object references written as objects of their own
	keys    []int // the parent tree keys of its annotations
}

// exemptions returns the object numbers among objects that tag the
// annotations the update wrote within policy, as [withoutAnnotationTags]
// describes them.
func (t *tagDiff) exemptions(ctx context.Context, objects []appendedObject) (map[int]bool, error) {
	signedCatalog, err := document.ResolveCatalog(ctx, t.rev)
	if err != nil {
		return nil, fmt.Errorf("reading the signed revision's catalog: %w", err)
	}
	if ref, ok := signedCatalog.Dict["StructTreeRoot"].(ast.PdfIndirectRef); !ok || ref.ObjectNum != t.root {
		// [changedCatalog] reports a structure tree root the update added or
		// replaced.
		return nil, nil
	}
	pages, err := pagesOf(ctx, t.doc)
	if err != nil {
		return nil, fmt.Errorf("reading the current pages: %w", err)
	}
	t.pages = make(map[int]bool, len(pages))
	for _, p := range pages {
		t.pages[p.Ref.ObjectNum] = true
	}

	tags := make(map[int]*annotationTag)
	claimed := make(map[int]bool)
	for i, obj := range objects {
		if i%100 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if obj.IsXRef || obj.Unread != "" || obj.IsAnnot || (obj.Type != "" && obj.Type != "StructElem") {
			continue
		}
		tag := t.newTag(ctx, obj.ObjNum)
		if tag == nil || t.held(obj.ObjNum) || slices.ContainsFunc(tag.annots, func(a int) bool { return claimed[a] }) {
			continue
		}
		for _, a := range tag.annots {
			claimed[a] = true
		}
		tags[obj.ObjNum] = tag
	}
	if len(tags) == 0 {
		return nil, nil
	}

	rootRef := ast.PdfIndirectRef{ObjectNum: t.root}
	signedRoot, err := document.ResolveDict(ctx, t.rev, rootRef)
	if err != nil {
		return nil, fmt.Errorf("reading the signed structure tree root: %w", err)
	}
	currentRoot, err := document.ResolveDict(ctx, t.doc, rootRef)
	if err != nil {
		return nil, fmt.Errorf("reading the current structure tree root: %w", err)
	}
	if signedRoot == nil || currentRoot == nil {
		// A root that is null in either revision holds no tree to add to.
		return nil, nil
	}
	signedTree, err := readParentTree(ctx, t.rev, signedRoot["ParentTree"])
	if err != nil {
		return nil, fmt.Errorf("reading the signed parent tree: %w", err)
	}
	currentTree, err := readParentTree(ctx, t.doc, currentRoot["ParentTree"])
	if err != nil {
		return nil, fmt.Errorf("reading the current parent tree: %w", err)
	}

	// ISO 32000-1, 14.7.4.4: the parent tree maps the /StructParent of an
	// annotation that is a content item to its parent structure element.
	for _, num := range slices.Sorted(maps.Keys(tags)) {
		tag := tags[num]
		for _, annot := range tag.annots {
			key, ok := t.structParent(ctx, annot)
			ref, isRef := currentTree.entries[key].(ast.PdfIndirectRef)
			if !ok || !isRef || ref.ObjectNum != num {
				delete(tags, num)
				break
			}
			tag.keys = append(tag.keys, key)
		}
	}

	byParent := make(map[int][]int)
	for num, tag := range tags {
		byParent[tag.parent] = append(byParent[tag.parent], num)
	}
	if _, ok := byParent[t.root]; !ok && t.appended[t.root] {
		byParent[t.root] = nil
	}
	exempt := make(map[int]bool)
	for _, parent := range slices.Sorted(maps.Keys(byParent)) {
		kids := byParent[parent]
		changed, ok, err := t.inserted(ctx, parent, kids, signedRoot, currentRoot)
		if err != nil {
			return nil, err
		}
		if !ok {
			for _, kid := range kids {
				delete(tags, kid)
			}
			continue
		}
		for _, num := range changed {
			exempt[num] = true
		}
	}

	confined := t.parentTreeConfined(ctx, signedTree, currentTree, tags)
	if confined {
		for num := range currentTree.nodes {
			if t.appended[num] {
				exempt[num] = true
			}
		}
	} else if !sameObject(signedRoot["ParentTree"], currentRoot["ParentTree"]) {
		// The root may point at another parent tree only when that tree
		// keeps the signed entries.
		delete(exempt, t.root)
	}
	for num, tag := range tags {
		exempt[num] = true
		for _, obj := range tag.objects {
			exempt[obj] = true
		}
	}

	// A structure object the update writes again as it was signed changes
	// nothing, and a writer that tags an annotation may rewrite the tree
	// around it.
	for _, obj := range objects {
		if structureTypes[obj.Type] && !exempt[obj.ObjNum] && sameDefinition(ctx, t.doc, t.rev, obj.ObjNum) {
			exempt[obj.ObjNum] = true
		}
	}
	return exempt, nil
}

// structureTypes are the /Type values of the objects a structure tree is built
// from (ISO 32000-1, 14.7.2, Tables 322 and 323; 14.7.4.2, Table 324; 14.7.4.3,
// Table 325).
var structureTypes = map[string]bool{"StructTreeRoot": true, "StructElem": true, "MCR": true, "OBJR": true}

// newTag returns the new structure element num of the current document when
// it tags only annotations of [tagDiff.tagged], each with its structure type,
// and carries nothing but [annotationTagEntries], or nil when it does not.
func (t *tagDiff) newTag(ctx context.Context, num int) *annotationTag {
	dict, err := document.ResolveDict(ctx, t.doc, ast.PdfIndirectRef{ObjectNum: num})
	if err != nil || dict == nil || !isStructElem(dict) {
		return nil
	}
	for key := range dict {
		if !annotationTagEntries[key] {
			return nil
		}
	}
	parent, ok := dict["P"].(ast.PdfIndirectRef)
	if !ok || !t.held(parent.ObjectNum) {
		return nil
	}
	if pg, ok := dict["Pg"]; ok && !t.isPage(pg) {
		return nil
	}
	if ns, ok := dict["NS"]; ok && !t.standardNamespace(ctx, ns) {
		return nil
	}

	tag := &annotationTag{parent: parent.ObjectNum}
	kids := dict["K"]
	if ref, ok := kids.(ast.PdfIndirectRef); ok {
		resolved, err := document.ResolveObject(ctx, t.doc, ref)
		if err != nil {
			return nil
		}
		if _, isArray := resolved.(ast.PdfArray); isArray {
			if !t.isNew(ref.ObjectNum) {
				return nil
			}
			tag.objects = append(tag.objects, ref.ObjectNum)
			kids = resolved
		}
	}
	list, ok := kids.(ast.PdfArray)
	if !ok && kids != nil {
		list = ast.PdfArray{kids}
	}
	if len(list) == 0 {
		return nil
	}
	s := dictName(dict, "S")
	for _, kid := range list {
		annot, objr, ok := t.objectReference(ctx, kid, s)
		if !ok {
			return nil
		}
		tag.annots = append(tag.annots, annot)
		if objr != 0 {
			tag.objects = append(tag.objects, objr)
		}
	}
	return tag
}

// objectReference reads kid, an entry of a new element's /K, as an object
// reference (ISO 32000-1, 14.7.4.3, Table 325) to an annotation of
// [tagDiff.tagged] that structure type s tags. It returns the annotation, the
// number of the object reference when it is an object of its own, which must
// be new, and false when kid is anything else.
func (t *tagDiff) objectReference(ctx context.Context, kid ast.PdfObject, s string) (annot, objr int, ok bool) {
	if ref, isRef := kid.(ast.PdfIndirectRef); isRef {
		if !t.isNew(ref.ObjectNum) {
			return 0, 0, false
		}
		objr = ref.ObjectNum
	}
	dict, err := document.ResolveDict(ctx, t.doc, kid)
	if err != nil || dict == nil || dictName(dict, "Type") != "OBJR" {
		return 0, 0, false
	}
	for key := range dict {
		if key != "Type" && key != "Obj" && key != "Pg" {
			return 0, 0, false
		}
	}
	obj, isRef := dict["Obj"].(ast.PdfIndirectRef)
	if !isRef || t.tagged[obj.ObjectNum] != s {
		return 0, 0, false
	}
	if pg, has := dict["Pg"]; has && !t.isPage(pg) {
		return 0, 0, false
	}
	return obj.ObjectNum, objr, true
}

// inserted reports whether the element parent, or the structure tree root,
// changed from the signed revision only by gaining the new elements kids among
// its own, and returns the objects that change consists of: the element when
// the update rewrote it and its /K array when the update wrote that.
// signedRoot and currentRoot are the structure tree root in both revisions.
func (t *tagDiff) inserted(ctx context.Context, parent int, kids []int, signedRoot, currentRoot ast.PdfDict) ([]int, bool, error) {
	isRoot := parent == t.root
	before, after := signedRoot, currentRoot
	if !isRoot {
		ref := ast.PdfIndirectRef{ObjectNum: parent}
		var err error
		if before, err = document.ResolveDict(ctx, t.rev, ref); err != nil || before == nil || !isStructElem(before) {
			return nil, false, ctx.Err()
		}
		if after, err = document.ResolveDict(ctx, t.doc, ref); err != nil || after == nil || !isStructElem(after) {
			return nil, false, ctx.Err()
		}
	}
	confined := dictConfined(before, after, func(key ast.PdfName, _, _ ast.PdfObject) bool {
		switch key {
		case "K":
			return true
		case "ParentTree", "ParentTreeNextKey":
			return isRoot
		}
		return false
	})
	if !confined {
		return nil, false, nil
	}

	signedKids, err := kidList(ctx, t.rev, before["K"])
	if err != nil {
		return nil, false, ctx.Err()
	}
	currentKids, err := kidList(ctx, t.doc, after["K"])
	if err != nil {
		return nil, false, ctx.Err()
	}
	added := make(map[int]bool, len(kids))
	for _, kid := range kids {
		added[kid] = true
	}
	rest := make(ast.PdfArray, 0, len(currentKids))
	found := 0
	for _, kid := range currentKids {
		if ref, ok := kid.(ast.PdfIndirectRef); ok && added[ref.ObjectNum] {
			found++
			continue
		}
		rest = append(rest, kid)
	}
	if found != len(kids) || !sameObject(signedKids, rest) {
		return nil, false, nil
	}

	var changed []int
	if t.appended[parent] {
		changed = append(changed, parent)
	}
	if ref, ok := after["K"].(ast.PdfIndirectRef); ok && t.appended[ref.ObjectNum] {
		if resolved, err := document.ResolveObject(ctx, t.doc, ref); err == nil {
			if _, isArray := resolved.(ast.PdfArray); isArray {
				changed = append(changed, ref.ObjectNum)
			}
		}
	}
	return changed, true, nil
}

// parentTreeConfined reports whether the current parent tree keeps every
// entry of the signed one, each value an array of the same entries or the same
// object, and adds only the keys of tags, each to the element that holds it.
func (t *tagDiff) parentTreeConfined(ctx context.Context, signed, current *parentTree, tags map[int]*annotationTag) bool {
	if signed.duplicate || current.duplicate {
		return false
	}
	added := make(map[int]int)
	for num, tag := range tags {
		for _, key := range tag.keys {
			added[key] = num
		}
	}
	for i, key := range slices.Sorted(maps.Keys(signed.entries)) {
		if i%100 == 0 && ctx.Err() != nil {
			return false
		}
		value, ok := current.entries[key]
		if !ok || !t.sameEntry(ctx, signed.entries[key], value) {
			return false
		}
	}
	for key, value := range current.entries {
		if _, ok := signed.entries[key]; ok {
			continue
		}
		ref, isRef := value.(ast.PdfIndirectRef)
		num, isAdded := added[key]
		if !isRef || !isAdded || ref.ObjectNum != num {
			return false
		}
	}
	return true
}

// sameEntry reports whether a parent tree value is the same in both
// revisions. A value that names an array, the parent elements of a page's
// marked content (ISO 32000-1, 14.7.4.4), is compared by its entries, so a
// rewritten array is a changed value. A value that names an element is
// compared by reference: what the element holds is compared where it changed.
func (t *tagDiff) sameEntry(ctx context.Context, before, after ast.PdfObject) bool {
	if !sameObject(before, after) {
		return false
	}
	if _, isRef := before.(ast.PdfIndirectRef); !isRef {
		return true
	}
	b, err := document.ResolveObject(ctx, t.rev, before)
	if err != nil {
		return false
	}
	a, err := document.ResolveObject(ctx, t.doc, after)
	if err != nil {
		return false
	}
	beforeArray, isArray := b.(ast.PdfArray)
	if !isArray {
		_, afterIsArray := a.(ast.PdfArray)
		return !afterIsArray
	}
	return sameObject(beforeArray, a)
}

// structParent returns the /StructParent key of the current annotation num.
func (t *tagDiff) structParent(ctx context.Context, num int) (int, bool) {
	annot, err := document.ResolveDict(ctx, t.doc, ast.PdfIndirectRef{ObjectNum: num})
	if err != nil || annot == nil {
		return 0, false
	}
	key, err := document.ResolveObject(ctx, t.doc, annot["StructParent"])
	if v, ok := key.(ast.PdfInt); err == nil && ok {
		return intconv.Clamp(v), true
	}
	return 0, false
}

// isNew reports whether the update defines num and the signed revision held
// no such number.
func (t *tagDiff) isNew(num int) bool {
	return t.appended[num] && !t.held(num)
}

// isPage reports whether obj references a page of the current page tree.
func (t *tagDiff) isPage(obj ast.PdfObject) bool {
	ref, ok := obj.(ast.PdfIndirectRef)
	return ok && t.pages[ref.ObjectNum]
}

// standardNamespace reports whether obj references a namespace dictionary
// (ISO 32000-2, 14.7.4.2) the signed revision held that names one of the
// [standardNamespaces].
func (t *tagDiff) standardNamespace(ctx context.Context, obj ast.PdfObject) bool {
	ref, ok := obj.(ast.PdfIndirectRef)
	if !ok || !t.rev.DefinesObject(ref.ObjectNum) {
		return false
	}
	ns, err := document.ResolveDict(ctx, t.doc, ref)
	if err != nil || ns == nil {
		return false
	}
	uri, err := document.ResolveString(ctx, t.doc, ns["NS"])
	return err == nil && standardNamespaces[document.DecodePdfString(uri)]
}

// isStructElem reports whether dict is a structure element: it names a
// structure type and, if it states a type, StructElem (ISO 32000-1, 14.7.2,
// Table 323).
func isStructElem(dict ast.PdfDict) bool {
	typ := dictName(dict, "Type")
	return dictName(dict, "S") != "" && (typ == "" || typ == "StructElem")
}

// kidList returns the kids a /K entry names in doc: the array it is or
// references, or the single kid it is otherwise, as written.
func kidList(ctx context.Context, doc *document.Document, kids ast.PdfObject) (ast.PdfArray, error) {
	if kids == nil {
		return ast.PdfArray{}, nil
	}
	resolved, err := document.ResolveObject(ctx, doc, kids)
	if err != nil {
		return nil, fmt.Errorf("resolving /K: %w", err)
	}
	if arr, ok := resolved.(ast.PdfArray); ok {
		return arr, nil
	}
	return ast.PdfArray{kids}, nil
}

// parentTree is the content of a structure tree's /ParentTree.
type parentTree struct {
	entries   map[int]ast.PdfObject // keys to their values, as written
	nodes     map[int]bool          // the objects the number tree is built from
	duplicate bool                  // a key is listed twice
}

// readParentTree reads the parent tree obj names in doc. An absent tree holds
// nothing.
func readParentTree(ctx context.Context, doc *document.Document, obj ast.PdfObject) (*parentTree, error) {
	tree := &parentTree{entries: make(map[int]ast.PdfObject), nodes: make(map[int]bool)}
	if obj == nil {
		return tree, nil
	}
	err := document.WalkNumberTree(ctx, doc, obj,
		func(num int) { tree.nodes[num] = true },
		func(key int, value ast.PdfObject) {
			if _, dup := tree.entries[key]; dup {
				tree.duplicate = true
				return
			}
			tree.entries[key] = value
		},
	)
	if err != nil {
		return nil, err
	}
	return tree, nil
}
