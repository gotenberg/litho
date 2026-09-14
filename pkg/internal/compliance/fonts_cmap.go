package compliance

import (
	"context"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/typography/cmap"
)

// cidToGIDMapRule returns the CIDToGIDMap rule for the given profile.
// ISO 19005-1 §6.3.3.2 applies the CIDToGIDMap requirement to part 1 as well,
// keyed on the CIDFont dictionary; earlier litho skipped part 1.
func cidToGIDMapRule(profile string) Rule {
	if IsPDFUA(profile) {
		if PDFUAPart(profile) == 2 {
			return Rule_14289_2_8_4_5_3_2_t01
		}
		return Rule_14289_1_7_21_3_2_t01
	}
	return ruleByPart(PDFAPart(profile), Rule_19005_1_6_3_3_2_t01, Rule_19005_2_6_2_11_3_2_t01, Rule_19005_3_6_2_11_3_2_t01, Rule_19005_4_6_2_10_3_2_t01)
}

// cidSystemInfoRule returns the CIDSystemInfo consistency rule.
// Returns a zero Rule if the check does not apply (part 1).
func cidSystemInfoRule(profile string) Rule {
	if IsPDFUA(profile) {
		if PDFUAPart(profile) == 2 {
			return Rule_14289_2_8_4_5_3_1_t01
		}
		return Rule_14289_1_7_21_3_1_t01
	}
	return ruleByPart(PDFAPart(profile), Rule_19005_1_6_3_3_1_t01, Rule_19005_2_6_2_11_3_1_t01, Rule_19005_3_6_2_11_3_1_t01, Rule_19005_4_6_2_10_3_1_t01)
}

// cmapEmbeddingRules returns the 3 CMap embedding rules (t1: non-predefined must
// be embedded, t2: WMode consistency, t3: usecmap non-predefined forbidden).
func cmapEmbeddingRules(profile string) (t1, t2, t3 Rule) {
	if IsPDFUA(profile) {
		if PDFUAPart(profile) == 2 {
			return Rule_14289_2_8_4_5_4_t01, Rule_14289_2_8_4_5_4_t02, Rule_14289_2_8_4_5_4_t03
		}
		return Rule_14289_1_7_21_3_3_t01, Rule_14289_1_7_21_3_3_t02, Rule_14289_1_7_21_3_3_t03
	}
	switch PDFAPart(profile) {
	case 1:
		// ISO 19005-1 §6.3.3.3 defines t1 (embedded/Identity) and t2 (WMode) but
		// has no usecmap rule, so t3 is empty for part 1.
		return Rule_19005_1_6_3_3_3_t01, Rule_19005_1_6_3_3_3_t02, Rule{}
	case 2:
		return Rule_19005_2_6_2_11_3_3_t01, Rule_19005_2_6_2_11_3_3_t02, Rule_19005_2_6_2_11_3_3_t03
	case 3:
		return Rule_19005_3_6_2_11_3_3_t01, Rule_19005_3_6_2_11_3_3_t02, Rule_19005_3_6_2_11_3_3_t03
	case 4:
		return Rule_19005_4_6_2_10_3_3_t01, Rule_19005_4_6_2_10_3_3_t02, Rule_19005_4_6_2_10_3_3_t03
	}
	return Rule{}, Rule{}, Rule{}
}

// cmapCIDRule returns the rule for CID value > 65535 checks.
func cmapCIDRule(profile string) Rule {
	return ruleByPart(PDFAPart(profile), Rule_19005_1_6_1_12_t10, Rule_19005_2_6_1_13_t10, Rule_19005_3_6_1_13_t10, Rule{})
}

// predefinedCMapPrefixes lists the predefined CMap name prefixes from
// ISO 32000-1 Table 118. A CMap name starting with one of these (followed
// by -H, -V, or -Supplement) is predefined and doesn't need embedding.
//
//nolint:gochecknoglobals // static lookup table
var predefinedCMapPrefixes = []string{
	"Adobe-GB1", "Adobe-CNS1", "Adobe-Japan1", "Adobe-Korea1",
	"Adobe-Japan2", "Identity-H", "Identity-V",
	// Common predefined names
	"UniGB-UCS2", "UniGB-UTF16", "UniCNS-UCS2", "UniCNS-UTF16",
	"UniJIS-UCS2", "UniJIS-UTF16", "UniKS-UCS2", "UniKS-UTF16",
	"UniJIS2004-UTF16", "UniJIS-UTF8",
}

// table116CMapNames is the complete set of predefined CMap names of
// ISO 32000-2, Table 116.
//
// ISO 14289-2, 8.4.5.4 exempts exactly these from the embedding requirement,
// and ISO 19005 parts 2 to 4 word their counterparts the same way. litho
// carried a prefix list that reached the Unicode CMaps and Identity but none of
// the legacy CJK families, so a font encoded with GB-EUC-H, 90ms-RKSJ-H or the
// bare H was reported as neither predefined nor embedded. pdfjs
// issue18466.pdf (UniGB-UTF16-H) and mixedfonts.pdf (UniJIS-UTF16-H) are the
// cases the corpus carries.
//
//nolint:gochecknoglobals // static lookup table mirroring Table 116.
var table116CMapNames = map[string]bool{
	"83pv-RKSJ-H":      true,
	"90ms-RKSJ-H":      true,
	"90ms-RKSJ-V":      true,
	"90msp-RKSJ-H":     true,
	"90msp-RKSJ-V":     true,
	"90pv-RKSJ-H":      true,
	"Add-RKSJ-H":       true,
	"Add-RKSJ-V":       true,
	"B5pc-H":           true,
	"B5pc-V":           true,
	"CNS-EUC-H":        true,
	"CNS-EUC-V":        true,
	"ETen-B5-H":        true,
	"ETen-B5-V":        true,
	"ETenms-B5-H":      true,
	"ETenms-B5-V":      true,
	"EUC-H":            true,
	"EUC-V":            true,
	"Ext-RKSJ-H":       true,
	"Ext-RKSJ-V":       true,
	"GB-EUC-H":         true,
	"GB-EUC-V":         true,
	"GBK-EUC-H":        true,
	"GBK-EUC-V":        true,
	"GBK2K-H":          true,
	"GBK2K-V":          true,
	"GBKp-EUC-H":       true,
	"GBKp-EUC-V":       true,
	"GBpc-EUC-H":       true,
	"GBpc-EUC-V":       true,
	"H":                true,
	"HKscs-B5-H":       true,
	"HKscs-B5-V":       true,
	"Identity-H":       true,
	"Identity-V":       true,
	"KSC-EUC-H":        true,
	"KSC-EUC-V":        true,
	"KSCms-UHC-H":      true,
	"KSCms-UHC-HW-H":   true,
	"KSCms-UHC-HW-V":   true,
	"KSCms-UHC-V":      true,
	"KSCpc-EUC-H":      true,
	"UniCNS-UCS2-H":    true,
	"UniCNS-UCS2-V":    true,
	"UniCNS-UTF16-H":   true,
	"UniCNS-UTF16-V":   true,
	"UniGB-UCS2-H":     true,
	"UniGB-UCS2-V":     true,
	"UniGB-UTF16-H":    true,
	"UniGB-UTF16-V":    true,
	"UniJIS-UCS2-H":    true,
	"UniJIS-UCS2-HW-H": true,
	"UniJIS-UCS2-HW-V": true,
	"UniJIS-UCS2-V":    true,
	"UniJIS-UTF16-H":   true,
	"UniJIS-UTF16-V":   true,
	"UniKS-UCS2-H":     true,
	"UniKS-UCS2-V":     true,
	"UniKS-UTF16-H":    true,
	"UniKS-UTF16-V":    true,
	"V":                true,
}

// isCIDSystemInfoShapedCMapName reports whether name has the form
// "Adobe-<Registry>-<Supplement>" (e.g. "Adobe-Korea1-2"): a CIDSystemInfo
// identifier, never a valid predefined CMap name from ISO 32000-1 Table 118.
// A /UseCMap or /Encoding given such a name references a CMap that is neither
// predefined nor embedded.
func isCIDSystemInfoShapedCMapName(name string) bool {
	if name == "" || name[len(name)-1] < '0' || name[len(name)-1] > '9' {
		return false
	}
	parts := strings.Split(name, "-")
	return len(parts) == 3 && parts[0] == "Adobe"
}

func isPredefinedCMap(name string) bool {
	if name == "Identity-H" || name == "Identity-V" {
		return true
	}
	// Reject CIDSystemInfo ordering identifiers like "Adobe-Korea1-2" which
	// match the pattern "Adobe-<Registry>-<Supplement>" (ending in a digit).
	// These are NOT predefined CMap names from Table 118.
	if len(name) > 0 && name[len(name)-1] >= '0' && name[len(name)-1] <= '9' {
		// CIDSystemInfo names follow the pattern Adobe-<Registry>-<Supplement>.
		parts := strings.Split(name, "-")
		if len(parts) == 3 && parts[0] == "Adobe" {
			return false
		}
	}
	if table116CMapNames[name] {
		return true
	}
	for _, prefix := range predefinedCMapPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// checkCIDToGIDMap validates that Type 2 CIDFonts have a /CIDToGIDMap entry (rule 3).
// visible reports whether the font is used for visible rendering: PDF/A-1's
// 6.3.3.2 t1 exempts an invisible font (renderingMode == 3).
func checkCIDToGIDMap(ctx context.Context, doc *document.Document, cidFontDict ast.PdfDict, cidSubtype, fontName string, pageNum int, profile string, visible bool) []Violation {
	rule := cidToGIDMapRule(profile)
	if rule.Specification == "" {
		return nil
	}

	// Only applies to CIDFontType2.
	if cidSubtype != "CIDFontType2" {
		return nil
	}

	// The two escapes are not the same escape, and litho applied part 2's to
	// part 1 as well. ISO 19005-2/3 §6.2.11.3.2 t1 and ISO 19005-4 §6.2.10.3.2
	// t1 read "containsFontFile == false": CIDToGIDMap maps CIDs into an
	// embedded TrueType program's glyph space (ISO 32000-1 §9.7.4, Table 117),
	// so a CIDFontType2 with no program has nothing to map and fails the
	// embedding rule instead. ISO 19005-1 §6.3.3.2 t1 reads "renderingMode == 3"
	// and exempts no unembedded font: pdfjs issue7835.pdf shows an unembedded
	// Arial-Black CIDFontType2 with no /CIDToGIDMap and veraPDF reports it.
	if PDFAPart(profile) != 1 {
		fd := document.ResolveFontDescriptor(ctx, doc, cidFontDict)
		if fd == nil || !isFontEmbedded(ctx, doc, fd, fontFileKeys(cidSubtype), PDFAPart(profile)) {
			return nil
		}
	}

	// PDF/A-1 6.3.3.2 t1 carries a renderingMode == 3 escape: a font used only
	// invisibly is exempt. Parts 2-4 instead key the escape on font embedding,
	// handled by the callers that already gate on visibility.
	if PDFAPart(profile) == 1 && !visible {
		return nil
	}

	cidToGIDObj, ok := cidFontDict["CIDToGIDMap"]
	if !ok {
		return failf(rule, "font %s on page %d", fontName, pageNum)
	}

	// CIDToGIDMap can be the name "Identity" or a stream.
	resolved, err := document.ResolveObject(ctx, doc, cidToGIDObj)
	if err != nil {
		doc.Logger.Debug("compliance: fonts: failed to resolve CIDToGIDMap",
			"font", fontName, "error", err)
		return failf(rule, "font %s on page %d (resolve error)", fontName, pageNum)
	}

	switch v := resolved.(type) {
	case ast.PdfName:
		if v == "Identity" {
			return nil
		}
		return failf(rule, "font %s on page %d CIDToGIDMap=%s", fontName, pageNum, v)
	case ast.PdfStream:
		return nil
	default:
		return failf(rule, "font %s on page %d CIDToGIDMap has invalid type", fontName, pageNum)
	}
}

// checkCIDSystemInfo validates CIDSystemInfo consistency between CIDFont and
// CMap dictionaries (rule 4).
func checkCIDSystemInfo(ctx context.Context, doc *document.Document, type0Dict, cidFontDict ast.PdfDict, fontName string, pageNum int, profile string) []Violation {
	rule := cidSystemInfoRule(profile)
	if rule.Specification == "" {
		return nil
	}

	// If encoding is Identity-H or Identity-V, any CIDSystemInfo values are valid.
	if hasIdentityCMap(ctx, doc, type0Dict) {
		return nil
	}

	// Get CIDFont CIDSystemInfo.
	cidSysInfo, err := document.ResolveDict(ctx, doc, cidFontDict["CIDSystemInfo"])
	if err != nil || cidSysInfo == nil {
		doc.Logger.Debug("compliance: fonts: CIDFont missing CIDSystemInfo",
			"font", fontName, "error", err)
		return nil
	}

	cidRegistry, _ := document.ResolveString(ctx, doc, cidSysInfo["Registry"])
	cidOrdering, _ := document.ResolveString(ctx, doc, cidSysInfo["Ordering"])
	cidSupplement, _ := document.ResolveInt(ctx, doc, cidSysInfo["Supplement"])

	// Get CMap CIDSystemInfo from either the embedded CMap stream or a predefined CMap name.
	encObj, ok := type0Dict["Encoding"]
	if !ok {
		return nil
	}

	var cmapRegistry, cmapOrdering ast.PdfString
	var cmapSupplement ast.PdfInt

	cmapStream, err := document.ResolveStream(ctx, doc, encObj)
	if err == nil {
		// Embedded CMap stream: read CIDSystemInfo from the stream dict.
		cmapSysInfo, err := document.ResolveDict(ctx, doc, cmapStream.Dict["CIDSystemInfo"])
		if err != nil || cmapSysInfo == nil {
			return failf(rule, "font %s on page %d: embedded CMap stream missing CIDSystemInfo",
				fontName, pageNum)
		}
		cmapRegistry, _ = document.ResolveString(ctx, doc, cmapSysInfo["Registry"])  // Best-effort: empty string on failure triggers mismatch below.
		cmapOrdering, _ = document.ResolveString(ctx, doc, cmapSysInfo["Ordering"])  // Best-effort: empty string on failure triggers mismatch below.
		cmapSupplement, _ = document.ResolveInt(ctx, doc, cmapSysInfo["Supplement"]) // Best-effort: zero on failure triggers mismatch below.
	} else {
		// Named (predefined) CMap: its CIDSystemInfo (Registry, Ordering,
		// Supplement) is fixed by ISO 32000-1 Table 118 and Adobe's registered
		// CMap resources, resolved by [cmap.PredefinedCMapCIDSystemInfo]. The
		// predefined encoding names (UniKS-UTF16-H, 90ms-RKSJ-H, ...) do not
		// follow a Registry-Ordering-Supplement pattern, so the CIDSystemInfo
		// comes from the tabulated resource, not the name text. A non-Identity
		// name that is neither embedded nor a real predefined CMap (including
		// CIDSystemInfo-shaped names like "Adobe-Korea1-2") supplies no resolvable
		// CIDSystemInfo, so the §6.2.11.3.1 consistency check cannot establish the
		// required Registry/Ordering relationship and fails.
		encName, _ := document.ResolveName(ctx, doc, encObj)
		// The tabulated CIDSystemInfo of some predefined CMaps changed with the
		// PDF generation (ISO 32000-2 widened UniKS/UniGB/UniJIS to newer Adobe
		// character collections), so select by profile: PDF/A-4 and PDF/UA-2 are
		// ISO 32000-2, the rest ISO 32000-1.
		pdf20 := PDFAPart(profile) == 4 || (IsPDFUA(profile) && PDFUAPart(profile) == 2)
		reg, ord, sup, ok := cmap.PredefinedCMapCIDSystemInfo(string(encName), pdf20)
		if !ok {
			return failf(rule, "font %s on page %d: encoding %q is neither embedded nor a predefined CMap",
				fontName, pageNum, string(encName))
		}
		cmapRegistry = ast.PdfString(reg)
		cmapOrdering = ast.PdfString(ord)
		cmapSupplement = ast.PdfInt(sup)
	}

	// Registry and Ordering must match.
	if string(cidRegistry) != string(cmapRegistry) || string(cidOrdering) != string(cmapOrdering) {
		return failf(rule, "font %s on page %d (CIDFont Registry=%s Ordering=%s, CMap Registry=%s Ordering=%s)",
			fontName, pageNum, cidRegistry, cidOrdering, cmapRegistry, cmapOrdering)
	}

	// CIDFont Supplement must be ≤ CMap Supplement (parts 2+ only;
	// PDF/A-1 §6.3.3.1 only requires Registry/Ordering match).
	// PDF/UA profiles also require Supplement check.
	part := PDFAPart(profile)
	checkSupplement := part >= 2 || IsPDFUA(profile)
	if checkSupplement && cidSupplement > cmapSupplement {
		return failf(rule, "font %s on page %d: CIDFont Supplement (%d) > CMap Supplement (%d)",
			fontName, pageNum, cidSupplement, cmapSupplement)
	}

	return nil
}

// checkCMapEmbedding validates CMap-related rules for Type0 fonts:
//   - t1: Non-predefined CMaps must be embedded as streams
//   - t2: Embedded CMap WMode dict value must match stream program value
//   - t3: Embedded CMap must not reference (usecmap) non-predefined CMaps
func checkCMapEmbedding(ctx context.Context, doc *document.Document, type0Dict ast.PdfDict, fontName string, pageNum int, profile string) []Violation {
	encObj, ok := type0Dict["Encoding"]
	if !ok {
		return nil
	}

	// Encoding is either a name (predefined) or a stream (embedded CMap).
	encName, _ := document.ResolveName(ctx, doc, encObj)
	encStream, streamErr := document.ResolveStream(ctx, doc, encObj)

	// Select rules based on profile.
	ruleT1, ruleT2, ruleT3 := cmapEmbeddingRules(profile)
	if ruleT1.Specification == "" {
		return nil
	}

	// t1: If encoding is a name and NOT predefined, it must be embedded.
	// For PDF/UA-2 (ISO 32000-2 Table 116) and PDF/A-1 (§6.3.3.3), only
	// Identity-H and Identity-V are exempt: those parts require every other CMap,
	// including the predefined CJK CMaps, to be embedded, so a predefined CJK
	// CMap name that is not embedded fails t1.
	if streamErr != nil && string(encName) != "" {
		predefined := isPredefinedCMap(string(encName))
		if PDFAPart(profile) == 1 {
			// ISO 19005-1, 6.3.3.3 admits only the Identity CMaps.
			predefined = encName == "Identity-H" || encName == "Identity-V"
		}
		if !predefined {
			return failf(ruleT1,
				"font %s on page %d: CMap %q is not predefined and not embedded",
				fontName, pageNum, encName)
		}
		return nil
	}

	if streamErr != nil || encStream.Dict == nil {
		return nil
	}

	var results []Violation

	// PDF/A-1 §6.3.3.3 requires EVERY CMap used in the file, except Identity-H/V,
	// to be embedded, including a base CMap that an embedded CMap pulls in via
	// /UseCMap. A /UseCMap given as a NAME (rather than an embedded stream)
	// references a non-embedded predefined CMap, so a non-Identity name fails t1.
	// A named /UseCMap base references a non-embedded CMap. Whether that fails
	// the embedding rule depends on the profile.
	if useName, _ := document.ResolveName(ctx, doc, encStream.Dict["UseCMap"]); useName != "" {
		// Part 1 (§6.3.3.3) requires every non-Identity CMap embedded, so any
		// named /UseCMap base fails. Parts 2+/4 (§6.2.11.3.3 / §6.2.10.3.3) and
		// PDF/UA (§7.21.3.3 / §8.4.5.4) permit the Table-118 predefined CMaps by
		// name, so only a NON-predefined named base fails the embedding rule t1.
		// litho's predefined-CMap prefix list is incomplete for legacy names
		// (H, KSC-EUC-H, ...), so `!isPredefinedCMap` over-fires on a real
		// predefined base; fire only on a CIDSystemInfo-shaped name
		// ("Adobe-<Registry>-<Supplement>"), which is never a Table-118 CMap name
		// (veraPDF fires §7.21.3.3 t1 / §8.4.5.4 t1 on /UseCMap /Adobe-Korea1-2).
		var unembedded bool
		switch {
		case PDFAPart(profile) == 1:
			unembedded = useName != "Identity-H" && useName != "Identity-V"
		case PDFAPart(profile) >= 2 || IsPDFUA(profile):
			unembedded = isCIDSystemInfoShapedCMapName(string(useName))
		}
		if unembedded {
			results = append(results, failf(ruleT1,
				"font %s on page %d: CMap uses non-embedded CMap %q via /UseCMap",
				fontName, pageNum, useName)...)
		}
	}

	// t2: WMode in dict must match WMode in stream program.
	// Both default to 0 when absent.
	dictWMode, _ := document.ResolveInt(ctx, doc, encStream.Dict["WMode"])
	data, err := doc.DecodeStream(ctx, encStream)
	if err == nil && len(data) > 0 {
		streamWMode := max(ExtractCMapStreamWMode(data),
			// default WMode per spec
			0)
		if int(dictWMode) != streamWMode {
			results = append(results, failf(ruleT2,
				"font %s on page %d: CMap dict WMode=%d but stream WMode=%d",
				fontName, pageNum, dictWMode, streamWMode)...)
		}

		// t3: Check for usecmap referencing non-predefined CMaps.
		// Check stream program text, /UseCMap dict key, and recursively
		// follow embedded CMap chains for non-predefined references.
		usedCMap := findNonPredefinedUseCMap(ctx, doc, data, encStream.Dict, profile, 0)
		if usedCMap != "" {
			// PDF/A-1 §6.3.3.3 has no usecmap rule (ruleT3 is empty there); the
			// central zero-Rule filter drops it, so an embedded CMap's usecmap
			// chain is not reported under part 1.
			results = append(results, failf(ruleT3,
				"font %s on page %d: CMap chain references non-predefined CMap %q",
				fontName, pageNum, usedCMap)...)
		}
	}

	return results
}

// checkCMapCIDValues validates §6.1.12 t10 / §6.1.13 t10: CID values in
// an embedded encoding CMap must not exceed 65,535.
func checkCMapCIDValues(ctx context.Context, doc *document.Document, type0Dict ast.PdfDict, fontName string, pageNum int, profile string) []Violation {
	rule := cmapCIDRule(profile)
	if rule.Specification == "" {
		return nil
	}

	encObj, ok := type0Dict["Encoding"]
	if !ok {
		return nil
	}

	encStream, err := document.ResolveStream(ctx, doc, encObj)
	if err != nil || encStream.Dict == nil {
		// Not an embedded CMap stream (could be a predefined name).
		return nil
	}

	data, err := doc.DecodeStream(ctx, encStream)
	if err != nil || len(data) == 0 {
		return nil
	}

	cidCMap, err := cmap.ParseCID(ctx, data)
	if err != nil || cidCMap == nil {
		return nil
	}

	for _, cidVal := range cidCMap.Mapping {
		if cidVal > maxCID {
			return failf(rule,
				"font %s on page %d: CID value %d exceeds maximum 65,535",
				fontName, pageNum, cidVal)
		}
	}

	return nil
}

// maxUseCMapChainDepth bounds how far the embedded usecmap/UseCMap chain is
// followed when reading attacker-controlled CMap stream data. Anti-DoS guard,
// not spec-mandated: real usecmap chains are 1-2 deep.
const maxUseCMapChainDepth = 5

// findNonPredefinedUseCMap checks a CMap (stream data + dict) for references to
// non-predefined CMaps, recursively following embedded CMap chains.
// Returns the first non-predefined CMap name found, or "" if all are predefined.
func findNonPredefinedUseCMap(ctx context.Context, doc *document.Document, data []byte, cmapDict ast.PdfDict, profile string, depth int) string {
	if depth > maxUseCMapChainDepth {
		return ""
	}

	// Check usecmap keyword in stream program text. A name scraped from the
	// program can pick up a stray token, so it is trusted only when it is
	// CMap-shaped; a name read from an embedded CMap's own /CMapName is
	// authoritative and carries no such doubt.
	usedCMap := extractUseCMap(data)
	authoritative := false
	// Also check /UseCMap dict key.
	if usedCMap == "" && cmapDict != nil {
		if ucObj := cmapDict["UseCMap"]; ucObj != nil {
			ucName, _ := document.ResolveName(ctx, doc, ucObj)
			if ucName != "" {
				usedCMap = string(ucName)
			}
			// If /UseCMap is an embedded stream, check the referenced CMap's
			// name. Even embedded CMaps must have predefined names per the spec.
			if ucStream, err := document.ResolveStream(ctx, doc, ucObj); err == nil {
				// Extract the CMap name from /CMapName in the stream dict,
				// or from the program text (/CMapName /<name> def).
				cmapName := extractCMapName(ctx, doc, ucStream)
				if cmapName != "" {
					usedCMap = cmapName
					authoritative = true
				}
				// If no name found, also recurse to check the inner CMap's
				// own /UseCMap references.
				if usedCMap == "" {
					innerData, _ := doc.DecodeStream(ctx, ucStream)
					if inner := findNonPredefinedUseCMap(ctx, doc, innerData, ucStream.Dict, profile, depth+1); inner != "" {
						return inner
					}
					return ""
				}
			}
		}
	}

	if usedCMap == "" || (!authoritative && !strings.Contains(usedCMap, "-")) {
		return ""
	}

	predefined := isPredefinedCMap(usedCMap)
	if profile == "PDF/UA-2" {
		predefined = usedCMap == "Identity-H" || usedCMap == "Identity-V"
	}
	if !predefined {
		return usedCMap
	}
	return ""
}

// extractCMapName extracts the CMap name from an embedded CMap stream.
// Checks /CMapName in the stream dict first, then parses the program text
// for "/CMapName /<name> def".
func extractCMapName(ctx context.Context, doc *document.Document, stream ast.PdfStream) string {
	// Check dict key.
	if stream.Dict != nil {
		if name, _ := document.ResolveName(ctx, doc, stream.Dict["CMapName"]); name != "" {
			return string(name)
		}
	}
	// Parse stream program text.
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil || len(data) == 0 {
		return ""
	}
	s := string(data)
	_, after, ok := strings.Cut(s, "/CMapName")
	if !ok {
		return ""
	}
	after = strings.TrimLeft(after, " \t\r\n")
	if len(after) == 0 || after[0] != '/' {
		return ""
	}
	// Extract name after '/'.
	name := after[1:]
	end := strings.IndexAny(name, " \t\r\n")
	if end >= 0 {
		name = name[:end]
	}
	return name
}

// ExtractCMapStreamWMode parses a CMap program for "/WMode N def" and returns N,
// or -1 when the program declares none.
//
// Remediation shares this parse so the value it writes into the CMap stream
// dictionary is the value the detector compares against
// (ISO 19005-4, 6.2.10.3.3 test 2).
// Returns -1 if not found.
func ExtractCMapStreamWMode(data []byte) int {
	s := string(data)
	_, after, ok := strings.Cut(s, "/WMode")
	if !ok {
		return -1
	}
	// Skip "/WMode" and any whitespace.
	rest := after
	rest = strings.TrimLeft(rest, " \t\r\n")
	if len(rest) == 0 {
		return -1
	}
	// Parse integer.
	if rest[0] >= '0' && rest[0] <= '9' {
		return int(rest[0] - '0')
	}
	return -1
}

// extractUseCMap parses a CMap program for "/<name> usecmap" and returns the name.
func extractUseCMap(data []byte) string {
	s := string(data)
	before, _, ok := strings.Cut(s, "usecmap")
	if !ok {
		return ""
	}
	// Search backwards for the CMap name (starts with /).
	before = strings.TrimRight(before, " \t\r\n")
	lastSlash := strings.LastIndex(before, "/")
	if lastSlash < 0 {
		return ""
	}
	name := strings.TrimSpace(before[lastSlash+1:])
	return name
}
