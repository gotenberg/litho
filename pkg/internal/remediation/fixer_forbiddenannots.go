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

// forbiddenUA2AnnotSubtypes are the annotation subtypes PDF/UA-2 forbids outright:
// Sound and Movie carry time-based media that cannot be made accessible
// (§8.9.2.4.11), and TrapNet is a prepress trapping construct with no accessible
// representation (§8.9.2.4.15). The only conforming remediation is to remove them.
// This set is distinct from the PDF/A [forbiddenAnnotSubtypes]: PDF/A permits
// TrapNet, while PDF/UA-2 does not.
var forbiddenUA2AnnotSubtypes = map[ast.PdfName]bool{
	"Sound":   true,
	"Movie":   true,
	"TrapNet": true,
}

// matchForbiddenAnnots matches the PDF/UA-2 forbidden-annotation violations this
// fixer repairs: Sound/Movie (§8.9.2.4.11) and TrapNet (§8.9.2.4.15).
func matchForbiddenAnnots(v compliance.Violation) bool {
	return v.Rule.Specification == "ISO_14289_2" &&
		(v.Rule.Clause == "8.9.2.4.11" || v.Rule.Clause == "8.9.2.4.15")
}

// fixForbiddenAnnots removes every Sound, Movie, and TrapNet annotation from each
// page's /Annots array. These subtypes are forbidden in PDF/UA-2 and have no
// conforming alternative, so removal is the only path to conformance, mirroring
// the forbidden-construct removal already done for JavaScript, XFA, and PresSteps.
//
// It removes only an /Annots array entry, not page content, exactly as the PDF/A
// forbidden-annotation fixer ([fixAnnotations]) does for §6.3.1; that fixer is the
// established safetyResource precedent, so this one is registered the same way. The
// writer's post-remediation integrity probe remains the backstop for any removal
// that would unexpectedly drop page content.
func fixForbiddenAnnots(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalogRef, ok := catalogRefForFixer(ctx, doc)
	if !ok {
		return nil, nil
	}
	catalogDict := resolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil, nil
	}
	pagesObj, ok := catalogDict["Pages"]
	if !ok {
		return nil, nil
	}
	var pages []annotPageEntry
	collectPageRefs(ctx, alloc, doc, pagesObj, &pages, make(map[int]bool))

	removed := 0
	for _, pe := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		annotsObj, ok := pe.dict["Annots"]
		if !ok {
			continue
		}
		// The array is rebuilt whole and written back, so it is read through
		// the allocator: the annotations fixer runs first and writes the
		// /Annots it filtered at its own object number, and a copy of the
		// file's array put back what it removed.
		arr := liveArray(ctx, alloc, doc, annotsObj)

		filtered := make(ast.PdfArray, 0, len(arr))
		for _, item := range arr {
			if d := liveAnnotation(ctx, alloc, doc, item); d != nil {
				if subtype, _ := document.ResolveName(ctx, doc, d["Subtype"]); forbiddenUA2AnnotSubtypes[subtype] {
					removed++
					continue
				}
			}
			filtered = append(filtered, item)
		}
		if len(filtered) == len(arr) {
			continue
		}

		// Rewrite the /Annots array where it lives: the array object when it is
		// referenced indirectly, otherwise the page dictionary that inlines it,
		// written back into its parent's /Kids when the page is written
		// directly there ([writeTreeNode]).
		if ref, ok := annotsObj.(ast.PdfIndirectRef); ok && ref.ObjectNum > 0 {
			alloc.AllocAt(ref.ObjectNum, filtered)
		} else {
			newPage := maps.Clone(liveTreeNode(ctx, alloc, doc, pe))
			newPage["Annots"] = filtered
			writeTreeNode(ctx, alloc, doc, pe, newPage)
		}
	}

	if removed == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("removed %d forbidden Sound/Movie/TrapNet annotation(s)", removed)}, nil
}
