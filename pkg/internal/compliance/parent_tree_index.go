package compliance

import (
	"context"
	"sync"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/intconv"
)

// numberTreeIndex is every value of a number tree, read once, answering for
// each key what [numberTreeValue] answers from the tree's root: the value of
// the first entry with that key a depth-first read meets, a node's /Nums
// before its /Kids, and for a key no entry holds whether the read was whole.
//
// A lookup of one key reads the tree from the root until it meets the key, so
// the checks that look up the /StructParent of every annotation read the
// structure parent tree once an annotation: on a file of 64,000 annotations
// that was 17 million object lookups and a gigabyte allocated for the visited
// sets. A lookup that meets its key has read exactly the nodes the whole read
// reads before that entry, with the same visited set, so the first entry of
// each key the whole read meets is the one the lookup returns, and a key the
// whole read never meets is one a lookup reads the whole tree for, stopping
// where the whole read stops.
type numberTreeIndex struct {
	values   map[int]ast.PdfObject
	complete bool

	// unread is set when an entry holds no value, where a lookup of its key
	// stops reading its node; the index then answers nothing, and each lookup
	// reads the tree itself.
	unread bool
}

// read reads node, a number tree node at depth, into the index, as
// [numberTreeValue] reads it, and reports whether it read the node whole.
func (x *numberTreeIndex) read(ctx context.Context, doc *document.Document, node ast.PdfDict, depth int, seen map[int]bool) bool {
	if depth > maxEFNameTreeDepth || ctx.Err() != nil {
		return false
	}
	if numsObj, ok := node["Nums"]; ok {
		nums, err := document.ResolveArray(ctx, doc, numsObj)
		if err == nil {
			for i := 0; i+1 < len(nums); i += 2 {
				k, kerr := document.ResolveInt(ctx, doc, nums[i])
				if kerr != nil {
					continue
				}
				if nums[i+1] == nil {
					x.unread = true
					continue
				}
				if key := intconv.Clamp(k); x.values[key] == nil {
					x.values[key] = nums[i+1]
				}
			}
		}
	}
	complete := true
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
				if !x.read(ctx, doc, kd, depth+1, seen) {
					complete = false
				}
			}
		}
	}
	return complete
}

// value returns what [numberTreeValue] returns for key from the tree's root.
func (x *numberTreeIndex) value(key int) (ast.PdfObject, bool) {
	if v := x.values[key]; v != nil {
		return v, true
	}
	return nil, x.complete
}

// parentTreeIndexes holds the structure parent trees a validation run has
// read whole, by the object number of the tree's root, for the check groups to
// share. A group that asks for a tree another is reading waits for that read.
type parentTreeIndexes struct {
	doc *document.Document

	mu    sync.Mutex
	trees map[int]*parentTreeEntry
}

// parentTreeEntry is one tree of [parentTreeIndexes], read at most once.
type parentTreeEntry struct {
	once  sync.Once
	index *numberTreeIndex // nil when the read did not end: ctx was done
}

// parentTreeIndexesKey is the context key a validation run hands its
// [parentTreeIndexes] to the check groups with.
type parentTreeIndexesKey struct{}

// withParentTreeIndexes returns ctx carrying an empty set of structure parent
// tree indexes for the validation run of doc.
func withParentTreeIndexes(ctx context.Context, doc *document.Document) context.Context {
	return context.WithValue(ctx, parentTreeIndexesKey{}, &parentTreeIndexes{doc: doc, trees: make(map[int]*parentTreeEntry)})
}

// parentTreeIndexOf returns the index of the number tree whose root is the
// indirect object ref and resolves to root, read on the first call of the
// validation run ctx carries, or nil when ctx carries no run of doc, root is
// written directly, the read did not end, or the tree holds an entry with no
// value.
func parentTreeIndexOf(ctx context.Context, doc *document.Document, ref ast.PdfObject, root ast.PdfDict) *numberTreeIndex {
	set, _ := ctx.Value(parentTreeIndexesKey{}).(*parentTreeIndexes)
	r, ok := ref.(ast.PdfIndirectRef)
	if set == nil || set.doc != doc || !ok {
		return nil
	}
	set.mu.Lock()
	entry := set.trees[r.ObjectNum]
	if entry == nil {
		entry = &parentTreeEntry{}
		set.trees[r.ObjectNum] = entry
	}
	set.mu.Unlock()
	entry.once.Do(func() {
		x := &numberTreeIndex{values: make(map[int]ast.PdfObject)}
		x.complete = x.read(ctx, doc, root, 0, make(map[int]bool))
		if ctx.Err() == nil && !x.unread {
			entry.index = x
		}
	})
	return entry.index
}
