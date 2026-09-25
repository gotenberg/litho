package remediation

import (
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// removePDFA1EmbeddedFiles deletes the embedded-file constructs ISO 19005-1
// forbids outright: §6.1.11 t2, the EmbeddedFiles entry in the document's name
// dictionary, and §6.1.11 t1, the EF key on a file specification dictionary.
//
// PDF/A-1 permits no embedded file at all, so the only conforming repair is to
// remove them, and that destroys content the author deliberately attached. The
// repair is therefore gated on aggressive mode, like every other fixer that
// cannot preserve what it touches.
//
// The part check is load-bearing rather than defensive: parts 2 and later
// permit embedded files, and PDF/A-3 is the basis of Factur-X, whose invoice
// payload IS an embedded file. Removing it would destroy the document's reason
// for existing.
func removePDFA1EmbeddedFiles(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, opts Options, profile string, catalogDict ast.PdfDict, catalogRef ast.PdfIndirectRef) []string {
	if opts.PreserveContent || compliance.PDFAPart(profile) != 1 {
		return nil
	}

	var fixes []string
	if fix := deleteEmbeddedFilesNameEntry(ctx, alloc, doc, catalogDict, catalogRef); fix != "" {
		fixes = append(fixes, fix)
	}
	if n := stripEFFromFileSpecs(ctx, alloc, doc); n > 0 {
		fixes = append(fixes, fmt.Sprintf("removed /EF from %d file specification(s)", n))
	}
	return fixes
}

// deleteEmbeddedFilesNameEntry removes /EmbeddedFiles from the catalog's /Names
// dictionary (ISO 19005-1, 6.1.11 t2). The name dictionary is reached either
// through an indirect reference or inline in the catalog, and only the object
// that actually holds it can be rewritten, so both cases are handled the way
// [fixAlternatePresentations] handles them for /AlternatePresentations.
func deleteEmbeddedFilesNameEntry(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalogDict ast.PdfDict, catalogRef ast.PdfIndirectRef) string {
	namesObj, ok := catalogDict["Names"]
	if !ok {
		return ""
	}

	var namesDict ast.PdfDict
	var namesRef ast.PdfIndirectRef
	namesIsRef := false

	if ref, isRef := namesObj.(ast.PdfIndirectRef); isRef {
		namesDict = resolveFromAllocOrDoc(ctx, alloc, doc, ref)
		namesRef = ref
		namesIsRef = true
	}
	if namesDict == nil {
		resolved, err := document.ResolveDict(ctx, doc, namesObj)
		if err != nil || resolved == nil {
			return ""
		}
		namesDict = resolved
		namesIsRef = false
	}

	if _, has := namesDict["EmbeddedFiles"]; !has {
		return ""
	}

	newNames := make(ast.PdfDict, len(namesDict))
	maps.Copy(newNames, namesDict)
	delete(newNames, "EmbeddedFiles")

	if namesIsRef && namesRef.ObjectNum > 0 {
		alloc.AllocAt(namesRef.ObjectNum, newNames)
	} else {
		if catalogRef.ObjectNum <= 0 {
			return ""
		}
		newCatalog := make(ast.PdfDict, len(catalogDict))
		maps.Copy(newCatalog, catalogDict)
		newCatalog["Names"] = newNames
		alloc.AllocAt(catalogRef.ObjectNum, newCatalog)
	}

	return "removed /EmbeddedFiles from catalog /Names"
}

// stripEFFromFileSpecs removes the /EF key from every file specification in the
// document (ISO 19005-1, 6.1.11 t1), leaving the specification itself intact:
// the rule forbids the key, not the reference to an external file.
//
// The scan is object-level rather than a walk down from the catalog because the
// rule is keyed on the COS object (CosFileSpecification), and because a
// specification carrying /EF is reachable through several containers. A file
// can report t1 with no /Names/EmbeddedFiles tree at all, so deleting that tree
// cannot be the whole repair.
//
// # Corpus evidence
//
//   - All corpora: 18 of the cells report t1 with no /Names/EmbeddedFiles tree
//     at all.
func stripEFFromFileSpecs(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) int {
	count := 0
	for objNum, entry := range doc.XRefEntries() {
		if ctx.Err() != nil {
			return count
		}
		if entry.Type != 1 || objNum == 0 {
			continue
		}
		ref := ast.PdfIndirectRef{ObjectNum: objNum, GenNum: entry.Generation}
		dict := resolveFromAllocOrDoc(ctx, alloc, doc, ref)
		if dict == nil {
			continue
		}
		if typeName, _ := document.ResolveName(ctx, doc, dict["Type"]); typeName != "Filespec" {
			continue
		}
		if _, hasEF := dict["EF"]; !hasEF {
			continue
		}

		newFS := make(ast.PdfDict, len(dict))
		maps.Copy(newFS, dict)
		delete(newFS, "EF")
		alloc.AllocAt(objNum, newFS)
		count++
	}

	return count + stripEFFromDirectFileSpecs(ctx, alloc, doc)
}

// stripEFFromDirectFileSpecs handles a specification written inline as the /FS
// value of an annotation. A direct dictionary has no object number of its own,
// so the enclosing annotation is what gets rewritten, mirroring
// [fixDirectFileSpecFUF].
func stripEFFromDirectFileSpecs(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) int {
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return 0
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return 0
	}
	pagesObj, ok := catalogDict["Pages"]
	if !ok {
		return 0
	}

	var pages []annotPageEntry
	collectPageRefs(ctx, alloc, doc, pagesObj, &pages, make(map[int]bool), 0)

	count := 0
	seen := make(map[int]bool)
	for _, pe := range pages {
		if ctx.Err() != nil {
			return count
		}
		annots, err := document.ResolveArray(ctx, doc, pe.dict["Annots"])
		if err != nil {
			continue
		}
		for _, annotObj := range annots {
			ref, isRef := annotObj.(ast.PdfIndirectRef)
			if !isRef || ref.ObjectNum <= 0 || seen[ref.ObjectNum] {
				continue
			}
			seen[ref.ObjectNum] = true

			annotDict := resolveFromAllocOrDoc(ctx, alloc, doc, ref)
			if annotDict == nil {
				continue
			}
			fsDict, direct := annotDict["FS"].(ast.PdfDict)
			if !direct {
				continue
			}
			if _, hasEF := fsDict["EF"]; !hasEF {
				continue
			}

			newFS := make(ast.PdfDict, len(fsDict))
			maps.Copy(newFS, fsDict)
			delete(newFS, "EF")

			newAnnot := make(ast.PdfDict, len(annotDict))
			maps.Copy(newAnnot, annotDict)
			newAnnot["FS"] = newFS
			alloc.AllocAt(ref.ObjectNum, newAnnot)
			count++
		}
	}
	return count
}
