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

func matchStructTree(v compliance.Violation) bool {
	return descriptionContains(v, "StructTreeRoot")
}

func fixStructTree(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalogRef, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
	if !ok {
		return nil, fmt.Errorf("trailer missing /Root")
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, fmt.Errorf("resolving catalog")
	}

	// Don't overwrite an existing StructTreeRoot.
	if _, ok := catalogDict["StructTreeRoot"]; ok {
		return nil, nil
	}

	// Minimal structure tree with a Document root element.
	docElem := ast.PdfDict{
		"Type": ast.PdfName("StructElem"),
		"S":    ast.PdfName("Document"),
		"K":    ast.PdfArray{},
	}
	docElemRef := alloc.Alloc(docElem)

	structTreeRoot := ast.PdfDict{
		"Type":       ast.PdfName("StructTreeRoot"),
		"K":          docElemRef,
		"ParentTree": ast.PdfDict{"Nums": ast.PdfArray{}},
	}
	structTreeRef := alloc.Alloc(structTreeRoot)

	// Set /Parent on the document element back to the tree root. Per
	// ISO 32000-1 §14.7.2 Table 323, a structure element's parent pointer
	// is the /Parent key, not /ParentTreeRoot (which is a different entry
	// on StructTreeRoot).
	docElem["Parent"] = structTreeRef
	alloc.AllocAt(docElemRef.ObjectNum, docElem)

	newCatalog := make(ast.PdfDict, len(catalogDict)+1)
	maps.Copy(newCatalog, catalogDict)
	newCatalog["StructTreeRoot"] = structTreeRef
	alloc.AllocAt(catalogRef.ObjectNum, newCatalog)

	return []string{"added minimal /StructTreeRoot to catalog"}, nil
}
