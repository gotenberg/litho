package mutations

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// Field flags of a button field (ISO 32000-1, Table 226): bit 16 is Radio and
// bit 17 is Pushbutton. A button with neither is a check box.
const (
	buttonFlagRadio      = 1 << 15
	buttonFlagPushbutton = 1 << 16
)

// flattenTags keeps a tagged document's structure tree in step with the widgets
// [FlattenForm] removes. A nil *flattenTags stands for an untagged document,
// and every method is a no-op on it.
//
// ISO 14289-1, 7.1 and ISO 14289-2, 8.2.2 require every piece of page content
// to be tagged as real content or marked as an artifact. A widget is not page
// content: it reaches the structure tree as an object reference (ISO 32000-1,
// 14.7.4.3), which ISO 14289-1, 7.18.4 places under a Form element. Flattening
// turns its appearance into page content, so the content needs an owner:
//
//   - A widget a structure element encloses hands that element its content.
//     The element's object reference becomes a marked-content reference to the
//     flattened appearance, which keeps the field where the author put it in
//     the logical structure and the reading order.
//   - A Form element that now holds content instead of its widget carries a
//     PrintField /Role attribute. ISO 32000-1, 14.8.4.5, Table 340 lets a Form
//     element with a Role attribute hold content items representing the value
//     of a non-interactive field, and requires one without it to hold exactly
//     one widget reference, which is ISO 14289-1, 7.18.4 test 2. The role comes
//     from the field type (ISO 32000-1, 14.8.5.6, Table 348).
//   - A widget no structure element encloses was outside the logical structure,
//     so its appearance is marked as an artifact (ISO 32000-1, 14.8.2.2). Tagging
//     it would put the value somewhere in a reading order the author never gave
//     it a place in.
//
// A widget that draws nothing leaves the tree with its object reference, and an
// element left with no kid at all goes with it: it describes nothing, and an
// empty Form element fails ISO 14289-1, 7.18.4 test 2 where the widget it held
// did not.
//
// The /ParentTree is read whole before anything changes. Each page's marked
// content needs an entry there naming its owner (ISO 32000-1, 14.7.4.4), and the
// tree is written back as a single node listing every key. A tree that cannot be
// read whole is left as it was, and every appearance is marked as an artifact. A
// page whose marked-content identifiers cannot be read has its appearances
// marked as artifacts too, because a guessed identifier could take over one the
// page already uses.
//
// # Corpus evidence
//
//   - litho annotstruct_ua2, formstruct_ua2, idfields_ua2: flattening added ISO
//     14289-1, 7.1 test 3 and ISO 14289-2, 8.2.2 test 1, 6 cells veraPDF
//     confirmed. With the appearances tagged or marked, veraPDF reports a subset
//     of the input's rules on each at both profiles.
//   - All corpora, 617 files that mention a form: the flattened output of 43
//     tagged files no longer fails ISO 14289-1, 7.1 test 3 or ISO 14289-2,
//     8.2.2 test 1, and no output fails a PDF/UA rule the untagged flatten
//     passed.
type flattenTags struct {
	catalogRef ast.PdfIndirectRef
	// rootRef is the structure tree root's object; zero when the catalog holds
	// the root directly.
	rootRef ast.PdfIndirectRef
	root    ast.PdfDict

	// loaded is set once the parent tree and the object references are read,
	// which happens on the first widget the flatten removes.
	loaded bool
	// usable is false when the parent tree could not be read whole.
	usable     bool
	parentTree map[int]ast.PdfObject
	nextKey    int
	keysAdded  bool
	// objrOwners maps a widget's object number to the elements whose /K holds
	// an object reference to it.
	objrOwners map[int][]ast.PdfIndirectRef

	// kids maps an element to the widgets whose object references it loses,
	// each to the kid that replaces it, nil for none.
	kids map[int]map[int]ast.PdfObject
	// roles maps a Form element that gains content to the role it takes.
	roles map[int]ast.PdfName
	// droppedKeys are the /StructParent keys of the widgets removed.
	droppedKeys map[int]bool
	pages       map[int]*flattenPageTags
	// inlineWidgets counts down the keys [flattenTags.replaceKid] gives widgets
	// written directly into /Annots.
	inlineWidgets int
}

// flattenPageTags is one page's share of [flattenTags].
type flattenPageTags struct {
	// ok is false when the page's marked content cannot be extended.
	ok bool
	// key is the page's /StructParents; newKey is set when the flatten assigned
	// it.
	key    int
	newKey bool
	// entries is the page's /ParentTree array, indexed by MCID.
	entries  ast.PdfArray
	nextMCID int
	changed  bool
}

// newFlattenTags returns the tagging state for the document, nil when it has no
// structure tree.
func newFlattenTags(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalogRef ast.PdfIndirectRef, catalogDict ast.PdfDict) *flattenTags {
	rootObj, ok := catalogDict["StructTreeRoot"]
	if !ok {
		return nil
	}
	root, err := resolveDictAllocFirst(ctx, alloc, doc, rootObj)
	if err != nil || root == nil {
		return nil
	}
	t := &flattenTags{
		catalogRef:  catalogRef,
		root:        root,
		kids:        make(map[int]map[int]ast.PdfObject),
		roles:       make(map[int]ast.PdfName),
		droppedKeys: make(map[int]bool),
		pages:       make(map[int]*flattenPageTags),
	}
	if ref, isRef := rootObj.(ast.PdfIndirectRef); isRef {
		t.rootRef = ref
	}
	return t
}

// artifactTags returns tagging state that marks every drawn appearance as an
// artifact and edits no structure tree: the state for content that goes into a
// tagged document which carries no structure element for it, where no
// element encloses the widget (ISO 32000-1, 14.8.2.2).
func artifactTags() *flattenTags {
	return &flattenTags{loaded: true}
}

// load reads the parent tree and the object references the first time a widget
// needs them.
func (t *flattenTags) load(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) {
	if t.loaded {
		return
	}
	t.loaded = true

	entries, maxKey, ok := readParentTree(ctx, alloc, doc, t.root["ParentTree"])
	if !ok {
		doc.Logger.Debug("flatten: the /ParentTree cannot be read whole, marking flattened appearances as artifacts")
		return
	}
	t.usable = true
	t.parentTree = entries
	t.nextKey = maxKey + 1
	if next, ok := intEntry(ctx, doc, t.root["ParentTreeNextKey"]); ok && next > t.nextKey {
		t.nextKey = next
	}

	t.objrOwners = make(map[int][]ast.PdfIndirectRef)
	collectObjectRefOwners(ctx, alloc, doc, t.root["K"], ast.PdfIndirectRef{}, t.objrOwners, make(map[int]bool), 0)
}

// bake records that the widget's appearance is drawn into page, and returns the
// operators that open and close the marked content around it.
func (t *flattenTags) bake(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, page *document.Page, annotObj ast.PdfObject, annotDict ast.PdfDict) (string, string) {
	if t == nil {
		return "", ""
	}
	const artifactOpen, artifactClose = "/Artifact BMC ", " EMC"
	t.load(ctx, alloc, doc)
	if !t.usable {
		return artifactOpen, artifactClose
	}

	ownerRef, owner, found := t.owner(ctx, alloc, doc, annotObj, annotDict)
	var ps *flattenPageTags
	if found {
		ps = t.pageTags(ctx, alloc, doc, page)
	}
	if !found || !ps.ok {
		t.detach(ctx, doc, annotObj, annotDict)
		return artifactOpen, artifactClose
	}

	var tag bytes.Buffer
	s, _ := owner["S"].(ast.PdfName)
	if s == "" {
		s = "Form"
	}
	if _, err := writer.SerializeName(&tag, s); err != nil {
		// A structure type with no PDF spelling cannot name the marked content.
		// The tag need not match the element (ISO 32000-1, 14.7.4.2): the MCID
		// is what ties the two.
		tag.Reset()
		tag.WriteString("/Form")
	}

	mcid := ps.nextMCID
	ps.nextMCID++
	for len(ps.entries) <= mcid {
		ps.entries = append(ps.entries, ast.PdfNull{})
	}
	ps.entries[mcid] = ownerRef
	ps.changed = true

	// A bare integer kid is marked content on the element's own page (ISO
	// 32000-1, Table 323); an element on another page names this one.
	var kid ast.PdfObject = ast.PdfInt(mcid)
	if pg, _ := owner["Pg"].(ast.PdfIndirectRef); pg.ObjectNum != page.Ref.ObjectNum {
		kid = ast.PdfDict{"Type": ast.PdfName("MCR"), "Pg": page.Ref, "MCID": ast.PdfInt(mcid)}
	}
	t.detach(ctx, doc, annotObj, annotDict)
	t.replaceKid(ownerRef, annotObj, kid)
	if isFormElement(ctx, alloc, doc, t.root, owner) {
		if _, set := t.roles[ownerRef.ObjectNum]; !set {
			t.roles[ownerRef.ObjectNum] = fieldRole(ctx, doc, annotDict)
		}
	}
	return fmt.Sprintf("%s <</MCID %d>> BDC ", tag.String(), mcid), " EMC"
}

// drop records that the widget leaves without drawing anything.
func (t *flattenTags) drop(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, annotObj ast.PdfObject, annotDict ast.PdfDict) {
	if t == nil {
		return
	}
	t.load(ctx, alloc, doc)
	if !t.usable {
		return
	}
	t.detach(ctx, doc, annotObj, annotDict)
}

// detach removes every object reference to the widget and its /ParentTree key.
func (t *flattenTags) detach(ctx context.Context, doc *document.Document, annotObj ast.PdfObject, annotDict ast.PdfDict) {
	if key, ok := intEntry(ctx, doc, annotDict["StructParent"]); ok {
		t.droppedKeys[key] = true
	}
	ref, isRef := annotObj.(ast.PdfIndirectRef)
	if !isRef {
		return
	}
	for _, owner := range t.objrOwners[ref.ObjectNum] {
		t.replaceKid(owner, annotObj, nil)
	}
}

// replaceKid records that owner's object reference to the widget becomes kid,
// or goes when kid is nil. A kid recorded before is not undone by a later nil.
func (t *flattenTags) replaceKid(owner ast.PdfIndirectRef, annotObj ast.PdfObject, kid ast.PdfObject) {
	var key int
	if ref, isRef := annotObj.(ast.PdfIndirectRef); isRef {
		key = ref.ObjectNum
	} else {
		// An inlined widget has no object number an /OBJR could name, but its
		// content still needs the owner to list it. A negative key stands for
		// it, one per widget, so two such widgets keep a kid each.
		t.inlineWidgets--
		key = t.inlineWidgets
	}
	byWidget, ok := t.kids[owner.ObjectNum]
	if !ok {
		byWidget = make(map[int]ast.PdfObject)
		t.kids[owner.ObjectNum] = byWidget
	}
	if kid == nil {
		if _, set := byWidget[key]; set {
			return
		}
	}
	byWidget[key] = kid
}

// owner returns the structure element enclosing the widget: the one its
// /StructParent names in the /ParentTree (ISO 32000-1, 14.7.4.4), or else the
// first whose /K holds an object reference to it.
func (t *flattenTags) owner(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, annotObj ast.PdfObject, annotDict ast.PdfDict) (ast.PdfIndirectRef, ast.PdfDict, bool) {
	var candidates []ast.PdfIndirectRef
	if key, ok := intEntry(ctx, doc, annotDict["StructParent"]); ok {
		if ref, isRef := t.parentTree[key].(ast.PdfIndirectRef); isRef {
			candidates = append(candidates, ref)
		}
	}
	if ref, isRef := annotObj.(ast.PdfIndirectRef); isRef {
		candidates = append(candidates, t.objrOwners[ref.ObjectNum]...)
	}
	for _, ref := range candidates {
		if t.rootRef.ObjectNum != 0 && ref.ObjectNum == t.rootRef.ObjectNum {
			continue
		}
		elem, err := resolveDictAllocFirst(ctx, alloc, doc, ref)
		if err != nil || elem == nil {
			continue
		}
		if _, isElement := elem["S"].(ast.PdfName); !isElement {
			continue
		}
		return ref, elem, true
	}
	return ast.PdfIndirectRef{}, nil, false
}

// pageTags returns the page's tagging state, reading its /StructParents entry
// and the highest MCID its content uses the first time.
func (t *flattenTags) pageTags(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, page *document.Page) *flattenPageTags {
	if ps, ok := t.pages[page.Ref.ObjectNum]; ok {
		return ps
	}
	ps := &flattenPageTags{}
	t.pages[page.Ref.ObjectNum] = ps
	if page.Ref.ObjectNum == 0 {
		// An inlined page has no object number to write /StructParents on.
		return ps
	}

	pageDict := latestPageDict(alloc, page)
	if _, has := pageDict["StructParents"]; has {
		key, ok := intEntry(ctx, doc, pageDict["StructParents"])
		if !ok {
			return ps
		}
		ps.key = key
		if value, listed := t.parentTree[key]; listed {
			entries, err := resolveArrayAllocFirst(ctx, alloc, doc, value)
			if err != nil {
				// A page's entry is an array of elements (ISO 32000-1,
				// 14.7.4.4); anything else leaves no place for a new MCID.
				return ps
			}
			ps.entries = slices.Clone(entries)
		}
	} else {
		ps.key = t.nextKey
		ps.newKey = true
	}

	maxMCID, err := pageMaxMCID(ctx, alloc, doc, page)
	if err != nil {
		doc.Logger.Debug("flatten: the page's marked content cannot be read, marking its flattened appearances as artifacts",
			slog.Int("page", page.Ref.ObjectNum), slog.Any("err", err))
		return ps
	}
	if ps.newKey {
		t.nextKey++
		t.keysAdded = true
	}
	ps.nextMCID = max(len(ps.entries), maxMCID+1)
	ps.ok = true
	return ps
}

// structParents returns the /StructParents key the flatten gave the page, if it
// gave it one.
func (t *flattenTags) structParents(page *document.Page) (ast.PdfInt, bool) {
	if t == nil {
		return 0, false
	}
	ps, ok := t.pages[page.Ref.ObjectNum]
	if !ok || !ps.ok || !ps.newKey || !ps.changed {
		return 0, false
	}
	return ast.PdfInt(ps.key), true
}

// commit writes the elements, the parent tree and the root the flatten changed.
func (t *flattenTags) commit(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) error {
	if t == nil || !t.usable {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("flattening the structure tree: %w", err)
	}

	emptied := t.rewriteOwners(ctx, alloc, doc)

	root := make(ast.PdfDict, len(t.root)+1)
	maps.Copy(root, t.root)
	rootChanged := false

	// An element emptied by the flatten leaves its parent's /K and every entry
	// of the parent tree that names it.
	gone := make(map[int]bool, len(emptied))
	for _, ref := range emptied {
		gone[ref.ObjectNum] = true
	}
	for _, ref := range emptied {
		elem, err := resolveDictAllocFirst(ctx, alloc, doc, ref)
		if err != nil || elem == nil {
			continue
		}
		parentRef, isRef := elem["P"].(ast.PdfIndirectRef)
		if !isRef {
			continue
		}
		if t.rootRef.ObjectNum != 0 && parentRef.ObjectNum == t.rootRef.ObjectNum {
			if kids, changed := withoutKids(ctx, alloc, doc, root["K"], gone); changed {
				root["K"] = kids
				rootChanged = true
			}
			continue
		}
		parent, err := resolveDictAllocFirst(ctx, alloc, doc, parentRef)
		if err != nil || parent == nil {
			continue
		}
		if kids, changed := withoutKids(ctx, alloc, doc, parent["K"], gone); changed {
			updated := make(ast.PdfDict, len(parent))
			maps.Copy(updated, parent)
			updated["K"] = kids
			alloc.AllocAt(parentRef.ObjectNum, updated)
		}
	}

	if tree, changed := t.rewriteParentTree(ctx, alloc, doc, gone); changed {
		switch ref := t.root["ParentTree"].(type) {
		case ast.PdfIndirectRef:
			alloc.AllocAt(ref.ObjectNum, tree)
		case nil:
			root["ParentTree"] = alloc.Alloc(tree)
			rootChanged = true
		default:
			root["ParentTree"] = tree
			rootChanged = true
		}
	}
	if t.keysAdded {
		root["ParentTreeNextKey"] = ast.PdfInt(t.nextKey)
		rootChanged = true
	}
	if !rootChanged {
		return nil
	}
	if t.rootRef.ObjectNum != 0 {
		alloc.AllocAt(t.rootRef.ObjectNum, root)
		return nil
	}
	catalog, err := LiveCatalogDict(ctx, alloc, doc, t.catalogRef)
	if err != nil {
		return fmt.Errorf("rewriting the catalog's structure tree root: %w", err)
	}
	updated := make(ast.PdfDict, len(catalog))
	maps.Copy(updated, catalog)
	updated["StructTreeRoot"] = root
	alloc.AllocAt(t.catalogRef.ObjectNum, updated)
	return nil
}

// rewriteOwners writes each element whose kids the flatten changed, and returns
// the ones it left with no kid, in object-number order.
func (t *flattenTags) rewriteOwners(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) []ast.PdfIndirectRef {
	owners := slices.Sorted(maps.Keys(t.kids))
	var emptied []ast.PdfIndirectRef
	for _, num := range owners {
		ref := ast.PdfIndirectRef{ObjectNum: num}
		elem, err := resolveDictAllocFirst(ctx, alloc, doc, ref)
		if err != nil || elem == nil {
			continue
		}
		replacements := t.kids[num]
		before := structKidList(ctx, alloc, doc, elem["K"])
		after := make(ast.PdfArray, 0, len(before)+len(replacements))
		placed := make(map[int]bool, len(replacements))
		for _, kid := range before {
			widget, isObjRef := objectRefTarget(ctx, alloc, doc, kid)
			replacement, replaced := replacements[widget]
			if !isObjRef || !replaced {
				after = append(after, kid)
				continue
			}
			placed[widget] = true
			if replacement != nil {
				after = append(after, replacement)
			}
		}
		// The element the parent tree names may never list the widget back in
		// /K. The content still needs it to.
		for _, widget := range slices.Sorted(maps.Keys(replacements)) {
			if replacement := replacements[widget]; replacement != nil && !placed[widget] {
				after = append(after, replacement)
			}
		}

		updated := make(ast.PdfDict, len(elem)+1)
		maps.Copy(updated, elem)
		if len(after) == 0 {
			delete(updated, "K")
		} else {
			updated["K"] = after
		}
		if role, ok := t.roles[num]; ok {
			updated["A"] = withPrintFieldRole(ctx, alloc, doc, elem["A"], role)
		}
		alloc.AllocAt(num, updated)
		if len(after) == 0 && len(before) > 0 {
			emptied = append(emptied, ref)
		}
	}
	return emptied
}

// rewriteParentTree returns the parent tree as one node once the flatten's keys
// are in and its dropped ones out, and whether anything changed.
func (t *flattenTags) rewriteParentTree(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, gone map[int]bool) (ast.PdfDict, bool) {
	changed := false
	tree := maps.Clone(t.parentTree)
	for key := range t.droppedKeys {
		value, listed := tree[key]
		if !listed {
			continue
		}
		// A page's entry is an array; an annotation's names one element (ISO
		// 32000-1, 14.7.4.4). A widget's key that a page's content also uses
		// keeps the page's entry.
		if _, err := resolveArrayAllocFirst(ctx, alloc, doc, value); err == nil {
			continue
		}
		delete(tree, key)
		changed = true
	}
	for _, num := range slices.Sorted(maps.Keys(t.pages)) {
		ps := t.pages[num]
		if ps.ok && ps.changed {
			tree[ps.key] = ps.entries
			changed = true
		}
	}
	for key, value := range tree {
		if ref, isRef := value.(ast.PdfIndirectRef); isRef && gone[ref.ObjectNum] {
			delete(tree, key)
			changed = true
		}
	}
	if !changed {
		return nil, false
	}
	nums := make(ast.PdfArray, 0, 2*len(tree))
	for _, key := range slices.Sorted(maps.Keys(tree)) {
		nums = append(nums, ast.PdfInt(key), tree[key])
	}
	return ast.PdfDict{"Nums": nums}, true
}

// readParentTree returns every key and value of the number tree rooted at obj
// (ISO 32000-1, 7.9.7), read from alloc first, and its highest key, -1 when it
// has none. ok is false when the tree cannot be read whole: a node that does not
// resolve, a /Nums array of odd length or with a key that is not an integer, or
// a descent past [maxStructTreeDepth]. An absent tree reads as an empty one.
// The walk reads each node and each /Kids array once.
func readParentTree(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject) (map[int]ast.PdfObject, int, bool) {
	entries := make(map[int]ast.PdfObject)
	maxKey := -1
	if obj == nil {
		return entries, maxKey, true
	}
	visited := make(map[int]bool)
	var walk func(node ast.PdfObject, depth int) bool
	walk = func(node ast.PdfObject, depth int) bool {
		if depth > maxStructTreeDepth || ctx.Err() != nil {
			return false
		}
		if ref, isRef := node.(ast.PdfIndirectRef); isRef {
			if visited[ref.ObjectNum] {
				// A strict tree names each node once. A repeat adds nothing the
				// first visit did not read.
				return true
			}
			visited[ref.ObjectNum] = true
		}
		dict, err := resolveDictAllocFirst(ctx, alloc, doc, node)
		if err != nil || dict == nil {
			return false
		}
		if numsObj, has := dict["Nums"]; has {
			nums, err := resolveArrayAllocFirst(ctx, alloc, doc, numsObj)
			if err != nil || len(nums)%2 != 0 {
				return false
			}
			for i := 0; i < len(nums); i += 2 {
				key, isInt := intEntry(ctx, doc, nums[i])
				if !isInt {
					return false
				}
				// The first value listed for a key is the one a lookup finds
				// ([document.LookupNumberTree]).
				if _, seen := entries[key]; !seen {
					entries[key] = nums[i+1]
				}
				maxKey = max(maxKey, key)
			}
		}
		if kidsObj, has := dict["Kids"]; has {
			// A /Kids array written as an object of its own can be named by
			// several direct nodes, and re-read per node it fans out 2^depth
			// (see [document.VisitedBefore]). A repeat adds nothing the first
			// read did not.
			if document.VisitedBefore(kidsObj, visited) {
				return true
			}
			kids, err := resolveArrayAllocFirst(ctx, alloc, doc, kidsObj)
			if err != nil {
				return false
			}
			for _, kid := range kids {
				if !walk(kid, depth+1) {
					return false
				}
			}
		}
		return true
	}
	if !walk(obj, 0) {
		return nil, 0, false
	}
	return entries, maxKey, true
}

// intEntry returns obj resolved as an integer, and false when it is absent or
// anything else. [document.ResolveInt] reads an absent or null value as 0, which
// would make every missing key the key 0.
func intEntry(ctx context.Context, doc *document.Document, obj ast.PdfObject) (int, bool) {
	if obj == nil {
		return 0, false
	}
	resolved, err := document.ResolveObject(ctx, doc, obj)
	if err != nil {
		return 0, false
	}
	i, ok := resolved.(ast.PdfInt)
	return int(i), ok
}

// collectObjectRefOwners records, for each object an /OBJR kid references (ISO
// 32000-1, 14.7.4.3), the indirect elements whose /K holds it. An element
// written directly into its parent has no object number to rewrite, so it is not
// recorded. The walk visits each indirect object once and stops at
// [maxStructTreeDepth].
func collectObjectRefOwners(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	kObj ast.PdfObject,
	owner ast.PdfIndirectRef,
	out map[int][]ast.PdfIndirectRef,
	visited map[int]bool,
	depth int,
) {
	if kObj == nil || depth > maxStructTreeDepth || ctx.Err() != nil {
		return
	}
	ref, isRef := kObj.(ast.PdfIndirectRef)
	if isRef {
		if visited[ref.ObjectNum] {
			return
		}
		visited[ref.ObjectNum] = true
	}
	resolved, err := resolveAllocFirst(ctx, alloc, doc, kObj)
	if err != nil {
		return
	}
	switch v := resolved.(type) {
	case ast.PdfArray:
		for _, kid := range v {
			collectObjectRefOwners(ctx, alloc, doc, kid, owner, out, visited, depth)
		}
	case ast.PdfDict:
		switch kind, _ := v["Type"].(ast.PdfName); kind {
		case "OBJR":
			target, isTarget := v["Obj"].(ast.PdfIndirectRef)
			if isTarget && owner.ObjectNum != 0 && !slices.Contains(out[target.ObjectNum], owner) {
				out[target.ObjectNum] = append(out[target.ObjectNum], owner)
			}
		case "MCR":
		default:
			elemOwner := ast.PdfIndirectRef{}
			if isRef {
				elemOwner = ref
			}
			collectObjectRefOwners(ctx, alloc, doc, v["K"], elemOwner, out, visited, depth+1)
		}
	}
}

// structKidList returns an element's /K as a list: the array it holds, or the
// single kid it names (ISO 32000-1, Table 323).
func structKidList(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, kObj ast.PdfObject) ast.PdfArray {
	if kObj == nil {
		return nil
	}
	if arr, err := resolveArrayAllocFirst(ctx, alloc, doc, kObj); err == nil {
		return slices.Clone(arr)
	}
	return ast.PdfArray{kObj}
}

// structKidListOnce is [structKidList] reading a /K array written as an object
// of its own once: it returns nil for one visited already holds, and marks it
// otherwise ([document.VisitedBefore]). A walk down /K reads its arrays this
// way: the direct elements such an array holds can each name it again as their
// /K, and read anew wherever it is reached it fans out 2^depth.
func structKidListOnce(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, kObj ast.PdfObject, visited map[int]bool) ast.PdfArray {
	if kObj == nil {
		return nil
	}
	if arr, err := resolveArrayAllocFirst(ctx, alloc, doc, kObj); err == nil {
		if document.VisitedBefore(kObj, visited) {
			return nil
		}
		return slices.Clone(arr)
	}
	return ast.PdfArray{kObj}
}

// objectRefTarget returns the object number an /OBJR kid references, and false
// for any other kid.
func objectRefTarget(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, kid ast.PdfObject) (int, bool) {
	if _, isInt := kid.(ast.PdfInt); isInt {
		return 0, false
	}
	dict, err := resolveDictAllocFirst(ctx, alloc, doc, kid)
	if err != nil || dict == nil {
		return 0, false
	}
	if kind, _ := dict["Type"].(ast.PdfName); kind != "OBJR" {
		return 0, false
	}
	target, ok := dict["Obj"].(ast.PdfIndirectRef)
	return target.ObjectNum, ok
}

// withoutKids returns /K without the kids that reference an element in gone,
// and whether it dropped any.
func withoutKids(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, kObj ast.PdfObject, gone map[int]bool) (ast.PdfArray, bool) {
	kids := structKidList(ctx, alloc, doc, kObj)
	kept := make(ast.PdfArray, 0, len(kids))
	for _, kid := range kids {
		if ref, isRef := kid.(ast.PdfIndirectRef); isRef && gone[ref.ObjectNum] {
			continue
		}
		kept = append(kept, kid)
	}
	return kept, len(kept) != len(kids)
}

// isFormElement reports whether the element's structure type is Form, directly
// or through the root's /RoleMap (ISO 32000-1, 14.8.4).
func isFormElement(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, root, elem ast.PdfDict) bool {
	s, _ := elem["S"].(ast.PdfName)
	roleMap, err := resolveDictAllocFirst(ctx, alloc, doc, root["RoleMap"])
	if err != nil {
		roleMap = nil
	}
	// Each step follows one role map entry, so a chain longer than the map
	// has looped.
	for range len(roleMap) + 1 {
		if s == "Form" {
			return true
		}
		mapped, err := document.ResolveName(ctx, doc, roleMap[s])
		if err != nil || mapped == "" {
			return false
		}
		s = mapped
	}
	return false
}

// fieldRole returns the PrintField role of the field a widget presents (ISO
// 32000-1, Table 348): rb, cb or pb for a radio button, check box or push button
// (Table 226), and tv for every other field. Table 348 defines tv for a field
// whose value was converted to text in a non-interactive document, which is
// what flattening does to a text or choice field; it has no role for a
// signature field, whose flattened appearance is static content like any other.
func fieldRole(ctx context.Context, doc *document.Document, annotDict ast.PdfDict) ast.PdfName {
	fieldType, _ := inheritedFieldTypeAndValue(ctx, doc, annotDict)
	if fieldType != "Btn" {
		return "tv"
	}
	flags := inheritedFieldFlags(ctx, doc, annotDict)
	switch {
	case flags&buttonFlagPushbutton != 0:
		return "pb"
	case flags&buttonFlagRadio != 0:
		return "rb"
	}
	return "cb"
}

// inheritedFieldFlags reads /Ff from the widget or the nearest ancestor that
// states it (ISO 32000-1, 12.7.3.1).
func inheritedFieldFlags(ctx context.Context, doc *document.Document, annotDict ast.PdfDict) int {
	dict := annotDict
	seen := make(map[int]bool)
	for depth := 0; depth < maxFieldAncestorDepth && dict != nil; depth++ {
		if flags, ok := intEntry(ctx, doc, dict["Ff"]); ok {
			return flags
		}
		_, parent, ok := document.ResolveFieldParent(ctx, doc, dict, seen)
		if !ok {
			break
		}
		dict = parent
	}
	return 0
}

// withPrintFieldRole returns the element's /A with a PrintField attribute
// object stating role (ISO 32000-1, 14.7.5 and Table 348). An attribute object
// the producer wrote for PrintField keeps the role it already states, and gains
// one when it states none; any other attribute object stays beside the new one.
func withPrintFieldRole(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, aObj ast.PdfObject, role ast.PdfName) ast.PdfObject {
	attr := ast.PdfDict{"O": ast.PdfName("PrintField"), "Role": role}
	if aObj == nil {
		return attr
	}
	isPrintField := func(d ast.PdfDict) bool {
		owner, err := document.ResolveName(ctx, doc, d["O"])
		return err == nil && owner == "PrintField"
	}
	withRole := func(d ast.PdfDict) ast.PdfDict {
		updated := make(ast.PdfDict, len(d)+1)
		maps.Copy(updated, d)
		updated["Role"] = role
		return updated
	}

	resolved, err := resolveAllocFirst(ctx, alloc, doc, aObj)
	if err != nil {
		return attr
	}
	switch v := resolved.(type) {
	case ast.PdfDict:
		if !isPrintField(v) {
			return ast.PdfArray{aObj, attr}
		}
		if _, has := v["Role"]; has {
			return aObj
		}
		return withRole(v)
	case ast.PdfArray:
		// The array interleaves attribute objects with revision numbers.
		out := slices.Clone(v)
		for i, entry := range out {
			d, err := resolveDictAllocFirst(ctx, alloc, doc, entry)
			if err != nil || d == nil || !isPrintField(d) {
				continue
			}
			if _, has := d["Role"]; has {
				return aObj
			}
			out[i] = withRole(d)
			return out
		}
		return append(out, attr)
	}
	return attr
}

// pageMaxMCID returns the highest marked-content identifier the page's content
// uses, -1 when it uses none. The content is read as earlier stages staged it.
// A BDC names its properties inline or through the page's /Properties
// resources (ISO 32000-1, 14.6.2), and both are read.
func pageMaxMCID(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, page *document.Page) (int, error) {
	data, err := collectLatestPageContents(ctx, alloc, doc, page)
	if err != nil {
		return -1, fmt.Errorf("reading the page content: %w", err)
	}
	ops, err := content.ParseStreamData(ctx, data)
	if err != nil {
		return -1, fmt.Errorf("parsing the page content: %w", err)
	}
	var properties ast.PdfDict
	propertiesRead := false
	maxMCID := -1
	for _, op := range ops {
		if op.Operator != "BDC" || len(op.Operands) < 2 {
			continue
		}
		var props ast.PdfDict
		switch v := op.Operands[1].(type) {
		case ast.PdfDict:
			props = v
		case ast.PdfName:
			if !propertiesRead {
				propertiesRead = true
				resources := latestPage(ctx, alloc, doc, page).Resources
				properties, _ = resolveDictAllocFirst(ctx, alloc, doc, resources["Properties"])
			}
			props, _ = resolveDictAllocFirst(ctx, alloc, doc, properties[v])
		}
		if mcid, ok := intEntry(ctx, doc, props["MCID"]); ok {
			maxMCID = max(maxMCID, mcid)
		}
	}
	return maxMCID, nil
}
