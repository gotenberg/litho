package remediation

import (
	"context"
	"crypto/rand"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

func matchTrailerID(v compliance.Violation) bool {
	// Clause 6.1.3 covers: t01 missing /ID, t04/t05 PDF/A-4 /Info
	// restrictions, t01 (part 4) empty /ID element. Exclude t02 (Encrypt)
	// which is handled by catalog-cleanup.
	return v.Rule.Clause == "6.1.3" && v.Rule.TestNumber != 2
}

func fixTrailerID(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, opts Options, _ []compliance.Violation, profile string) ([]string, error) {
	var fixes []string

	// /ID missing or malformed (empty element).
	needReplace := false
	if id, ok := doc.Index.Trailer["ID"]; ok {
		if arr, ok := id.(ast.PdfArray); ok {
			for _, e := range arr {
				if s, ok := e.(ast.PdfString); ok && len(s) == 0 {
					needReplace = true
					break
				}
			}
			if len(arr) != 2 {
				needReplace = true
			}
		} else {
			needReplace = true
		}
	} else {
		needReplace = true
	}
	if needReplace {
		id := opts.PinnedID
		if len(id) != 16 {
			id = make([]byte, 16)
			// crypto/rand.Read never fails on supported platforms; the Go
			// runtime aborts on getrandom/EAGAIN.
			_, _ = rand.Read(id)
		}
		doc.Index.Trailer["ID"] = ast.PdfArray{
			ast.PdfString(string(id)),
			ast.PdfString(string(id)),
		}
		fixes = append(fixes, "added /ID to trailer")
	}

	// PDF/A-4 §6.1.3 tests 4-5 constrain /Info to only /ModDate when
	// no /PieceInfo is defined on the catalog.
	if strings.HasPrefix(profile, "PDF/A-4") {
		if infoFixes := enforceInfoDictLimits(ctx, alloc, doc); len(infoFixes) > 0 {
			fixes = append(fixes, infoFixes...)
		}
	}

	if len(fixes) == 0 {
		return nil, nil
	}
	return fixes, nil
}

// enforceInfoDictLimits applies ISO 19005-4 §6.1.3 tests 4-5: /Info is
// optional for PDF/A-4 and, when present, must only carry /ModDate
// unless the catalog defines /PieceInfo. The trailer reference is
// removed outright when the catalog lacks /PieceInfo; otherwise the
// /Info dict is rewritten through the allocator so the stripped entries
// reach the output.
func enforceInfoDictLimits(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document) []string {
	infoObj, ok := doc.Index.Trailer["Info"]
	if !ok {
		return nil
	}
	catalogRef, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
	if !ok {
		return nil
	}
	catalogDict, err := document.ResolveDict(ctx, doc, catalogRef)
	if err != nil || catalogDict == nil {
		return nil
	}
	_, hasPieceInfo := catalogDict["PieceInfo"]

	if !hasPieceInfo {
		delete(doc.Index.Trailer, "Info")
		return []string{"removed /Info from trailer (PDF/A-4 §6.1.3)"}
	}

	infoDict, err := document.ResolveDict(ctx, doc, infoObj)
	if err != nil || infoDict == nil {
		return nil
	}
	changed := false
	newInfo := make(ast.PdfDict, len(infoDict))
	for k, v := range infoDict {
		if k == "ModDate" {
			newInfo[k] = v
			continue
		}
		changed = true
	}
	if !changed {
		return nil
	}
	if ref, isRef := infoObj.(ast.PdfIndirectRef); isRef && ref.ObjectNum > 0 {
		alloc.AllocAt(ref.ObjectNum, newInfo)
	} else {
		doc.Index.Trailer["Info"] = newInfo
	}
	return []string{"trimmed /Info to /ModDate only (PDF/A-4 §6.1.3)"}
}
