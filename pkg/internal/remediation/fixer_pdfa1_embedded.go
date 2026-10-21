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
// cannot be the whole repair. A specification stored in an object stream (ISO
// 32000-1, 7.5.7) is repaired too, for the reason [walkNumberedObjects] gives.
//
// # Corpus evidence
//
//   - All corpora: 18 of the cells report t1 with no /Names/EmbeddedFiles tree
//     at all.
//   - pdfjs bug1997343.pdf: its aggressive PDF/A-1b remediation stripped /EF
//     from 2 of its 19 specifications, and from the other 17, stored in object
//     streams, only in the round after the full rewrite unpacked them.
func stripEFFromFileSpecs(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) int {
	count := 0
	for objNum, entry := range sweptObjects(doc) {
		if ctx.Err() != nil {
			return count
		}
		if entry.Free || objNum == 0 {
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

	inline, err := stripEFFromInlineFileSpecs(ctx, alloc, doc)
	if err != nil {
		doc.Logger.Debug("remediation: stripping /EF from inline file specifications stopped", "error", err)
	}
	return count + inline
}

// stripEFFromInlineFileSpecs removes /EF from every file specification written
// inline, which ISO 32000-1, 7.11.3 permits, and returns how many it cleared. A
// direct dictionary has no object number of its own, so the object holding it
// is what gets rewritten ([rewriteInlineFileSpecs]). It reaches every one
// §6.1.11 t1 judges: an annotation's /FS, a Screen annotation's media clip at
// /A/R/C/D, an /AF entry, an inline annotation's, in any object.
//
// An inline dictionary without /Type is no file specification to the
// validator, and keeps its /EF: the repair destroys an attachment only where
// the rule forbids it. veraPDF reads it the same way, measured on a
// FileAttachment /FS with /EF and no /Type, which fails neither ISO 19005-1,
// 6.1.11 t1 nor any ISO 19005-3, 6.8 test, where the same /FS typed /Filespec
// fails them.
//
// # Corpus evidence
//
//   - litho inlinefs_4.pdf: two inline /AF entries embed a file, and both
//     kept ISO 19005-1, 6.1.11 t1 on veraPDF after an aggressive PDF/A-1b
//     remediation, where litho saw one.
func stripEFFromInlineFileSpecs(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) (int, error) {
	count := 0
	err := rewriteInlineFileSpecs(ctx, alloc, doc, func(_ int, _ string, spec ast.PdfDict) (ast.PdfObject, bool) {
		if _, hasEF := spec["EF"]; !hasEF {
			return nil, false
		}
		stripped := maps.Clone(spec)
		delete(stripped, "EF")
		count++
		return stripped, true
	})
	return count, err
}
