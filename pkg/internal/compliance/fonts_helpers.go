package compliance

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// standard14Fonts is the set of PDF standard 14 font base names.
// PDF/A does NOT exempt these from embedding, but they are used
// for certain structural checks.
//
//nolint:gochecknoglobals // constant lookup table.
var standard14Fonts = map[string]bool{
	"Times-Roman":           true,
	"Times-Bold":            true,
	"Times-Italic":          true,
	"Times-BoldItalic":      true,
	"Helvetica":             true,
	"Helvetica-Bold":        true,
	"Helvetica-Oblique":     true,
	"Helvetica-BoldOblique": true,
	"Courier":               true,
	"Courier-Bold":          true,
	"Courier-Oblique":       true,
	"Courier-BoldOblique":   true,
	"Symbol":                true,
	"ZapfDingbats":          true,
}

// fontEmbeddingRule returns the font embedding rule for the given profile.
func fontEmbeddingRule(profile string) Rule {
	if IsPDFUA(profile) {
		if PDFUAPart(profile) == 2 {
			return Rule_14289_2_8_4_5_5_1_t01
		}
		return Rule_14289_1_7_21_4_1_t01
	}
	return ruleByPart(PDFAPart(profile), Rule_19005_1_6_3_4_t01, Rule_19005_2_6_2_11_4_1_t01, Rule_19005_3_6_2_11_4_1_t01, Rule_19005_4_6_2_10_4_1_t01)
}

// toUnicodeRule returns the ToUnicode mapping rule for the given profile.
// Returns a zero Rule if the check does not apply (Level B never
// requires Unicode mapping).
func toUnicodeRule(profile string) Rule {
	if IsPDFUA(profile) {
		if PDFUAPart(profile) == 2 {
			return Rule_14289_2_8_4_5_8_t01
		}
		return Rule_14289_1_7_21_7_t01
	}
	if !RequiresUnicodeMapping(profile) {
		return Rule{}
	}
	switch PDFAPart(profile) {
	case 1:
		return Rule_19005_1_6_3_8_t01
	case 2:
		return Rule_19005_2_6_2_11_7_2_t01
	case 3:
		return Rule_19005_3_6_2_11_7_2_t01
	case 4:
		// No part-4 presence requirement: every PDFA-4 profile's only
		// ToUnicode rule (6.2.10.7 t1) starts `toUnicode == null ||`,
		// so a missing map passes and only present values are tested
		// (see toUnicodeCMapValuesRule).
		return Rule{}
	}
	return Rule{}
}

// glyphWidthRule returns the glyph width consistency rule.
func glyphWidthRule(profile string) Rule {
	if IsPDFUA(profile) {
		if PDFUAPart(profile) == 2 {
			return Rule_14289_2_8_4_5_6_t01
		}
		return Rule_14289_1_7_21_5_t01
	}
	return ruleByPart(PDFAPart(profile), Rule_19005_1_6_3_6_t01, Rule_19005_2_6_2_11_5_t01, Rule_19005_3_6_2_11_5_t01, Rule_19005_4_6_2_10_5_t01)
}

// notdefRule returns the .notdef glyph rule.
// Returns a zero Rule for part 1 (not applicable).
func notdefRule(profile string) Rule {
	if IsPDFUA(profile) {
		if PDFUAPart(profile) == 2 {
			return Rule_14289_2_8_4_5_9_t01
		}
		return Rule_14289_1_7_21_8_t01
	}
	return ruleByPart(PDFAPart(profile), Rule{}, Rule_19005_2_6_2_11_8_t01, Rule_19005_3_6_2_11_8_t01, Rule_19005_4_6_2_10_9_t01)
}

// trueTypeEncodingNonSymbolicRule returns the TrueType non-symbolic encoding rule.
func trueTypeEncodingNonSymbolicRule(profile string) Rule {
	if IsPDFUA(profile) {
		if PDFUAPart(profile) == 2 {
			return Rule_14289_2_8_4_5_7_t02
		}
		return Rule_14289_1_7_21_6_t02
	}
	return ruleByPart(PDFAPart(profile), Rule_19005_1_6_3_7_t01, Rule_19005_2_6_2_11_6_t02, Rule_19005_3_6_2_11_6_t02, Rule_19005_4_6_2_10_6_t02)
}

// trueTypeEncodingSymbolicNoEncodingRule returns the rule that symbolic TrueType
// fonts must not specify /Encoding.
func trueTypeEncodingSymbolicNoEncodingRule(profile string) Rule {
	if IsPDFUA(profile) {
		if PDFUAPart(profile) == 2 {
			return Rule_14289_2_8_4_5_7_t03
		}
		return Rule_14289_1_7_21_6_t03
	}
	return ruleByPart(PDFAPart(profile), Rule_19005_1_6_3_7_t02, Rule_19005_2_6_2_11_6_t03, Rule_19005_3_6_2_11_6_t03, Rule_19005_4_6_2_10_6_t03)
}

// trueTypeSymbolicCmapRule returns the rule for symbolic TrueType cmap requirements.
func trueTypeSymbolicCmapRule(profile string) Rule {
	if IsPDFUA(profile) {
		if PDFUAPart(profile) == 2 {
			return Rule_14289_2_8_4_5_7_t04
		}
		return Rule_14289_1_7_21_6_t04
	}
	return ruleByPart(PDFAPart(profile), Rule_19005_1_6_3_7_t03, Rule_19005_2_6_2_11_6_t04, Rule_19005_3_6_2_11_6_t04, Rule_19005_4_6_2_10_6_t04)
}

// trueTypeNonSymbolicCmapRule returns the rule for non-symbolic TrueType cmap
// requirements. PDF/A-1 states no such requirement, so it returns the zero Rule
// there and the caller skips the check.
func trueTypeNonSymbolicCmapRule(profile string) Rule {
	if IsPDFUA(profile) {
		if PDFUAPart(profile) == 2 {
			return Rule_14289_2_8_4_5_7_t01
		}
		return Rule_14289_1_7_21_6_t01
	}
	return ruleByPart(PDFAPart(profile), Rule{}, Rule_19005_2_6_2_11_6_t01, Rule_19005_3_6_2_11_6_t01, Rule_19005_4_6_2_10_6_t01)
}

// toUnicodeCMapValuesRule returns the ToUnicode CMap values rule.
// The check only applies to profiles that require text-to-Unicode
// mapping (Level A under Part 1, Levels U/A under Parts 2/3, and all
// PDF/A-4 profiles); Level B never requires Unicode mapping and so
// never triggers it. Parts 2/3 key the forbidden-value test (U+0000,
// U+FFFE, U+FEFF) as §6.2.11.7.2 t2: that test belongs to the
// ToUnicode CMap value requirement, not to the §6.2.11.7.3 ActualText
// requirement.
func toUnicodeCMapValuesRule(profile string) Rule {
	if IsPDFUA(profile) {
		if PDFUAPart(profile) == 2 {
			return Rule_14289_2_8_4_5_8_t02
		}
		return Rule_14289_1_7_21_7_t02
	}
	if !RequiresUnicodeMapping(profile) {
		return Rule{}
	}
	return ruleByPart(PDFAPart(profile), Rule{}, Rule_19005_2_6_2_11_7_2_t02, Rule_19005_3_6_2_11_7_2_t02, Rule_19005_4_6_2_10_7_t01)
}

// puaActualTextRule returns the rule that PUA characters must carry
// an ActualText entry. ActualText is a tagged-content concept (Marked
// Content BDC operands), so the rule only fires for Level A profiles.
// Level U requires Unicode mapping but not tagging; Level B requires
// neither.
func puaActualTextRule(profile string) Rule {
	if IsPDFUA(profile) {
		// ISO 14289-2 §8.4.3 requires real content mapped to PUA to carry an
		// ActualText or Alt entry. PDF/UA-1 has no equivalent rule wired here.
		if PDFUAPart(profile) == 2 {
			return Rule_14289_2_8_4_3_t01
		}
		return Rule{}
	}
	if PDFALevel(profile) != 'a' {
		return Rule{}
	}
	return ruleByPart(PDFAPart(profile), Rule{}, Rule_19005_2_6_2_11_7_3_t01, Rule_19005_3_6_2_11_7_3_t01, Rule{})
}

// isType3Font returns true if the font subtype is "Type3".
func isType3Font(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	subtype, _ := document.ResolveName(ctx, doc, fontDict["Subtype"])
	return subtype == "Type3"
}

// validFontSubtypes lists the font /Subtype values permitted by the PDF/A font
// conformance clauses (ISO 19005-1 §6.3.2 t2, ISO 19005-2/3 §6.2.11.2 t2,
// ISO 19005-4 §6.2.10.2 t2).
//
//nolint:gochecknoglobals // static lookup table.
var validFontSubtypes = map[string]bool{
	"Type1": true, "MMType1": true, "TrueType": true, "Type3": true,
	"Type0": true, "CIDFontType0": true, "CIDFontType2": true,
}

// simpleFontSubtypes lists the /Subtype values that make a font dictionary a
// simple font, which is what veraPDF's PDSimpleFont covers. Type0 is composite
// and the CIDFontType values name a descendant, so none of them belong here,
// and a subtype outside the table is not a font type at all.
//
//nolint:gochecknoglobals // static lookup table.
var simpleFontSubtypes = map[string]bool{
	"Type1": true, "MMType1": true, "TrueType": true, "Type3": true,
}

// fontReqKeyRule selects the font required-key rule by PDF/A part and test
// number: ISO 19005-1 §6.3.2 tN, ISO 19005-2/3 §6.2.11.2 tN, ISO 19005-4
// §6.2.10.2 tN.
func fontReqKeyRule(part, test int) Rule {
	switch test {
	case 1:
		return ruleByPart(part, Rule_19005_1_6_3_2_t01, Rule_19005_2_6_2_11_2_t01, Rule_19005_3_6_2_11_2_t01, Rule_19005_4_6_2_10_2_t01)
	case 2:
		return ruleByPart(part, Rule_19005_1_6_3_2_t02, Rule_19005_2_6_2_11_2_t02, Rule_19005_3_6_2_11_2_t02, Rule_19005_4_6_2_10_2_t02)
	case 3:
		// Every part carries the rule, not just part 1: ISO 19005-2 and -3
		// §6.2.11.2 t3 and ISO 19005-4 §6.2.10.2 t3 each state it as
		// "Subtype == Type3 || fontName != null", the same shape as
		// ISO 19005-1 §6.3.2 t3.
		return ruleByPart(part, Rule_19005_1_6_3_2_t03, Rule_19005_2_6_2_11_2_t03, Rule_19005_3_6_2_11_2_t03, Rule_19005_4_6_2_10_2_t03)
	case 4:
		return ruleByPart(part, Rule_19005_1_6_3_2_t04, Rule_19005_2_6_2_11_2_t04, Rule_19005_3_6_2_11_2_t04, Rule_19005_4_6_2_10_2_t04)
	case 5:
		return ruleByPart(part, Rule_19005_1_6_3_2_t05, Rule_19005_2_6_2_11_2_t05, Rule_19005_3_6_2_11_2_t05, Rule_19005_4_6_2_10_2_t05)
	case 6:
		return ruleByPart(part, Rule_19005_1_6_3_2_t06, Rule_19005_2_6_2_11_2_t06, Rule_19005_3_6_2_11_2_t06, Rule_19005_4_6_2_10_2_t06)
	case 7:
		return ruleByPart(part, Rule_19005_1_6_3_2_t07, Rule_19005_2_6_2_11_2_t07, Rule_19005_3_6_2_11_2_t07, Rule_19005_4_6_2_10_2_t07)
	}
	return Rule{}
}

// namesAFontProgram reports whether a FontFile3 /Subtype names a glyph format
// at all, whatever the part permits.
//
// This is the embedding question, which is not the permission question.
// ISO 19005-1, 6.3.2 test 7 forbids OpenType, but an OpenType FontFile3 still
// holds a program a consumer can read, so veraPDF reports the font as embedded
// and answers the subtype separately. Asking isValidFontFileSubtype here made
// litho add a "font not embedded" finding veraPDF never raises, on six cells of
// the remediation differential.
func namesAFontProgram(subtype string) bool {
	switch subtype {
	case "Type1C", "CIDFontType0C", "OpenType":
		return true
	}
	return false
}

// isValidFontFileSubtype reports whether an embedded FontFile3 /Subtype is
// permitted: Type1C and CIDFontType0C for every part, plus OpenType for parts
// 2+ (ISO 19005-1 §6.3.2 t7 forbids OpenType; §6.2.11.2/§6.2.10.2 t7 allow it).
func isValidFontFileSubtype(part int, subtype string) bool {
	switch subtype {
	case "Type1C", "CIDFontType0C":
		return true
	case "OpenType":
		return part != 1
	}
	return false
}

// checkSimpleFontRequiredKeys validates the PDF/A required-key rules that apply
// to simple fonts only (PDSimpleFont): /FirstChar (t4), /LastChar (t5), and
// /Widths sized LastChar - FirstChar + 1 (t6). The standard 14 fonts are exempt,
// matching the isStandard escape in each rule. Callers must invoke it only for
// simple (non-Type0) fonts.
func checkSimpleFontRequiredKeys(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, part int) []Violation {
	if part == 0 || isStandard14Type1Font(ctx, doc, fontDict) {
		return nil
	}

	var results []Violation
	_, hasFirst := fontDict["FirstChar"]
	_, hasLast := fontDict["LastChar"]
	if !hasFirst {
		results = append(results, failf(fontReqKeyRule(part, 4), "simple font missing /FirstChar")...)
	}
	if !hasLast {
		results = append(results, failf(fontReqKeyRule(part, 5), "simple font missing /LastChar")...)
	}

	widths, err := document.ResolveArray(ctx, doc, fontDict["Widths"])
	if err != nil || widths == nil {
		results = append(results, failf(fontReqKeyRule(part, 6), "simple font missing /Widths")...)
		return results
	}
	// The clause reads Widths_size == LastChar - FirstChar + 1 with no escape
	// for a bound that is absent or unreadable, and a font missing one cannot
	// size its /Widths correctly whatever the array holds. veraPDF evaluates it
	// with the missing bound as zero, so a font declaring /LastChar 66 and no
	// /FirstChar is told to carry 67 entries, and one declaring /FirstChar 65
	// and no /LastChar is told to carry -64. Skipping those reported the missing
	// key alone and left the sizing requirement unanswered.
	first, errF := document.ResolveInt(ctx, doc, fontDict["FirstChar"])
	if errF != nil {
		first = 0
	}
	last, errL := document.ResolveInt(ctx, doc, fontDict["LastChar"])
	if errL != nil {
		last = 0
	}
	if expected := int(last - first + 1); len(widths) != expected {
		results = append(results, failf(fontReqKeyRule(part, 6),
			"simple font /Widths has %d entries, expected %d", len(widths), expected)...)
	}
	return results
}

// checkFontRequiredKeys validates the PDF/A font-dictionary required-key rules
// that apply to every font (PDFont): /Type must be Font (t1), /Subtype must be a
// recognised font subtype (t2), and /BaseFont must be present unless the font is
// a Type 3 font (t3, PDF/A-1 only).
func checkFontRequiredKeys(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, subtype string, part int) []Violation {
	if part == 0 {
		return nil
	}
	var results []Violation

	if typeName, _ := document.ResolveName(ctx, doc, fontDict["Type"]); typeName != "Font" {
		results = append(results, failf(fontReqKeyRule(part, 1), "font /Type is %q, not Font", typeName)...)
	}
	if !validFontSubtypes[subtype] {
		results = append(results, failf(fontReqKeyRule(part, 2), "font /Subtype %q is not a valid font subtype", subtype)...)
	}
	if subtype != "Type3" {
		if _, ok := fontDict["BaseFont"]; !ok {
			if rule := fontReqKeyRule(part, 3); rule.Specification != "" {
				results = append(results, failf(rule, "font missing /BaseFont")...)
			}
		}
	}

	// t7: an embedded FontFile3 program /Subtype must be a permitted value.
	if fd := document.ResolveFontDescriptor(ctx, doc, fontDict); fd != nil {
		if ff3, ok := fd["FontFile3"]; ok {
			if stream, err := document.ResolveStream(ctx, doc, ff3); err == nil {
				ffSubtype, _ := document.ResolveName(ctx, doc, stream.Dict["Subtype"])
				if ffSubtype != "" && !isValidFontFileSubtype(part, string(ffSubtype)) {
					results = append(results, failf(fontReqKeyRule(part, 7),
						"embedded font program /Subtype %q is not permitted", ffSubtype)...)
				}
			}
		}
	}
	return results
}

// isStandard14Type1Font reports whether the font is one of the 14 standard
// fonts used as such: a Type1 (or MMType1) font whose /BaseFont is a standard
// name. The standard 14 fonts are Type1 programs, so veraPDF's isStandard
// escape in ISO 19005-2 §6.2.11.2 t4-t6 (the PDType1Font standard-14 test) does
// not cover a /TrueType font that merely shares a standard name, e.g. an
// unembedded "Helvetica" TrueType: such a font is not standard and must carry
// /FirstChar, /LastChar, and a /Widths array of LastChar - FirstChar + 1.
func isStandard14Type1Font(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	if !isStandard14Font(ctx, doc, fontDict) {
		return false
	}
	sub, _ := document.ResolveName(ctx, doc, fontDict["Subtype"])
	return sub == "Type1" || sub == "MMType1"
}

// isStandard14Font returns true if the font's BaseFont is one of the 14
// standard PDF fonts.
func isStandard14Font(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	basefont, _ := document.ResolveName(ctx, doc, fontDict["BaseFont"])
	return standard14Fonts[string(basefont)]
}

// isFontEmbedded checks whether the font descriptor contains a FontFile,
// FontFile2, or FontFile3 stream indicating an embedded font program.
// fontFileKeys returns the FontDescriptor font-file keys that constitute valid
// embedding for a font of the given subtype, per ISO 32000-1 Table 126. A
// CIDFont must carry its program in the key matching its glyph format:
// CIDFontType0 (CFF) in /FontFile3, CIDFontType2 (TrueType) in /FontFile2 or an
// OpenType /FontFile3. /FontFile holds a Type1 program, valid only for a simple
// Type1 font, so it never embeds a CIDFont; counting it lets a mis-keyed
// CIDFont pass the embedding rule that veraPDF fails (§6.2.10.4.1 / §6.2.11.4.1).
func fontFileKeys(subtype string) []ast.PdfName {
	switch subtype {
	case "CIDFontType0":
		return []ast.PdfName{"FontFile3"}
	case "CIDFontType2":
		return []ast.PdfName{"FontFile2", "FontFile3"}
	default:
		return []ast.PdfName{"FontFile", "FontFile2", "FontFile3"}
	}
}

func isFontEmbedded(ctx context.Context, doc *document.Document, fd ast.PdfDict, validKeys []ast.PdfName, part int) bool {
	if fd == nil {
		return false
	}
	for _, key := range validKeys {
		if obj, ok := fd[key]; ok {
			resolved, err := document.ResolveObject(ctx, doc, obj)
			if err != nil {
				doc.Logger.Debug("compliance: fonts: failed to resolve font file entry",
					"key", key, "error", err)
				continue
			}
			if stream, isStream := resolved.(ast.PdfStream); isStream {
				// A /FontFile3 says which program it holds. One naming a subtype
				// the part does not define, or naming none at all, states no
				// glyph format, so it embeds nothing a consumer can read:
				// veraPDF reports the font as not embedded. /FontFile and
				// /FontFile2 have one format each and carry no such key.
				if key == "FontFile3" {
					subtype, _ := document.ResolveName(ctx, doc, stream.Dict["Subtype"])
					if !namesAFontProgram(string(subtype)) {
						continue
					}
				}
				return true
			}
			// Non-null, non-stream: treat as present but invalid (still "embedded"
			// for the purpose of the embedding check).
			if resolved != nil {
				if _, isNull := resolved.(ast.PdfNull); !isNull {
					return true
				}
			}
		}
	}
	return false
}

// isSymbolicFont returns true if the font's flags indicate a symbolic font
// (bit 3 set in the FontDescriptor Flags field).
func isSymbolicFont(ctx context.Context, doc *document.Document, fd ast.PdfDict) bool {
	if fd == nil {
		return false
	}
	flags, err := document.ResolveInt(ctx, doc, fd["Flags"])
	if err != nil {
		doc.Logger.Debug("compliance: fonts: failed to resolve Flags", "error", err)
		return false
	}
	const symbolicBit = 1 << 2 // bit 3 (0-indexed bit 2) = symbolic
	symbolic := (int(flags) & symbolicBit) != 0
	return symbolic
}

// fontBaseFont returns the /BaseFont name from a font dictionary.
func fontBaseFont(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) string {
	name, _ := document.ResolveName(ctx, doc, fontDict["BaseFont"])
	return string(name)
}

// fontSubtype returns the /Subtype name from a font dictionary.
func fontSubtype(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) string {
	name, _ := document.ResolveName(ctx, doc, fontDict["Subtype"])
	return string(name)
}

// isSubsetFont returns true if the font's BaseFont name starts with a
// 6-letter tag followed by '+' (e.g., "ABCDEF+Arial"), indicating a subset.
func isSubsetFont(basefont string) bool {
	if len(basefont) < 8 {
		return false
	}
	if basefont[6] != '+' {
		return false
	}
	for i := range 6 {
		c := basefont[i]
		if c < 'A' || c > 'Z' {
			return false
		}
	}
	return true
}

// hasToUnicode returns true if the font dictionary has a /ToUnicode entry.
func hasToUnicode(fontDict ast.PdfDict) bool {
	_, ok := fontDict["ToUnicode"]
	return ok
}

// checkFontEmbedding validates that a font program is embedded (rule 1).
func checkFontEmbedding(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, isType3 bool) []Violation {
	rule := fontEmbeddingRule(profile)
	if rule.Specification == "" {
		return nil
	}

	// Type 3 fonts have no external font program.
	if isType3 {
		return nil
	}

	fd := document.ResolveFontDescriptor(ctx, doc, fontDict)
	if fd == nil {
		// No FontDescriptor means not embedded. PDF/A requires embedding even
		// for the 14 standard fonts.
		return failf(rule, "font %s on page %d", fontName, pageNum)
	}

	if !isFontEmbedded(ctx, doc, fd, fontFileKeys(fontSubtype(ctx, doc, fontDict)), PDFAPart(profile)) {
		return failf(rule, "font %s on page %d", fontName, pageNum)
	}

	return nil
}

// checkNotdefInDifferences reports a .notdef entry in /Encoding /Differences
// only when the character code it maps is actually shown by a text operator.
// ISO 19005-2 §6.2.11.8 forbids a reference to .notdef "from any of the text
// showing operators"; veraPDF is usage-driven here. Keying off the mere
// presence of .notdef in the array (a common font-setup artifact for slots the
// content never draws) produced false positives, so the check requires the
// mapped code to appear in usage.
func checkNotdefInDifferences(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, rule Rule, usage *fontUsageInfo) []Violation {
	if usage == nil {
		return nil
	}
	encObj, ok := fontDict["Encoding"]
	if !ok {
		return nil
	}
	encDict, err := document.ResolveDict(ctx, doc, encObj)
	if err != nil || encDict == nil {
		return nil
	}
	diffArr, err := document.ResolveArray(ctx, doc, encDict["Differences"])
	if err != nil || diffArr == nil {
		return nil
	}
	code := 0
	for _, item := range diffArr {
		switch v := item.(type) {
		case ast.PdfInt:
			code = int(v)
		case ast.PdfName:
			if v == ".notdef" && code >= 0 && code <= 255 && usage.charCodes[byte(code)] { //nolint:gosec // G115: code is bounded to 0..255 by the guard.
				return failf(rule, "font %s on page %d: charCode %d maps to .notdef via /Differences", fontName, pageNum, code)
			}
			code++
		}
	}
	return nil
}

// lookupCIDWidth returns the PDF-declared width for a CID from the /W array
// and /DW default of a CIDFont dict.
func lookupCIDWidth(ctx context.Context, doc *document.Document, cidFontDict ast.PdfDict, cid uint16) float64 {
	// Per ISO 32000-1 Table 117, any element of /W (including a width value)
	// may be an indirect reference; document.Float returns 0 on an unresolved
	// reference, so resolve every numeric read first. Real ZUGFeRD producers
	// emit [cidFirst cidLast <width ref>], which otherwise read as width 0 and
	// trip the empty-glyph .notdef heuristic on conforming files.
	num := func(o ast.PdfObject) float64 {
		r, _ := document.ResolveObject(ctx, doc, o)
		return document.Float(r)
	}

	// Default width.
	dw := num(cidFontDict["DW"])
	if dw == 0 {
		dw = 1000 // PDF spec default.
	}

	wObj, ok := cidFontDict["W"]
	if !ok {
		return dw
	}
	wArr, err := document.ResolveArray(ctx, doc, wObj)
	if err != nil || len(wArr) == 0 {
		return dw
	}

	cidInt := int(cid)
	for i := 0; i < len(wArr); {
		first := int(num(wArr[i]))
		if i+1 >= len(wArr) {
			break
		}
		// Format 1: [cidStart [w1 w2 ...]]
		if arr, err := document.ResolveArray(ctx, doc, wArr[i+1]); err == nil && arr != nil {
			for j, w := range arr {
				if first+j == cidInt {
					return num(w)
				}
			}
			i += 2
			continue
		}
		// Format 2: [cidFirst cidLast w]
		if i+2 >= len(wArr) {
			break
		}
		last := int(num(wArr[i+1]))
		if cidInt >= first && cidInt <= last {
			return num(wArr[i+2])
		}
		i += 3
	}
	return dw
}

// perGlyphToUnicodeRule selects the rule fired per used glyph whose Unicode
// mapping cannot be resolved. ISO 19005-2/3 §6.2.11.7.2 t1 governs both the
// missing-map and the unmapped-code case, so this is identical to the
// font-level selector [toUnicodeRule]. The same identity holds for PDF/UA
// (ISO 14289-1 §7.21.7 t1, ISO 14289-2 §8.4.5.8 t1).
func perGlyphToUnicodeRule(profile string) Rule {
	return toUnicodeRule(profile)
}
