package remediation

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// matchListContent matches the violation an LI raises when it holds real
// content directly (ISO 14289-2 §8.2.5.25 t2).
//
// The PDF/UA-1 sibling §7.2 t20 is a different defect and keeps its own fixer.
// t20 fires on a mis-roled child structure element, which [fixListBody] moves
// by re-parenting an indirect object; this rule fires on a content item, which
// carries no /P to repoint and reaches its owner only through the /ParentTree.
// Pointing either fixer at the other's rule makes it match and then decline,
// which reads as a repair that silently does nothing.
func matchListContent(v compliance.Violation) bool {
	return v.Rule == compliance.Rule_14289_2_8_2_5_25_t02
}

// listContentRepair is one LI whose direct content items move into a new LBody.
type listContentRepair struct {
	node  structNode
	kids  ast.PdfArray
	mcids []int
	pgRef ast.PdfIndirectRef
}

// fixListContent encloses an LI's direct content items in an LBody
// (ISO 14289-2 §8.2.5.25 t2: real content within an LI that is not enclosed in
// an Lbl shall be enclosed in an LBody). The LBody takes the slot of the first
// content item, so reading order is preserved, and any Lbl sibling stays where
// it is.
//
// Moving a content item is not the same edit as moving a structure element.
// Per ISO 32000-2 §14.7.4.4 a marked-content sequence reaches its owner only
// through the page's /StructParents key indexing the /ParentTree, so the slot
// for every moved MCID is repointed at the new LBody here. Leaving it pointing
// at the LI would trade §8.2.5.25 for an untagged-content violation
// (ISO 14289-2 §8.2.2 t1). The parent-tree fixer cannot cover this: it only
// registers pages that have no entry yet and skips any page already keyed.
//
// The repair is declined, rather than approximated, in four cases:
//
//   - a content item that is not a bare MCID integer. An /MCR may name its own
//     /Pg, and an /OBJR is reached through the referenced object's
//     /StructParent key rather than the page's MCID array, so both need a
//     different repair than a slot rewrite.
//   - an LI that already owns an LBody. A second one is not a shape any
//     producer writes, and merging into the existing one reorders content.
//   - a Div, Part or NonStruct child. The rule looks through those wrappers
//     (ISO 32000-2 §14.8.4.4), so the LI is flagged while its content sits
//     deeper than the /K this fixer rebuilds.
//   - an LI with no direct /Pg, or a /ParentTree this fixer cannot rewrite.
//
// The edit reorders logical structure without touching page content, so it is
// registered as safetyStructural.
func fixListContent(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, profile string) ([]string, error) {
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}
	stRef, ok := catalogDict["StructTreeRoot"].(ast.PdfIndirectRef)
	if !ok || stRef.ObjectNum == 0 {
		return nil, nil
	}
	stDict := resolveFromAllocOrDoc(ctx, alloc, doc, stRef)
	if stDict == nil {
		return nil, nil
	}

	// Read the ParentTree before allocating anything. A tree this fixer
	// cannot rewrite (one built from /Kids, or with malformed /Nums) means
	// the moved MCIDs would keep resolving to the LI, so the whole repair is
	// abandoned rather than half applied.
	nums, _, flat := existingParentTreeNums(ctx, alloc, doc, stDict)
	if !flat {
		return nil, nil
	}

	nodes, err := collectStructElems(ctx, alloc, doc, "LI")
	if err != nil {
		return nil, fmt.Errorf("collecting LI elements: %w", err)
	}
	roleMap := structTreeRoleMap(ctx, alloc, doc)

	var repairs []listContentRepair
	for _, node := range nodes {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("list-content remediation canceled: %w", err)
		}
		if repair, ok := planListContentRepair(ctx, doc, node, roleMap); ok {
			repairs = append(repairs, repair)
		}
	}
	if len(repairs) == 0 {
		return nil, nil
	}

	// PDF/UA-2 judges a structure element by the namespace it belongs to
	// (ISO 14289-2 §8.2.4), so the new LBody is bound to the PDF 2.0 standard
	// namespace the same way [fixSingleDocRoot] binds the Document it creates.
	var nsRef ast.PdfIndirectRef
	var haveNS, stDirty bool
	if compliance.PDFUAPart(profile) == 2 {
		ref, stWithNS := ensurePDF2Namespace(ctx, alloc, doc, stDict)
		nsRef, haveNS = ref, true
		if stWithNS != nil {
			stDict = stWithNS
			stDirty = true
		}
	}

	moved := make(map[int]map[int]ast.PdfIndirectRef)
	fixes := make([]string, 0, len(repairs))
	for _, repair := range repairs {
		lbody := ast.PdfDict{
			"Type": ast.PdfName("StructElem"),
			"S":    ast.PdfName("LBody"),
			"P":    repair.node.ref,
			"Pg":   repair.pgRef,
			"K":    mcidArray(repair.mcids),
		}
		if haveNS {
			lbody["NS"] = nsRef
		}
		lbodyRef := alloc.Alloc(lbody)

		newLI := make(ast.PdfDict, len(repair.node.dict))
		maps.Copy(newLI, repair.node.dict)
		newLI["K"] = replaceContentItems(repair.kids, lbodyRef)
		alloc.AllocAt(repair.node.ref.ObjectNum, newLI)

		if moved[repair.pgRef.ObjectNum] == nil {
			moved[repair.pgRef.ObjectNum] = make(map[int]ast.PdfIndirectRef)
		}
		for _, mcid := range repair.mcids {
			moved[repair.pgRef.ObjectNum][mcid] = lbodyRef
		}
		fixes = append(fixes,
			fmt.Sprintf("enclosed %d list-item content item(s) in an LBody", len(repair.mcids)))
	}

	if repointParentTree(ctx, alloc, doc, nums, moved) {
		stDict = withParentTreeNums(ctx, alloc, doc, stDict, nums)
		stDirty = true
	}
	if stDirty {
		alloc.AllocAt(stRef.ObjectNum, stDict)
	}
	return fixes, nil
}

// planListContentRepair decides whether one LI can be repaired, returning the
// content items to move. It mirrors the validator's view of a content item
// (ISO 14289-2 §8.2.5.25 t2 via iso32005Kids: MCID integers, /MCR and /OBJR)
// but narrows the repair to bare MCID integers, since those are the only ones
// the page's /ParentTree array addresses by index.
func planListContentRepair(ctx context.Context, doc *document.Document, node structNode, roleMap map[ast.PdfName]ast.PdfName) (listContentRepair, bool) {
	kids := resolveStructKids(ctx, doc, node.dict["K"])
	if len(kids) == 0 {
		// Per ISO 32000-1 §14.7.2 /K may be a single content item instead of
		// an array, and a lone MCID integer is exactly that form.
		// resolveStructKids wraps a lone dictionary but not a lone integer,
		// which is neither an array nor a dictionary, so it reports no kids
		// at all. Most list items carrying this violation are written that
		// way, so declining here would leave the rule effectively unfixed.
		mcid, isInt := node.dict["K"].(ast.PdfInt)
		if !isInt {
			return listContentRepair{}, false
		}
		kids = ast.PdfArray{mcid}
	}
	pgRef, ok := node.dict["Pg"].(ast.PdfIndirectRef)
	if !ok || pgRef.ObjectNum == 0 {
		return listContentRepair{}, false
	}

	var mcids []int
	var hasLBody, unsupported bool
	for _, item := range kids {
		if n, isInt := item.(ast.PdfInt); isInt {
			// An id past [maxParentTreeMCID] would size the rebuilt ParentTree
			// slot array, so decline the repair as for a negative one.
			if n < 0 || n > maxParentTreeMCID {
				unsupported = true
				continue
			}
			mcids = append(mcids, int(n))
			continue
		}
		d, err := document.ResolveDict(ctx, doc, item)
		if err != nil || d == nil {
			// An indirect MCID integer or an unresolvable kid: the
			// validator may still count it as content, so decline rather
			// than rebuild a /K whose items are not all understood.
			unsupported = true
			continue
		}
		if t, _ := document.ResolveName(ctx, doc, d["Type"]); t == "MCR" || t == "OBJR" {
			unsupported = true
			continue
		}
		switch resolvedStructType(ctx, doc, d, roleMap) {
		case "LBody":
			hasLBody = true
		case "Div", "Part", "NonStruct":
			unsupported = true
		}
	}
	if unsupported || hasLBody || len(mcids) == 0 {
		return listContentRepair{}, false
	}
	return listContentRepair{node: node, kids: kids, mcids: mcids, pgRef: pgRef}, true
}

// mcidArray renders moved marked-content ids as a structure element's /K.
func mcidArray(mcids []int) ast.PdfArray {
	out := make(ast.PdfArray, 0, len(mcids))
	for _, mcid := range mcids {
		out = append(out, ast.PdfInt(mcid))
	}
	return out
}

// replaceContentItems rebuilds an LI's /K with the container in the slot of the
// first content item, preserving the order of every other child.
func replaceContentItems(kids ast.PdfArray, containerRef ast.PdfIndirectRef) ast.PdfArray {
	out := make(ast.PdfArray, 0, len(kids))
	inserted := false
	for _, item := range kids {
		if _, isInt := item.(ast.PdfInt); isInt {
			if !inserted {
				out = append(out, containerRef)
				inserted = true
			}
			continue
		}
		out = append(out, item)
	}
	return out
}

// repointParentTree rewrites the /ParentTree slots of moved marked content so
// each MCID resolves to its new owner. It reports whether nums itself changed,
// which happens only when a page's entry is a direct array: an indirect entry
// is rewritten at its own object number so any sharing survives. Pages are
// visited in object order to keep output deterministic.
func repointParentTree(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, nums ast.PdfArray, moved map[int]map[int]ast.PdfIndirectRef) bool {
	pageNums := make([]int, 0, len(moved))
	for pageNum := range moved {
		pageNums = append(pageNums, pageNum)
	}
	slices.Sort(pageNums)

	changed := false
	for _, pageNum := range pageNums {
		pageDict := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: pageNum})
		if pageDict == nil {
			continue
		}
		spObj, ok := pageDict["StructParents"]
		if !ok {
			continue
		}
		key, err := document.ResolveInt(ctx, doc, spObj)
		if err != nil {
			continue
		}
		slot := -1
		for i := 0; i+1 < len(nums); i += 2 {
			if k, isInt := nums[i].(ast.PdfInt); isInt && int(k) == int(key) {
				slot = i + 1
				break
			}
		}
		if slot < 0 {
			// The page owns no entry, so the content was already
			// unreachable. That is the parent-tree fixer's violation.
			continue
		}
		if ref, isRef := nums[slot].(ast.PdfIndirectRef); isRef {
			arr, err := document.ResolveArray(ctx, doc, ref)
			if err != nil {
				continue
			}
			alloc.AllocAt(ref.ObjectNum, patchParentTreeSlots(arr, moved[pageNum]))
			continue
		}
		arr, err := document.ResolveArray(ctx, doc, nums[slot])
		if err != nil {
			continue
		}
		nums[slot] = patchParentTreeSlots(arr, moved[pageNum])
		changed = true
	}
	return changed
}

// patchParentTreeSlots returns a copy of a page's MCID-indexed ParentTree array
// with the moved ids repointed. The array grows when a moved id sits past its
// end, with the gap filled by nulls the way [fixParentTree] builds a fresh one.
func patchParentTreeSlots(arr ast.PdfArray, slots map[int]ast.PdfIndirectRef) ast.PdfArray {
	size := len(arr)
	for mcid := range slots {
		if mcid+1 > size {
			size = mcid + 1
		}
	}
	out := make(ast.PdfArray, size)
	for i := range out {
		if i < len(arr) {
			out[i] = arr[i]
			continue
		}
		out[i] = ast.PdfNull{}
	}
	for mcid, ref := range slots {
		out[mcid] = ref
	}
	return out
}

// withParentTreeNums stores rewritten /Nums back on the structure tree root,
// patching the /ParentTree at its own object number when it is indirect and
// returning the root to write otherwise.
func withParentTreeNums(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, stDict ast.PdfDict, nums ast.PdfArray) ast.PdfDict {
	ptObj, ok := stDict["ParentTree"]
	if !ok {
		return stDict
	}
	if ptRef, isRef := ptObj.(ast.PdfIndirectRef); isRef {
		ptDict := resolveFromAllocOrDoc(ctx, alloc, doc, ptRef)
		if ptDict == nil {
			return stDict
		}
		newPT := make(ast.PdfDict, len(ptDict)+1)
		maps.Copy(newPT, ptDict)
		newPT["Nums"] = nums
		alloc.AllocAt(ptRef.ObjectNum, newPT)
		return stDict
	}
	ptDict, err := document.ResolveDict(ctx, doc, ptObj)
	if err != nil || ptDict == nil {
		return stDict
	}
	newPT := make(ast.PdfDict, len(ptDict)+1)
	maps.Copy(newPT, ptDict)
	newPT["Nums"] = nums
	newST := make(ast.PdfDict, len(stDict)+1)
	maps.Copy(newST, stDict)
	newST["ParentTree"] = newPT
	return newST
}
