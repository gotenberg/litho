package remediation

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// matchAFRelationship matches the file-specification /AFRelationship violations
// this fixer repairs: every associated file shall carry /AFRelationship under
// PDF/A-2/3/4 §6.8, and a file specification referenced by a FileAttachment
// annotation shall carry it under PDF/UA-2 §8.9.2.4.10.
func matchAFRelationship(v compliance.Violation) bool {
	return v.Rule.Clause == "6.8" ||
		(v.Rule.Specification == "ISO_14289_2" && v.Rule.Clause == "8.9.2.4.10")
}

// fixAFRelationship adds /AFRelationship /Unspecified to every file specification
// that lacks one, in both locations the rules cover: the catalog
// /Names/EmbeddedFiles name tree (§6.8) and the /FS of each FileAttachment
// annotation (PDF/UA-2 §8.9.2.4.10). /Unspecified is the honest default: the rules
// require the entry to be present, not a particular relationship, and the source
// document records no relationship to infer. The repair is additive and never
// changes what renders.
func fixAFRelationship(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, profile string) ([]string, error) {
	catalogRef, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}

	var fixes []string

	count := fixAFInEmbeddedFilesTree(ctx, doc, alloc, catalogDict)
	count += fixAFInFileAttachmentAnnots(ctx, doc, alloc, catalogDict)
	allCount, specRefs := fixAFOnAllFilespecs(ctx, doc, alloc)
	count += allCount
	if count > 0 {
		fixes = append(fixes, fmt.Sprintf("added /AFRelationship /Unspecified to %d file specs", count))
	}

	// ISO 19005-3 §6.8 t4: every file specification dictionary must be an
	// associated file, i.e. referenced from an /AF array. Wire the catalog
	// /AF for PDF/A-3 targets; parts 2 and 4 carry no such requirement at
	// this scope.
	if compliance.PDFAPart(profile) == 3 && len(specRefs) > 0 {
		if afFix := ensureCatalogAFRefs(ctx, doc, alloc, catalogRef, specRefs); afFix != "" {
			fixes = append(fixes, afFix)
		}
	}

	return fixes, nil
}

// fixAFOnAllFilespecs walks every indirect object and adds /AFRelationship
// /Unspecified to each /Type /Filespec dictionary that lacks it. The name-tree
// and annotation walks miss loose file specifications, but ISO 19005-3 §6.8 t3
// applies to every file specification dictionary, embedded or not. Returns the
// number of repaired dicts and the sorted object numbers of all filespecs.
func fixAFOnAllFilespecs(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator) (int, []int) {
	var objNums []int
	for objNum, entry := range doc.Index.Entries {
		if ctx.Err() != nil {
			break
		}
		if entry.Free || objNum <= 0 {
			continue
		}
		d := resolveFromAllocOrDoc(ctx, alloc, doc, ast.PdfIndirectRef{ObjectNum: objNum})
		if d == nil {
			continue
		}
		if tn, _ := document.ResolveName(ctx, doc, d["Type"]); tn != "Filespec" {
			continue
		}
		objNums = append(objNums, objNum)
	}
	slices.Sort(objNums)

	count := 0
	for _, objNum := range objNums {
		count += fixFileSpecAFRelationship(ctx, doc, alloc, ast.PdfIndirectRef{ObjectNum: objNum})
	}
	return count, objNums
}

// ensureCatalogAFRefs appends every filespec object number missing from the
// catalog /AF array, creating the array if absent. Returns a fix description,
// or "" when the catalog already references every filespec. The single-ref
// Factur-X variant is [ensureCatalogAF].
func ensureCatalogAFRefs(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, catalogRef ast.PdfIndirectRef, specRefs []int) string {
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return ""
	}

	existing := make(map[int]bool)
	var afArr ast.PdfArray
	if arr, err := document.ResolveArray(ctx, doc, catalogDict["AF"]); err == nil {
		afArr = slices.Clone(arr)
		for _, elem := range arr {
			if ref, ok := elem.(ast.PdfIndirectRef); ok {
				existing[ref.ObjectNum] = true
			}
		}
	}

	added := 0
	for _, objNum := range specRefs {
		if existing[objNum] {
			continue
		}
		afArr = append(afArr, ast.PdfIndirectRef{ObjectNum: objNum})
		added++
	}
	if added == 0 {
		return ""
	}

	newCatalog := make(ast.PdfDict, len(catalogDict)+1)
	maps.Copy(newCatalog, catalogDict)
	newCatalog["AF"] = afArr
	alloc.AllocAt(catalogRef.ObjectNum, newCatalog)
	return fmt.Sprintf("added %d file spec(s) to catalog /AF array", added)
}

// fixAFInEmbeddedFilesTree repairs the file specifications in the catalog
// /Names/EmbeddedFiles name tree (PDF/A-2/3/4 §6.8).
func fixAFInEmbeddedFilesTree(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, catalogDict ast.PdfDict) int {
	namesDict, err := document.ResolveDict(ctx, doc, catalogDict["Names"])
	if err != nil || namesDict == nil {
		return 0
	}
	efObj, ok := namesDict["EmbeddedFiles"]
	if !ok {
		return 0
	}
	return fixAFInNameTree(ctx, doc, alloc, efObj)
}

// fixAFInFileAttachmentAnnots repairs the file specification referenced by each
// FileAttachment annotation's /FS entry (PDF/UA-2 §8.9.2.4.10). Only an
// indirectly-referenced /FS can be repaired, since an object number is needed to
// queue the replacement; an inline file specification is left untouched.
func fixAFInFileAttachmentAnnots(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, catalogDict ast.PdfDict) int {
	pagesObj, ok := catalogDict["Pages"]
	if !ok {
		return 0
	}
	var pages []annotPageEntry
	collectPageRefs(ctx, alloc, doc, pagesObj, &pages, make(map[int]bool), 0)

	count := 0
	for _, pe := range pages {
		if ctx.Err() != nil {
			return count
		}
		annots, err := document.ResolveArray(ctx, doc, pe.dict["Annots"])
		if err != nil {
			continue
		}
		for _, annotObj := range annots {
			annotDict, err := document.ResolveDict(ctx, doc, annotObj)
			if err != nil || annotDict == nil {
				continue
			}
			if subtype, _ := document.ResolveName(ctx, doc, annotDict["Subtype"]); subtype != "FileAttachment" {
				continue
			}
			if _, ok := annotDict["FS"].(ast.PdfIndirectRef); !ok {
				continue
			}
			count += fixFileSpecAFRelationship(ctx, doc, alloc, annotDict["FS"])
		}
	}
	return count
}

func fixAFInNameTree(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, obj ast.PdfObject) int {
	d, err := document.ResolveDict(ctx, doc, obj)
	if err != nil || d == nil {
		return 0
	}

	count := 0

	// Process /Names array (leaf node): [name1, ref1, name2, ref2, ...]
	if namesArr, ok := d["Names"]; ok {
		if arr, err := document.ResolveArray(ctx, doc, namesArr); err == nil {
			for i := 1; i < len(arr); i += 2 {
				count += fixFileSpecAFRelationship(ctx, doc, alloc, arr[i])
			}
		}
	}

	// Recurse into /Kids (intermediate nodes).
	if kidsArr, ok := d["Kids"]; ok {
		if arr, err := document.ResolveArray(ctx, doc, kidsArr); err == nil {
			for _, kid := range arr {
				count += fixAFInNameTree(ctx, doc, alloc, kid)
			}
		}
	}

	return count
}

func fixFileSpecAFRelationship(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, obj ast.PdfObject) int {
	var ref ast.PdfIndirectRef
	if r, ok := obj.(ast.PdfIndirectRef); ok {
		ref = r
	}

	// Alloc-aware read: the name-tree, annotation, and all-objects walks
	// overlap, so a second visit must see the first visit's pending write
	// and skip instead of re-allocating from the stale on-disk dict.
	var fsDict ast.PdfDict
	if ref.ObjectNum > 0 {
		fsDict = resolveFromAllocOrDoc(ctx, alloc, doc, ref)
	}
	if fsDict == nil {
		resolved, err := document.ResolveDict(ctx, doc, obj)
		if err != nil || resolved == nil {
			return 0
		}
		fsDict = resolved
	}

	if _, ok := fsDict["AFRelationship"]; ok {
		return 0 // already present
	}

	newFS := make(ast.PdfDict, len(fsDict)+1)
	maps.Copy(newFS, fsDict)
	newFS["AFRelationship"] = ast.PdfName("Unspecified")

	if ref.ObjectNum > 0 {
		alloc.AllocAt(ref.ObjectNum, newFS)
	}
	return 1
}
