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

// matchStructParent matches the PDF/UA structure-element /P (parent) presence
// violations: ISO 14289-1 §7.1 t12 and ISO 14289-2 §8.2.1 t2. Both require every
// structure element dictionary to carry a /P entry pointing at its parent node.
func matchStructParent(v compliance.Violation) bool {
	switch {
	case v.Rule.Specification == "ISO_14289_1" && v.Rule.Clause == "7.1" && v.Rule.TestNumber == 12:
		return true
	case v.Rule.Specification == "ISO_14289_2" && v.Rule.Clause == "8.2.1" && v.Rule.TestNumber == 2:
		return true
	}
	return false
}

// fixStructParent adds the required /P (parent) entry to every structure element
// that lacks one. The correct value is unambiguous: a structure element's parent
// is the node that references it through /K, which the fixer already holds while
// walking down from the StructTreeRoot. The repair is therefore deterministic and
// lossless, adds only a structural back-pointer (ISO 32000-2:2020 Table 323), and
// never changes what renders, so it is registered as safetyStructural and runs in
// every mode.
//
// Only indirectly-referenced elements are repaired, since an object number is
// needed to queue the replacement; a rare inline structure element (a bare dict
// embedded in a parent's /K) is left untouched rather than risk rewriting its
// container incorrectly. Children are matched to the detection walk exactly: a /K
// item is a structure element only when it carries an /S (structure type) key,
// which excludes marked-content references, object references, and MCID integers.
func fixStructParent(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
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

	visited := make(map[int]bool)
	added := 0
	if err := setParentRefs(ctx, alloc, doc, stRef, stDict["K"], visited, 0, &added); err != nil {
		return nil, err
	}
	if added == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("added /P parent entry to %d structure element(s)", added)}, nil
}

// setParentRefs walks the /K children of a node whose indirect reference is
// parentRef, setting /P = parentRef on every child structure element that lacks
// one, then recurses into that child. visited guards against reference cycles and
// depth bounds the recursion as an anti-DoS guard; see [maxStructTreeDepth].
func setParentRefs(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, parentRef ast.PdfIndirectRef, kObj ast.PdfObject, visited map[int]bool, depth int, added *int) error {
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
		if !isIndirect || ref.ObjectNum == 0 {
			continue // Inline children cannot be queued for replacement by object number.
		}
		if visited[ref.ObjectNum] {
			continue
		}
		visited[ref.ObjectNum] = true

		child := resolveFromAllocOrDoc(ctx, alloc, doc, ref)
		if child == nil {
			continue
		}
		if _, hasS := child["S"]; !hasS {
			continue // Marked-content ref, object ref, or MCID: not a structure element.
		}

		if _, hasP := child["P"]; !hasP {
			patched := make(ast.PdfDict, len(child)+1)
			maps.Copy(patched, child)
			patched["P"] = parentRef
			alloc.AllocAt(ref.ObjectNum, patched)
			*added++
			child = patched
		}
		if err := setParentRefs(ctx, alloc, doc, ref, child["K"], visited, depth+1, added); err != nil {
			return err
		}
	}
	return nil
}
