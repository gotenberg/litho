package mutations

import (
	"bytes"
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// treeLeafEntries caps the key and value pairs one leaf of a number or name
// tree litho writes holds. ISO 32000-1, 7.9.6 and 7.9.7 set no bound, but ISO
// 19005-1, 6.1.12 (Table 1) caps an array at 8191 elements, so a /Nums or
// /Names array holding more than 4095 pairs would make a PDF/A-1 output fail
// for the tree litho built. Not an anti-DoS guard: it shapes the output.
const treeLeafEntries = 2048

// buildNumberTree writes entries as a number tree (ISO 32000-1, 7.9.7) and
// returns its root. A tree of at most [treeLeafEntries] keys is one node
// holding /Nums; a larger one is split into leaves under intermediate nodes,
// each with the /Limits Table 37 requires of it.
func buildNumberTree(alloc *writer.ObjAllocator, entries map[int]ast.PdfObject) ast.PdfIndirectRef {
	keys := slices.Sorted(maps.Keys(entries))
	pairs := make([]treePair, len(keys))
	for i, key := range keys {
		pairs[i] = treePair{key: ast.PdfInt(key), value: entries[key]}
	}
	return buildTree(alloc, pairs, "Nums")
}

// buildNameTree writes entries as a name tree (ISO 32000-1, 7.9.6), keyed by
// the byte strings of its map, in the lexical order the clause requires.
func buildNameTree(alloc *writer.ObjAllocator, entries map[string]ast.PdfObject) ast.PdfIndirectRef {
	keys := slices.SortedFunc(maps.Keys(entries), func(a, b string) int { return bytes.Compare([]byte(a), []byte(b)) })
	pairs := make([]treePair, len(keys))
	for i, key := range keys {
		pairs[i] = treePair{key: ast.PdfString(key), value: entries[key]}
	}
	return buildTree(alloc, pairs, "Names")
}

// treePair is one key and value of a number or name tree.
type treePair struct {
	key   ast.PdfObject
	value ast.PdfObject
}

// buildTree writes pairs, sorted, as a tree whose leaves hold them under
// leafKey, and returns its root. The root carries no /Limits (ISO 32000-1,
// Table 36 and Table 37).
func buildTree(alloc *writer.ObjAllocator, pairs []treePair, leafKey ast.PdfName) ast.PdfIndirectRef {
	if len(pairs) <= treeLeafEntries {
		leaf := make(ast.PdfArray, 0, 2*len(pairs))
		for _, pair := range pairs {
			leaf = append(leaf, pair.key, pair.value)
		}
		return alloc.Alloc(ast.PdfDict{leafKey: leaf})
	}
	type node struct {
		ref         ast.PdfIndirectRef
		first, last ast.PdfObject
	}
	var level []node
	for start := 0; start < len(pairs); start += treeLeafEntries {
		chunk := pairs[start:min(start+treeLeafEntries, len(pairs))]
		leaf := make(ast.PdfArray, 0, 2*len(chunk))
		for _, pair := range chunk {
			leaf = append(leaf, pair.key, pair.value)
		}
		first, last := chunk[0].key, chunk[len(chunk)-1].key
		ref := alloc.Alloc(ast.PdfDict{leafKey: leaf, "Limits": ast.PdfArray{first, last}})
		level = append(level, node{ref: ref, first: first, last: last})
	}
	for len(level) > treeLeafEntries {
		var up []node
		for start := 0; start < len(level); start += treeLeafEntries {
			chunk := level[start:min(start+treeLeafEntries, len(level))]
			kids := make(ast.PdfArray, len(chunk))
			for i, n := range chunk {
				kids[i] = n.ref
			}
			first, last := chunk[0].first, chunk[len(chunk)-1].last
			ref := alloc.Alloc(ast.PdfDict{"Kids": kids, "Limits": ast.PdfArray{first, last}})
			up = append(up, node{ref: ref, first: first, last: last})
		}
		level = up
	}
	kids := make(ast.PdfArray, len(level))
	for i, n := range level {
		kids[i] = n.ref
	}
	return alloc.Alloc(ast.PdfDict{"Kids": kids})
}

// readNameTree returns every key and value of the name tree rooted at obj
// (ISO 32000-1, 7.9.6), read from alloc first, keyed by the raw bytes of each
// key. ok is false when the tree cannot be read whole: a node that does not
// resolve, a /Names array of odd length or with a key that is not a string, or
// a descent past [maxStructTreeDepth]. An absent tree reads as an empty one.
// The first value listed for a key is the one kept, as a lookup finds it.
func readNameTree(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject) (map[string]ast.PdfObject, bool) {
	entries := make(map[string]ast.PdfObject)
	if obj == nil {
		return entries, true
	}
	visited := make(map[int]bool)
	var walk func(node ast.PdfObject, depth int) bool
	walk = func(node ast.PdfObject, depth int) bool {
		if depth > maxStructTreeDepth || ctx.Err() != nil {
			return false
		}
		if ref, isRef := node.(ast.PdfIndirectRef); isRef {
			if visited[ref.ObjectNum] {
				return true
			}
			visited[ref.ObjectNum] = true
		}
		dict, err := resolveDictAllocFirst(ctx, alloc, doc, node)
		if err != nil || dict == nil {
			return false
		}
		if namesObj, has := dict["Names"]; has {
			names, err := resolveArrayAllocFirst(ctx, alloc, doc, namesObj)
			if err != nil || len(names)%2 != 0 {
				return false
			}
			for i := 0; i < len(names); i += 2 {
				key, err := resolveAllocFirst(ctx, alloc, doc, names[i])
				if err != nil {
					return false
				}
				s, isString := key.(ast.PdfString)
				if !isString {
					return false
				}
				if _, seen := entries[string(s)]; !seen {
					entries[string(s)] = names[i+1]
				}
			}
		}
		if kidsObj, has := dict["Kids"]; has {
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
		return nil, false
	}
	return entries, true
}

// documentLang is [catalogLang] for doc's catalog as the file holds it.
func documentLang(ctx context.Context, doc *document.Document) (ast.PdfString, string) {
	catalogRef, ok := CatalogRef(ctx, doc)
	if !ok {
		return "", ""
	}
	catalog, err := CatalogDict(ctx, doc, catalogRef)
	if err != nil {
		return "", ""
	}
	return catalogLang(ctx, nil, doc, catalog)
}

// catalogLang returns the catalog's /Lang string and the natural language it
// names (ISO 32000-1, 14.9.2.1), "" when it names none.
func catalogLang(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalog ast.PdfDict) (ast.PdfString, string) {
	obj, err := resolveAllocFirst(ctx, alloc, doc, catalog["Lang"])
	if err != nil {
		return "", ""
	}
	s, _ := obj.(ast.PdfString)
	return s, strings.TrimSpace(document.DecodePdfString(s))
}
