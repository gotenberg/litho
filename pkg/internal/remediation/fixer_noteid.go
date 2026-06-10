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

// matchNoteID matches the PDF/UA-1 Note-identifier violation this fixer repairs:
// every Note structure element shall carry a non-empty /ID (ISO 14289-1 §7.9 t1).
func matchNoteID(v compliance.Violation) bool {
	return v.Rule.Specification == "ISO_14289_1" && v.Rule.Clause == "7.9" && v.Rule.TestNumber == 1
}

// fixNoteID adds a unique /ID to every Note structure element that lacks one
// (ISO 14289-1 §7.9 t1). The /ID is an opaque identifier with no human-readable
// meaning, so a generated value is a faithful repair rather than a guess: §7.9
// requires only presence (t1) and uniqueness (t2), both of which a fresh value
// keyed on the element's object number satisfies. Existing /ID values are never
// rewritten, so no /IDTree or /Ref reference is invalidated; pre-existing duplicate
// IDs (t2) are left untouched because renaming a referenced identifier is unsafe.
//
// Note elements are found by their raw /S name, exactly as the detection does
// (structure.go), so checker and fixer stay in lockstep. Only an
// indirectly-referenced element is repaired, since an object number is needed to
// queue the replacement.
func fixNoteID(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalogRef, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
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

	existingIDs := make(map[string]bool)
	var needID []ast.PdfIndirectRef
	visited := make(map[int]bool)
	if err := collectNotes(ctx, alloc, doc, stDict["K"], visited, 0, existingIDs, &needID); err != nil {
		return nil, err
	}

	added := 0
	for _, ref := range needID {
		elem := resolveFromAllocOrDoc(ctx, alloc, doc, ref)
		if elem == nil {
			continue
		}
		id := uniqueNoteID(ref.ObjectNum, existingIDs)
		existingIDs[id] = true
		patched := make(ast.PdfDict, len(elem)+1)
		maps.Copy(patched, elem)
		patched["ID"] = ast.PdfString(id)
		alloc.AllocAt(ref.ObjectNum, patched)
		added++
	}

	if added == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("added unique /ID to %d Note element(s)", added)}, nil
}

// uniqueNoteID derives a deterministic identifier from the element's object
// number, disambiguating against existing identifiers in the unlikely event of a
// collision.
func uniqueNoteID(objNum int, existing map[string]bool) string {
	id := fmt.Sprintf("litho-note-%d", objNum)
	for i := 0; existing[id]; i++ {
		id = fmt.Sprintf("litho-note-%d-%d", objNum, i)
	}
	return id
}

// collectNotes walks the structure tree from a node's /K children, recording the
// /ID of every Note element (for uniqueness) and the references of Note elements
// that lack one. visited guards against reference cycles; depth is bounded by
// [maxStructTreeDepth] as an anti-DoS guard.
func collectNotes(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, kObj ast.PdfObject, visited map[int]bool, depth int, existingIDs map[string]bool, needID *[]ast.PdfIndirectRef) error {
	if kObj == nil || depth > maxStructTreeDepth {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	arr, err := document.ResolveArray(ctx, doc, kObj)
	if err != nil {
		arr = ast.PdfArray{kObj} // /K may be a single child rather than an array.
	}

	for _, item := range arr {
		ref, isIndirect := item.(ast.PdfIndirectRef)
		if !isIndirect || ref.ObjectNum == 0 || visited[ref.ObjectNum] {
			continue
		}
		visited[ref.ObjectNum] = true

		elem := resolveFromAllocOrDoc(ctx, alloc, doc, ref)
		if elem == nil {
			continue
		}
		if _, hasS := elem["S"]; !hasS {
			continue // Not a structure element (marked-content or object reference).
		}
		if typeName, _ := document.ResolveName(ctx, doc, elem["S"]); typeName == "Note" {
			if idStr, _ := document.ResolveString(ctx, doc, elem["ID"]); len(idStr) > 0 {
				existingIDs[string(idStr)] = true
			} else {
				*needID = append(*needID, ref)
			}
		}
		if err := collectNotes(ctx, alloc, doc, elem["K"], visited, depth+1, existingIDs, needID); err != nil {
			return err
		}
	}
	return nil
}
