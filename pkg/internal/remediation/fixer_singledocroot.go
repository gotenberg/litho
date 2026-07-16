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

// pdf2NamespaceURI is the PDF 2.0 standard structure namespace (ISO 32000-2
// §14.8.6), the namespace the single Document element must declare (§8.2.5.2 t2).
const pdf2NamespaceURI = "http://iso.org/pdf2/ssn"

// matchSingleDocRoot matches the PDF/UA-2 violation requiring the structure tree
// root to contain a single Document structure element as its only child
// (ISO 14289-2 §8.2.5.2 t1).
func matchSingleDocRoot(v compliance.Violation) bool {
	return v.Rule.Specification == "ISO_14289_2" && v.Rule.Clause == "8.2.5.2" && v.Rule.TestNumber == 1
}

// fixSingleDocRoot wraps the structure tree root's children in a single Document
// element when none is present (ISO 14289-2 §8.2.5.2 t1). It handles only the
// unambiguous case where the root has at least one structure-element child and no
// Document among them: a new Document is created with the original children, each
// child's /P is repointed to it, and the root's /K is repointed to the Document.
// The Document is given an explicit PDF 2.0 /NS so the repair cannot induce the
// §8.2.5.2 t2 namespace violation, finding or creating that namespace in the root's
// /Namespaces. Roots that already contain a Document (or whose children are not all
// indirect structure elements) are left untouched, since merging into or splitting
// an existing grouping is not unambiguous.
//
// The edit reorders the logical structure without changing page content, so it is
// registered as safetyStructural.
func fixSingleDocRoot(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}
	stRef, ok := catalogDict["StructTreeRoot"].(ast.PdfIndirectRef)
	if !ok || stRef.ObjectNum == 0 {
		return nil, nil
	}
	stDict := resolveFromAllocOrDoc(ctx, alloc, doc, stRef)
	if stDict == nil {
		return nil, nil
	}

	kids, ok := indirectStructChildren(ctx, alloc, doc, stDict["K"])
	if !ok || len(kids) == 0 {
		return nil, nil // Empty, inline, or non-structure children: not handled.
	}
	for _, ref := range kids {
		elem := resolveFromAllocOrDoc(ctx, alloc, doc, ref)
		if elem == nil {
			return nil, nil
		}
		if name, _ := document.ResolveName(ctx, doc, elem["S"]); name == "Document" {
			return nil, nil // A Document already exists; wrapping it is ambiguous.
		}
	}

	nsRef, stWithNS := ensurePDF2Namespace(ctx, alloc, doc, stDict)

	kidArray := make(ast.PdfArray, len(kids))
	for i, ref := range kids {
		kidArray[i] = ref
	}
	docElem := ast.PdfDict{
		"Type": ast.PdfName("StructElem"),
		"S":    ast.PdfName("Document"),
		"P":    stRef,
		"K":    kidArray,
		"NS":   nsRef,
	}
	docRef := alloc.Alloc(docElem)

	for _, ref := range kids {
		elem := resolveFromAllocOrDoc(ctx, alloc, doc, ref)
		if elem == nil {
			continue
		}
		patched := make(ast.PdfDict, len(elem)+1)
		maps.Copy(patched, elem)
		patched["P"] = docRef
		alloc.AllocAt(ref.ObjectNum, patched)
	}

	base := stDict
	if stWithNS != nil {
		base = stWithNS
	}
	newST := make(ast.PdfDict, len(base)+1)
	maps.Copy(newST, base)
	newST["K"] = docRef
	alloc.AllocAt(stRef.ObjectNum, newST)

	return []string{fmt.Sprintf("wrapped %d top-level structure element(s) in a Document element", len(kids))}, nil
}

// indirectStructChildren resolves a /K entry to the list of its children and
// returns their references only when every child is an indirectly-referenced
// structure element (a dict carrying /S). It returns ok=false otherwise, so the
// caller can decline to restructure a root whose children it cannot cleanly move.
func indirectStructChildren(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, kObj ast.PdfObject) ([]ast.PdfIndirectRef, bool) {
	if kObj == nil {
		return nil, false
	}
	arr, err := document.ResolveArray(ctx, doc, kObj)
	if err != nil {
		arr = ast.PdfArray{kObj}
	}
	refs := make([]ast.PdfIndirectRef, 0, len(arr))
	for _, item := range arr {
		ref, isIndirect := item.(ast.PdfIndirectRef)
		if !isIndirect || ref.ObjectNum == 0 {
			return nil, false
		}
		elem := resolveFromAllocOrDoc(ctx, alloc, doc, ref)
		if elem == nil {
			return nil, false
		}
		if _, hasS := elem["S"]; !hasS {
			return nil, false
		}
		refs = append(refs, ref)
	}
	return refs, true
}

// ensurePDF2Namespace returns a reference to a PDF 2.0 namespace object reachable
// from the structure tree root's /Namespaces, creating and appending one when
// absent. When it adds the namespace it returns the updated root dictionary as
// the second result; otherwise that result is nil and stDict is unchanged.
func ensurePDF2Namespace(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, stDict ast.PdfDict) (ast.PdfIndirectRef, ast.PdfDict) {
	var items ast.PdfArray
	if arr, err := document.ResolveArray(ctx, doc, stDict["Namespaces"]); err == nil {
		items = arr
		for _, item := range arr {
			ref, ok := item.(ast.PdfIndirectRef)
			if !ok {
				continue
			}
			nsd := resolveFromAllocOrDoc(ctx, alloc, doc, ref)
			if nsd == nil {
				continue
			}
			if uri, _ := document.ResolveString(ctx, doc, nsd["NS"]); string(uri) == pdf2NamespaceURI {
				return ref, nil
			}
		}
	}

	ns := ast.PdfDict{"Type": ast.PdfName("Namespace"), "NS": ast.PdfString(pdf2NamespaceURI)}
	ref := alloc.Alloc(ns)
	newArr := make(ast.PdfArray, 0, len(items)+1)
	newArr = append(newArr, items...)
	newArr = append(newArr, ref)
	newST := make(ast.PdfDict, len(stDict)+1)
	maps.Copy(newST, stDict)
	newST["Namespaces"] = newArr
	return ref, newST
}
