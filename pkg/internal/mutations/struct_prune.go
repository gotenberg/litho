package mutations

import (
	"context"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// maxStructTreeDepth caps the structure tree walk in [pruneStructTree].
// Anti-DoS guard, not spec-mandated: a real tree nests a few dozen levels at
// most, and a crafted /K cycle must not hang the writer. It matches
// maxDestTreeDepth for the same reason.
const maxStructTreeDepth = 64

// pruneStructTree drops the structure elements that describe pages the
// operation removed.
//
// A structure element names the page its content sits on in /Pg (ISO 32000-1,
// 14.7.2, Table 323), so an element left behind both describes a page the
// document no longer has and, because the rewrite copies whatever the catalog
// reaches, keeps that page and its content stream in the output. Deleting page 1
// of govdocs thread_400_400912.pdf carried the page along with 17204 bytes of
// its content, held there by the structure tree alone once /OpenAction and the
// form field tree had let go.
func pruneStructTree(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	catalogDict ast.PdfDict,
	removed map[int]bool,
) error {
	root, err := document.ResolveDict(ctx, doc, catalogDict["StructTreeRoot"])
	if err != nil || root == nil {
		return nil
	}

	newRoot := make(ast.PdfDict, len(root))
	maps.Copy(newRoot, root)
	changed := false

	kids := pruneStructKids(ctx, alloc, doc, root["K"], removed, false, 0)
	if kids.Changed {
		changed = true
		// A queued /K array was already rewritten in place; storing the returned
		// reference again would write the reference AS the array object, which
		// cost thread_000_000761.pdf its whole structure tree.
		if !kids.Queued {
			newRoot["K"] = kids.Value
		}
	}
	tree := pruneParentTree(ctx, alloc, doc, root["ParentTree"], removed, 0)
	if tree.Changed {
		changed = true
		if !tree.Queued {
			newRoot["ParentTree"] = tree.Value
		}
	}

	// Either walk stopping short leaves elements naming a removed page, and the
	// rewrite copies whatever the catalog reaches, so the page and its content
	// would ship inside a document reporting one fewer page. Refuse: the error
	// reaches [writer] before anything is written, so the caller gets no file
	// rather than a half-pruned one. Pruning /K alone is not enough either, which
	// is why /ParentTree counts here too.
	if kids.Truncated || tree.Truncated {
		return ErrStructTreeTooDeepToPrune
	}

	if !changed {
		return nil
	}
	if ref, isRef := catalogDict["StructTreeRoot"].(ast.PdfIndirectRef); isRef {
		alloc.AllocAt(ref.ObjectNum, newRoot)
	}
	return nil
}

// onRemovedPage reports whether a dictionary's /Pg names a page the operation
// removed. Structure elements, marked-content references and object references
// all carry it (ISO 32000-1, Tables 323, 324 and 325).
func onRemovedPage(dict ast.PdfDict, removed map[int]bool) bool {
	ref, isRef := dict["Pg"].(ast.PdfIndirectRef)
	return isRef && removed[ref.ObjectNum]
}

// structKidsResult reports what pruning one /K entry produced.
//
// Queued distinguishes the case where the /K array is itself an indirect object
// that was rewritten in place, so the owning element keeps its reference
// untouched, from the case where the owner must store Value. Conflating the two
// left a dropped single kid still referenced by its parent, and with it the page
// it named: govdocs thread_000_000761.pdf kept 2196 bytes that way.
type structKidsResult struct {
	Value     ast.PdfObject
	Changed   bool
	Survivors int
	Queued    bool
	// Truncated reports that the walk stopped at [maxStructTreeDepth] with
	// subtree left unread. Everything below that point keeps whatever /Pg it
	// named, so a page the caller asked to delete survives in the output along
	// with its content stream. The caller must refuse rather than report a
	// deletion it did not perform.
	Truncated bool
}

// pruneStructKids filters a /K entry, which ISO 32000-1, Table 323 allows to be
// a single kid or an array of them.
//
// ownerOnRemovedPage says the owning element's /Pg is going, which is what makes
// a bare integer kid, a marked-content identifier on that very page, dead too.
func pruneStructKids(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	kidsObj ast.PdfObject,
	removed map[int]bool,
	ownerOnRemovedPage bool,
	depth int,
) structKidsResult {
	truncated := false
	res := pruneStructKidsAt(ctx, alloc, doc, kidsObj, removed, make(map[int]structElemVerdict), ownerOnRemovedPage, depth, &truncated)
	res.Truncated = truncated
	return res
}

// structElemVerdict is what pruning one element decided, remembered so an
// element named more than once is pruned once and answers the same every time.
type structElemVerdict struct{ drop, changed bool }

// pruneStructKidsAt is [pruneStructKids] carrying the verdicts already reached.
//
// memo is a cache, not a visited set, and the difference matters here. This
// walk fans out over /K, so [maxStructTreeDepth] caps the work at branch^depth
// rather than at depth, and an element whose /K names the same child twice
// costs 2^64. Simply refusing the second visit would answer "keep, unchanged"
// where the first visit may have said "drop", and the two entries of one /K
// array would then disagree. Returning the remembered verdict keeps the output
// exactly what the unguarded walk produced while the work stays linear. Its
// [writer.ObjAllocator] write is safe to skip on a repeat, since the first
// visit already made the identical one.
//
// A well-formed tree never hits the memo at all, because no element is named
// twice.
func pruneStructKidsAt(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	kidsObj ast.PdfObject,
	removed map[int]bool,
	memo map[int]structElemVerdict,
	ownerOnRemovedPage bool,
	depth int,
	truncated *bool,
) structKidsResult {
	// The two conditions are not the same thing and must not share a branch. An
	// element with no /K is a leaf, which every tagged tree is full of; the depth
	// bound is a subtree this walk never read. Marking the leaf truncated would
	// make the delete refuse on ordinary documents.
	if depth > maxStructTreeDepth {
		*truncated = true
		return structKidsResult{Value: kidsObj, Survivors: 1}
	}
	if kidsObj == nil {
		return structKidsResult{Value: kidsObj, Survivors: 1}
	}

	arr, err := document.ResolveArray(ctx, doc, kidsObj)
	if err != nil {
		drop, changed := pruneStructKidAt(ctx, alloc, doc, kidsObj, removed, memo, ownerOnRemovedPage, depth, truncated)
		if drop {
			return structKidsResult{Changed: true}
		}
		return structKidsResult{Value: kidsObj, Changed: changed, Survivors: 1}
	}

	kept := make(ast.PdfArray, 0, len(arr))
	changed := false
	for _, kid := range arr {
		drop, kidChanged := pruneStructKidAt(ctx, alloc, doc, kid, removed, memo, ownerOnRemovedPage, depth, truncated)
		if drop {
			changed = true
			continue
		}
		changed = changed || kidChanged
		kept = append(kept, kid)
	}
	if !changed {
		return structKidsResult{Value: kidsObj, Survivors: len(arr)}
	}
	if ref, isRef := kidsObj.(ast.PdfIndirectRef); isRef {
		alloc.AllocAt(ref.ObjectNum, kept)
		return structKidsResult{Value: kidsObj, Changed: true, Survivors: len(kept), Queued: true}
	}
	return structKidsResult{Value: kept, Changed: true, Survivors: len(kept)}
}

// pruneStructKid decides the fate of one kid of a structure element, reporting
// whether it must be dropped and whether it was rewritten in place.
func pruneStructKidAt(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	kid ast.PdfObject,
	removed map[int]bool,
	memo map[int]structElemVerdict,
	ownerOnRemovedPage bool,
	depth int,
	truncated *bool,
) (drop, changed bool) {
	// A bare integer is a marked-content identifier in the owner's own page
	// content (ISO 32000-1, 14.7.4.2). It survives exactly as long as that page.
	if _, isInt := kid.(ast.PdfInt); isInt {
		return ownerOnRemovedPage, false
	}
	dict, err := document.ResolveDict(ctx, doc, kid)
	if err != nil || dict == nil {
		return false, false
	}
	// A marked-content or object reference has no descendants to save. Its /Pg is
	// optional (ISO 32000-1, Tables 324 and 325) and defaults to the owning
	// element's page, so one without it dies with that page exactly as a bare
	// marked-content identifier does. Reading only its own /Pg kept an /OBJR
	// naming a link annotation on the deleted page of govdocs
	// thread_100_100587.pdf, and through the link's /GoTo the page itself.
	if kind, _ := dict["Type"].(ast.PdfName); kind == "MCR" || kind == "OBJR" {
		if _, hasOwnPage := dict["Pg"]; !hasOwnPage {
			return ownerOnRemovedPage, false
		}
		return onRemovedPage(dict, removed), false
	}
	return pruneStructElementAt(ctx, alloc, doc, kid, dict, removed, memo, depth, truncated)
}

// pruneStructElement filters one element's own /K and decides whether the
// element itself survives.
//
// An element goes only when nothing of it is left: its page is removed and no
// descendant outlived it. An element whose /Pg is removed but whose children sit
// on pages that stayed keeps its place and loses only /Pg, because dropping it
// would detach those children from the tree, and because /Pg is optional
// (ISO 32000-1, Table 323) and the content it named is gone.
func pruneStructElementAt(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	obj ast.PdfObject,
	element ast.PdfDict,
	removed map[int]bool,
	memo map[int]structElemVerdict,
	depth int,
	truncated *bool,
) (drop, changed bool) {
	// An element named twice answers with the verdict it already reached, so a
	// /K that fans out is pruned once. Only an indirect element can be named
	// twice: one written directly into its parent has no object of its own.
	elemRef, elemIsRef := obj.(ast.PdfIndirectRef)
	if elemIsRef {
		if v, done := memo[elemRef.ObjectNum]; done {
			return v.drop, v.changed
		}
	}

	pageGone := onRemovedPage(element, removed)

	kids := pruneStructKidsAt(ctx, alloc, doc, element["K"], removed, memo, pageGone, depth+1, truncated)
	survivors := kids.Survivors
	if _, hasKids := element["K"]; !hasKids {
		survivors = 0
	}

	remember := func(drop, changed bool) (bool, bool) {
		if elemIsRef {
			memo[elemRef.ObjectNum] = structElemVerdict{drop: drop, changed: changed}
		}
		return drop, changed
	}

	if pageGone && survivors == 0 {
		return remember(true, false)
	}
	if !kids.Changed && !pageGone {
		return remember(false, false)
	}

	ref, isRef := elemRef, elemIsRef
	if !isRef {
		return remember(false, kids.Changed)
	}
	newElement := make(ast.PdfDict, len(element))
	maps.Copy(newElement, element)
	if pageGone {
		delete(newElement, "Pg")
	}
	if kids.Changed && !kids.Queued {
		if kids.Value == nil {
			delete(newElement, "K")
		} else {
			newElement["K"] = kids.Value
		}
	}
	alloc.AllocAt(ref.ObjectNum, newElement)
	return remember(false, true)
}

// pruneParentTree drops the /ParentTree entries whose elements describe removed
// pages.
//
// The number tree maps each /StructParents key back to the elements marked on
// that page (ISO 32000-1, 14.7.4.4). An entry for a removed page still reaches
// its elements, and through their /Pg the page itself, so pruning the /K tree
// alone leaves the page exactly where it was.
func pruneParentTree(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	nodeObj ast.PdfObject,
	removed map[int]bool,
	depth int,
) structKidsResult {
	truncated := false
	res := pruneParentTreeAt(ctx, alloc, doc, nodeObj, removed, make(map[int]bool), depth, &truncated)
	res.Truncated = truncated
	return res
}

// pruneParentTreeAt is [pruneParentTree] carrying the nodes already walked.
//
// visited costs nothing on a well-formed tree and bounds a malformed one. The
// walk fans out over /Kids, so [maxStructTreeDepth] caps the work at
// branch^depth rather than at depth: a node whose /Kids names the same child
// twice costs 2^64. The output does not change either way, since each kid is
// written back at its own index, which is why this is a limit rather than a
// repair.
func pruneParentTreeAt(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	nodeObj ast.PdfObject,
	removed map[int]bool,
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
	node, err := document.ResolveDict(ctx, doc, nodeObj)
	if err != nil || node == nil {
		return structKidsResult{Value: nodeObj}
	}

	newNode := make(ast.PdfDict, len(node))
	maps.Copy(newNode, node)
	changed := false

	if kidsObj, hasKids := node["Kids"]; hasKids {
		if kids, kidsErr := document.ResolveArray(ctx, doc, kidsObj); kidsErr == nil {
			newKids := make(ast.PdfArray, len(kids))
			kidsChanged := false
			for i, kid := range kids {
				res := pruneParentTreeAt(ctx, alloc, doc, kid, removed, visited, depth+1, truncated)
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
					alloc.AllocAt(ref.ObjectNum, newKids)
				} else {
					newNode["Kids"] = newKids
				}
			}
		}
	}

	if numsObj, hasNums := node["Nums"]; hasNums {
		if nums, numsErr := document.ResolveArray(ctx, doc, numsObj); numsErr == nil {
			kept := make(ast.PdfArray, 0, len(nums))
			numsChanged := false
			for i := 0; i+1 < len(nums); i += 2 {
				value, dropped := pruneParentTreeValue(ctx, alloc, doc, nums[i+1], removed)
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
					alloc.AllocAt(ref.ObjectNum, kept)
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
	// store the replacement. Missing that left pdfjs issue18503.pdf, whose
	// /ParentTree and /Nums are both direct, entirely unpruned: the tree was
	// filtered and then thrown away.
	if ref, isRef := nodeObj.(ast.PdfIndirectRef); isRef {
		alloc.AllocAt(ref.ObjectNum, newNode)
		return structKidsResult{Value: nodeObj, Changed: true, Queued: true}
	}
	return structKidsResult{Value: newNode, Changed: true}
}

// pruneParentTreeValue filters one /Nums value, which is either a single element
// or an array of them. It returns the replacement and whether anything was
// dropped; a nil replacement means the whole entry goes.
func pruneParentTreeValue(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	value ast.PdfObject,
	removed map[int]bool,
) (ast.PdfObject, bool) {
	if arr, err := document.ResolveArray(ctx, doc, value); err == nil {
		kept := make(ast.PdfArray, 0, len(arr))
		for _, entry := range arr {
			if parentTreeEntryIsRemoved(ctx, alloc, doc, entry, removed) {
				continue
			}
			clearRemovedPgOnAncestors(ctx, alloc, doc, entry, removed)
			kept = append(kept, entry)
		}
		if len(kept) == len(arr) {
			return value, false
		}
		if len(kept) == 0 {
			return nil, true
		}
		if ref, isRef := value.(ast.PdfIndirectRef); isRef {
			alloc.AllocAt(ref.ObjectNum, kept)
			return value, true
		}
		return kept, true
	}
	if parentTreeEntryIsRemoved(ctx, alloc, doc, value, removed) {
		return nil, true
	}
	clearRemovedPgOnAncestors(ctx, alloc, doc, value, removed)
	return value, false
}

// clearRemovedPgOnAncestors walks an element's /P chain and drops the /Pg of
// every ancestor that names a removed page.
//
// The /K walk cannot reach these. A structure tree is meant to be symmetric, but
// real files list an element in /ParentTree whose parent never lists it back in
// /K (pdfjs issue18503.pdf), so the ancestor is reachable only upwards. It still
// holds the page: the element survives, its /P is a live reference, and 22297
// bytes of the deleted page came with it. The ancestor keeps its place and loses
// only /Pg, which is optional (ISO 32000-1, Table 323) and named content that is
// now gone.
func clearRemovedPgOnAncestors(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	obj ast.PdfObject,
	removed map[int]bool,
) {
	visited := make(map[int]bool)
	current := obj
	for range maxStructTreeDepth {
		ref, isRef := current.(ast.PdfIndirectRef)
		if !isRef || visited[ref.ObjectNum] {
			return
		}
		visited[ref.ObjectNum] = true

		element := elementForPruning(ctx, alloc, doc, ref)
		if element == nil {
			return
		}
		if onRemovedPage(element, removed) {
			newElement := make(ast.PdfDict, len(element))
			maps.Copy(newElement, element)
			delete(newElement, "Pg")
			alloc.AllocAt(ref.ObjectNum, newElement)
			element = newElement
		}
		current = element["P"]
	}
}

// elementForPruning returns the version of an element the prune should act on:
// the one already queued in the allocator when there is one, so passes compose
// instead of overwriting each other.
func elementForPruning(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	ref ast.PdfIndirectRef,
) ast.PdfDict {
	if pending, queued := alloc.Pending()[ref.ObjectNum].(ast.PdfDict); queued {
		return pending
	}
	dict, err := document.ResolveDict(ctx, doc, ref)
	if err != nil {
		return nil
	}
	return dict
}

// parentTreeEntryIsRemoved reports whether a /ParentTree value still names a
// removed page. The elements the /K walk kept have already lost their /Pg, so
// only the ones it dropped still answer yes.
func parentTreeEntryIsRemoved(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	obj ast.PdfObject,
	removed map[int]bool,
) bool {
	if ref, isRef := obj.(ast.PdfIndirectRef); isRef {
		if pending, queued := alloc.Pending()[ref.ObjectNum].(ast.PdfDict); queued {
			return onRemovedPage(pending, removed)
		}
	}
	dict, err := document.ResolveDict(ctx, doc, obj)
	if err != nil || dict == nil {
		return false
	}
	return onRemovedPage(dict, removed)
}
