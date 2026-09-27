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
// this fixer repairs. Exactly three rules state the requirement:
//
//   - ISO 19005-3 §6.8 t3, covered by the clause below. ISO 19005-2 states no
//     AFRelationship requirement at all, so "PDF/A-2/3/4 §6.8" was wrong.
//   - ISO 14289-2 §8.9.2.4.10 t1, a file specification referenced by a
//     FileAttachment annotation.
//   - ISO 19005-4 §6.9 t4, which numbers the SAME requirement differently and
//     so went unclaimed while [fixAFRelationship] already knew how to repair it.
//
// The part-4 arm matches on the full specification, clause and test number.
// Never clausePrefix: clause 6.9 is an AcroForm rule in ISO 19005-1, the
// optional-content configuration rules in ISO 19005-2 and -3, and the embedded
// file rules in ISO 19005-4.
func matchAFRelationship(v compliance.Violation) bool {
	return v.Rule.Clause == "6.8" ||
		(v.Rule.Specification == "ISO_14289_2" && v.Rule.Clause == "8.9.2.4.10") ||
		(v.Rule.Specification == "ISO_19005_4" && v.Rule.Clause == "6.9" && v.Rule.TestNumber == 4)
}

// fixAFRelationship adds /AFRelationship /Unspecified to every file specification
// that lacks one, wherever the rules find it: the catalog /Names/EmbeddedFiles
// name tree (§6.8), the /FS of each FileAttachment annotation (PDF/UA-2
// §8.9.2.4.10), every indirect /Type /Filespec dictionary, and every one written
// inline in any object ([inlineFileSpecAF]). /Unspecified is the honest default:
// the rules require the entry to be present, not a particular relationship, and
// the source document records no relationship to infer. The repair is additive
// and never changes what renders.
func fixAFRelationship(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, profile string) ([]string, error) {
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}

	var fixes []string

	treeSpecs := make(map[int]bool)
	count := fixAFInEmbeddedFilesTree(ctx, doc, alloc, catalogDict, treeSpecs)
	count += fixAFInFileAttachmentAnnots(ctx, doc, alloc, catalogDict)
	allCount, specRefs := fixAFOnAllFilespecs(ctx, doc, alloc)
	count += allCount

	// ISO 19005-3 §6.8 t4: every file specification dictionary must be an
	// associated file, i.e. referenced from an /AF array. Wire the catalog
	// /AF for PDF/A-3 targets; parts 2 and 4 carry no such requirement at
	// this scope.
	part3 := compliance.PDFAPart(profile) == 3
	inline := inlineFileSpecAF{alloc: alloc, promote: part3}
	if _, err := rewriteInlineFileSpecs(ctx, alloc, doc, inline.repair); err != nil {
		return nil, err
	}
	count += inline.relationships
	if part3 {
		specRefs = associatedFileSpecRefs(specRefs, treeSpecs, inline.promoted)
	}
	if count > 0 {
		fixes = append(fixes, fmt.Sprintf("added /AFRelationship /Unspecified to %d file specs", count))
	}
	if len(inline.promoted) > 0 {
		fixes = append(fixes, fmt.Sprintf("moved %d inline file spec(s) to objects of their own", len(inline.promoted)))
	}
	if part3 && len(specRefs) > 0 {
		if afFix := ensureCatalogAFRefs(ctx, doc, alloc, catalogRef, specRefs); afFix != "" {
			fixes = append(fixes, afFix)
		}
	}

	return fixes, nil
}

// associatedFileSpecRefs returns the sorted object numbers of every file
// specification the catalog /AF must list: the ones typed /Filespec, the
// ones the /Names /EmbeddedFiles tree names, and the ones promoted from
// inline dictionaries.
//
// ISO 32000-1, 7.11.4 makes a value of the /EmbeddedFiles tree a file
// specification whatever its /Type says, and compliance judges it as one
// under ISO 19005-3, 6.8 t4, so the sweep's /Type test alone left a mistyped
// one unlisted.
//
// # Corpus evidence
//
//   - pdfjs issue9367.pdf: its tree names three file specifications typed
//     /F, and each kept 6.8 t4 after a PDF/A-3b remediation.
func associatedFileSpecRefs(typed []int, tree map[int]bool, promoted []int) []int {
	all := make(map[int]bool, len(typed)+len(tree)+len(promoted))
	for _, objNum := range typed {
		all[objNum] = true
	}
	for objNum := range tree {
		all[objNum] = true
	}
	for _, objNum := range promoted {
		all[objNum] = true
	}
	return slices.Sorted(maps.Keys(all))
}

// inlineFileSpecAF repairs the file specifications written inline, the
// [inlineFileSpecRepair] [fixAFRelationship] hands [rewriteInlineFileSpecs]. It
// gives each one without /AFRelationship the /Unspecified the other walks give
// (ISO 19005-3, 6.8 t3 and ISO 19005-4, 6.9 t4), and with promote set moves it
// to an object of its own, so an /AF array can list it (ISO 19005-3, 6.8 t4).
//
// An /AF array lists indirect objects (ISO 32000-2, 14.13), and veraPDF counts
// an inline file specification as associated with nothing, even one an /AF
// array lists inline: measured on an inline /AF entry of a Link annotation,
// which fails 6.8 t4. ISO 32000-1, 7.3.10 makes an indirect reference the same
// value as the object it names, so the move changes nothing a reader resolves,
// and an /AF array that listed the dictionary inline lists the object after it.
//
// # Corpus evidence
//
//   - pdfjs issue14203.pdf: a FileAttachment's inline /FS kept 6.8 t4 after
//     a PDF/A-3b remediation.
//   - litho afref_3b.pdf: a FileAttachment's inline /FS and a Link's /GoToR
//     /F kept 6.8 t4 after a PDF/A-3b remediation, on litho and veraPDF
//     alike.
//   - litho inlinefs_4.pdf: the inline /AF entries of a Link written inline,
//     of a form XObject and of the catalog kept 6.8 t4, and the form's 6.8 t3,
//     on veraPDF after a PDF/A-3b remediation, where litho reported none.
type inlineFileSpecAF struct {
	alloc   *writer.ObjAllocator
	promote bool
	// promoted holds the object numbers of the file specifications moved out
	// of line, in the order they moved.
	promoted []int
	// relationships counts the file specifications given an /AFRelationship.
	relationships int
}

func (r *inlineFileSpecAF) repair(_ int, _ string, spec ast.PdfDict) (ast.PdfObject, bool) {
	patched, added := spec, false
	if _, ok := spec["AFRelationship"]; !ok {
		patched = maps.Clone(spec)
		patched["AFRelationship"] = ast.PdfName("Unspecified")
		r.relationships++
		added = true
	}
	if !r.promote {
		return patched, added
	}
	ref := r.alloc.Alloc(patched)
	r.promoted = append(r.promoted, ref.ObjectNum)
	return ref, true
}

// fixAFOnAllFilespecs walks every indirect object and adds /AFRelationship
// /Unspecified to each /Type /Filespec dictionary that lacks it. The name-tree
// and annotation walks miss loose file specifications, but ISO 19005-3 §6.8 t3
// applies to every file specification dictionary, embedded or not. Returns the
// number of repaired dicts and the sorted object numbers of all filespecs.
func fixAFOnAllFilespecs(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator) (int, []int) {
	var objNums []int
	for objNum, entry := range sweptObjects(doc) {
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
// /Names/EmbeddedFiles name tree (PDF/A-2/3/4 §6.8), and records in specs the
// object number of each one the tree names indirectly.
func fixAFInEmbeddedFilesTree(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, catalogDict ast.PdfDict, specs map[int]bool) int {
	namesDict, err := document.ResolveDict(ctx, doc, catalogDict["Names"])
	if err != nil || namesDict == nil {
		return 0
	}
	efObj, ok := namesDict["EmbeddedFiles"]
	if !ok {
		return 0
	}
	return fixAFInNameTree(ctx, doc, alloc, efObj, make(map[int]bool), 0, specs)
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
		// The array and each annotation are read through the allocator. The
		// annotations fixer runs first, writes the /Annots it filtered at its
		// own object number and nulls what it removed, and a FileAttachment
		// read from the file's array came back with its repaired /FS. An
		// earlier fixer may also have rewritten a kept annotation
		// (filespec-fuf patches a direct /FS through it), and reading the
		// file's copy made the AllocAt below discard that pending fix.
		for _, annotObj := range liveArray(ctx, alloc, doc, pe.dict["Annots"]) {
			annotDict := liveAnnotation(ctx, alloc, doc, annotObj)
			if annotDict == nil {
				continue
			}
			if subtype, _ := document.ResolveName(ctx, doc, annotDict["Subtype"]); subtype != "FileAttachment" {
				continue
			}
			switch fsObj := annotDict["FS"].(type) {
			case ast.PdfIndirectRef:
				count += fixFileSpecAFRelationship(ctx, doc, alloc, fsObj)
			case ast.PdfDict:
				count += fixDirectFileSpecInAnnot(alloc, annotObj, annotDict, fsObj)
			}
		}
	}
	return count
}

// fixAFInNameTree walks an EmbeddedFiles name tree, adding /AFRelationship to
// each filespec and recording in specs the object number of each indirect one
// that resolves to a dictionary. seen visits each node once and depth is
// capped: a name tree is a strict tree, so a /Kids tree whose every node lists
// the same child twice would otherwise fan out 2^depth (a denial of service).
func fixAFInNameTree(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, obj ast.PdfObject, seen map[int]bool, depth int, specs map[int]bool) int {
	if depth > maxNameTreeWalkDepth {
		return 0
	}
	if ref, ok := obj.(ast.PdfIndirectRef); ok {
		if seen[ref.ObjectNum] {
			return 0
		}
		seen[ref.ObjectNum] = true
	}
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
				if ref, ok := arr[i].(ast.PdfIndirectRef); ok && ref.ObjectNum > 0 && resolveFromAllocOrDoc(ctx, alloc, doc, ref) != nil {
					specs[ref.ObjectNum] = true
				}
			}
		}
	}

	// Recurse into /Kids (intermediate nodes).
	if kidsArr, ok := d["Kids"]; ok {
		if arr, err := document.ResolveArray(ctx, doc, kidsArr); err == nil {
			for _, kid := range arr {
				count += fixAFInNameTree(ctx, doc, alloc, kid, seen, depth+1, specs)
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

	// A direct file specification has no object number to write to. Reporting a
	// repair that was discarded makes remediation claim progress it never made,
	// and the same defect is still there on the next run.
	if ref.ObjectNum <= 0 {
		return 0
	}
	alloc.AllocAt(ref.ObjectNum, newFS)
	return 1
}

// fixDirectFileSpecInAnnot adds /AFRelationship to a file specification written
// as a direct dictionary in an annotation's /FS.
//
// ISO 32000-1, 7.11.3 does not require a file specification to be an indirect
// object, and an inline one has no object number the writer can address. The
// repair goes through the annotation holding it, the nearest object that has
// one. An annotation that is itself direct is left alone: nothing on the path
// can be written, so nothing is reported either.
func fixDirectFileSpecInAnnot(alloc *writer.ObjAllocator, annotObj ast.PdfObject, annotDict, fsDict ast.PdfDict) int {
	if _, ok := fsDict["AFRelationship"]; ok {
		return 0
	}
	ref, isRef := annotObj.(ast.PdfIndirectRef)
	if !isRef || ref.ObjectNum <= 0 {
		return 0
	}

	newFS := make(ast.PdfDict, len(fsDict)+1)
	maps.Copy(newFS, fsDict)
	newFS["AFRelationship"] = ast.PdfName("Unspecified")

	newAnnot := make(ast.PdfDict, len(annotDict))
	maps.Copy(newAnnot, annotDict)
	newAnnot["FS"] = newFS

	alloc.AllocAt(ref.ObjectNum, newAnnot)
	return 1
}
