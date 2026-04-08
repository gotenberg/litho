package remediation

import (
	"context"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

func matchJavaScript(v compliance.Violation) bool {
	return descriptionContains(v, "JavaScript")
}

func fixJavaScript(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalogRef, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}

	var fixes []string

	// Remove /AA (additional actions) from catalog if it contains JavaScript.
	if _, ok := catalogDict["AA"]; ok {
		newCatalog := make(ast.PdfDict, len(catalogDict))
		maps.Copy(newCatalog, catalogDict)
		delete(newCatalog, "AA")
		catalogDict = newCatalog
		fixes = append(fixes, "removed /AA from catalog")
	}

	// Remove /OpenAction if it's JavaScript.
	if oaObj, ok := catalogDict["OpenAction"]; ok {
		if isJavaScriptAction(ctx, doc, oaObj) {
			newCatalog := make(ast.PdfDict, len(catalogDict))
			maps.Copy(newCatalog, catalogDict)
			delete(newCatalog, "OpenAction")
			catalogDict = newCatalog
			fixes = append(fixes, "removed JavaScript /OpenAction from catalog")
		}
	}

	// Remove /Names/JavaScript from catalog.
	if namesObj, ok := catalogDict["Names"]; ok {
		if namesDict, err := document.ResolveDict(ctx, doc, namesObj); err == nil && namesDict != nil {
			if _, ok := namesDict["JavaScript"]; ok {
				newNames := make(ast.PdfDict, len(namesDict))
				maps.Copy(newNames, namesDict)
				delete(newNames, "JavaScript")
				namesRef := alloc.Alloc(newNames)

				newCatalog := make(ast.PdfDict, len(catalogDict))
				maps.Copy(newCatalog, catalogDict)
				catalogDict["Names"] = namesRef
				catalogDict = newCatalog
				fixes = append(fixes, "removed /Names/JavaScript from catalog")
			}
		}
	}

	if len(fixes) > 0 {
		alloc.AllocAt(catalogRef.ObjectNum, catalogDict)
	}

	return fixes, nil
}

func isJavaScriptAction(ctx context.Context, doc *document.Document, obj ast.PdfObject) bool {
	d, err := document.ResolveDict(ctx, doc, obj)
	if err != nil || d == nil {
		return false
	}
	if s, ok := d["S"].(ast.PdfName); ok {
		return string(s) == "JavaScript"
	}
	return false
}
