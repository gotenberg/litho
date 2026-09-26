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

// matchFileSpecFUF matches the embedded-file specification /F + /UF requirement
// (ISO 19005-2/3 §6.8 t02, ISO 19005-4 §6.9 t02, ISO 14289-1 §7.11 t01). All
// four rules share the phrase "F and UF" in their description, so keying on it
// covers every profile without enumerating clauses.
func matchFileSpecFUF(v compliance.Violation) bool {
	return descriptionContains(v, "F and UF")
}

// fixFileSpecFUF adds a missing /F or /UF key to an embedded-file specification
// dictionary. A file spec for an embedded file (one carrying /EF) must declare
// both /F (the file name) and /UF (its Unicode form); the two hold the same
// name, so when exactly one is present the other is filled from it. The repair
// is render-neutral metadata: it never invents a name (both-missing is left
// alone, as is an empty source value, since the rules also require the keys to
// be non-empty), and it does not touch the embedded bytes. Only indirect file
// spec objects are rewritten, the ones stored in an object stream (ISO
// 32000-1, 7.5.7) included, for the reason [walkNumberedObjects] gives.
func fixFileSpecFUF(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	var count int
	for objNum, entry := range doc.XRefEntries() {
		if err := ctx.Err(); err != nil {
			return nil, err
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

		newFS := fileSpecFUFPatch(ctx, doc, dict)
		if newFS == nil {
			continue
		}
		alloc.AllocAt(objNum, newFS)
		count++
	}

	count += fixDirectFileSpecFUF(ctx, alloc, doc)

	if count == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("filled missing /F or /UF on %d embedded file spec(s)", count)}, nil
}

// fileSpecFUFPatch returns a copy of an embedded-file specification with the
// missing /F or /UF filled in from the one that is present, or nil when there is
// nothing to repair. The two keys hold the same name, so neither is invented:
// both-missing and an empty source value are left alone, since the rules also
// require the keys to be non-empty.
func fileSpecFUFPatch(ctx context.Context, doc *document.Document, dict ast.PdfDict) ast.PdfDict {
	if _, hasEF := dict["EF"]; !hasEF {
		return nil
	}

	fObj, hasF := dict["F"]
	ufObj, hasUF := dict["UF"]
	if hasF == hasUF {
		// Both present (conformant) or both absent (no name to derive).
		return nil
	}

	srcObj, dstKey := fObj, ast.PdfName("UF")
	if !hasF {
		srcObj, dstKey = ufObj, "F"
	}
	if s, _ := document.ResolveString(ctx, doc, srcObj); s == "" {
		// An empty source name cannot satisfy the non-empty requirement.
		return nil
	}

	newFS := make(ast.PdfDict, len(dict)+1)
	maps.Copy(newFS, dict)
	newFS[dstKey] = srcObj
	return newFS
}

// fixDirectFileSpecFUF repairs an embedded-file specification written as a
// direct dictionary in an annotation's /FS.
//
// ISO 32000-1, 7.11.3 does not require a file specification to be an indirect
// object, so the cross-reference sweep above cannot see one written inline. The
// repair goes through the annotation holding it, the nearest object the writer
// can address. The annotation is read through the allocator so a pending
// rewrite from another fixer (the /AFRelationship repair visits the same
// annotations) is preserved instead of being overwritten with the stale
// on-disk dictionary.
func fixDirectFileSpecFUF(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) int {
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
			if typeName, _ := document.ResolveName(ctx, doc, fsDict["Type"]); typeName != "Filespec" {
				continue
			}

			newFS := fileSpecFUFPatch(ctx, doc, fsDict)
			if newFS == nil {
				continue
			}

			newAnnot := make(ast.PdfDict, len(annotDict))
			maps.Copy(newAnnot, annotDict)
			newAnnot["FS"] = newFS
			alloc.AllocAt(ref.ObjectNum, newAnnot)
			count++
		}
	}
	return count
}
