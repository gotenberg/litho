package compliance

import (
	"context"
	"log/slog"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/typography/cmap"
)

// --- CMap-related rule selectors ---

// cidToGIDMapRule returns the CIDToGIDMap rule for the given profile.
// Returns a zero Rule if the check does not apply (part 1).
func cidToGIDMapRule(profile string) Rule {
	if isPDFUA(profile) {
		return Rule_14289_1_7_21_3_2_t01
	}
	return ruleByPart(pdfaPart(profile), Rule{}, Rule_19005_2_6_2_11_3_2_t01, Rule_19005_3_6_2_11_3_2_t01, Rule_19005_4_6_2_10_3_2_t01)
}

// cidSystemInfoRule returns the CIDSystemInfo consistency rule.
// Returns a zero Rule if the check does not apply (part 1).
func cidSystemInfoRule(profile string) Rule {
	if isPDFUA(profile) {
		return Rule_14289_1_7_21_3_1_t01
	}
	return ruleByPart(pdfaPart(profile), Rule_19005_1_6_3_3_1_t01, Rule_19005_2_6_2_11_3_1_t01, Rule_19005_3_6_2_11_3_1_t01, Rule_19005_4_6_2_10_3_1_t01)
}

// cmapEmbeddingRules returns the 3 CMap embedding rules (t1: non-predefined must
// be embedded, t2: WMode consistency, t3: usecmap non-predefined forbidden).
func cmapEmbeddingRules(profile string) (t1, t2, t3 Rule) {
	if isPDFUA(profile) {
		if strings.Contains(profile, "2") {
			return Rule_14289_2_8_4_5_4_t01, Rule_14289_2_8_4_5_4_t02, Rule_14289_2_8_4_5_4_t03
		}
		return Rule_14289_1_7_21_3_3_t01, Rule_14289_1_7_21_3_3_t02, Rule_14289_1_7_21_3_3_t03
	}
	switch pdfaPart(profile) {
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
	return ruleByPart(pdfaPart(profile), Rule_19005_1_6_1_12_t10, Rule_19005_2_6_1_13_t10, Rule_19005_3_6_1_13_t10, Rule{})
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

func isPredefinedCMap(name string) bool {
	if name == "Identity-H" || name == "Identity-V" {
		return true
	}
	// Reject CIDSystemInfo ordering identifiers like "Adobe-Korea1-2" which
	// match the pattern "Adobe-<Registry>-<Supplement>" (ending in a digit).
	// These are NOT predefined CMap names from Table 118.
	if len(name) > 0 && name[len(name)-1] >= '0' && name[len(name)-1] <= '9' {
		// Check if it looks like a CIDSystemInfo name (Registry-Ordering-Supplement).
		parts := strings.Split(name, "-")
		if len(parts) == 3 && parts[0] == "Adobe" {
			return false
		}
	}
	for _, prefix := range predefinedCMapPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

// --- CMap check implementations ---

// checkCIDToGIDMap validates that Type 2 CIDFonts have a /CIDToGIDMap entry (rule 3).
func checkCIDToGIDMap(ctx context.Context, doc *document.Document, cidFontDict ast.PdfDict, cidSubtype, fontName string, pageNum int, profile string) []Violation {
	rule := cidToGIDMapRule(profile)
	if rule.Specification == "" {
		return nil
	}

	// Only applies to CIDFontType2.
	if cidSubtype != "CIDFontType2" {
		return nil
	}

	cidToGIDObj, ok := cidFontDict["CIDToGIDMap"]
	if !ok {
		return failf(rule, "font %s on page %d", fontName, pageNum)
	}

	// CIDToGIDMap can be the name "Identity" or a stream.
	resolved, err := document.ResolveObject(ctx, doc, cidToGIDObj)
	if err != nil {
		slog.Debug("compliance: fonts: failed to resolve CIDToGIDMap",
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
		slog.Debug("compliance: fonts: CIDFont missing CIDSystemInfo",
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
		// Embedded CMap stream — read CIDSystemInfo from the stream dict.
		cmapSysInfo, err := document.ResolveDict(ctx, doc, cmapStream.Dict["CIDSystemInfo"])
		if err != nil || cmapSysInfo == nil {
			return failf(rule, "font %s on page %d: embedded CMap stream missing CIDSystemInfo",
				fontName, pageNum)
		}
		cmapRegistry, _ = document.ResolveString(ctx, doc, cmapSysInfo["Registry"])
		cmapOrdering, _ = document.ResolveString(ctx, doc, cmapSysInfo["Ordering"])
		cmapSupplement, _ = document.ResolveInt(ctx, doc, cmapSysInfo["Supplement"])
	} else {
		// Named (predefined) CMap — extract CIDSystemInfo from the name.
		// Predefined CMap names follow the pattern "Registry-Ordering-Supplement"
		// (e.g., "Adobe-Japan1-2", "Adobe-Korea1-0").
		encName, _ := document.ResolveName(ctx, doc, encObj)
		reg, ord, sup := parsePredefinedCMapName(string(encName))
		if reg == "" {
			// Non-Identity encoding that can't be resolved — the CIDSystemInfo
			// relationship can't be satisfied.
			return failf(rule, "font %s on page %d: encoding %q is not a valid predefined CMap",
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
	part := pdfaPart(profile)
	checkSupplement := part >= 2 || isPDFUA(profile)
	if checkSupplement && cidSupplement > cmapSupplement {
		return failf(rule, "font %s on page %d: CIDFont Supplement (%d) > CMap Supplement (%d)",
			fontName, pageNum, cidSupplement, cmapSupplement)
	}

	return nil
}

// parsePredefinedCMapName extracts Registry, Ordering, and Supplement from a
// predefined CMap name like "Adobe-Japan1-2". Returns empty strings if the
// name doesn't match the expected pattern.
func parsePredefinedCMapName(name string) (registry, ordering string, supplement int) {
	// Strip -H/-V suffix if present.
	base := name
	if strings.HasSuffix(base, "-H") || strings.HasSuffix(base, "-V") {
		base = base[:len(base)-2]
	}

	// Expected format: Registry-Ordering-Supplement (e.g., "Adobe-Japan1-2").
	// Find the last hyphen to split Supplement.
	lastDash := strings.LastIndex(base, "-")
	if lastDash < 0 {
		return "", "", 0
	}
	supStr := base[lastDash+1:]
	rest := base[:lastDash]

	// Parse Supplement as integer.
	sup := 0
	for _, ch := range supStr {
		if ch < '0' || ch > '9' {
			return "", "", 0
		}
		sup = sup*10 + int(ch-'0')
	}

	// Find the first hyphen to split Registry from Ordering.
	before, after, ok := strings.Cut(rest, "-")
	if !ok {
		return "", "", 0
	}

	return before, after, sup
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

	// Check if encoding is a name (not embedded) vs stream (embedded).
	encName, _ := document.ResolveName(ctx, doc, encObj)
	encStream, streamErr := document.ResolveStream(ctx, doc, encObj)

	// Select rules based on profile.
	ruleT1, ruleT2, ruleT3 := cmapEmbeddingRules(profile)
	if ruleT1.Specification == "" {
		return nil
	}

	// t1: If encoding is a name and NOT predefined, it must be embedded.
	// For PDF/UA-2 (ISO 32000-2 Table 116), only Identity-H/V are predefined.
	if streamErr != nil && string(encName) != "" {
		predefined := isPredefinedCMap(string(encName))
		if profile == "PDF/UA-2" {
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

	// t2: WMode in dict must match WMode in stream program.
	// Both default to 0 when absent.
	dictWMode, _ := document.ResolveInt(ctx, doc, encStream.Dict["WMode"])
	data, err := doc.DecodeStream(encStream)
	if err == nil && len(data) > 0 {
		streamWMode := max(extractCMapStreamWMode(data),
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

	data, err := doc.DecodeStream(encStream)
	if err != nil || len(data) == 0 {
		return nil
	}

	cidCMap, err := cmap.ParseCID(data)
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

// findNonPredefinedUseCMap checks a CMap (stream data + dict) for references to
// non-predefined CMaps, recursively following embedded CMap chains.
// Returns the first non-predefined CMap name found, or "" if all are predefined.
func findNonPredefinedUseCMap(ctx context.Context, doc *document.Document, data []byte, cmapDict ast.PdfDict, profile string, depth int) string {
	if depth > 5 {
		return ""
	}

	// Check usecmap keyword in stream program text.
	usedCMap := extractUseCMap(data)
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
				}
				// If no name found, also recurse to check the inner CMap's
				// own /UseCMap references.
				if usedCMap == "" {
					innerData, _ := doc.DecodeStream(ucStream)
					if inner := findNonPredefinedUseCMap(ctx, doc, innerData, ucStream.Dict, profile, depth+1); inner != "" {
						return inner
					}
					return ""
				}
			}
		}
	}

	if usedCMap == "" || !strings.Contains(usedCMap, "-") {
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
	data, err := doc.DecodeStream(stream)
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

// extractCMapStreamWMode parses a CMap program for "/WMode N def" and returns N.
// Returns -1 if not found.
func extractCMapStreamWMode(data []byte) int {
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
