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

// matchFontKeys matches the PDF/A font-dictionary required-key violations this
// fixer repairs: a missing /Type Font (t1, all parts) and a missing /BaseFont on
// a non-Type 3 font (t3, PDF/A-1 only). It keys on the exact test number plus the
// clause and specification because §6.3.2 is overloaded: in PDF/A-1 it is the
// font clause, while in PDF/A-2/3 it is the annotation clause. Restricting the
// §6.3.2 match to ISO_19005_1 avoids claiming a PDF/A-2/3 annotation violation.
func matchFontKeys(v compliance.Violation) bool {
	c, spec := v.Rule.Clause, v.Rule.Specification
	switch v.Rule.TestNumber {
	case 1:
		// /Type Font: ISO 19005-1 §6.3.2, ISO 19005-2/3 §6.2.11.2, ISO 19005-4 §6.2.10.2.
		return (c == "6.3.2" && spec == "ISO_19005_1") || c == "6.2.11.2" || c == "6.2.10.2"
	case 3:
		// /BaseFont: ISO 19005-1 §6.3.2 only.
		return c == "6.3.2" && spec == "ISO_19005_1"
	}
	return false
}

// fixFontKeys adds two required font-dictionary keys whose correct value is
// unambiguous, so the repair is lossless and rendering-neutral:
//   - /Type Font, which every font dictionary shall carry (ISO 32000-1 Table 110).
//   - /BaseFont, copied verbatim from /FontDescriptor /FontName, which ISO 32000-1
//     Table 122 defines to equal /BaseFont. It is set only when /FontName resolves
//     to a non-empty name; a name is never synthesised, since fabricating one
//     could alter font matching (the lesson of the reverted font-width repair).
//
// Both edits are deterministic and preserve visible rendering, so the fixer runs
// in every mode and is registered as safetyResource rather than gated on the
// aggressive (opts.PreserveContent == false) flag. Fonts are located via the
// /Font dictionaries of the /Resources each page resolves its names against
// ([pageResourceCategory]) and by /Subtype, never by /Type, because a font
// missing /Type is exactly what this fixer repairs.
func fixFontKeys(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		doc.Logger.Debug("remediation.font-keys: cannot resolve catalog", "error", err)
		return nil, nil
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		doc.Logger.Debug("remediation.font-keys: cannot resolve pages", "error", err)
		return nil, nil
	}

	processed := make(map[int]bool)
	var typeAdded, baseFontAdded int

	for _, pe := range pageTreeEntries(ctx, alloc, doc, pages) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		fontDict := pageResourceCategory(ctx, alloc, doc, pe, "Font")
		if fontDict == nil {
			continue
		}

		for _, fontEntry := range fontDict {
			ref, ok := fontEntry.(ast.PdfIndirectRef)
			if !ok || ref.ObjectNum == 0 {
				continue // Only an indirectly-referenced font can be replaced.
			}
			if processed[ref.ObjectNum] {
				continue
			}
			processed[ref.ObjectNum] = true

			fd := resolveFromAllocOrDoc(ctx, alloc, doc, ref)
			if fd == nil {
				continue
			}

			needType := false
			if t, _ := fd["Type"].(ast.PdfName); t != "Font" {
				needType = true
			}

			var baseFont ast.PdfName
			subtype, _ := fd["Subtype"].(ast.PdfName)
			if _, has := fd["BaseFont"]; !has && subtype != "Type3" {
				if desc, derr := document.ResolveDict(ctx, doc, fd["FontDescriptor"]); derr == nil && desc != nil {
					if fn, _ := document.ResolveName(ctx, doc, desc["FontName"]); fn != "" {
						baseFont = fn
					}
				}
			}

			if !needType && baseFont == "" {
				continue
			}

			newFont := make(ast.PdfDict, len(fd)+2)
			maps.Copy(newFont, fd)
			if needType {
				newFont["Type"] = ast.PdfName("Font")
				typeAdded++
			}
			if baseFont != "" {
				newFont["BaseFont"] = baseFont
				baseFontAdded++
			}
			alloc.AllocAt(ref.ObjectNum, newFont)
		}
	}

	var fixes []string
	if typeAdded > 0 {
		fixes = append(fixes, fmt.Sprintf("added /Type /Font to %d font dictionary(ies)", typeAdded))
	}
	if baseFontAdded > 0 {
		fixes = append(fixes, fmt.Sprintf("added /BaseFont from /FontDescriptor /FontName to %d font dictionary(ies)", baseFontAdded))
	}
	return fixes, nil
}
