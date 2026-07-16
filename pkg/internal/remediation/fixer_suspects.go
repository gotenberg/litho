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

// matchSuspects matches the PDF/UA /MarkInfo /Suspects violation (ISO 14289-1
// §7.1 t04, shared by PDF/UA-1 and PDF/UA-2). It keys on the exact clause and
// test number, which clause 7.1 shares with other accessibility checks.
func matchSuspects(v compliance.Violation) bool {
	return v.Rule.Clause == "7.1" && v.Rule.TestNumber == 4
}

// fixSuspects removes a /MarkInfo /Suspects entry whose value is true. ISO
// 14289-1 §7.1 requires Suspects to be false (or absent); ISO 32000-1 Table 321
// makes false the default, so deleting a true entry reverts to the conforming
// default rather than asserting a value. Removing the flag does not mask
// untagged content: genuinely untagged painting operators are caught
// independently by the content-marking checks (§7.1 t01-t03) and the
// StructTreeRoot/MarkInfo structural rules, none of which this fixer suppresses.
// The fixer mirrors the validator
// [compliance] checkSuspects: it acts only when Suspects resolves to true, and
// leaves an absent or already-false entry untouched. A /MarkInfo given as an
// indirect object is rewritten in place; an inline /MarkInfo is rewritten within
// a copy of the catalog.
func fixSuspects(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil, fmt.Errorf("trailer missing /Root")
	}
	catalog := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalog == nil {
		return nil, fmt.Errorf("resolving catalog")
	}

	miObj, ok := catalog["MarkInfo"]
	if !ok {
		return nil, nil
	}

	var miRef ast.PdfIndirectRef
	var mi ast.PdfDict
	if ref, isRef := miObj.(ast.PdfIndirectRef); isRef {
		miRef = ref
		mi = resolveFromAllocOrDoc(ctx, alloc, doc, ref)
	} else if d, err := document.ResolveDict(ctx, doc, miObj); err == nil {
		mi = d
	}
	if mi == nil {
		return nil, nil
	}

	susObj, ok := mi["Suspects"]
	if !ok {
		return nil, nil
	}
	sus, err := document.ResolveBool(ctx, doc, susObj)
	if err != nil {
		// An unreadable /Suspects is left alone rather than guessed at.
		doc.Logger.Debug("remediation: suspects: cannot resolve /Suspects", "error", err)
		return nil, nil
	}
	if !sus {
		return nil, nil
	}

	newMI := make(ast.PdfDict, len(mi))
	maps.Copy(newMI, mi)
	delete(newMI, "Suspects")

	if miRef.ObjectNum > 0 {
		alloc.AllocAt(miRef.ObjectNum, newMI)
	} else {
		newCatalog := make(ast.PdfDict, len(catalog))
		maps.Copy(newCatalog, catalog)
		newCatalog["MarkInfo"] = newMI
		alloc.AllocAt(catalogRef.ObjectNum, newCatalog)
	}

	return []string{"removed /Suspects true from /MarkInfo (reverts to the default false)"}, nil
}
