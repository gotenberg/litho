package compliance

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// --- Standard 14 font names ---

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

// --- Rule selector helpers ---

// fontEmbeddingRule returns the font embedding rule for the given profile.
func fontEmbeddingRule(profile string) Rule {
	if IsPDFUA(profile) {
		return Rule_14289_1_7_21_4_1_t01
	}
	return ruleByPart(PDFAPart(profile), Rule_19005_1_6_3_4_t01, Rule_19005_2_6_2_11_4_1_t01, Rule_19005_3_6_2_11_4_1_t01, Rule_19005_4_6_2_10_4_1_t01)
}

// toUnicodeRule returns the ToUnicode mapping rule for the given profile.
// Returns a zero Rule if the check does not apply.
func toUnicodeRule(profile string) Rule {
	if IsPDFUA(profile) {
		return Rule_14289_1_7_21_7_t01
	}
	switch PDFAPart(profile) {
	case 1:
		// Part 1: applies to all levels.
		return Rule_19005_1_6_3_8_t01
	case 2:
		level := PDFALevel(profile)
		if level == 'u' || level == 'a' {
			return Rule_19005_2_6_2_11_7_2_t01
		}
		return Rule{}
	case 3:
		level := PDFALevel(profile)
		if level == 'u' || level == 'a' {
			return Rule_19005_3_6_2_11_7_2_t01
		}
		return Rule{}
	case 4:
		// All PDF/A-4 profiles require ToUnicode.
		return Rule_19005_4_6_2_10_7_t01
	}
	return Rule{}
}

// glyphWidthRule returns the glyph width consistency rule.
func glyphWidthRule(profile string) Rule {
	if IsPDFUA(profile) {
		return Rule_14289_1_7_21_5_t01
	}
	return ruleByPart(PDFAPart(profile), Rule_19005_1_6_3_6_t01, Rule_19005_2_6_2_11_5_t01, Rule_19005_3_6_2_11_5_t01, Rule_19005_4_6_2_10_5_t01)
}

// notdefRule returns the .notdef glyph rule.
// Returns a zero Rule for part 1 (not applicable).
func notdefRule(profile string) Rule {
	if IsPDFUA(profile) {
		return Rule_14289_1_7_21_8_t01
	}
	return ruleByPart(PDFAPart(profile), Rule{}, Rule_19005_2_6_2_11_8_t01, Rule_19005_3_6_2_11_8_t01, Rule_19005_4_6_2_10_9_t01)
}

// trueTypeEncodingNonSymbolicRule returns the TrueType non-symbolic encoding rule.
func trueTypeEncodingNonSymbolicRule(profile string) Rule {
	if IsPDFUA(profile) {
		return Rule_14289_1_7_21_6_t02
	}
	return ruleByPart(PDFAPart(profile), Rule_19005_1_6_3_7_t01, Rule_19005_2_6_2_11_6_t02, Rule_19005_3_6_2_11_6_t02, Rule_19005_4_6_2_10_6_t02)
}

// trueTypeEncodingSymbolicNoEncodingRule returns the rule that symbolic TrueType
// fonts must not specify /Encoding.
func trueTypeEncodingSymbolicNoEncodingRule(profile string) Rule {
	if IsPDFUA(profile) {
		return Rule_14289_1_7_21_6_t03
	}
	return ruleByPart(PDFAPart(profile), Rule_19005_1_6_3_7_t02, Rule_19005_2_6_2_11_6_t03, Rule_19005_3_6_2_11_6_t03, Rule_19005_4_6_2_10_6_t03)
}

// trueTypeSymbolicCmapRule returns the rule for symbolic TrueType cmap requirements.
func trueTypeSymbolicCmapRule(profile string) Rule {
	if IsPDFUA(profile) {
		return Rule_14289_1_7_21_6_t04
	}
	return ruleByPart(PDFAPart(profile), Rule_19005_1_6_3_7_t03, Rule_19005_2_6_2_11_6_t04, Rule_19005_3_6_2_11_6_t04, Rule_19005_4_6_2_10_6_t04)
}

// fontNameUTF8Rule returns the font name UTF-8 rule.
// Only applicable to parts 2+, 4, and PDF/UA.
func fontNameUTF8Rule(profile string) Rule {
	if IsPDFUA(profile) {
		// PDF/UA reuses the font general rules but there is no specific
		// BaseFont UTF-8 rule in 14289-1; skip.
		return Rule{}
	}
	return ruleByPart(PDFAPart(profile), Rule{}, Rule_19005_2_6_2_11_2_t03, Rule_19005_3_6_2_11_2_t03, Rule_19005_4_6_2_10_2_t03)
}

// toUnicodeCMapValuesRule returns the ToUnicode CMap values rule.
func toUnicodeCMapValuesRule(profile string) Rule {
	if IsPDFUA(profile) {
		if strings.Contains(profile, "2") {
			return Rule_14289_2_8_4_5_8_t02
		}
		return Rule_14289_1_7_21_7_t02
	}
	return ruleByPart(PDFAPart(profile), Rule{}, Rule_19005_2_6_2_11_7_3_t01, Rule_19005_3_6_2_11_7_3_t01, Rule_19005_4_6_2_10_7_t01)
}

func puaActualTextRule(profile string) Rule {
	return ruleByPart(PDFAPart(profile), Rule{}, Rule_19005_2_6_2_11_7_3_t01, Rule_19005_3_6_2_11_7_3_t01, Rule{})
}

// --- Helper functions ---

// isType3Font returns true if the font subtype is "Type3".
func isType3Font(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	subtype, _ := document.ResolveName(ctx, doc, fontDict["Subtype"])
	return subtype == "Type3"
}

// isStandard14Font returns true if the font's BaseFont is one of the 14
// standard PDF fonts.
func isStandard14Font(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	basefont, _ := document.ResolveName(ctx, doc, fontDict["BaseFont"])
	return standard14Fonts[string(basefont)]
}

// findFontDescriptor resolves the /FontDescriptor dictionary from a font dict.
// Returns nil if no FontDescriptor is present.
func findFontDescriptor(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) ast.PdfDict {
	fdObj, ok := fontDict["FontDescriptor"]
	if !ok {
		return nil
	}
	fd, err := document.ResolveDict(ctx, doc, fdObj)
	if err != nil {
		complog(doc).Debug("compliance: fonts: failed to resolve FontDescriptor", "error", err)
		return nil
	}
	return fd
}

// isFontEmbedded checks whether the font descriptor contains a FontFile,
// FontFile2, or FontFile3 stream indicating an embedded font program.
func isFontEmbedded(ctx context.Context, doc *document.Document, fd ast.PdfDict) bool {
	if fd == nil {
		return false
	}
	for _, key := range []ast.PdfName{"FontFile", "FontFile2", "FontFile3"} {
		if obj, ok := fd[key]; ok {
			resolved, err := document.ResolveObject(ctx, doc, obj)
			if err != nil {
				complog(doc).Debug("compliance: fonts: failed to resolve font file entry",
					"key", key, "error", err)
				continue
			}
			if _, isStream := resolved.(ast.PdfStream); isStream {
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
		complog(doc).Debug("compliance: fonts: failed to resolve Flags", "error", err)
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

	fd := findFontDescriptor(ctx, doc, fontDict)
	if fd == nil {
		// No FontDescriptor means not embedded. PDF/A requires embedding even
		// for the 14 standard fonts.
		return failf(rule, "font %s on page %d", fontName, pageNum)
	}

	if !isFontEmbedded(ctx, doc, fd) {
		return failf(rule, "font %s on page %d", fontName, pageNum)
	}

	return nil
}

// checkFontNameUTF8 validates that the /BaseFont name is valid UTF-8 (rule 8).
func checkFontNameUTF8(_ context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string) []Violation {
	rule := fontNameUTF8Rule(profile)
	if rule.Specification == "" {
		return nil
	}

	basefontObj, ok := fontDict["BaseFont"]
	if !ok {
		// BaseFont key is truly absent — this is not a UTF-8 violation; the
		// embedding check will catch the missing font program. Do not flag here
		// to avoid false positives when the key exists but cannot be decoded.
		return nil
	}

	basefontName, ok := basefontObj.(ast.PdfName)
	if !ok {
		// BaseFont resolves to a non-name type — not a UTF-8 violation.
		return nil
	}

	if !utf8.ValidString(string(basefontName)) {
		return failf(rule, "font %s on page %d BaseFont=%s", fontName, pageNum, string(basefontName))
	}

	// Suppress unused warning for doc — used in other check functions via
	// the same signature pattern.
	_ = doc
	return nil
}

// checkNotdefInDifferences checks /Encoding /Differences for explicit ".notdef" entries.
func checkNotdefInDifferences(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, rule Rule) []Violation {
	encObj, ok := fontDict["Encoding"]
	if !ok {
		return nil
	}
	encDict, err := document.ResolveDict(ctx, doc, encObj)
	if err != nil || encDict == nil {
		return nil
	}
	diffObj, ok := encDict["Differences"]
	if !ok {
		return nil
	}
	diffArr, err := document.ResolveArray(ctx, doc, diffObj)
	if err != nil || diffArr == nil {
		return nil
	}
	for _, item := range diffArr {
		if n, ok := item.(ast.PdfName); ok && n == ".notdef" {
			return failf(rule, "font %s on page %d: .notdef glyph in /Differences", fontName, pageNum)
		}
	}
	return nil
}

// simpleFontWidths extracts FirstChar and the /Widths array from a simple font dict.
func simpleFontWidths(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) (int, []ast.PdfObject) {
	firstChar := int(document.Float(fontDict["FirstChar"]))
	widths, err := document.ResolveArray(ctx, doc, fontDict["Widths"])
	if err != nil {
		return firstChar, nil
	}
	return firstChar, widths
}

// simpleFontWidthIsZero returns true if the PDF /Widths entry for charCode is 0.
func simpleFontWidthIsZero(firstChar int, widths []ast.PdfObject, charCode byte) bool {
	idx := int(charCode) - firstChar
	if idx < 0 || idx >= len(widths) {
		return false // Not in range — can't determine.
	}
	return document.Float(widths[idx]) == 0
}

// lookupCIDWidth returns the PDF-declared width for a CID from the /W array
// and /DW default of a CIDFont dict.
func lookupCIDWidth(ctx context.Context, doc *document.Document, cidFontDict ast.PdfDict, cid uint16) float64 {
	// Default width.
	dw := document.Float(cidFontDict["DW"])
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
		first := int(document.Float(wArr[i]))
		if i+1 >= len(wArr) {
			break
		}
		// Format 1: [cidStart [w1 w2 ...]]
		if arr, err := document.ResolveArray(ctx, doc, wArr[i+1]); err == nil && arr != nil {
			for j, w := range arr {
				if first+j == cidInt {
					return document.Float(w)
				}
			}
			i += 2
			continue
		}
		// Format 2: [cidFirst cidLast w]
		if i+2 >= len(wArr) {
			break
		}
		last := int(document.Float(wArr[i+1]))
		if cidInt >= first && cidInt <= last {
			return document.Float(wArr[i+2])
		}
		i += 3
	}
	return dw
}
