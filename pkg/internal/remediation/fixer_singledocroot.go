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
//
// # Corpus evidence
//
//   - All corpora: hanging the old roots straight off the new Document made 12
//     files fail after remediation, a stray LI becoming "Table 5. Document-LI".
//   - All corpora: listing the Document in place of each element it wrapped
//     left the structure tree of 311 of 1126 tagged files remediated to
//     PDF/UA-2 unreachable from its root, 35808 kid slots, and neither litho nor
//     veraPDF reported it.
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

	docRef := alloc.Alloc(ast.PdfDict{
		"Type": ast.PdfName("StructElem"),
		"S":    ast.PdfName("Document"),
		"P":    stRef,
		"NS":   nsRef,
	})

	// A Document may not hold every structure type. ISO/TS 32005 Table 5
	// forbids 24 of them directly under it, so hanging the old roots straight
	// off the new Document repairs the missing root and introduces a
	// containment fault instead: a stray LI became "Table 5. Document-LI".
	// Where the matrix forbids the pairing, put the container it does allow in
	// between.
	kidArray := make(ast.PdfArray, 0, len(kids))
	interposed := 0
	for _, ref := range kids {
		elem := resolveFromAllocOrDoc(ctx, alloc, doc, ref)
		if elem == nil {
			kidArray = append(kidArray, ref)
			continue
		}
		role, _ := document.ResolveName(ctx, doc, elem["S"])
		// The Document lists the element as its kid, or the container that
		// holds it: ISO 32000-1, 14.7.2, Table 323 has /K list an element's
		// children, and each child's /P name the element listing it.
		parentRef, kid := docRef, ref
		container, needed := compliance.ISO32005Interposer("Document", string(role))
		if needed {
			// The container owns the element; the Document owns the container.
			parentRef = alloc.Alloc(ast.PdfDict{
				"Type": ast.PdfName("StructElem"),
				"S":    ast.PdfName(container),
				"P":    docRef,
				"K":    ast.PdfArray{ref},
				"NS":   nsRef,
			})
			kid = parentRef
			interposed++
		}
		kidArray = append(kidArray, kid)

		patched := make(ast.PdfDict, len(elem)+1)
		maps.Copy(patched, elem)
		patched["P"] = parentRef
		alloc.AllocAt(ref.ObjectNum, patched)
	}

	docElem := resolveFromAllocOrDoc(ctx, alloc, doc, docRef)
	newDoc := make(ast.PdfDict, len(docElem)+1)
	maps.Copy(newDoc, docElem)
	newDoc["K"] = kidArray
	alloc.AllocAt(docRef.ObjectNum, newDoc)

	base := stDict
	if stWithNS != nil {
		base = stWithNS
	}
	newST := make(ast.PdfDict, len(base)+1)
	maps.Copy(newST, base)
	newST["K"] = docRef
	alloc.AllocAt(stRef.ObjectNum, newST)

	fixes := []string{fmt.Sprintf("wrapped %d top-level structure element(s) in a Document element", len(kids))}
	if interposed > 0 {
		fixes = append(fixes, fmt.Sprintf("interposed a container for %d element(s) a Document may not hold", interposed))
	}
	return fixes, nil
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
	return ensureNamespace(ctx, alloc, doc, stDict, pdf2NamespaceURI)
}

// ensureNamespace is [ensurePDF2Namespace] for the namespace whose URI is uri.
func ensureNamespace(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, stDict ast.PdfDict, uri string) (ast.PdfIndirectRef, ast.PdfDict) {
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
			if got, _ := document.ResolveString(ctx, doc, nsd["NS"]); string(got) == uri {
				return ref, nil
			}
		}
	}

	ns := ast.PdfDict{"Type": ast.PdfName("Namespace"), "NS": ast.PdfString(uri)}
	ref := alloc.Alloc(ns)
	newArr := make(ast.PdfArray, 0, len(items)+1)
	newArr = append(newArr, items...)
	newArr = append(newArr, ref)
	newST := make(ast.PdfDict, len(stDict)+1)
	maps.Copy(newST, stDict)
	newST["Namespaces"] = newArr
	return ref, newST
}
