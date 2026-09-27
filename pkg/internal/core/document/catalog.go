package document

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"math"

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
//
// MediaBox is the media box as the page or its nearest ancestor writes it.
// CropBox, BleedBox, TrimBox and ArtBox are the effective boundaries: the
// default ISO 32000-1, 7.7.3.3, Table 30 gives each applied, then each reduced
// to its intersection with the media box as ISO 32000-1, 14.11.2.1 requires
// (see [clampToMediaBox]). DeclaredCropBox, DeclaredBleedBox, DeclaredTrimBox
// and DeclaredArtBox hold the entries those four are derived from, as written
// and nil when absent. An inherited crop box is declared on an ancestor /Pages
// node. Dict holds the page's own entries verbatim.
//
// Resources is the page's /Resources merged key by key with every ancestor's,
// a leniency for reading (see [traversePageTree]). InheritedResources is the
// /Resources entry of the nearest ancestor on the page tree walk that states
// one, as written: the dictionary ISO 32000-1, 7.7.3.4 hands whole to a page
// stating none. An ancestor states one unless the entry is absent or null
// (ISO 32000-1, 7.3.7); any other value ends the search, as veraPDF reads it.
// A writer that gives a page /Resources of its own builds them from
// InheritedResources, never from Resources, which can name a resource the
// page cannot reach.
type Page struct {
	Ref              ast.PdfIndirectRef
	Dict             ast.PdfDict
	Resources        ast.PdfDict
	MediaBox         ast.PdfArray      // Inherited; as written
	CropBox          ast.PdfArray      // Inherited; defaults to MediaBox; clamped to MediaBox
	BleedBox         ast.PdfArray      // Non-inherited; defaults to CropBox; clamped to MediaBox
	TrimBox          ast.PdfArray      // Non-inherited; defaults to CropBox; clamped to MediaBox
	ArtBox           ast.PdfArray      // Non-inherited; defaults to CropBox; clamped to MediaBox
	DeclaredCropBox  ast.PdfArray      // Inherited; as written; nil when no node declares one
	DeclaredBleedBox ast.PdfArray      // As written; nil when absent
	DeclaredTrimBox  ast.PdfArray      // As written; nil when absent
	DeclaredArtBox   ast.PdfArray      // As written; nil when absent
	Rotate           int               // Inherited; clockwise degrees (0, 90, 180, 270)
	UserUnit         float64           // Non-inherited; default 1.0 (PDF 1.6+, ISO 32000-1, Table 30)
	DPM              map[string]string // Document Part Metadata

	// InheritedResources is the nearest ancestor's /Resources entry, as
	// written, nil when none states one.
	InheritedResources ast.PdfObject
}

// BuildPageMap constructs a reverse-lookup map from each page's [ast.PdfIndirectRef]
// to its 1-based page number, enabling clean destination resolution without
// mutating any struct fields.
//
// A page whose reference carries a generation above zero is also recorded under
// generation zero. [Document.Resolve] matches on the object number alone (see
// [Document.XRefEntry] for why), so a reference litho happily resolves to a page
// has to find that page here too. Without the alias, a destination naming the
// page under the wrong generation resolves but misses the lookup, and the
// bookmark reports no page.
//
// A document that genuinely reuses an object number for two pages would collide
// here, and the first page wins. The page tree lists each page once
// (ISO 32000-1, 7.7.3.2), so that shape is already malformed.
//
// # Corpus evidence
//
//   - govdocs thread_400_400594.pdf: holds page 12 as "40 2 R" and sends eight
//     outline entries to "40 0 R". The reference resolved, the lookup missed,
//     and every one of those bookmarks reported no page while remediating the
//     same file produced a working outline.
func BuildPageMap(pages []*Page) map[ast.PdfIndirectRef]int {
	m := make(map[ast.PdfIndirectRef]int, len(pages))
	for i, p := range pages {
		m[p.Ref] = i + 1
	}
	for i, p := range pages {
		if p.Ref.GenNum == 0 {
			continue
		}
		alias := ast.PdfIndirectRef{ObjectNum: p.Ref.ObjectNum}
		if _, taken := m[alias]; !taken {
			m[alias] = i + 1
		}
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

// CatalogRef returns the indirect reference to the document catalog. It
// prefers the trailer /Root; when that is missing or does not resolve to a
// catalog with a /Pages tree, it scans the object table for a /Type /Catalog
// object and returns its reference, mirroring [ResolveCatalog]'s recovery.
// This lets a full rewrite reproduce a document whose active trailer lost
// /Root (a damaged incremental update, or a corrupted trailer) instead of
// aborting. Returns ok=false only when no catalog object exists at all (a
// synthesised catalog has no source object to reference). The reference is
// what a full rewrite must copy so remediation catalog patches, keyed by the
// source catalog's object number, still apply.
func (d *Document) CatalogRef(ctx context.Context) (ast.PdfIndirectRef, bool) {
	if rootRef, ok := d.Index.Trailer["Root"].(ast.PdfIndirectRef); ok {
		if dict, err := ResolveDict(ctx, d, rootRef); err == nil && dict != nil && catalogHasPages(ctx, d, dict) {
			return rootRef, true
		}
	}
	// Prefer a /Type /Catalog with a resolvable /Pages child.
	if ref, ok := scanForCatalogObjRef(ctx, d, func(dict ast.PdfDict) bool {
		typeName, _ := ResolveName(ctx, d, dict["Type"]) //nolint:errcheck // a malformed /Type just means "not a catalog".
		return typeName == "Catalog" && catalogHasPages(ctx, d, dict)
	}); ok {
		return ref, true
	}
	// Fall back to any dict with a resolvable /Pages child.
	return scanForCatalogObjRef(ctx, d, func(dict ast.PdfDict) bool {
		return catalogHasPages(ctx, d, dict)
	})
}

// scanForCatalogObjRef returns the reference of the first matching indirect
// object, in the order [scanObjects] defines. It yields the reference, which
// a writer needs, rather than the resolved dict.
func scanForCatalogObjRef(ctx context.Context, doc *Document, match func(ast.PdfDict) bool) (ast.PdfIndirectRef, bool) {
	ref, dict := scanObjects(ctx, doc, match)
	return ref, dict != nil
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
	// Most confident: /Type /Catalog with a /Pages child.
	if _, dict := scanObjects(ctx, doc, func(d ast.PdfDict) bool {
		// Silent error: a missing or malformed /Type just means the
		// candidate is not a catalog. Move on.
		typeName, _ := ResolveName(ctx, doc, d["Type"]) //nolint:errcheck // see comment.
		return typeName == "Catalog" && catalogHasPages(ctx, doc, d)
	}); dict != nil {
		return dict
	}
	// Any dict with a resolvable /Pages child.
	if _, dict := scanObjects(ctx, doc, func(d ast.PdfDict) bool {
		return catalogHasPages(ctx, doc, d)
	}); dict != nil {
		return dict
	}
	// Pages root with no catalog above it. Synthesise one.
	pagesRef := findPagesRoot(ctx, doc)
	if pagesRef != (ast.PdfIndirectRef{}) {
		return ast.PdfDict{"Type": ast.PdfName("Catalog"), "Pages": pagesRef}
	}
	return nil
}

// scanObjects iterates every in-use uncompressed entry in the
// cross-reference table, resolves each to a dictionary, and returns the
// first one that matches the predicate, with its reference. Compressed
// entries are skipped: resolving one triggers a full decompression of its
// containing object stream, and catalog / pages-root objects are almost
// always direct. Scanning compressed objects would inflate the cost of
// every recovery path by several orders of magnitude on files with large
// object streams.
//
// The scan runs in ascending object-number order. A damaged file can hold
// several candidates, and ranging the table map would pick one at random
// on each run, so the same bytes would recover different catalogs and
// write different output (CONTRIBUTING "Deterministic output"). The single
// order also keeps [ResolveCatalog] and [Document.CatalogRef], which both
// recover through this scan, agreeing on the catalog.
//
// The candidates are every object the file's bytes define, the ones its table
// leaves out included, so the scan runs the raw-byte augmentation first (see
// [Document.augmentXrefFromRawScan]). Recovery is leniency on read that ISO
// 32000-1 does not describe, and the reader it follows is PDF.js: when the
// catalog the trailer names is unusable, PDF.js re-reads the file in recovery
// mode (XRef.indexObjects), which rebuilds its index from every "N G obj"
// header in the bytes and resolves the catalog through that index. veraPDF
// recovers no catalog at all and reports the file. Ranging the index without
// the augmentation saw the objects the table leaves out only when some earlier
// lookup had missed and run it, so the same bytes recovered a catalog or
// failed to depending on what the caller did first (CONTRIBUTING
// "Deterministic output").
//
// # Corpus evidence
//
//   - All corpora: 14 files recover their catalog through this scan, and
//     none holds more than one candidate catalog or page-tree root, so the
//     fixed order changes no corpus output.
//   - All corpora: no file recovers a different catalog or page count with
//     the augmentation run first or not, before running it here or after.
//   - pdfjs bug1980958.pdf: is the one file whose recovery now runs the
//     augmentation while it opens, and its catalog, its page count and its
//     PDF/A-1b and PDF/A-2b remediations do not change.
func scanObjects(ctx context.Context, doc *Document, match func(ast.PdfDict) bool) (ast.PdfIndirectRef, ast.PdfDict) {
	if err := doc.AugmentXRef(ctx); err != nil {
		// ctx is done. Ranging the index without the objects the table leaves
		// out would recover a catalog, or none, depending on when it was
		// cancelled, and the caller fails on ctx anyway.
		doc.Logger.Debug("catalog recovery: raw-byte augmentation did not complete", slog.Any("err", err))
		return ast.PdfIndirectRef{}, nil
	}
	// The loop calls Resolve, and XRefEntries yields from a copy taken under
	// the lock.
	for objNum, entry := range doc.XRefEntries() {
		if err := ctx.Err(); err != nil {
			return ast.PdfIndirectRef{}, nil
		}
		if objNum == 0 || entry.Type != 1 {
			continue
		}
		ref := ast.PdfIndirectRef{ObjectNum: objNum, GenNum: entry.Generation}
		resolved, err := doc.Resolve(ctx, ref)
		if err != nil {
			continue
		}
		if dict, ok := resolved.(ast.PdfDict); ok && match(dict) {
			return ref, dict
		}
	}
	return ast.PdfIndirectRef{}, nil
}

// findPagesRoot locates a /Type /Pages dictionary that records no /Parent,
// which makes it the effective page tree root when the catalog is missing
// or damaged. When several qualify, the lowest object number wins, for the
// reason [scanObjects] gives: the pick must not change from run to run.
func findPagesRoot(ctx context.Context, doc *Document) ast.PdfIndirectRef {
	ref, _ := scanObjects(ctx, doc, func(dict ast.PdfDict) bool {
		// Silent error: an object without a resolvable /Type is not a
		// Pages node and can be skipped.
		typeName, _ := ResolveName(ctx, doc, dict["Type"]) //nolint:errcheck // see comment.
		if typeName != "Pages" {
			return false
		}
		// Absent or wrongly typed /Parent simply means this node has
		// no parent recorded; treat it as root-eligible.
		parentRef, _ := dict["Parent"].(ast.PdfIndirectRef) //nolint:errcheck // see comment.
		return parentRef == (ast.PdfIndirectRef{})
	})
	return ref
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
	done := make(map[int]bool)
	err = traversePageTree(ctx, doc, pagesDict, ast.PdfIndirectRef{}, nil, nil, nil, nil, 0, &pages, visited, done, 0)
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

// maxPageTreeDepth caps recursion so a self-referential /Kids chain cannot
// exhaust the goroutine stack. Anti-DoS guard, not spec-mandated: ISO
// 32000-1, 7.7.3.2 makes the page tree a strict tree.
//
// This is the home note for the bound. Two other packages cap the same /Kids
// descent at 100: compliance/check.go (collectPagesRecursive) and
// remediation/fixer_annotations.go (collectPageRefs and its siblings). They
// bound THE SAME structure for the same purpose, so the spread is a real
// inconsistency rather than loose wording: a page tree nested 101 to 256 deep
// would be traversed here and silently truncated there. Real documents never
// nest that deep, so the spread is unreachable, which is why the values are
// documented rather than realigned; changing one would alter behaviour for no
// measured reason. If they are ever unified, bind by reference rather than
// copying the number, the way remediation/fixer_lang.go takes
// compliance.MaxStructTreeWalkDepth.
//
// # Corpus evidence
//
//   - All corpora: 5170 of 5171 files parse, and the deepest page tree is 4.
//   - All corpora: no file nests deeper than 100, so the gap with the
//     compliance and remediation caps is never reached.
const maxPageTreeDepth = 256

// MaxPageTreeDepth is [maxPageTreeDepth], for a writer that walks the page
// tree the reader walks and has to stop where the reader stops, a node
// deeper than the bound being one the reader never reaches.
const MaxPageTreeDepth = maxPageTreeDepth

// traversePageTree walks one page tree node, applying inherited attributes,
// and appends each leaf page to pages.
//
// # Corpus evidence
//
//   - govdocs thread_200_200147.pdf: the page tree root states /XObject
//     <</Im11 ...>> while most pages state a /Resources holding only /Font
//     and /ColorSpace, so the /Resources merge reports Im11 on every page
//     and qpdf reports it only on the first.
func traversePageTree(ctx context.Context, doc *Document, node ast.PdfDict, nodeRef ast.PdfIndirectRef, inheritedResources ast.PdfDict, inheritedResourcesEntry ast.PdfObject, inheritedMediaBox, inheritedCropBox ast.PdfArray, inheritedRotate int, pages *[]*Page, visited, done map[int]bool, depth int) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("page tree traversal canceled: %w", err)
	}
	if depth > maxPageTreeDepth {
		return fmt.Errorf("page tree exceeds depth limit (%d)", maxPageTreeDepth)
	}
	if nodeRef.ObjectNum != 0 {
		// visited is the current ancestor path: a node reached while still on
		// its own path is a genuine cycle (a /Kids chain looping back).
		if visited[nodeRef.ObjectNum] {
			return fmt.Errorf("page tree cycle at object %d", nodeRef.ObjectNum)
		}
		// done is every node already fully traversed. A page tree is a strict
		// tree (ISO 32000-1, 7.7.3.2), so a node reached again through a
		// different /Kids path is malformed; traversing it once is correct and
		// keeps the walk linear. Without this a crafted tree whose every node
		// lists the same child twice fans out 2^depth, a denial of service.
		if done[nodeRef.ObjectNum] {
			return nil
		}
		visited[nodeRef.ObjectNum] = true
		done[nodeRef.ObjectNum] = true
		defer delete(visited, nodeRef.ObjectNum)
	}

	// Merge Resources with the inherited ones: sub-dicts (/Font, /XObject,
	// /ExtGState and the rest) are merged so an entry the node states wins and
	// one it omits falls back to the ancestor.
	//
	// Deliberate leniency. ISO 32000-1, 7.7.3.4 makes /Resources inheritable,
	// which means a node that states one replaces the ancestor's entirely, and
	// that is what qpdf implements: it pushes an ancestor's /Resources onto a
	// page only when the page states none. The merge instead keeps a page whose
	// own dictionary is partial working, at the cost of listing a resource the
	// page cannot legally name.
	currentResources := mergeResourceDicts(ctx, doc, inheritedResources, node)

	// What ISO 32000-1, 7.7.3.4 hands a kid stating no /Resources: this
	// node's entry, whole, when it states one (see [Page]).
	currentResourcesEntry := inheritedResourcesEntry
	if entry, ok := node["Resources"]; ok && !isNullOrNil(entry) {
		currentResourcesEntry = entry
	}

	// Inherit or override MediaBox
	currentMediaBox := inheritedMediaBox
	if mbObj, ok := node["MediaBox"]; ok {
		mbArr, err := ResolveArray(ctx, doc, mbObj)
		if err == nil {
			if mbArr != nil {
				currentMediaBox = ResolveRectangle(ctx, doc, mbArr)
			}
		}
	}

	// Inherit or override CropBox (inheritable per ISO 32000-1, 7.7.3.3,
	// Table 30).
	currentCropBox := inheritedCropBox
	if cbObj, ok := node["CropBox"]; ok {
		cbArr, err := ResolveArray(ctx, doc, cbObj)
		if err == nil {
			if cbArr != nil {
				currentCropBox = ResolveRectangle(ctx, doc, cbArr)
			}
		}
	}

	// Inherit or override Rotate (inheritable per ISO 32000-1, 7.7.3.3, Table
	// 30). The entry may itself be indirect, which ISO 32000-1, 7.3.9 allows
	// anywhere.
	currentRotate := inheritedRotate
	if rotObj, ok := node["Rotate"]; ok {
		if ref, isRef := rotObj.(ast.PdfIndirectRef); isRef {
			if resolved, err := ResolveObject(ctx, doc, ref); err == nil {
				rotObj = resolved
			}
		}
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

		// Resolve once, then branch on what came back. Routing this through
		// ResolveArray first put the tolerance below out of reach: that
		// helper reports a non-array as an error, so a producer writing
		// /Kids as a single reference, the very case the tolerance names,
		// returned early and lost the entire page tree. A file whose only
		// fault was "/Kids 3 0 R" read as zero pages in litho and one in
		// poppler.
		resolved, err := ResolveObject(ctx, doc, kidsObj)
		if err != nil {
			return fmt.Errorf("resolving /Kids: %w", err)
		}

		var kidsArr ast.PdfArray
		switch v := resolved.(type) {
		case ast.PdfArray:
			// An empty array means zero kids and the loop below is a no-op.
			kidsArr = v
		case ast.PdfDict:
			// ISO 32000-1, 7.7.3.2 makes /Kids an array, so a lone node
			// written without one is non-conforming, but it is still that
			// node's child. Keep kidsObj rather than the resolved
			// dictionary: when it is a reference it carries the identity
			// the walk needs to reach the kid and set its parent.
			kidsArr = ast.PdfArray{kidsObj}
		default:
			if !isNullOrNil(resolved) {
				return fmt.Errorf("/Kids is not an array (got %T)", resolved)
			}
			// Null resolves to no kids, which a damaged branch may well be.
		}

		// A damaged branch costs that branch, not the document. A /Kids chain
		// that loops, or a kid that will not resolve, says nothing about its
		// siblings, so prune it and keep walking: a tree whose first kid is a
		// sound page and whose second loops still yields that page. Aborting the
		// whole walk instead discards every page already collected. Cancellation
		// is not a damaged branch and still stops the traversal.
		for _, kidObj := range kidsArr {
			var kidRef ast.PdfIndirectRef
			if r, ok := kidObj.(ast.PdfIndirectRef); ok {
				kidRef = r
			}
			kidDict, err := resolveKidDict(ctx, doc, kidObj)
			if err != nil || kidDict == nil {
				doc.Logger.Debug("pruning unresolvable page tree kid",
					slog.Int("object", kidRef.ObjectNum),
					slog.Any("err", err),
				)
				continue
			}

			err = traversePageTree(ctx, doc, kidDict, kidRef, currentResources, currentResourcesEntry, currentMediaBox, currentCropBox, currentRotate, pages, visited, done, depth+1)
			if err != nil {
				if ctxErr := ctx.Err(); ctxErr != nil {
					return fmt.Errorf("traversing page tree kid: %w", err)
				}
				doc.Logger.Debug("pruning damaged page tree branch",
					slog.Int("object", kidRef.ObjectNum),
					slog.Any("err", err),
				)
				continue
			}
		}
	case "Page":
		// The crop box defaults to the media box (ISO 32000-1, 7.7.3.3,
		// Table 30) and is reduced to its intersection with it (ISO 32000-1,
		// 14.11.2.1).
		effectiveCropBox := clampToMediaBox(currentCropBox, currentMediaBox, currentMediaBox)

		// BleedBox, TrimBox and ArtBox are not inheritable and each defaults
		// to the crop box (ISO 32000-1, 7.7.3.3, Table 30). ISO 32000-1,
		// 14.11.2.1 reduces each, like the crop box, to its intersection with
		// the media box, and with nothing else: the crop box "has no defined
		// relationship with any of the other boundaries", so a bleed box wider
		// than the crop box but inside the media box stays as written.
		declaredBleedBox := resolveBoxArray(ctx, doc, node, "BleedBox")
		declaredTrimBox := resolveBoxArray(ctx, doc, node, "TrimBox")
		declaredArtBox := resolveBoxArray(ctx, doc, node, "ArtBox")
		bleedBox := clampToMediaBox(declaredBleedBox, currentMediaBox, effectiveCropBox)
		trimBox := clampToMediaBox(declaredTrimBox, currentMediaBox, effectiveCropBox)
		artBox := clampToMediaBox(declaredArtBox, currentMediaBox, effectiveCropBox)

		// UserUnit (PDF 1.6+, ISO 32000-1, 7.7.3.3, Table 30): a positive,
		// non-inheritable scaling factor. Default is 1.0 (1 unit = 1/72
		// inch).
		userUnit := 1.0
		if uuObj, ok := node["UserUnit"]; ok {
			if v := Float(uuObj); v > 0 {
				userUnit = v
			}
		}

		*pages = append(*pages, &Page{
			Ref:              nodeRef,
			Dict:             node,
			Resources:        currentResources,
			MediaBox:         currentMediaBox,
			CropBox:          effectiveCropBox,
			BleedBox:         bleedBox,
			TrimBox:          trimBox,
			ArtBox:           artBox,
			DeclaredCropBox:  currentCropBox,
			DeclaredBleedBox: declaredBleedBox,
			DeclaredTrimBox:  declaredTrimBox,
			DeclaredArtBox:   declaredArtBox,
			Rotate:           currentRotate,
			UserUnit:         userUnit,

			InheritedResources: inheritedResourcesEntry,
		})
	}

	return nil
}

// clampToMediaBox returns the region a page boundary effectively covers: box
// reduced to its intersection with the media box, or def, the boundary's
// default, when box is absent or covers nothing.
//
// ISO 32000-1, 14.11.2.1 says the crop, bleed, trim and art boxes "shall not
// ordinarily extend beyond the boundaries of the media box. If they do, they
// are effectively reduced to their intersection with the media box." A producer
// that writes one larger than the page describes a region that does not exist,
// and taking it verbatim overstates the page. The crop box size feeds stamp
// placement and clipping, so the overstatement is not only cosmetic. def is the
// default ISO 32000-1, 7.7.3.3, Table 30 gives the boundary: the media box for
// the crop box, the crop box for the other three.
//
// A rectangle is an array of four numbers (ISO 32000-1, 7.9.5), so box counts
// as absent unless it has exactly four entries, which is also how poppler reads
// a five-entry bleed box. A media box that is not four entries leaves nothing
// to intersect with, and box stays as written.
//
// Deliberate leniency: a box that misses the media box, meets it only along an
// edge, or encloses no area itself has an intersection with no area, which
// 14.11.2.1 would make an empty boundary. An empty region describes no page, so
// the box is treated as absent and its Table 30 default applies. poppler
// agrees for a zero-area box and, for one that misses the media box, collapses
// it onto the media box edge, a zero-area rectangle no consumer can use.
//
// # Corpus evidence
//
//   - govdocs thread_200_200062: declares a 612x792 media box and a 614x794 crop
//     box. Taken verbatim, litho answered 614x794 where poppler answers 612x792.
//   - All corpora: 47 pages in 12 files declare 56 bleed, trim or art boxes
//     that extend beyond the media box. Taken verbatim, all 56 differed from
//     poppler, which reports the intersection. Clamped, all five boxes of each
//     of those pages match poppler.
//   - All corpora: no declared bleed, trim or art box misses the media box
//     entirely or has other than four entries, so the fallbacks to the
//     default change no page.
//   - govdocs thread_300_300104.pdf: every page declares a 699x879 bleed box
//     on a 612x792 media box.
//   - verapdf PDF_A-2b__veraPDF_test_suite_6-1-13-t09-fail-b.pdf: declares
//     14405-unit bleed, trim and art boxes on a 500-unit media box. The
//     ISO 19005-2, 6.1.13 size check reads the declared entries from the page
//     dictionary, not these effective boxes, and still fails the file.
func clampToMediaBox(box, media, def ast.PdfArray) ast.PdfArray {
	if len(box) != 4 {
		return def
	}
	if len(media) != 4 {
		return box
	}
	bx0, by0, bx1, by1 := boxCorners(box)
	mx0, my0, mx1, my1 := boxCorners(media)

	x0, y0 := math.Max(bx0, mx0), math.Max(by0, my0)
	x1, y1 := math.Min(bx1, mx1), math.Min(by1, my1)
	if x1 <= x0 || y1 <= y0 {
		return def
	}
	if x0 == bx0 && y0 == by0 && x1 == bx1 && y1 == by1 {
		return box
	}
	return ast.PdfArray{ast.PdfReal(x0), ast.PdfReal(y0), ast.PdfReal(x1), ast.PdfReal(y1)}
}

// boxCorners returns a rectangle's lower-left and upper-right corners, ordering
// each pair so a box written with its corners swapped still reads correctly
// (ISO 32000-1, 7.9.5).
func boxCorners(box ast.PdfArray) (x0, y0, x1, y1 float64) {
	a, b := Float(box[0]), Float(box[2])
	c, d := Float(box[1]), Float(box[3])
	return math.Min(a, b), math.Min(c, d), math.Max(a, b), math.Max(c, d)
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
//
// Deliberate leniency: ISO 32000-1, 7.7.3.2 (Table 29) allows only page
// objects and page tree nodes as kids, and 7.7.3.3 makes a page object "a
// dictionary", which a stream (7.3.8.1: "a dictionary followed by zero or
// more bytes") is not. Reading the stream's dictionary keeps the page.
// Which writers emit this shape is not recorded.
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
//
// Deliberate leniency: ISO 32000-1, 7.7.3.2, Table 29 and 7.7.3.3, Table 30
// make /Type Required on both kinds of node, as Pages and Page. The shape
// test reads /Kids as the Table 29 entry only a page tree node has, and
// /MediaBox or /Contents as Table 30 entries of a page. Which writers omit
// /Type is not recorded.
func inferPageTreeNodeType(ctx context.Context, doc *Document, node ast.PdfDict) string {
	var typeName ast.PdfName
	if typeObj, ok := node["Type"]; ok {
		if typeStr, err := ResolveName(ctx, doc, typeObj); err == nil {
			typeName = typeStr
		}
	}
	return PageTreeNodeRole(typeName, node)
}

// PageTreeNodeRole returns what the page tree walk reads node as, given the
// name its /Type resolves to, empty when it resolves to none: that name when
// there is one, "Pages" or "Page" by the node's shape when there is not, and
// an empty string when the shape says neither. It is the rule
// [ResolvePages] applies (see inferPageTreeNodeType), for a writer that has
// to write a page tree the way the reader reads it.
func PageTreeNodeRole(typeName ast.PdfName, node ast.PdfDict) string {
	if typeName != "" {
		return string(typeName)
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

// resolveBoxArray returns the rectangle a page dict declares under key, or nil
// when the entry is absent or is not an array. The caller applies the default
// and the media box intersection (see [clampToMediaBox]).
func resolveBoxArray(ctx context.Context, doc *Document, dict ast.PdfDict, key ast.PdfName) ast.PdfArray {
	obj, ok := dict[key]
	if !ok {
		return nil
	}
	arr, err := ResolveArray(ctx, doc, obj)
	if err != nil || arr == nil {
		return nil
	}
	return ResolveRectangle(ctx, doc, arr)
}

// ResolveRectangle returns arr with every indirect element replaced by the
// object it names, so a rectangle survives [Float].
//
// ISO 32000-1, 7.3.9 permits an indirect reference wherever a direct object may
// appear, including inside an array, and producers do write rectangles that
// way. Read verbatim, such coordinates are references rather than numbers, so
// the page measures 0 by 0.
//
// The array is copied only when an element needs resolving, so the ordinary
// rectangle keeps its backing array.
//
// # Corpus evidence
//
//   - pdfjs issue7872.pdf: declares /MediaBox [4 0 R 5 0 R 6 0 R 7 0 R], and
//     read verbatim the page measured 0 by 0.
func ResolveRectangle(ctx context.Context, doc *Document, arr ast.PdfArray) ast.PdfArray {
	indirect := false
	for _, elem := range arr {
		if _, ok := elem.(ast.PdfIndirectRef); ok {
			indirect = true
			break
		}
	}
	if !indirect {
		return arr
	}

	out := make(ast.PdfArray, len(arr))
	for i, elem := range arr {
		ref, ok := elem.(ast.PdfIndirectRef)
		if !ok {
			out[i] = elem
			continue
		}
		resolved, err := ResolveObject(ctx, doc, ref)
		if err != nil {
			doc.Logger.Debug("resolving indirect rectangle coordinate",
				slog.Int("object", ref.ObjectNum),
				slog.Any("err", err),
			)
			out[i] = elem
			continue
		}
		out[i] = resolved
	}
	return out
}
