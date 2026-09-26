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

// widthFixTolerance mirrors the compliance width-consistency tolerance: a width
// entry within one glyph-space unit of the font program's advance width already
// passes the check, so only larger deviations are rewritten. Keeping the
// threshold equal to the checker's keeps the fixer idempotent and avoids churning
// values the validator would accept.
const widthFixTolerance = 1.0

// matchFontWidth matches glyph-width consistency violations (ISO 19005-1 §6.3.6,
// ISO 19005-2/3 §6.2.11.5, ISO 19005-4 §6.2.10.5). The checker reports each as
// "... width mismatch", the one context substring common to every profile and to
// both the simple-font (/Widths) and CIDFont (/W) variants.
//
// This is the only matcher in the registry that carries NO rule identity: a
// violation is claimed on the Context substring alone. That is precise in
// practice because nine emit sites, all in the width checker, are the only
// producers of the phrase, but it is a free-text contract between two packages.
// It reads [compliance.WidthMismatchContext] rather than a local copy so the
// two cannot drift, and TestWidthMismatchContext_IsTheWordingTheFixerMatches
// fails if the wording changes.
func matchFontWidth(v compliance.Violation) bool {
	return contextContains(v, compliance.WidthMismatchContext)
}

// fixFontWidth rewrites a CIDFontType2's /W widths to match the advance widths
// in its embedded font program, the authoritative source for glyph metrics. The
// repair is restricted to CIDFontType2 with an Identity CIDToGIDMap, where CID
// equals GID and no character-code-to-glyph mapping is involved: the program's
// width for a CID is unambiguous.
//
// Simple-font /Widths are deliberately NOT repaired. Their correction requires
// litho's character-code to glyph-name to GID resolution, which is unreliable for
// non-standard and high-bit codes (e.g. WinAnsi 0x96) on real fonts: it can pick
// the wrong glyph and yield a spurious "mismatch", so rewriting /Widths from it
// would corrupt correct widths. Until encoding resolution is reliable, simple
// fonts are left to the validator to report.
//
// A CIDFont stored in an object stream (ISO 32000-1, 7.5.7) is repaired too,
// for the reason [walkNumberedObjects] gives.
func fixFontWidth(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, _ []compliance.Violation, _ string) ([]string, error) {
	var count int
	for objNum, entry := range doc.XRefEntries() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.Free || objNum == 0 {
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

		if correctCIDFontWidths(ctx, alloc, doc, objNum, fontDict) {
			count++
		}
	}

	if count == 0 {
		return nil, nil
	}
	return []string{fmt.Sprintf("corrected glyph widths in %d font(s)", count)}, nil
}

// correctCIDFontWidths rewrites a CIDFont's /W array to the font program's
// advance widths. Only the individual-entry format [cid [w ...]] is patched; the
// range format [cidFirst cidLast w] is preserved verbatim, since correcting a
// range whose glyphs have differing widths would require splitting it. Returns
// true when it changed the font.
func correctCIDFontWidths(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, objNum int, fontDict ast.PdfDict) bool {
	// Only repair when CID == GID holds, so a CID indexes the font program's
	// glyph widths directly. That is true for a CIDFontType2 with an Identity
	// (or absent, default-Identity) CIDToGIDMap. For CIDFontType0 (CFF) or a
	// stream CIDToGIDMap the mapping is not identity, and computing widths by
	// treating the CID as the GID would write wrong values and regress the file.
	subtype, _ := document.ResolveName(ctx, doc, fontDict["Subtype"])
	if subtype != "CIDFontType2" {
		return false
	}
	if ctg, ok := fontDict["CIDToGIDMap"]; ok {
		if name, _ := document.ResolveName(ctx, doc, ctg); name != "Identity" {
			return false
		}
	}

	expected, ok := compliance.ExpectedCIDFontWidths(ctx, doc, fontDict)
	if !ok {
		return false
	}
	wArr, err := document.ResolveArray(ctx, doc, fontDict["W"])
	if err != nil || len(wArr) == 0 {
		return false
	}

	newW, changed := patchCIDWidthArray(ctx, doc, wArr, expected)
	if !changed {
		return false
	}

	writeFontWidthArray(alloc, fontDict, objNum, "W", newW)
	return true
}

// patchCIDWidthArray returns a copy of a CIDFont /W array with individual-entry
// widths corrected to the expected values, and whether anything changed. It
// mirrors the validator's /W parse: [cid [w ...]] runs are patched per glyph,
// [cidFirst cidLast w] runs and anything malformed are copied through unchanged.
func patchCIDWidthArray(ctx context.Context, doc *document.Document, wArr ast.PdfArray, expected map[int]int) (ast.PdfArray, bool) {
	out := make(ast.PdfArray, 0, len(wArr))
	changed := false
	for i := 0; i < len(wArr); {
		cidStart, ok := resolveIntElem(ctx, doc, wArr[i])
		if !ok || i+1 >= len(wArr) {
			out = append(out, wArr[i])
			i++
			continue
		}

		if sub, err := document.ResolveArray(ctx, doc, wArr[i+1]); err == nil && sub != nil {
			// Individual format: [cidStart [w0 w1 ...]].
			newSub := make(ast.PdfArray, len(sub))
			for j, w := range sub {
				newSub[j] = w
				if exp, ok := expected[cidStart+j]; ok && math.Abs(document.Float(w)-float64(exp)) > widthFixTolerance {
					newSub[j] = ast.PdfInt(exp)
					changed = true
				}
			}
			out = append(out, wArr[i], newSub)
			i += 2
			continue
		}

		if _, ok := resolveIntElem(ctx, doc, wArr[i+1]); ok {
			// Range format: [cidStart cidEnd w]. Preserve verbatim.
			if i+2 >= len(wArr) {
				out = append(out, wArr[i:]...)
				break
			}
			out = append(out, wArr[i], wArr[i+1], wArr[i+2])
			i += 3
			continue
		}

		out = append(out, wArr[i])
		i++
	}
	return out, changed
}

// writeFontWidthArray stores the corrected width array back: in place when the
// original entry is its own indirect object, otherwise inlined on a copy of the
// font dict.
func writeFontWidthArray(alloc *writer.ObjAllocator, fontDict ast.PdfDict, objNum int, key ast.PdfName, arr ast.PdfArray) {
	if ref, ok := fontDict[key].(ast.PdfIndirectRef); ok && ref.ObjectNum > 0 {
		alloc.AllocAt(ref.ObjectNum, arr)
		return
	}
	newFont := make(ast.PdfDict, len(fontDict))
	maps.Copy(newFont, fontDict)
	newFont[key] = arr
	alloc.AllocAt(objNum, newFont)
}

// resolveIntElem resolves a /W array element to an int, reporting ok=false when
// it is not an integer.
func resolveIntElem(ctx context.Context, doc *document.Document, obj ast.PdfObject) (int, bool) {
	r, err := document.ResolveObject(ctx, doc, obj)
	if err != nil {
		return 0, false
	}
	if n, ok := r.(ast.PdfInt); ok {
		return int(n), true
	}
	return 0, false
}
