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

		// Parts 2/3 and PDF/UA compare /CharSet against the whole font
		// program, which no page's codes enter into, so
		// checkCharSetDeclaredCoverage states it with the dictionary rules.
		if PDFAPart(profile) != 1 {
			return nil
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

// checkCharSetDeclaredCoverage states the parts 2/3 and PDF/UA form of the
// /CharSet rule: a strict bidirectional comparison against the entire font
// program glyph set, extras and omissions both forbidden.
//
// It is a property of the font object, not of one page's codes, so it belongs
// with the dictionary rules for the same reason [checkGlyphWidths] does.
// Judging it per page multiplied one finding by the pages that select the font.
//
// Part 1 is a different rule, scoped to the glyphs a page actually renders, and
// stays with the usage rules in [checkGlyphCoverage].
//
// # Corpus evidence
//
//   - govdocs thread_300_300511.pdf: reports glyph "O" of NSNPEK+CMR12 missing
//     from /CharSet on 31 pages, where veraPDF states it once against the font.
func checkCharSetDeclaredCoverage(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string) []Violation {
	if PDFAPart(profile) == 1 {
		return nil
	}
	if !isSubsetFont(fontBaseFont(ctx, doc, fontDict)) {
		return nil
	}
	if subtype := fontSubtype(ctx, doc, fontDict); subtype != "Type1" && subtype != "MMType1" {
		return nil
	}
	fd := document.ResolveFontDescriptor(ctx, doc, fontDict)
	if fd == nil {
		return nil
	}
	rule := charSetRule(profile)
	if rule.Specification == "" && !IsPDFUA(profile) {
		return nil
	}
	// An absent /CharSet is conformant here ("CharSet == null ||
	// charSetListsAllGlyphs"); a present but empty one is not, so key presence
	// decides rather than the decoded value.
	charSetObj, hasCharSet := fd["CharSet"]
	if !hasCharSet {
		return nil
	}
	charSetStr, _ := document.ResolveString(ctx, doc, charSetObj)

	completenessRule := charSetCompletenessRule(profile)
	if completenessRule.Specification == "" {
		return nil
	}
	fontData := extractFontProgramData(ctx, doc, fd)
	if fontData == nil {
		return nil
	}
	glyphNames := typography.ExtractGlyphNames(ctx, fontData)
	if glyphNames == nil {
		return nil
	}
	return checkCharSetCompleteness(string(charSetStr), glyphNames, fontName, pageNum, completenessRule, true)
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

	// Lowest missing name, not the first one the loop met: glyphNames is keyed
	// by GID, Go randomizes map iteration, and the rule fires once per font, so
	// the first-found name made the message differ between runs of the same file.
	var missing lowestOffender[string]
	for _, name := range glyphNames {
		if name == ".notdef" || name == "" {
			continue
		}
		if !charSetNames[name] {
			missing.offer(name)
		}
	}
	if name, any := missing.result(); any {
		return failf(rule, "font %s on page %d: glyph %q in font program not listed in /CharSet",
			fontName, pageNum, name)
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
	var extra lowestOffender[string]
	for name := range charSetNames {
		if name == ".notdef" {
			continue
		}
		if !fontNameSet[name] {
			extra.offer(name)
		}
	}
	if name, any := extra.result(); any {
		return failf(rule, "font %s on page %d: /CharSet lists glyph %q not present in font program",
			fontName, pageNum, name)
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

	// Lowest missing name, for the reason given in lowestOffender.
	var missing lowestOffender[string]
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
			missing.offer(name)
		}
	}
	if name, any := missing.result(); any {
		return failf(rule, "font %s on page %d: rendered glyph %q not listed in /CharSet",
			fontName, pageNum, name)
	}
	return nil
}

// checkCIDFontGlyphCoverage validates that CIDFont subsets have a /CIDSet entry
// in their FontDescriptor.
func checkCIDFontGlyphCoverage(ctx context.Context, doc *document.Document, cidFontDict ast.PdfDict, fontName string, pageNum int, profile string, rendered bool, usedCIDs map[uint16]bool) []Violation {
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
	// veraPDF keys §6.3.5 t3 on the Glyph object, so it fires only for a subset
	// CIDFont that renders at least one glyph: a font declared in /Resources but
	// never selected for text rendering produces no Glyph and is out of scope.
	// "rendered" mirrors that scope: the font shows at least one character code,
	// in any text-rendering mode. It is NOT the font's visibility (mode != 3):
	// veraPDF builds a Glyph for invisible (mode 3) text too, so a subset CIDFont
	// rendered only in mode 3 still fires. It is NOT the decoded CID set either:
	// a CJK Identity-H font whose CIDs litho cannot enumerate has shown codes and
	// must fire, so gating on usedCIDs would wrongly exempt it.
	if PDFAPart(profile) == 1 && rendered {
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

	// Part 1 (§6.3.5 t3): unlike parts 2+ (full-program §6.2.11.4.2 t2), veraPDF
	// scopes the part-1 CIDSet check to the glyphs the content actually renders,
	// so a CIDSet omitting an unused or empty-outline glyph is conformant.
	if PDFAPart(profile) == 1 {
		cidSetObj, hasCIDSet := fd["CIDSet"]
		if hasCIDSet {
			// §6.3.5 t3 has no fontFile exemption (unlike parts 2+ §6.2.11.4.2
			// t2): a subset CIDFont with a /CIDSet but no embedded program cannot
			// satisfy cidSetListsAllGlyphs, since there is no program to list, so
			// veraPDF fires (alongside the separate not-embedded rule).
			if extractFontProgramData(ctx, doc, fd) == nil {
				return failf(rule, "font %s on page %d: /CIDSet present but the CIDFont subset is not embedded", fontName, pageNum)
			}
			// CFF subsets: ValidCIDs enumerates the embedded subset charset.
			if r := checkCIDSetCompletenessCFF(ctx, doc, cidFontDict, cidSetObj, fontName, pageNum, rule); r != nil {
				return r
			}
			// TrueType subsets: only a rendered glyph with outline data that is
			// missing from the CIDSet is a defect.
			if r := checkCIDSetCompletenessTrueTypeRendered(ctx, doc, cidFontDict, fd, cidSetObj, fontName, pageNum, rule, usedCIDs); r != nil {
				return r
			}
		}
	}

	return nil
}

// decodeCIDSetBits decodes a /CIDSet bitfield: bit N of byte M (MSB first)
// represents CID M*8 + (7-N).
func decodeCIDSetBits(data []byte) map[uint16]bool {
	bits := make(map[uint16]bool)
	for i, b := range data {
		for bit := 7; bit >= 0; bit-- {
			if b&(1<<uint(bit)) != 0 {
				bits[uint16(i*8+(7-bit))] = true //nolint:gosec,mnd // CID bit-index; a conformant CIDSet covers the 16-bit CID space, 8 bits/byte MSB-first.
			}
		}
	}
	return bits
}

// checkCIDSetCompletenessTrueType verifies that a TrueType (CIDFontType2) subset
// /CIDSet identifies the glyphs of the font program under an Identity
// /CIDToGIDMap (ISO 19005 §6.2.11.4.2 t2 / §6.3.5 t3), which is also the map of
// a font that has no such entry (ISO 32000-1, 9.7.4.2, Table 117). veraPDF
// requires this for unrendered and empty-outline slots a non-subsetted program
// retains, not only the glyphs the content paints. GID 0 (.notdef) is exempt.
// A /CIDToGIDMap stream is judged by [checkCIDSetStreamMap]; non-TrueType
// programs return nil.
//
// veraPDF lists the program's CIDs through its horizontal metrics, one per hmtx
// long metric (CIDFontType2Program.getCIDList), so it requires every GID below
// hhea.numberOfHMetrics and not the glyphs past it, which share the last
// advance. A set bit whose CID the program does not hold fails too
// (containsCID).
//
// # Corpus evidence
//
//   - pdfjs issue5202.pdf: embeds an IPAexMincho subset whose numberOfHMetrics
//     is 1 beside a six-glyph /CIDSet, and veraPDF passes it.
//   - pdfjs issue5874.pdf: embeds a TrueType subset with no /CIDToGIDMap whose
//     /CIDSet leaves out 18800 of its 19642 glyphs, and veraPDF reports it.
func checkCIDSetCompletenessTrueType(ctx context.Context, doc *document.Document, cidFontDict, fd ast.PdfDict, cidSetObj ast.PdfObject, fontName string, pageNum int, rule Rule) []Violation {
	// veraPDF's CIDToGIDMapping reads an absent entry as Identity too.
	lookup, ok := cidToGIDMapping(ctx, doc, cidFontDict)
	if !ok {
		return nil
	}
	fontData := extractFontProgramData(ctx, doc, fd)
	if fontData == nil {
		return nil
	}
	numGlyphs := typography.ExtractNumGlyphs(fontData)
	if numGlyphs == 0 {
		return nil // Not a parseable TrueType program (e.g. CFF).
	}
	stream, err := document.ResolveStream(ctx, doc, cidSetObj)
	if err != nil {
		return nil
	}
	cidSetData, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		return nil
	}
	cidSetBits := decodeCIDSetBits(cidSetData)
	if lookup != nil {
		return checkCIDSetStreamMap(ctx, doc, cidFontDict, cidSetBits, numGlyphs, fontName, pageNum, rule)
	}
	listed := min(typography.ExtractNumberOfHMetrics(fontData), numGlyphs)
	for gid := 1; gid < listed; gid++ {
		if !cidSetBits[uint16(gid)] { //nolint:gosec // gid < numGlyphs, a uint16 from maxp.
			return failf(rule, "font %s on page %d: CID %d in font program not in /CIDSet",
				fontName, pageNum, gid)
		}
	}
	past := -1
	for cid := range cidSetBits {
		if int(cid) >= numGlyphs && (past < 0 || int(cid) < past) {
			past = int(cid)
		}
	}
	if past >= 0 {
		return failf(rule, "font %s on page %d: /CIDSet lists CID %d, past the program's %d glyphs",
			fontName, pageNum, past, numGlyphs)
	}
	return nil
}

// checkCIDSetStreamMap judges a TrueType /CIDSet against a /CIDToGIDMap stream
// (ISO 19005-2, 6.2.11.4.2 t2: every CID present in the font program). A CID the
// map covers is present when the glyph it selects exists, GID 0 included, since
// .notdef is a glyph of the program: veraPDF lists the program's CIDs as every
// CID below the map's length whose GID is below maxp.numGlyphs
// (CIDFontType2Program.getCIDList and containsCID), and fails a set bit for any
// other CID. CID 0 is exempt both ways.
//
// # Corpus evidence
//
//   - pdfjs issue11768_reduced.pdf: maps 15443 CIDs, 68 of them to real glyphs,
//     and its /CIDSet lists only those 68; veraPDF reports it.
func checkCIDSetStreamMap(ctx context.Context, doc *document.Document, cidFontDict ast.PdfDict, cidSetBits map[uint16]bool, numGlyphs int, fontName string, pageNum int, rule Rule) []Violation {
	gids, ok := cidToGIDStreamEntries(ctx, doc, cidFontDict["CIDToGIDMap"])
	if !ok {
		return nil
	}
	inSet := func(cid int) bool {
		return cid <= maxCID && cidSetBits[uint16(cid)] //nolint:gosec // cid <= maxCID, the 16-bit CID space.
	}
	for cid := 1; cid < len(gids); cid++ {
		if gids[cid] < numGlyphs && !inSet(cid) {
			return failf(rule, "font %s on page %d: CID %d, mapped to GID %d of the font program, not in /CIDSet",
				fontName, pageNum, cid, gids[cid])
		}
	}
	past := -1
	for cid := range cidSetBits {
		c := int(cid)
		if c == 0 || (c < len(gids) && gids[c] < numGlyphs) {
			continue
		}
		if past < 0 || c < past {
			past = c
		}
	}
	if past >= 0 {
		return failf(rule, "font %s on page %d: /CIDSet lists CID %d, which the font program does not hold",
			fontName, pageNum, past)
	}
	return nil
}

// cidToGIDStreamEntries reads a /CIDToGIDMap stream as veraPDF's
// CIDToGIDMapping.readMapping does: one big-endian GID per two bytes, and a
// trailing odd byte as one more entry holding that byte shifted left by 8. ok is
// false when the stream cannot be read.
func cidToGIDStreamEntries(ctx context.Context, doc *document.Document, obj ast.PdfObject) (gids []int, ok bool) {
	stream, err := document.ResolveStream(ctx, doc, obj)
	if err != nil {
		return nil, false
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		return nil, false
	}
	gids = make([]int, 0, (len(data)+1)/2)
	for i := 0; i < len(data); i += 2 {
		gid := int(data[i]) << 8
		if i+1 < len(data) {
			gid |= int(data[i+1])
		}
		gids = append(gids, gid)
	}
	return gids, true
}

// checkCIDSetCompletenessTrueTypeRendered is the rendered-scoped TrueType CIDSet
// check for PDF/A-1 §6.3.5 t3: a CID the content renders whose glyph has outline
// data must appear in the /CIDSet. Unused CIDs and empty-outline glyphs are
// exempt, which is the part-1 scope (parts 2+ require the full program; see
// [checkCIDSetCompletenessTrueType]). The CIDSet is indexed by CID; the GID used
// for the outline test comes from the /CIDToGIDMap (Identity, or a parsed
// stream). usedCIDs nil returns nil.
func checkCIDSetCompletenessTrueTypeRendered(ctx context.Context, doc *document.Document, cidFontDict, fd ast.PdfDict, cidSetObj ast.PdfObject, fontName string, pageNum int, rule Rule, usedCIDs map[uint16]bool) []Violation {
	if usedCIDs == nil {
		return nil
	}
	fontData := extractFontProgramData(ctx, doc, fd)
	if fontData == nil {
		return nil
	}
	emptyGIDs := typography.ExtractEmptyGIDs(ctx, fontData)
	if emptyGIDs == nil {
		return nil // Not a TrueType program.
	}
	cidToGID, ok := cidToGIDMapping(ctx, doc, cidFontDict)
	if !ok {
		return nil // A /CIDToGIDMap stream that cannot be read; skip rather than guess.
	}
	stream, err := document.ResolveStream(ctx, doc, cidSetObj)
	if err != nil {
		return nil
	}
	cidSetData, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		return nil
	}
	cidSetBits := decodeCIDSetBits(cidSetData)
	// Lowest offending CID; see lowestOffender.
	var worst lowestOffender[uint16]
	for cid := range usedCIDs {
		if cid == 0 {
			continue // .notdef
		}
		gid := cid
		if cidToGID != nil {
			gid = cidToGID(cid)
		}
		if emptyGIDs[gid] {
			continue // glyph has no outline data
		}
		if !cidSetBits[cid] {
			worst.offer(cid)
		}
	}
	if cid, any := worst.result(); any {
		return failf(rule, "font %s on page %d: rendered CID %d not in /CIDSet", fontName, pageNum, cid)
	}
	return nil
}

// cidToGIDMapping returns a CID→GID lookup for a CIDFontType2's /CIDToGIDMap. A
// nil lookup means /Identity (CID == GID), the default when the entry is absent.
// ok is false only when the entry is a stream that cannot be read, in which case
// the caller skips the check rather than guess the mapping.
func cidToGIDMapping(ctx context.Context, doc *document.Document, cidFontDict ast.PdfDict) (lookup func(uint16) uint16, ok bool) {
	obj, has := cidFontDict["CIDToGIDMap"]
	if !has {
		return nil, true
	}
	if name, _ := document.ResolveName(ctx, doc, obj); name != "" {
		return nil, name == "Identity"
	}
	stream, err := document.ResolveStream(ctx, doc, obj)
	if err != nil {
		return nil, false
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		return nil, false
	}
	return func(cid uint16) uint16 {
		i := int(cid) * 2
		if i+1 >= len(data) {
			return 0 // beyond the map → .notdef
		}
		return uint16(data[i])<<8 | uint16(data[i+1])
	}, true
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

	cidSetBits := decodeCIDSetBits(cidSetData)

	fd := document.ResolveFontDescriptor(ctx, doc, cidFontDict)
	if fd == nil {
		return nil
	}
	fontData := extractFontProgramData(ctx, doc, fd)
	if fontData == nil {
		return nil
	}

	// TrueType subsets use the full-program check (every glyph in the program);
	// CFF subsets are scoped to the CIDs the content actually renders.
	if typography.ExtractNumGlyphs(fontData) > 0 {
		return checkCIDSetCompletenessTrueType(ctx, doc, cidFontDict, fd, cidSetObj, fontName, pageNum, rule)
	}

	validCIDs := typography.ExtractValidCIDs(fontData)
	// Lowest offending CID; see lowestOffender.
	var worst lowestOffender[uint16]
	for cid := range validCIDs {
		if cid == 0 {
			continue // .notdef
		}
		if !usedCIDs[cid] {
			continue
		}
		if !cidSetBits[cid] {
			worst.offer(cid)
		}
	}
	if cid, any := worst.result(); any {
		return failf(rule, "font %s on page %d: CID %d in font program not in /CIDSet", fontName, pageNum, cid)
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
	cidSetBits := decodeCIDSetBits(cidSetData)

	// Lowest offending CID; see lowestOffender.
	var worst lowestOffender[uint16]
	for cid := range validCIDs {
		if cid == 0 {
			continue // .notdef
		}
		if !cidSetBits[cid] {
			worst.offer(cid)
		}
	}
	if cid, any := worst.result(); any {
		return failf(rule, "font %s on page %d: CID %d in font program not in /CIDSet", fontName, pageNum, cid)
	}
	return nil
}
