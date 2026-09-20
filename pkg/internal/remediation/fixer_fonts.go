package remediation

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"slices"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/typography"
)

func matchFontFixes(v compliance.Violation) bool {
	return clausePrefix(v,
		"6.2.10.5", "6.2.11.5", "6.3.6", // glyph width mismatch
		"7.21.5", "8.4.5.6", // the PDF/UA-1 and PDF/UA-2 restatements of the width rule
		"6.2.10.7",                 // ToUnicode maps to U+0000/FEFF/FFFE
		"6.2.11.7",                 // Unicode mapping required (PDF/A-2)
		"6.3.8",                    // ToUnicode required (PDF/A-1b)
		"6.2.10.3.1", "6.2.11.3.1", // CIDSystemInfo mismatch
		"7.21.3.1", "8.4.5.3.1", // the PDF/UA restatements of it
		"6.2.11.4.2", // CIDSet incomplete
		"6.3.5",      // CharSet incomplete
		"7.21.4.2",   // PDF/UA-1 CharSet/CIDSet incomplete
	)
}

func fixFontIssues(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ Options, violations []compliance.Violation, _ string) ([]string, error) {
	// Width corrections are gated on a reported width mismatch
	// (ISO 19005-1 §6.3.6, 19005-2 §6.2.10.5/§6.2.11.5). These clauses
	// require /Widths to agree with the embedded font program's metrics,
	// so a correction is warranted only where that disagreement is
	// reported. Rewriting widths uninvited turned conforming /Widths
	// arrays into new §6.3.6 violations whenever litho's font-program
	// width extraction rounded or interpreted a metric differently from
	// the value the original producer already wrote conformantly.
	// A regenerated /CIDSet or /CharSet is subject to the same hazard as
	// /Widths: litho's enumeration of the font program's glyphs need not match
	// the CHECK's, so rewriting one uninvited replaces a conforming set with a
	// non-conforming one. bug911034.pdf went from 0 to 1 violations of
	// ISO 19005-2, 6.2.11.4.2 that way ("CID 1 in font program not in
	// /CIDSet"). Regenerate only where the disagreement is actually reported.
	hasWidthViolation := false
	hasCIDSetViolation := false
	hasCharSetViolation := false
	needCIDSet := false
	for _, v := range violations {
		// The PDF/UA restatements belong here for the same reason they belong
		// in [matchFontFixes]: ISO 14289-1 7.21.5 and ISO 14289-2 8.4.5.6 state
		// the identical requirement, so a mismatch reported under a UA profile
		// is just as much a reported mismatch. Claiming the violation without
		// enabling this flag left the fixer running and repairing nothing: the
		// matcher learned 7.21.5 in 5ea5b54e while this gate, written in
		// f17f6792, kept only the PDF/A spellings.
		if clausePrefix(v, "6.2.10.5", "6.2.11.5", "6.3.6", "7.21.5", "8.4.5.6") {
			hasWidthViolation = true
		}
		// Kept apart: a reported /CharSet defect says nothing about /CIDSet,
		// and regenerating the other one uninvited is what replaced a
		// conforming set with a non-conforming one. ISO 14289-1, 7.21.4.2
		// covers both, so it enables both.
		if clausePrefix(v, "6.2.11.4.2", "7.21.4.2") {
			hasCIDSetViolation = true
		}
		if clausePrefix(v, "6.3.5", "7.21.4.2") {
			hasCharSetViolation = true
		}
		// ISO 19005-1, 6.3.5 t3 is a PRESENCE rule: the descriptor of a rendered
		// CIDFont subset shall INCLUDE a /CIDSet stream. It needs a flag of its
		// own for two reasons. The regeneration path above rewrites only a key
		// that is already there, so a reported absence reaches nothing; and
		// clausePrefix cannot tell t3 from t2 (a Type 1 /CharSet) because both
		// sit under clause 6.3.5, so t3 was raising the /CharSet flag, which
		// only ever reaches simple fonts, and the CIDFont the violation named
		// was never touched.
		//
		// The /CharSet twin, t2, is deliberately NOT repaired. Supplying one
		// measured as a FALSE FIX: on 6-2-10-9-t01-fail-a under PDF_A-1a and
		// PDF_A-1b litho accepted the written /CharSet while veraPDF went on
		// reporting 6.3.5 t2, taking the corpus disagreement metric 14 -> 16.
		// litho's glyph-name enumeration need not match the checker's, which is
		// the hazard the /Widths comment above already records.
		if v.Rule.Clause == "6.3.5" && v.Rule.TestNumber == 3 {
			needCIDSet = true
		}
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, nil
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil {
		return nil, nil
	}

	var fixes []string
	processed := make(map[int]bool)

	for _, page := range pages {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if page.Resources == nil {
			continue
		}
		fontDictObj, ok := page.Resources["Font"]
		if !ok {
			continue
		}
		fontDict, err := document.ResolveDict(ctx, doc, fontDictObj)
		if err != nil || fontDict == nil {
			continue
		}

		// Which codes this page actually shows, per font resource name. A
		// generated /ToUnicode is only attached when it covers them, because
		// that is the set the validator holds it to.
		var shown map[string]map[byte]bool
		if ops, perr := content.ParsePageContent(ctx, doc, page.Dict); perr == nil {
			shown = compliance.ShownCharCodes(ops)
		}

		// Sorted iteration so CMap object allocation (and the output
		// bytes) is deterministic across runs.
		for _, fontName := range sortedDictKeys(fontDict) {
			fontEntry := fontDict[fontName]
			var fontRef ast.PdfIndirectRef
			if ref, ok := fontEntry.(ast.PdfIndirectRef); ok {
				fontRef = ref
			}
			if fontRef.ObjectNum > 0 && processed[fontRef.ObjectNum] {
				continue
			}
			if fontRef.ObjectNum > 0 {
				processed[fontRef.ObjectNum] = true
			}

			fd, err := document.ResolveDict(ctx, doc, fontEntry)
			if err != nil || fd == nil {
				continue
			}

			subtype, _ := fd["Subtype"].(ast.PdfName)
			switch string(subtype) {
			case "Type0":
				if f := fixType0Font(ctx, doc, alloc, fd, fontRef, hasWidthViolation, hasCIDSetViolation, needCIDSet); len(f) > 0 {
					fixes = append(fixes, f...)
				}
			case "Type1", "TrueType", "MMType1":
				if f := fixSimpleFont(ctx, doc, alloc, fd, fontRef, hasWidthViolation, hasCharSetViolation); len(f) > 0 {
					fixes = append(fixes, f...)
				}
				// A /ToUnicode is built from the font's /Encoding, so it needs
				// none of the width repair's preconditions: no /Widths array,
				// no /FontDescriptor, and no program metrics. Running it inside
				// fixSimpleFont meant one of those early returns cancelled it,
				// and every Type 1 font kept its missing Unicode mapping
				// (ISO 19005-2, 6.2.11.7.2).
				if f := fixSimpleFontToUnicode(ctx, doc, alloc, fd, fontRef, shown[string(fontName)]); len(f) > 0 {
					fixes = append(fixes, f...)
				}
			}
		}
	}

	if len(fixes) == 0 {
		return nil, nil
	}
	return fixes, nil
}

func fixSimpleFont(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, fontDict ast.PdfDict, fontRef ast.PdfIndirectRef, fixWidths, regenCharSet bool) []string {
	fdObj, ok := fontDict["FontDescriptor"]
	if !ok {
		return nil
	}
	var fdRef ast.PdfIndirectRef
	if ref, ok := fdObj.(ast.PdfIndirectRef); ok {
		fdRef = ref
	}
	fdDict, err := document.ResolveDict(ctx, doc, fdObj)
	if err != nil || fdDict == nil {
		return nil
	}

	fontData := extractFontProgram(ctx, doc, fdDict)
	if fontData == nil {
		return nil
	}

	binaryWidths := typography.ExtractBinaryWidths(ctx, fontData)
	if len(binaryWidths) == 0 {
		return nil
	}

	widthsObj, ok := fontDict["Widths"]
	if !ok {
		return nil
	}
	widths, err := document.ResolveArray(ctx, doc, widthsObj)
	if err != nil || widths == nil {
		return nil
	}

	firstChar := 0
	if fc, ok := fontDict["FirstChar"].(ast.PdfInt); ok {
		firstChar = int(fc)
	}

	// Shared metrics with the width CHECK (compliance.ExpectedSimpleFontWidths):
	// the remediation must compute the same expected widths the check uses, or
	// it writes values the check then rejects under ISO 19005-2 §6.2.11.5. The
	// old private mapping fell back to gid == charCode for codes absent from the
	// cmap and lacked the Type1 glyph-name chain, so it produced widths that
	// disagreed with the font program and stayed non-conforming after the fix.
	expectedWidths, okMetrics := compliance.ExpectedSimpleFontWidths(ctx, doc, fontDict)

	modified := false
	newWidths := make(ast.PdfArray, len(widths))
	copy(newWidths, widths)

	if !fixWidths || !okMetrics {
		widths = nil
	}
	for i, wObj := range widths {
		pdfWidth := document.Float(wObj)
		expected, ok := expectedWidths[firstChar+i]
		if !ok {
			continue
		}
		if math.Abs(pdfWidth-expected) > 1.0 {
			newWidths[i] = ast.PdfReal(math.Round(expected*100) / 100)
			modified = true
		}
	}

	var fixes []string
	baseName := fontBaseName(fontDict)

	if modified {
		// Build on the latest version of the font dictionary: an earlier repair
		// in this same pass may already have rewritten it. The caller reads the
		// dictionary once from the document and hands that value here, so
		// copying it would discard what a peer wrote. fonts runs at registry
		// 144, after cid-gidmap (110), font-keys (111), tounicode-ranges (114)
		// and nested-resources (131), every one of which writes font
		// dictionaries. [fixType0Font] and [fixSimpleFontToUnicode] already
		// read this way; this one did not.
		targetFont := fontDict
		if allocated, ok := alloc.Pending()[fontRef.ObjectNum]; ok {
			if d, isDict := allocated.(ast.PdfDict); isDict {
				targetFont = d
			}
		}
		newFont := make(ast.PdfDict, len(targetFont))
		maps.Copy(newFont, targetFont)
		newFont["Widths"] = newWidths
		// A direct font dictionary has no object number, so nothing can be
		// written and the repair must not be reported.
		if alloc.AllocAt(fontRef.ObjectNum, newFont) {
			fixes = append(fixes, fmt.Sprintf("corrected /Widths for font %s", baseName))
		}
	}

	// Fix CharSet if incomplete. Supplying a MISSING one (ISO 19005-1, 6.3.5
	// t2) was tried and reverted: litho accepted the written /CharSet while
	// veraPDF kept reporting the rule, which silences the violation on litho's
	// side without repairing it. See the scan in [fixFontIssues].
	if _, ok := fdDict["CharSet"]; ok && regenCharSet {
		if fixed := fixCharSet(ctx, alloc, fdDict, fdRef, fontData); fixed {
			fixes = append(fixes, fmt.Sprintf("regenerated /CharSet for font %s", baseName))
		}
	}

	return fixes
}

func fixType0Font(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, fontDict ast.PdfDict, fontRef ast.PdfIndirectRef, fixWidths, regenCIDSet, createCIDSet bool) []string {
	descArr, err := document.ResolveArray(ctx, doc, fontDict["DescendantFonts"])
	if err != nil || len(descArr) == 0 {
		return nil
	}

	var cidRef ast.PdfIndirectRef
	if ref, ok := descArr[0].(ast.PdfIndirectRef); ok {
		cidRef = ref
	}
	// Alloc-aware read. What is built from this is handed to [persistCIDFont],
	// which writes it back at cidRef.ObjectNum, so the read and the write sit
	// in different functions and neither looks wrong on its own. cid-gidmap
	// (registry 110) writes this same descendant alloc-aware and runs first,
	// so reading the document here discards its /CIDToGIDMap repair.
	var cidFont ast.PdfDict
	if cidRef.ObjectNum > 0 {
		cidFont = resolveFromAllocOrDoc(ctx, alloc, doc, cidRef)
	} else if d, derr := document.ResolveDict(ctx, doc, descArr[0]); derr == nil {
		cidFont = d
	}
	if cidFont == nil {
		return nil
	}

	fdObj, ok := cidFont["FontDescriptor"]
	if !ok {
		return nil
	}
	var fdRef ast.PdfIndirectRef
	if ref, ok := fdObj.(ast.PdfIndirectRef); ok {
		fdRef = ref
	}
	fdDict, err := document.ResolveDict(ctx, doc, fdObj)
	if err != nil || fdDict == nil {
		return nil
	}

	fontData := extractFontProgram(ctx, doc, fdDict)
	if fontData == nil {
		return nil
	}

	// CID-keyed widths: /W is CID-indexed and CID-keyed CFF subsets
	// renumber GIDs, so the GID-keyed map would repair the wrong entries.
	binaryWidths := typography.ExtractCIDBinaryWidths(ctx, fontData)
	if len(binaryWidths) == 0 {
		return nil
	}
	upm := typography.ReadUnitsPerEm(fontData)
	if upm == 0 {
		upm = 1000
	}

	var fixes []string
	baseName := fontBaseName(fontDict)

	// /W and /DW are both repaired onto ONE copy of the CIDFont: persisting
	// each separately would start from the original dictionary again and the
	// second write would discard the first.
	newCID := make(ast.PdfDict, len(cidFont))
	maps.Copy(newCID, cidFont)
	var pending []string

	// Fix /W array, only when the validator reported a width mismatch.
	if wObj, ok := cidFont["W"]; ok && fixWidths {
		wArr, err := document.ResolveArray(ctx, doc, wObj)
		if err == nil && wArr != nil {
			if newW, corrected := correctCIDWidths(ctx, doc, wArr, binaryWidths, upm); corrected {
				newCID["W"] = newW
				pending = append(pending, fmt.Sprintf("corrected /W widths for CIDFont %s", baseName))
			}
		}
	}

	// Fix /DW, the width every CID outside /W is drawn with, including the
	// .notdef the font renders for an unmapped code (ISO 32000-1, 9.7.4.3).
	// The /W repair above never reaches it, so 6-2-11-4-1-t02-fail-e stayed
	// reporting ".notdef glyph width mismatch (declared=1000.0, font=545.4)"
	// on every pass with nothing fixed.
	if fixWidths {
		if dw, corrected := correctDefaultCIDWidth(ctx, doc, cidFont, fontData, upm); corrected {
			newCID["DW"] = dw
			pending = append(pending, fmt.Sprintf("corrected /DW for CIDFont %s", baseName))
		}
	}

	if len(pending) > 0 && persistCIDFont(alloc, fontDict, fontRef, newCID, cidRef, descArr) {
		fixes = append(fixes, pending...)
	}

	// Fix /CIDSet if incomplete, or supply the one ISO 19005-1, 6.3.5 t3
	// requires of a rendered CIDFont subset. Regeneration stays gated on the key
	// already being present: the conditional rules (6.2.11.4.2, 7.21.4.2) say
	// only that a PRESENT /CIDSet must identify every CID in the program, so
	// writing one uninvited would answer a rule nobody reported.
	_, hasCIDSet := fdDict["CIDSet"]
	if (hasCIDSet && regenCIDSet) || (!hasCIDSet && createCIDSet) {
		if fixed := fixCIDSet(ctx, doc, alloc, fdDict, fdRef, fontData); fixed {
			verb := "regenerated"
			if !hasCIDSet {
				verb = "added"
			}
			fixes = append(fixes, fmt.Sprintf("%s /CIDSet for font %s", verb, baseName))
		}
	}

	// Generate /ToUnicode if missing or invalid. The mapping below keys
	// destinations by GID and emits them as character codes, which is only
	// true when the code IS the GID: the encoding must be Identity-H/V
	// (code = CID, ISO 32000-1, 9.7.5.2) and the CIDFont must not carry a
	// /CIDToGIDMap stream (CID = GID, 9.7.4.2). A stream states a mapping
	// that is deliberately not the identity, so assuming otherwise emits a
	// destination belonging to a different glyph: ArialMT came back as
	// "qe^kk" instead of "THANN".
	if needsToUnicode(ctx, doc, fontDict) && identityCIDRoute(ctx, doc, fontDict, cidFont) {
		gidToUnicode := typography.ExtractGIDToUnicodeAuthoritative(ctx, fontData)
		if len(gidToUnicode) > 0 {
			mapping := make(map[uint16]rune)
			for gid, r := range gidToUnicode {
				if isValidUnicodeMapping(r) {
					mapping[gid] = r
				}
			}
			// See the simple-font path: without an object number to write the
			// font dictionary at, the CMap has nothing to attach to.
			if cmapData := generateToUnicodeCMap(mapping, 2); cmapData != nil && fontRef.ObjectNum > 0 {
				cmapStream := ast.NewStream(ast.PdfDict{}, cmapData)
				cmapRef := alloc.Alloc(cmapStream)
				// Use the latest version of fontDict if already modified.
				targetFont := fontDict
				if allocated, ok := alloc.Pending()[fontRef.ObjectNum]; ok {
					if d, ok := allocated.(ast.PdfDict); ok {
						targetFont = d
					}
				}
				newFont := make(ast.PdfDict, len(targetFont)+1)
				maps.Copy(newFont, targetFont)
				newFont["ToUnicode"] = cmapRef
				alloc.AllocAt(fontRef.ObjectNum, newFont)
				fixes = append(fixes, fmt.Sprintf("generated /ToUnicode CMap for font %s", baseName))
			}
		}
	}

	return fixes
}

// persistCIDWidths writes the corrected /W array at the innermost enclosing
// object that exists in the file, and reports whether anything was written.
//
// Per ISO 32000-1, Table 121 /DescendantFonts shall hold a reference to the
// CIDFont dictionary, but producers inline that dictionary, and a direct
// dictionary has no object number to write at. Writing only when the CIDFont
// itself was indirect meant the corrected widths were computed, dropped, and
// still reported as repaired: remediating issue18298.pdf corrected the same
// three CIDFonts on every pass while the width mismatch stayed in the output.
//
// The corrected array is stored directly inside the copied CIDFont rather than
// at the /W reference, so a /W array shared by several fonts is not rewritten
// on behalf of one of them.
func persistCIDFont(alloc *writer.ObjAllocator, fontDict ast.PdfDict, fontRef ast.PdfIndirectRef, newCID ast.PdfDict, cidRef ast.PdfIndirectRef, descArr ast.PdfArray) bool {
	if alloc.AllocAt(cidRef.ObjectNum, newCID) {
		return true
	}

	newArr := make(ast.PdfArray, len(descArr))
	copy(newArr, descArr)
	newArr[0] = newCID
	if dfRef, ok := fontDict["DescendantFonts"].(ast.PdfIndirectRef); ok && alloc.AllocAt(dfRef.ObjectNum, newArr) {
		return true
	}

	newFont := make(ast.PdfDict, len(fontDict))
	maps.Copy(newFont, fontDict)
	newFont["DescendantFonts"] = newArr
	return alloc.AllocAt(fontRef.ObjectNum, newFont)
}

// correctDefaultCIDWidth returns the /DW a CIDFont should declare and whether
// it differs from the one it has.
//
// /DW is the width of every CID with no /W entry, which is exactly the set a
// viewer draws as .notdef, so it must agree with the program's glyph 0 advance
// (ISO 19005-2, 6.2.11.5). The GID-keyed extractor is the one the detector uses
// for this value; the CID-keyed map the /W repair needs does not answer it.
func correctDefaultCIDWidth(ctx context.Context, doc *document.Document, cidFont ast.PdfDict, fontData []byte, upm uint16) (ast.PdfReal, bool) {
	gidWidths := typography.ExtractBinaryWidths(ctx, fontData)
	notdef, ok := gidWidths[0]
	if !ok {
		return 0, false
	}
	expected := notdef * 1000.0 / float64(upm)

	// An absent or zero /DW means the 1000 default of Table 117.
	declared := 1000.0
	if resolved, err := document.ResolveObject(ctx, doc, cidFont["DW"]); err == nil {
		if w := document.Float(resolved); w != 0 {
			declared = w
		}
	}
	if math.Abs(declared-expected) <= 1.0 {
		return 0, false
	}
	return ast.PdfReal(math.Round(expected*100) / 100), true
}

func correctCIDWidths(ctx context.Context, doc *document.Document, wArr ast.PdfArray, binaryWidths map[uint16]float64, upm uint16) (ast.PdfArray, bool) {
	modified := false
	newW := make(ast.PdfArray, 0, len(wArr))

	i := 0
	for i < len(wArr) {
		if i+1 >= len(wArr) {
			newW = append(newW, wArr[i:]...)
			break
		}

		startCID := int(document.Float(wArr[i]))
		next := wArr[i+1]

		if nextArr, err := document.ResolveArray(ctx, doc, next); err == nil && nextArr != nil {
			corrected := make(ast.PdfArray, len(nextArr))
			for j, wObj := range nextArr {
				pdfWidth := document.Float(wObj)
				cid := uint16(startCID + j) //nolint:gosec // CID range bounded
				if bw, ok := binaryWidths[cid]; ok {
					expected := bw * 1000.0 / float64(upm)
					if math.Abs(pdfWidth-expected) > 1.0 {
						corrected[j] = ast.PdfReal(math.Round(expected*100) / 100)
						modified = true
						continue
					}
				}
				corrected[j] = wObj
			}
			newW = append(newW, wArr[i], corrected)
			i += 2
		} else if i+2 < len(wArr) {
			newW = append(newW, wArr[i], wArr[i+1], wArr[i+2])
			i += 3
		} else {
			newW = append(newW, wArr[i:]...)
			break
		}
	}

	return newW, modified
}

func fixCharSet(ctx context.Context, alloc *writer.ObjAllocator, fdDict ast.PdfDict, fdRef ast.PdfIndirectRef, fontData []byte) bool {
	glyphNames := typography.ExtractGlyphNames(ctx, fontData)
	if len(glyphNames) == 0 {
		return false
	}

	// [typography.ExtractGlyphNames] is keyed by glyph ID, and ranging a map
	// emits the names in a different order on every run. /CharSet is written
	// into the output, so that order IS the output bytes: the same input
	// produced "/A/B" and "/B/A" on consecutive remediations, breaking the
	// reproducible-build contract. Glyph ID order is the font's own.
	gids := make([]uint16, 0, len(glyphNames))
	for gid := range glyphNames {
		gids = append(gids, gid)
	}
	slices.Sort(gids)

	names := make([]string, 0, len(glyphNames))
	for _, gid := range gids {
		if name := glyphNames[gid]; name != "" && name != ".notdef" {
			names = append(names, "/"+name)
		}
	}
	if len(names) == 0 {
		return false
	}

	charSet := strings.Join(names, "")
	// A repair that rewrites a value already equal to itself is not a repair:
	// remediation reported a fix, wrote a fresh revision, and remediating the
	// result grew the file again on every pass.
	if existing, ok := fdDict["CharSet"].(ast.PdfString); ok && string(existing) == charSet {
		return false
	}

	newFD := make(ast.PdfDict, len(fdDict))
	maps.Copy(newFD, fdDict)
	newFD["CharSet"] = ast.PdfString(charSet)
	// A direct font descriptor has no object number, so nothing can be written
	// and the caller must not report a repair absent from the output.
	return alloc.AllocAt(fdRef.ObjectNum, newFD)
}

func fixCIDSet(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, fdDict ast.PdfDict, fdRef ast.PdfIndirectRef, fontData []byte) bool {
	if err := ctx.Err(); err != nil {
		return false
	}
	// Match what the CHECK requires, or the regenerated set is rejected the
	// moment it is written. For a TrueType program under an Identity
	// /CIDToGIDMap the rule is every GID in 1..numGlyphs-1
	// (ISO 19005-2, 6.2.11.4.2 t2), not merely the CIDs with outlines:
	// compliance.checkCIDSetCompletenessTrueType walks that whole range.
	// Writing only the outlined CIDs left "CID 1 in font program not in
	// /CIDSet" on bug911034.pdf as soon as the /CIDToGIDMap fixer supplied
	// the Identity key that activates the rule.
	validCIDs := typography.ExtractValidCIDs(fontData)
	if numGlyphs := typography.ExtractNumGlyphs(fontData); numGlyphs > 0 {
		validCIDs = make(map[uint16]bool, numGlyphs)
		for gid := 1; gid < numGlyphs; gid++ {
			validCIDs[uint16(gid)] = true //nolint:gosec // gid < numGlyphs, a uint16 from maxp.
		}
	}
	if len(validCIDs) == 0 {
		return false
	}

	maxCID := uint16(0)
	for cid := range validCIDs {
		if cid > maxCID {
			maxCID = cid
		}
	}

	byteCount := int(maxCID)/8 + 1
	cidSet := make([]byte, byteCount)
	for cid := range validCIDs {
		byteIdx := int(cid) / 8
		bitIdx := 7 - (int(cid) % 8)
		if byteIdx < len(cidSet) {
			cidSet[byteIdx] |= 1 << uint(bitIdx)
		}
	}

	// Rewriting a /CIDSet already equal to the computed one is not a repair:
	// it reports a fix and appends a revision, so remediating the result grew
	// the file again on every pass.
	if existing, ok := fdDict["CIDSet"]; ok {
		if stream, err := document.ResolveStream(ctx, doc, existing); err == nil && stream.Dict != nil {
			if data, derr := doc.DecodeStream(ctx, stream); derr == nil && bytes.Equal(data, cidSet) {
				return false
			}
		}
	}

	// A direct font descriptor has no object number, so the updated descriptor
	// cannot be written. Allocating the set first would emit a stream nothing
	// references and report a repair absent from the output.
	if fdRef.ObjectNum <= 0 {
		return false
	}

	newStream := ast.NewStream(ast.PdfDict{}, cidSet)
	cidSetRef := alloc.Alloc(newStream)

	newFD := make(ast.PdfDict, len(fdDict))
	maps.Copy(newFD, fdDict)
	newFD["CIDSet"] = cidSetRef
	return alloc.AllocAt(fdRef.ObjectNum, newFD)
}

func extractFontProgram(ctx context.Context, doc *document.Document, fdDict ast.PdfDict) []byte {
	for _, key := range []ast.PdfName{"FontFile2", "FontFile3", "FontFile"} {
		obj, ok := fdDict[key]
		if !ok {
			continue
		}
		stream, err := document.ResolveStream(ctx, doc, obj)
		if err != nil {
			continue
		}
		data, err := doc.DecodeStream(ctx, stream)
		if err != nil {
			continue
		}
		return data
	}
	return nil
}

func fontBaseName(fontDict ast.PdfDict) string {
	if bn, ok := fontDict["BaseFont"].(ast.PdfName); ok {
		return string(bn)
	}
	return "unknown"
}

// needsToUnicode returns true if the font dict has no /ToUnicode entry or
// the existing ToUnicode CMap contains forbidden mappings (U+0000, U+FEFF,
// U+FFFE) that violate PDF/A requirements.
func needsToUnicode(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	tuObj, ok := fontDict["ToUnicode"]
	if !ok {
		return true
	}
	// If an existing ToUnicode is present, check for forbidden values.
	stream, err := document.ResolveStream(ctx, doc, tuObj)
	if err != nil {
		return true
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		// CMap unreadable: don't trigger regeneration. A regenerated
		// CMap is built from the font's encoding tables and may not
		// match the document's intended glyph-to-Unicode mapping;
		// preserving the existing one keeps any user-supplied custom
		// mapping intact. The validator will still flag the file if
		// it is truly non-compliant.
		doc.Logger.Debug("remediation: ToUnicode CMap decode failed, leaving as-is",
			slog.Any("err", err))
		return false
	}
	return cmapHasForbiddenDestination(data)
}

// cmapHasForbiddenDestination reports whether any bfchar or bfrange
// destination in a ToUnicode CMap maps to U+0000, U+FEFF, or U+FFFE
// (forbidden by ISO 19005-2 §6.2.11.7.2 and ISO 19005-4 §6.2.10.7). Only
// destination operands are inspected: a previous whole-stream substring
// scan also matched the "<0000> <FFFF>" codespacerange declaration,
// classifying nearly every valid CMap as broken and replacing it with a
// regenerated guess.
func cmapHasForbiddenDestination(data []byte) bool {
	for _, block := range cmapOperatorBlocks(data, "bfchar") {
		for line := range strings.Lines(block) {
			toks := cmapHexTokens(line)
			// <srcCode> <dstString>; the source operand may legally be
			// <0000> for a two-byte code.
			if slices.ContainsFunc(toksAfter(toks, 1), utf16HexHasForbidden) {
				return true
			}
		}
	}
	for _, block := range cmapOperatorBlocks(data, "bfrange") {
		for line := range strings.Lines(block) {
			toks := cmapHexTokens(line)
			// <lo> <hi> <dst> or <lo> <hi> [<dst> <dst> ...].
			if slices.ContainsFunc(toksAfter(toks, 2), utf16HexHasForbidden) {
				return true
			}
		}
	}
	return false
}

// cmapOperatorBlocks returns the text between each beginKIND/endKIND pair.
func cmapOperatorBlocks(data []byte, kind string) []string {
	var out []string
	s := string(data)
	begin, end := "begin"+kind, "end"+kind
	for {
		i := strings.Index(s, begin)
		if i < 0 {
			break
		}
		rest := s[i+len(begin):]
		before, after, ok := strings.Cut(rest, end)
		if !ok {
			break
		}
		out = append(out, before)
		s = after
	}
	return out
}

// cmapHexTokens extracts the contents of every <...> hex string on a line.
func cmapHexTokens(line string) []string {
	var out []string
	for {
		i := strings.IndexByte(line, '<')
		if i < 0 {
			break
		}
		j := strings.IndexByte(line[i:], '>')
		if j < 0 {
			break
		}
		out = append(out, line[i+1:i+j])
		line = line[i+j+1:]
	}
	return out
}

// toksAfter returns toks[n:] or nil when fewer than n+1 tokens exist.
func toksAfter(toks []string, n int) []string {
	if len(toks) <= n {
		return nil
	}
	return toks[n:]
}

// utf16HexHasForbidden reports whether a UTF-16BE hex destination contains
// a 0x0000, 0xFEFF, or 0xFFFE code unit. Surrogate pairs never produce
// these units, so a flat 16-bit scan is exact.
func utf16HexHasForbidden(hexStr string) bool {
	hexStr = strings.TrimSpace(hexStr)
	for i := 0; i+4 <= len(hexStr); i += 4 {
		switch strings.ToUpper(hexStr[i : i+4]) {
		case "0000", "FEFF", "FFFE":
			return true
		}
	}
	return false
}

// generateToUnicodeCMap builds a minimal ToUnicode CMap stream from a
// charcode-to-Unicode mapping. The CMap maps 2-byte character codes to
// Unicode code points using bfchar entries.
// isValidUnicodeMapping returns true if r is a valid Unicode code point for
// a ToUnicode CMap entry. Excludes U+0000, BOM markers, and Private Use
// Area ranges (PUA mappings trigger clause 6.2.11.7.3 violations).
func isValidUnicodeMapping(r rune) bool {
	if r <= 0 || r == 0xFEFF || r == 0xFFFE {
		return false
	}
	// A /ToUnicode entry states what a glyph MEANS as text (ISO 32000-1,
	// 9.10.3), and a control character is not text. Accepting them let a
	// font whose GID table is an identity map produce <01> -> U+0001 for
	// every code, and since extraction prefers /ToUnicode over the font's
	// own encoding, remediation replaced readable text with control bytes.
	// An empty mapping writes no /ToUnicode at all, which leaves the
	// original, working extraction path in place.
	if r <= 0x1F || (r >= 0x7F && r <= 0x9F) {
		return false
	}
	// Surrogate halves are not characters and cannot be encoded in the
	// UTF-16BE values a bfchar destination carries.
	if r >= 0xD800 && r <= 0xDFFF {
		return false
	}
	// Noncharacters: U+FDD0-U+FDEF and the last two of every plane.
	if (r >= 0xFDD0 && r <= 0xFDEF) || r&0xFFFE == 0xFFFE {
		return false
	}
	// BMP PUA: U+E000–U+F8FF.
	if r >= 0xE000 && r <= 0xF8FF {
		return false
	}
	// Supplementary PUA-A: U+F0000–U+FFFFD.
	if r >= 0xF0000 && r <= 0xFFFFD {
		return false
	}
	// Supplementary PUA-B: U+100000–U+10FFFD.
	if r >= 0x100000 && r <= 0x10FFFD {
		return false
	}
	return true
}

func generateToUnicodeCMap(mapping map[uint16]rune, codeBytes int) []byte {
	// Sort keys for deterministic output. The codespacerange and bfchar
	// source codes must share one width: simple fonts address glyphs with
	// single-byte codes, and a two-byte codespace would misalign every
	// lookup a conforming reader performs against the page's one-byte
	// strings.
	keys := make([]uint16, 0, len(mapping))
	for k := range mapping {
		if codeBytes == 1 && k > 0xFF {
			continue
		}
		keys = append(keys, k)
	}
	if len(keys) == 0 {
		return nil
	}
	slices.Sort(keys)

	var buf strings.Builder
	buf.WriteString("/CIDInit /ProcSet findresource begin\n")
	buf.WriteString("12 dict begin\n")
	buf.WriteString("begincmap\n")
	buf.WriteString("/CIDSystemInfo <</Registry (Adobe) /Ordering (UCS) /Supplement 0>> def\n")
	buf.WriteString("/CMapName /Adobe-Identity-UCS def\n")
	buf.WriteString("/CMapType 2 def\n")
	buf.WriteString("1 begincodespacerange\n")
	if codeBytes == 1 {
		buf.WriteString("<00> <FF>\n")
	} else {
		buf.WriteString("<0000> <FFFF>\n")
	}
	buf.WriteString("endcodespacerange\n")

	writeCode := func(code uint16) {
		if codeBytes == 1 {
			fmt.Fprintf(&buf, "<%02X>", code)
		} else {
			fmt.Fprintf(&buf, "<%04X>", code)
		}
	}

	// Write bfchar entries in chunks of 100 (CMap spec limit).
	for i := 0; i < len(keys); i += 100 {
		end := min(i+100, len(keys))
		chunk := keys[i:end]
		fmt.Fprintf(&buf, "%d beginbfchar\n", len(chunk))
		for _, code := range chunk {
			r := mapping[code]
			writeCode(code)
			if r <= 0xFFFF {
				fmt.Fprintf(&buf, " <%04X>\n", uint16(r)) //nolint:gosec // rune fits in uint16 per guard
			} else {
				// Supplementary plane: encode as UTF-16 surrogate pair.
				hi := uint16(0xD800 + (uint32(r-0x10000) >> 10))   //nolint:gosec // surrogate arithmetic
				lo := uint16(0xDC00 + (uint32(r-0x10000) & 0x3FF)) //nolint:gosec // surrogate arithmetic
				fmt.Fprintf(&buf, " <%04X%04X>\n", hi, lo)
			}
		}
		buf.WriteString("endbfchar\n")
	}

	buf.WriteString("endcmap\n")
	buf.WriteString("CMapName currentdict /CMap defineresource pop\n")
	buf.WriteString("end end\n") //nolint:dupword // PostScript: one `end` closes the CMap dict, the other the resource category (ISO 32000-1, 9.7.5.4).
	return []byte(buf.String())
}

// identityCIDRoute reports whether a Type 0 font's character codes reach glyphs
// by the identity route, so a GID-keyed Unicode table can be emitted verbatim
// as a /ToUnicode CMap.
//
// Two links must both be the identity: the /Encoding CMap taking a code to a
// CID (Identity-H or Identity-V, ISO 32000-1, 9.7.5.2), and /CIDToGIDMap taking
// a CID to a GID (absent or /Identity, 9.7.4.2). A predefined CMap or a
// /CIDToGIDMap stream breaks the chain, and there is no trustworthy mapping to
// write.
func identityCIDRoute(ctx context.Context, doc *document.Document, fontDict, cidFont ast.PdfDict) bool {
	encName, _ := document.ResolveName(ctx, doc, fontDict["Encoding"])
	if encName != "Identity-H" && encName != "Identity-V" {
		return false
	}
	mapObj, ok := cidFont["CIDToGIDMap"]
	if !ok {
		return true // Absent means Identity.
	}
	if name, err := document.ResolveName(ctx, doc, mapObj); err == nil && name == "Identity" {
		return true
	}
	return false
}

// overlaySimpleFontEncoding replaces destinations in a pending /ToUnicode
// mapping with the ones the font dictionary's /Encoding states.
//
// [typography.ResolveFont] already resolves a base encoding and its
// /Differences into code-to-rune form, so the encoding tables are not
// reproduced here. A symbolic font resolves to no encoding at all, and its
// codes keep whatever the font program answered.
// fontIsSymbolic reports whether a font's descriptor sets the symbolic flag,
// bit 3 of /Flags (ISO 32000-1, Table 123).
func fontIsSymbolic(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	fdDict, err := document.ResolveDict(ctx, doc, fontDict["FontDescriptor"])
	if err != nil || fdDict == nil {
		return false
	}
	flags, err := document.ResolveInt(ctx, doc, fdDict["Flags"])
	if err != nil {
		return false
	}
	const symbolicFlag = 1 << 2
	return int(flags)&symbolicFlag != 0
}

// fixSimpleFontToUnicode generates a /ToUnicode CMap for a simple font that
// lacks a usable one, returning what it repaired.
//
// It runs independently of the width and /CharSet repairs because it shares
// none of their preconditions: the mapping comes from the font's /Encoding,
// not from a /Widths array, a /FontDescriptor, or the program's metrics. While
// it lived inside fixSimpleFont, any one of those early returns cancelled it,
// so every Type 1 font kept its missing Unicode mapping.
//
// shownCodes is the set of character codes the page actually shows. The CMap is
// attached only when it maps all of them: ISO 19005-2, 6.2.11.7.2 asks for the
// USED codes, and an /Encoding-derived mapping need not reach every one. A
// partial map satisfies litho's own presence check while veraPDF still reports
// the rule, which reports a repair the output does not contain.
func fixSimpleFontToUnicode(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, fontDict ast.PdfDict, fontRef ast.PdfIndirectRef, shownCodes map[byte]bool) []string {
	if !needsToUnicode(ctx, doc, fontDict) || fontRef.ObjectNum == 0 || len(shownCodes) == 0 {
		return nil
	}

	// A symbolic font that states no /Encoding is drawn through its built-in
	// encoding, and ISO 32000-1, 9.6.6.1 says a conforming reader shall not
	// substitute a standard one for it. Deriving Unicode from a standard
	// encoding there invents meanings: the TeX math fonts of
	// 6-3-8-t01-fail-a are symbolic with no /Encoding, and the mapping turned
	// "−∞ ∞" into "−1 1" in extracted text while satisfying the rule. An
	// explicit /Encoding, even on a symbolic font, is the producer's own
	// statement and is trusted.
	if _, stated := fontDict["Encoding"]; !stated && fontIsSymbolic(ctx, doc, fontDict) {
		return nil
	}

	mapping := make(map[uint16]rune)

	// The program route needs a cmap table, which a Type 1 or bare CFF program
	// does not have. Its absence is not a reason to emit nothing: the
	// /Encoding alone states what the codes mean.
	if fdDict, err := document.ResolveDict(ctx, doc, fontDict["FontDescriptor"]); err == nil && fdDict != nil {
		if fontData := extractFontProgram(ctx, doc, fdDict); fontData != nil {
			if charToGID := typography.ExtractCharCodeToGID(ctx, fontData); charToGID != nil {
				gidToUnicode := typography.ExtractGIDToUnicodeAuthoritative(ctx, fontData)
				for charCode, gid := range charToGID {
					if r, ok := gidToUnicode[gid]; ok && isValidUnicodeMapping(r) {
						mapping[charCode] = r
					}
				}
			}
		}
	}

	// What a code MEANS in a simple font is stated by its /Encoding, through
	// glyph names (ISO 32000-1, 9.6.6); the font program's cmap only says which
	// glyph gets drawn. The two disagree whenever the two encodings do: under
	// /MacRomanEncoding code 0xD5 is quoteright, and taking the program's
	// answer wrote U+00D5, turning every ’ in a 470 KB document into Õ at
	// exactly the same length.
	overlaySimpleFontEncoding(ctx, doc, fontDict, mapping)

	for code := range shownCodes {
		if _, ok := mapping[uint16(code)]; !ok {
			return nil
		}
	}

	// Simple fonts address glyphs with one-byte codes.
	cmapData := generateToUnicodeCMap(mapping, 1)
	if cmapData == nil {
		return nil
	}
	cmapRef := alloc.Alloc(ast.NewStream(ast.PdfDict{}, cmapData))

	// Build on the latest version of the font dictionary: an earlier repair in
	// this same pass may already have rewritten it.
	targetFont := fontDict
	if allocated, ok := alloc.Pending()[fontRef.ObjectNum]; ok {
		if d, ok := allocated.(ast.PdfDict); ok {
			targetFont = d
		}
	}
	newFont := make(ast.PdfDict, len(targetFont)+1)
	maps.Copy(newFont, targetFont)
	newFont["ToUnicode"] = cmapRef
	if !alloc.AllocAt(fontRef.ObjectNum, newFont) {
		return nil
	}
	return []string{fmt.Sprintf("generated /ToUnicode CMap for font %s", fontBaseName(fontDict))}
}

func overlaySimpleFontEncoding(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, mapping map[uint16]rune) {
	font, err := typography.ResolveFont(ctx, doc, fontDict)
	if err != nil || font == nil {
		doc.Logger.Debug("resolving font for /ToUnicode encoding overlay", slog.Any("err", err))
		return
	}
	for code, r := range font.Encoding {
		if isValidUnicodeMapping(r) {
			mapping[uint16(code)] = r
		}
	}
}
