package remediation

import (
	"context"
	"fmt"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/intconv"
)

func matchFormTooltip(v compliance.Violation) bool {
	return descriptionContains(v, "TU") && descriptionContains(v, "widget") ||
		descriptionContains(v, "tooltip") ||
		descriptionContains(v, "label and no /Contents") ||
		descriptionContains(v, "label for a widget") ||
		descriptionContains(v, "labelling")
}

// fixFormTooltip adds /TU (tooltip) entries to form fields that lack them, and
// a /Contents entry to a merged widget that needs one.
//
// PDF/UA-1 §7.18.4 and PDF/UA-2 §8.10.2.3 require widget annotations / form
// fields to expose a description for assistive technology. The two do not
// accept the same entry: §8.10.2.3 test 1 is answered by a Lbl structure
// element or by /Contents on the annotation and NOT by /TU, so a widget given
// only a tooltip still fails it. That is why /Contents is written below as well.
//
// A signed signature field is repaired like any other. The entries go on the
// field dictionary, never on the signature dictionary its /V names, whose
// /ByteRange fixes the bytes the digest covers (ISO 32000-1, 12.8.1): an
// incremental update keeps the signed revision byte for byte, so the signature
// still verifies, and a full rewrite drops the signed field whole. That is the
// line [fixSignatureReferences] draws too: in standard mode it leaves a signed
// signature DICTIONARY alone, not the field holding it. annot-nesting writes
// /StructParent on the same widget in the same run, so skipping the tooltip
// would leave the field written and the ISO 14289-1, 7.18.1 violation standing.
func fixFormTooltip(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, fmt.Errorf("resolving catalog: %w", err)
	}

	acroFormObj, ok := catalog.Dict["AcroForm"]
	if !ok {
		return nil, nil
	}
	acroForm, err := document.ResolveDict(ctx, doc, acroFormObj)
	if err != nil || acroForm == nil {
		return nil, nil //nolint:nilerr // An unresolvable /AcroForm is null (ISO 32000-1, 7.3.10): no field to repair.
	}

	fieldsObj, ok := acroForm["Fields"]
	if !ok {
		return nil, nil
	}
	fields, err := document.ResolveArray(ctx, doc, fieldsObj)
	if err != nil || len(fields) == 0 {
		return nil, nil //nolint:nilerr // An unresolvable /Fields is null (ISO 32000-1, 7.3.10): no field to repair.
	}

	// An annotation's /StructParent keys the structure tree's /ParentTree to
	// the element that encloses it, which ISO 14289-2 §8.9.4.2 makes relevant
	// to the /Contents written below. Resolved once for the whole walk.
	var parentTree ast.PdfObject
	if stObj, ok := catalog.Dict["StructTreeRoot"]; ok {
		if st, stErr := document.ResolveDict(ctx, doc, stObj); stErr == nil && st != nil {
			parentTree = st["ParentTree"]
		}
	}

	var count int
	walkFormTooltipFields(ctx, alloc, doc, parentTree, fields, &count, make(map[int]bool), 0)

	if count == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("added /TU tooltip to %d form field(s)", count)}, nil
}

// enclosingAlt returns the /Alt of the structure element enclosing a widget
// annotation, which ISO 14289-2 §8.9.4.2 defines as the element the
// annotation's /StructParent names through the /ParentTree.
//
// §8.9.4.2 test 1 requires a widget's /Contents and that /Alt to hold identical
// values, so a /Contents derived from the field name repairs §8.10.2.3 and
// introduces §8.9.4.2 in the same pass wherever an /Alt already says something
// else. The string is returned unchanged rather than decoded and re-encoded, so
// the two entries stay byte-identical.
//
// An /Alt present but EMPTY reports true with an empty string: the clause binds
// on the entry existing, not on it saying anything, and treating empty as
// absent reintroduced the mismatch. The /Alt may also be an indirect
// reference, which is why the value is resolved.
//
// # Corpus evidence
//
//   - verapdf PDF_UA-1__7.18.1-t03-fail-c.pdf: treating an empty /Alt as
//     absent reintroduced the mismatch.
//   - verapdf PDF_UA-1__7.18.1-t03-pass-b.pdf: its /Alt is an indirect
//     reference.
func enclosingAlt(ctx context.Context, doc *document.Document, parentTree ast.PdfObject, field ast.PdfDict) (ast.PdfString, bool) {
	if parentTree == nil {
		return "", false
	}
	spObj, ok := field["StructParent"]
	if !ok {
		return "", false
	}
	key, err := document.ResolveInt(ctx, doc, spObj)
	if err != nil {
		return "", false
	}
	elemObj, err := document.LookupNumberTree(ctx, doc, parentTree, intconv.Clamp(key))
	if err != nil || elemObj == nil {
		return "", false
	}
	elem, err := document.ResolveDict(ctx, doc, elemObj)
	if err != nil || elem == nil {
		return "", false
	}
	altObj, ok := elem["Alt"]
	if !ok {
		return "", false
	}
	resolved, err := document.ResolveObject(ctx, doc, altObj)
	if err != nil {
		return "", false
	}
	alt, isStr := resolved.(ast.PdfString)
	if !isStr {
		return "", false
	}
	return alt, true
}

// walkFormTooltipFields recursively walks form field tree and patches each
// field that lacks a tooltip-equivalent entry. seen processes each field
// object, and each /Kids array written as an object of its own, once
// ([document.VisitedBefore]), so a shared or cyclic /Kids tree cannot force
// exponential work: the direct fields such an array holds can each name it
// again. The descent stops at [maxFieldTreeDepth], as the field action walk
// does, for the stack's sake.
//
// # Corpus evidence
//
//   - All corpora: at PDF/UA-1, 1815 widgets in 151 files lost the
//     /StructParent annot-nesting had just given them.
//   - pdfjs bug1669099.pdf, govdocs thread_300_300033.pdf: the next round
//     nested every widget again, leaving 30 Form elements for 15 widgets and
//     316 for 158.
func walkFormTooltipFields(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, parentTree ast.PdfObject, fields ast.PdfArray, count *int, seen map[int]bool, depth int) {
	if depth > maxFieldTreeDepth {
		return
	}
	for _, fieldObj := range fields {
		if ctx.Err() != nil {
			return
		}

		var fieldRef ast.PdfIndirectRef
		if ref, ok := fieldObj.(ast.PdfIndirectRef); ok {
			fieldRef = ref
			if seen[ref.ObjectNum] {
				continue
			}
			seen[ref.ObjectNum] = true
		}
		// Alloc-first: the field is written back whole, and on a merged field
		// and widget annot-nesting has already queued the /StructParent that
		// links it to its new Form element (ISO 14289-1, 7.18.4).
		field := liveDictValue(ctx, alloc, doc, fieldObj)
		if field == nil {
			continue
		}

		// Recurse into /Kids first.
		if kidsObj, ok := field["Kids"]; ok && !document.VisitedBefore(kidsObj, seen) {
			if kids := liveArray(ctx, alloc, doc, kidsObj); len(kids) > 0 {
				walkFormTooltipFields(ctx, alloc, doc, parentTree, kids, count, seen, depth+1)
			}
		}

		// Already has /TU or /Contents: nothing to do.
		if _, hasTU := field["TU"]; hasTU {
			continue
		}
		if _, hasContents := field["Contents"]; hasContents {
			continue
		}

		// Need /T to derive a tooltip; skip otherwise.
		nameObj, hasName := field["T"]
		if !hasName {
			continue
		}
		nameStr, ok := nameObj.(ast.PdfString)
		if !ok || len(nameStr) == 0 {
			continue
		}

		newField := make(ast.PdfDict, len(field)+2)
		maps.Copy(newField, field)
		newField["TU"] = nameStr
		// Widget annotations need /Contents on the annotation itself for
		// PDF/UA-2 §8.10.2.3. When the field IS the widget (merged
		// field+widget dict), set both /TU and /Contents on the same dict.
		if subtype, _ := field["Subtype"].(ast.PdfName); subtype == "Widget" {
			if _, hasContents := field["Contents"]; !hasContents {
				// Per ISO 14289-2, 8.9.4.2, /Contents shall equal the /Alt of
				// the enclosing structure element. Preferring the field name
				// there repairs 8.10.2.3 and creates 8.9.4.2 in one pass.
				//
				// An EMPTY /Alt is copied as an empty /Contents rather than
				// skipped. 8.10.2.3 asks only that the entry be present when
				// no Lbl element labels the widget, and /TU does not answer it
				// (see checkWidgetLabel and the empty /Contents case in
				// TestCheckWidgetAA), so writing nothing leaves 8.10.2.3
				// unfixed while writing the field name breaks 8.9.4.2. The
				// empty copy satisfies both.
				if alt, hasAlt := enclosingAlt(ctx, doc, parentTree, field); hasAlt {
					newField["Contents"] = alt
				} else {
					newField["Contents"] = nameStr
				}
			}
		}
		if fieldRef.ObjectNum > 0 {
			alloc.AllocAt(fieldRef.ObjectNum, newField)
			*count++
		}
	}
}
