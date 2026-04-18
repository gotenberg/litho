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
	return descriptionContains(v, "StructTreeRoot") ||
		descriptionContains(v, "Namespaces missing") ||
		descriptionContains(v, "not mapped to a standard namespace")
}

func fixStructTree(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, profile string) ([]string, error) {
	catalogRef, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
	if !ok {
		return nil, fmt.Errorf("trailer missing /Root")
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, fmt.Errorf("resolving catalog")
	}

	var fixes []string

	needsNamespaces := profile == "PDF/UA-2"

	// Check for existing StructTreeRoot.
	if stObj, ok := catalogDict["StructTreeRoot"]; ok {
		if !needsNamespaces {
			return nil, nil
		}
		// Patch existing tree: add /Namespaces if missing.
		var stRef ast.PdfIndirectRef
		if ref, ok := stObj.(ast.PdfIndirectRef); ok {
			stRef = ref
		}
		stDict := resolveFromAllocOrDoc(ctx, alloc, doc, stRef)
		if stDict == nil {
			if d, err := document.ResolveDict(ctx, doc, stObj); err == nil {
				stDict = d
			}
		}
		if stDict != nil {
			if _, hasNS := stDict["Namespaces"]; !hasNS {
				nsArray := buildStandardNamespaces(alloc)
				newST := make(ast.PdfDict, len(stDict)+1)
				maps.Copy(newST, stDict)
				newST["Namespaces"] = nsArray

				// Ensure /Document element has /NS pointing to PDF 1.7.
				ensureDocumentElementNS(ctx, alloc, doc, newST, nsArray)

				if stRef.ObjectNum > 0 {
					alloc.AllocAt(stRef.ObjectNum, newST)
				}
				fixes = append(fixes, "added /Namespaces to existing StructTreeRoot")
			}
		}
		return fixes, nil
	}

	// Create minimal structure tree with a Document root element.
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

	if needsNamespaces {
		nsArray := buildStandardNamespaces(alloc)
		structTreeRoot["Namespaces"] = nsArray
		if arr, ok := nsArray.(ast.PdfArray); ok && len(arr) > 0 {
			docElem["NS"] = arr[0] // PDF 1.7 namespace
		}
	}

	structTreeRef := alloc.Alloc(structTreeRoot)
	docElem["Parent"] = structTreeRef
	alloc.AllocAt(docElemRef.ObjectNum, docElem)

	newCatalog := make(ast.PdfDict, len(catalogDict)+1)
	maps.Copy(newCatalog, catalogDict)
	newCatalog["StructTreeRoot"] = structTreeRef
	alloc.AllocAt(catalogRef.ObjectNum, newCatalog)

	fixes = append(fixes, "added minimal /StructTreeRoot to catalog")
	return fixes, nil
}

// buildStandardNamespaces creates the PDF 1.7 and PDF 2.0 namespace dicts
// as required by ISO 32000-2:2020 §14.8.6 for PDF/UA-2.
func buildStandardNamespaces(alloc *writer.ObjAllocator) ast.PdfObject {
	ns17 := ast.PdfDict{
		"Type": ast.PdfName("Namespace"),
		"NS":   ast.PdfString("http://iso.org/pdf/ssn"),
	}
	ns17Ref := alloc.Alloc(ns17)

	ns20 := ast.PdfDict{
		"Type": ast.PdfName("Namespace"),
		"NS":   ast.PdfString("http://iso.org/pdf2/ssn"),
	}
	ns20Ref := alloc.Alloc(ns20)

	return ast.PdfArray{ns17Ref, ns20Ref}
}

// ensureDocumentElementNS sets /NS on the root /Document structure element
// if it doesn't already have one.
func ensureDocumentElementNS(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, stDict ast.PdfDict, nsArray ast.PdfObject) {
	kObj, ok := stDict["K"]
	if !ok {
		return
	}

	var docRef ast.PdfIndirectRef
	if ref, ok := kObj.(ast.PdfIndirectRef); ok {
		docRef = ref
	}

	docElem := resolveFromAllocOrDoc(ctx, alloc, doc, docRef)
	if docElem == nil {
		if d, err := document.ResolveDict(ctx, doc, kObj); err == nil {
			docElem = d
		}
	}
	if docElem == nil {
		return
	}

	if _, hasNS := docElem["NS"]; hasNS {
		return
	}

	newDoc := make(ast.PdfDict, len(docElem)+1)
	maps.Copy(newDoc, docElem)
	// Point to the first namespace (PDF 1.7).
	if arr, ok := nsArray.(ast.PdfArray); ok && len(arr) > 0 {
		newDoc["NS"] = arr[0]
	}
	if docRef.ObjectNum > 0 {
		alloc.AllocAt(docRef.ObjectNum, newDoc)
	}
}
