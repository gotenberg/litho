package compliance

import (
	"bytes"
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/typography"
)

// fontUsageInfo tracks how a font is used in a content stream.
type fontUsageInfo struct {
	visible    bool            // true if used with rendering mode != 3
	charCodes  map[byte]bool   // character codes referenced by Tj/TJ
	rawStrings []ast.PdfString // raw string operands from text operators (for CID .notdef detection)
}

// collectFontUsage returns usage information for each font referenced in the
// page content stream: whether it's used for visible rendering and which
// character codes are referenced. Returns nil if ops is nil or empty (in which
// case all fonts should be checked).
func collectFontUsage(ops []content.Operation) map[string]*fontUsageInfo {
	if len(ops) == 0 {
		return nil
	}

	usage := make(map[string]*fontUsageInfo)
	currentFont := ""
	renderMode := 0 // default rendering mode is 0 (fill)

	getOrCreate := func(name string) *fontUsageInfo {
		if u, ok := usage[name]; ok {
			return u
		}
		u := &fontUsageInfo{charCodes: make(map[byte]bool)}
		usage[name] = u
		return u
	}

	collectStringBytes := func(obj ast.PdfObject) {
		if currentFont == "" {
			return
		}
		u := getOrCreate(currentFont)
		switch s := obj.(type) {
		case ast.PdfString:
			for _, b := range []byte(s) {
				u.charCodes[b] = true
			}
			u.rawStrings = append(u.rawStrings, s)
		case ast.PdfArray:
			for _, item := range s {
				if str, ok := item.(ast.PdfString); ok {
					for _, b := range []byte(str) {
						u.charCodes[b] = true
					}
					u.rawStrings = append(u.rawStrings, str)
				}
			}
		}
	}

	for _, op := range ops {
		switch op.Operator {
		case "Tf":
			if len(op.Operands) >= 1 {
				if name, ok := op.Operands[0].(ast.PdfName); ok {
					currentFont = string(name)
				}
			}
		case "Tr":
			if len(op.Operands) >= 1 {
				if modeInt, ok := op.Operands[0].(ast.PdfInt); ok {
					renderMode = int(modeInt)
				}
			}
		case "Tj":
			if currentFont != "" && renderMode != 3 {
				getOrCreate(currentFont).visible = true
			}
			if len(op.Operands) >= 1 {
				collectStringBytes(op.Operands[0])
			}
		case "TJ":
			if currentFont != "" && renderMode != 3 {
				getOrCreate(currentFont).visible = true
			}
			if len(op.Operands) >= 1 {
				collectStringBytes(op.Operands[0])
			}
		case "'", "\"":
			if currentFont != "" && renderMode != 3 {
				getOrCreate(currentFont).visible = true
			}
			if len(op.Operands) >= 1 {
				collectStringBytes(op.Operands[len(op.Operands)-1])
			}
		}
	}

	return usage
}

// --- Main check group ---

// checkFontGroup validates font-related compliance rules across all pages.
func checkFontGroup(ctx context.Context, vctx *validationCtx) []Violation {
	doc, catalog, profile := vctx.doc, vctx.catalog, vctx.profile

	complog(doc).Debug("compliance: fonts: starting font checks", "profile", profile)

	if len(vctx.pages) == 0 {
		return nil
	}

	var results []Violation

	// Track fonts already checked to avoid duplicate violations.
	checked := make(map[string]bool)

	for pageIdx := range vctx.pages {
		p := &vctx.pages[pageIdx]
		page := p.dict
		if ctx.Err() != nil {
			return results
		}

		resources := p.resources
		if resources == nil {
			continue
		}

		fontDictObj, ok := resources["Font"]
		if !ok {
			continue
		}

		fontDict, err := document.ResolveDict(ctx, doc, fontDictObj)
		if err != nil {
			complog(doc).Debug("compliance: fonts: failed to resolve /Font dict",
				"page", pageIdx+1, "error", err)
			continue
		}

		// Collect font usage from the page content stream: which fonts are
		// visible (rendering mode != 3) and which character codes are used.
		fontUsage := collectFontUsage(p.parsedOps(ctx))

		for fontName, fontRef := range fontDict {
			if ctx.Err() != nil {
				return results
			}

			fontKey := string(fontName)
			if checked[fontKey] {
				continue
			}

			font, err := document.ResolveDict(ctx, doc, fontRef)
			if err != nil || font == nil {
				complog(doc).Debug("compliance: fonts: failed to resolve font dict",
					"font", fontKey, "page", pageIdx+1, "error", err)
				continue
			}

			checked[fontKey] = true
			// Skip width/embedding checks for fonts not used for visible rendering.
			var usage *fontUsageInfo
			if fontUsage != nil {
				usage = fontUsage[fontKey]
			}
			isVisible := fontUsage == nil || (usage != nil && usage.visible)

			results = append(results, checkSingleFont(ctx, doc, font, fontKey, pageIdx+1, profile, isVisible, usage)...)

			// PUA ActualText check (needs page and catalog context).
			results = append(results, checkPUAWithoutActualText(ctx, doc, font, fontKey, pageIdx+1, profile, page, catalog)...)
		}
	}

	complog(doc).Debug("compliance: fonts: font checks complete",
		"violations", len(results), "fonts_checked", len(checked))
	return results
}

// checkSingleFont runs all font checks against a single font dictionary.
// If isVisible is false, width consistency and embedding checks are skipped
// (the font is not used for visible rendering). Usage provides per-charCode
// usage information for ToUnicode exemption checks.
func checkSingleFont(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, isVisible bool, usage *fontUsageInfo) []Violation {
	var results []Violation

	subtype := fontSubtype(ctx, doc, fontDict)

	// Type 3 fonts have no font program — skip embedding checks.
	isType3 := isType3Font(ctx, doc, fontDict)

	// Type 0 (composite) fonts: check descendant CIDFonts.
	if subtype == "Type0" {
		results = append(results, checkType0Font(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)
		return results
	}

	// Font embedding — only for fonts used for visible rendering.
	if isVisible {
		results = append(results, checkFontEmbedding(ctx, doc, fontDict, fontName, pageNum, profile, isType3)...)
	}

	// ToUnicode mapping.
	results = append(results, checkToUnicode(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)

	// TrueType encoding.
	if subtype == "TrueType" {
		results = append(results, checkTrueTypeEncoding(ctx, doc, fontDict, fontName, pageNum, profile)...)
	}

	// Font name UTF-8.
	results = append(results, checkFontNameUTF8(ctx, doc, fontDict, fontName, pageNum, profile)...)

	// Glyph width consistency — only for fonts used for visible rendering.
	if isVisible {
		results = append(results, checkGlyphWidths(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)
	}

	// .notdef glyph.
	results = append(results, checkNotdefGlyph(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)

	// Glyph coverage (CharSet for Type1 subsets).
	results = append(results, checkGlyphCoverage(ctx, doc, fontDict, fontName, pageNum, profile)...)

	// ToUnicode CMap values.
	results = append(results, checkToUnicodeCMapValues(ctx, doc, fontDict, fontName, pageNum, profile)...)

	return results
}

// checkPUAWithoutActualText flags fonts with PUA ToUnicode mappings when
// neither the structure tree nor the page content stream has any ActualText.
// This is a conservative check: it only fires when there is zero ActualText
// anywhere, avoiding false positives from ActualText that covers a different
// span than the PUA character.
func checkPUAWithoutActualText(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, page ast.PdfDict, catalog ast.PdfDict) []Violation {
	rule := puaActualTextRule(profile)
	if rule.Specification == "" {
		return nil
	}

	tuObj, ok := fontDict["ToUnicode"]
	if !ok {
		return nil
	}
	stream, err := document.ResolveStream(ctx, doc, tuObj)
	if err != nil {
		complog(doc).Debug("compliance: cannot resolve ToUnicode stream", "error", err)
		return nil
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil || !containsPUAMapping(string(data)) {
		return nil
	}

	// PUA mapping exists. Check for ActualText in structure tree.
	treeRoot, _ := document.ResolveDict(ctx, doc, catalog["StructTreeRoot"])
	if treeRoot != nil && structTreeHasActualText(ctx, doc, treeRoot, 0) {
		return nil
	}

	// Check for ActualText in content stream (BDC properties).
	contentData, _ := getPageContentBytes(ctx, doc, page)
	if len(contentData) > 0 && bytes.Contains(contentData, []byte("ActualText")) {
		return nil
	}

	return failf(rule, "font %s on page %d: ToUnicode maps to PUA without ActualText", fontName, pageNum)
}

// structTreeHasActualText recursively checks if any structure element has /ActualText.
func structTreeHasActualText(ctx context.Context, doc *document.Document, node ast.PdfDict, depth int) bool {
	if depth > 30 {
		return false
	}
	if _, ok := node["ActualText"]; ok {
		return true
	}
	kObj := node["K"]
	if kObj == nil {
		return false
	}
	if arr, _ := document.ResolveArray(ctx, doc, kObj); len(arr) > 0 {
		for _, kid := range arr {
			kidDict, _ := document.ResolveDict(ctx, doc, kid)
			if kidDict != nil && structTreeHasActualText(ctx, doc, kidDict, depth+1) {
				return true
			}
		}
	} else if kidDict, _ := document.ResolveDict(ctx, doc, kObj); kidDict != nil {
		return structTreeHasActualText(ctx, doc, kidDict, depth+1)
	}
	return false
}

// --- Type 0 (composite) font checks ---

// checkType0Font validates composite font properties including descendant
// CIDFonts, CIDSystemInfo, CIDToGIDMap, and ToUnicode.
func checkType0Font(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, usage *fontUsageInfo) []Violation {
	results := make([]Violation, 0, 4)

	// Resolve /DescendantFonts array.
	descArr, err := document.ResolveArray(ctx, doc, fontDict["DescendantFonts"])
	if err != nil || len(descArr) == 0 {
		complog(doc).Debug("compliance: fonts: Type0 font has no DescendantFonts",
			"font", fontName, "error", err)
		// Without descendants we cannot check further, but the font is still
		// subject to embedding checks at the Type0 level.
		return results
	}

	// PDF spec requires exactly one descendant CIDFont.
	cidFontDict, err := document.ResolveDict(ctx, doc, descArr[0])
	if err != nil || cidFontDict == nil {
		complog(doc).Debug("compliance: fonts: failed to resolve descendant CIDFont",
			"font", fontName, "error", err)
		return results
	}

	cidSubtype := fontSubtype(ctx, doc, cidFontDict)

	// Font embedding for the CIDFont.
	results = append(results, checkFontEmbedding(ctx, doc, cidFontDict, fontName, pageNum, profile, false)...)

	// Font name UTF-8 for the Type0 font.
	results = append(results, checkFontNameUTF8(ctx, doc, fontDict, fontName, pageNum, profile)...)

	// CIDToGIDMap.
	results = append(results, checkCIDToGIDMap(ctx, doc, cidFontDict, cidSubtype, fontName, pageNum, profile)...)

	// CIDSystemInfo consistency.
	results = append(results, checkCIDSystemInfo(ctx, doc, fontDict, cidFontDict, fontName, pageNum, profile)...)

	// ToUnicode for composite fonts.
	results = append(results, checkType0ToUnicode(ctx, doc, fontDict, cidFontDict, fontName, pageNum, profile)...)

	// CMap embedding and WMode.
	results = append(results, checkCMapEmbedding(ctx, doc, fontDict, fontName, pageNum, profile)...)

	// CID value limit (t10: max CID 65535).
	results = append(results, checkCMapCIDValues(ctx, doc, fontDict, fontName, pageNum, profile)...)

	// .notdef glyph.
	results = append(results, checkNotdefGlyph(ctx, doc, fontDict, fontName, pageNum, profile, nil)...)
	results = append(results, checkType0NotdefGlyph(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)

	// Glyph width consistency for CIDFont.
	cidFd := findFontDescriptor(ctx, doc, cidFontDict)
	if cidFd != nil {
		fontData := extractFontProgramData(ctx, doc, cidFd)
		if fontData != nil {
			binaryWidths := typography.ExtractBinaryWidths(ctx, fontData)
			if binaryWidths != nil {
				unitsPerEm := typography.ReadUnitsPerEm(fontData)
				if unitsPerEm == 0 {
					unitsPerEm = 1000
				}
				rule := glyphWidthRule(profile)
				if rule.Specification != "" {
					results = append(results, checkCIDFontWidths(ctx, doc, cidFontDict, binaryWidths, unitsPerEm, fontName, pageNum, rule)...)
				}
			}
		}
	}

	// CIDFont glyph coverage (CIDSet for CIDFont subsets).
	results = append(results, checkCIDFontGlyphCoverage(ctx, doc, cidFontDict, fontName, pageNum, profile)...)

	// ToUnicode CMap values.
	results = append(results, checkToUnicodeCMapValues(ctx, doc, fontDict, fontName, pageNum, profile)...)

	return results
}

// checkNotdefGlyph checks for .notdef glyph references. A character code that
// maps to .notdef (GID 0 or glyph name ".notdef") is a violation.
//
// For simple fonts (Type1/TrueType): resolves encoding and checks each used
// character code. For TrueType with embedded programs: also checks the font's
// cmap for GID 0 mappings.
func checkNotdefGlyph(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, usage *fontUsageInfo) []Violation {
	rule := notdefRule(profile)
	if rule.Specification == "" {
		return nil
	}

	// Check /Encoding /Differences for explicit ".notdef" entries.
	if r := checkNotdefInDifferences(ctx, doc, fontDict, fontName, pageNum, rule); r != nil {
		return r
	}

	if usage == nil || len(usage.charCodes) == 0 {
		return nil
	}

	subtype := fontSubtype(ctx, doc, fontDict)

	// For Type1/CFF: if the encoding explicitly maps a used charCode to
	// ".notdef", it's a violation. For charCodes below 0x20 (control range)
	// with no encoding entry, the glyph is .notdef since standard encodings
	// don't define glyphs for control characters. For charCodes >= 0x20,
	// absence of explicit encoding doesn't imply .notdef (standard encoding
	// provides defaults for printable ASCII).
	if subtype == "Type1" || subtype == "MMType1" {
		enc := resolveSimpleFontEncoding(ctx, doc, fontDict)
		if len(enc) > 0 {
			for code := range usage.charCodes {
				glyphName := enc[code]
				if glyphName == ".notdef" {
					return failf(rule, "font %s on page %d: charCode %d maps to .notdef", fontName, pageNum, code)
				}
				if glyphName == "" && code < 0x20 {
					return failf(rule, "font %s on page %d: charCode %d maps to .notdef", fontName, pageNum, code)
				}
			}
		}
	}

	// For TrueType: compose PDF encoding → glyph name → GID via post table.
	// If post table absent, use cmap directly for charCode 0 (always .notdef).
	if subtype == "TrueType" {
		fd := findFontDescriptor(ctx, doc, fontDict)
		if fd != nil {
			fontData := extractFontProgramData(ctx, doc, fd)
			if fontData != nil {
				glyphNames := typography.ExtractGlyphNames(ctx, fontData)
				if glyphNames != nil {
					// Build glyphName→GID reverse map.
					nameToGID := make(map[string]uint16, len(glyphNames))
					for gid, gn := range glyphNames {
						if _, ok := nameToGID[gn]; !ok {
							nameToGID[gn] = gid
						}
					}
					enc := resolveSimpleFontEncoding(ctx, doc, fontDict)
					for code := range usage.charCodes {
						glyphName := enc[code]
						if glyphName == "" || glyphName == ".notdef" {
							return failf(rule, "font %s on page %d: charCode %d maps to .notdef", fontName, pageNum, code)
						}
						if gid, ok := nameToGID[glyphName]; ok && gid == 0 {
							return failf(rule, "font %s on page %d: charCode %d maps to GID 0 (.notdef)", fontName, pageNum, code)
						}
					}
				} else {
					// No post table glyph names — use cmap + loca + /Widths.
					charToGID := typography.ExtractCharCodeToGID(ctx, fontData)
					if charToGID != nil {
						// GID 0 is always .notdef.
						if usage.charCodes[0] && charToGID[0] == 0 {
							return failf(rule, "font %s on page %d: charCode 0 maps to GID 0 (.notdef)", fontName, pageNum)
						}
						// For non-zero GIDs: empty glyph data + PDF width=0 → .notdef.
						emptyGIDs := typography.ExtractEmptyGIDs(ctx, fontData)
						if emptyGIDs != nil {
							firstChar, widths := simpleFontWidths(ctx, doc, fontDict)
							for code := range usage.charCodes {
								gid, inCmap := charToGID[uint16(code)]
								if !inCmap {
									continue // No cmap entry — can't determine GID.
								}
								if gid == 0 {
									return failf(rule, "font %s on page %d: charCode %d maps to GID 0 (.notdef)", fontName, pageNum, code)
								}
								if emptyGIDs[gid] && simpleFontWidthIsZero(firstChar, widths, code) {
									return failf(rule, "font %s on page %d: charCode %d maps to empty glyph with width 0 (.notdef)", fontName, pageNum, code)
								}
							}
						}
					}
				}
			}
		}
	}

	return nil
}

// checkType0NotdefGlyph checks if a Type0 (composite) font references CID 0
// (.notdef) in any text operator string, or references a CID that is absent
// from the embedded font program's charset (also renders as .notdef).
// For Identity-H/Identity-V encoded fonts, scans raw string bytes for 2-byte CIDs.
func checkType0NotdefGlyph(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, usage *fontUsageInfo) []Violation {
	rule := notdefRule(profile)
	if rule.Specification == "" || usage == nil || len(usage.rawStrings) == 0 {
		return nil
	}

	// Only handle Identity-H / Identity-V encoding (2-byte CIDs).
	encName, _ := document.ResolveName(ctx, doc, fontDict["Encoding"])
	if encName != "Identity-H" && encName != "Identity-V" {
		return nil
	}

	// Collect all referenced CIDs from raw strings.
	usedCIDs := make(map[uint16]bool)
	hasOddLengthString := false
	for _, s := range usage.rawStrings {
		data := []byte(s)
		if len(data)%2 != 0 {
			hasOddLengthString = true
		}
		for i := 0; i+1 < len(data); i += 2 {
			cid := uint16(data[i])<<8 | uint16(data[i+1])
			usedCIDs[cid] = true
		}
	}

	// An odd-length string with a 2-byte CMap (Identity-H/V) means the
	// last byte cannot form a valid CID — the renderer uses .notdef.
	if hasOddLengthString {
		return failf(rule, "font %s on page %d: odd-length string with 2-byte CMap references .notdef", fontName, pageNum)
	}

	// CID 0 is always .notdef.
	if usedCIDs[0] {
		return failf(rule, "font %s on page %d: text references CID 0 (.notdef)", fontName, pageNum)
	}

	// Resolve descendant CIDFont to verify referenced CIDs exist in the font program.
	descArr, err := document.ResolveArray(ctx, doc, fontDict["DescendantFonts"])
	if err != nil || len(descArr) == 0 {
		return nil
	}
	cidFontDict, err := document.ResolveDict(ctx, doc, descArr[0])
	if err != nil || cidFontDict == nil {
		return nil
	}

	fd := findFontDescriptor(ctx, doc, cidFontDict)
	if fd == nil {
		return nil
	}
	fontData := extractFontProgramData(ctx, doc, fd)
	if fontData == nil {
		return nil
	}

	cidSubtype := fontSubtype(ctx, doc, cidFontDict)

	switch cidSubtype {
	case "CIDFontType0":
		// CFF font: check if CIDs exist in the charset.
		validCIDs := typography.ExtractValidCIDs(fontData)
		if validCIDs == nil {
			return nil
		}
		for cid := range usedCIDs {
			if !validCIDs[cid] {
				return failf(rule, "font %s on page %d: CID %d not in font charset (.notdef)", fontName, pageNum, cid)
			}
		}

	case "CIDFontType2":
		// TrueType CIDFont: with CIDToGIDMap=/Identity, CID == GID.
		cidToGIDMap, _ := document.ResolveName(ctx, doc, cidFontDict["CIDToGIDMap"])
		if cidToGIDMap != "Identity" {
			return nil
		}
		// Empty glyph data + PDF width=0 → .notdef.
		// Empty glyph data + PDF width>0 → valid invisible glyph (space, etc.).
		emptyGIDs := typography.ExtractEmptyGIDs(ctx, fontData)
		if emptyGIDs != nil {
			for cid := range usedCIDs {
				if emptyGIDs[cid] && lookupCIDWidth(ctx, doc, cidFontDict, cid) == 0 {
					return failf(rule, "font %s on page %d: CID %d maps to empty glyph with width 0 (.notdef)", fontName, pageNum, cid)
				}
			}
		}
	}

	return nil
}
