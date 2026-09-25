package document

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// maxNumberTreeDepth caps the recursion depth in [LookupNumberTree]. Anti-DoS
// guard, not spec-mandated: number trees in real documents are a handful of
// levels deep, and the cap stops an adversarial file that nests /Kids deeply or
// introduces a cycle through indirect references. A lookup that hits the cap
// degrades to "not found" rather than overflowing the goroutine stack.
//
// It is the same bound as [maxNameTreeDepth], deliberately: ISO 32000-1, 7.9.7
// gives a number tree the same shape as the name tree of 7.9.6, so the same
// depth is generous for the same reason.
const maxNumberTreeDepth = maxNameTreeDepth

// LookupNumberTree returns the value the number tree rooted at nodeObj maps key
// to, unresolved, or nil when the tree holds no such key.
//
// A number tree keys by integer and holds its leaves in /Nums as alternating
// key and value entries, with /Kids for intermediate nodes (ISO 32000-1,
// 7.9.7). A page's /StructParents entry is a key into the structure tree's
// /ParentTree, which is what this exists to reach.
//
// /Limits is deliberately not used to prune the walk. Producers get tree bounds
// wrong often enough that [searchNameTree] has to retry its entire search with
// them ignored, and a /ParentTree lookup runs once per page, so a full walk
// costs little and cannot be defeated by a bad /Limits pair.
func LookupNumberTree(ctx context.Context, doc *Document, nodeObj ast.PdfObject, key int) (ast.PdfObject, error) {
	return searchNumberTreeAt(ctx, doc, nodeObj, key, make(map[int]bool), 0)
}

// searchNumberTreeAt searches one number-tree node for key.
//
// seen visits each node once: a number tree is a strict tree, so a /Kids entry
// reached twice is malformed, and without the guard a node whose /Kids lists
// the same child twice fans out 2^depth on a key that is absent, which is a
// denial of service.
func searchNumberTreeAt(
	ctx context.Context,
	doc *Document,
	nodeObj ast.PdfObject,
	key int,
	seen map[int]bool,
	depth int,
) (ast.PdfObject, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if depth > maxNumberTreeDepth {
		return nil, nil
	}
	if ref, ok := nodeObj.(ast.PdfIndirectRef); ok {
		if seen[ref.ObjectNum] {
			return nil, nil
		}
		seen[ref.ObjectNum] = true
	}

	nodeDict, err := ResolveDict(ctx, doc, nodeObj)
	if err != nil {
		return nil, err
	}
	if nodeDict == nil {
		return nil, nil
	}

	// A node may carry both /Nums and /Kids in a malformed file. Read the
	// leaves first: a direct hit needs no descent.
	if numsObj, ok := nodeDict["Nums"]; ok {
		nums, numsErr := ResolveArray(ctx, doc, numsObj)
		if numsErr != nil {
			return nil, numsErr
		}
		for i := 0; i+1 < len(nums); i += 2 {
			k, kErr := ResolveInt(ctx, doc, nums[i])
			if kErr != nil || int(k) != key {
				continue
			}
			return nums[i+1], nil
		}
	}

	kidsObj, ok := nodeDict["Kids"]
	if !ok {
		return nil, nil
	}
	kids, err := ResolveArray(ctx, doc, kidsObj)
	if err != nil {
		return nil, err
	}
	for _, kid := range kids {
		found, findErr := searchNumberTreeAt(ctx, doc, kid, key, seen, depth+1)
		if findErr != nil {
			return nil, findErr
		}
		if found != nil {
			return found, nil
		}
	}
	return nil, nil
}

// WalkNumberTree calls leaf with each key and value the number tree rooted at
// nodeObj holds, the value unresolved, in the order the tree lists them, and
// node with the object number of each indirect object the tree is built from:
// its node dictionaries and each /Kids or /Nums array written as an object of
// its own (ISO 32000-1, 7.9.7, Table 37). A /Nums entry whose key is not an
// integer is skipped, and a key the tree lists twice is passed to leaf twice.
//
// Each node is walked once and the descent stops at [maxNumberTreeDepth], as
// in [LookupNumberTree], so a cycle or a /Kids entry listed twice ends the
// walk there. A node that does not resolve reports its error.
func WalkNumberTree(
	ctx context.Context,
	doc *Document,
	nodeObj ast.PdfObject,
	node func(num int),
	leaf func(key int, value ast.PdfObject),
) error {
	return walkNumberTreeAt(ctx, doc, nodeObj, node, leaf, make(map[int]bool), 0)
}

// walkNumberTreeAt walks one number-tree node for [WalkNumberTree].
func walkNumberTreeAt(
	ctx context.Context,
	doc *Document,
	nodeObj ast.PdfObject,
	node func(num int),
	leaf func(key int, value ast.PdfObject),
	seen map[int]bool,
	depth int,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if depth > maxNumberTreeDepth {
		return nil
	}
	if ref, ok := nodeObj.(ast.PdfIndirectRef); ok {
		if seen[ref.ObjectNum] {
			return nil
		}
		seen[ref.ObjectNum] = true
		node(ref.ObjectNum)
	}
	nodeDict, err := ResolveDict(ctx, doc, nodeObj)
	if err != nil {
		return fmt.Errorf("resolving a number tree node: %w", err)
	}
	if nodeDict == nil {
		return nil
	}

	if numsObj, ok := nodeDict["Nums"]; ok {
		if ref, isRef := numsObj.(ast.PdfIndirectRef); isRef {
			node(ref.ObjectNum)
		}
		nums, err := ResolveArray(ctx, doc, numsObj)
		if err != nil {
			return fmt.Errorf("resolving a number tree node's /Nums: %w", err)
		}
		for i := 0; i+1 < len(nums); i += 2 {
			// Table 37 keys a number tree by integer, so a null, which
			// ResolveInt reads as 0, names no key either.
			k, err := ResolveObject(ctx, doc, nums[i])
			if key, ok := k.(ast.PdfInt); err == nil && ok {
				leaf(int(key), nums[i+1])
			}
		}
	}

	kidsObj, ok := nodeDict["Kids"]
	if !ok {
		return nil
	}
	if ref, isRef := kidsObj.(ast.PdfIndirectRef); isRef {
		node(ref.ObjectNum)
	}
	kids, err := ResolveArray(ctx, doc, kidsObj)
	if err != nil {
		return fmt.Errorf("resolving a number tree node's /Kids: %w", err)
	}
	for _, kid := range kids {
		if err := walkNumberTreeAt(ctx, doc, kid, node, leaf, seen, depth+1); err != nil {
			return err
		}
	}
	return nil
}
