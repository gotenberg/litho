package compliance

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// orphanMCIDRule selects the PDF/UA "content shall be tagged or Artifact"
// rule: ISO 14289-1 §7.1 t3, ISO 14289-2 §8.2.2 t1.
func orphanMCIDRule(profile string) Rule {
	if PDFUAPart(profile) == 2 {
		return Rule_14289_2_8_2_2_t01
	}
	return Rule_14289_1_7_1_t03
}

// checkOrphanMCIDs fires when a page content stream carries marked content
// with an /MCID that the ParentTree does not cover. Per ISO 14289-1 §7.1
// (and ISO 14289-2 §8.2.2), real content is "tagged" only when it is
// reachable from a structure element, and the spec-defined link from a
// content item back to its structure element runs forward through the page
// /StructParents key into the ParentTree, not by walking the structure tree
// downward. So a marked-content item that the ParentTree never maps is
// untagged real content even if some structure element's /K happens to name
// the same MCID. Content inside an Artifact scope is exempt per the rule.
func checkOrphanMCIDs(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	if !IsPDFUA(profile) {
		return nil
	}
	rule := orphanMCIDRule(profile)

	treeRoot, err := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if err != nil || treeRoot == nil {
		return nil // absence of any structure tree is other rules' business
	}

	var results []Violation
	for pageIdx, page := range collectPages(ctx, doc, catalog) {
		if ctx.Err() != nil {
			return results
		}
		data, err := resolvePageContentBytes(ctx, doc, page)
		if err != nil || len(data) == 0 {
			continue
		}
		ops, err := content.ParseStreamData(ctx, data)
		if err != nil {
			continue
		}
		mcids := collectNonArtifactMCIDs(ctx, ops)
		if len(mcids) == 0 {
			continue
		}
		// A ParentTree the walk could not read whole says nothing about which
		// MCIDs it covers. Reporting the ones it never reached as unreachable
		// fails a conforming file on an anti-DoS bound, so abstain instead.
		covered, determined := parentTreeMCIDs(ctx, doc, treeRoot, page)
		if !determined {
			continue
		}
		for _, mcid := range mcids {
			if !covered[mcid] {
				results = append(results, failf(rule,
					"page %d: marked content MCID %d is not reachable through the ParentTree", pageIdx+1, mcid)...)
				break // one finding per page: the page already fails, further MCIDs add no information
			}
		}
	}
	return results
}

// collectNonArtifactMCIDs returns the MCIDs of BDC properties outside any
// enclosing Artifact marked-content scope, in stream order.
func collectNonArtifactMCIDs(ctx context.Context, ops []content.Operation) []int {
	var mcids []int
	artifactDepth := 0
	inArtifact := 0
	for i, op := range ops {
		if i%200 == 0 && ctx.Err() != nil {
			return mcids
		}
		switch op.Operator {
		case "BMC", "BDC":
			artifactDepth++
			if len(op.Operands) >= 1 {
				if tag, ok := op.Operands[0].(ast.PdfName); ok && tag == "Artifact" {
					if inArtifact == 0 {
						inArtifact = artifactDepth
					}
				}
			}
			if inArtifact != 0 {
				continue
			}
			if op.Operator == "BDC" && len(op.Operands) >= 2 {
				if props, ok := op.Operands[1].(ast.PdfDict); ok {
					if mcidObj, ok := props["MCID"]; ok {
						if n, ok := mcidObj.(ast.PdfInt); ok && n >= 0 {
							mcids = append(mcids, int(n))
						}
					}
				}
			}
		case "EMC":
			if inArtifact == artifactDepth {
				inArtifact = 0
			}
			if artifactDepth > 0 {
				artifactDepth--
			}
		}
	}
	return mcids
}

// parentTreeMCIDs returns the set of MCIDs the ParentTree covers for the
// given page: the page /StructParents key selects a Nums entry whose array
// is indexed by MCID. It also reports whether that coverage is determined.
//
// A page naming no /StructParents, an unresolvable key and an absent ParentTree
// all leave the set empty and determined: the file genuinely covers nothing, so
// its content is genuinely unreachable. A walk stopped by [maxEFNameTreeDepth]
// is the one case that is not an answer.
func parentTreeMCIDs(ctx context.Context, doc *document.Document, treeRoot, page ast.PdfDict) (covered map[int]bool, determined bool) {
	covered = make(map[int]bool)
	spObj, ok := page["StructParents"]
	if !ok {
		return covered, true
	}
	key, err := document.ResolveInt(ctx, doc, spObj)
	if err != nil {
		return covered, true
	}
	ptDict, err := document.ResolveDict(ctx, doc, treeRoot["ParentTree"])
	if err != nil || ptDict == nil {
		return covered, true
	}
	arr, complete := numberTreeLookup(ctx, doc, ptDict, int(key), 0, make(map[int]bool))
	if !complete {
		return covered, false
	}
	for mcid, elem := range arr {
		if !isNullObject(elem) {
			covered[mcid] = true
		}
	}
	return covered, true
}

// parentTreeMCIDReach maps each MCID the ParentTree entry of holder's
// /StructParents names (a page's, or a form XObject's) to whether the
// structure element it names reaches the structure tree root, in the sense of
// [structElemReachesRoot]. An MCID the entry does not name is absent from the
// map. complete is false when [numberTreeLookup] stopped before reading the
// tree whole, and then an absent MCID is no evidence the file maps nothing.
func parentTreeMCIDReach(ctx context.Context, doc *document.Document, treeRoot, holder ast.PdfDict) (reach map[int]bool, complete bool) {
	reach = make(map[int]bool)
	spObj, ok := holder["StructParents"]
	if !ok {
		return reach, true
	}
	key, err := document.ResolveInt(ctx, doc, spObj)
	if err != nil {
		return reach, true
	}
	ptDict, err := document.ResolveDict(ctx, doc, treeRoot["ParentTree"])
	if err != nil || ptDict == nil {
		return reach, true
	}
	entries, complete := numberTreeLookup(ctx, doc, ptDict, int(key), 0, make(map[int]bool))
	for mcid, elem := range entries {
		if !isNullObject(elem) {
			reach[mcid] = structElemReachesRoot(ctx, doc, elem)
		}
	}
	return reach, complete
}

// structParentEntry returns the ParentTree entry the /StructParent of holder
// names (ISO 32000-1, 14.7.4.4), unresolved, and whether the tree was read
// whole: a nil entry is an absent key only when complete is true.
func structParentEntry(ctx context.Context, doc *document.Document, treeRoot, holder ast.PdfDict) (elem ast.PdfObject, complete bool) {
	key, err := document.ResolveInt(ctx, doc, holder["StructParent"])
	if err != nil {
		return nil, true
	}
	ptDict, err := document.ResolveDict(ctx, doc, treeRoot["ParentTree"])
	if err != nil || ptDict == nil {
		return nil, true
	}
	return numberTreeValue(ctx, doc, ptDict, int(key), 0, make(map[int]bool))
}

// structElemReachesRoot reports whether climbing /P from elem reaches a
// dictionary whose /Type is /StructTreeRoot. It mirrors veraPDF's
// GFSEGroupedContent.isTaggedContent: the root is recognised by its /Type, a
// missing or non-dictionary /P ends the climb, and an object met twice is a
// loop that ends it too. The climb is bounded by [maxStructTreeWalkDepth].
func structElemReachesRoot(ctx context.Context, doc *document.Document, elem ast.PdfObject) bool {
	visited := make(map[int]bool)
	cur := elem
	for range maxStructTreeWalkDepth {
		if ctx.Err() != nil {
			return false
		}
		if ref, ok := cur.(ast.PdfIndirectRef); ok {
			if visited[ref.ObjectNum] {
				return false
			}
			visited[ref.ObjectNum] = true
		}
		d, err := document.ResolveDict(ctx, doc, cur)
		if err != nil || d == nil {
			return false
		}
		if typ, _ := document.ResolveName(ctx, doc, d["Type"]); typ == "StructTreeRoot" {
			return true
		}
		next, ok := d["P"]
		if !ok {
			return false
		}
		cur = next
	}
	return false
}

// mcidValue returns the marked-content identifier obj holds, as veraPDF's
// GFOpMarkedContent.getMCID reads it: the /MCID entry counts only as an
// integer (getAttribute(MCID, COS_INTEGER)), so an /MCID written as null, a
// name or a real is no identifier and tags nothing (ISO 32000-1, 14.7.4.2
// makes it an integer). Measured on each under PDF/UA-1 and PDF/UA-2.
func mcidValue(ctx context.Context, doc *document.Document, obj ast.PdfObject) (int, bool) {
	resolved, err := document.ResolveObject(ctx, doc, obj)
	if err != nil {
		return 0, false
	}
	n, ok := resolved.(ast.PdfInt)
	return int(n), ok
}

// numberTreeLookup resolves a key in a number tree (Nums leaves, Kids
// recursion) to its array value, reporting whether it read the tree whole.
// Depth is capped by [maxEFNameTreeDepth], the shared anti-DoS guard for tree
// walks.
//
// A nil array means the key is absent only when complete is true. Stopping at
// the cap yields nil as well, and a caller that reads the two as the same thing
// fails a conforming file: absence from a walk that stopped early is not
// evidence the file names nothing.
func numberTreeLookup(ctx context.Context, doc *document.Document, node ast.PdfDict, key, depth int, seen map[int]bool) (entries ast.PdfArray, complete bool) {
	if depth > maxEFNameTreeDepth || ctx.Err() != nil {
		return nil, false
	}
	if numsObj, ok := node["Nums"]; ok {
		nums, err := document.ResolveArray(ctx, doc, numsObj)
		if err == nil {
			for i := 0; i+1 < len(nums); i += 2 {
				k, kerr := document.ResolveInt(ctx, doc, nums[i])
				if kerr != nil || int(k) != key {
					continue
				}
				// The key is here, so the answer is whole whether or not its
				// value resolves to an array.
				arr, aerr := document.ResolveArray(ctx, doc, nums[i+1])
				if aerr == nil {
					return arr, true
				}
				return nil, true
			}
		}
	}
	// A kid that stopped early leaves the whole lookup incomplete: the key may
	// sit in the part that went unread.
	complete = true
	if kidsObj, ok := node["Kids"]; ok {
		kids, err := document.ResolveArray(ctx, doc, kidsObj)
		if err == nil {
			for _, kid := range kids {
				if ref, ok := kid.(ast.PdfIndirectRef); ok {
					if seen[ref.ObjectNum] {
						continue
					}
					seen[ref.ObjectNum] = true
				}
				kd, kerr := document.ResolveDict(ctx, doc, kid)
				if kerr != nil || kd == nil {
					continue
				}
				arr, kidComplete := numberTreeLookup(ctx, doc, kd, key, depth+1, seen)
				if arr != nil {
					return arr, kidComplete
				}
				if !kidComplete {
					complete = false
				}
			}
		}
	}
	return nil, complete
}

// isNullObject reports whether obj is a PDF null (or Go nil).
func isNullObject(obj ast.PdfObject) bool {
	if obj == nil {
		return true
	}
	_, isNull := obj.(ast.PdfNull)
	return isNull
}
