package mutations

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// CheckRemovedAnnotationUses refuses a redaction that removed an annotation
// another page still shows. An annotation is one object however many pages
// list it in /Annots (ISO 32000-1, 12.5.2), so dropping it from the page whose
// region selected it leaves it drawn, with whatever it quotes, on every other
// page that lists it. A widget is worse off still: its field leaves /AcroForm
// with it, and the page keeping it carries a widget no field names (12.7.3.1).
// Such an annotation reaches outside the region, so it reports
// [ErrAnnotationStraddlesRegion], as [removeRegionAnnotations] does for one
// whose rectangle does. An annotation every listing page's region covers is
// removed from each of them and passes.
//
// It reads each page as the redaction left it, so it runs once every page's
// regions have been applied.
func CheckRemovedAnnotationUses(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	pages []*document.Page,
	removed []int,
) error {
	if len(removed) == 0 {
		return nil
	}
	targets := make(map[int]bool, len(removed))
	for _, objNum := range removed {
		targets[objNum] = true
	}
	for i, page := range pages {
		if err := ctx.Err(); err != nil {
			return err
		}
		if page.Ref.ObjectNum == 0 {
			continue
		}
		annots := latestAnnots(ctx, alloc, doc, latestPageDict(alloc, page))
		for _, entry := range annots {
			ref, ok := entry.(ast.PdfIndirectRef)
			if ok && targets[ref.ObjectNum] {
				return fmt.Errorf("%w: annotation (obj %d) is still shown on page %d",
					ErrAnnotationStraddlesRegion, ref.ObjectNum, i+1)
			}
		}
	}
	return nil
}

// CheckRewrittenAnnotationUses refuses a redaction that rewrote an annotation
// another page also lists: cut it to the part outside the regions, or drew it
// into its page and emptied or removed it. The annotation is one object (ISO
// 32000-1, 12.5.2), so every page listing it now shows the rewrite, which only
// the page whose regions made it asked for. rewritten maps each 0-based page
// index to the annotations the redaction rewrote for it, and pages are read
// as the redaction left them. An annotation rewritten for one page and listed
// on no other passes.
func CheckRewrittenAnnotationUses(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	pages []*document.Page,
	rewritten map[int][]int,
) error {
	if len(rewritten) == 0 {
		return nil
	}
	owner := make(map[int]int)
	for _, page := range slices.Sorted(maps.Keys(rewritten)) {
		for _, objNum := range rewritten[page] {
			if at, cut := owner[objNum]; cut {
				return fmt.Errorf("%w: annotation (obj %d) is rewritten for pages %d and %d",
					ErrAnnotationStraddlesRegion, objNum, at+1, page+1)
			}
			owner[objNum] = page
		}
	}
	for i, page := range pages {
		if err := ctx.Err(); err != nil {
			return err
		}
		if page.Ref.ObjectNum == 0 {
			continue
		}
		for _, entry := range latestAnnots(ctx, alloc, doc, latestPageDict(alloc, page)) {
			ref, ok := entry.(ast.PdfIndirectRef)
			if !ok {
				continue
			}
			if at, cut := owner[ref.ObjectNum]; cut && at != i {
				return fmt.Errorf("%w: annotation (obj %d), rewritten for the regions of page %d, is also shown on page %d",
					ErrAnnotationStraddlesRegion, ref.ObjectNum, at+1, i+1)
			}
		}
	}
	return nil
}

// latestAnnots returns a page's /Annots as the redaction staged it: an array
// the allocator holds a replacement for is read from the allocator.
func latestAnnots(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, pageDict ast.PdfDict) ast.PdfArray {
	obj, ok := pageDict["Annots"]
	if !ok {
		return nil
	}
	if ref, isRef := obj.(ast.PdfIndirectRef); isRef {
		if pending, staged := alloc.Pending()[ref.ObjectNum]; staged {
			obj = pending
		}
	}
	annots, err := document.ResolveArray(ctx, doc, obj)
	if err != nil {
		return nil // An unresolvable /Annots is null (ISO 32000-1, 7.3.10): the page shows no annotation.
	}
	return annots
}
