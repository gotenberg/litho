package compliance

import (
	"bytes"
	"context"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/typography"
	"github.com/gotenberg/litho/pkg/internal/typography/cmap"
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

// checkFontGroup validates font-related compliance rules across all pages.
func checkFontGroup(ctx context.Context, vctx *validationCtx) []Violation {
	doc, catalog, profile := vctx.doc, vctx.catalog, vctx.profile

	doc.Logger.Debug("compliance: fonts: starting font checks", "profile", profile)

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
			doc.Logger.Debug("compliance: fonts: failed to resolve /Font dict",
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
				doc.Logger.Debug("compliance: fonts: failed to resolve font dict",
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

	doc.Logger.Debug("compliance: fonts: font checks complete",
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

	// Required font-dictionary keys (PDFont): §6.3.2 / §6.2.11.2 / §6.2.10.2.
	results = append(results, checkFontRequiredKeys(ctx, doc, fontDict, subtype, PDFAPart(profile))...)

	// Type 3 fonts have no font program, so skip embedding checks.
	isType3 := isType3Font(ctx, doc, fontDict)

	// Type 0 (composite) fonts: check descendant CIDFonts.
	if subtype == "Type0" {
		results = append(results, checkType0Font(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)
		return results
	}

	// Required keys for simple fonts: /FirstChar, /LastChar, /Widths.
	results = append(results, checkSimpleFontRequiredKeys(ctx, doc, fontDict, PDFAPart(profile))...)

	// Font embedding: only for fonts used for visible rendering.
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

	// Glyph width consistency: only for fonts used for visible rendering.
	if isVisible {
		results = append(results, checkGlyphWidths(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)
		// Glyph presence (§6.3.8 / §6.2.11.4.1 t2 / §6.2.10.4.1 t2): every
		// glyph referenced for visible rendering must exist in the program.
		results = append(results, checkSimpleGlyphPresence(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)
	}

	// .notdef glyph.
	results = append(results, checkNotdefGlyph(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)

	// Glyph coverage (CharSet for Type1 subsets).
	results = append(results, checkGlyphCoverage(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)

	// ToUnicode CMap values.
	results = append(results, checkToUnicodeCMapValues(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)

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
		doc.Logger.Debug("compliance: cannot resolve ToUnicode stream", "error", err)
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
	contentData, _ := resolvePageContentBytes(ctx, doc, page)
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

// checkType0Font validates composite font properties including descendant
// CIDFonts, CIDSystemInfo, CIDToGIDMap, and ToUnicode.
func checkType0Font(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, usage *fontUsageInfo) []Violation {
	results := make([]Violation, 0, 4)

	// Resolve /DescendantFonts array.
	descArr, err := document.ResolveArray(ctx, doc, fontDict["DescendantFonts"])
	if err != nil || len(descArr) == 0 {
		doc.Logger.Debug("compliance: fonts: Type0 font has no DescendantFonts",
			"font", fontName, "error", err)
		// Without descendants no further checks are possible, but the font is
		// still subject to embedding checks at the Type0 level.
		return results
	}

	// PDF spec requires exactly one descendant CIDFont.
	cidFontDict, err := document.ResolveDict(ctx, doc, descArr[0])
	if err != nil || cidFontDict == nil {
		doc.Logger.Debug("compliance: fonts: failed to resolve descendant CIDFont",
			"font", fontName, "error", err)
		return results
	}

	cidSubtype := fontSubtype(ctx, doc, cidFontDict)

	// Font embedding for the CIDFont.
	results = append(results, checkFontEmbedding(ctx, doc, cidFontDict, fontName, pageNum, profile, false)...)

	// Font name UTF-8 for the Type0 font.
	results = append(results, checkFontNameUTF8(ctx, doc, fontDict, fontName, pageNum, profile)...)

	// CIDToGIDMap. A font with no recorded usage is treated as visible so the
	// check still runs when content-stream usage could not be collected.
	visible := usage == nil || usage.visible
	results = append(results, checkCIDToGIDMap(ctx, doc, cidFontDict, cidSubtype, fontName, pageNum, profile, visible)...)

	// CIDSystemInfo consistency.
	results = append(results, checkCIDSystemInfo(ctx, doc, fontDict, cidFontDict, fontName, pageNum, profile)...)

	// ToUnicode for composite fonts.
	results = append(results, checkType0ToUnicode(ctx, doc, fontDict, cidFontDict, fontName, pageNum, profile)...)

	// Per-glyph ToUnicode coverage: a present map must cover every code
	// rendered through the font (§6.2.11.7.2 t2 / UA 7.21.7 / 8.4.5.8).
	results = append(results, checkType0PerGlyphToUnicode(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)

	// CMap embedding and WMode.
	results = append(results, checkCMapEmbedding(ctx, doc, fontDict, fontName, pageNum, profile)...)

	// CID value limit (t10: max CID 65535).
	results = append(results, checkCMapCIDValues(ctx, doc, fontDict, fontName, pageNum, profile)...)

	// .notdef glyph.
	results = append(results, checkNotdefGlyph(ctx, doc, fontDict, fontName, pageNum, profile, nil)...)
	results = append(results, checkType0NotdefGlyph(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)
	results = append(results, checkType0GlyphPresence(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)

	// Glyph width consistency for CIDFont. Widths are CID-keyed: CID-keyed
	// CFF subsets renumber GIDs densely while the /W array stays
	// CID-indexed, so a GID-keyed map reads the wrong charstrings.
	cidFd := document.ResolveFontDescriptor(ctx, doc, cidFontDict)
	if cidFd != nil {
		fontData := extractFontProgramData(ctx, doc, cidFd)
		if fontData != nil {
			binaryWidths := typography.ExtractCIDBinaryWidths(ctx, fontData)
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

	// CIDFont glyph coverage (CIDSet for CIDFont subsets). Completeness
	// is scoped to USED CIDs: veraPDF passes a CIDSet that omits unused
	// glyphs (verified live on 6-2-11-4-1-t02-fail-e under PDF/A-2b,
	// where flagging unused CID 4998 was a 30-cell litho-only pool).
	results = append(results, checkCIDFontGlyphCoverage(ctx, doc, cidFontDict, fontName, pageNum, profile, type0UsedCIDs(ctx, doc, fontDict, usage))...)

	// ToUnicode CMap values.
	results = append(results, checkToUnicodeCMapValues(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)

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
		fd := document.ResolveFontDescriptor(ctx, doc, fontDict)
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
					// No post table glyph names, so use cmap + loca + /Widths.
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
									continue // No cmap entry, can't determine GID.
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

// collectType0CIDs decodes a Type0 font's shown strings into the set of CIDs
// they reference. It returns (cids, notdef, ok):
//   - ok is false when litho cannot decode the /Encoding to CIDs (an embedded
//     CMap stream, or an unrecognized predefined CMap); the caller skips the
//     check rather than risk a false positive.
//   - notdef is true when decoding reached a character code that unambiguously
//     renders as the .notdef glyph (an Identity odd-length tail).
//
// Identity-H/V treats each 2-byte unit as the CID directly. A predefined CMap
// (Adobe-Japan1 etc.) is decoded codespace-aware through its bundled bcmap; the
// resolvable CIDs are returned for the caller's embedded-font charset check,
// while unmapped codes are skipped to stay free of false positives.
func collectType0CIDs(encName string, embedded *cmap.CIDMap, usage *fontUsageInfo) (cids map[uint16]bool, notdef, ok bool) {
	// An embedded CMap stream resolves to an empty name; decode through the
	// parsed stream instead so the used-glyph checks cover fonts the
	// veraPDF test suite encodes with custom CMaps (the 6-1-12-t10 family
	// was silent across every profile without this).
	if encName == "" && embedded != nil {
		cids = make(map[uint16]bool)
		for _, s := range usage.rawStrings {
			for _, c := range embedded.Decode([]byte(s)) {
				if !c.Mapped {
					continue
				}
				// A CMap mapping a used code to a CID beyond 65535
				// references a glyph no font can contain (ISO 32000-2
				// limits CIDs to 0..65535): the renderer draws .notdef,
				// which is exactly what the caller's notdef path reports.
				if c.CID < 0 || c.CID > 0xFFFF {
					return nil, true, true
				}
				cids[uint16(c.CID)] = true
			}
		}
		return cids, false, true
	}
	switch encName {
	case "Identity-H", "Identity-V":
		cids = make(map[uint16]bool)
		for _, s := range usage.rawStrings {
			data := []byte(s)
			// An odd-length string under a 2-byte CMap leaves a trailing byte
			// that cannot form a CID; the renderer draws .notdef.
			if len(data)%2 != 0 {
				return nil, true, true
			}
			for i := 0; i+1 < len(data); i += 2 {
				cids[uint16(data[i])<<8|uint16(data[i+1])] = true
			}
		}
		return cids, false, true
	default:
		// Any other named encoding is a predefined CMap, decoded through its
		// bundled bcmap. A nil map means the name is not a recognized
		// predefined CMap (or /Encoding is an embedded stream, which resolves
		// to an empty name here), so the check is skipped.
		//
		// An unmapped code is NOT treated as .notdef here: litho does not
		// resolve a bcmap's usecmap base, so a code could be unmapped merely
		// because its mapping lives in an unresolved base CMap. Flagging it
		// would be a false positive. Only the CIDs we can resolve are collected
		// and verified against the embedded font program by the caller, which
		// is independent of CMap completeness. This trades a possibly missed
		// .notdef (a false negative) for zero false positives.
		cm := cmap.LoadPredefinedCIDMap(encName)
		if cm == nil {
			return nil, false, false
		}
		cids = make(map[uint16]bool)
		for _, s := range usage.rawStrings {
			for _, c := range cm.Decode([]byte(s)) {
				if c.Mapped && c.CID >= 0 && c.CID <= 0xFFFF {
					cids[uint16(c.CID)] = true
				}
			}
		}
		return cids, false, true
	}
}

// checkType0NotdefGlyph checks if a Type0 (composite) font references CID 0
// (.notdef) in any text operator string, or references a CID that is absent
// from the embedded font program's charset (also renders as .notdef). It
// handles Identity-H/V and predefined CMap encodings (see [collectType0CIDs]);
// embedded CMap streams are not decoded and are left unchecked.
// type0UsedCIDs resolves the CIDs a Type0 font's text operators reference,
// through the same /Encoding decoding the notdef check uses (predefined
// CMaps, Identity, or an embedded CMap stream). Returns nil when the
// encoding cannot be decoded or nothing is used: callers treat nil as
// "usage unknown" and skip usage-scoped checks rather than over-fire.
func type0UsedCIDs(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, usage *fontUsageInfo) map[uint16]bool {
	if usage == nil || len(usage.rawStrings) == 0 {
		return nil
	}
	encName, _ := document.ResolveName(ctx, doc, fontDict["Encoding"])
	var embedded *cmap.CIDMap
	if encName == "" {
		if stream, err := document.ResolveStream(ctx, doc, fontDict["Encoding"]); err == nil {
			if data, derr := doc.DecodeStream(ctx, stream); derr == nil {
				embedded = cmap.ParseEmbeddedCIDMap(ctx, data)
			}
		}
	}
	cids, _, ok := collectType0CIDs(string(encName), embedded, usage)
	if !ok || len(cids) == 0 {
		return nil
	}
	return cids
}

func checkType0NotdefGlyph(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, usage *fontUsageInfo) []Violation {
	rule := notdefRule(profile)
	if rule.Specification == "" || usage == nil || len(usage.rawStrings) == 0 {
		return nil
	}

	encName, _ := document.ResolveName(ctx, doc, fontDict["Encoding"])
	var embedded *cmap.CIDMap
	if encName == "" {
		if stream, err := document.ResolveStream(ctx, doc, fontDict["Encoding"]); err == nil {
			if data, derr := doc.DecodeStream(ctx, stream); derr == nil {
				embedded = cmap.ParseEmbeddedCIDMap(ctx, data)
			}
		}
	}
	usedCIDs, notdef, ok := collectType0CIDs(string(encName), embedded, usage)
	if !ok {
		// litho cannot decode this /Encoding to CIDs (an unparseable
		// embedded CMap stream or an unrecognized predefined CMap); leave
		// it unchecked rather than risk a false positive.
		return nil
	}
	if notdef {
		// A decode-level .notdef (odd-length tail or a CID beyond 65535)
		// also means no real glyph backs the rendered code: veraPDF fires
		// the glyph-presence rule alongside notdef (verified live on
		// 6-1-12-t10-fail-a under UA-1: 7.21.4.1 t2 + 7.21.8 t1).
		results := failf(rule, "font %s on page %d: text renders .notdef (odd-length code tail or CID beyond 65535)", fontName, pageNum)
		if pr := glyphPresenceRule(profile); pr.Specification != "" {
			results = append(results, failf(pr,
				"font %s on page %d: rendered code resolves to no glyph in the embedded font program", fontName, pageNum)...)
		}
		return results
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

	fd := document.ResolveFontDescriptor(ctx, doc, cidFontDict)
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
		// CFF font: check if CIDs exist in the charset. Sort for a
		// deterministic context when several CIDs are absent.
		validCIDs := typography.ExtractValidCIDs(fontData)
		if validCIDs == nil {
			return nil
		}
		cids := make([]uint16, 0, len(usedCIDs))
		for cid := range usedCIDs {
			cids = append(cids, cid)
		}
		slices.Sort(cids)
		for _, cid := range cids {
			if !validCIDs[cid] {
				// veraPDF fires BOTH the .notdef rule and the
				// glyph-presence rule on a used CID absent from the
				// charset (verified live on
				// PDF_A-2b__veraPDF_test_suite_6-1-13-t10-fail-a:
				// 6.2.11.8 t1 + 6.2.11.4.1 t2 on the same usedGlyph).
				results := failf(rule, "font %s on page %d: CID %d not in font charset (.notdef)", fontName, pageNum, cid)
				if pr := glyphPresenceRule(profile); pr.Specification != "" {
					results = append(results, failf(pr,
						"font %s on page %d: CID %d not present in embedded font program", fontName, pageNum, cid)...)
				}
				return results
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

// absentType0CIDs returns the set of used CIDs whose glyph is absent from the
// composite font's embedded program, plus the descendant CIDFont subtype. A CID
// is absent when a CIDFontType0 (CFF) charset omits it, or a CIDFontType2
// (TrueType, Identity CIDToGIDMap) maps it to an empty glyf entry with a zero
// advance; CID 0 is always .notdef. veraPDF fires the glyph-presence and notdef
// rules on these. The subtype lets callers apply the CFF-specific ToUnicode
// exemption: veraPDF builds no Glyph object for a CFF charset-absent CID so it
// skips the per-glyph ToUnicode rule there (verified live: 6-2-11-4-1-t02-fail-d
// CFF CID 104 fires 6.2.11.4.1 t2 + 6.2.11.8 t1 without 6.2.11.7.2 t1), but a
// TrueType empty glyph still gets a Glyph object and still fires ToUnicode
// (6-2-11-4-1-t02-fail-e CID 9215). ok is false when litho cannot decode the
// encoding to CIDs; callers then stay conservative.
func absentType0CIDs(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, usage *fontUsageInfo) (absent map[uint16]bool, subtype string, ok bool) {
	absent = make(map[uint16]bool)
	if usage == nil || len(usage.rawStrings) == 0 {
		return absent, "", false
	}

	encName, _ := document.ResolveName(ctx, doc, fontDict["Encoding"])
	var embedded *cmap.CIDMap
	if encName == "" {
		if stream, err := document.ResolveStream(ctx, doc, fontDict["Encoding"]); err == nil {
			if data, derr := doc.DecodeStream(ctx, stream); derr == nil {
				embedded = cmap.ParseEmbeddedCIDMap(ctx, data)
			}
		}
	}
	usedCIDs, _, decodeOK := collectType0CIDs(string(encName), embedded, usage)
	if !decodeOK {
		return absent, "", false
	}
	// CID 0 (.notdef) is deliberately NOT treated as absent here: its glyph IS
	// present in the program (it is the .notdef glyph), so veraPDF's
	// isGlyphPresent is true and neither the glyph-presence nor the per-glyph
	// ToUnicode rule fires on it. A reference to CID 0 is a .notdef-reference
	// concern handled by checkType0NotdefGlyph instead.

	descArr, err := document.ResolveArray(ctx, doc, fontDict["DescendantFonts"])
	if err != nil || len(descArr) == 0 {
		return absent, "", true
	}
	cidFontDict, err := document.ResolveDict(ctx, doc, descArr[0])
	if err != nil || cidFontDict == nil {
		return absent, "", true
	}
	subtype = fontSubtype(ctx, doc, cidFontDict)
	fd := document.ResolveFontDescriptor(ctx, doc, cidFontDict)
	if fd == nil {
		return absent, subtype, true
	}
	fontData := extractFontProgramData(ctx, doc, fd)
	if fontData == nil {
		return absent, subtype, true
	}

	switch subtype {
	case "CIDFontType0":
		if validCIDs := typography.ExtractValidCIDs(fontData); validCIDs != nil {
			for cid := range usedCIDs {
				if !validCIDs[cid] {
					absent[cid] = true
				}
			}
		}
	case "CIDFontType2":
		if cidToGIDMap, _ := document.ResolveName(ctx, doc, cidFontDict["CIDToGIDMap"]); cidToGIDMap == "Identity" {
			if emptyGIDs := typography.ExtractEmptyGIDs(ctx, fontData); emptyGIDs != nil {
				for cid := range usedCIDs {
					if emptyGIDs[cid] && lookupCIDWidth(ctx, doc, cidFontDict, cid) == 0 {
						absent[cid] = true
					}
				}
			}
		}
	}
	return absent, subtype, true
}

// checkType0GlyphPresence emits the glyph-presence rule for a composite font's
// used CIDs that are absent from the embedded program. PDF/A parts 2-4 and
// PDF/UA emit glyph presence alongside the .notdef rule in
// [checkType0NotdefGlyph]; this covers PDF/A-1, whose profiles define §6.3.5 t1
// (glyph presence) but no .notdef rule, so checkType0NotdefGlyph returns before
// reaching the emission. veraPDF fires §6.3.5 t1 on the same absent CIDs
// (verified: native A-1a/A-1b 6-2-11-4-1-t02-fail-* and 6-2-10-4-1-t02-fail-*).
func checkType0GlyphPresence(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, usage *fontUsageInfo) []Violation {
	rule := glyphPresenceRule(profile)
	if rule.Specification == "" {
		return nil
	}
	// Parts with a .notdef rule already emit glyph presence via
	// checkType0NotdefGlyph; only PDF/A-1 reaches this path.
	if notdefRule(profile).Specification != "" {
		return nil
	}
	absent, _, ok := absentType0CIDs(ctx, doc, fontDict, usage)
	if !ok || len(absent) == 0 {
		return nil
	}
	return failf(rule, "font %s on page %d: rendered code resolves to no glyph in the embedded font program", fontName, pageNum)
}
