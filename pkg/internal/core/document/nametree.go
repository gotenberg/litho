package document

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// ResolveNamedDest looks up a named destination by name, searching the catalog's
// /Names -> /Dests name tree first, then the legacy /Dests dictionary.
// Returns nil if the destination is not found.
func ResolveNamedDest(ctx context.Context, doc *Document, name string) (ast.PdfArray, error) {
	catalog, err := GetCatalog(ctx, doc)
	if err != nil {
		return nil, err
	}

	// Preferred: /Names -> /Dests name tree (PDF 1.2+).
	if namesObj, ok := catalog.Dict["Names"]; ok {
		namesRes, err := ResolveObject(ctx, doc, namesObj)
		if err != nil {
			return nil, err
		}
		if namesDict, ok := namesRes.(ast.PdfDict); ok {
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
		destsRes, err := ResolveObject(ctx, doc, destsObj)
		if err != nil {
			return nil, err
		}
		if destsDict, ok := destsRes.(ast.PdfDict); ok {
			if destObj, ok := destsDict[ast.PdfName(name)]; ok {
				return resolveDestValue(ctx, doc, destObj)
			}
		}
	}

	return nil, nil
}

// searchNameTree recursively searches a name tree node for the given key.
// Name trees use /Kids for intermediate nodes and /Names for leaf nodes,
// with optional /Limits [min max] arrays for efficient pruning.
func searchNameTree(ctx context.Context, doc *Document, nodeObj ast.PdfObject, key string) (ast.PdfArray, error) {
	nodeRes, err := ResolveObject(ctx, doc, nodeObj)
	if err != nil {
		return nil, err
	}
	nodeDict, ok := nodeRes.(ast.PdfDict)
	if !ok {
		return nil, nil
	}

	// Check /Limits to prune this branch early.
	if limitsObj, ok := nodeDict["Limits"]; ok {
		limitsRes, err := ResolveObject(ctx, doc, limitsObj)
		if err != nil {
			return nil, err
		}
		if limitsArr, ok := limitsRes.(ast.PdfArray); ok && len(limitsArr) >= 2 {
			minStr := decodeLimitString(limitsArr[0])
			maxStr := decodeLimitString(limitsArr[1])
			if minStr != "" && maxStr != "" {
				if key < minStr || key > maxStr {
					return nil, nil
				}
			}
		}
	}

	// Intermediate node: recurse into /Kids.
	if kidsObj, ok := nodeDict["Kids"]; ok {
		kidsRes, err := ResolveObject(ctx, doc, kidsObj)
		if err != nil {
			return nil, err
		}
		kidsArr, ok := kidsRes.(ast.PdfArray)
		if !ok {
			return nil, nil
		}
		for _, kid := range kidsArr {
			result, err := searchNameTree(ctx, doc, kid, key)
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
	namesRes, err := ResolveObject(ctx, doc, namesObj)
	if err != nil {
		return nil, err
	}
	namesArr, ok := namesRes.(ast.PdfArray)
	if !ok {
		return nil, nil
	}

	for i := 0; i+1 < len(namesArr); i += 2 {
		entryName := decodeLimitString(namesArr[i])
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
			dRes, err := ResolveObject(ctx, doc, dObj)
			if err != nil {
				return nil, err
			}
			if dArr, ok := dRes.(ast.PdfArray); ok {
				return dArr, nil
			}
		}
	}

	return nil, nil
}

// decodeLimitString extracts a Go string from a name tree key,
// which may be a PdfString or PdfName.
func decodeLimitString(obj ast.PdfObject) string {
	switch v := obj.(type) {
	case ast.PdfString:
		return DecodePdfString(v)
	case ast.PdfName:
		return string(v)
	}
	return ""
}
