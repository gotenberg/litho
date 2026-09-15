package document

import (
	"context"
	"log/slog"

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
//
// A miss is retried with the /Limits ignored. ISO 32000-1, 7.9.6 requires keys
// to be ordered by byte value and every /Limits pair to bound its subtree, and
// producers get it wrong: pdfjs issue14864.pdf sorts by decoded text, so its
// root claims a maximum of "section*.4" while the subtree holds UTF-16 keys
// starting 0xFE. Pruning on that discards the whole tree at the root, and two
// bookmarks that every other reader resolves came back with no destination at
// all. The rescan costs a full walk only when the fast path already failed.
func searchNameTree(ctx context.Context, doc *Document, nodeObj ast.PdfObject, key string) (ast.PdfArray, error) {
	obj, err := lookupNameTree(ctx, doc, nodeObj, key, func(v ast.PdfObject) (ast.PdfObject, error) {
		arr, err := resolveDestValue(ctx, doc, v)
		if arr == nil {
			return nil, err
		}
		return arr, err
	})
	arr, _ := obj.(ast.PdfArray)
	return arr, err
}

// LookupNameTree returns the value the name tree rooted at nodeObj maps key to,
// unresolved, or nil when the tree holds no such key. Keys compare as byte
// strings (ISO 32000-1, 7.9.6), and a miss is retried with the /Limits ignored,
// as in [ResolveNamedDest].
func LookupNameTree(ctx context.Context, doc *Document, nodeObj ast.PdfObject, key string) (ast.PdfObject, error) {
	return lookupNameTree(ctx, doc, nodeObj, key, func(v ast.PdfObject) (ast.PdfObject, error) {
		return v, nil
	})
}

// lookupNameTree searches with the /Limits pruning first and then without it,
// handing each value whose key matches to leaf, which returns nil to keep
// searching.
func lookupNameTree(ctx context.Context, doc *Document, nodeObj ast.PdfObject, key string, leaf func(ast.PdfObject) (ast.PdfObject, error)) (ast.PdfObject, error) {
	result, err := searchNameTreeAt(ctx, doc, nodeObj, key, make(map[int]bool), 0, true, leaf)
	if err != nil || result != nil {
		return result, err
	}
	return searchNameTreeAt(ctx, doc, nodeObj, key, make(map[int]bool), 0, false, leaf)
}

// searchNameTreeAt searches a name-tree node for key and returns what leaf
// makes of the first matching value it accepts. seen visits each node
// once: a name tree is a strict tree, so a /Kids entry reached twice is
// malformed, and without the guard a node whose /Kids lists the same child
// twice (with no pruning /Limits) fans out 2^depth on a key that is absent (a
// denial of service).
func searchNameTreeAt(ctx context.Context, doc *Document, nodeObj ast.PdfObject, key string, seen map[int]bool, depth int, prune bool, leaf func(ast.PdfObject) (ast.PdfObject, error)) (ast.PdfObject, error) {
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
	if limitsObj, ok := nodeDict["Limits"]; ok && prune {
		limitsArr, err := ResolveArray(ctx, doc, limitsObj)
		if err != nil {
			return nil, err
		}
		if len(limitsArr) >= 2 {
			minStr := nameTreeKey(ctx, doc, limitsArr[0])
			maxStr := nameTreeKey(ctx, doc, limitsArr[1])
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
			result, err := searchNameTreeAt(ctx, doc, kid, key, seen, depth+1, prune, leaf)
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
		entryName := nameTreeKey(ctx, doc, namesArr[i])
		if entryName == key {
			return leaf(namesArr[i+1])
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

// maxDestIndirectionDepth caps how many times [ResolveDestinationPage] follows
// a /D entry into another destination dictionary. Anti-DoS guard, not
// spec-mandated: a real destination reaches its array in one hop, and the
// action form (<< /S /GoTo /D ... >>) adds at most one more. A dictionary whose
// /D points back at itself made the walk recurse until the goroutine stack
// overflowed, which Go cannot recover from, so a 700-byte file killed the
// process through the public Bookmarks and Annotations calls.
const maxDestIndirectionDepth = 8

// ResolveDestinationPage resolves a destination to a 1-based page number,
// returning 0 when no page can be determined.
//
// Per ISO 32000-1, 12.3.2, dest may be an explicit destination array, a name or
// a byte string naming an entry in the /Names /Dests tree or the legacy /Dests
// dictionary, or a dictionary holding the destination under /D. Outline items
// and link annotations share all of these forms, so both go through here.
//
// pageMap maps page object references to 1-based page numbers; build it with
// [BuildPageMap].
func ResolveDestinationPage(ctx context.Context, doc *Document, dest ast.PdfObject, pageMap map[ast.PdfIndirectRef]int) int {
	return resolveDestinationPage(ctx, doc, dest, pageMap, 0)
}

func resolveDestinationPage(ctx context.Context, doc *Document, dest ast.PdfObject, pageMap map[ast.PdfIndirectRef]int, depth int) int {
	if depth > maxDestIndirectionDepth {
		return 0
	}
	if err := ctx.Err(); err != nil {
		return 0
	}

	resolved, err := ResolveObject(ctx, doc, dest)
	if err != nil {
		doc.Logger.Debug("resolving destination object", slog.Any("err", err))
		return 0
	}

	switch v := resolved.(type) {
	case ast.PdfArray:
		return pageFromDestArray(v, pageMap)
	case ast.PdfDict:
		// A dictionary in /Dest is malformed: ISO 32000-1, Table 153 allows an
		// array, a name or a byte string there, and an action belongs in /A.
		// Real producers write the action anyway, wrapping the destination in
		// << /Type /Action /S /GoTo /D [...] >>, and every other reader follows
		// it. Four govdocs outlines (thread_300_300865 and friends) reported no
		// page at all for every one of their bookmarks. /D holds the destination
		// in both the action form and the plain << /D ... >> form, so one lookup
		// covers each.
		if inner, ok := v["D"]; ok {
			return resolveDestinationPage(ctx, doc, inner, pageMap, depth+1)
		}
		return 0
	}

	// Named destination: look up in the name tree by raw byte key. Name tree
	// keys are byte strings (ISO 32000-1, 7.9.6); decoding via
	// [DecodePdfString] would collapse producer-disambiguated destinations like
	// "kid1" and "kid1\x01" onto the same key and the wrong page would surface.
	var name string
	switch v := resolved.(type) {
	case ast.PdfString:
		name = string(v)
	case ast.PdfName:
		name = string(v)
	default:
		return 0
	}

	destArr, err := ResolveNamedDest(ctx, doc, name)
	if err != nil || destArr == nil {
		return 0
	}
	return pageFromDestArray(destArr, pageMap)
}

// pageFromDestArray extracts the 1-based page number from a destination array.
// Per ISO 32000-1, 12.3.2.2 the first element is an indirect reference to the
// page object, or a zero-based page number for a destination in a remote
// document.
//
// Deliberate leniency: the integer form is accepted for this document too.
// It is out of place in a same-document destination, but producers write it
// anyway (govdocs thread_300_300254.pdf sends a link on page 20 to
// /Dest [19 /XYZ 87 243 0]), and pdf.js resolves it the same way, with
// "else if (Number.isInteger(destRef)) { pageNumber = destRef + 1; }" in
// web/pdf_link_service.js. The index is bounded by the page map so a number
// naming no page of this document still reports none.
func pageFromDestArray(arr ast.PdfArray, pageMap map[ast.PdfIndirectRef]int) int {
	if len(arr) == 0 {
		return 0
	}
	switch first := arr[0].(type) {
	case ast.PdfIndirectRef:
		if page, ok := pageMap[first]; ok {
			return page
		}
		// The generation is not part of what [Document.Resolve] matches on, so a
		// destination naming a generation the page tree does not use still
		// resolves to that page. [BuildPageMap] records the zero-generation
		// alias; this covers the other direction, a destination carrying a
		// generation the page does not.
		return pageMap[ast.PdfIndirectRef{ObjectNum: first.ObjectNum}]
	case ast.PdfInt:
		if first < 0 || int(first) >= len(pageMap) {
			return 0
		}
		return int(first) + 1
	}
	return 0
}

// nameTreeKey extracts the raw byte-string key from a name tree entry.
// Per ISO 32000-1 §7.9.6, name tree keys are compared as byte strings,
// not as decoded text: lookup is byte-exact. Routing keys through
// [DecodePdfString] would collapse producer-disambiguated entries
// (e.g. "kid1" vs "kid1\x01") onto the same key and the lookup would
// hit the wrong destination.
//
// The key is resolved first because ISO 32000-1, 7.3.10 lets any object be
// written as an indirect reference, and producers do: pdfjs issue6204.pdf
// writes its /Names array as [18 0 R 11 0 R 19 0 R 13 0 R], with the keys in
// objects 18 and 19. Type-switching on the unresolved value yielded the empty
// string for every entry, so no key ever matched and the file's links all
// reported no destination.
func nameTreeKey(ctx context.Context, doc *Document, obj ast.PdfObject) string {
	resolved, err := ResolveObject(ctx, doc, obj)
	if err != nil {
		doc.Logger.Debug("resolving name tree key", slog.Any("err", err))
		return ""
	}
	switch v := resolved.(type) {
	case ast.PdfString:
		return string(v)
	case ast.PdfName:
		return string(v)
	}
	return ""
}
