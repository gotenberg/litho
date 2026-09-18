package mutations

import (
	"context"
	"fmt"
	"maps"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/heuristics"
)

// removeRegionAnnotations drops the annotations sitting inside the zones and
// returns the object numbers it removed, ascending.
//
// Clearing a page's content stream does not touch its annotations. An
// annotation over cleared content keeps whatever it quotes in /Contents or
// /A /URI, keeps any appearance stream that draws across the area, and its
// /Rect goes on marking precisely where the removed content was. Selection is
// geometric for the same reason the rest of the redaction is: a hidden or
// zero-opacity annotation is exactly what a failed redaction leaves behind.
//
// It refuses rather than remove approximately. An annotation reaching outside
// the region reports [ErrAnnotationStraddlesRegion], because an annotation is
// atomic and dropping it would take behaviour the caller never selected. A
// widget reports [ErrAnnotationIsFormWidget], because its field lives in the
// catalog's tree and would be left naming an annotation no page carries. One
// whose /Rect cannot be read reports [ErrAnnotationNotPlaceable]: it cannot be
// ruled outside the region either.
//
// The page dictionary is read alloc-first and written back through the
// allocator, so the change rolls back with everything else when a later stage
// refuses.
func removeRegionAnnotations(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	page *document.Page,
	zones []heuristics.Zone,
) ([]int, error) {
	baseDict := latestPageDict(alloc, page)
	annotsObj, ok := baseDict["Annots"]
	if !ok {
		return nil, nil
	}
	annots, err := document.ResolveArray(ctx, doc, annotsObj)
	if err != nil || annots == nil {
		return nil, nil
	}

	kept := make(ast.PdfArray, 0, len(annots))
	var removed []int

	for _, entry := range annots {
		annot, err := document.ResolveDict(ctx, doc, entry)
		if err != nil || annot == nil {
			// Not an annotation this can read, so not one it can place.
			return nil, fmt.Errorf("%w: an entry of /Annots", ErrAnnotationNotPlaceable)
		}

		rect, ok := annotRect(ctx, doc, annot)
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrAnnotationNotPlaceable, annotLabel(entry))
		}

		selected := false
		for _, z := range zones {
			if heuristics.ZonesOverlap(rect, z) {
				selected = true
				break
			}
		}
		if !selected {
			kept = append(kept, entry)
			continue
		}

		contained := false
		for _, z := range zones {
			if heuristics.ZoneContains(z, rect) {
				contained = true
				break
			}
		}
		if !contained {
			return nil, fmt.Errorf("%w: %s", ErrAnnotationStraddlesRegion, annotLabel(entry))
		}
		if subtype, _ := annot["Subtype"].(ast.PdfName); subtype == "Widget" {
			return nil, fmt.Errorf("%w: %s", ErrAnnotationIsFormWidget, annotLabel(entry))
		}

		if ref, isRef := entry.(ast.PdfIndirectRef); isRef {
			removed = append(removed, ref.ObjectNum)
		}
	}

	if len(kept) == len(annots) {
		return nil, nil
	}

	// Written as a direct array on the page rather than back into whatever
	// object /Annots named. An /Annots array shared with another page keeps its
	// own entries that way, and the page dictionary is the one object this
	// already rewrites.
	newDict := make(ast.PdfDict, len(baseDict))
	maps.Copy(newDict, baseDict)
	newDict["Annots"] = kept
	if !alloc.AllocAt(page.Ref.ObjectNum, newDict) {
		return nil, ErrPageNotAddressable
	}

	sort.Ints(removed)
	return removed, nil
}

// annotRect returns the annotation's rectangle as a zone.
//
// [heuristics.ZoneFromRect] normalises the corners, which matters here: per
// ISO 32000-1, 7.9.5 a rectangle may state either diagonal, so /Rect
// [urx ury llx lly] is as valid as the other order and appears in real files.
// Comparing un-normalised corners would silently miss those annotations.
func annotRect(ctx context.Context, doc *document.Document, annot ast.PdfDict) (heuristics.Zone, bool) {
	obj, ok := annot["Rect"]
	if !ok {
		return heuristics.Zone{}, false
	}
	arr, err := document.ResolveArray(ctx, doc, obj)
	if err != nil || len(arr) != 4 {
		return heuristics.Zone{}, false
	}
	var r [4]float64
	for i := range r {
		// Per ISO 32000-1, 7.3.10 any value may be written as an indirect
		// reference, a rectangle's numbers included, so each is resolved before
		// it is read. Refusing one for being a reference would report an
		// annotation unplaceable that every other reader places without trouble.
		elem, err := document.ResolveObject(ctx, doc, arr[i])
		if err != nil {
			return heuristics.Zone{}, false
		}
		v, isNum := document.Number(elem)
		if !isNum {
			return heuristics.Zone{}, false
		}
		r[i] = v
	}
	return heuristics.ZoneFromRect(r), true
}

// annotLabel names an annotation for a refusal, so the same file always names
// the same one.
func annotLabel(entry ast.PdfObject) string {
	if ref, isRef := entry.(ast.PdfIndirectRef); isRef {
		return fmt.Sprintf("annotation %d", ref.ObjectNum)
	}
	return "an annotation written directly into /Annots"
}
