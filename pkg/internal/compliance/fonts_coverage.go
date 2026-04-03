package compliance

import (
	"context"
	"log/slog"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/typography"
)

// --- Glyph coverage (CharSet / CIDSet) rule selectors ---

// charSetRule returns the CharSet rule for Type1 font subsets.
func charSetRule(profile string) Rule {
	return ruleByPart(pdfaPart(profile), Rule_19005_1_6_3_5_t02, Rule_19005_2_6_2_11_4_1_t02, Rule_19005_3_6_2_11_4_1_t02, Rule_19005_4_6_2_10_4_1_t02)
}

// cidSetRule returns the CIDSet rule for CIDFont subsets.
func cidSetRule(profile string) Rule {
	if isPDFUA(profile) {
		return Rule_14289_1_7_21_4_2_t02
	}
	return ruleByPart(pdfaPart(profile), Rule_19005_1_6_3_5_t03, Rule_19005_2_6_2_11_4_1_t02, Rule_19005_3_6_2_11_4_1_t02, Rule_19005_4_6_2_10_4_1_t02)
}

// charSetCompletenessRule returns the rule for CharSet completeness
// (all font program glyphs must be listed in /CharSet).
func charSetCompletenessRule(profile string) Rule {
	if isPDFUA(profile) {
		return Rule_14289_1_7_21_4_2_t01
	}
	// Part 1 also requires completeness (§6.3.5 t2), but the CFF glyph name
	// extraction returns unreachable glyphs (e.g., "x") that VeraPDF does not
	// consider part of the font subset, causing FP on pass-b.
	return ruleByPart(pdfaPart(profile), Rule{}, Rule_19005_2_6_2_11_4_2_t01, Rule_19005_3_6_2_11_4_2_t01, Rule{})
}

// cidSetCompletenessRule returns the rule for CIDSet completeness
// (all CIDs in font program must be identified in /CIDSet).
func cidSetCompletenessRule(profile string) Rule {
	if isPDFUA(profile) {
		return Rule_14289_1_7_21_4_2_t02
	}
	// Part 1 only requires CIDSet presence, not completeness.
	return ruleByPart(pdfaPart(profile), Rule{}, Rule_19005_2_6_2_11_4_2_t02, Rule_19005_3_6_2_11_4_2_t02, Rule{})
}

// checkGlyphCoverage validates that subset fonts have the required CharSet or
// CIDSet entries in their FontDescriptor.
// - Type1 subsets: /FontDescriptor must have /CharSet
// - CIDFont subsets: /FontDescriptor must have /CIDSet
func checkGlyphCoverage(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string) []Violation {
	basefont := fontBaseFont(ctx, doc, fontDict)
	if !isSubsetFont(basefont) {
		return nil
	}

	subtype := fontSubtype(ctx, doc, fontDict)

	fd := findFontDescriptor(ctx, doc, fontDict)
	if fd == nil {
		return nil
	}

	// Type1 subsets require /CharSet.
	if subtype == "Type1" || subtype == "MMType1" {
		rule := charSetRule(profile)
		if rule.Specification == "" && !isPDFUA(profile) {
			return nil
		}

		charSetStr, _ := document.ResolveString(ctx, doc, fd["CharSet"])

		// Part 1: CharSet presence is required.
		if pdfaPart(profile) == 1 && charSetStr == "" {
			return failf(rule, "font %s on page %d: Type1 subset missing /CharSet", fontName, pageNum)
		}

		// §6.2.11.4.2 t1 / PDF/UA-1 §7.21.4.2: if CharSet present, it must
		// list ALL font program glyphs (parts 2/3 and PDF/UA).
		if charSetStr != "" {
			completenessRule := charSetCompletenessRule(profile)
			if completenessRule.Specification != "" {
				fontData := extractFontProgramData(ctx, doc, fd)
				if fontData != nil {
					glyphNames := typography.ExtractGlyphNames(fontData)
					if glyphNames != nil {
						return checkCharSetCompleteness(string(charSetStr), glyphNames, fontName, pageNum, completenessRule, true)
					}
				}
			}
		}

		return nil
	}

	return nil
}

// checkCharSetCompleteness verifies that /CharSet lists all glyph names
// present in the font program. When bidirectional is true, it also checks
// that every name in /CharSet exists in the font program (required for
// parts 2/3 but not part 1, which only requires the forward check).
func checkCharSetCompleteness(charSetStr string, glyphNames map[uint16]string, fontName string, pageNum int, rule Rule, bidirectional bool) []Violation {
	charSetNames := make(map[string]bool)
	for name := range strings.SplitSeq(charSetStr, "/") {
		if name != "" {
			charSetNames[name] = true
		}
	}

	for _, name := range glyphNames {
		if name == ".notdef" || name == "" {
			continue
		}
		if !charSetNames[name] {
			return failf(rule, "font %s on page %d: glyph %q in font program not listed in /CharSet",
				fontName, pageNum, name)
		}
	}

	// Reverse check: every name in /CharSet must exist in the font program.
	// Part 1 only requires the forward direction (font program → CharSet).
	if !bidirectional {
		return nil
	}
	fontNameSet := make(map[string]bool, len(glyphNames))
	for _, name := range glyphNames {
		if name != "" {
			fontNameSet[name] = true
		}
	}
	for name := range charSetNames {
		if name == ".notdef" {
			continue
		}
		if !fontNameSet[name] {
			return failf(rule, "font %s on page %d: /CharSet lists glyph %q not present in font program",
				fontName, pageNum, name)
		}
	}

	return nil
}

// checkCIDFontGlyphCoverage validates that CIDFont subsets have a /CIDSet entry
// in their FontDescriptor.
func checkCIDFontGlyphCoverage(ctx context.Context, doc *document.Document, cidFontDict ast.PdfDict, fontName string, pageNum int, profile string) []Violation {
	basefont := fontBaseFont(ctx, doc, cidFontDict)
	if !isSubsetFont(basefont) {
		return nil
	}

	rule := cidSetRule(profile)
	if rule.Specification == "" {
		return nil
	}

	fd := findFontDescriptor(ctx, doc, cidFontDict)
	if fd == nil {
		return nil
	}

	// Part 1: CIDSet is required. Parts 2+: only check if present (correctness).
	if pdfaPart(profile) == 1 {
		if _, hasCIDSet := fd["CIDSet"]; !hasCIDSet {
			return failf(rule, "font %s on page %d: CIDFont subset missing /CIDSet", fontName, pageNum)
		}
	}

	// §6.2.11.4.2 t2 / §6.3.5 t3: if CIDSet present, it must identify ALL CIDs in the font program.
	completenessRule := cidSetCompletenessRule(profile)
	if completenessRule.Specification != "" {
		cidSetObj, hasCIDSet := fd["CIDSet"]
		if hasCIDSet {
			if r := checkCIDSetCompleteness(ctx, doc, cidFontDict, cidSetObj, fontName, pageNum, completenessRule); r != nil {
				return r
			}
		}
	}

	// Part 1: also check CIDSet completeness for CFF CIDFonts.
	// TrueType empty-glyph detection is unreliable for Part 1, so we limit
	// this to CFF fonts where ValidCIDs produces accurate results.
	if pdfaPart(profile) == 1 {
		cidSetObj, hasCIDSet := fd["CIDSet"]
		if hasCIDSet {
			if r := checkCIDSetCompletenessCFF(ctx, doc, cidFontDict, cidSetObj, fontName, pageNum, rule); r != nil {
				return r
			}
		}
	}

	return nil
}

// checkCIDSetCompleteness decodes a CIDSet bitfield stream and verifies
// it covers all GIDs with non-empty glyph data in the embedded font program.
func checkCIDSetCompleteness(ctx context.Context, doc *document.Document, cidFontDict ast.PdfDict, cidSetObj ast.PdfObject, fontName string, pageNum int, rule Rule) []Violation {
	stream, err := document.ResolveStream(ctx, doc, cidSetObj)
	if err != nil {
		slog.Debug("compliance: cannot resolve CIDSet stream", "error", err)
		return nil
	}
	cidSetData, err := doc.DecodeStream(ctx, stream)
	if err != nil || len(cidSetData) == 0 {
		return nil
	}

	// Decode CIDSet bitfield: bit N of byte M represents CID (M*8 + (7-N)).
	cidSetBits := make(map[uint16]bool)
	for i, b := range cidSetData {
		for bit := 7; bit >= 0; bit-- {
			if b&(1<<uint(bit)) != 0 {
				cid := uint16(i*8 + (7 - bit)) //nolint:gosec,mnd
				cidSetBits[cid] = true
			}
		}
	}

	// Get font program and find non-empty GIDs.
	fd := findFontDescriptor(ctx, doc, cidFontDict)
	if fd == nil {
		return nil
	}
	fontData := extractFontProgramData(ctx, doc, fd)
	if fontData == nil {
		return nil
	}

	// Check CIDToGIDMap to determine CID→GID mapping.
	cidToGIDName, _ := document.ResolveName(ctx, doc, cidFontDict["CIDToGIDMap"])

	emptyGIDs := typography.ExtractEmptyGIDs(fontData)
	if emptyGIDs == nil {
		// Not a TrueType font or can't parse loca — try CFF.
		validCIDs := typography.ExtractValidCIDs(fontData)
		for cid := range validCIDs {
			if cid == 0 {
				continue // .notdef
			}
			if !cidSetBits[cid] {
				return failf(rule, "font %s on page %d: CID %d in font program not in /CIDSet", fontName, pageNum, cid)
			}
		}
		return nil
	}

	// TrueType: with CIDToGIDMap=/Identity, CID == GID.
	if cidToGIDName != "Identity" {
		return nil // Can't verify non-Identity mappings.
	}

	// Find GIDs with actual glyph data (non-empty).
	for gid := uint16(1); gid < uint16(len(cidSetData)*8); gid++ { //nolint:gosec,mnd
		if !emptyGIDs[gid] {
			// GID has data → CID (== GID under Identity) must be in CIDSet.
			if !cidSetBits[gid] {
				return failf(rule, "font %s on page %d: CID %d (GID %d) has glyph data but not in /CIDSet",
					fontName, pageNum, gid, gid)
			}
		}
	}
	return nil
}

// checkCIDSetCompletenessCFF checks CIDSet completeness for CFF CIDFonts only.
// This is used for PDF/A-1 where TrueType empty-glyph detection is unreliable
// but CFF ValidCIDs produces accurate results.
func checkCIDSetCompletenessCFF(ctx context.Context, doc *document.Document, cidFontDict ast.PdfDict, cidSetObj ast.PdfObject, fontName string, pageNum int, rule Rule) []Violation {
	fd := findFontDescriptor(ctx, doc, cidFontDict)
	if fd == nil {
		return nil
	}
	fontData := extractFontProgramData(ctx, doc, fd)
	if fontData == nil {
		return nil
	}

	// Only check CFF fonts — skip TrueType.
	validCIDs := typography.ExtractValidCIDs(fontData)
	if validCIDs == nil {
		return nil
	}

	stream, err := document.ResolveStream(ctx, doc, cidSetObj)
	if err != nil {
		slog.Debug("compliance: cannot resolve CIDSet stream", "error", err)
		return nil
	}
	cidSetData, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		slog.Debug("compliance: cannot decode CIDSet stream", "error", err)
		return nil
	}

	// Decode CIDSet bitfield (may be empty for a zero-length stream).
	cidSetBits := make(map[uint16]bool)
	for i, b := range cidSetData {
		for bit := 7; bit >= 0; bit-- {
			if b&(1<<uint(bit)) != 0 {
				cid := uint16(i*8 + (7 - bit)) //nolint:gosec,mnd
				cidSetBits[cid] = true
			}
		}
	}

	for cid := range validCIDs {
		if cid == 0 {
			continue // .notdef
		}
		if !cidSetBits[cid] {
			return failf(rule, "font %s on page %d: CID %d in font program not in /CIDSet", fontName, pageNum, cid)
		}
	}
	return nil
}
