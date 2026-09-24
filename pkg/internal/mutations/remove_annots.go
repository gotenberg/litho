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
	var removed, rootWidgets []int
	var kidWidgets []kidWidget

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
			// A widget carrying /Parent is a kid of a field that lives on above
			// it. Detaching one means pruning that parent's /Kids and deciding
			// what becomes of a field whose kids all went, so it still refuses.
			//
			// A parentless widget is the merged field-and-annotation of
			// ISO 32000-1, 12.5.6.19: it IS its own root field, and AcroForm
			// /Fields holds root fields only (12.7.2), so removing it is
			// dropping one entry from that array and nothing else. Whether it
			// also states /FT makes no difference to the detachment.
			if parentObj, hasParent := annot["Parent"]; hasParent {
				// The parent has to be an object this can rewrite, and the
				// widget an entry that parent can name. Either missing leaves
				// nothing to detach from, so it still refuses.
				parentRef, parentIsRef := parentObj.(ast.PdfIndirectRef)
				ref, widgetIsRef := entry.(ast.PdfIndirectRef)
				if !parentIsRef || !widgetIsRef {
					return nil, fmt.Errorf("%w: %s", ErrAnnotationIsFormWidget, annotLabel(entry))
				}
				kidWidgets = append(kidWidgets, kidWidget{widget: ref.ObjectNum, parent: parentRef})
			} else if ref, isRef := entry.(ast.PdfIndirectRef); isRef {
				// A widget written directly into /Annots cannot be named by
				// /Fields, which holds references, so there is nothing to prune.
				rootWidgets = append(rootWidgets, ref.ObjectNum)
			}
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

	if err := pruneKidWidgets(ctx, alloc, doc, kidWidgets); err != nil {
		return nil, err
	}
	if err := pruneRootFields(ctx, alloc, doc, rootWidgets); err != nil {
		return nil, err
	}

	sort.Ints(removed)
	return removed, nil
}

// kidWidget is a removed widget and the field that named it through /Kids.
type kidWidget struct {
	widget int
	parent ast.PdfIndirectRef
}

// pruneKidWidgets detaches removed widgets from the fields that named them.
//
// A widget carrying /Parent is reached by that field's /Kids (ISO 32000-1,
// 12.7.3.1), so dropping it from the page alone leaves the field naming an
// annotation no page carries. Removing it there is one hop: the field is the
// direct parent, and nothing further up has to be walked. That matters, because
// /Parent chains in the wild are not always trees, and a walk would need the
// cycle guard [rootFieldOf] carries.
//
// A field whose last kid goes keeps the field and loses the /Kids entry rather
// than being removed itself. Roughly half the fields carrying /Kids have exactly
// one, so this is the common outcome, not a corner: the field still states its
// name and type, other fields may share that name, and its value is
// [ScrubFormFieldValues]' to clear. Cascading upward instead would decide on the
// caller's behalf that a named field should cease to exist, which
// [pruneFieldKids] declines to do for the same reason.
//
// Parents are read alloc-first and grouped, so two widgets under one field, or a
// field pruned again for a later page, compose rather than overwrite each other.
//
// # Corpus evidence
//
//   - All corpora: one file carries 16 /Parent chains that cycle.
func pruneKidWidgets(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	kids []kidWidget,
) error {
	if len(kids) == 0 {
		return nil
	}

	// Grouped in first-seen order, so one field is rewritten once and the same
	// document always writes the same bytes.
	byParent := make(map[int][]int, len(kids))
	var order []ast.PdfIndirectRef
	for _, k := range kids {
		if _, seen := byParent[k.parent.ObjectNum]; !seen {
			order = append(order, k.parent)
		}
		byParent[k.parent.ObjectNum] = append(byParent[k.parent.ObjectNum], k.widget)
	}

	for _, parentRef := range order {
		parent := ResolveFromAllocOrDoc(ctx, alloc, doc, parentRef)
		if parent == nil {
			return ErrFormNotAddressable
		}
		// Resolved from the document for the same reason, and with the same
		// invariant, as in pruneRootFields: the pruned array is written INTO the
		// field dictionary, never back as an object of its own.
		kidsArr, err := document.ResolveArray(ctx, doc, parent["Kids"])
		if err != nil || kidsArr == nil {
			continue
		}

		dropping := make(map[int]bool, len(byParent[parentRef.ObjectNum]))
		for _, objNum := range byParent[parentRef.ObjectNum] {
			dropping[objNum] = true
		}
		kept := make(ast.PdfArray, 0, len(kidsArr))
		for _, kid := range kidsArr {
			if ref, isRef := kid.(ast.PdfIndirectRef); isRef && dropping[ref.ObjectNum] {
				continue
			}
			kept = append(kept, kid)
		}
		if len(kept) == len(kidsArr) {
			continue
		}

		newParent := make(ast.PdfDict, len(parent))
		maps.Copy(newParent, parent)
		if len(kept) == 0 {
			delete(newParent, "Kids")
		} else {
			newParent["Kids"] = kept
		}
		if !alloc.AllocAt(parentRef.ObjectNum, newParent) {
			return ErrFormNotAddressable
		}
	}
	return nil
}

// pruneRootFields drops the given root fields from the document's AcroForm.
//
// A removed merged field-and-annotation is still named by /Fields, and a form
// naming a field no page carries is one a viewer may still tab to and a
// document-wide scrub may still find. Per ISO 32000-1, 12.7.2 that array holds
// the ROOT fields, which is exactly what a parentless widget is, so the entry
// goes and nothing below it has to be walked.
//
// Read alloc-first and written back through the allocator, so this joins the
// rollback a later refusal relies on. A document stating no form, or one whose
// /Fields does not name these objects, needs no change: removing the widget
// from the page was the whole of it.
func pruneRootFields(
	ctx context.Context,
	alloc *writer.ObjAllocator,
	doc *document.Document,
	rootWidgets []int,
) error {
	if len(rootWidgets) == 0 {
		return nil
	}
	catalogRef, ok := CatalogRef(ctx, doc)
	if !ok {
		return nil
	}
	catalogDict := ResolveFromAllocOrDoc(ctx, alloc, doc, catalogRef)
	if catalogDict == nil {
		return nil
	}
	acroFormObj, hasForm := catalogDict["AcroForm"]
	if !hasForm {
		return nil
	}
	// Alloc-first, like the catalog above, and for a reason one level less
	// obvious: a caller redacting page after page shares one allocator, so a
	// form this already pruned for an earlier page has to be read back as it now
	// stands. Resolving it from the document would prune the original a second
	// time and write that over the earlier result, putting the first page's
	// field back into /Fields with no widget left to carry it.
	var acroForm ast.PdfDict
	if ref, isRef := acroFormObj.(ast.PdfIndirectRef); isRef {
		acroForm = ResolveFromAllocOrDoc(ctx, alloc, doc, ref)
	} else {
		acroForm, _ = acroFormObj.(ast.PdfDict)
	}
	if acroForm == nil {
		return nil
	}
	// Resolved from the document rather than alloc-first, which is safe only
	// because of an invariant this function keeps: the pruned array is written
	// INTO the form dictionary, never back as an object of its own. A form read
	// from the allocator therefore already carries a direct /Fields, and
	// ResolveArray has nothing stale to fetch. Write the array separately one
	// day and this becomes the same bug the read above exists to avoid.
	fields, err := document.ResolveArray(ctx, doc, acroForm["Fields"])
	if err != nil || fields == nil {
		return nil
	}

	dropping := make(map[int]bool, len(rootWidgets))
	for _, objNum := range rootWidgets {
		dropping[objNum] = true
	}

	keptFields := make(ast.PdfArray, 0, len(fields))
	for _, entry := range fields {
		if ref, isRef := entry.(ast.PdfIndirectRef); isRef && dropping[ref.ObjectNum] {
			continue
		}
		keptFields = append(keptFields, entry)
	}
	if len(keptFields) == len(fields) {
		return nil
	}

	newForm := make(ast.PdfDict, len(acroForm))
	maps.Copy(newForm, acroForm)
	newForm["Fields"] = keptFields

	// An inline /AcroForm travels with the catalog, so the catalog is what gets
	// rewritten; an indirect one is replaced where it stands.
	if ref, isRef := acroFormObj.(ast.PdfIndirectRef); isRef {
		if !alloc.AllocAt(ref.ObjectNum, newForm) {
			return ErrFormNotAddressable
		}
		return nil
	}
	newCatalog := make(ast.PdfDict, len(catalogDict))
	maps.Copy(newCatalog, catalogDict)
	newCatalog["AcroForm"] = newForm
	if !alloc.AllocAt(catalogRef.ObjectNum, newCatalog) {
		return ErrFormNotAddressable
	}
	return nil
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
