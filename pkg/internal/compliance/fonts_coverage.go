package compliance

import (
	"context"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/typography"
)

// charSetRule returns the CharSet rule for Type1 font subsets.
func charSetRule(profile string) Rule {
	return ruleByPart(PDFAPart(profile), Rule_19005_1_6_3_5_t02, Rule_19005_2_6_2_11_4_1_t02, Rule_19005_3_6_2_11_4_1_t02, Rule_19005_4_6_2_10_4_1_t02)
}

// cidSetRule returns the CIDSet rule for CIDFont subsets. PDF/UA-2 has
// no CharSet or CIDSet rules at all (the PDFUA-2 profile keys font
// completeness on 8.4.5.5.1 t2, glyph presence in the font program), so
// the UA-2 branch returns the zero Rule.
func cidSetRule(profile string) Rule {
	if IsPDFUA(profile) {
		if PDFUAPart(profile) == 2 {
			return Rule{}
		}
		return Rule_14289_1_7_21_4_2_t02
	}
	return ruleByPart(PDFAPart(profile), Rule_19005_1_6_3_5_t03, Rule_19005_2_6_2_11_4_1_t02, Rule_19005_3_6_2_11_4_1_t02, Rule_19005_4_6_2_10_4_1_t02)
}

// charSetCompletenessRule returns the rule for CharSet completeness.
//
// Parts 2/3 (§6.2.11.4.2 t1) require CharSet to list every glyph defined
// in the font subset: a bidirectional check against the full CFF
// charset. Part 1 (§6.3.5 t2) is narrower. It only requires CharSet to
// cover the glyphs actually rendered on pages that reference the font.
// A Type 1 subsetter commonly packs extra glyphs into the subset that
// the content stream never uses. ISO 19005-1 §6.3.5 does not specify
// whether such unreachable glyphs belong to the "subset" that CharSet
// must list; litho follows the interpretation that only glyphs actually
// rendered count for part 1. Litho therefore runs the part 1 check
// one-directional against a rendered-glyph set computed from
// content-stream usage, and reserves the bidirectional full-charset
// check for parts 2/3, whose §6.2.11.4.2 t1 rule is explicit.
func charSetCompletenessRule(profile string) Rule {
	if IsPDFUA(profile) {
		if PDFUAPart(profile) == 2 {
			return Rule{} // No CharSet rule in PDF/UA-2; see cidSetRule.
		}
		return Rule_14289_1_7_21_4_2_t01
	}
	return ruleByPart(PDFAPart(profile), Rule_19005_1_6_3_5_t02, Rule_19005_2_6_2_11_4_2_t01, Rule_19005_3_6_2_11_4_2_t01, Rule{})
}

// cidSetCompletenessRule returns the rule for CIDSet completeness
// (all CIDs in font program must be identified in /CIDSet).
func cidSetCompletenessRule(profile string) Rule {
	if IsPDFUA(profile) {
		if PDFUAPart(profile) == 2 {
			return Rule{} // No CIDSet rule in PDF/UA-2; see cidSetRule.
		}
		return Rule_14289_1_7_21_4_2_t02
	}
	// Part 1 only requires CIDSet presence, not completeness.
	return ruleByPart(PDFAPart(profile), Rule{}, Rule_19005_2_6_2_11_4_2_t02, Rule_19005_3_6_2_11_4_2_t02, Rule{})
}

// checkGlyphCoverage validates that subset fonts have the required CharSet or
// CIDSet entries in their FontDescriptor.
// - Type1 subsets: /FontDescriptor must have /CharSet
// - CIDFont subsets: /FontDescriptor must have /CIDSet
//
// usage, when non-nil, provides the set of character codes actually
// referenced by text operators on the page that hosts fontDict. It is
// consulted for PDF/A-1's one-directional CharSet completeness check
// (§6.3.5 t2), where CharSet only has to cover rendered glyphs; it is
// ignored for parts 2/3, whose rule requires the full font-program
// charset to be listed.
func checkGlyphCoverage(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, usage *fontUsageInfo) []Violation {
	basefont := fontBaseFont(ctx, doc, fontDict)
	if !isSubsetFont(basefont) {
		return nil
	}

	subtype := fontSubtype(ctx, doc, fontDict)

	fd := document.ResolveFontDescriptor(ctx, doc, fontDict)
	if fd == nil {
		return nil
	}

	// Type1 subsets require /CharSet.
	if subtype == "Type1" || subtype == "MMType1" {
		rule := charSetRule(profile)
		if rule.Specification == "" && !IsPDFUA(profile) {
			return nil
		}

		charSetObj, hasCharSet := fd["CharSet"]
		charSetStr, _ := document.ResolveString(ctx, doc, charSetObj)

		// Part 1: CharSet presence is required.
		if PDFAPart(profile) == 1 && charSetStr == "" {
			return failf(rule, "font %s on page %d: Type1 subset missing /CharSet", fontName, pageNum)
		}

		// Parts 2+/UA: an absent CharSet is conformant (the rule reads
		// "CharSet == null || charSetListsAllGlyphs"). A present-but-empty
		// CharSet is NOT absent: it lists zero glyphs while the subset font
		// program defines some, so completeness fails (veraPDF fires
		// §6.2.11.4.2 t1). Distinguish the two by key presence, not by the
		// decoded value, so the completeness check below runs on /CharSet ().
		if !hasCharSet {
			return nil
		}

		completenessRule := charSetCompletenessRule(profile)
		if completenessRule.Specification == "" {
			return nil
		}

		fontData := extractFontProgramData(ctx, doc, fd)
		if fontData == nil {
			return nil
		}

		// Parts 2/3 / PDF/UA: strict bidirectional check against the
		// entire CFF charset. CharSet must equal the font program glyph
		// set (extras forbidden, missing glyphs forbidden).
		if PDFAPart(profile) != 1 {
			glyphNames := typography.ExtractGlyphNames(ctx, fontData)
			if glyphNames == nil {
				return nil
			}
			return checkCharSetCompleteness(string(charSetStr), glyphNames, fontName, pageNum, completenessRule, true)
		}

		// Part 1: §6.3.5 t2 is one-directional and scoped to rendered glyphs:
		// every glyph the content actually paints must be named in /CharSet,
		// while glyphs a subsetter packed in but never uses need not be listed.
		// The reference set is therefore the used character codes mapped through
		// the font encoding to glyph names, intersected with the names the
		// embedded program actually defines, so a /Encoding that names a glyph
		// differently from the program (math/symbol fonts) cannot produce a
		// false positive: only a name that is both rendered and present in the
		// program, yet absent from /CharSet, is a genuine omission.
		glyphNames := typography.ExtractGlyphNames(ctx, fontData)
		if glyphNames == nil {
			return nil
		}
		return checkCharSetRenderedGlyphs(ctx, doc, fontDict, glyphNames, string(charSetStr), fontName, pageNum, completenessRule, usage)
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

// checkCharSetRenderedGlyphs implements ISO 19005-1 §6.3.5 t2 for part 1: every
// glyph the content renders must be named in /CharSet, while glyphs a subsetter
// packed in but never paints need not be listed. The reference set is the
// rendered glyphs (used codes mapped through the font encoding) intersected with
// glyphNames (the names the embedded program defines). The intersection makes
// the check false-positive-safe: an /Encoding that names a glyph differently
// from the program (math/symbol fonts) yields a name absent from glyphNames,
// which is skipped, so only a name that is rendered, present in the program, and
// absent from /CharSet is reported. A nil usage or unresolvable encoding defers.
func checkCharSetRenderedGlyphs(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, glyphNames map[uint16]string, charSetStr, fontName string, pageNum int, rule Rule, usage *fontUsageInfo) []Violation {
	if usage == nil {
		return nil
	}
	encoding := resolveSimpleFontEncoding(ctx, doc, fontDict)
	if encoding == nil {
		return nil
	}

	programNames := make(map[string]bool, len(glyphNames))
	for _, name := range glyphNames {
		if name != "" {
			programNames[name] = true
		}
	}
	charSetNames := make(map[string]bool)
	for name := range strings.SplitSeq(charSetStr, "/") {
		if name != "" {
			charSetNames[name] = true
		}
	}

	for code := range usage.charCodes {
		name := encoding[code]
		if name == "" || name == ".notdef" {
			continue
		}
		// Skip names the program does not define: an encoding/program name
		// divergence is unresolvable here, so reporting it would risk a false
		// positive.
		if !programNames[name] {
			continue
		}
		if !charSetNames[name] {
			return failf(rule, "font %s on page %d: rendered glyph %q not listed in /CharSet",
				fontName, pageNum, name)
		}
	}
	return nil
}

// checkCIDFontGlyphCoverage validates that CIDFont subsets have a /CIDSet entry
// in their FontDescriptor.
func checkCIDFontGlyphCoverage(ctx context.Context, doc *document.Document, cidFontDict ast.PdfDict, fontName string, pageNum int, profile string, usedCIDs map[uint16]bool) []Violation {
	basefont := fontBaseFont(ctx, doc, cidFontDict)
	if !isSubsetFont(basefont) {
		return nil
	}

	rule := cidSetRule(profile)
	if rule.Specification == "" {
		return nil
	}

	fd := document.ResolveFontDescriptor(ctx, doc, cidFontDict)
	if fd == nil {
		return nil
	}

	// Part 1: CIDSet is required. Parts 2+: only check if present (correctness).
	if PDFAPart(profile) == 1 {
		if _, hasCIDSet := fd["CIDSet"]; !hasCIDSet {
			return failf(rule, "font %s on page %d: CIDFont subset missing /CIDSet", fontName, pageNum)
		}
	}

	// §6.2.11.4.2 t2 / §6.3.5 t3: if CIDSet present, it must identify ALL CIDs in the font program.
	completenessRule := cidSetCompletenessRule(profile)
	if completenessRule.Specification != "" {
		cidSetObj, hasCIDSet := fd["CIDSet"]
		if hasCIDSet {
			if r := checkCIDSetCompleteness(ctx, doc, cidFontDict, cidSetObj, fontName, pageNum, completenessRule, usedCIDs); r != nil {
				return r
			}
		}
	}

	// Part 1: also check CIDSet completeness for CFF CIDFonts.
	// TrueType empty-glyph detection is unreliable for Part 1, so the
	// check is limited to CFF fonts where ValidCIDs produces accurate
	// results.
	if PDFAPart(profile) == 1 {
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
func checkCIDSetCompleteness(ctx context.Context, doc *document.Document, cidFontDict ast.PdfDict, cidSetObj ast.PdfObject, fontName string, pageNum int, rule Rule, usedCIDs map[uint16]bool) []Violation {
	// Completeness is scoped to USED glyphs. ISO 19005-2 §6.2.11.4.2 t2
	// keys CIDSet on the CIDs the document actually renders: a glyph the
	// content stream never paints cannot cause a rendering failure, so a
	// CIDSet omitting an unused CID is conformant and flagging it would
	// be a false positive. With usage unknown, nothing can be checked
	// without over-firing.
	if usedCIDs == nil {
		return nil
	}
	stream, err := document.ResolveStream(ctx, doc, cidSetObj)
	if err != nil {
		doc.Logger.Debug("compliance: cannot resolve CIDSet stream", "error", err)
		return nil
	}
	cidSetData, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		return nil
	}
	// A present-but-empty CIDSet is not "nothing to check": when the document
	// renders any CID, an empty set cannot identify it, so completeness fails
	// (ISO 19005-2 §6.2.11.4.2 t2). veraPDF flags this; an empty cidSetData
	// falls through with an empty cidSetBits map so the CFF coverage loop fires
	// on the first used CID. No conforming file pairs used glyphs with an empty
	// CIDSet (it would be self-contradictory), so this cannot misfire.

	// Decode CIDSet bitfield: bit N of byte M represents CID (M*8 + (7-N)).
	cidSetBits := make(map[uint16]bool)
	for i, b := range cidSetData {
		for bit := 7; bit >= 0; bit-- {
			if b&(1<<uint(bit)) != 0 {
				cid := uint16(i*8 + (7 - bit)) //nolint:gosec,mnd // G115: CID bit-index; a conformant CIDSet covers the 16-bit CID space (8 bits/byte, MSB-first).
				cidSetBits[cid] = true
			}
		}
	}

	// Get font program and find non-empty GIDs.
	fd := document.ResolveFontDescriptor(ctx, doc, cidFontDict)
	if fd == nil {
		return nil
	}
	fontData := extractFontProgramData(ctx, doc, fd)
	if fontData == nil {
		return nil
	}

	// Check CIDToGIDMap to determine CID→GID mapping.
	cidToGIDName, _ := document.ResolveName(ctx, doc, cidFontDict["CIDToGIDMap"])

	emptyGIDs := typography.ExtractEmptyGIDs(ctx, fontData)
	if emptyGIDs == nil {
		// Not a TrueType font or can't parse loca. Try CFF.
		validCIDs := typography.ExtractValidCIDs(fontData)
		for cid := range validCIDs {
			if cid == 0 {
				continue // .notdef
			}
			if !usedCIDs[cid] {
				continue
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
	for gid := uint16(1); gid < uint16(len(cidSetData)*8); gid++ { //nolint:gosec,mnd // G115: len*8 is the CID count; a conformant CIDSet covers the 16-bit CID space.
		if !usedCIDs[gid] {
			continue
		}
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
	fd := document.ResolveFontDescriptor(ctx, doc, cidFontDict)
	if fd == nil {
		return nil
	}
	fontData := extractFontProgramData(ctx, doc, fd)
	if fontData == nil {
		return nil
	}

	// Only check CFF fonts. Skip TrueType.
	validCIDs := typography.ExtractValidCIDs(fontData)
	if validCIDs == nil {
		return nil
	}

	stream, err := document.ResolveStream(ctx, doc, cidSetObj)
	if err != nil {
		doc.Logger.Debug("compliance: cannot resolve CIDSet stream", "error", err)
		return nil
	}
	cidSetData, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		doc.Logger.Debug("compliance: cannot decode CIDSet stream", "error", err)
		return nil
	}

	// Decode CIDSet bitfield (may be empty for a zero-length stream).
	cidSetBits := make(map[uint16]bool)
	for i, b := range cidSetData {
		for bit := 7; bit >= 0; bit-- {
			if b&(1<<uint(bit)) != 0 {
				cid := uint16(i*8 + (7 - bit)) //nolint:gosec,mnd // G115: CID bit-index; a conformant CIDSet covers the 16-bit CID space (8 bits/byte, MSB-first).
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
