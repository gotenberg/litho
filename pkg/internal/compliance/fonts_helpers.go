package compliance

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
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

// embedsFontProgram reports whether the font's descriptor carries a font
// program, whichever container names it.
//
// A standard 14 font is a shorthand for metrics a reader already holds, and
// ISO 32000-1, 9.6.2.2 makes /FirstChar, /LastChar, /Widths and /FontDescriptor
// all present or all absent for exactly that reason. Embedding a program leaves
// the shorthand behind: the font now carries its own glyphs, so it owes its own
// metrics, and veraPDF's isStandard is false for it.
//
// # Corpus evidence
//
//   - pdfjs issue11150_reduced.pdf: names /Symbol and embeds a /FontFile while
//     declaring none of /FirstChar, /LastChar and /Widths, and veraPDF reports
//     all of t4, t5 and t6.
func embedsFontProgram(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	fd := document.ResolveFontDescriptor(ctx, doc, fontDict)
	if fd == nil {
		return false
	}
	for _, key := range []ast.PdfName{"FontFile", "FontFile2", "FontFile3"} {
		if _, ok := fd[key]; ok {
			return true
		}
	}
	return false
}

// checkSimpleFontRequiredKeys validates the PDF/A required-key rules that apply
// to simple fonts only (PDSimpleFont): /FirstChar (t4), /LastChar (t5), and
// /Widths sized LastChar - FirstChar + 1 (t6). The standard 14 fonts are exempt,
// matching the isStandard escape in each rule. Callers must invoke it only for
// simple (non-Type0) fonts.
func checkSimpleFontRequiredKeys(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, part int) []Violation {
	if part == 0 || (isStandard14Type1Font(ctx, doc, fontDict) && !embedsFontProgram(ctx, doc, fontDict)) {
		return nil
	}

	var results []Violation
	// veraPDF reads /FirstChar and /LastChar as numbers (PDSimpleFont), so one
	// that resolves to null, or to no number, is missing (ISO 32000-1, 7.3.7 and
	// 7.3.10). Measured on a null and on a reference to a null object.
	hasFirst := numberKeyPresent(ctx, doc, fontDict, "FirstChar")
	hasLast := numberKeyPresent(ctx, doc, fontDict, "LastChar")
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

// type0NameDict returns the dictionary veraPDF reads a Type0 font's name from
// for the /BaseFont rule, ISO 19005-1, 6.3.2 t3 and ISO 19005-2, 6.2.11.2 t3:
// its descendant CIDFont, the first entry of /DescendantFonts, or nil, which
// names no font, when the font has none.
//
// veraPDF models a Type0 font as a CIDFont over its descendant's dictionary
// (PDType0Font is a PDCIDFont, and PDFont reads fontName from that dictionary's
// /BaseFont), so the rule's "fontName != null" asks about the descendant's
// /BaseFont. That relaxes ISO 32000-1, 9.7.5, Table 121, which requires the
// Type0 dictionary's own /BaseFont, to match the validator. Measured with
// veraPDF 1.30 at PDF/A-1b and PDF/A-2b on an Identity-H font over a TrueType
// CIDFont: a Type0 font whose own /BaseFont is absent, or is a string, passes
// the rule when its descendant's is a name. A descendant whose /BaseFont is
// absent or a string stops veraPDF's validation with an exception, so no count
// is known for it; litho states the rule for the Type0 font then.
//
// # Corpus evidence
//
//   - pdfjs issue12823.pdf: its NotoColorEmoji Type0 font names a /BaseFont
//     but no descendant, and veraPDF fails ISO 19005-1, 6.3.2 t3 and ISO
//     19005-2, 6.2.11.2 t3 for it at PDF/A-1b, PDF/A-2b and PDF/A-2u, where
//     litho failed neither.
func type0NameDict(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) ast.PdfDict {
	descArr, err := document.ResolveArray(ctx, doc, fontDict["DescendantFonts"])
	if err != nil || len(descArr) == 0 {
		return nil
	}
	cid, err := document.ResolveDict(ctx, doc, descArr[0])
	if err != nil {
		return nil
	}
	return cid
}

// checkFontRequiredKeys validates the PDF/A font-dictionary required-key rules
// that apply to every font (PDFont): /Type must be Font (t1), /Subtype must be a
// recognised font subtype (t2), and /BaseFont must name the font unless it is a
// Type 3 font (t3). Every part carries t3, stated as
// "Subtype == Type3 || fontName != null": ISO 19005-1 6.3.2 t3, ISO 19005-2 and
// -3 6.2.11.2 t3, ISO 19005-4 6.2.10.2 t3. A Type0 font's name is read from
// its descendant CIDFont ([type0NameDict]).
//
// # Corpus evidence
//
//   - pdfjs issue1350.pdf: writes both its fonts as
//     /BaseFont<feff004100410043...>, a UTF-16BE hex string, and veraPDF
//     reports each one with the font itself resolved to null.
func checkFontRequiredKeys(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, subtype string, part int) []Violation {
	if part == 0 {
		return nil
	}
	var results []Violation

	// Three defects reach this rule and only one of them is "the file names the
	// wrong type". [ast.PdfName] renders with a leading slash, so %q on the
	// empty name printed `"/"` and read as though the font carried a name
	// consisting of a slash, whether /Type was absent, null, or not a name at
	// all. ISO 32000-1, 7.3.9 makes a null entry equivalent to an absent one,
	// and [document.ResolveName] reports both as the empty name, so they state
	// the same defect. The /BaseFont branch below already draws this
	// distinction; draw it here too.
	typeObj, typePresent := fontDict["Type"]
	typeName, typeErr := document.ResolveName(ctx, doc, typeObj)
	switch {
	case typeErr != nil:
		results = append(results, failf(fontReqKeyRule(part, 1), "font /Type is not a name")...)
	case !typePresent || typeName == "":
		results = append(results, failf(fontReqKeyRule(part, 1), "font missing /Type")...)
	case typeName != "Font":
		results = append(results, failf(fontReqKeyRule(part, 1), "font /Type is %s, not Font", typeName)...)
	}
	if !validFontSubtypes[subtype] {
		results = append(results, failf(fontReqKeyRule(part, 2), "font /Subtype %q is not a valid font subtype", subtype)...)
	}
	if subtype != "Type3" {
		// ISO 32000-1, 9.6.2, Table 111 types /BaseFont as a name, and the test
		// every part states is "fontName != null", not "the key is present". A
		// value of any other type names no font, so it fails exactly as a
		// missing entry does, and veraPDF reports such a font as resolved to
		// null.
		named, subject := fontDict, "font "
		if subtype == "Type0" {
			named, subject = type0NameDict(ctx, doc, fontDict), "Type0 font read through its descendant CIDFont: "
		}
		baseFont, present := named["BaseFont"]
		_, nameErr := document.ResolveName(ctx, doc, baseFont)
		if !present || nameErr != nil {
			if rule := fontReqKeyRule(part, 3); rule.Specification != "" {
				if !present {
					results = append(results, failf(rule, "%smissing /BaseFont", subject)...)
				} else {
					results = append(results, failf(rule, "%s/BaseFont is not a name", subject)...)
				}
			}
		}
	}

	return append(results, fontFileSubtypeViolations(ctx, doc, fontDict, part, "")...)
}

// fontFileSubtypeViolations states t7 when the FontFile3 program the font's
// descriptor embeds names a /Subtype the part does not permit, prefixing the
// context with subject. A Type0 font reads the descriptor of its descendant
// ([document.ResolveFontDescriptor]), as veraPDF's PDType0Font does, and the
// descendant CIDFont is a PDFont of its own, so one program fails the rule
// for both ([fontJudgements.descendantRules]).
//
// # Corpus evidence
//
//   - litho fontcff2_2b.pdf and fontotread_2b.pdf: four Type0 fonts each over
//     an /OpenType program, and veraPDF 1.30.2 fails ISO 19005-1, 6.3.2 t7 14
//     and 12 times at PDF/A-1a and 1b, where litho failed it 10 and 8 times.
//   - litho, verapdf and facturx corpora: no other count of the rule moves
//     at any level.
func fontFileSubtypeViolations(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, part int, subject string) []Violation {
	fd := document.ResolveFontDescriptor(ctx, doc, fontDict)
	if fd == nil {
		return nil
	}
	ff3, ok := fd["FontFile3"]
	if !ok {
		return nil
	}
	stream, err := document.ResolveStream(ctx, doc, ff3)
	if err != nil {
		return nil
	}
	// Best-effort: a /Subtype that is no name reads as absent, which the
	// test passes.
	ffSubtype, _ := document.ResolveName(ctx, doc, stream.Dict["Subtype"])
	if ffSubtype == "" || isValidFontFileSubtype(part, string(ffSubtype)) {
		return nil
	}
	return failf(fontReqKeyRule(part, 7), "%sembedded font program /Subtype %q is not permitted", subject, string(ffSubtype))
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

// isFontEmbedded reports whether a font descriptor carries a font program a
// consumer can read, which is what the embedding rules ask (ISO 19005-2,
// 6.2.11.4.1 t1 and its restatements). fontSub is the font's own /Subtype, the
// descendant's for a CIDFont, and selects the keys that may hold the program.
func isFontEmbedded(ctx context.Context, doc *document.Document, fd ast.PdfDict, fontSub string, part int) bool {
	if fd == nil {
		return false
	}
	for _, key := range fontFileKeys(fontSub) {
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
					if !fontFile3ParsesForFont(ctx, doc, fontSub, string(subtype), obj, stream) {
						continue
					}
				}
				if !streamHoldsFontProgram(ctx, doc, key, obj, stream) {
					continue
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

// fontFile3ParsesForFont reports whether a /FontFile3 program sits in the
// container the consumer will read it as. ISO 32000-1, Table 126 names two: a
// bare CFF for "Type1C" and "CIDFontType0C", an sfnt for "OpenType".
//
// veraPDF picks the parser from the font, not from the stream.
// truetype.PDTrueTypeFont.getFontProgram calls parseTrueTypeFontProgram for
// /FontFile2 and /FontFile3 alike, so a simple TrueType font reads its program
// as an sfnt whatever /Subtype names it. type1.PDType1Font and PDCIDFont
// instead dispatch on that /Subtype. A program in the other container leaves
// the parse unfinished, and GFPDFont.getcontainsFontFile, which answers
// getFontProgram() != null && fontProgramParsed, reports the font as not
// embedded (ISO 19005-2, 6.2.11.4.1 t1; ISO 14289-2, 8.4.5.5.1 t1).
//
// Measured against veraPDF 1.30.2, moving the same program bytes between
// declarations: a /Type1 font fails an sfnt under "Type1C" and passes a bare
// CFF; a /CIDFontType0 fails an OTTO under "CIDFontType0C" and passes the same
// bytes under "OpenType"; a /TrueType font passes an sfnt under "Type1C" and
// fails a bare CFF there.
//
// The font decides the reading of an "OpenType" program too. A TrueType or
// CIDFontType2 font reads it as a TrueType program, which needs the 'hhea' and
// 'hmtx' tables ([sfntMetricsReadable]); a Type 1 or CIDFontType0 font builds
// OpenTypeFontProgram as a CFF one, which needs a 'CFF ' table
// ([openTypeCFFReadable]) and throws "Can't locate "CFF " table" without one.
// A 'CFF2' table (OpenType 1.8) is not one. Measured against veraPDF 1.30.2
// at PDF/A-2b: an "OTTO" with 'CFF2' outlines and metrics is not embedded in a
// /Type1 or a /CIDFontType0 font and is embedded in a /TrueType or a
// /CIDFontType2 one; a version-1.0 sfnt with no 'CFF ' table is not embedded
// in a /Type1 or a /CIDFontType0 font; an "OTTO" with 'CFF ' outlines and no
// 'hhea' or 'hmtx' is not embedded in a /TrueType or a /CIDFontType2 font.
// litho fontcff2_2b.pdf plants each.
//
// The glyph rules take the same verdict: [extractFontProgramData] hands them
// no program the font cannot read, and hands them the one it can as the font
// reads it ([ProgramAsFontReadsIt]).
//
// # Corpus evidence
//
//   - pdfjs issue7598.pdf: embeds an OTTO under "Type1C" in a /Type1 font, the
//     source of the /Type1 measurement.
//   - pdfjs issue20426.pdf, issue6782.pdf: embed an OTTO under
//     "CIDFontType0C" in a /CIDFontType0, the source of the /CIDFontType0
//     measurement.
//   - pdfjs issue4668.pdf: embeds an sfnt under "Type1C" in a /TrueType font,
//     the source of the /TrueType measurement.
//   - All corpora: no "OpenType" program lacks the tables its font reads,
//     and none carries a 'CFF2' table, so the OpenType readings move no cell.
func fontFile3ParsesForFont(ctx context.Context, doc *document.Document, fontSub, declared string, obj ast.PdfObject, stream ast.PdfStream) bool {
	data, err := decodeFontProgram(ctx, doc, obj, stream)
	if err != nil || len(data) == 0 {
		// Bytes that will not decode, and an empty program, are
		// [streamHoldsFontProgram]'s to judge. Leave both to it.
		return true
	}
	if fontSub == "TrueType" {
		return isSfntFontProgram(data) && sfntMetricsReadable(data)
	}
	switch declared {
	case "OpenType":
		if !isSfntFontProgram(data) {
			return false
		}
		if fontSub == "CIDFontType2" {
			return sfntMetricsReadable(data)
		}
		return openTypeCFFReadable(data)
	case "Type1C", "CIDFontType0C":
		return isCFFFontProgram(data)
	}
	return true
}

// ProgramAsFontReadsIt returns an embedded program in the form the font whose
// /Subtype is fontSub reads it, so every glyph rule weighs the tables veraPDF
// weighs, not the outlines the bytes carry.
//
// A Type 1 or CIDFontType0 font reads an sfnt through its 'CFF ' table
// (OpenTypeFontProgram, built with isCFF, hands every glyph, width and code
// query to the CFFFontProgram it parses from that table), so such a program
// is returned as that table, a bare CFF. Its 'hmtx' widths, 'cmap' and 'post'
// names are then read by no rule, as veraPDF reads none of them.
//
// A TrueType or CIDFontType2 font reads any sfnt as a TrueType program
// (TrueTypeFontProgram and CIDFontType2Program), its "OTTO" version tag
// included, so such a program is returned under the TrueType version tag
// 0x00010000: its 'cmap', 'hmtx', 'maxp' and 'post' answer, and its 'CFF '
// table answers nothing. The table directory is unchanged, so the copy reads
// exactly the tables the program carries. ISO 32000-1, 9.9 and Table 126 name
// the container; the font's own type names the reading.
//
// Measured against veraPDF 1.30.2 at every PDF/A and PDF/UA level, on
// programs whose CFF charstrings advance 600 and whose 'hmtx' states 500: a
// Type 1 font over an "OTTO" passes /Widths 600 and fails /Widths 500; a Type
// 1 font over a version-1.0 sfnt carrying 'glyf' and a 'CFF ' table passes
// /Widths 600, and so does a CIDFontType0 font over the CID-keyed equivalent
// under /W 600; a CIDFontType2 font over an "OTTO" passes /W 500 and fails
// /W 600; a TrueType font over an "OTTO" with no 'head' weighs its advances
// of 1000 against 2048 units and passes /Widths 488 ([headlessUnitsPerEm]);
// and a CIDFontType2 font over an "OTTO" whose 'maxp' counts three glyphs
// fails glyph presence and .notdef for CID 3, which its 'CFF ' charset holds.
// litho fontotread_2b.pdf plants each.
//
// Any other program, and one whose reading needs no change, is returned as
// it is. Remediation repairs widths and /CharSet from the same form, so the
// repair and the check read one program.
func ProgramAsFontReadsIt(fontSub string, data []byte) []byte {
	if !isSfntFontProgram(data) {
		return data
	}
	switch fontSub {
	case "Type1", "MMType1", "CIDFontType0":
		records, err := truetype.ParseTableDirectory(data)
		if err != nil {
			return data
		}
		if table, err := truetype.TableData(data, records, "CFF "); err == nil && len(table) > 0 {
			return table
		}
	case "TrueType", "CIDFontType2":
		if string(data[:4]) == "OTTO" {
			read := slices.Clone(data)
			copy(read, "\x00\x01\x00\x00")
			return read
		}
	}
	return data
}

// programFontSubtype returns the /Subtype of the font whose descriptor
// [document.ResolveFontDescriptor] reads for fontDict: the font's own for a
// simple font, the descendant CIDFont's for a Type 0 font, which is the font
// that embeds the program ([extractFontProgramData]).
func programFontSubtype(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) string {
	if _, direct := fontDict["FontDescriptor"]; !direct {
		if cid := type0NameDict(ctx, doc, fontDict); cid != nil {
			return fontSubtype(ctx, doc, cid)
		}
	}
	return fontSubtype(ctx, doc, fontDict)
}

// decodeFontProgram decodes an embedded font program, reusing the document's
// font-program cache when the stream is reached through an indirect reference.
//
// The font rules re-read the same embedded program for every page that declares
// the font, and inflating it is far from free. A direct stream is not
// cacheable, having no object number to key on, and is not shared between pages
// anyway.
//
// The returned slice is shared with every other caller and must not be
// modified. All callers pass it to a parser that only reads it.
//
// # Corpus evidence
//
//   - govdocs: on one 4 MB government document the program decode ran 11248
//     times over 66 distinct streams and accounted for roughly three quarters
//     of the whole validation.
//   - pdfjs poppler-395-0-fuzzed.pdf: its /FontFile2 keeps 37 bytes before a
//     byte ASCIIHexDecode rejects. They open with 00 7F 00 00, no sfnt
//     version, so judged as the program they would fail a font veraPDF
//     passes as embedded.
func decodeFontProgram(ctx context.Context, doc *document.Document, obj ast.PdfObject, stream ast.PdfStream) ([]byte, error) {
	ref, indirect := obj.(ast.PdfIndirectRef)
	if indirect {
		if data, hit := doc.CachedFontProgram(ref.ObjectNum); hit {
			return data, nil
		}
	}
	data, report, err := doc.DecodeStreamReport(ctx, stream)
	if err != nil {
		return nil, err
	}
	// The filter chain keeps the bytes an ASCII filter decoded before damaged
	// data, which are a fragment of the program rather than the program. A
	// program whose bytes will not decode says nothing about its container, and
	// veraPDF counts the font as embedded (see [streamHoldsFontProgram]), so
	// the fragment reads as a program that will not decode. The chain keeps
	// what FlateDecode, LZWDecode and BrotliDecode decoded before damage too,
	// and those bytes stand as the program: veraPDF reads a program whose
	// deflate data merely ends early, and one whose deflate data is corrupt is
	// weighed below.
	if i := slices.IndexFunc(report.Damaged, isASCIIFilter); i >= 0 {
		return nil, fmt.Errorf("%w: %s data is damaged", errDamagedFontProgram, report.Damaged[i])
	}
	// The filter keeps what it inflated before corrupt deflate data, which is
	// not the program the file embeds; see [deflateDataCorrupt]. Such a program
	// reads as absent, so the font counts as unembedded and no rule weighs its
	// glyphs, as in veraPDF.
	if deflateDataCorrupt(ctx, doc, stream, len(data)) {
		data = nil
	}
	if indirect {
		doc.SetCachedFontProgram(ref.ObjectNum, data)
	}
	return data, nil
}

// isASCIIFilter reports whether name is ASCIIHexDecode or ASCII85Decode, in
// full or abbreviated (ISO 32000-1, Table 6 and Table 94).
func isASCIIFilter(name ast.PdfName) bool {
	switch name {
	case "ASCIIHexDecode", "AHx", "ASCII85Decode", "A85":
		return true
	}
	return false
}

// deflateDataCorrupt reports whether a stream filtered by FlateDecode alone
// holds deflate data that is corrupt or fails its Adler-32 check, as opposed to
// cut short. litho's filter keeps the bytes inflated before the damage, which
// suits text extraction, but veraPDF cannot read a font program whose deflate
// data is corrupt, or whose check fails, and reports
// the font as not embedded (ISO 19005-2, 6.2.11.4.1 t1), then skips the rules
// that need the program, the /CIDToGIDMap one (6.2.11.3.2 t1) among them. A
// stream that merely ends early still embeds its glyphs for veraPDF. Measured
// on a /FontFile2 with one deflate byte flipped (a failed check) and on one cut
// 40 bytes short. inflated bounds the strict pass: the damage lies within the
// bytes the filter already produced.
//
// # Corpus evidence
//
//   - pdfjs issue11651.pdf: deflate data with an invalid distance, measured
//     alongside the two constructed programs.
func deflateDataCorrupt(ctx context.Context, doc *document.Document, stream ast.PdfStream, inflated int) bool {
	filter := stream.Dict["Filter"]
	if arr, err := document.ResolveArray(ctx, doc, filter); err == nil && len(arr) == 1 {
		filter = arr[0]
	}
	if name, _ := document.ResolveName(ctx, doc, filter); name != "FlateDecode" {
		return false
	}
	raw, err := stream.Bytes(ctx)
	if err != nil {
		return false
	}
	zr, err := zlib.NewReader(bytes.NewReader(raw))
	if err != nil {
		// A damaged zlib header is a different failure, not measured here.
		return false
	}
	defer zr.Close()
	_, err = io.CopyN(io.Discard, zr, int64(inflated)+1)
	var corrupt flate.CorruptInputError
	return errors.As(err, &corrupt) || errors.Is(err, zlib.ErrChecksum)
}

// streamHoldsFontProgram reports whether a font file stream actually carries a
// font program of the kind its key declares. Per ISO 32000-1, Table 126,
// /FontFile holds a Type 1 program, /FontFile2 a TrueType program, and
// /FontFile3 the program its /Subtype names, so a stream that decodes to
// nothing, or to a different container entirely, embeds no glyphs a consumer
// can read and leaves the font unembedded. veraPDF reports each of these under
// ISO 19005-2 §6.2.11.4.1 t1.
//
// Beyond the container, a program counts only when it holds the structure
// veraPDF's parser needs before it reads a glyph ([fontProgramReadable]). Its
// internal consistency past that point is not judged: a truncated but
// well-formed program still embeds glyphs, and deciding otherwise would turn
// every parser difference into a false positive.
//
// # Corpus evidence
//
//   - pdfjs poppler-395-0-fuzzed.pdf: carries a /FontFile2 with corrupt
//     ASCIIHex data that veraPDF passes as embedded.
//   - pdfjs bug866395.pdf: carries an empty /FontFile3.
//   - pdfjs bug860632.pdf, issue14462_reduced.pdf: embed a PFB container as
//     /FontFile.
//   - pdfjs issue13193.pdf, issue9262_reduced.pdf: embed a TrueType
//     Collection as /FontFile2.
//   - pdfjs issue4668.pdf: declares /Type1C over a TrueType sfnt, and veraPDF
//     reports it embedded. Enforcing the /Subtype would fail it.
func streamHoldsFontProgram(ctx context.Context, doc *document.Document, key ast.PdfName, obj ast.PdfObject, stream ast.PdfStream) bool {
	data, err := decodeFontProgram(ctx, doc, obj, stream)
	if err != nil {
		// A program whose bytes will not decode says nothing about which
		// container it holds, and veraPDF still counts the font as embedded.
		// Leave it embedded rather than guess.
		doc.Logger.Debug("compliance: fonts: font program will not decode",
			"key", key, "error", err)
		return true
	}
	if len(data) == 0 {
		return false
	}
	return fontContainerMatches(key, data) && fontProgramReadable(key, data)
}

// fontContainerMatches reports whether data is in the container the font file
// key declares; see [streamHoldsFontProgram].
func fontContainerMatches(key ast.PdfName, data []byte) bool {
	switch key {
	case "FontFile":
		// A Type 1 program is the clear, encrypted and fixed portions measured
		// by /Length1, /Length2 and /Length3 (ISO 32000-1 §9.9.1). The PFB
		// container wraps those in 6-byte segment headers starting 0x80 0x01,
		// which is a different file format.
		return !bytes.HasPrefix(data, []byte{0x80, 0x01})
	case "FontFile2":
		// A TrueType program is an sfnt. "ttcf" is a TrueType Collection, which
		// holds several fonts and names none of them, so it is not the single
		// program Table 126 asks for.
		return isSfntFontProgram(data)
	case "FontFile3":
		// Every format /FontFile3 may name is one of two containers: a bare CFF
		// for "Type1C" and "CIDFontType0C", an sfnt for "OpenType"
		// (ISO 32000-1, Table 126). Bytes that are neither carry no font
		// container at all, so they embed nothing.
		//
		// Which of the two the /Subtype names is deliberately not enforced.
		// veraPDF reports a TrueType sfnt under /Type1C as embedded, because
		// its verdict is whether its own parser succeeded
		// (GFPDFont.getcontainsFontFile returns getFontProgram() != null &&
		// fontProgramParsed), not which container the subtype named.
		// Rejecting the mismatch would fail every producer that mislabels a
		// program consumers still read.
		return isCFFFontProgram(data) || isSfntFontProgram(data)
	}
	return true
}

// isCFFFontProgram reports whether data opens with a readable bare CFF header:
// the fixed major, minor, hdrSize and offSize record of CFF 1.0 §6, where
// hdrSize counts the header itself and so spans at least those four bytes, and
// offSize is an offset width of 1 to 4. Bytes that fail this hold no INDEX a
// consumer could reach, which is the container-level mistake this judges.
//
// The major version is deliberately not read: CFF2 raises it to 2, and veraPDF
// reports a program carrying that version as embedded.
func isCFFFontProgram(data []byte) bool {
	const cffHeaderSize = 4
	if len(data) < cffHeaderSize {
		return false
	}
	hdrSize, offSize := int(data[2]), int(data[3])
	return hdrSize >= cffHeaderSize && len(data) >= hdrSize && offSize >= 1 && offSize <= 4
}

// isSfntFontProgram reports whether data begins with an sfnt version this
// specification defines for a single font: 0x00010000 for TrueType outlines,
// "true" for the Apple variant, or "OTTO" for CFF outlines (ISO 32000-1 §9.9,
// OpenType 1.8 §Table Directory). A "ttcf" collection is deliberately excluded.
func isSfntFontProgram(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	switch string(data[:4]) {
	case "\x00\x01\x00\x00", "true", "OTTO":
		return true
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

// hasToUnicode returns true if the font dictionary has a /ToUnicode entry. One
// that resolves to null supplies no mapping (ISO 32000-1, 7.3.7 and 7.3.10):
// veraPDF finds no Unicode value for the glyphs then, measured on a null and on
// a reference to a null object.
func hasToUnicode(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	return nonNullKeyPresent(ctx, doc, fontDict, "ToUnicode")
}

// numberKeyPresent reports whether dict's key resolves to a number.
func numberKeyPresent(ctx context.Context, doc *document.Document, dict ast.PdfDict, key ast.PdfName) bool {
	obj, ok := dict[key]
	if !ok {
		return false
	}
	resolved, err := document.ResolveObject(ctx, doc, obj)
	if err != nil {
		return false
	}
	switch resolved.(type) {
	case ast.PdfInt, ast.PdfReal:
		return true
	}
	return false
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

	if !isFontEmbedded(ctx, doc, fd, fontSubtype(ctx, doc, fontDict), PDFAPart(profile)) {
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
// mapped code to appear in usage. Each such code is recorded in faults.
func checkNotdefInDifferences(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, faults *glyphFaults, usage *fontUsageInfo) {
	if usage == nil {
		return
	}
	encObj, ok := fontDict["Encoding"]
	if !ok {
		return
	}
	encDict, err := document.ResolveDict(ctx, doc, encObj)
	if err != nil || encDict == nil {
		return
	}
	diffArr, err := document.ResolveArray(ctx, doc, encDict["Differences"])
	if err != nil || diffArr == nil {
		return
	}
	code := 0
	for _, item := range diffArr {
		switch v := item.(type) {
		case ast.PdfInt:
			code = int(v)
		case ast.PdfName:
			if v == ".notdef" && code >= 0 && code <= 255 && usage.charCodes[byte(code)] { //nolint:gosec // G115: code is bounded to 0..255 by the guard.
				faults.add(uint32(code), "font %s on page %d: charCode %d maps to .notdef via /Differences", fontName, pageNum, code) //nolint:gosec // G115: code is bounded to 0..255 by the guard.
			}
			code++
		}
	}
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

	// ISO 32000-1, Table 117 makes 1000 the width of a font with no /DW, and
	// an explicit 0 declares a width like any other, which veraPDF reads
	// (PDCIDFont.getDefaultWidth).
	dw := 1000.0
	switch r, _ := document.ResolveObject(ctx, doc, cidFontDict["DW"]); r.(type) {
	case ast.PdfInt, ast.PdfReal:
		dw = document.Float(r)
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
