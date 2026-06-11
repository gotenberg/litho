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
		descriptionContains(v, "not mapped to a standard namespace") ||
		// ISO 14289-2 §8.2.5.2 t2: Document element not in the PDF 2.0
		// namespace.
		(v.Rule.Specification == "ISO_14289_2" && v.Rule.Clause == "8.2.5.2" && v.Rule.TestNumber == 2)
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
				nsArray, ns20Ref := buildStandardNamespaces(alloc)
				newST := make(ast.PdfDict, len(stDict)+1)
				maps.Copy(newST, stDict)
				newST["Namespaces"] = nsArray

				ensureDocumentElementNS(ctx, alloc, doc, newST, ns20Ref)

				if stRef.ObjectNum > 0 {
					alloc.AllocAt(stRef.ObjectNum, newST)
				}
				fixes = append(fixes, "added /Namespaces to existing StructTreeRoot")
			} else if docRef, docElem, needs := documentElementNeedingNS(ctx, alloc, doc, stDict); needs && docRef.ObjectNum > 0 {
				// /Namespaces exists but the Document element still names
				// the PDF 1.7 namespace (or none): repoint it at PDF 2.0,
				// reusing a listed 2.0 namespace when present.
				ns20Ref, stWithNS := ensurePDF2Namespace(ctx, alloc, doc, stDict)
				if stWithNS != nil && stRef.ObjectNum > 0 {
					alloc.AllocAt(stRef.ObjectNum, stWithNS)
				}
				newDoc := make(ast.PdfDict, len(docElem)+1)
				maps.Copy(newDoc, docElem)
				newDoc["NS"] = ns20Ref
				alloc.AllocAt(docRef.ObjectNum, newDoc)
				fixes = append(fixes, "pointed Document element /NS at the PDF 2.0 namespace")
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
		nsArray, ns20Ref := buildStandardNamespaces(alloc)
		structTreeRoot["Namespaces"] = nsArray
		// ISO 14289-2 §8.2.5.2 t2: the root Document element shall be in
		// the PDF 2.0 namespace, not PDF 1.7.
		docElem["NS"] = ns20Ref
	}

	structTreeRef := alloc.Alloc(structTreeRoot)
	// Per ISO 32000-2, Table 355: the parent entry of a structure element
	// is /P. A /Parent key is not a structure-element entry, so conforming
	// readers treat the element as parentless.
	docElem["P"] = structTreeRef
	alloc.AllocAt(docElemRef.ObjectNum, docElem)

	newCatalog := make(ast.PdfDict, len(catalogDict)+1)
	maps.Copy(newCatalog, catalogDict)
	newCatalog["StructTreeRoot"] = structTreeRef
	alloc.AllocAt(catalogRef.ObjectNum, newCatalog)

	fixes = append(fixes, "added minimal /StructTreeRoot to catalog")
	return fixes, nil
}

// pdf1NamespaceURI is the PDF 1.7 standard structure namespace
// (ISO 32000-2 §14.8.6).
const pdf1NamespaceURI = "http://iso.org/pdf/ssn"

// buildStandardNamespaces creates the PDF 1.7 and PDF 2.0 namespace dicts
// required by ISO 32000-2:2020 §14.8.6 for PDF/UA-2. It returns the
// /Namespaces array and the PDF 2.0 namespace reference, which ISO 14289-2
// §8.2.5.2 t2 requires as the /NS of the root Document element.
func buildStandardNamespaces(alloc *writer.ObjAllocator) (ast.PdfArray, ast.PdfIndirectRef) {
	ns17 := ast.PdfDict{
		"Type": ast.PdfName("Namespace"),
		"NS":   ast.PdfString(pdf1NamespaceURI),
	}
	ns17Ref := alloc.Alloc(ns17)

	ns20 := ast.PdfDict{
		"Type": ast.PdfName("Namespace"),
		"NS":   ast.PdfString(pdf2NamespaceURI),
	}
	ns20Ref := alloc.Alloc(ns20)

	return ast.PdfArray{ns17Ref, ns20Ref}, ns20Ref
}

// ensureDocumentElementNS points the root /Document structure element's /NS
// at the PDF 2.0 namespace (ISO 14289-2 §8.2.5.2 t2). It sets /NS when
// missing and repoints it when it names the PDF 1.7 namespace; any other
// existing namespace is left untouched.
func ensureDocumentElementNS(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, stDict ast.PdfDict, ns20Ref ast.PdfIndirectRef) {
	docRef, docElem, needs := documentElementNeedingNS(ctx, alloc, doc, stDict)
	if !needs || docRef.ObjectNum == 0 {
		return
	}

	newDoc := make(ast.PdfDict, len(docElem)+1)
	maps.Copy(newDoc, docElem)
	newDoc["NS"] = ns20Ref
	alloc.AllocAt(docRef.ObjectNum, newDoc)
}

// documentElementNeedingNS resolves the structure tree root's single child
// and returns it when it is a Document element whose /NS is missing or
// names the PDF 1.7 namespace. /K may be the element reference itself or a
// one-element array holding it.
func documentElementNeedingNS(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, stDict ast.PdfDict) (ast.PdfIndirectRef, ast.PdfDict, bool) {
	kObj, hasK := stDict["K"]
	if !hasK {
		return ast.PdfIndirectRef{}, nil, false
	}

	docRef, ok := kObj.(ast.PdfIndirectRef)
	if !ok {
		if arr, err := document.ResolveArray(ctx, doc, kObj); err == nil && len(arr) == 1 {
			docRef, ok = arr[0].(ast.PdfIndirectRef)
		}
		if !ok {
			return ast.PdfIndirectRef{}, nil, false
		}
	}

	docElem := resolveFromAllocOrDoc(ctx, alloc, doc, docRef)
	if docElem == nil {
		return ast.PdfIndirectRef{}, nil, false
	}
	// Only a Document element carries the §8.2.5.2 t2 obligation. Other
	// root children (e.g. Note) may be valid only in the PDF 1.7
	// namespace, so retagging them to 2.0 would create a new violation.
	if name, err := document.ResolveName(ctx, doc, docElem["S"]); err != nil || name != "Document" {
		return ast.PdfIndirectRef{}, nil, false
	}
	if nsObj, hasNS := docElem["NS"]; hasNS && !namespaceIsPDF1(ctx, alloc, doc, nsObj) {
		return ast.PdfIndirectRef{}, nil, false
	}
	return docRef, docElem, true
}

// namespaceIsPDF1 reports whether an /NS value resolves to the PDF 1.7
// standard structure namespace.
func namespaceIsPDF1(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, nsObj ast.PdfObject) bool {
	var nsDict ast.PdfDict
	if ref, ok := nsObj.(ast.PdfIndirectRef); ok {
		nsDict = resolveFromAllocOrDoc(ctx, alloc, doc, ref)
	}
	if nsDict == nil {
		if d, err := document.ResolveDict(ctx, doc, nsObj); err == nil {
			nsDict = d
		}
	}
	if nsDict == nil {
		return false
	}
	uri, err := document.ResolveString(ctx, doc, nsDict["NS"])
	if err != nil {
		return false
	}
	return string(uri) == pdf1NamespaceURI
}
