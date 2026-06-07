package compliance

import (
	"context"
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/typography"
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

// ExpectedSimpleFontWidths returns the value each /Widths entry should hold for a
// simple font (Type1/TrueType/MMType1), computed from the embedded font program
// and aligned to the font's existing /Widths array: entry i is the width for char
// code FirstChar+i. A value of -1 means the width could not be determined (no
// glyph mapping, or a control code the consistency check ignores) and the
// existing entry should be kept. Returns ok=false when the font is not a
// width-checkable simple font or has no parseable program. Remediation uses this
// to repair §6.2.11.5 / §6.3.6 width mismatches with values the width check
// accepts, keeping checker and fixer in lock-step via [simpleFontGlyphMetrics].
func ExpectedSimpleFontWidths(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) ([]int, bool) {
	subtype := fontSubtype(ctx, doc, fontDict)
	if subtype != "Type1" && subtype != "TrueType" && subtype != "MMType1" {
		return nil, false
	}

	binaryWidths, unitsPerEm, charCodeToGID, ok := simpleFontGlyphMetrics(ctx, doc, fontDict)
	if !ok {
		return nil, false
	}

	widths, err := document.ResolveArray(ctx, doc, fontDict["Widths"])
	if err != nil || len(widths) == 0 {
		return nil, false
	}
	firstChar, err := document.ResolveInt(ctx, doc, fontDict["FirstChar"])
	if err != nil {
		return nil, false
	}

	out := make([]int, len(widths))
	for i := range widths {
		out[i] = -1
		charCode := int(firstChar) + i
		// Mirror checkSimpleFontWidths: control codes below 0x20 are not
		// width-checked, so leave their entries untouched.
		if charCode < 0x20 {
			continue
		}
		// charCode = FirstChar (0-255) + array index, so it fits uint16.
		gid := uint16(charCode) //nolint:gosec // bounded by /Widths length + FirstChar
		if charCodeToGID != nil {
			mapped, ok := charCodeToGID[uint16(charCode)] //nolint:gosec // see above
			if !ok {
				continue
			}
			gid = mapped
		}
		bw, ok := binaryWidths[gid]
		if !ok {
			continue
		}
		out[i] = int(math.Round(float64(bw) * 1000.0 / float64(unitsPerEm)))
	}
	return out, true
}

// checkSimpleFontWidths compares /Widths array against binary widths for simple fonts.
// charCodeToGID is the cmap-derived charCode → GID mapping (may be nil for non-TrueType).
func checkSimpleFontWidths(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, binaryWidths map[uint16]uint16, unitsPerEm uint16, fontName string, pageNum int, rule Rule, charCodeToGID map[uint16]uint16, usage *fontUsageInfo) []Violation {
	widthsObj, hasWidths := fontDict["Widths"]
	if !hasWidths {
		return failf(rule, "font %s on page %d: missing /Widths array", fontName, pageNum)
	}

	widths, err := document.ResolveArray(ctx, doc, widthsObj)
	if err != nil {
		doc.Logger.Debug("compliance: fonts: cannot resolve /Widths", "font", fontName, "error", err)
		return failf(rule, "font %s on page %d: cannot resolve /Widths", fontName, pageNum)
	}

	firstChar, err := document.ResolveInt(ctx, doc, fontDict["FirstChar"])
	if err != nil {
		return failf(rule, "font %s on page %d: missing /FirstChar", fontName, pageNum)
	}

	for i, wObj := range widths {
		charCode := int(firstChar) + i
		if charCode < 0x20 {
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
		// The spec requires width consistency only for "glyphs referenced
		// for rendering." Skip charCodes not used in text operators.
		charUsed := usage != nil && usage.charCodes[byte(charCode)] //nolint:gosec
		pdfWidth := document.Float(wObj)
		if pdfWidth == 0 && !charUsed {
			continue
		}
		// Skip non-used charCodes when glyph is missing from font program
		// (GID 0 from name mapping), only flag if the charCode is used.
		if gid == 0 && !charUsed {
			continue
		}
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
