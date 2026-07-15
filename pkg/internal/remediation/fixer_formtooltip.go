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

func matchFormTooltip(v compliance.Violation) bool {
	return descriptionContains(v, "TU") && descriptionContains(v, "widget") ||
		descriptionContains(v, "tooltip") ||
		descriptionContains(v, "label and no /Contents") ||
		descriptionContains(v, "label for a widget") ||
		descriptionContains(v, "labelling")
}

// fixFormTooltip adds /TU (tooltip) entries to form fields that lack them.
// PDF/UA-1 §7.18.4 and PDF/UA-2 §8.10.2.3 require widget annotations / form
// fields to expose a description for assistive technology, either via /TU on
// the field or /Contents on the widget annotation.
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
		return nil, nil
	}

	fieldsObj, ok := acroForm["Fields"]
	if !ok {
		return nil, nil
	}
	fields, err := document.ResolveArray(ctx, doc, fieldsObj)
	if err != nil || len(fields) == 0 {
		return nil, nil
	}

	var count int
	walkFormTooltipFields(ctx, alloc, doc, fields, &count, make(map[int]bool))

	if count == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("added /TU tooltip to %d form field(s)", count)}, nil
}

// walkFormTooltipFields recursively walks form field tree and patches each
// field that lacks a tooltip-equivalent entry. seen processes each field object
// once, so a shared or cyclic /Kids tree cannot force exponential work.
func walkFormTooltipFields(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, fields ast.PdfArray, count *int, seen map[int]bool) {
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
		field, err := document.ResolveDict(ctx, doc, fieldObj)
		if err != nil || field == nil {
			continue
		}

		// Recurse into /Kids first.
		if kidsObj, ok := field["Kids"]; ok {
			if kids, err := document.ResolveArray(ctx, doc, kidsObj); err == nil {
				walkFormTooltipFields(ctx, alloc, doc, kids, count, seen)
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
				newField["Contents"] = nameStr
			}
		}
		if fieldRef.ObjectNum > 0 {
			alloc.AllocAt(fieldRef.ObjectNum, newField)
			*count++
		}
	}
}
