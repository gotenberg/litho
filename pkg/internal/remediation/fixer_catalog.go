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

func matchCatalogCleanup(v compliance.Violation) bool {
	return (v.Rule.Clause == "6.1.3" && v.Rule.TestNumber == 2) || // Encrypt in trailer
		clausePrefix(v, "6.10") || // OCG config
		clausePrefix(v, "6.11") || // AlternatePresentations
		clausePrefix(v, "6.1.11") // Perms dict
}

func fixCatalogCleanup(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalogRef, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}

	var fixes []string

	// 6.1.3: Remove /Encrypt from trailer to strip encryption on rewrite.
	if _, ok := doc.Index.Trailer["Encrypt"]; ok {
		newTrailer := make(ast.PdfDict, len(doc.Index.Trailer))
		maps.Copy(newTrailer, doc.Index.Trailer)
		delete(newTrailer, "Encrypt")
		doc.Index.Trailer = newTrailer
		fixes = append(fixes, "removed /Encrypt from trailer")
	}

	// 6.10: Ensure OCG config dicts have /Name.
	if ocpObj, ok := catalogDict["OCProperties"]; ok {
		ocFixes := fixOCGConfigs(ctx, alloc, doc, catalogDict, catalogRef, ocpObj)
		fixes = append(fixes, ocFixes...)
		// Re-read catalog in case it was updated.
		catalogDict = resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
		if catalogDict == nil {
			return fixes, nil
		}
	}

	// 6.11: Remove /AlternatePresentations from catalog /Names.
	if namesObj, ok := catalogDict["Names"]; ok {
		apFixes := fixAlternatePresentations(ctx, alloc, doc, catalogDict, catalogRef, namesObj)
		fixes = append(fixes, apFixes...)
		// Re-read catalog in case it was updated.
		catalogDict = resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
		if catalogDict == nil {
			return fixes, nil
		}
	}

	// 6.1.11: Remove non-permitted keys from /Perms dict (keep only /UR3 and /DocMDP).
	if permsObj, ok := catalogDict["Perms"]; ok {
		pFixes := fixPermsDict(ctx, alloc, doc, catalogDict, catalogRef, permsObj)
		fixes = append(fixes, pFixes...)
	}

	return fixes, nil
}

// fixOCGConfigs walks /OCProperties /D and /Configs, adding /Name to any
// config dict that lacks it. Per ISO 32000-2, Table 100.
func fixOCGConfigs(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalogDict ast.PdfDict, catalogRef ast.PdfIndirectRef, ocpObj ast.PdfObject) []string {
	var ocpDict ast.PdfDict
	var ocpRef ast.PdfIndirectRef
	var ocpIsRef bool

	if ref, ok := ocpObj.(ast.PdfIndirectRef); ok {
		ocpDict = resolveFromAllocOrDoc(ctx, alloc, doc, ref)
		ocpRef = ref
		ocpIsRef = true
	}
	if ocpDict == nil {
		resolved, err := document.ResolveDict(ctx, doc, ocpObj)
		if err != nil || resolved == nil {
			return nil
		}
		ocpDict = resolved
	}

	var fixes []string
	modified := false
	newOCP := make(ast.PdfDict, len(ocpDict))
	maps.Copy(newOCP, ocpDict)

	// Fix /D (default config).
	if dObj, ok := newOCP["D"]; ok {
		fixed := ensureOCGConfigName(ctx, alloc, doc, dObj, "D")
		if fixed {
			fixes = append(fixes, "added /Name to OCG default config /D")
			modified = true
		}
	}

	// Fix /Configs array entries.
	if configsObj, ok := newOCP["Configs"]; ok {
		arr, err := document.ResolveArray(ctx, doc, configsObj)
		if err != nil || arr == nil {
			return fixes
		}
		for i, entry := range arr {
			fixed := ensureOCGConfigName(ctx, alloc, doc, entry, fmt.Sprintf("Configs[%d]", i))
			if fixed {
				fixes = append(fixes, fmt.Sprintf("added /Name to OCG config Configs[%d]", i))
				modified = true
			}
		}
	}

	if modified {
		if ocpIsRef {
			alloc.AllocAt(ocpRef.ObjectNum, newOCP)
		} else {
			// OCProperties is inline in catalog. Update catalog to point at the new dict.
			newCatalog := make(ast.PdfDict, len(catalogDict))
			maps.Copy(newCatalog, catalogDict)
			newCatalog["OCProperties"] = newOCP
			alloc.AllocAt(catalogRef.ObjectNum, newCatalog)
		}
	}

	return fixes
}

// ensureOCGConfigName adds /Name to an OCG config dict if missing.
// Returns true if a modification was made.
func ensureOCGConfigName(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, obj ast.PdfObject, label string) bool {
	ref, isRef := obj.(ast.PdfIndirectRef)

	var d ast.PdfDict
	if isRef {
		d = resolveFromAllocOrDoc(ctx, alloc, doc, ref)
	}
	if d == nil {
		resolved, err := document.ResolveDict(ctx, doc, obj)
		if err != nil || resolved == nil {
			return false
		}
		d = resolved
	}

	if _, ok := d["Name"]; ok {
		return false
	}

	newD := make(ast.PdfDict, len(d)+1)
	maps.Copy(newD, d)
	newD["Name"] = ast.PdfString(label)

	if isRef {
		alloc.AllocAt(ref.ObjectNum, newD)
	}

	return true
}

// fixAlternatePresentations removes /AlternatePresentations from catalog /Names.
func fixAlternatePresentations(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalogDict ast.PdfDict, catalogRef ast.PdfIndirectRef, namesObj ast.PdfObject) []string {
	var namesDict ast.PdfDict
	var namesRef ast.PdfIndirectRef
	namesIsRef := false

	if ref, ok := namesObj.(ast.PdfIndirectRef); ok {
		namesDict = resolveFromAllocOrDoc(ctx, alloc, doc, ref)
		namesRef = ref
		namesIsRef = true
	}
	if namesDict == nil {
		resolved, err := document.ResolveDict(ctx, doc, namesObj)
		if err != nil || resolved == nil {
			return nil
		}
		namesDict = resolved
	}

	if _, ok := namesDict["AlternatePresentations"]; !ok {
		return nil
	}

	newNames := make(ast.PdfDict, len(namesDict))
	maps.Copy(newNames, namesDict)
	delete(newNames, "AlternatePresentations")

	if namesIsRef {
		alloc.AllocAt(namesRef.ObjectNum, newNames)
	} else {
		newCatalog := make(ast.PdfDict, len(catalogDict))
		maps.Copy(newCatalog, catalogDict)
		newCatalog["Names"] = newNames
		alloc.AllocAt(catalogRef.ObjectNum, newCatalog)
	}

	return []string{"removed /AlternatePresentations from catalog /Names"}
}

// fixPermsDict removes all keys except /UR3 and /DocMDP from the /Perms dict.
// Per ISO 32000-2, 12.8.4, PDF/A restricts the permitted entries.
func fixPermsDict(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, catalogDict ast.PdfDict, catalogRef ast.PdfIndirectRef, permsObj ast.PdfObject) []string {
	var permsDict ast.PdfDict
	var permsRef ast.PdfIndirectRef
	permsIsRef := false

	if ref, ok := permsObj.(ast.PdfIndirectRef); ok {
		permsDict = resolveFromAllocOrDoc(ctx, alloc, doc, ref)
		permsRef = ref
		permsIsRef = true
	}
	if permsDict == nil {
		resolved, err := document.ResolveDict(ctx, doc, permsObj)
		if err != nil || resolved == nil {
			return nil
		}
		permsDict = resolved
	}

	// Collect keys to remove.
	var removed []string
	for k := range permsDict {
		if k == "UR3" || k == "DocMDP" {
			continue
		}
		removed = append(removed, string(k))
	}

	if len(removed) == 0 {
		return nil
	}

	newPerms := make(ast.PdfDict, 2)
	if v, ok := permsDict["UR3"]; ok {
		newPerms["UR3"] = v
	}
	if v, ok := permsDict["DocMDP"]; ok {
		newPerms["DocMDP"] = v
	}

	if permsIsRef {
		alloc.AllocAt(permsRef.ObjectNum, newPerms)
	} else {
		newCatalog := make(ast.PdfDict, len(catalogDict))
		maps.Copy(newCatalog, catalogDict)
		newCatalog["Perms"] = newPerms
		alloc.AllocAt(catalogRef.ObjectNum, newCatalog)
	}

	return []string{"removed non-permitted keys from /Perms dict"}
}
