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

// matchAnnotNesting claims the ISO 14289-1 rules that place an annotation in
// the logical structure: §7.18.1 test 1 (every other annotation inside an
// Annot), §7.18.4 test 1 (a Widget inside a Form) and §7.18.5 test 1 (a Link
// inside a Link). All three compare the same value, the standard type of the
// element the annotation's /StructParent names, so one repair serves them.
//
// The test numbers are named exactly. §7.18.1 test 2 asks for a description
// rather than a place, and litho does not fabricate descriptions
// (see [fixFigureAlt]); §7.18.4 test 2 and §7.18.5 test 2 are separate
// requirements this fixer does not satisfy.
func matchAnnotNesting(v compliance.Violation) bool {
	if v.Rule.Specification != "ISO_14289_1" || v.Rule.TestNumber != 1 {
		return false
	}
	switch v.Rule.Clause {
	case "7.18.1", "7.18.4", "7.18.5":
		return true
	}
	return false
}

// annotNestingRole returns the structure type ISO 14289-1 §7.18 requires around
// an annotation of the given subtype, and whether this fixer handles it.
//
// PrinterMark is excluded because §7.18.8 requires the opposite, that it stay
// out of the logical structure, and TrapNet because §7.18.2 forbids the
// annotation outright: giving either a structure parent would trade one
// violation for another.
func annotNestingRole(subtype ast.PdfName) (ast.PdfName, bool) {
	switch subtype {
	case "":
		return "", false
	case "PrinterMark", "TrapNet":
		return "", false
	case "Widget":
		return "Form", true
	case "Link":
		return "Link", true
	}
	return "Annot", true
}

// fixAnnotNesting gives an annotation that stands outside the logical structure
// the enclosing structure element ISO 14289-1 §7.18.1, §7.18.4 and §7.18.5
// require: a new element of the right standard type, holding an object
// reference to the annotation, registered in the structure parent tree so the
// annotation's /StructParent reaches it (ISO 32000-2, 14.7.4.4).
//
// It runs for PDF/UA-1 only. ISO 14289-1 §7.18.1 requires a Popup to be nested,
// while ISO 14289-2 §8.9.2.4.9 requires it to be an artifact, which
// [fixAnnotArtifact] enforces by removing /StructParent. Scoping by profile
// keeps the two from undoing each other.
//
// Only an annotation with no /StructParent at all is repaired. One whose
// /StructParent names an element of the wrong type is left alone: re-typing an
// element the producer built, or moving the annotation under a different
// parent, can invalidate the surrounding tree, and the violation carries no
// page or annotation index with which to target it precisely.
//
// The new element hangs off the Document element. ISO/TS 32005 Table 5 forbids
// StructTreeRoot from holding an Annot directly, and forbids Form, L, LI,
// Table and the other grouping types from holding one, but permits Document.
func fixAnnotNesting(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, profile string) ([]string, error) {
	if profile != "PDF/UA-1" {
		return nil, nil
	}

	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}

	stRef, isRef := catalogDict["StructTreeRoot"].(ast.PdfIndirectRef)
	if !isRef || stRef.ObjectNum == 0 {
		return nil, nil
	}
	stDict := resolveFromAllocOrDoc(ctx, alloc, doc, stRef)
	if stDict == nil {
		return nil, nil
	}

	docElemRef, isRef := stDict["K"].(ast.PdfIndirectRef)
	if !isRef || docElemRef.ObjectNum == 0 {
		return nil, nil
	}
	docElem := resolveFromAllocOrDoc(ctx, alloc, doc, docElemRef)
	if docElem == nil {
		return nil, nil
	}

	// Append after the highest existing key so /Nums stays sorted, and refuse a
	// tree this helper cannot extend safely (ISO 32000-2, 7.9.7).
	parentTreeNums, nextKey, mergeable := existingParentTreeNums(ctx, alloc, doc, stDict)
	if !mergeable {
		doc.Logger.Debug("remediation: annot-nesting skipped, ParentTree is not safely mergeable")
		return nil, nil
	}

	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("resolving catalog: %w", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return nil, fmt.Errorf("resolving pages: %w", err)
	}

	var (
		newKids ast.PdfArray
		count   int
	)
	for _, page := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		annots, err := document.ResolveArray(ctx, doc, page.Dict["Annots"])
		if err != nil || len(annots) == 0 {
			continue
		}
		for _, annotObj := range annots {
			annotRef, isRef := annotObj.(ast.PdfIndirectRef)
			if !isRef || annotRef.ObjectNum == 0 {
				continue
			}
			annot := resolveFromAllocOrDoc(ctx, alloc, doc, annotRef)
			if annot == nil {
				if d, derr := document.ResolveDict(ctx, doc, annotObj); derr == nil {
					annot = d
				}
			}
			if annot == nil {
				continue
			}
			if _, has := annot["StructParent"]; has {
				continue
			}
			subtype, _ := document.ResolveName(ctx, doc, annot["Subtype"])
			role, handled := annotNestingRole(subtype)
			if !handled {
				continue
			}

			// A Form element satisfies §7.18.4 test 2 only when its single
			// child is an object reference to the widget, so the reference is
			// written for every type rather than left to that one case.
			elem := ast.PdfDict{
				"Type": ast.PdfName("StructElem"),
				"S":    role,
				"P":    docElemRef,
				"K": ast.PdfArray{ast.PdfDict{
					"Type": ast.PdfName("OBJR"),
					"Obj":  annotRef,
				}},
			}
			if page.Ref.ObjectNum > 0 {
				elem["Pg"] = page.Ref
			}
			elemRef := alloc.Alloc(elem)

			newAnnot := make(ast.PdfDict, len(annot)+1)
			maps.Copy(newAnnot, annot)
			newAnnot["StructParent"] = ast.PdfInt(nextKey)
			alloc.AllocAt(annotRef.ObjectNum, newAnnot)

			parentTreeNums = append(parentTreeNums, ast.PdfInt(nextKey), elemRef)
			newKids = append(newKids, elemRef)
			nextKey++
			count++
		}
	}

	if count == 0 {
		return nil, nil
	}

	// Preserve the Document element's existing children: a single non-array
	// child, reference or inline dict, is normalized rather than dropped.
	var kChildren ast.PdfArray
	switch existing := docElem["K"].(type) {
	case nil:
	case ast.PdfArray:
		kChildren = append(kChildren, existing...)
	default:
		kChildren = append(kChildren, existing)
	}
	kChildren = append(kChildren, newKids...)

	newDocElem := make(ast.PdfDict, len(docElem)+1)
	maps.Copy(newDocElem, docElem)
	newDocElem["K"] = kChildren
	alloc.AllocAt(docElemRef.ObjectNum, newDocElem)

	// Write the tree back the way the other structural fixers do: a direct
	// /Nums dict on a StructTreeRoot queued at its own object number, so a
	// later fixer reading alloc-first sees this one's work (see
	// [fixParentTree] and [fixContentTag]). Allocating a separate object and
	// repointing /ParentTree at it would orphan the original and drop whatever
	// sibling keys it carried. /ParentTreeNextKey is maintained because this
	// fixer consumes keys (ISO 32000-2, Table 323).
	newST := make(ast.PdfDict, len(stDict)+2)
	maps.Copy(newST, stDict)
	newST["ParentTree"] = ast.PdfDict{"Nums": parentTreeNums}
	newST["ParentTreeNextKey"] = ast.PdfInt(nextKey)
	alloc.AllocAt(stRef.ObjectNum, newST)

	return []string{fmt.Sprintf("nested %d annotation(s) in a structure element", count)}, nil
}
