package mutations

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// maxStructTreeDepth caps the structure tree walk in [structPrune].
// Anti-DoS guard, not spec-mandated: a real tree nests a few dozen levels at
// most, and a crafted /K cycle must not hang the writer. It matches
// maxDestTreeDepth for the same reason.
const maxStructTreeDepth = 64

// structPrune decides which elements of a structure tree outlive the pages an
// operation takes out of the document, and rewrites the ones that stay so they
// name none of those pages.
//
// A structure element names the page its content sits on in /Pg (ISO 32000-1,
// 14.7.2, Table 323), so an element left behind both describes a page the
// document no longer has and, because a rewrite copies whatever it reaches,
// keeps that page and its content stream in the output.
//
// Objects are read from alloc first, so a prune composes with the edits earlier
// stages queued there, and every rewrite is queued in alloc at the object's own
// number. DeletePages prunes the document in place; a copy of some pages into
// another document prunes a scratch allocator in the source's numbering, whose
// objects then stand in for the source's when the copy runs (see
// [newStructSource]).
type structPrune struct {
	alloc *writer.ObjAllocator
	doc   *document.Document
	// gone reports whether the page an entry names leaves the document.
	gone func(ast.PdfIndirectRef) bool
	// keptAnnots holds the annotations the pages that stay list in /Annots. An
	// object reference to an annotation outside it dies (see
	// [structPrune.objectOnGonePage]).
	keptAnnots map[int]bool
	// dropEmptied drops an element that carries no content that stays once
	// the operation took something from it, whatever page it names (see
	// [structPrune.element]): it describes nothing the output holds. An
	// element naming no page whose kids all went otherwise stays behind empty,
	// as a Form element left with no widget, which ISO 14289-1, 7.18.4
	// forbids.
	dropEmptied bool
	// keepAlways holds the elements kept whatever pages go, as the Document
	// element at the top of a copied tree is (see [newStructSource]).
	keepAlways map[int]bool
	memo       map[int]structElemVerdict
	// arrays remembers what pruning each /K array written as an object of
	// its own produced, as memo remembers an element's verdict: the direct
	// elements such an array holds can each name it again as their /K (see
	// [structPrune.kids]).
	arrays map[int]structKidsResult
	// truncated reports that a walk stopped at [maxStructTreeDepth] with part of
	// the tree unread.
	truncated bool
}

// structElemVerdict is what pruning one element decided, remembered so an
// element named more than once is pruned once and answers the same every time:
// whether it goes, and whether it carries content that stays (see
// [structPrune.element]).
type structElemVerdict struct{ drop, carries bool }

// newStructPrune returns a prune of the tree doc holds as alloc stages it, for
// an operation after which the pages gone reports no longer belong to the
// document. keptAnnots lists the annotations the remaining pages list.
func newStructPrune(alloc *writer.ObjAllocator, doc *document.Document, gone func(ast.PdfIndirectRef) bool, keptAnnots map[int]bool, dropEmptied bool) *structPrune {
	return &structPrune{
		alloc:       alloc,
		doc:         doc,
		gone:        gone,
		keptAnnots:  keptAnnots,
		dropEmptied: dropEmptied,
		memo:        make(map[int]structElemVerdict),
		arrays:      make(map[int]structKidsResult),
	}
}

// pageAnnotations returns the object numbers of the annotations the pages
// list in /Annots, read from alloc first.
func pageAnnotations(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pages []ast.PdfIndirectRef) map[int]bool {
	out := make(map[int]bool)
	for _, page := range pages {
		dict, err := resolveDictAllocFirst(ctx, alloc, doc, page)
		if err != nil || dict == nil {
			continue
		}
		annots, err := resolveArrayAllocFirst(ctx, alloc, doc, dict["Annots"])
		if err != nil {
			continue
		}
		for _, annot := range annots {
			if ref, isRef := annot.(ast.PdfIndirectRef); isRef {
				out[ref.ObjectNum] = true
			}
		}
	}
	return out
}

// pruneStructTree drops the structure elements that describe pages a deletion
// removed: an element on a removed page none of whose descendants survives,
// and an element every kid of which went, which describes nothing the
// document holds any more. The root's single Document element stays whatever
// its kids (see [rootDocument]).
//
// Every entry of the tree that names elements is pruned with the /K hierarchy:
// the /ParentTree, the /IDTree and each element's /Ref (ISO 32000-2, Table
// 355). Each is a live reference the rewrite follows, so an element one of them
// kept would carry its page into the output.
//
// # Corpus evidence
//
//   - govdocs thread_400_400912.pdf: deleting page 1 carried the page along
//     with 17204 bytes of its content, held there by the structure tree alone
//     once /OpenAction and the form field tree had let go.
//   - govdocs thread_000_000761.pdf: storing the returned reference again after
//     its /K array was rewritten in place wrote the reference as the array
//     object, which cost the file its whole structure tree.
func pruneStructTree(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	catalogRef ast.PdfIndirectRef,
	kept []ast.PdfIndirectRef,
	removed map[int]bool,
) error {
	catalogDict, err := LiveCatalogDict(ctx, alloc, doc, catalogRef)
	if err != nil {
		return fmt.Errorf("reading the catalog: %w", err)
	}
	root, err := resolveDictAllocFirst(ctx, alloc, doc, catalogDict["StructTreeRoot"])
	if err != nil || root == nil {
		return nil
	}

	p := newStructPrune(alloc, doc, func(ref ast.PdfIndirectRef) bool { return removed[ref.ObjectNum] },
		pageAnnotations(ctx, alloc, doc, kept), true)
	if docElem := rootDocument(ctx, alloc, doc, root); docElem.ObjectNum != 0 {
		p.keepAlways = map[int]bool{docElem.ObjectNum: true}
	}

	newRoot := maps.Clone(root)
	changed := false

	kids := p.kids(ctx, root["K"], false, 0)
	if kids.Changed {
		changed = true
		// A queued /K array was already rewritten in place; storing the returned
		// reference again would write the reference AS the array object and
		// lose the whole structure tree.
		if !kids.Queued {
			setOrDelete(newRoot, "K", kids.Value)
		}
	}
	tree := p.parentTree(ctx, root["ParentTree"], 0)
	if tree.Changed {
		changed = true
		if !tree.Queued {
			setOrDelete(newRoot, "ParentTree", tree.Value)
		}
	}
	ids, idsChanged, idsTruncated := p.idTree(ctx, root["IDTree"])
	if idsChanged {
		changed = true
		if _, isRef := root["IDTree"].(ast.PdfIndirectRef); !isRef {
			setOrDelete(newRoot, "IDTree", ids)
		}
	}
	p.refs(ctx)
	p.emptyDropped(ctx)

	// Any walk stopping short leaves elements naming a removed page, and the
	// rewrite copies whatever the catalog reaches, so the page and its content
	// would ship inside a document reporting one fewer page. Refuse: the error
	// reaches the writer before anything is written, so the caller gets no file
	// rather than a half-pruned one. Pruning /K alone is not enough either,
	// which is why /ParentTree and /IDTree count here too.
	if p.truncated || tree.Truncated || idsTruncated {
		return ErrPagePruneIncomplete
	}

	if !changed {
		return nil
	}
	if ref, isRef := catalogDict["StructTreeRoot"].(ast.PdfIndirectRef); isRef {
		alloc.AllocAt(ref.ObjectNum, newRoot)
		return nil
	}
	// A root written directly into the catalog has no number of its own, so
	// the catalog carries the pruned one.
	updated := maps.Clone(catalogDict)
	updated["StructTreeRoot"] = newRoot
	alloc.AllocAt(catalogRef.ObjectNum, updated)
	return nil
}

// emptyDropped rewrites each element the prune dropped without the entries
// that reach content: its /K, /Pg and /Ref. The tree no longer names it, but a
// kept element can still, as its /P where the /ParentTree lists an element its
// parent never lists back in /K, and a rewrite following that /P would follow
// the dropped element's kids to the pages they named.
func (p *structPrune) emptyDropped(ctx context.Context) {
	for _, num := range slices.Sorted(maps.Keys(p.memo)) {
		if !p.memo[num].drop {
			continue
		}
		elem, err := resolveDictAllocFirst(ctx, p.alloc, p.doc, ast.PdfIndirectRef{ObjectNum: num})
		if err != nil || elem == nil {
			continue
		}
		emptied := maps.Clone(elem)
		for _, key := range []ast.PdfName{"K", "Pg", "Ref"} {
			delete(emptied, key)
		}
		if len(emptied) != len(elem) {
			p.alloc.AllocAt(num, emptied)
		}
	}
}

// setOrDelete stores value under key, or removes key when value is nil.
func setOrDelete(dict ast.PdfDict, key ast.PdfName, value ast.PdfObject) {
	if value == nil {
		delete(dict, key)
		return
	}
	dict[key] = value
}

// pageGone reports whether a dictionary's /Pg names a page that goes.
// Structure elements, marked-content references and object references all
// carry it (ISO 32000-1, Tables 323, 324 and 325).
func (p *structPrune) pageGone(dict ast.PdfDict) bool {
	ref, isRef := dict["Pg"].(ast.PdfIndirectRef)
	return isRef && p.gone(ref)
}

// structKidsResult reports what pruning one /K entry produced.
//
// Queued distinguishes the case where the /K array is itself an indirect object
// that was rewritten in place, so the owning element keeps its reference
// untouched, from the case where the owner must store Value. Conflating the two
// left a dropped single kid still referenced by its parent, and with it the page
// it named.
//
// # Corpus evidence
//
//   - govdocs thread_000_000761.pdf: kept 2196 bytes of the deleted page
//     through a dropped single kid its parent still referenced.
type structKidsResult struct {
	Value     ast.PdfObject
	Changed   bool
	Survivors int
	// Dropped counts the kids that went, and Carries says a kid that stayed
	// carries content that stays (see [structPrune.element]).
	Dropped int
	Carries bool
	Queued  bool
	// Truncated reports that the walk stopped at [maxStructTreeDepth] with
	// subtree left unread. Everything below that point keeps whatever /Pg it
	// named, so a page the caller asked to delete survives in the output along
	// with its content stream. The caller must refuse rather than report a
	// deletion it did not perform.
	Truncated bool
}

// kids filters a /K entry, which ISO 32000-1, Table 323 allows to be a single
// kid or an array of them.
//
// ownerGone says the owning element's /Pg is going, which is what makes a bare
// integer kid, a marked-content identifier on that very page, dead too.
//
// The verdicts are a cache, not a visited set, and the difference matters here.
// This walk fans out over /K, so [maxStructTreeDepth] caps the work at
// branch^depth rather than at depth, and an element whose /K names the same
// child twice costs 2^64. Simply refusing the second visit would answer "keep,
// unchanged" where the first visit may have said "drop", and the two entries of
// one /K array would then disagree. Returning the remembered verdict keeps the
// output exactly what the unguarded walk produced while the work stays linear.
// A /K array written as an object of its own is remembered the same way
// ([structPrune.arrays]): its direct elements, which no element memo holds,
// can each name it again. A well-formed tree never hits either memo, because
// no element or array is named twice.
func (p *structPrune) kids(ctx context.Context, kidsObj ast.PdfObject, ownerGone bool, depth int) structKidsResult {
	// The two conditions are not the same thing and must not share a branch. An
	// element with no /K is a leaf, which every tagged tree is full of; the depth
	// bound is a subtree this walk never read. Marking the leaf truncated would
	// make the delete refuse on ordinary documents.
	if depth > maxStructTreeDepth {
		p.truncated = true
		return structKidsResult{Value: kidsObj, Survivors: 1, Carries: true, Truncated: true}
	}
	if kidsObj == nil {
		return structKidsResult{Value: kidsObj, Survivors: 1}
	}

	arr, err := resolveArrayAllocFirst(ctx, p.alloc, p.doc, kidsObj)
	if err != nil {
		out := p.kid(ctx, kidsObj, ownerGone, depth)
		if out.drop {
			return structKidsResult{Changed: true, Dropped: 1}
		}
		return structKidsResult{Value: out.replacement, Changed: out.replaced, Survivors: 1, Carries: out.carries}
	}

	// An array written as an object of its own answers with what its first
	// walk produced, as an element does: direct elements that each name it
	// again would otherwise fan out 2^depth. Recorded before the descent, so
	// an array reached again below its own walk answers "keep, unchanged".
	arrayRef, arrayIsRef := kidsObj.(ast.PdfIndirectRef)
	if arrayIsRef {
		if prior, done := p.arrays[arrayRef.ObjectNum]; done {
			return prior
		}
		p.arrays[arrayRef.ObjectNum] = structKidsResult{Value: kidsObj, Survivors: 1, Carries: true}
	}

	res := structKidsResult{}
	kept := make(ast.PdfArray, 0, len(arr))
	for _, kid := range arr {
		out := p.kid(ctx, kid, ownerGone, depth)
		if out.drop {
			res.Changed = true
			res.Dropped++
			continue
		}
		res.Changed = res.Changed || out.replaced
		res.Carries = res.Carries || out.carries
		kept = append(kept, out.replacement)
	}
	res.Survivors = len(kept)
	switch {
	case !res.Changed:
		res.Value = kidsObj
	case arrayIsRef:
		p.alloc.AllocAt(arrayRef.ObjectNum, kept)
		res.Value, res.Queued = kidsObj, true
	default:
		res.Value = kept
	}
	if arrayIsRef {
		p.arrays[arrayRef.ObjectNum] = res
	}
	return res
}

// kidOutcome is what pruning one kid of a structure element decided.
type kidOutcome struct {
	drop bool
	// replacement takes the kid's place in its owner's /K, and replaced says
	// it differs from the kid: only a kid written directly into /K is
	// replaced, an indirect element being rewritten at its own number.
	replacement ast.PdfObject
	replaced    bool
	// carries says the kid describes content on a page that stays (see
	// [structPrune.element]).
	carries bool
}

// kid decides the fate of one kid of a structure element: whether it must be
// dropped, and otherwise what takes its place in the owner's /K and whether
// that differs from the kid. Only a kid written directly into /K is replaced:
// an indirect element is rewritten at its own number.
//
// # Corpus evidence
//
//   - govdocs thread_100_100587.pdf: reading only an /OBJR's own /Pg kept one
//     naming a link annotation on the deleted page, and through the link's
//     /GoTo the page itself.
func (p *structPrune) kid(ctx context.Context, kid ast.PdfObject, ownerGone bool, depth int) kidOutcome {
	// A bare integer is a marked-content identifier in the owner's own page
	// content (ISO 32000-1, 14.7.4.2). It survives exactly as long as that page.
	if _, isInt := kid.(ast.PdfInt); isInt {
		return kidOutcome{drop: ownerGone, replacement: kid, carries: !ownerGone}
	}
	dict, err := resolveDictAllocFirst(ctx, p.alloc, p.doc, kid)
	if err != nil || dict == nil {
		return kidOutcome{replacement: kid}
	}
	// A marked-content or object reference has no descendants to save. Its /Pg is
	// optional (ISO 32000-1, Tables 324 and 325) and defaults to the owning
	// element's page, so one without it dies with that page exactly as a bare
	// marked-content identifier does. Reading only its own /Pg would keep an
	// /OBJR naming a link annotation on the deleted page, and through the
	// link's /GoTo the page itself.
	if kind, _ := dict["Type"].(ast.PdfName); kind == "MCR" || kind == "OBJR" {
		gone := ownerGone
		if _, hasOwnPage := dict["Pg"]; hasOwnPage {
			gone = p.pageGone(dict)
		}
		if !gone && kind == "OBJR" {
			gone = p.objectOnGonePage(ctx, dict["Obj"])
		}
		return kidOutcome{drop: gone, replacement: kid, carries: !gone}
	}
	return p.element(ctx, kid, dict, depth)
}

// objectOnGonePage reports whether the object an /OBJR references is an
// annotation no page that stays lists in /Annots (ISO 32000-1, 12.5.2). It
// left with its page, or the operation took it off a page that stays, as a
// link to a page that goes is (see [splitDestOverrides]). Copying the
// reference would copy the annotation, and through its /P or its action the
// page it names.
func (p *structPrune) objectOnGonePage(ctx context.Context, obj ast.PdfObject) bool {
	ref, isRef := obj.(ast.PdfIndirectRef)
	if !isRef || p.keptAnnots == nil || p.keptAnnots[ref.ObjectNum] {
		return false
	}
	target, err := resolveAllocFirst(ctx, p.alloc, p.doc, ref)
	if err != nil {
		return false
	}
	// An annotation dictionary states its /Subtype (Table 164); an XObject an
	// object reference names is a stream, and stays with the content drawing it.
	dict, isDict := target.(ast.PdfDict)
	if !isDict {
		return false
	}
	_, isAnnotation := dict["Subtype"].(ast.PdfName)
	return isAnnotation
}

// element filters one element's own /K and decides whether the element itself
// survives, rewriting it when it does and changed.
//
// An element carries content that stays when its /Pg names a page that stays,
// or a kid of it does: a marked-content sequence or object on such a page (ISO
// 32000-1, 14.7.4), or an element carrying some in turn. One carrying none
// goes once the operation took something from it, its page or a kid: it
// describes nothing the document still holds. An element that lost nothing
// stays, carrying content or not: an empty cell of a table the operation keeps
// is still one of its cells, and a merge, which removes no page, keeps every
// element as it was.
//
// Without dropEmptied, an element goes only when its page is removed and no
// kid outlived it. An element whose /Pg is removed but whose children sit on
// pages that stayed keeps its place and loses only /Pg either way, because
// dropping it would detach those children from the tree, and because /Pg is
// optional (ISO 32000-1, Table 323) and the content it named is gone.
//
// # Corpus evidence
//
//   - govdocs thread_000_000153.pdf: the empty cells of tables on pages the
//     split left out kept their rows, and their tables, as long as a
//     surviving kid was enough to keep an element: every split of the file
//     carried skeleton tables that failed ISO 14289-1, 7.2 test 42.
//   - All corpora, 1085 tagged files: while a surviving kid kept an element,
//     splitting out page 1 added 7.2 test 42 at PDF/UA-1 and ISO 14289-2,
//     8.2.5.26 test 3 at PDF/UA-2 on 16 files each. Since an element needs
//     content that stays, it adds them on none.
func (p *structPrune) element(ctx context.Context, obj ast.PdfObject, element ast.PdfDict, depth int) kidOutcome {
	// An element named twice answers with the verdict it already reached, so a
	// /K that fans out is pruned once. Only an indirect element can be named
	// twice: one written directly into its parent has no object of its own.
	elemRef, elemIsRef := obj.(ast.PdfIndirectRef)
	if elemIsRef {
		if v, done := p.memo[elemRef.ObjectNum]; done {
			return kidOutcome{drop: v.drop, replacement: obj, carries: v.carries}
		}
		// Recorded before the descent, so an element that names itself below
		// answers "keep" to its own descendant rather than looping.
		p.memo[elemRef.ObjectNum] = structElemVerdict{carries: true}
	}

	pageGone := p.pageGone(element)
	kidsObj, hasKids := element["K"]
	kids := p.kids(ctx, kidsObj, pageGone, depth+1)
	survivors := kids.Survivors
	if !hasKids {
		survivors = 0
	}
	_, statesPage := element["Pg"].(ast.PdfIndirectRef)
	carries := statesPage && !pageGone || hasKids && kids.Carries

	var drop bool
	switch {
	case elemIsRef && p.keepAlways[elemRef.ObjectNum]:
	case p.dropEmptied:
		drop = !carries && (pageGone || kids.Dropped > 0)
	default:
		drop = pageGone && survivors == 0
	}
	if elemIsRef {
		p.memo[elemRef.ObjectNum] = structElemVerdict{drop: drop, carries: carries}
	}
	if drop {
		return kidOutcome{drop: true, replacement: obj}
	}
	if !kids.Changed && !pageGone {
		return kidOutcome{replacement: obj, carries: carries}
	}

	newElement := maps.Clone(element)
	if pageGone {
		delete(newElement, "Pg")
	}
	if kids.Changed && !kids.Queued {
		setOrDelete(newElement, "K", kids.Value)
	}
	if elemIsRef {
		p.alloc.AllocAt(elemRef.ObjectNum, newElement)
		return kidOutcome{replacement: obj, carries: carries}
	}
	return kidOutcome{replacement: newElement, replaced: true, carries: carries}
}

// subtree prunes an element the /K walk did not reach, as the root of a
// subtree of its own, and reports whether it is kept. obj must be the
// element's reference. Something other than a structure element is not
// pruned and reports false.
func (p *structPrune) subtree(ctx context.Context, obj ast.PdfObject) bool {
	ref, isRef := obj.(ast.PdfIndirectRef)
	if !isRef {
		return false
	}
	if v, done := p.memo[ref.ObjectNum]; done {
		return !v.drop
	}
	dict, err := resolveDictAllocFirst(ctx, p.alloc, p.doc, ref)
	if err != nil || dict == nil {
		return false
	}
	if _, isElement := dict["S"].(ast.PdfName); !isElement {
		return false
	}
	return !p.element(ctx, obj, dict, 0).drop
}

// refs drops from each element the prune kept the /Ref entries naming elements
// it dropped (ISO 32000-2, 14.7.2, Table 355). An element /Ref names that the
// walks never reached is pruned as a subtree of its own first.
func (p *structPrune) refs(ctx context.Context) {
	for _, num := range slices.Sorted(maps.Keys(p.memo)) {
		if ctx.Err() != nil {
			return
		}
		if p.memo[num].drop {
			continue
		}
		ref := ast.PdfIndirectRef{ObjectNum: num}
		element, err := resolveDictAllocFirst(ctx, p.alloc, p.doc, ref)
		if err != nil || element == nil {
			continue
		}
		targets, err := resolveArrayAllocFirst(ctx, p.alloc, p.doc, element["Ref"])
		if err != nil {
			continue
		}
		kept := make(ast.PdfArray, 0, len(targets))
		for _, target := range targets {
			if p.subtree(ctx, target) {
				kept = append(kept, target)
			}
		}
		if len(kept) == len(targets) {
			continue
		}
		updated := maps.Clone(element)
		if len(kept) == 0 {
			delete(updated, "Ref")
		} else {
			updated["Ref"] = kept
		}
		p.alloc.AllocAt(num, updated)
	}
}

// idTree drops from the /IDTree the identifiers of the elements the prune
// dropped (ISO 32000-1, 14.7.2, Table 322). An element it names that the walks
// never reached is pruned as a subtree of its own first. It returns the tree
// to store in the root, whether anything changed, and whether the walk stopped
// at its depth bound.
func (p *structPrune) idTree(ctx context.Context, tree ast.PdfObject) (ast.PdfObject, bool, bool) {
	if tree == nil {
		return nil, false, false
	}
	return pruneDestNameTree(ctx, p.alloc, p.doc, tree, dropWhen(func(value ast.PdfObject) bool {
		return !p.subtree(ctx, value)
	}), 0)
}

// parentTree drops the /ParentTree entries whose elements describe removed
// pages.
//
// The number tree maps each /StructParents key back to the elements marked on
// that page (ISO 32000-1, 14.7.4.4). An entry for a removed page still reaches
// its elements, and through their /Pg the page itself, so pruning the /K tree
// alone leaves the page exactly where it was.
func (p *structPrune) parentTree(ctx context.Context, nodeObj ast.PdfObject, depth int) structKidsResult {
	truncated := false
	res := p.parentTreeAt(ctx, nodeObj, make(map[int]bool), depth, &truncated)
	res.Truncated = truncated
	return res
}

// parentTreeAt is [structPrune.parentTree] carrying the nodes already walked.
//
// visited costs nothing on a well-formed tree and bounds a malformed one. The
// walk fans out over /Kids, so [maxStructTreeDepth] caps the work at
// branch^depth rather than at depth: a node whose /Kids names the same child
// twice costs 2^64. The output does not change either way, since each kid is
// written back at its own index, which is why this is a limit rather than a
// repair.
//
// # Corpus evidence
//
//   - pdfjs issue18503.pdf: its /ParentTree and /Nums are both direct, and
//     not storing the replacement for a direct node left the tree entirely
//     unpruned: it was filtered and then thrown away.
func (p *structPrune) parentTreeAt(
	ctx context.Context,
	nodeObj ast.PdfObject,
	visited map[int]bool,
	depth int,
	truncated *bool,
) structKidsResult {
	// Split for the same reason as the /K walk: a nil node is an absent subtree,
	// the depth bound is one this walk declined to read.
	if depth > maxStructTreeDepth {
		*truncated = true
		return structKidsResult{Value: nodeObj}
	}
	if nodeObj == nil {
		return structKidsResult{Value: nodeObj}
	}
	if ref, isRef := nodeObj.(ast.PdfIndirectRef); isRef {
		if visited[ref.ObjectNum] {
			return structKidsResult{Value: nodeObj}
		}
		visited[ref.ObjectNum] = true
	}
	node, err := resolveDictAllocFirst(ctx, p.alloc, p.doc, nodeObj)
	if err != nil || node == nil {
		return structKidsResult{Value: nodeObj}
	}

	newNode := maps.Clone(node)
	changed := false

	// A /Kids array written as an object of its own is walked once too, and
	// rewritten at its own object number: its direct nodes can each name it
	// again, which fans out 2^depth however the nodes are marked
	// ([document.VisitedBefore]).
	if kidsObj, hasKids := node["Kids"]; hasKids && !document.VisitedBefore(kidsObj, visited) {
		if kids, kidsErr := resolveArrayAllocFirst(ctx, p.alloc, p.doc, kidsObj); kidsErr == nil {
			newKids := make(ast.PdfArray, len(kids))
			kidsChanged := false
			for i, kid := range kids {
				res := p.parentTreeAt(ctx, kid, visited, depth+1, truncated)
				newKids[i] = kid
				if !res.Changed {
					continue
				}
				kidsChanged = true
				if !res.Queued {
					newKids[i] = res.Value
				}
			}
			if kidsChanged {
				changed = true
				if ref, isRef := kidsObj.(ast.PdfIndirectRef); isRef {
					p.alloc.AllocAt(ref.ObjectNum, newKids)
				} else {
					newNode["Kids"] = newKids
				}
			}
		}
	}

	if numsObj, hasNums := node["Nums"]; hasNums {
		if nums, numsErr := resolveArrayAllocFirst(ctx, p.alloc, p.doc, numsObj); numsErr == nil {
			kept := make(ast.PdfArray, 0, len(nums))
			numsChanged := false
			for i := 0; i+1 < len(nums); i += 2 {
				value, dropped := p.parentTreeValue(ctx, nums[i+1])
				if dropped {
					numsChanged = true
					if value == nil {
						continue
					}
				}
				kept = append(kept, nums[i], value)
			}
			if numsChanged {
				changed = true
				if ref, isRef := numsObj.(ast.PdfIndirectRef); isRef {
					p.alloc.AllocAt(ref.ObjectNum, kept)
				} else {
					newNode["Nums"] = kept
				}
			}
		}
	}

	if !changed {
		return structKidsResult{Value: nodeObj}
	}
	// A direct node has no object of its own to rewrite, so the caller has to
	// store the replacement, or the filtered tree is thrown away.
	if ref, isRef := nodeObj.(ast.PdfIndirectRef); isRef {
		p.alloc.AllocAt(ref.ObjectNum, newNode)
		return structKidsResult{Value: nodeObj, Changed: true, Queued: true}
	}
	return structKidsResult{Value: newNode, Changed: true}
}

// parentTreeValue filters one /Nums value, which is either a single element or
// an array of them. It returns the replacement and whether anything was
// dropped; a nil replacement means the whole entry goes.
func (p *structPrune) parentTreeValue(ctx context.Context, value ast.PdfObject) (ast.PdfObject, bool) {
	if arr, err := resolveArrayAllocFirst(ctx, p.alloc, p.doc, value); err == nil {
		kept := make(ast.PdfArray, 0, len(arr))
		for _, entry := range arr {
			if p.parentTreeEntryIsRemoved(ctx, entry) {
				continue
			}
			p.clearGonePgOnAncestors(ctx, entry)
			kept = append(kept, entry)
		}
		if len(kept) == len(arr) {
			return value, false
		}
		if len(kept) == 0 {
			return nil, true
		}
		if ref, isRef := value.(ast.PdfIndirectRef); isRef {
			p.alloc.AllocAt(ref.ObjectNum, kept)
			return value, true
		}
		return kept, true
	}
	if p.parentTreeEntryIsRemoved(ctx, value) {
		return nil, true
	}
	p.clearGonePgOnAncestors(ctx, value)
	return value, false
}

// clearGonePgOnAncestors walks an element's /P chain and drops the /Pg of
// every ancestor that names a page that goes.
//
// The /K walk cannot reach these. A structure tree is meant to be symmetric, but
// real files list an element in /ParentTree whose parent never lists it back in
// /K, so the ancestor is reachable only upwards. It still holds the page: the
// element survives and its /P is a live reference. The ancestor keeps its place
// and loses only /Pg, which is optional (ISO 32000-1, Table 323) and named
// content that is now gone.
//
// # Corpus evidence
//
//   - pdfjs issue18503.pdf: lists an element in /ParentTree whose parent never
//     lists it back in /K, and 22297 bytes of the deleted page came along
//     through that ancestor.
func (p *structPrune) clearGonePgOnAncestors(ctx context.Context, obj ast.PdfObject) {
	visited := make(map[int]bool)
	current := obj
	for range maxStructTreeDepth {
		ref, isRef := current.(ast.PdfIndirectRef)
		if !isRef || visited[ref.ObjectNum] {
			return
		}
		visited[ref.ObjectNum] = true

		element, err := resolveDictAllocFirst(ctx, p.alloc, p.doc, ref)
		if err != nil || element == nil {
			return
		}
		if p.pageGone(element) {
			newElement := maps.Clone(element)
			delete(newElement, "Pg")
			p.alloc.AllocAt(ref.ObjectNum, newElement)
			element = newElement
		}
		current = element["P"]
	}
}

// parentTreeEntryIsRemoved reports whether a /ParentTree value names an element
// the prune dropped. One the /K walk never reached is dropped when its own /Pg
// names a page that goes; the elements it kept have already lost that /Pg.
func (p *structPrune) parentTreeEntryIsRemoved(ctx context.Context, obj ast.PdfObject) bool {
	if ref, isRef := obj.(ast.PdfIndirectRef); isRef {
		if v, decided := p.memo[ref.ObjectNum]; decided {
			return v.drop
		}
	}
	dict, err := resolveDictAllocFirst(ctx, p.alloc, p.doc, obj)
	if err != nil || dict == nil {
		return false
	}
	return p.pageGone(dict)
}
