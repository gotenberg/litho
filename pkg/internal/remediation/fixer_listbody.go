package remediation

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// matchListBody matches the "LI contains a non-Lbl/non-LBody child" violation
// (ISO 14289-1 §7.2 t20), emitted in structure.go. The list defect once
// shared the t10 rule with the table-cell containment check and needed
// context disambiguation; t20 is list-specific, so none is needed. Matching
// the generated rule constant keys the fixer to the live emission site: a
// renumbering surfaces as a compile or test failure instead of a fixer that
// silently never fires (which is how the previous t10 keying died when the
// clause table was realigned).
func matchListBody(v compliance.Violation) bool {
	return v.Rule == compliance.Rule_14289_1_7_2_t20
}

// fixListBody wraps an LI's orphan content in an LBody. A list item whose direct
// children include a structure element other than Lbl or LBody is non-compliant
// (ISO 14289-1 §7.2): that real content belongs in an LBody. The fixer allocates
// an LBody, moves the orphan children into it (updating each child's /P), and
// replaces them in the LI's /K with the new LBody. It mirrors the validator,
// which flags only direct child structure elements (those with an /S role) that
// resolve to a role other than Lbl or LBody; raw marked content and object
// references carry no /S and are left in place. Only orphans given as indirect
// references are moved, so the rebuild never rewrites an inline subtree, and an
// LI that already owns an LBody is left untouched rather than gaining a second
// one. The /ParentTree is left untouched: the moved objects keep their object
// numbers, so only the /P pointers and the LI's /K change.
func fixListBody(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	return wrapStructChildren(ctx, alloc, doc, "LI", wrapOrphans, "wrapped %d orphan list-item child(ren) in an LBody")
}

// wrapOrphans wraps an LI's orphan child structure elements in a new LBody. It
// returns the number of children wrapped, or 0 when no repair applies. An orphan
// is a direct child whose /S role resolves to something other than Lbl or LBody,
// matching the validator; a child without a resolvable role, raw marked content,
// or an object reference is left in place. The fixer bails (returns 0) when the
// LI already owns an LBody, or when any orphan is inline, since neither can be
// re-parented in place safely.
func wrapOrphans(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, li structNode) int {
	arr := resolveStructKids(ctx, doc, li.dict["K"])
	if len(arr) == 0 {
		return 0
	}

	var hasLBody, inlineOrphan bool
	var orphanRefs []ast.PdfIndirectRef
	for _, item := range arr {
		d, err := document.ResolveDict(ctx, doc, item)
		if err != nil || d == nil {
			// Raw marked content or an object reference: not flagged, left alone.
			continue
		}
		switch structType(ctx, doc, d) {
		case "Lbl":
			// The list label stays in place.
		case "LBody":
			hasLBody = true
		case "":
			// An unresolvable /S is not flagged by the validator; leave it.
		default:
			if ref, ok := item.(ast.PdfIndirectRef); ok {
				orphanRefs = append(orphanRefs, ref)
			} else {
				inlineOrphan = true
			}
		}
	}

	if hasLBody || len(orphanRefs) == 0 || inlineOrphan {
		return 0
	}

	return reparentIntoContainer(ctx, alloc, doc, li, arr, orphanRefs, "LBody")
}
