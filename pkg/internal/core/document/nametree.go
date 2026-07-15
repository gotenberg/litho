package document

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// ResolveNamedDest looks up a named destination by name, searching the [Catalog]
// /Names -> /Dests name tree first, then the legacy /Dests dictionary.
// Returns nil if the destination is not found.
func ResolveNamedDest(ctx context.Context, doc *Document, name string) (ast.PdfArray, error) {
	catalog, err := ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, err
	}

	// Preferred: /Names -> /Dests name tree (PDF 1.2+).
	if namesObj, ok := catalog.Dict["Names"]; ok {
		namesDict, err := ResolveDict(ctx, doc, namesObj)
		if err != nil {
			return nil, err
		}
		if namesDict != nil {
			if destsObj, ok := namesDict["Dests"]; ok {
				result, err := searchNameTree(ctx, doc, destsObj, name)
				if err != nil {
					return nil, err
				}
				if result != nil {
					return result, nil
				}
			}
		}
	}

	// Fallback: legacy /Dests dictionary (PDF 1.1).
	if destsObj, ok := catalog.Dict["Dests"]; ok {
		destsDict, err := ResolveDict(ctx, doc, destsObj)
		if err != nil {
			return nil, err
		}
		if destsDict != nil {
			if destObj, ok := destsDict[ast.PdfName(name)]; ok {
				return resolveDestValue(ctx, doc, destObj)
			}
		}
	}

	return nil, nil
}

// maxNameTreeDepth caps the recursion depth in [searchNameTree]. PDF
// name trees in real documents rarely exceed a handful of levels; the
// cap defends against adversarial files that nest /Kids deeply or
// introduce cycles via indirect references. A search that hits the cap
// degrades to "not found" rather than overflowing the goroutine stack.
const maxNameTreeDepth = 64

// searchNameTree recursively searches a name tree node for the given key.
// Name trees use /Kids for intermediate nodes and /Names for leaf nodes,
// with optional /Limits [min max] arrays for efficient pruning.
func searchNameTree(ctx context.Context, doc *Document, nodeObj ast.PdfObject, key string) (ast.PdfArray, error) {
	return searchNameTreeAt(ctx, doc, nodeObj, key, make(map[int]bool), 0)
}

// searchNameTreeAt searches a name-tree node for key. seen visits each node
// once: a name tree is a strict tree, so a /Kids entry reached twice is
// malformed, and without the guard a node whose /Kids lists the same child
// twice (with no pruning /Limits) fans out 2^depth on a key that is absent (a
// denial of service).
func searchNameTreeAt(ctx context.Context, doc *Document, nodeObj ast.PdfObject, key string, seen map[int]bool, depth int) (ast.PdfArray, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if depth > maxNameTreeDepth {
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

	// Check /Limits to prune this branch early.
	if limitsObj, ok := nodeDict["Limits"]; ok {
		limitsArr, err := ResolveArray(ctx, doc, limitsObj)
		if err != nil {
			return nil, err
		}
		if len(limitsArr) >= 2 {
			minStr := nameTreeKey(limitsArr[0])
			maxStr := nameTreeKey(limitsArr[1])
			if minStr != "" && maxStr != "" {
				if key < minStr || key > maxStr {
					return nil, nil
				}
			}
		}
	}

	// Intermediate node: recurse into /Kids.
	if kidsObj, ok := nodeDict["Kids"]; ok {
		kidsArr, err := ResolveArray(ctx, doc, kidsObj)
		if err != nil {
			return nil, err
		}
		if kidsArr == nil {
			return nil, nil
		}
		for _, kid := range kidsArr {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			result, err := searchNameTreeAt(ctx, doc, kid, key, seen, depth+1)
			if err != nil {
				return nil, err
			}
			if result != nil {
				return result, nil
			}
		}
		return nil, nil
	}

	// Leaf node: scan /Names array (alternating key/value pairs).
	namesObj, ok := nodeDict["Names"]
	if !ok {
		return nil, nil
	}
	namesArr, err := ResolveArray(ctx, doc, namesObj)
	if err != nil {
		return nil, err
	}
	if namesArr == nil {
		return nil, nil
	}

	for i := 0; i+1 < len(namesArr); i += 2 {
		entryName := nameTreeKey(namesArr[i])
		if entryName == key {
			return resolveDestValue(ctx, doc, namesArr[i+1])
		}
	}

	return nil, nil
}

// resolveDestValue resolves a destination value to a PdfArray.
// The value may be a direct array or a dictionary containing a /D array.
func resolveDestValue(ctx context.Context, doc *Document, obj ast.PdfObject) (ast.PdfArray, error) {
	res, err := ResolveObject(ctx, doc, obj)
	if err != nil {
		return nil, err
	}

	switch v := res.(type) {
	case ast.PdfArray:
		return v, nil
	case ast.PdfDict:
		// Destination dictionaries store the actual array under /D.
		if dObj, ok := v["D"]; ok {
			dArr, err := ResolveArray(ctx, doc, dObj)
			if err != nil {
				return nil, err
			}
			if dArr != nil {
				return dArr, nil
			}
		}
	}

	return nil, nil
}

// nameTreeKey extracts the raw byte-string key from a name tree entry.
// Per ISO 32000-1 §7.9.6, name tree keys are compared as byte strings,
// not as decoded text: lookup is byte-exact. Routing keys through
// [DecodePdfString] would collapse producer-disambiguated entries
// (e.g. "kid1" vs "kid1\x01") onto the same key and the lookup would
// hit the wrong destination.
func nameTreeKey(obj ast.PdfObject) string {
	switch v := obj.(type) {
	case ast.PdfString:
		return string(v)
	case ast.PdfName:
		return string(v)
	}
	return ""
}
