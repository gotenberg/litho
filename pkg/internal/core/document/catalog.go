package document

import (
	"context"
	"fmt"
	"log/slog"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// Catalog represents the root node of the PDF document.
type Catalog struct {
	Dict ast.PdfDict
}

// PageTree represents an intermediate node in the page tree.
type PageTree struct {
	Dict ast.PdfDict
}

// Page represents a single page in the PDF document.
type Page struct {
	Ref       ast.PdfIndirectRef
	Dict      ast.PdfDict
	Resources ast.PdfDict
	MediaBox  ast.PdfArray
	CropBox   ast.PdfArray      // Inherited; defaults to MediaBox per PDF spec
	BleedBox  ast.PdfArray      // Non-inherited; defaults to CropBox
	TrimBox   ast.PdfArray      // Non-inherited; defaults to CropBox
	ArtBox    ast.PdfArray      // Non-inherited; defaults to CropBox
	Rotate    int               // Inherited; clockwise degrees (0, 90, 180, 270)
	UserUnit  float64           // Non-inherited; default 1.0 (PDF 1.6+, §7.7.3.3)
	DPM       map[string]string // Document Part Metadata
}

// BuildPageMap constructs a reverse-lookup map from each page's [ast.PdfIndirectRef]
// to its 1-based page number, enabling clean destination resolution without
// mutating any struct fields.
func BuildPageMap(pages []*Page) map[ast.PdfIndirectRef]int {
	m := make(map[ast.PdfIndirectRef]int, len(pages))
	for i, p := range pages {
		m[p.Ref] = i + 1
	}
	return m
}

// ResolveCatalog retrieves the [Catalog] from the document trailer.
// When the trailer has no /Root, points at the wrong object type, or
// references a catalog whose /Pages is missing, the function falls back
// to scanning the indirect-object table for a dictionary tagged
// /Type /Catalog. Writers that emit incremental updates without
// repeating /Root, or that corrupt the trailer, still expose a usable
// catalog through this recovery path.
func ResolveCatalog(ctx context.Context, doc *Document) (*Catalog, error) {
	rootObj, hasRoot := doc.Index.Trailer["Root"]
	if hasRoot {
		catalogDict, err := ResolveDict(ctx, doc, rootObj)
		if err == nil && catalogDict != nil && catalogHasPages(ctx, doc, catalogDict) {
			return &Catalog{Dict: catalogDict}, nil
		}
	}

	if recovered := scanForCatalog(ctx, doc); recovered != nil {
		doc.Logger.Debug("catalog recovered via object-table scan")
		return &Catalog{Dict: recovered}, nil
	}

	if !hasRoot {
		return nil, fmt.Errorf("trailer missing /Root")
	}
	catalogDict, err := ResolveDict(ctx, doc, rootObj)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve catalog: %w", err)
	}
	if catalogDict == nil {
		return nil, fmt.Errorf("catalog is not a dictionary")
	}
	return &Catalog{Dict: catalogDict}, nil
}

// catalogHasPages reports whether a candidate catalog dict points at a
// resolvable /Pages node. Recovery rejects catalogs whose /Pages is
// absent or dangling; those catalogs would fail the subsequent page
// tree traversal, and a scan can usually find a better candidate.
func catalogHasPages(ctx context.Context, doc *Document, catalog ast.PdfDict) bool {
	pagesObj, ok := catalog["Pages"]
	if !ok {
		return false
	}
	pagesDict, err := ResolveDict(ctx, doc, pagesObj)
	return err == nil && pagesDict != nil
}

// scanForCatalog walks every indirect object in the cross-reference
// table looking for a usable catalog. It tries three paths, in order of
// confidence:
//
//  1. A dictionary tagged /Type /Catalog whose /Pages child resolves.
//  2. Any dictionary whose /Pages child resolves (de-facto catalog
//     when /Type is missing).
//  3. A /Type /Pages root node, in which case the function synthesises
//     a minimal catalog pointing at it.
//
// The scan is bounded by the xref size and only pays its cost on the
// error path.
func scanForCatalog(ctx context.Context, doc *Document) ast.PdfDict {
	// Pass 1: /Type /Catalog with /Pages child.
	if dict := scanObjects(ctx, doc, func(d ast.PdfDict) bool {
		// Silent error: a missing or malformed /Type just means the
		// candidate is not a catalog. Move on.
		typeName, _ := ResolveName(ctx, doc, d["Type"]) //nolint:errcheck // see comment.
		return typeName == "Catalog" && catalogHasPages(ctx, doc, d)
	}); dict != nil {
		return dict
	}
	// Pass 2: any dict with a resolvable /Pages child.
	if dict := scanObjects(ctx, doc, func(d ast.PdfDict) bool {
		return catalogHasPages(ctx, doc, d)
	}); dict != nil {
		return dict
	}
	// Pass 3: Pages root with no catalog above it. Synthesise one.
	pagesRef := findPagesRoot(ctx, doc)
	if pagesRef != (ast.PdfIndirectRef{}) {
		return ast.PdfDict{"Type": ast.PdfName("Catalog"), "Pages": pagesRef}
	}
	return nil
}

// scanObjects iterates every in-use uncompressed entry in the
// cross-reference table, resolves each to a dictionary, and returns
// the first one that matches the predicate. Compressed entries are
// skipped: resolving one triggers a full decompression of its
// containing object stream, and catalog / pages-root objects are
// almost always direct. Scanning compressed objects would inflate the
// cost of every recovery path by several orders of magnitude on files
// with large object streams.
func scanObjects(ctx context.Context, doc *Document, match func(ast.PdfDict) bool) ast.PdfDict {
	// Iterate a snapshot: the loop calls Resolve, which can trigger the
	// lazy xref augmentation write, so ranging Index.Entries directly
	// would race a concurrent augmentation.
	for objNum, entry := range doc.XRefEntriesSnapshot() {
		if err := ctx.Err(); err != nil {
			return nil
		}
		if objNum == 0 || entry.Type != 1 {
			continue
		}
		resolved, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: objNum, GenNum: entry.Generation})
		if err != nil {
			continue
		}
		dict, ok := resolved.(ast.PdfDict)
		if !ok {
			continue
		}
		if match(dict) {
			return dict
		}
	}
	return nil
}

// findPagesRoot locates a /Type /Pages dictionary that is not referenced
// as a /Kids entry of another Pages node. That node is the effective
// page tree root when the catalog is missing or damaged.
func findPagesRoot(ctx context.Context, doc *Document) ast.PdfIndirectRef {
	// Collect every /Type /Pages node. Same rationale as scanObjects:
	// limit the scan to uncompressed entries to avoid decompressing
	// large object streams that are unlikely to hold a pages-root
	// dictionary.
	candidates := make(map[int]ast.PdfIndirectRef)
	// Snapshot for the same reason as scanObjects: Resolve in the loop
	// can trigger the lazy xref augmentation write.
	for objNum, entry := range doc.XRefEntriesSnapshot() {
		if err := ctx.Err(); err != nil {
			return ast.PdfIndirectRef{}
		}
		if objNum == 0 || entry.Type != 1 {
			continue
		}
		resolved, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: objNum, GenNum: entry.Generation})
		if err != nil {
			continue
		}
		dict, ok := resolved.(ast.PdfDict)
		if !ok {
			continue
		}
		// Silent error: an object without a resolvable /Type is not a
		// Pages node and can be skipped.
		typeName, _ := ResolveName(ctx, doc, dict["Type"]) //nolint:errcheck // see comment.
		if typeName != "Pages" {
			continue
		}
		candidates[objNum] = ast.PdfIndirectRef{ObjectNum: objNum, GenNum: entry.Generation}
	}
	// Remove any candidate that appears as a /Parent of another node.
	for _, ref := range candidates {
		resolved, err := doc.Resolve(ctx, ref)
		if err != nil {
			continue
		}
		dict, ok := resolved.(ast.PdfDict)
		if !ok {
			continue
		}
		// Absent or wrongly typed /Parent simply means this node has
		// no parent recorded; treat it as root-eligible.
		parentRef, _ := dict["Parent"].(ast.PdfIndirectRef) //nolint:errcheck // see comment.
		if parentRef != (ast.PdfIndirectRef{}) {
			delete(candidates, ref.ObjectNum)
		}
	}
	for _, ref := range candidates {
		return ref
	}
	return ast.PdfIndirectRef{}
}

// ResolvePages extracts all [Page] entries from the [Catalog] by traversing the page tree.
func ResolvePages(ctx context.Context, doc *Document, catalog *Catalog) ([]*Page, error) {
	pagesObj, ok := catalog.Dict["Pages"]
	if !ok {
		return nil, fmt.Errorf("catalog missing /Pages")
	}

	pagesDict, err := ResolveDict(ctx, doc, pagesObj)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve pages root: %w", err)
	}
	if pagesDict == nil {
		return nil, fmt.Errorf("pages root is not a dictionary")
	}

	var pages []*Page
	// Start traversal from the root. The visited map guards against
	// cyclic page trees in damaged documents where a /Parent chain
	// loops back to an ancestor. The recursion-depth counter catches
	// deeply nested but non-cyclic trees that would otherwise exhaust
	// the stack.
	visited := make(map[int]bool)
	err = traversePageTree(ctx, doc, pagesDict, ast.PdfIndirectRef{}, nil, nil, nil, 0, &pages, visited, 0)
	if err != nil {
		return nil, fmt.Errorf("traversing page tree: %w", err)
	}

	doc.Logger.Debug("page tree traversed",
		slog.Int("pages", len(pages)),
	)

	// Associate Document Parts (PDF 2.0)
	dpartMap, err := ResolveDPartMap(ctx, doc)
	if err != nil {
		doc.Logger.Debug("resolving DPart map", slog.Any("err", err))
	}
	for _, page := range pages {
		if dpm, ok := dpartMap[page.Ref.ObjectNum]; ok {
			page.DPM = dpm
		} else {
			// Fallback: check if the page dict has a direct /DPart reference
			if dpartObj, ok := page.Dict["DPart"]; ok {
				dpartDict, err := ResolveDict(ctx, doc, dpartObj)
				if err != nil {
					doc.Logger.Debug("resolving page /DPart dict", slog.Any("err", err))
				}
				if dpartDict != nil {
					if dpmObj, ok := dpartDict["DPM"]; ok {
						dpmDict, err := ResolveDict(ctx, doc, dpmObj)
						if err != nil {
							doc.Logger.Debug("resolving page /DPM dict", slog.Any("err", err))
						}
						if dpmDict != nil {
							dpm := make(map[string]string)
							for k, v := range dpmDict {
								vRes, err := ResolveObject(ctx, doc, v)
								if err != nil {
									doc.Logger.Debug("resolving DPM value", slog.String("key", string(k)), slog.Any("err", err))
								}
								if str, ok := vRes.(ast.PdfString); ok {
									dpm[string(k)] = DecodePdfString(str)
								} else if name, ok := vRes.(ast.PdfName); ok {
									dpm[string(k)] = string(name)
								}
							}
							page.DPM = dpm
						}
					}
				}
			}
		}
	}

	return pages, nil
}

// maxPageTreeDepth caps recursion so a malformed document with a
// self-referential /Kids chain cannot exhaust the goroutine stack. Real
// PDFs never nest page trees more than a handful of levels.
const maxPageTreeDepth = 256

func traversePageTree(ctx context.Context, doc *Document, node ast.PdfDict, nodeRef ast.PdfIndirectRef, inheritedResources ast.PdfDict, inheritedMediaBox, inheritedCropBox ast.PdfArray, inheritedRotate int, pages *[]*Page, visited map[int]bool, depth int) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("page tree traversal canceled: %w", err)
	}
	if depth > maxPageTreeDepth {
		return fmt.Errorf("page tree exceeds depth limit (%d)", maxPageTreeDepth)
	}
	if nodeRef.ObjectNum != 0 {
		if visited[nodeRef.ObjectNum] {
			return fmt.Errorf("page tree cycle at object %d", nodeRef.ObjectNum)
		}
		visited[nodeRef.ObjectNum] = true
		defer delete(visited, nodeRef.ObjectNum)
	}

	// Merge Resources: the node's resources are merged with inherited ones.
	// Sub-dicts (/Font, /XObject, /ExtGState, etc.) are merged so that
	// entries from the current node take precedence, but missing entries
	// fall back to the parent. This handles PDFs where a page defines
	// and handles PDFs where a page defines partial resources.
	currentResources := mergeResourceDicts(ctx, doc, inheritedResources, node)

	// Inherit or override MediaBox
	currentMediaBox := inheritedMediaBox
	if mbObj, ok := node["MediaBox"]; ok {
		mbArr, err := ResolveArray(ctx, doc, mbObj)
		if err == nil {
			if mbArr != nil {
				currentMediaBox = mbArr
			}
		}
	}

	// Inherit or override CropBox (inheritable per PDF spec)
	currentCropBox := inheritedCropBox
	if cbObj, ok := node["CropBox"]; ok {
		cbArr, err := ResolveArray(ctx, doc, cbObj)
		if err == nil {
			if cbArr != nil {
				currentCropBox = cbArr
			}
		}
	}

	// Inherit or override Rotate (inheritable per PDF spec §7.7.3.3)
	currentRotate := inheritedRotate
	if rotObj, ok := node["Rotate"]; ok {
		if rotInt, ok := rotObj.(ast.PdfInt); ok {
			currentRotate = int(rotInt)
		}
	}

	typeStr := inferPageTreeNodeType(ctx, doc, node)
	if typeStr == "" {
		return fmt.Errorf("node missing /Type and shape is not recognisable")
	}

	switch typeStr {
	case "Pages":
		kidsObj, ok := node["Kids"]
		if !ok {
			return fmt.Errorf("pages node missing /Kids")
		}

		kidsArr, err := ResolveArray(ctx, doc, kidsObj)
		if err != nil {
			return fmt.Errorf("failed to resolve /Kids array: %w", err)
		}
		// ResolveArray returns a nil slice for valid-but-empty arrays
		// (`[]` in PDF syntax) and also for non-array types that failed
		// a type assertion. Disambiguate via the source object: when
		// kidsObj was genuinely an array, an empty slice means "no
		// kids" and the loop below is a no-op. When kidsObj was a
		// single indirect ref, treat it as a one-element array to
		// tolerate non-conforming producers.
		if kidsArr == nil {
			resolved, resolvedErr := ResolveObject(ctx, doc, kidsObj)
			if resolvedErr != nil {
				return fmt.Errorf("/Kids: %w", resolvedErr)
			}
			switch v := resolved.(type) {
			case ast.PdfArray:
				// Empty or nil-slice array: zero kids, nothing to do.
			case ast.PdfDict:
				// Direct dict (no intermediate ref): treat as single
				// child with an empty parent ref.
				_ = v
				kidsArr = ast.PdfArray{kidsObj}
			default:
				if ref, ok := kidsObj.(ast.PdfIndirectRef); ok {
					kidsArr = ast.PdfArray{ref}
					break
				}
				return fmt.Errorf("/Kids is not an array (got %T)", resolved)
			}
		}

		for _, kidObj := range kidsArr {
			var kidRef ast.PdfIndirectRef
			if r, ok := kidObj.(ast.PdfIndirectRef); ok {
				kidRef = r
			}
			kidDict, err := resolveKidDict(ctx, doc, kidObj)
			if err != nil {
				return fmt.Errorf("failed to resolve kid: %w", err)
			}
			if kidDict == nil {
				return fmt.Errorf("kid is not a dictionary")
			}

			err = traversePageTree(ctx, doc, kidDict, kidRef, currentResources, currentMediaBox, currentCropBox, currentRotate, pages, visited, depth+1)
			if err != nil {
				return fmt.Errorf("traversing page tree kid: %w", err)
			}
		}
	case "Page":
		// CropBox defaults to MediaBox per PDF spec §14.11.2
		effectiveCropBox := currentCropBox
		if effectiveCropBox == nil {
			effectiveCropBox = currentMediaBox
		}

		// BleedBox, TrimBox, ArtBox are non-inheritable; default to CropBox.
		bleedBox := resolveBoxArray(ctx, doc, node, "BleedBox", effectiveCropBox)
		trimBox := resolveBoxArray(ctx, doc, node, "TrimBox", effectiveCropBox)
		artBox := resolveBoxArray(ctx, doc, node, "ArtBox", effectiveCropBox)

		// UserUnit (PDF 1.6+, §7.7.3.3): non-inheritable scaling factor.
		// Default is 1.0 (1 unit = 1/72 inch).
		userUnit := 1.0
		if uuObj, ok := node["UserUnit"]; ok {
			if v := Float(uuObj); v > 0 {
				userUnit = v
			}
		}

		*pages = append(*pages, &Page{
			Ref:       nodeRef,
			Dict:      node,
			Resources: currentResources,
			MediaBox:  currentMediaBox,
			CropBox:   effectiveCropBox,
			BleedBox:  bleedBox,
			TrimBox:   trimBox,
			ArtBox:    artBox,
			Rotate:    currentRotate,
			UserUnit:  userUnit,
		})
	}

	return nil
}

// mergeResourceDicts merges a node's /Resources with inherited resources.
// Top-level keys (Font, XObject, etc.) from the node take precedence.
// Within each sub-dict, entries are merged with node entries winning.
// If the node has no /Resources, the inherited dict is returned as-is.
func mergeResourceDicts(ctx context.Context, doc *Document, inherited ast.PdfDict, node ast.PdfDict) ast.PdfDict {
	resObj, ok := node["Resources"]
	if !ok {
		return inherited
	}
	nodeRes, err := ResolveDict(ctx, doc, resObj)
	if err != nil {
		return inherited
	}
	if nodeRes == nil {
		return inherited
	}
	if inherited == nil {
		return nodeRes
	}

	// Merge: start with a copy of inherited, overlay node entries.
	merged := make(ast.PdfDict, len(inherited)+len(nodeRes))
	maps.Copy(merged, inherited)
	for k, v := range nodeRes {
		// For sub-dicts (Font, XObject, ExtGState, etc.), merge entries.
		inheritedSub, inheritedIsDict := inherited[k]
		if !inheritedIsDict {
			merged[k] = v
			continue
		}
		nodeSub, err := ResolveDict(ctx, doc, v)
		if err != nil {
			doc.Logger.Debug("resolving resource sub-dict for merge", slog.String("key", string(k)), slog.Any("err", err))
		}
		if nodeSub == nil {
			merged[k] = v
			continue
		}
		inheritedSubDict, err := ResolveDict(ctx, doc, inheritedSub)
		if err != nil {
			doc.Logger.Debug("resolving inherited resource sub-dict for merge", slog.String("key", string(k)), slog.Any("err", err))
		}
		if inheritedSubDict == nil {
			merged[k] = v
			continue
		}
		// Merge sub-dict: inherited entries as base, node entries override.
		sub := make(ast.PdfDict, len(inheritedSubDict)+len(nodeSub))
		maps.Copy(sub, inheritedSubDict)
		maps.Copy(sub, nodeSub)
		merged[k] = sub
	}
	return merged
}

// resolveKidDict reads a page-tree kid reference as a dictionary. When
// the object resolves to a stream instead (some writers package page
// dicts alongside a zero-length stream payload), the stream's own dict
// is returned. The wider type-coercion failure surfaces as an error so
// the caller can attribute the traversal abort to the offending kid.
func resolveKidDict(ctx context.Context, doc *Document, kidObj ast.PdfObject) (ast.PdfDict, error) {
	dict, err := ResolveDict(ctx, doc, kidObj)
	if err == nil && dict != nil {
		return dict, nil
	}
	resolved, resolveErr := ResolveObject(ctx, doc, kidObj)
	if resolveErr != nil {
		return nil, resolveErr
	}
	if stream, ok := resolved.(ast.PdfStream); ok && stream.Dict != nil {
		return stream.Dict, nil
	}
	if err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("kid is not a dictionary (got %T)", resolved)
}

// inferPageTreeNodeType returns the logical type of a page-tree node
// ("Pages" or "Page"), preferring the explicit /Type entry and falling
// back to shape-based inference for nodes that omit it. Writers
// occasionally skip /Type on intermediate nodes; downstream PDF viewers
// tolerate this by reading /Kids (Pages node) or /MediaBox/Contents
// (leaf Page node). Returns an empty string when the node is
// unrecognisable.
func inferPageTreeNodeType(ctx context.Context, doc *Document, node ast.PdfDict) string {
	if typeObj, ok := node["Type"]; ok {
		if typeStr, err := ResolveName(ctx, doc, typeObj); err == nil && typeStr != "" {
			return string(typeStr)
		}
	}
	if _, hasKids := node["Kids"]; hasKids {
		return "Pages"
	}
	if _, hasMediaBox := node["MediaBox"]; hasMediaBox {
		return "Page"
	}
	if _, hasContents := node["Contents"]; hasContents {
		return "Page"
	}
	return ""
}

// resolveBoxArray extracts a named rectangle array from a page dict,
// falling back to the provided default if the key is absent.
func resolveBoxArray(ctx context.Context, doc *Document, dict ast.PdfDict, key ast.PdfName, fallback ast.PdfArray) ast.PdfArray {
	obj, ok := dict[key]
	if !ok {
		return fallback
	}
	arr, err := ResolveArray(ctx, doc, obj)
	if err != nil {
		return fallback
	}
	if len(arr) < 4 {
		return fallback
	}
	return arr
}
