package remediation

import (
	"context"
	"fmt"
	"maps"
	"math"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// widthFixTolerance mirrors the compliance width-consistency tolerance: a
// /Widths entry within one glyph-space unit of the font program's advance width
// already passes the check, so only larger deviations are rewritten. Keeping the
// threshold equal to the checker's keeps the fixer idempotent and avoids churning
// values the validator would accept.
const widthFixTolerance = 1.0

// matchFontWidth matches glyph-width consistency violations (ISO 19005-1 §6.3.6,
// ISO 19005-2/3 §6.2.11.5, ISO 19005-4 §6.2.10.5). The checker reports each as
// "char N width mismatch", the one context substring common to every profile.
func matchFontWidth(v compliance.Violation) bool {
	return contextContains(v, "width mismatch")
}

// fixFontWidth rewrites a simple font's /Widths entries to match the advance
// widths in its embedded font program. The font program is the authoritative
// source for glyph widths, so an entry that disagrees with it is wrong; setting
// it to the program's width is the spec-mandated repair (ISO 19005 glyph-width
// consistency) and the value the [compliance] width check accepts. Only entries
// the width could be computed for are changed; entries within tolerance are left
// untouched, so a font whose widths already agree is unmodified.
func fixFontWidth(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	var count int
	for objNum, entry := range doc.Index.Entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.Type != 1 || objNum == 0 {
			continue
		}

		ref := ast.PdfIndirectRef{ObjectNum: objNum, GenNum: entry.Generation}
		fontDict := resolveFromAllocOrDoc(ctx, alloc, doc, ref)
		if fontDict == nil {
			continue
		}
		if t, _ := fontDict["Type"].(ast.PdfName); t != "Font" {
			continue
		}

		expected, ok := compliance.ExpectedSimpleFontWidths(ctx, doc, fontDict)
		if !ok {
			continue
		}
		widths, err := document.ResolveArray(ctx, doc, fontDict["Widths"])
		if err != nil || len(widths) != len(expected) {
			continue
		}

		newWidths := make(ast.PdfArray, len(widths))
		changed := false
		for i, w := range widths {
			newWidths[i] = w
			if expected[i] < 0 {
				continue
			}
			if math.Abs(document.Float(w)-float64(expected[i])) > widthFixTolerance {
				newWidths[i] = ast.PdfInt(expected[i])
				changed = true
			}
		}
		if !changed {
			continue
		}

		// Rewrite the /Widths array in place when it is its own indirect object;
		// otherwise inline the corrected array on a copy of the font dict.
		if wref, ok := fontDict["Widths"].(ast.PdfIndirectRef); ok && wref.ObjectNum > 0 {
			alloc.AllocAt(wref.ObjectNum, newWidths)
		} else {
			newFont := make(ast.PdfDict, len(fontDict))
			maps.Copy(newFont, fontDict)
			newFont["Widths"] = newWidths
			alloc.AllocAt(objNum, newFont)
		}
		count++
	}

	if count == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("corrected glyph widths in %d font(s)", count)}, nil
}
