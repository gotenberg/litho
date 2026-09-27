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
// be non-empty), and it does not touch the embedded bytes. It repairs every
// indirect file spec object, the ones stored in an object stream (ISO 32000-1,
// 7.5.7) included, for the reason [walkNumberedObjects] gives, and every one
// written inline, which ISO 32000-1, 7.11.3 permits, through the object holding
// it ([rewriteInlineFileSpecs]), the nearest one the writer can address.
//
// # Corpus evidence
//
//   - pdfjs issue14203.pdf: a FileAttachment's inline /FS carries /F and no
//     /UF.
//   - litho inlinefs_4.pdf: a Link's inline /AF entry carries /F and no /UF,
//     and kept its ISO 19005-4, 6.9 t2 finding after a PDF/A-4 remediation.
func fixFileSpecFUF(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	var count int
	for objNum, entry := range sweptObjects(doc) {
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

	if _, err := rewriteInlineFileSpecs(ctx, alloc, doc, func(_ int, _ string, spec ast.PdfDict) (ast.PdfObject, bool) {
		patched := fileSpecFUFPatch(ctx, doc, spec)
		if patched == nil {
			return nil, false
		}
		count++
		return patched, true
	}); err != nil {
		return nil, err
	}

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
