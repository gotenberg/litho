package compliance

import (
	"context"
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/typography"
	"github.com/gotenberg/litho/pkg/internal/typography/cmap"
)

// widthTolerance is the maximum allowed difference between PDF-declared
// and binary font widths, in 1/1000 text space units.
const widthTolerance = 1.0

// checkGlyphWidths validates glyph width consistency (rule 5).
// Compares PDF-declared widths against the embedded font program's binary widths.
func checkGlyphWidths(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, usage *fontUsageInfo) []Violation {
	rule := glyphWidthRule(profile)
	if rule.Specification == "" {
		return nil
	}

	// Type3 fonts: compare /Widths against CharProc d0/d1 widths.
	if isType3Font(ctx, doc, fontDict) {
		return checkType3FontWidths(ctx, doc, fontDict, fontName, pageNum, rule)
	}

	binaryWidths, unitsPerEm, charCodeToGID, ok := simpleFontGlyphMetrics(ctx, doc, fontDict)
	if !ok {
		return nil
	}

	subtype := fontSubtype(ctx, doc, fontDict)

	// Simple fonts (Type1, TrueType, MMType1): compare /Widths array using FirstChar offset.
	if subtype == "Type1" || subtype == "TrueType" || subtype == "MMType1" {
		return checkSimpleFontWidths(ctx, doc, fontDict, binaryWidths, unitsPerEm, fontName, pageNum, rule, charCodeToGID, usage)
	}

	return nil
}

// simpleFontGlyphMetrics extracts the embedded font program's per-GID advance
// widths, its units-per-em, and a char-code to GID map for a simple font.
// Returns ok=false when the font program is absent or unparseable. Shared by the
// width-consistency check and the [ExpectedSimpleFontWidths] repair helper so the
// two always agree on the authoritative widths.
func simpleFontGlyphMetrics(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) (map[uint16]uint16, uint16, map[uint16]uint16, bool) {
	fd := document.ResolveFontDescriptor(ctx, doc, fontDict)
	if fd == nil {
		return nil, 0, nil, false
	}

	fontData := extractFontProgramData(ctx, doc, fd)
	if fontData == nil {
		return nil, 0, nil, false
	}

	binaryWidths := typography.ExtractBinaryWidths(ctx, fontData)
	if binaryWidths == nil {
		return nil, 0, nil, false
	}

	unitsPerEm := typography.ReadUnitsPerEm(fontData)
	if unitsPerEm == 0 {
		unitsPerEm = 1000
	}

	// Extract charCode → GID mapping from font's cmap table (TrueType/OTTO).
	charCodeToGID := typography.ExtractCharCodeToGID(ctx, fontData)

	// For fonts without a cmap (Type1/CFF), build charCode→GID via glyph names:
	// PDF encoding maps charCode→glyphName; font program maps GID→glyphName.
	if charCodeToGID == nil {
		glyphNames := typography.ExtractGlyphNames(ctx, fontData)
		if glyphNames != nil {
			enc := resolveSimpleFontEncoding(ctx, doc, fontDict)
			if len(enc) > 0 {
				nameToGID := make(map[string]uint16, len(glyphNames))
				for gid, name := range glyphNames {
					if _, exists := nameToGID[name]; !exists {
						nameToGID[name] = gid
					}
				}
				charCodeToGID = make(map[uint16]uint16)
				for code, name := range enc {
					if name == ".notdef" || name == "" {
						continue
					}
					if gid, ok := nameToGID[name]; ok {
						charCodeToGID[uint16(code)] = gid
					} else {
						// Glyph name not in font charset → maps to GID 0 (.notdef).
						// Include it so width comparison can detect the mismatch.
						charCodeToGID[uint16(code)] = 0
					}
				}
			}
		}
	}

	return binaryWidths, unitsPerEm, charCodeToGID, true
}

// checkSimpleFontWidths compares /Widths array against binary widths for simple fonts.
// charCodeToGID is the cmap-derived charCode → GID mapping (may be nil for non-TrueType).
func checkSimpleFontWidths(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, binaryWidths map[uint16]uint16, unitsPerEm uint16, fontName string, pageNum int, rule Rule, charCodeToGID map[uint16]uint16, usage *fontUsageInfo) []Violation {
	// veraPDF's width test runs per USED glyph and passes whenever the
	// dictionary width is unresolvable: the test is `renderingMode == 3 ||
	// widthFromFontProgram == null || widthFromDictionary == null ||
	// |diff| <= 1`, so a missing /Widths or /FirstChar never fails this
	// rule (it fails the font-dict key rules instead), and a font used
	// only invisibly is exempt.
	if usage == nil || len(usage.charCodes) == 0 || !usage.visible {
		return nil
	}

	widthsObj, hasWidths := fontDict["Widths"]
	if !hasWidths {
		return nil
	}

	widths, err := document.ResolveArray(ctx, doc, widthsObj)
	if err != nil {
		doc.Logger.Debug("compliance: fonts: cannot resolve /Widths", "font", fontName, "error", err)
		return nil
	}

	firstChar, err := document.ResolveInt(ctx, doc, fontDict["FirstChar"])
	if err != nil {
		return nil
	}

	// veraPDF builds a width Glyph for code 0 only when a text operator
	// actually shows it; an unrendered NUL in /Widths is never tested
	// (6-3-8-t01-fail-a passes the width rule despite /Widths[0]). When code 0
	// IS rendered, the rule compares the resolved glyph's program advance (the
	// .notdef glyph's advance when the code maps to no glyph) against the
	// declared width: /Widths[0-FirstChar] when code 0 is in range, else
	// /MissingWidth (default 0). Verified live on 6-2-11-8-t01-fail-a/b, where
	// the shown <00> maps to .notdef (advance 632/750) but declares width 0.
	if usage.charCodes[0] {
		if v := checkSimpleFontCode0Width(ctx, doc, fontDict, binaryWidths, unitsPerEm, fontName, pageNum, rule, charCodeToGID, int(firstChar), widths); v != nil {
			return v
		}
	}

	for i, wObj := range widths {
		charCode := int(firstChar) + i
		if charCode < 0 || charCode > 255 {
			continue
		}
		gid := uint16(charCode) //nolint:gosec
		if charCodeToGID != nil {
			mappedGID, ok := charCodeToGID[uint16(charCode)] //nolint:gosec
			if !ok {
				continue
			}
			gid = mappedGID
		}
		// Width consistency applies only to glyphs referenced for
		// rendering: skip charCodes not used in text operators.
		charUsed := usage.charCodes[byte(charCode)] //nolint:gosec
		if !charUsed {
			continue
		}
		// veraPDF's NUL handling: a 0x00 charCode never appears among its
		// usedGlyphs (PDF_A-1a__veraPDF_test_suite_6-3-8-t01-fail-a
		// passes PDFA-1B entirely despite /Widths[0]=826 on an absent
		// glyph), so skip it here regardless of the GID mapping.
		if charCode == 0 {
			continue
		}
		pdfWidth := document.Float(wObj)
		// A width outside the 32-bit integer range cannot be a real
		// metric: it is crafted overflow input (implementation-limits
		// fixtures write -2157483648). ISO 32000-2 §7.3.3 leaves
		// out-of-range integer handling unspecified, and veraPDF treats
		// such values as unparseable rather than mismatching, so skip
		// the comparison instead of flagging it.
		if pdfWidth < math.MinInt32 || pdfWidth > math.MaxInt32 {
			continue
		}
		// Absent glyphs resolve to GID 0: veraPDF's widthFromFontProgram
		// then carries the .notdef glyph's own advance width, and the
		// width rule fires ALONGSIDE glyph presence when the declared
		// width disagrees (verified live on
		// PDF_A-2b__veraPDF_test_suite_6-2-11-4-1-t02-fail-a, where both
		// 6.2.11.5 t1 and 6.2.11.4.1 t2 cite the same usedGlyph).
		bw, ok := binaryWidths[gid]
		if !ok {
			continue
		}
		expectedWidth := float64(bw) * 1000.0 / float64(unitsPerEm)
		if math.Abs(pdfWidth-expectedWidth) > widthTolerance {
			return failf(rule, "font %s on page %d: char %d width mismatch (PDF=%.1f, font=%.1f)",
				fontName, pageNum, charCode, pdfWidth, expectedWidth)
		}
	}

	return nil
}

// checkSimpleFontCode0Width applies the glyph-width rule to a rendered code 0.
// The main /Widths loop skips code 0 (it often falls below /FirstChar and so is
// absent from the array), but veraPDF still tests it when shown: it resolves the
// glyph the same way as any code (charCode → GID, falling back to GID 0 /.notdef
// when the code maps to no glyph name) and compares the program advance against
// the declared width. The declared width is /Widths[0-FirstChar] when code 0 is
// covered by the array, otherwise /MissingWidth (default 0).
func checkSimpleFontCode0Width(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, binaryWidths map[uint16]uint16, unitsPerEm uint16, fontName string, pageNum int, rule Rule, charCodeToGID map[uint16]uint16, firstChar int, widths []ast.PdfObject) []Violation {
	// Symbolic fonts (FontDescriptor /Flags bit 3) map code 0 through the
	// embedded program's built-in encoding, which litho does not reconstruct
	// for CFF; the GID-0 fallback below would then read the wrong glyph and
	// misfire (CMSY8 code 0 resolves to a real glyph veraPDF accepts). Restrict
	// the code-0 check to non-symbolic fonts, where the /Encoding name path and
	// the .notdef fallback match veraPDF (6-2-11-8-t01-fail-a/b are Flags 32).
	if isSymbolicFont(ctx, doc, document.ResolveFontDescriptor(ctx, doc, fontDict)) {
		return nil
	}

	// veraPDF resolves code 0 to GID 0 (.notdef) when no glyph name maps it,
	// then reads that glyph's own advance from the program.
	gid := uint16(0)
	if charCodeToGID != nil {
		if mapped, ok := charCodeToGID[0]; ok {
			gid = mapped
		}
	}
	bw, ok := binaryWidths[gid]
	if !ok {
		// No program advance for the resolved glyph: veraPDF treats
		// widthFromFontProgram as null and the rule passes.
		return nil
	}

	// Declared width: /Widths[0-FirstChar] when code 0 is covered by the array,
	// else /MissingWidth (default 0, ISO 32000-1 Table 111).
	pdfWidth := simpleFontMissingWidth(ctx, doc, fontDict)
	if firstChar <= 0 && -firstChar < len(widths) {
		pdfWidth = document.Float(widths[-firstChar])
	}
	if pdfWidth < math.MinInt32 || pdfWidth > math.MaxInt32 {
		return nil
	}

	expectedWidth := float64(bw) * 1000.0 / float64(unitsPerEm)
	if math.Abs(pdfWidth-expectedWidth) > widthTolerance {
		return failf(rule, "font %s on page %d: char 0 width mismatch (PDF=%.1f, font=%.1f)",
			fontName, pageNum, pdfWidth, expectedWidth)
	}
	return nil
}

// simpleFontMissingWidth returns the FontDescriptor /MissingWidth for a simple
// font, the width veraPDF uses for a code outside the /Widths array. Defaults to
// 0 when absent (ISO 32000-1 Table 111).
func simpleFontMissingWidth(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) float64 {
	fd := document.ResolveFontDescriptor(ctx, doc, fontDict)
	if fd == nil {
		return 0
	}
	mw, _ := document.ResolveObject(ctx, doc, fd["MissingWidth"])
	return document.Float(mw)
}

// glyphPresenceRule selects the per-part "embedded font program defines all
// glyphs referenced for rendering" rule: ISO 19005-1 §6.3.5 t1, ISO 19005-2/3
// §6.2.11.4.1 t2, ISO 19005-4 §6.2.10.4.1 t2. Both the PDFA-1A and PDFA-1B
// veraPDF profiles carry §6.3.5 t1 (test `isGlyphPresent`); clause 6.3.8 t1 is
// the unrelated ToUnicode requirement (`toUnicode != null`).
func glyphPresenceRule(profile string) Rule {
	// PDF/UA ids verified live on PDF_A-1b__veraPDF_test_suite
	// _6-1-12-t10-fail-a remediated to UA-1/UA-2: 7.21.4.1 t2 and
	// 8.4.5.5.1 t2 fire alongside the notdef and toUnicode rules.
	if IsPDFUA(profile) {
		if PDFUAPart(profile) == 2 {
			return Rule_14289_2_8_4_5_5_1_t02
		}
		return Rule_14289_1_7_21_4_1_t02
	}
	return ruleByPart(PDFAPart(profile), Rule_19005_1_6_3_5_t01, Rule_19005_2_6_2_11_4_1_t02, Rule_19005_3_6_2_11_4_1_t02, Rule_19005_4_6_2_10_4_1_t02)
}

// checkSimpleGlyphPresence validates that every glyph referenced for visible
// rendering exists in the embedded font program (veraPDF usedGlyph test
// `renderingMode == 3 || isGlyphPresent == null || isGlyphPresent == true`).
// A used charCode whose glyph name is absent from the program maps to GID 0
// in simpleFontGlyphMetrics; that is the "glyph not present" signal. Codes
// litho cannot map at all stay unchecked (isGlyphPresent == null passes).
func checkSimpleGlyphPresence(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, usage *fontUsageInfo) []Violation {
	rule := glyphPresenceRule(profile)
	if rule.Specification == "" {
		return nil
	}
	if usage == nil || len(usage.charCodes) == 0 || !usage.visible {
		return nil
	}
	_, _, charCodeToGID, ok := simpleFontGlyphMetrics(ctx, doc, fontDict)
	if !ok || charCodeToGID == nil {
		return nil
	}
	for code := range usage.charCodes {
		gid, mapped := charCodeToGID[uint16(code)]
		if mapped && gid == 0 {
			return failf(rule, "font %s on page %d: glyph for charCode %d not present in embedded font program", fontName, pageNum, code)
		}
	}
	return nil
}

// ExpectedCIDFontWidths returns each CID's correct width (in 1000-unit glyph
// space) for a CIDFont, computed from the embedded font program. The map is
// keyed by CID, which the width check treats as equal to the glyph index
// (the Identity CIDToGIDMap assumption); a CID absent from the map has no glyph
// in the program and should keep its declared width. Returns ok=false when the
// font program is absent or unparseable. Remediation uses this to repair §6.2.11.5
// width mismatches in the /W array, sharing the same metrics extraction as the
// width check so the two stay consistent.
func ExpectedCIDFontWidths(ctx context.Context, doc *document.Document, cidFontDict ast.PdfDict) (map[int]int, bool) {
	fd := document.ResolveFontDescriptor(ctx, doc, cidFontDict)
	if fd == nil {
		return nil, false
	}
	fontData := extractFontProgramData(ctx, doc, fd)
	if fontData == nil {
		return nil, false
	}
	// CID-keyed map: shares typography.ExtractCIDBinaryWidths with the
	// width check so detector and repair agree on the authoritative widths
	// (CID-keyed CFF subsets renumber GIDs; /W stays CID-indexed).
	binaryWidths := typography.ExtractCIDBinaryWidths(ctx, fontData)
	if binaryWidths == nil {
		return nil, false
	}
	unitsPerEm := typography.ReadUnitsPerEm(fontData)
	if unitsPerEm == 0 {
		unitsPerEm = 1000
	}

	out := make(map[int]int, len(binaryWidths))
	for cid, bw := range binaryWidths {
		out[int(cid)] = int(math.Round(float64(bw) * 1000.0 / float64(unitsPerEm)))
	}
	return out, true
}

// checkType3FontWidths compares /Widths against glyph widths declared by
// d0/d1 operators in Type3 font CharProcs.
func checkType3FontWidths(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, rule Rule) []Violation {
	widthsObj, hasWidths := fontDict["Widths"]
	if !hasWidths {
		return nil
	}
	widths, err := document.ResolveArray(ctx, doc, widthsObj)
	if err != nil || len(widths) == 0 {
		return nil
	}
	firstChar, err := document.ResolveInt(ctx, doc, fontDict["FirstChar"])
	if err != nil {
		doc.Logger.Debug("compliance: cannot resolve FirstChar", "error", err)
		return nil
	}

	// Build charCode→glyphName from /Encoding /Differences.
	enc := resolveSimpleFontEncoding(ctx, doc, fontDict)

	// Resolve CharProcs dict.
	charProcs, err := document.ResolveDict(ctx, doc, fontDict["CharProcs"])
	if err != nil || charProcs == nil {
		return nil
	}

	// Extract FontMatrix scale factor. Type3 FontMatrix is typically
	// [0.001 0 0 0.001 0 0], meaning glyph coordinates are in 1/1000 units
	// and widths from d0/d1 are in glyph space. The PDF /Widths values are
	// in text space (1/1000 of user space unit). Scale = FontMatrix[0] * 1000.
	fmScale := 1.0
	if fmArr, err := document.ResolveArray(ctx, doc, fontDict["FontMatrix"]); err == nil && len(fmArr) >= 1 {
		sx := document.Float(fmArr[0])
		if sx > 0 {
			fmScale = sx * 1000.0
		}
	}

	// Extract widths from each CharProc by finding d0/d1 operator.
	charProcWidths := make(map[string]float64)
	for glyphName, cpObj := range charProcs {
		stream, err := document.ResolveStream(ctx, doc, cpObj)
		if err != nil {
			doc.Logger.Debug("compliance: cannot resolve CharProc stream", "error", err)
			continue
		}
		streamData, err := doc.DecodeStream(ctx, stream)
		if err != nil {
			doc.Logger.Debug("compliance: cannot decode CharProc stream", "error", err)
			continue
		}
		ops, err := content.ParseStreamData(ctx, streamData)
		if err != nil {
			doc.Logger.Debug("compliance: cannot parse CharProc stream", "error", err)
			continue
		}
		for _, op := range ops {
			// d1: wx wy llx lly urx ury d1
			// d0: wx wy d0
			if (op.Operator == "d1" && len(op.Operands) >= 6) ||
				(op.Operator == "d0" && len(op.Operands) >= 2) {
				wx := document.Float(op.Operands[0])
				charProcWidths[string(glyphName)] = wx
				break
			}
		}
	}

	// Compare /Widths against CharProc widths.
	for i, wObj := range widths {
		charCode := byte(int(firstChar) + i) //nolint:gosec
		glyphName := enc[charCode]
		if glyphName == "" {
			continue
		}
		cpWidth, ok := charProcWidths[glyphName]
		if !ok {
			continue
		}
		pdfWidth := document.Float(wObj)
		expectedWidth := cpWidth * fmScale
		if math.Abs(pdfWidth-expectedWidth) > widthTolerance {
			return failf(rule, "font %s on page %d: char %d (%s) width mismatch (PDF=%.1f, glyph=%.1f)",
				fontName, pageNum, int(firstChar)+i, glyphName, pdfWidth, expectedWidth)
		}
	}
	return nil
}

// checkCIDFontWidths compares /W array against binary widths for CIDFonts.
func checkCIDFontWidths(ctx context.Context, doc *document.Document, cidFontDict ast.PdfDict, binaryWidths map[uint16]uint16, unitsPerEm uint16, fontName string, pageNum int, rule Rule) []Violation {
	wObj, ok := cidFontDict["W"]
	if !ok {
		return nil
	}

	wArr, err := document.ResolveArray(ctx, doc, wObj)
	if err != nil {
		doc.Logger.Debug("compliance: fonts: resolving CID width array", "font", fontName, "error", err)
		return nil
	}
	if wArr == nil {
		return nil
	}

	// /W array format: [cid [w1 w2 ...]] or [cidFirst cidLast w]
	i := 0
	for i < len(wArr) {
		cidStartRes, err := document.ResolveObject(ctx, doc, wArr[i])
		if err != nil {
			doc.Logger.Debug("compliance: fonts: resolving CID start value", "error", err)
		}
		cidStartInt, ok := cidStartRes.(ast.PdfInt)
		if !ok {
			i++
			continue
		}
		cidStart := int(cidStartInt)
		i++
		if i >= len(wArr) {
			break
		}

		nextRes, err := document.ResolveObject(ctx, doc, wArr[i])
		if err != nil {
			doc.Logger.Debug("compliance: fonts: resolving CID width entry", "error", err)
		}
		switch next := nextRes.(type) {
		case ast.PdfArray:
			// [cidStart [w1 w2 w3 ...]]
			for j, wEntry := range next {
				cid := cidStart + j
				// Skip .notdef (CID 0), no visible outlines.
				if cid == 0 {
					continue
				}
				gid := uint16(cid) //nolint:gosec
				pdfWidth := document.Float(wEntry)
				if pdfWidth == 0 {
					continue
				}
				// Out-of-int32-range widths are crafted overflow input,
				// not metrics; see checkSimpleFontWidths.
				if pdfWidth < math.MinInt32 || pdfWidth > math.MaxInt32 {
					continue
				}
				bw, ok := binaryWidths[gid]
				if !ok {
					continue
				}
				expectedWidth := float64(bw) * 1000.0 / float64(unitsPerEm)
				if math.Abs(pdfWidth-expectedWidth) > widthTolerance {
					return failf(rule, "font %s on page %d: CID %d width mismatch (PDF=%.1f, font=%.1f)",
						fontName, pageNum, cid, pdfWidth, expectedWidth)
				}
			}
			i++
		case ast.PdfInt:
			// [cidStart cidEnd w]
			cidEnd := int(next)
			i++
			if i >= len(wArr) {
				break
			}
			pdfWidth := document.Float(wArr[i])
			i++
			if pdfWidth == 0 {
				continue
			}
			// Out-of-int32-range widths are crafted overflow input,
			// not metrics; see checkSimpleFontWidths.
			if pdfWidth < math.MinInt32 || pdfWidth > math.MaxInt32 {
				continue
			}
			for cid := cidStart; cid <= cidEnd; cid++ {
				// Skip .notdef (CID 0), no visible outlines.
				if cid == 0 {
					continue
				}
				gid := uint16(cid) //nolint:gosec
				bw, ok := binaryWidths[gid]
				if !ok {
					continue
				}
				expectedWidth := float64(bw) * 1000.0 / float64(unitsPerEm)
				if math.Abs(pdfWidth-expectedWidth) > widthTolerance {
					return failf(rule, "font %s on page %d: CID %d width mismatch (PDF=%.1f, font=%.1f)",
						fontName, pageNum, cid, pdfWidth, expectedWidth)
				}
			}
		default:
			i++
		}
	}

	return nil
}

// extractFontProgramData gets the raw font program bytes from FontDescriptor.
func extractFontProgramData(ctx context.Context, doc *document.Document, fdDict ast.PdfDict) []byte {
	for _, key := range []ast.PdfName{"FontFile2", "FontFile3", "FontFile"} {
		ffObj, ok := fdDict[key]
		if !ok {
			continue
		}
		stream, err := document.ResolveStream(ctx, doc, ffObj)
		if err != nil {
			doc.Logger.Debug("compliance: fonts: resolving font program stream", "key", key, "error", err)
		}
		if stream.Dict == nil {
			continue
		}
		data, err := doc.DecodeStream(ctx, stream)
		if err != nil {
			// Fall back to raw data.
			rawData, rawErr := stream.Bytes(ctx)
			if rawErr == nil && len(rawData) > 0 {
				return rawData
			}
			doc.Logger.Debug("compliance: fonts: failed to decode font program stream", "key", key, "error", err)
			continue
		}
		return data
	}
	return nil
}

// checkType0PerGlyphToUnicode validates that every code rendered through a
// Type0 font resolves to a Unicode value via the font's /ToUnicode CMap
// (veraPDF usedGlyph test `toUnicode != null`, verified live on
// 6-1-12-t10-fail-a under UA-1/UA-2: 7.21.7 t1 / 8.4.5.8 t1 fire alongside
// the presence and notdef rules). A missing /ToUnicode entirely is the
// font-level rule's job; this check covers a present map with gaps.
func checkType0PerGlyphToUnicode(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, usage *fontUsageInfo) []Violation {
	rule := perGlyphToUnicodeRule(profile)
	if rule.Specification == "" {
		return nil
	}
	if usage == nil || len(usage.rawStrings) == 0 || !usage.visible {
		return nil
	}
	tuStream, err := document.ResolveStream(ctx, doc, fontDict["ToUnicode"])
	if err != nil {
		return nil
	}
	data, err := doc.DecodeStream(ctx, tuStream)
	if err != nil {
		return nil
	}
	tu, err := cmap.Parse(ctx, data)
	if err != nil || tu == nil || len(tu.Mapping) == 0 {
		return nil
	}

	// veraPDF does not fire the per-glyph ToUnicode rule on a code that does not
	// resolve to a real renderable glyph: CID 0 is .notdef (covered by the
	// notdef rule), and a CIDFontType0 (CFF) charset-absent CID gets no glyph
	// object at all. Both are exempt. The exemption is CFF-specific for charset
	// gaps: a CIDFontType2 (TrueType) empty glyph still gets a glyph object and
	// still fires the rule, so the absent set is restricted to CFF descendants.
	// For Identity-H/V an unmapped two-byte code is the CID directly, so skip it
	// without disturbing the codespace-aware scan that handles mixed-width maps.
	encName, _ := document.ResolveName(ctx, doc, fontDict["Encoding"])
	identity := encName == "Identity-H" || encName == "Identity-V"
	var cffAbsent map[uint16]bool
	cff := false
	if identity {
		if absent, subtype, _ := absentType0CIDs(ctx, doc, fontDict, usage); subtype == "CIDFontType0" {
			cffAbsent = absent
			cff = true
		}
	}

	for _, s := range usage.rawStrings {
		b := []byte(s)
		for off := 0; off < len(b); {
			consumed, _, ok := tu.Map(b[off:])
			if consumed <= 0 {
				consumed = 1
			}
			if !ok {
				// The exemption is CFF-specific. For a CIDFontType0 descendant
				// veraPDF builds no Glyph object for CID 0 (.notdef) or a
				// charset-absent CID, so it never fires the rule there. A
				// CIDFontType2 (TrueType) CID 0 maps to GID 0, a real glyph slot
				// that still gets a Glyph object and still fires the rule.
				skip := false
				if cff && consumed == 2 && off+1 < len(b) {
					cid := uint16(b[off])<<8 | uint16(b[off+1])
					skip = cid == 0 || cffAbsent[cid]
				}
				if !skip {
					return failf(rule, "font %s on page %d: rendered code has no ToUnicode mapping", fontName, pageNum)
				}
			}
			off += consumed
		}
	}
	return nil
}

// ExpectedSimpleFontWidths returns the authoritative width (in 1/1000 text
// space units) for each charCode of a simple font, using exactly the
// resolution the width check applies: the font program's metrics through
// the cmap or glyph-name mapping, with codes that map to GID 0 carrying the
// .notdef glyph's own advance width. Codes litho cannot map are absent from
// the map and must keep their declared width (veraPDF treats them as
// unresolvable). Remediation uses this so the repair and the check can
// never drift.
func ExpectedSimpleFontWidths(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) (map[int]float64, bool) {
	binaryWidths, unitsPerEm, charCodeToGID, ok := simpleFontGlyphMetrics(ctx, doc, fontDict)
	if !ok || charCodeToGID == nil {
		return nil, false
	}
	out := make(map[int]float64, len(charCodeToGID))
	for code, gid := range charCodeToGID {
		bw, ok := binaryWidths[gid]
		if !ok {
			continue
		}
		out[int(code)] = float64(bw) * 1000.0 / float64(unitsPerEm)
	}
	return out, true
}
