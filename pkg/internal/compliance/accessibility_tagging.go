package compliance

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/intconv"
)

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
	entries, complete := numberTreeLookup(ctx, doc, ptDict, intconv.Clamp(key), 0, make(map[int]bool))
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
	return numberTreeValue(ctx, doc, ptDict, intconv.Clamp(key), 0, make(map[int]bool))
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
	return intconv.Clamp(n), ok
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
				if kerr != nil || intconv.Clamp(k) != key {
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
	// A /Kids array written as an object of its own is read once, as a node
	// is: its direct nodes can each name it again ([document.VisitedBefore]).
	if kidsObj, ok := node["Kids"]; ok && !document.VisitedBefore(kidsObj, seen) {
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
