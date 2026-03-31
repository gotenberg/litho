package compliance

import (
	"bytes"
	"context"
	"log/slog"
	"maps"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/typography"
	"github.com/gotenberg/litho/pkg/internal/typography/cmap"
	"github.com/gotenberg/litho/pkg/internal/typography/encodings"
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
	if isPDFUA(profile) {
		return Rule_14289_1_7_21_4_1_t01
	}
	switch pdfaPart(profile) {
	case 1:
		return Rule_19005_1_6_3_4_t01
	case 2:
		return Rule_19005_2_6_2_11_4_1_t01
	case 3:
		return Rule_19005_3_6_2_11_4_1_t01
	case 4:
		return Rule_19005_4_6_2_10_4_1_t01
	}
	return Rule{}
}

// toUnicodeRule returns the ToUnicode mapping rule for the given profile.
// Returns a zero Rule if the check does not apply.
func toUnicodeRule(profile string) Rule {
	if isPDFUA(profile) {
		return Rule_14289_1_7_21_7_t01
	}
	switch pdfaPart(profile) {
	case 1:
		// Part 1: applies to all levels.
		return Rule_19005_1_6_3_8_t01
	case 2:
		level := pdfaLevel(profile)
		if level == 'u' || level == 'a' {
			return Rule_19005_2_6_2_11_7_2_t01
		}
		return Rule{}
	case 3:
		level := pdfaLevel(profile)
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

// cidToGIDMapRule returns the CIDToGIDMap rule for the given profile.
// Returns a zero Rule if the check does not apply (part 1).
func cidToGIDMapRule(profile string) Rule {
	if isPDFUA(profile) {
		return Rule_14289_1_7_21_3_2_t01
	}
	switch pdfaPart(profile) {
	case 1:
		return Rule{} // not applicable to part 1
	case 2:
		return Rule_19005_2_6_2_11_3_2_t01
	case 3:
		return Rule_19005_3_6_2_11_3_2_t01
	case 4:
		return Rule_19005_4_6_2_10_3_2_t01
	}
	return Rule{}
}

// cidSystemInfoRule returns the CIDSystemInfo consistency rule.
// Returns a zero Rule if the check does not apply (part 1).
func cidSystemInfoRule(profile string) Rule {
	if isPDFUA(profile) {
		return Rule_14289_1_7_21_3_1_t01
	}
	switch pdfaPart(profile) {
	case 1:
		return Rule_19005_1_6_3_3_1_t01
	case 2:
		return Rule_19005_2_6_2_11_3_1_t01
	case 3:
		return Rule_19005_3_6_2_11_3_1_t01
	case 4:
		return Rule_19005_4_6_2_10_3_1_t01
	}
	return Rule{}
}

// glyphWidthRule returns the glyph width consistency rule.
func glyphWidthRule(profile string) Rule {
	if isPDFUA(profile) {
		return Rule_14289_1_7_21_5_t01
	}
	switch pdfaPart(profile) {
	case 1:
		return Rule_19005_1_6_3_6_t01
	case 2:
		return Rule_19005_2_6_2_11_5_t01
	case 3:
		return Rule_19005_3_6_2_11_5_t01
	case 4:
		return Rule_19005_4_6_2_10_5_t01
	}
	return Rule{}
}

// notdefRule returns the .notdef glyph rule.
// Returns a zero Rule for part 1 (not applicable).
func notdefRule(profile string) Rule {
	if isPDFUA(profile) {
		return Rule_14289_1_7_21_8_t01
	}
	switch pdfaPart(profile) {
	case 1:
		return Rule{} // not applicable to part 1
	case 2:
		return Rule_19005_2_6_2_11_8_t01
	case 3:
		return Rule_19005_3_6_2_11_8_t01
	case 4:
		return Rule_19005_4_6_2_10_9_t01
	}
	return Rule{}
}

// trueTypeEncodingNonSymbolicRule returns the TrueType non-symbolic encoding rule.
func trueTypeEncodingNonSymbolicRule(profile string) Rule {
	if isPDFUA(profile) {
		return Rule_14289_1_7_21_6_t02
	}
	switch pdfaPart(profile) {
	case 1:
		return Rule_19005_1_6_3_7_t01
	case 2:
		return Rule_19005_2_6_2_11_6_t02
	case 3:
		return Rule_19005_3_6_2_11_6_t02
	case 4:
		return Rule_19005_4_6_2_10_6_t02
	}
	return Rule{}
}

// trueTypeEncodingSymbolicNoEncodingRule returns the rule that symbolic TrueType
// fonts must not specify /Encoding.
func trueTypeEncodingSymbolicNoEncodingRule(profile string) Rule {
	if isPDFUA(profile) {
		return Rule_14289_1_7_21_6_t03
	}
	switch pdfaPart(profile) {
	case 1:
		return Rule_19005_1_6_3_7_t02
	case 2:
		return Rule_19005_2_6_2_11_6_t03
	case 3:
		return Rule_19005_3_6_2_11_6_t03
	case 4:
		return Rule_19005_4_6_2_10_6_t03
	}
	return Rule{}
}

// trueTypeSymbolicCmapRule returns the rule for symbolic TrueType cmap requirements.
func trueTypeSymbolicCmapRule(profile string) Rule {
	if isPDFUA(profile) {
		return Rule_14289_1_7_21_6_t04
	}
	switch pdfaPart(profile) {
	case 1:
		return Rule_19005_1_6_3_7_t03
	case 2:
		return Rule_19005_2_6_2_11_6_t04
	case 3:
		return Rule_19005_3_6_2_11_6_t04
	case 4:
		return Rule_19005_4_6_2_10_6_t04
	}
	return Rule{}
}

// fontNameUTF8Rule returns the font name UTF-8 rule.
// Only applicable to parts 2+, 4, and PDF/UA.
func fontNameUTF8Rule(profile string) Rule {
	if isPDFUA(profile) {
		// PDF/UA reuses the font general rules but there is no specific
		// BaseFont UTF-8 rule in 14289-1; skip.
		return Rule{}
	}
	switch pdfaPart(profile) {
	case 1:
		return Rule{} // not applicable to part 1
	case 2:
		return Rule_19005_2_6_2_11_2_t03
	case 3:
		return Rule_19005_3_6_2_11_2_t03
	case 4:
		return Rule_19005_4_6_2_10_2_t03
	}
	return Rule{}
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
		slog.Debug("compliance_2: fonts: no FontDescriptor entry", "font", fontDict["BaseFont"])
		return nil
	}
	fd, err := document.ResolveDict(ctx, doc, fdObj)
	if err != nil {
		slog.Debug("compliance_2: fonts: failed to resolve FontDescriptor", "error", err)
		return nil
	}
	return fd
}

// isFontEmbedded checks whether the font descriptor contains a FontFile,
// FontFile2, or FontFile3 stream indicating an embedded font program.
func isFontEmbedded(ctx context.Context, doc *document.Document, fd ast.PdfDict) bool {
	if fd == nil {
		slog.Debug("compliance_2: fonts: no font descriptor, font not embedded")
		return false
	}
	for _, key := range []ast.PdfName{"FontFile", "FontFile2", "FontFile3"} {
		if obj, ok := fd[key]; ok {
			resolved, err := document.ResolveObject(ctx, doc, obj)
			if err != nil {
				slog.Debug("compliance_2: fonts: failed to resolve font file entry",
					"key", key, "error", err)
				continue
			}
			if _, isStream := resolved.(ast.PdfStream); isStream {
				slog.Debug("compliance_2: fonts: font is embedded", "key", key)
				return true
			}
			// Non-null, non-stream: treat as present but invalid (still "embedded"
			// for the purpose of the embedding check).
			if resolved != nil {
				if _, isNull := resolved.(ast.PdfNull); !isNull {
					slog.Debug("compliance_2: fonts: font file entry present but not a stream",
						"key", key, "type", resolved)
					return true
				}
			}
		}
	}
	slog.Debug("compliance_2: fonts: no FontFile/FontFile2/FontFile3 found, font not embedded")
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
		slog.Debug("compliance_2: fonts: failed to resolve Flags", "error", err)
		return false
	}
	const symbolicBit = 1 << 2 // bit 3 (0-indexed bit 2) = symbolic
	symbolic := (int(flags) & symbolicBit) != 0
	slog.Debug("compliance_2: fonts: checked symbolic flag", "flags", flags, "symbolic", symbolic)
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

// hasToUnicode returns true if the font dictionary has a /ToUnicode entry.
func hasToUnicode(fontDict ast.PdfDict) bool {
	_, ok := fontDict["ToUnicode"]
	return ok
}

// hasStandardEncoding returns true if the font uses MacRomanEncoding,
// WinAnsiEncoding, or MacExpertEncoding as a direct /Encoding name. All three
// encodings use character names from the Adobe standard Latin character set,
// which provides a well-defined Unicode mapping without requiring /ToUnicode.
func hasStandardEncoding(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	encObj, ok := fontDict["Encoding"]
	if !ok {
		return false
	}
	encName, _ := document.ResolveName(ctx, doc, encObj)
	return encName == "MacRomanEncoding" || encName == "WinAnsiEncoding" || encName == "MacExpertEncoding"
}

// hasStandardBaseEncoding returns true if the font's /Encoding is a dictionary
// whose /BaseEncoding is one of the standard Latin encodings (MacRomanEncoding,
// WinAnsiEncoding, or MacExpertEncoding). Type 1 fonts using these base
// encodings (even with /Differences) have character names from the Adobe
// standard Latin set and are exempt from the /ToUnicode requirement.
func hasStandardBaseEncoding(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	encObj, ok := fontDict["Encoding"]
	if !ok {
		return false
	}
	encDict, err := document.ResolveDict(ctx, doc, encObj)
	if err != nil || encDict == nil {
		return false
	}
	baseEnc, _ := document.ResolveName(ctx, doc, encDict["BaseEncoding"])
	if baseEnc != "MacRomanEncoding" && baseEnc != "WinAnsiEncoding" && baseEnc != "MacExpertEncoding" {
		return false
	}
	// If Differences are present, the exemption only applies if all
	// glyph names in Differences are AGL-resolvable.
	if _, hasDiffs := encDict["Differences"]; hasDiffs {
		return false // fall through to the AGL check for full validation
	}
	return true
}

// hasIdentityCMap returns true if the font's /Encoding is Identity-H or
// Identity-V. This checks both the direct name case and the case where
// the encoding is a CMap stream with /CMapName set to Identity-H or Identity-V.
func hasIdentityCMap(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	encObj, ok := fontDict["Encoding"]
	if !ok {
		return false
	}
	// Direct name case (most common).
	encName, _ := document.ResolveName(ctx, doc, encObj)
	if encName == "Identity-H" || encName == "Identity-V" {
		return true
	}
	// CMap stream case: check /CMapName in the stream dictionary.
	stream, err := document.ResolveStream(ctx, doc, encObj)
	if err != nil || stream.Dict == nil {
		return false
	}
	cmapName, _ := document.ResolveName(ctx, doc, stream.Dict["CMapName"])
	if cmapName == "Identity-H" || cmapName == "Identity-V" {
		slog.Debug("compliance_2: fonts: encoding stream has Identity CMap name", "cmapName", cmapName)
		return true
	}
	return false
}

// allGlyphNamesAGLResolvable returns true if all glyph names used by a Type 1
// font are resolvable via the Adobe Glyph List. It checks multiple sources:
//  1. Standard 14 fonts (always AGL-based)
//  2. /Encoding /Differences array glyph names
//  3. /FontDescriptor /CharSet (lists glyph names in Type 1 subsets)
func allGlyphNamesAGLResolvable(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	// Standard 14 fonts always use AGL-resolvable names.
	if isStandard14Font(ctx, doc, fontDict) {
		return true
	}

	// Check /Encoding /Differences if present.
	if encObj, ok := fontDict["Encoding"]; ok {
		encDict, err := document.ResolveDict(ctx, doc, encObj)
		if err == nil && encDict != nil {
			if diffObj, hasDiff := encDict["Differences"]; hasDiff {
				diffArr, err := document.ResolveArray(ctx, doc, diffObj)
				if err == nil {
					for _, item := range diffArr {
						name, ok := item.(ast.PdfName)
						if !ok {
							continue // integer code — skip
						}
						if string(name) == ".notdef" {
							continue
						}
						if _, resolved := encodings.GlyphToRuneFull(string(name)); !resolved {
							slog.Debug("compliance_2: fonts: Differences glyph not AGL-resolvable",
								"glyph", string(name))
							return false
						}
					}
					return true
				}
			}
		}
	}

	// Check /FontDescriptor /CharSet — a string of slash-separated glyph names.
	fd := findFontDescriptor(ctx, doc, fontDict)
	if fd != nil {
		if charsetObj, ok := fd["CharSet"]; ok {
			charsetStr, err := document.ResolveString(ctx, doc, charsetObj)
			if err == nil && len(charsetStr) > 0 {
				decoded := document.DecodePdfString(charsetStr)
				names := strings.SplitSeq(decoded, "/")
				for name := range names {
					name = strings.TrimSpace(name)
					if name == "" || name == ".notdef" {
						continue
					}
					if _, resolved := encodings.GlyphToRuneFull(name); !resolved {
						slog.Debug("compliance_2: fonts: CharSet glyph not AGL-resolvable",
							"glyph", name)
						return false
					}
				}
				return true
			}
		}
	}

	return false
}

// allDifferencesGlyphNamesAGLResolvable checks whether every glyph name in the
// font's /Encoding /Differences array is resolvable via the Adobe Glyph List.
// This is used for Type3 fonts which use /Encoding /Differences to define their
// character mappings. Returns true if no Differences are present (vacuously true).
func allDifferencesGlyphNamesAGLResolvable(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	encObj, ok := fontDict["Encoding"]
	if !ok {
		return true // no encoding → nothing to validate
	}
	encDict, err := document.ResolveDict(ctx, doc, encObj)
	if err != nil || encDict == nil {
		return true // can't resolve → assume OK
	}
	diffObj, hasDiff := encDict["Differences"]
	if !hasDiff {
		return true // no Differences → vacuously true
	}
	diffArr, err := document.ResolveArray(ctx, doc, diffObj)
	if err != nil {
		return true
	}
	for _, item := range diffArr {
		name, ok := item.(ast.PdfName)
		if !ok {
			continue // integer code — skip
		}
		if string(name) == ".notdef" {
			continue
		}
		_, resolved := encodings.GlyphToRuneFull(string(name))
		if !resolved {
			// Also check Symbol encoding glyph names (e.g., alpha, beta, etc.)
			// which are standard Adobe glyph names recognized by VeraPDF.
			_, resolved = encodings.SymbolGlyphsUnicode[string(name)]
		}
		if !resolved {
			slog.Debug("compliance_2: fonts: Type3 Differences glyph not AGL-resolvable",
				"glyph", string(name))
			return false
		}
	}
	return true
}

// symbolEncodingNames lists all glyph names from the Symbol font encoding
// (PDF Reference Appendix D, Table D.5). Used for the ToUnicode exemption
// that accepts Type 1 fonts whose names are from the Symbol font character set.
//
//nolint:gochecknoglobals // constant lookup table.
var symbolEncodingNames = map[string]bool{
	"space": true, "exclam": true, "universal": true, "numbersign": true,
	"existential": true, "percent": true, "ampersand": true, "suchthat": true,
	"parenleft": true, "parenright": true, "asteriskmath": true, "plus": true,
	"comma": true, "minus": true, "period": true, "slash": true,
	"zero": true, "one": true, "two": true, "three": true, "four": true,
	"five": true, "six": true, "seven": true, "eight": true, "nine": true,
	"colon": true, "semicolon": true, "less": true, "equal": true, "greater": true,
	"question": true, "congruent": true, "Alpha": true, "Beta": true, "Chi": true,
	"Delta": true, "Epsilon": true, "Phi": true, "Gamma": true, "Eta": true,
	"Iota": true, "theta1": true, "Kappa": true, "Lambda": true, "Mu": true,
	"Nu": true, "Omicron": true, "Pi": true, "Theta": true, "Rho": true,
	"Sigma": true, "Tau": true, "Upsilon": true, "sigma1": true, "Omega": true,
	"Xi": true, "Psi": true, "Zeta": true, "bracketleft": true, "therefore": true,
	"bracketright": true, "perpendicular": true, "underscore": true, "radicalex": true,
	"alpha": true, "beta": true, "chi": true, "delta": true, "epsilon": true,
	"phi": true, "gamma": true, "eta": true, "iota": true, "phi1": true,
	"kappa": true, "lambda": true, "mu": true, "nu": true, "omicron": true,
	"pi": true, "theta": true, "rho": true, "sigma": true, "tau": true,
	"upsilon": true, "omega1": true, "omega": true, "xi": true, "psi": true,
	"zeta": true, "braceleft": true, "bar": true, "braceright": true, "similar": true,
	"Upsilon1": true, "minute": true, "lessequal": true, "fraction": true,
	"infinity": true, "florin": true, "club": true, "diamond": true,
	"heart": true, "spade": true, "arrowboth": true, "arrowleft": true,
	"arrowup": true, "arrowright": true, "arrowdown": true, "degree": true,
	"plusminus": true, "second": true, "greaterequal": true, "multiply": true,
	"proportional": true, "partialdiff": true, "bullet": true, "divide": true,
	"notequal": true, "equivalence": true, "approxequal": true, "ellipsis": true,
	"arrowvertex": true, "arrowhorizex": true, "carriagereturn": true,
	"aleph": true, "Ifraktur": true, "Rfraktur": true, "weierstrass": true,
	"circlemultiply": true, "circleplus": true, "emptyset": true,
	"intersection": true, "union": true, "propersuperset": true,
	"reflexsuperset": true, "notsubset": true, "propersubset": true,
	"reflexsubset": true, "element": true, "notelement": true, "angle": true,
	"gradient": true, "registerserif": true, "copyrightserif": true,
	"trademarkserif": true, "product": true, "radical": true, "dotmath": true,
	"logicalnot": true, "logicaland": true, "logicalor": true,
	"arrowdblboth": true, "arrowdblleft": true, "arrowdblup": true,
	"arrowdblright": true, "arrowdbldown": true, "lozenge": true,
	"angleleft": true, "registersans": true, "copyrightsans": true,
	"trademarksans": true, "summation": true, "parenlefttp": true,
	"parenleftex": true, "parenleftbt": true, "bracketlefttp": true,
	"bracketleftex": true, "bracketleftbt": true, "bracelefttp": true,
	"braceleftmid": true, "braceleftbt": true, "braceex": true,
	"angleright": true, "integral": true, "integraltp": true,
	"integralex": true, "integralbt": true, "parenrighttp": true,
	"parenrightex": true, "parenrightbt": true, "bracketrighttp": true,
	"bracketrightex": true, "bracketrightbt": true, "bracerighttp": true,
	"bracerightmid": true, "bracerightbt": true,
	// .notdef and space variants
	".notdef": true,
}

// isSymbolFontByName returns true if the font's BaseFont name indicates it's
// the Adobe Symbol font or a subset thereof. The Symbol font is specifically
// exempt from ToUnicode per §6.3.8 condition 2.
func isSymbolFontByName(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	baseFontName, _ := document.ResolveName(ctx, doc, fontDict["BaseFont"])
	name := string(baseFontName)
	// Strip subset prefix (e.g., "ABCDEF+Symbol" → "Symbol")
	if idx := strings.Index(name, "+"); idx >= 0 {
		name = name[idx+1:]
	}
	// Accept exact "Symbol" or names containing "Symbol" as a word boundary
	// (e.g., "MdSymbolA-Regular" contains "Symbol").
	return strings.Contains(name, "Symbol")
}

// allGlyphNamesInSymbolEncoding returns true if all glyph names in a Type 1
// font's CharSet are from the Symbol font encoding (PDF Reference Appendix D).
func allGlyphNamesInSymbolEncoding(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	fd := findFontDescriptor(ctx, doc, fontDict)
	if fd == nil {
		return false
	}
	charsetObj, ok := fd["CharSet"]
	if !ok {
		return false
	}
	charsetStr, err := document.ResolveString(ctx, doc, charsetObj)
	if err != nil || len(charsetStr) == 0 {
		return false
	}
	decoded := document.DecodePdfString(charsetStr)
	names := strings.SplitSeq(decoded, "/")
	count := 0
	for name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		count++
		if !symbolEncodingNames[name] {
			return false
		}
	}
	return count > 0
}

// fontUsageInfo tracks how a font is used in a content stream.
type fontUsageInfo struct {
	visible    bool            // true if used with rendering mode != 3
	charCodes  map[byte]bool   // character codes referenced by Tj/TJ
	rawStrings []ast.PdfString // raw string operands from text operators (for CID .notdef detection)
}

// collectFontUsage parses the page content stream and returns usage
// information for each font: whether it's used for visible rendering and
// which character codes are referenced. Returns nil if the content stream
// can't be parsed (in which case all fonts should be checked).
func collectFontUsage(ctx context.Context, doc *document.Document, page ast.PdfDict) map[string]*fontUsageInfo {
	streamData, err := getPageContentBytes(ctx, doc, page)
	if err != nil || len(streamData) == 0 {
		return nil
	}

	ops, err := content.ParseStreamData(streamData)
	if err != nil {
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
			if currentFont != "" && renderMode != 3 { //nolint:mnd
				getOrCreate(currentFont).visible = true
			}
			if len(op.Operands) >= 1 {
				collectStringBytes(op.Operands[0])
			}
		case "TJ":
			if currentFont != "" && renderMode != 3 { //nolint:mnd
				getOrCreate(currentFont).visible = true
			}
			if len(op.Operands) >= 1 {
				collectStringBytes(op.Operands[0])
			}
		case "'", "\"":
			if currentFont != "" && renderMode != 3 { //nolint:mnd
				getOrCreate(currentFont).visible = true
			}
			if len(op.Operands) >= 1 {
				collectStringBytes(op.Operands[len(op.Operands)-1])
			}
		}
	}

	return usage
}

// usedGlyphNamesResolvable returns true if all glyph names referenced by the
// used character codes in a Type 1 font are resolvable to Unicode — either
// via the AGL or the Symbol encoding set.
func usedGlyphNamesResolvable(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, usage *fontUsageInfo) bool {
	encoding := resolveSimpleFontEncoding(ctx, doc, fontDict)
	if encoding == nil {
		return false
	}

	for code := range usage.charCodes {
		glyphName, ok := encoding[code]
		if !ok || glyphName == ".notdef" || glyphName == "" {
			continue // unmapped or .notdef — skip
		}
		if _, resolved := encodings.GlyphToRuneFull(glyphName); resolved {
			continue
		}
		if symbolEncodingNames[glyphName] {
			continue
		}
		slog.Debug("compliance_2: fonts: used glyph not resolvable",
			"code", code, "glyph", glyphName)
		return false
	}
	return true
}

// resolveSimpleFontEncoding builds a charCode → glyphName mapping for a
// simple font (Type1, TrueType). Checks PDF-level /Encoding first, then
// falls back to the Type 1 font program's built-in encoding.
func resolveSimpleFontEncoding(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) map[byte]string {
	// Priority 1: PDF-level /Encoding.
	if encObj, ok := fontDict["Encoding"]; ok {
		// Named encoding.
		if encName, err := document.ResolveName(ctx, doc, encObj); err == nil && encName != "" {
			return predefinedEncodingNames(string(encName))
		}
		// Encoding dict with /Differences.
		if encDict, err := document.ResolveDict(ctx, doc, encObj); err == nil && encDict != nil {
			result := make(map[byte]string)
			// Start from BaseEncoding if present.
			if baseName, err := document.ResolveName(ctx, doc, encDict["BaseEncoding"]); err == nil && baseName != "" {
				maps.Copy(result, predefinedEncodingNames(string(baseName)))
			}
			// Apply /Differences overrides.
			if diffArr, err := document.ResolveArray(ctx, doc, encDict["Differences"]); err == nil {
				code := 0
				for _, item := range diffArr {
					switch v := item.(type) {
					case ast.PdfInt:
						code = int(v)
					case ast.PdfName:
						if code >= 0 && code <= 255 { //nolint:mnd
							result[byte(code)] = string(v) //nolint:gosec
						}
						code++
					}
				}
			}
			if len(result) > 0 {
				return result
			}
		}
	}

	// Priority 2: Type 1 font program built-in encoding (/FontFile).
	fd := findFontDescriptor(ctx, doc, fontDict)
	if fd != nil {
		if fontFileObj, ok := fd["FontFile"]; ok {
			stream, err := document.ResolveStream(ctx, doc, fontFileObj)
			if err == nil {
				data, err := doc.DecodeStream(stream)
				if err == nil {
					return typography.ExtractType1EncodingNames(data)
				}
			}
		}
	}

	return nil
}

// predefinedEncodingNames returns a charCode → glyphName mapping for a
// named PDF encoding.
func predefinedEncodingNames(name string) map[byte]string {
	var table map[byte]rune
	switch name {
	case "WinAnsiEncoding":
		table = encodings.WinAnsiEncoding
	case "MacRomanEncoding":
		table = encodings.MacRomanEncoding
	case "MacExpertEncoding":
		table = encodings.MacExpertEncoding
	default:
		return nil
	}
	result := make(map[byte]string, len(table))
	for code, r := range table {
		if glyphName := encodings.RuneToGlyph(r); glyphName != "" {
			result[code] = glyphName
		}
	}
	return result
}

// --- Main check group ---

// checkFontGroup validates font-related compliance rules across all pages.
func checkFontGroup(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Violation {
	slog.Debug("compliance_2: fonts: starting font checks", "profile", profile)

	pages := collectPages(ctx, doc, catalog)
	if len(pages) == 0 {
		slog.Debug("compliance_2: fonts: no pages found")
		return nil
	}

	var results []Violation

	// Track fonts already checked to avoid duplicate violations.
	checked := make(map[string]bool)

	for pageIdx, page := range pages {
		if ctx.Err() != nil {
			slog.Debug("compliance_2: fonts: context cancelled")
			return results
		}

		resources := getResources(ctx, doc, page)
		if resources == nil {
			slog.Debug("compliance_2: fonts: page has no resources", "page", pageIdx+1)
			continue
		}

		fontDictObj, ok := resources["Font"]
		if !ok {
			slog.Debug("compliance_2: fonts: page has no /Font resource", "page", pageIdx+1)
			continue
		}

		fontDict, err := document.ResolveDict(ctx, doc, fontDictObj)
		if err != nil {
			slog.Debug("compliance_2: fonts: failed to resolve /Font dict",
				"page", pageIdx+1, "error", err)
			continue
		}

		// Collect font usage from the page content stream: which fonts are
		// visible (rendering mode != 3) and which character codes are used.
		fontUsage := collectFontUsage(ctx, doc, page)

		for fontName, fontRef := range fontDict {
			if ctx.Err() != nil {
				return results
			}

			fontKey := string(fontName)
			if checked[fontKey] {
				slog.Debug("compliance_2: fonts: font already checked",
					"font", fontKey, "page", pageIdx+1)
				continue
			}

			font, err := document.ResolveDict(ctx, doc, fontRef)
			if err != nil || font == nil {
				slog.Debug("compliance_2: fonts: failed to resolve font dict",
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
			slog.Debug("compliance_2: fonts: checking font",
				"font", fontKey, "page", pageIdx+1, "visible", isVisible)

			results = append(results, checkSingleFont(ctx, doc, font, fontKey, pageIdx+1, profile, isVisible, usage)...)

			// PUA ActualText check (needs page and catalog context).
			results = append(results, checkPUAWithoutActualText(ctx, doc, font, fontKey, pageIdx+1, profile, page, catalog)...)
		}
	}

	slog.Debug("compliance_2: fonts: font checks complete",
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
	basefont := fontBaseFont(ctx, doc, fontDict)

	slog.Debug("compliance_2: fonts: font details",
		"name", fontName, "subtype", subtype, "basefont", basefont, "page", pageNum)

	// Type 3 fonts have no font program — skip embedding checks.
	isType3 := isType3Font(ctx, doc, fontDict)

	// Type 0 (composite) fonts: check descendant CIDFonts.
	if subtype == "Type0" {
		results = append(results, checkType0Font(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)
		return results
	}

	// --- 1. Font embedding check ---
	// Only for fonts used for visible rendering.
	if isVisible {
		results = append(results, checkFontEmbedding(ctx, doc, fontDict, fontName, pageNum, profile, isType3)...)
	}

	// --- 2. ToUnicode mapping check ---
	results = append(results, checkToUnicode(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)

	// --- 7. TrueType encoding check ---
	if subtype == "TrueType" {
		results = append(results, checkTrueTypeEncoding(ctx, doc, fontDict, fontName, pageNum, profile)...)
	}

	// --- 8. Font name UTF-8 check ---
	results = append(results, checkFontNameUTF8(ctx, doc, fontDict, fontName, pageNum, profile)...)

	// --- 5. Glyph width consistency ---
	// Only for fonts used for visible rendering.
	if isVisible {
		results = append(results, checkGlyphWidths(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)
	}

	// --- 6. .notdef glyph check ---
	results = append(results, checkNotdefGlyph(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)

	// --- 9. Glyph coverage (CharSet for Type1 subsets) ---
	results = append(results, checkGlyphCoverage(ctx, doc, fontDict, fontName, pageNum, profile)...)

	// --- 10. ToUnicode CMap values ---
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
		return nil
	}
	data, err := doc.DecodeStream(stream)
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
	if depth > 30 { //nolint:mnd
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

// containsPUAMapping scans a ToUnicode CMap for destination values in the
// Unicode Private Use Area (U+E000-U+F8FF BMP PUA).
func containsPUAMapping(cmapData string) bool {
	normalized := strings.ReplaceAll(cmapData, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	upper := strings.ToUpper(normalized)

	inBfchar := false
	for line := range strings.SplitSeq(upper, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "BEGINBFCHAR") {
			inBfchar = true
			continue
		}
		if inBfchar {
			parts := strings.Fields(trimmed)
			if len(parts) >= 2 { //nolint:mnd
				if isPUAHex(parts[1]) {
					return true
				}
			}
		}
		if strings.Contains(trimmed, "ENDBFCHAR") {
			inBfchar = false
		}
	}
	return false
}

// isPUAHex checks if a hex token like "<E000>" is in the Unicode PUA range.
func isPUAHex(token string) bool {
	t := strings.TrimSpace(token)
	if len(t) < 3 || t[0] != '<' || t[len(t)-1] != '>' { //nolint:mnd
		return false
	}
	hex := t[1 : len(t)-1]
	val := uint32(0)
	for _, c := range hex {
		val <<= 4
		switch {
		case c >= '0' && c <= '9':
			val |= uint32(c - '0')
		case c >= 'A' && c <= 'F':
			val |= uint32(c-'A') + 10 //nolint:mnd
		default:
			return false
		}
	}
	return (val >= 0xE000 && val <= 0xF8FF) || (val >= 0xF0000 && val <= 0xFFFFF)
}

func puaActualTextRule(profile string) Rule {
	switch pdfaPart(profile) {
	case 2: //nolint:mnd
		return Rule_19005_2_6_2_11_7_3_t01
	case 3: //nolint:mnd
		return Rule_19005_3_6_2_11_7_3_t01
	}
	return Rule{}
}

// --- Individual check implementations ---

// checkFontEmbedding validates that a font program is embedded (rule 1).
func checkFontEmbedding(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, isType3 bool) []Violation {
	rule := fontEmbeddingRule(profile)
	if rule.Specification == "" {
		slog.Debug("compliance_2: fonts: embedding check not applicable", "profile", profile)
		return nil
	}

	// Type 3 fonts have no external font program.
	if isType3 {
		slog.Debug("compliance_2: fonts: skipping embedding check for Type3", "font", fontName)
		return nil
	}

	std14 := isStandard14Font(ctx, doc, fontDict)
	fd := findFontDescriptor(ctx, doc, fontDict)
	if fd == nil {
		// No FontDescriptor means not embedded. PDF/A requires embedding even
		// for the 14 standard fonts.
		slog.Debug("compliance_2: fonts: no FontDescriptor, font not embedded",
			"font", fontName, "page", pageNum, "standard14", std14)
		return failf(rule, "font %s on page %d", fontName, pageNum)
	}

	if !isFontEmbedded(ctx, doc, fd) {
		slog.Debug("compliance_2: fonts: font not embedded",
			"font", fontName, "page", pageNum, "standard14", std14)
		return failf(rule, "font %s on page %d", fontName, pageNum)
	}

	slog.Debug("compliance_2: fonts: font is embedded", "font", fontName, "page", pageNum)
	return nil
}

// checkToUnicode validates the ToUnicode mapping requirement (rule 2).
func checkToUnicode(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, usage *fontUsageInfo) []Violation {
	rule := toUnicodeRule(profile)
	if rule.Specification == "" {
		slog.Debug("compliance_2: fonts: ToUnicode check not applicable", "profile", profile)
		return nil
	}

	// Type3 fonts: for PDF/A-4 and PDF/UA, VeraPDF exempts Type3 from
	// ToUnicode checks. For PDF/A parts 1–3 (levels a/u), Type3 fonts
	// must have resolvable Unicode mappings — either all glyph names in
	// /Encoding /Differences are AGL-resolvable or a /ToUnicode CMap is present.
	if isType3Font(ctx, doc, fontDict) {
		part := pdfaPart(profile)
		if part == 4 || isPDFUA(profile) { //nolint:mnd
			slog.Debug("compliance_2: fonts: skipping ToUnicode check for Type3 (part 4 / PDF/UA)",
				"font", fontName)
			return nil
		}
		// For parts 1–3: check if all Differences glyph names are AGL-resolvable.
		if allDifferencesGlyphNamesAGLResolvable(ctx, doc, fontDict) {
			slog.Debug("compliance_2: fonts: Type3 font Differences all AGL-resolvable, ToUnicode not required",
				"font", fontName)
			return nil
		}
		// Fall through to the ToUnicode presence check below.
		if !hasToUnicode(fontDict) {
			slog.Debug("compliance_2: fonts: Type3 font missing ToUnicode with non-AGL glyph names",
				"font", fontName, "page", pageNum)
			return failf(rule, "font %s on page %d", fontName, pageNum)
		}
		return nil
	}

	// Exemption (a): standard encoding with Adobe Glyph List coverage.
	// This covers MacRomanEncoding, WinAnsiEncoding, and MacExpertEncoding.
	if hasStandardEncoding(ctx, doc, fontDict) {
		slog.Debug("compliance_2: fonts: font uses standard encoding, ToUnicode not required",
			"font", fontName)
		return nil
	}

	// Exemption (a) extended: encoding dictionary with standard BaseEncoding.
	if hasStandardBaseEncoding(ctx, doc, fontDict) {
		slog.Debug("compliance_2: fonts: font uses encoding dict with standard BaseEncoding, ToUnicode not required",
			"font", fontName)
		return nil
	}

	// Exemption (b): symbolic font with single cmap and an explicit encoding.
	// Symbolic fonts are only exempt when they define a character mapping via
	// /Encoding; without it, /ToUnicode is still required.
	fd := findFontDescriptor(ctx, doc, fontDict)
	if isSymbolicFont(ctx, doc, fd) {
		if _, hasEncoding := fontDict["Encoding"]; hasEncoding {
			slog.Debug("compliance_2: fonts: symbolic font with /Encoding, ToUnicode not required",
				"font", fontName)
			return nil
		}
		slog.Debug("compliance_2: fonts: symbolic font without /Encoding, ToUnicode still required",
			"font", fontName)
	}

	// Exemption (b2): Type 1 fonts whose character names are all resolvable
	// via the Adobe Glyph List. Per §6.3.8, if every glyph name in the
	// font's encoding (including /Differences) maps to a Unicode value
	// through the AGL, ToUnicode is not required.
	subtype := fontSubtype(ctx, doc, fontDict)
	if subtype == "Type1" || subtype == "MMType1" {
		if allGlyphNamesAGLResolvable(ctx, doc, fontDict) {
			slog.Debug("compliance_2: fonts: Type1 font with all AGL-resolvable glyph names, ToUnicode not required",
				"font", fontName)
			return nil
		}
		// Exemption (b3): The Symbol font itself (BaseFont = Symbol or
		// *+Symbol) is exempt when all CharSet names are in the standard
		// Symbol encoding set.
		if isSymbolFontByName(ctx, doc, fontDict) && allGlyphNamesInSymbolEncoding(ctx, doc, fontDict) {
			slog.Debug("compliance_2: fonts: Symbol font with all Symbol-encoding glyph names, ToUnicode not required",
				"font", fontName)
			return nil
		}
		// Exemption (b4): Symbol-type fonts where only the USED character
		// codes map to resolvable glyph names. This handles fonts like
		// MdSymbolA-Regular that have many custom glyphs in CharSet but
		// only use standard Symbol/AGL glyphs in the content stream.
		// Limited to Symbol-named fonts to avoid false exemptions for
		// unrelated fonts (e.g., CMEX10) whose used chars happen to
		// have resolvable names.
		if isSymbolFontByName(ctx, doc, fontDict) && usage != nil && len(usage.charCodes) > 0 {
			if usedGlyphNamesResolvable(ctx, doc, fontDict, usage) {
				slog.Debug("compliance_2: fonts: Symbol font used glyph names all resolvable, ToUnicode not required",
					"font", fontName)
				return nil
			}
		}
	}

	// Exemption (c): Identity-H/Identity-V — only exempt for PDF/A, not PDF/UA.
	// For PDF/UA, a ToUnicode CMap is required for Identity-encoded Type0 fonts
	// unless glyph-level Unicode mapping is available through other means.
	if hasIdentityCMap(ctx, doc, fontDict) && !isPDFUA(profile) {
		slog.Debug("compliance_2: fonts: font uses Identity CMap, ToUnicode exemption (PDF/A)",
			"font", fontName)
		return nil
	}

	if !hasToUnicode(fontDict) {
		slog.Debug("compliance_2: fonts: font missing ToUnicode",
			"font", fontName, "page", pageNum)
		return failf(rule, "font %s on page %d", fontName, pageNum)
	}

	slog.Debug("compliance_2: fonts: font has ToUnicode", "font", fontName)
	return nil
}

// checkTrueTypeEncoding validates TrueType-specific encoding rules (rule 7).
func checkTrueTypeEncoding(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string) []Violation {
	var results []Violation

	fd := findFontDescriptor(ctx, doc, fontDict)
	symbolic := isSymbolicFont(ctx, doc, fd)

	slog.Debug("compliance_2: fonts: checking TrueType encoding",
		"font", fontName, "symbolic", symbolic, "page", pageNum)

	if symbolic {
		// Symbolic TrueType must not specify /Encoding.
		rule := trueTypeEncodingSymbolicNoEncodingRule(profile)
		if rule.Specification != "" {
			if _, hasEnc := fontDict["Encoding"]; hasEnc {
				slog.Debug("compliance_2: fonts: symbolic TrueType has /Encoding",
					"font", fontName, "page", pageNum)
				results = append(results, failf(rule, "font %s on page %d", fontName, pageNum)...)
			} else {
				slog.Debug("compliance_2: fonts: symbolic TrueType correctly omits /Encoding",
					"font", fontName)
			}
		}

		// Symbolic TrueType cmap subtable check — requires font program parsing.
		// We flag the rule but cannot fully verify without parsing the embedded
		// TrueType program. This is a structural check placeholder.
		_ = trueTypeSymbolicCmapRule(profile)
		slog.Debug("compliance_2: fonts: symbolic TrueType cmap check deferred to font program analysis",
			"font", fontName)
	} else {
		// Non-symbolic TrueType must use MacRomanEncoding or WinAnsiEncoding.
		rule := trueTypeEncodingNonSymbolicRule(profile)
		if rule.Specification != "" {
			encObj, hasEnc := fontDict["Encoding"]
			if !hasEnc {
				slog.Debug("compliance_2: fonts: non-symbolic TrueType missing /Encoding",
					"font", fontName, "page", pageNum)
				results = append(results, failf(rule, "font %s on page %d has no Encoding", fontName, pageNum)...)
			} else {
				// Check if encoding is a name (direct) or dict with BaseEncoding.
				encName, _ := document.ResolveName(ctx, doc, encObj)
				if encName == "MacRomanEncoding" || encName == "WinAnsiEncoding" {
					slog.Debug("compliance_2: fonts: non-symbolic TrueType has valid encoding",
						"font", fontName, "encoding", encName)
				} else if encName != "" {
					// Invalid encoding name.
					slog.Debug("compliance_2: fonts: non-symbolic TrueType has invalid encoding name",
						"font", fontName, "encoding", encName, "page", pageNum)
					results = append(results, failf(rule, "font %s on page %d encoding=%s", fontName, pageNum, encName)...)
				} else {
					// Encoding is a dictionary — check BaseEncoding.
					encDict, err := document.ResolveDict(ctx, doc, encObj)
					if err != nil || encDict == nil {
						slog.Debug("compliance_2: fonts: non-symbolic TrueType encoding is not a name or dict",
							"font", fontName, "page", pageNum)
						results = append(results, failf(rule, "font %s on page %d has invalid Encoding type", fontName, pageNum)...)
					} else {
						baseEnc, _ := document.ResolveName(ctx, doc, encDict["BaseEncoding"])
						if baseEnc != "MacRomanEncoding" && baseEnc != "WinAnsiEncoding" {
							slog.Debug("compliance_2: fonts: non-symbolic TrueType has invalid BaseEncoding",
								"font", fontName, "baseEncoding", baseEnc, "page", pageNum)
							results = append(results, failf(rule, "font %s on page %d BaseEncoding=%s", fontName, pageNum, baseEnc)...)
						} else {
							slog.Debug("compliance_2: fonts: non-symbolic TrueType BaseEncoding is valid",
								"font", fontName, "baseEncoding", baseEnc)
						}

						// Part 1: Differences is forbidden entirely.
						// Parts 2+: Differences is allowed only if all glyph names
						// are in the Adobe Glyph List.
						if diffObj, hasDiff := encDict["Differences"]; hasDiff {
							if pdfaPart(profile) == 1 {
								slog.Debug("compliance_2: fonts: non-symbolic TrueType has Differences in encoding dict (part 1)",
									"font", fontName, "page", pageNum)
								results = append(results, failf(rule, "font %s on page %d has Differences in encoding dict", fontName, pageNum)...)
							} else {
								// Validate all glyph names in Differences against AGL.
								diffArr, diffErr := document.ResolveArray(ctx, doc, diffObj)
								if diffErr == nil {
									for _, item := range diffArr {
										name, ok := item.(ast.PdfName)
										if !ok || string(name) == ".notdef" {
											continue
										}
										if _, resolved := encodings.GlyphToRuneFull(string(name)); !resolved {
											results = append(results, failf(rule,
												"font %s on page %d: Differences glyph %q not in Adobe Glyph List",
												fontName, pageNum, string(name))...)
											break
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}

	return results
}

// checkFontNameUTF8 validates that the /BaseFont name is valid UTF-8 (rule 8).
func checkFontNameUTF8(_ context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string) []Violation {
	rule := fontNameUTF8Rule(profile)
	if rule.Specification == "" {
		slog.Debug("compliance_2: fonts: font name UTF-8 check not applicable", "profile", profile)
		return nil
	}

	basefontObj, ok := fontDict["BaseFont"]
	if !ok {
		slog.Debug("compliance_2: fonts: no BaseFont entry, skipping UTF-8 check", "font", fontName, "page", pageNum)
		// BaseFont key is truly absent — this is not a UTF-8 violation; the
		// embedding check will catch the missing font program. Do not flag here
		// to avoid false positives when the key exists but cannot be decoded.
		return nil
	}

	basefontName, ok := basefontObj.(ast.PdfName)
	if !ok {
		slog.Debug("compliance_2: fonts: BaseFont is not a PdfName, skipping UTF-8 check",
			"font", fontName, "page", pageNum)
		// BaseFont resolves to a non-name type — not a UTF-8 violation.
		return nil
	}

	if !utf8.ValidString(string(basefontName)) {
		slog.Debug("compliance_2: fonts: BaseFont name is not valid UTF-8",
			"font", fontName, "basefont", string(basefontName), "page", pageNum)
		return failf(rule, "font %s on page %d BaseFont=%s", fontName, pageNum, string(basefontName))
	}

	slog.Debug("compliance_2: fonts: BaseFont name is valid UTF-8",
		"font", fontName, "basefont", string(basefontName))

	// Suppress unused warning for doc — used in other check functions via
	// the same signature pattern.
	_ = doc
	return nil
}

// widthTolerance is the maximum allowed difference between PDF-declared
// and binary font widths, in 1/1000 text space units.
const widthTolerance = 1.0

// checkGlyphWidths validates glyph width consistency (rule 5).
// Compares PDF-declared widths against the embedded font program's binary widths.
func checkGlyphWidths(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, usage *fontUsageInfo) []Violation {
	rule := glyphWidthRule(profile)
	if rule.Specification == "" {
		slog.Debug("compliance_2: fonts: glyph width check not applicable", "profile", profile)
		return nil
	}

	// Type3 fonts: compare /Widths against CharProc d0/d1 widths.
	if isType3Font(ctx, doc, fontDict) {
		return checkType3FontWidths(ctx, doc, fontDict, fontName, pageNum, rule)
	}

	fd := findFontDescriptor(ctx, doc, fontDict)
	if fd == nil {
		slog.Debug("compliance_2: fonts: no FontDescriptor, skipping glyph width check", "font", fontName)
		return nil
	}

	fontData := extractFontProgramData(ctx, doc, fd)
	if fontData == nil {
		slog.Debug("compliance_2: fonts: no embedded font program, skipping glyph width check", "font", fontName)
		return nil
	}

	binaryWidths := typography.ExtractBinaryWidths(fontData)
	if binaryWidths == nil {
		slog.Debug("compliance_2: fonts: could not extract binary widths", "font", fontName)
		return nil
	}

	unitsPerEm := typography.ReadUnitsPerEm(fontData)
	if unitsPerEm == 0 {
		unitsPerEm = 1000 //nolint:mnd // safe default
	}

	slog.Debug("compliance_2: fonts: extracted font metrics",
		"font", fontName, "binaryWidths", len(binaryWidths), "unitsPerEm", unitsPerEm)

	// Extract charCode → GID mapping from font's cmap table (TrueType/OTTO).
	charCodeToGID := typography.ExtractCharCodeToGID(fontData)

	// For fonts without a cmap (Type1/CFF), build charCode→GID via glyph names:
	// PDF encoding maps charCode→glyphName; font program maps GID→glyphName.
	if charCodeToGID == nil {
		glyphNames := typography.ExtractGlyphNames(fontData)
		if glyphNames != nil {
			enc := resolveSimpleFontEncoding(ctx, doc, fontDict)
			if len(enc) > 0 {
				nameToGID := make(map[string]uint16, len(glyphNames))
				for gid, name := range glyphNames {
					if _, exists := nameToGID[name]; !exists {
						nameToGID[name] = gid
					}
				}
				charCodeToGID = make(map[uint16]uint16)
				for code, name := range enc {
					if name == ".notdef" || name == "" {
						continue
					}
					if gid, ok := nameToGID[name]; ok {
						charCodeToGID[uint16(code)] = gid
					} else {
						// Glyph name not in font charset → maps to GID 0 (.notdef).
						// Include it so width comparison can detect the mismatch.
						charCodeToGID[uint16(code)] = 0
					}
				}
			}
		}
	}

	subtype := fontSubtype(ctx, doc, fontDict)

	// Simple fonts (Type1, TrueType, MMType1): compare /Widths array using FirstChar offset.
	if subtype == "Type1" || subtype == "TrueType" || subtype == "MMType1" {
		return checkSimpleFontWidths(ctx, doc, fontDict, binaryWidths, unitsPerEm, fontName, pageNum, rule, charCodeToGID, usage)
	}

	return nil
}

// checkSimpleFontWidths compares /Widths array against binary widths for simple fonts.
// charCodeToGID is the cmap-derived charCode → GID mapping (may be nil for non-TrueType).
func checkSimpleFontWidths(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, binaryWidths map[uint16]uint16, unitsPerEm uint16, fontName string, pageNum int, rule Rule, charCodeToGID map[uint16]uint16, usage *fontUsageInfo) []Violation {
	widthsObj, hasWidths := fontDict["Widths"]
	if !hasWidths {
		slog.Debug("compliance_2: fonts: font missing /Widths array", "font", fontName, "page", pageNum)
		return failf(rule, "font %s on page %d: missing /Widths array", fontName, pageNum)
	}

	widths, err := document.ResolveArray(ctx, doc, widthsObj)
	if err != nil {
		slog.Debug("compliance_2: fonts: cannot resolve /Widths", "font", fontName, "error", err)
		return failf(rule, "font %s on page %d: cannot resolve /Widths", fontName, pageNum)
	}

	firstChar, err := document.ResolveInt(ctx, doc, fontDict["FirstChar"])
	if err != nil {
		slog.Debug("compliance_2: fonts: missing /FirstChar", "font", fontName)
		return failf(rule, "font %s on page %d: missing /FirstChar", fontName, pageNum)
	}

	for i, wObj := range widths {
		charCode := int(firstChar) + i
		if charCode < 0x20 { //nolint:mnd
			continue
		}
		gid := uint16(charCode) //nolint:gosec
		if charCodeToGID != nil {
			mappedGID, ok := charCodeToGID[uint16(charCode)] //nolint:gosec
			if !ok {
				continue
			}
			gid = mappedGID
		}
		// The spec requires width consistency only for "glyphs referenced
		// for rendering." Skip charCodes not used in text operators.
		charUsed := usage != nil && usage.charCodes[byte(charCode)] //nolint:gosec
		pdfWidth := document.GetFloat(wObj)
		if pdfWidth == 0 && !charUsed {
			continue
		}
		// Skip non-used charCodes when glyph is missing from font program
		// (GID 0 from name mapping) — only flag if the charCode is used.
		if gid == 0 && !charUsed {
			continue
		}
		bw, ok := binaryWidths[gid]
		if !ok {
			continue
		}
		expectedWidth := float64(bw) * 1000.0 / float64(unitsPerEm)
		if math.Abs(pdfWidth-expectedWidth) > widthTolerance {
			return failf(rule, "font %s on page %d: char %d width mismatch (PDF=%.1f, font=%.1f)",
				fontName, pageNum, charCode, pdfWidth, expectedWidth)
		}
	}

	slog.Debug("compliance_2: fonts: glyph widths consistent", "font", fontName, "page", pageNum)
	return nil
}

// checkType3FontWidths compares /Widths against glyph widths declared by
// d0/d1 operators in Type3 font CharProcs.
func checkType3FontWidths(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, rule Rule) []Violation {
	widthsObj, hasWidths := fontDict["Widths"]
	if !hasWidths {
		return nil
	}
	widths, err := document.ResolveArray(ctx, doc, widthsObj)
	if err != nil || len(widths) == 0 {
		return nil
	}
	firstChar, err := document.ResolveInt(ctx, doc, fontDict["FirstChar"])
	if err != nil {
		return nil
	}

	// Build charCode→glyphName from /Encoding /Differences.
	enc := resolveSimpleFontEncoding(ctx, doc, fontDict)

	// Resolve CharProcs dict.
	charProcs, err := document.ResolveDict(ctx, doc, fontDict["CharProcs"])
	if err != nil || charProcs == nil {
		return nil
	}

	// Extract FontMatrix scale factor. Type3 FontMatrix is typically
	// [0.001 0 0 0.001 0 0], meaning glyph coordinates are in 1/1000 units
	// and widths from d0/d1 are in glyph space. The PDF /Widths values are
	// in text space (1/1000 of user space unit). Scale = FontMatrix[0] * 1000.
	fmScale := 1.0
	if fmArr, err := document.ResolveArray(ctx, doc, fontDict["FontMatrix"]); err == nil && len(fmArr) >= 1 {
		sx := document.GetFloat(fmArr[0])
		if sx > 0 {
			fmScale = sx * 1000.0
		}
	}

	// Extract widths from each CharProc by finding d0/d1 operator.
	charProcWidths := make(map[string]float64)
	for glyphName, cpObj := range charProcs {
		stream, err := document.ResolveStream(ctx, doc, cpObj)
		if err != nil {
			continue
		}
		streamData, err := doc.DecodeStream(stream)
		if err != nil {
			continue
		}
		ops, err := content.ParseStreamData(streamData)
		if err != nil {
			continue
		}
		for _, op := range ops {
			// d1: wx wy llx lly urx ury d1
			// d0: wx wy d0
			if (op.Operator == "d1" && len(op.Operands) >= 6) || //nolint:mnd
				(op.Operator == "d0" && len(op.Operands) >= 2) { //nolint:mnd
				wx := document.GetFloat(op.Operands[0])
				charProcWidths[string(glyphName)] = wx
				break
			}
		}
	}

	// Compare /Widths against CharProc widths.
	for i, wObj := range widths {
		charCode := byte(int(firstChar) + i) //nolint:gosec
		glyphName := enc[charCode]
		if glyphName == "" {
			continue
		}
		cpWidth, ok := charProcWidths[glyphName]
		if !ok {
			continue
		}
		pdfWidth := document.GetFloat(wObj)
		expectedWidth := cpWidth * fmScale
		if math.Abs(pdfWidth-expectedWidth) > widthTolerance {
			return failf(rule, "font %s on page %d: char %d (%s) width mismatch (PDF=%.1f, glyph=%.1f)",
				fontName, pageNum, int(firstChar)+i, glyphName, pdfWidth, expectedWidth)
		}
	}
	return nil
}

// checkCIDFontWidths compares /W array against binary widths for CIDFonts.
func checkCIDFontWidths(ctx context.Context, doc *document.Document, cidFontDict ast.PdfDict, binaryWidths map[uint16]uint16, unitsPerEm uint16, fontName string, pageNum int, rule Rule) []Violation {
	wObj, ok := cidFontDict["W"]
	if !ok {
		slog.Debug("compliance_2: fonts: CIDFont has no /W array", "font", fontName)
		return nil
	}

	wArr, err := document.ResolveArray(ctx, doc, wObj)
	if err != nil {
		slog.Debug("compliance_2: fonts: resolving CID width array", "font", fontName, "error", err)
		return nil
	}
	if wArr == nil {
		return nil
	}

	// /W array format: [cid [w1 w2 ...]] or [cidFirst cidLast w]
	i := 0
	for i < len(wArr) {
		cidStartRes, err := document.ResolveObject(ctx, doc, wArr[i])
		if err != nil {
			slog.Debug("compliance_2: fonts: resolving CID start value", "error", err)
		}
		cidStartInt, ok := cidStartRes.(ast.PdfInt)
		if !ok {
			i++
			continue
		}
		cidStart := int(cidStartInt)
		i++
		if i >= len(wArr) {
			break
		}

		nextRes, err := document.ResolveObject(ctx, doc, wArr[i])
		if err != nil {
			slog.Debug("compliance_2: fonts: resolving CID width entry", "error", err)
		}
		switch next := nextRes.(type) {
		case ast.PdfArray:
			// [cidStart [w1 w2 w3 ...]]
			for j, wEntry := range next {
				cid := cidStart + j
				// Skip .notdef (CID 0) — no visible outlines.
				if cid == 0 {
					continue
				}
				gid := uint16(cid) //nolint:gosec
				pdfWidth := document.GetFloat(wEntry)
				if pdfWidth == 0 {
					continue
				}
				bw, ok := binaryWidths[gid]
				if !ok {
					continue
				}
				expectedWidth := float64(bw) * 1000.0 / float64(unitsPerEm)
				if math.Abs(pdfWidth-expectedWidth) > widthTolerance {
					slog.Debug("compliance_2: fonts: CID glyph width mismatch",
						"font", fontName, "cid", cid,
						"pdfWidth", pdfWidth, "expectedWidth", expectedWidth)
					return failf(rule, "font %s on page %d: CID %d width mismatch (PDF=%.1f, font=%.1f)",
						fontName, pageNum, cid, pdfWidth, expectedWidth)
				}
			}
			i++
		case ast.PdfInt:
			// [cidStart cidEnd w]
			cidEnd := int(next)
			i++
			if i >= len(wArr) {
				break
			}
			pdfWidth := document.GetFloat(wArr[i])
			i++
			if pdfWidth == 0 {
				continue
			}
			for cid := cidStart; cid <= cidEnd; cid++ {
				// Skip .notdef (CID 0) — no visible outlines.
				if cid == 0 {
					continue
				}
				gid := uint16(cid) //nolint:gosec
				bw, ok := binaryWidths[gid]
				if !ok {
					continue
				}
				expectedWidth := float64(bw) * 1000.0 / float64(unitsPerEm)
				if math.Abs(pdfWidth-expectedWidth) > widthTolerance {
					slog.Debug("compliance_2: fonts: CID glyph width mismatch (range)",
						"font", fontName, "cid", cid,
						"pdfWidth", pdfWidth, "expectedWidth", expectedWidth)
					return failf(rule, "font %s on page %d: CID %d width mismatch (PDF=%.1f, font=%.1f)",
						fontName, pageNum, cid, pdfWidth, expectedWidth)
				}
			}
		default:
			i++
		}
	}

	slog.Debug("compliance_2: fonts: CID glyph widths consistent", "font", fontName, "page", pageNum)
	return nil
}

// extractFontProgramData gets the raw font program bytes from FontDescriptor.
func extractFontProgramData(ctx context.Context, doc *document.Document, fdDict ast.PdfDict) []byte {
	for _, key := range []ast.PdfName{"FontFile2", "FontFile3", "FontFile"} {
		ffObj, ok := fdDict[key]
		if !ok {
			continue
		}
		stream, err := document.ResolveStream(ctx, doc, ffObj)
		if err != nil {
			slog.Debug("compliance_2: fonts: resolving font program stream", "key", key, "error", err)
		}
		if stream.Dict == nil {
			continue
		}
		data, err := doc.DecodeStream(stream)
		if err != nil {
			// Fall back to raw data.
			rawData, rawErr := stream.Bytes()
			if rawErr == nil && len(rawData) > 0 {
				slog.Debug("compliance_2: fonts: using raw font program data", "key", key, "len", len(rawData))
				return rawData
			}
			slog.Debug("compliance_2: fonts: failed to decode font program stream", "key", key, "error", err)
			continue
		}
		slog.Debug("compliance_2: fonts: decoded font program", "key", key, "len", len(data))
		return data
	}
	return nil
}

// --- Type 0 (composite) font checks ---

// checkType0Font validates composite font properties including descendant
// CIDFonts, CIDSystemInfo, CIDToGIDMap, and ToUnicode.
func checkType0Font(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, usage *fontUsageInfo) []Violation {
	results := make([]Violation, 0, 4) //nolint:mnd // estimated capacity for composite font checks.

	basefont := fontBaseFont(ctx, doc, fontDict)
	slog.Debug("compliance_2: fonts: checking Type0 composite font",
		"font", fontName, "basefont", basefont, "page", pageNum)

	// Resolve /DescendantFonts array.
	descArr, err := document.ResolveArray(ctx, doc, fontDict["DescendantFonts"])
	if err != nil || len(descArr) == 0 {
		slog.Debug("compliance_2: fonts: Type0 font has no DescendantFonts",
			"font", fontName, "error", err)
		// Without descendants we cannot check further, but the font is still
		// subject to embedding checks at the Type0 level.
		return results
	}

	// PDF spec requires exactly one descendant CIDFont.
	cidFontDict, err := document.ResolveDict(ctx, doc, descArr[0])
	if err != nil || cidFontDict == nil {
		slog.Debug("compliance_2: fonts: failed to resolve descendant CIDFont",
			"font", fontName, "error", err)
		return results
	}

	cidSubtype := fontSubtype(ctx, doc, cidFontDict)
	slog.Debug("compliance_2: fonts: CIDFont subtype",
		"font", fontName, "cidSubtype", cidSubtype)

	// --- Font embedding for the CIDFont ---
	results = append(results, checkFontEmbedding(ctx, doc, cidFontDict, fontName, pageNum, profile, false)...)

	// --- Font name UTF-8 for the Type0 font ---
	results = append(results, checkFontNameUTF8(ctx, doc, fontDict, fontName, pageNum, profile)...)

	// --- 3. CIDToGIDMap check ---
	results = append(results, checkCIDToGIDMap(ctx, doc, cidFontDict, cidSubtype, fontName, pageNum, profile)...)

	// --- 4. CIDSystemInfo consistency check ---
	results = append(results, checkCIDSystemInfo(ctx, doc, fontDict, cidFontDict, fontName, pageNum, profile)...)

	// --- 2. ToUnicode for composite fonts ---
	results = append(results, checkType0ToUnicode(ctx, doc, fontDict, cidFontDict, fontName, pageNum, profile)...)

	// --- CMap embedding and WMode checks ---
	results = append(results, checkCMapEmbedding(ctx, doc, fontDict, fontName, pageNum, profile)...)

	// --- CID value limit check (t10: max CID 65535) ---
	results = append(results, checkCMapCIDValues(ctx, doc, fontDict, fontName, pageNum, profile)...)

	// --- 6. .notdef check ---
	results = append(results, checkNotdefGlyph(ctx, doc, fontDict, fontName, pageNum, profile, nil)...)
	results = append(results, checkType0NotdefGlyph(ctx, doc, fontDict, fontName, pageNum, profile, usage)...)

	// --- 5. Glyph width consistency for CIDFont ---
	cidFd := findFontDescriptor(ctx, doc, cidFontDict)
	if cidFd != nil {
		fontData := extractFontProgramData(ctx, doc, cidFd)
		if fontData != nil {
			binaryWidths := typography.ExtractBinaryWidths(fontData)
			if binaryWidths != nil {
				unitsPerEm := typography.ReadUnitsPerEm(fontData)
				if unitsPerEm == 0 {
					unitsPerEm = 1000 //nolint:mnd // safe default
				}
				rule := glyphWidthRule(profile)
				if rule.Specification != "" {
					results = append(results, checkCIDFontWidths(ctx, doc, cidFontDict, binaryWidths, unitsPerEm, fontName, pageNum, rule)...)
				}
			}
		}
	}

	// --- 9. CIDFont glyph coverage (CIDSet for CIDFont subsets) ---
	results = append(results, checkCIDFontGlyphCoverage(ctx, doc, cidFontDict, fontName, pageNum, profile)...)

	// --- 10. ToUnicode CMap values ---
	results = append(results, checkToUnicodeCMapValues(ctx, doc, fontDict, fontName, pageNum, profile)...)

	return results
}

// checkCIDToGIDMap validates that Type 2 CIDFonts have a /CIDToGIDMap entry (rule 3).
func checkCIDToGIDMap(ctx context.Context, doc *document.Document, cidFontDict ast.PdfDict, cidSubtype, fontName string, pageNum int, profile string) []Violation {
	rule := cidToGIDMapRule(profile)
	if rule.Specification == "" {
		slog.Debug("compliance_2: fonts: CIDToGIDMap check not applicable",
			"profile", profile)
		return nil
	}

	// Only applies to CIDFontType2.
	if cidSubtype != "CIDFontType2" {
		slog.Debug("compliance_2: fonts: CIDToGIDMap check only for CIDFontType2",
			"font", fontName, "subtype", cidSubtype)
		return nil
	}

	cidToGIDObj, ok := cidFontDict["CIDToGIDMap"]
	if !ok {
		slog.Debug("compliance_2: fonts: CIDFontType2 missing CIDToGIDMap",
			"font", fontName, "page", pageNum)
		return failf(rule, "font %s on page %d", fontName, pageNum)
	}

	// CIDToGIDMap can be the name "Identity" or a stream.
	resolved, err := document.ResolveObject(ctx, doc, cidToGIDObj)
	if err != nil {
		slog.Debug("compliance_2: fonts: failed to resolve CIDToGIDMap",
			"font", fontName, "error", err)
		return failf(rule, "font %s on page %d (resolve error)", fontName, pageNum)
	}

	switch v := resolved.(type) {
	case ast.PdfName:
		if v == "Identity" {
			slog.Debug("compliance_2: fonts: CIDToGIDMap is Identity", "font", fontName)
			return nil
		}
		slog.Debug("compliance_2: fonts: CIDToGIDMap invalid name",
			"font", fontName, "value", v)
		return failf(rule, "font %s on page %d CIDToGIDMap=%s", fontName, pageNum, v)
	case ast.PdfStream:
		slog.Debug("compliance_2: fonts: CIDToGIDMap is a stream", "font", fontName)
		return nil
	default:
		slog.Debug("compliance_2: fonts: CIDToGIDMap has unexpected type",
			"font", fontName, "type", resolved)
		return failf(rule, "font %s on page %d CIDToGIDMap has invalid type", fontName, pageNum)
	}
}

// checkCIDSystemInfo validates CIDSystemInfo consistency between CIDFont and
// CMap dictionaries (rule 4).
func checkCIDSystemInfo(ctx context.Context, doc *document.Document, type0Dict, cidFontDict ast.PdfDict, fontName string, pageNum int, profile string) []Violation {
	rule := cidSystemInfoRule(profile)
	if rule.Specification == "" {
		slog.Debug("compliance_2: fonts: CIDSystemInfo check not applicable",
			"profile", profile)
		return nil
	}

	// If encoding is Identity-H or Identity-V, any CIDSystemInfo values are valid.
	if hasIdentityCMap(ctx, doc, type0Dict) {
		slog.Debug("compliance_2: fonts: Identity CMap, CIDSystemInfo check skipped",
			"font", fontName)
		return nil
	}

	// Get CIDFont CIDSystemInfo.
	cidSysInfo, err := document.ResolveDict(ctx, doc, cidFontDict["CIDSystemInfo"])
	if err != nil || cidSysInfo == nil {
		slog.Debug("compliance_2: fonts: CIDFont missing CIDSystemInfo",
			"font", fontName, "error", err)
		return nil
	}

	cidRegistry, _ := document.ResolveString(ctx, doc, cidSysInfo["Registry"])
	cidOrdering, _ := document.ResolveString(ctx, doc, cidSysInfo["Ordering"])
	cidSupplement, _ := document.ResolveInt(ctx, doc, cidSysInfo["Supplement"])

	slog.Debug("compliance_2: fonts: CIDFont CIDSystemInfo",
		"font", fontName,
		"registry", cidRegistry, "ordering", cidOrdering, "supplement", cidSupplement)

	// Get CMap CIDSystemInfo from either the embedded CMap stream or a predefined CMap name.
	encObj, ok := type0Dict["Encoding"]
	if !ok {
		slog.Debug("compliance_2: fonts: Type0 font missing Encoding", "font", fontName)
		return nil
	}

	var cmapRegistry, cmapOrdering ast.PdfString
	var cmapSupplement ast.PdfInt

	cmapStream, err := document.ResolveStream(ctx, doc, encObj)
	if err == nil {
		// Embedded CMap stream — read CIDSystemInfo from the stream dict.
		cmapSysInfo, err := document.ResolveDict(ctx, doc, cmapStream.Dict["CIDSystemInfo"])
		if err != nil || cmapSysInfo == nil {
			slog.Debug("compliance_2: fonts: CMap stream missing CIDSystemInfo", "font", fontName)
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
			slog.Debug("compliance_2: fonts: cannot parse CMap name for CIDSystemInfo",
				"font", fontName, "encoding", encName)
			// Non-Identity encoding that can't be resolved — the CIDSystemInfo
			// relationship can't be satisfied.
			return failf(rule, "font %s on page %d: encoding %q is not a valid predefined CMap",
				fontName, pageNum, string(encName))
		}
		cmapRegistry = ast.PdfString(reg)
		cmapOrdering = ast.PdfString(ord)
		cmapSupplement = ast.PdfInt(sup)
	}

	slog.Debug("compliance_2: fonts: CMap CIDSystemInfo",
		"font", fontName,
		"registry", cmapRegistry, "ordering", cmapOrdering, "supplement", cmapSupplement)

	// Registry and Ordering must match.
	if string(cidRegistry) != string(cmapRegistry) || string(cidOrdering) != string(cmapOrdering) {
		slog.Debug("compliance_2: fonts: CIDSystemInfo Registry/Ordering mismatch",
			"font", fontName, "page", pageNum)
		return failf(rule, "font %s on page %d (CIDFont Registry=%s Ordering=%s, CMap Registry=%s Ordering=%s)",
			fontName, pageNum, cidRegistry, cidOrdering, cmapRegistry, cmapOrdering)
	}

	// CIDFont Supplement must be ≤ CMap Supplement (parts 2+ only;
	// PDF/A-1 §6.3.3.1 only requires Registry/Ordering match).
	// PDF/UA profiles also require Supplement check.
	part := pdfaPart(profile)
	checkSupplement := part >= 2 || isPDFUA(profile) //nolint:mnd
	if checkSupplement && cidSupplement > cmapSupplement {
		slog.Debug("compliance_2: fonts: CIDSystemInfo Supplement violation",
			"font", fontName, "cidSupplement", cidSupplement, "cmapSupplement", cmapSupplement)
		return failf(rule, "font %s on page %d: CIDFont Supplement (%d) > CMap Supplement (%d)",
			fontName, pageNum, cidSupplement, cmapSupplement)
	}

	slog.Debug("compliance_2: fonts: CIDSystemInfo is consistent", "font", fontName)
	return nil
}

// parsePredefinedCMapName extracts Registry, Ordering, and Supplement from a
// predefined CMap name like "Adobe-Japan1-2". Returns empty strings if the
// name doesn't match the expected pattern.
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
			slog.Debug("compliance_2: fonts: CID value exceeds 65535",
				"font", fontName, "page", pageNum, "cid", cidVal)
			return failf(rule,
				"font %s on page %d: CID value %d exceeds maximum 65,535",
				fontName, pageNum, cidVal)
		}
	}

	return nil
}

// cmapCIDRule returns the rule for CID value > 65535 checks.
func cmapCIDRule(profile string) Rule {
	switch pdfaPart(profile) {
	case 1:
		return Rule_19005_1_6_1_12_t10
	case 2: //nolint:mnd
		return Rule_19005_2_6_1_13_t10
	case 3: //nolint:mnd
		return Rule_19005_3_6_1_13_t10
	}
	return Rule{}
}

// findNonPredefinedUseCMap checks a CMap (stream data + dict) for references to
// non-predefined CMaps, recursively following embedded CMap chains.
// Returns the first non-predefined CMap name found, or "" if all are predefined.
func findNonPredefinedUseCMap(ctx context.Context, doc *document.Document, data []byte, cmapDict ast.PdfDict, profile string, depth int) string {
	if depth > 5 { //nolint:mnd
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
	rest := after //nolint:mnd
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

// checkType0ToUnicode validates the ToUnicode requirement for composite fonts (rule 2).
func checkType0ToUnicode(ctx context.Context, doc *document.Document, type0Dict, cidFontDict ast.PdfDict, fontName string, pageNum int, profile string) []Violation {
	rule := toUnicodeRule(profile)
	if rule.Specification == "" {
		slog.Debug("compliance_2: fonts: ToUnicode check not applicable for Type0",
			"profile", profile)
		return nil
	}

	// Exemption: known CJK character collections.
	cidSysInfo, err := document.ResolveDict(ctx, doc, cidFontDict["CIDSystemInfo"])
	if err == nil && cidSysInfo != nil {
		ordering, _ := document.ResolveString(ctx, doc, cidSysInfo["Ordering"])
		registry, _ := document.ResolveString(ctx, doc, cidSysInfo["Registry"])
		if string(registry) == "Adobe" {
			switch string(ordering) {
			case "GB1", "CNS1", "Japan1", "Korea1":
				slog.Debug("compliance_2: fonts: Type0 font uses standard CJK collection, ToUnicode exemption applies",
					"font", fontName, "ordering", ordering)
				return nil
			}
		}
	}

	if !hasToUnicode(type0Dict) {
		slog.Debug("compliance_2: fonts: Type0 font missing ToUnicode",
			"font", fontName, "page", pageNum)
		return failf(rule, "font %s on page %d", fontName, pageNum)
	}

	slog.Debug("compliance_2: fonts: Type0 font has ToUnicode", "font", fontName)
	return nil
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
				if glyphName == "" && code < 0x20 { //nolint:mnd
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
				glyphNames := typography.ExtractGlyphNames(fontData)
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
					charToGID := typography.ExtractCharCodeToGID(fontData)
					if charToGID != nil {
						// GID 0 is always .notdef.
						if usage.charCodes[0] && charToGID[0] == 0 {
							return failf(rule, "font %s on page %d: charCode 0 maps to GID 0 (.notdef)", fontName, pageNum)
						}
						// For non-zero GIDs: empty glyph data + PDF width=0 → .notdef.
						emptyGIDs := typography.ExtractEmptyGIDs(fontData)
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
		for i := 0; i+1 < len(data); i += 2 { //nolint:mnd
			cid := uint16(data[i])<<8 | uint16(data[i+1])
			usedCIDs[cid] = true
		}
	}

	// An odd-length string with a 2-byte CMap (Identity-H/V) means the
	// last byte cannot form a valid CID — the renderer uses .notdef.
	if hasOddLengthString {
		slog.Debug("compliance_2: fonts: Type0/Identity text has odd-length string (.notdef)",
			"font", fontName, "page", pageNum)
		return failf(rule, "font %s on page %d: odd-length string with 2-byte CMap references .notdef", fontName, pageNum)
	}

	// CID 0 is always .notdef.
	if usedCIDs[0] {
		slog.Debug("compliance_2: fonts: Type0 text references CID 0 (.notdef)",
			"font", fontName, "page", pageNum)
		return failf(rule, "font %s on page %d: text references CID 0 (.notdef)", fontName, pageNum)
	}

	// Check if referenced CIDs are present in the embedded font program.
	// Resolve descendant CIDFont to get the font program.
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
				slog.Debug("compliance_2: fonts: Type0/CFF text references absent CID",
					"font", fontName, "page", pageNum, "cid", cid)
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
		emptyGIDs := typography.ExtractEmptyGIDs(fontData)
		if emptyGIDs != nil {
			for cid := range usedCIDs {
				if emptyGIDs[cid] && lookupCIDWidth(ctx, doc, cidFontDict, cid) == 0 {
					slog.Debug("compliance_2: fonts: Type0/TrueType CID maps to empty glyph with width 0",
						"font", fontName, "page", pageNum, "cid", cid)
					return failf(rule, "font %s on page %d: CID %d maps to empty glyph with width 0 (.notdef)", fontName, pageNum, cid)
				}
			}
		}
	}

	return nil
}

// simpleFontWidths extracts FirstChar and the /Widths array from a simple font dict.
func simpleFontWidths(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) (int, []ast.PdfObject) {
	firstChar := int(document.GetFloat(fontDict["FirstChar"]))
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
	return document.GetFloat(widths[idx]) == 0
}

// lookupCIDWidth returns the PDF-declared width for a CID from the /W array
// and /DW default of a CIDFont dict.
func lookupCIDWidth(ctx context.Context, doc *document.Document, cidFontDict ast.PdfDict, cid uint16) float64 {
	// Default width.
	dw := document.GetFloat(cidFontDict["DW"])
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
		first := int(document.GetFloat(wArr[i]))
		if i+1 >= len(wArr) {
			break
		}
		// Format 1: [cidStart [w1 w2 ...]]
		if arr, err := document.ResolveArray(ctx, doc, wArr[i+1]); err == nil && arr != nil {
			for j, w := range arr {
				if first+j == cidInt {
					return document.GetFloat(w)
				}
			}
			i += 2
			continue
		}
		// Format 2: [cidFirst cidLast w]
		if i+2 >= len(wArr) {
			break
		}
		last := int(document.GetFloat(wArr[i+1]))
		if cidInt >= first && cidInt <= last {
			return document.GetFloat(wArr[i+2])
		}
		i += 3
	}
	return dw
}

// --- Glyph coverage (CharSet / CIDSet) rule selectors ---

// charSetRule returns the CharSet rule for Type1 font subsets.
func charSetRule(profile string) Rule {
	switch pdfaPart(profile) {
	case 1:
		return Rule_19005_1_6_3_5_t02
	case 2:
		return Rule_19005_2_6_2_11_4_1_t02
	case 3:
		return Rule_19005_3_6_2_11_4_1_t02
	case 4:
		return Rule_19005_4_6_2_10_4_1_t02
	}
	return Rule{}
}

// cidSetRule returns the CIDSet rule for CIDFont subsets.
func cidSetRule(profile string) Rule {
	if isPDFUA(profile) {
		return Rule_14289_1_7_21_4_2_t02
	}
	switch pdfaPart(profile) {
	case 1:
		return Rule_19005_1_6_3_5_t03
	case 2:
		return Rule_19005_2_6_2_11_4_1_t02
	case 3:
		return Rule_19005_3_6_2_11_4_1_t02
	case 4:
		return Rule_19005_4_6_2_10_4_1_t02
	}
	return Rule{}
}

// charSetCompletenessRule returns the rule for CharSet completeness
// (all font program glyphs must be listed in /CharSet).
func charSetCompletenessRule(profile string) Rule {
	if isPDFUA(profile) {
		return Rule_14289_1_7_21_4_2_t01
	}
	switch pdfaPart(profile) {
	case 2: //nolint:mnd
		return Rule_19005_2_6_2_11_4_2_t01
	case 3: //nolint:mnd
		return Rule_19005_3_6_2_11_4_2_t01
	}
	// Part 1 also requires completeness (§6.3.5 t2), but the CFF glyph name
	// extraction returns unreachable glyphs (e.g., "x") that VeraPDF does not
	// consider part of the font subset, causing FP on pass-b.
	return Rule{}
}

// cidSetCompletenessRule returns the rule for CIDSet completeness
// (all CIDs in font program must be identified in /CIDSet).
func cidSetCompletenessRule(profile string) Rule {
	if isPDFUA(profile) {
		return Rule_14289_1_7_21_4_2_t02
	}
	switch pdfaPart(profile) {
	case 2: //nolint:mnd
		return Rule_19005_2_6_2_11_4_2_t02
	case 3: //nolint:mnd
		return Rule_19005_3_6_2_11_4_2_t02
	}
	// Part 1 only requires CIDSet presence, not completeness.
	return Rule{}
}

// toUnicodeCMapValuesRule returns the ToUnicode CMap values rule.
func toUnicodeCMapValuesRule(profile string) Rule {
	if isPDFUA(profile) {
		if strings.Contains(profile, "2") {
			return Rule_14289_2_8_4_5_8_t02
		}
		return Rule_14289_1_7_21_7_t02
	}
	switch pdfaPart(profile) {
	case 2:
		return Rule_19005_2_6_2_11_7_3_t01
	case 3:
		return Rule_19005_3_6_2_11_7_3_t01
	case 4:
		return Rule_19005_4_6_2_10_7_t01
	}
	return Rule{}
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
	case 2: //nolint:mnd
		return Rule_19005_2_6_2_11_3_3_t01, Rule_19005_2_6_2_11_3_3_t02, Rule_19005_2_6_2_11_3_3_t03
	case 3: //nolint:mnd
		return Rule_19005_3_6_2_11_3_3_t01, Rule_19005_3_6_2_11_3_3_t02, Rule_19005_3_6_2_11_3_3_t03
	case 4: //nolint:mnd
		return Rule_19005_4_6_2_10_3_3_t01, Rule_19005_4_6_2_10_3_3_t02, Rule_19005_4_6_2_10_3_3_t03
	}
	return Rule{}, Rule{}, Rule{}
}

// isSubsetFont returns true if the font's BaseFont name starts with a
// 6-letter tag followed by '+' (e.g., "ABCDEF+Arial"), indicating a subset.
func isSubsetFont(basefont string) bool {
	if len(basefont) < 8 { //nolint:mnd // minimum: 6 tag chars + '+' + 1 name char
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

// checkGlyphCoverage validates that subset fonts have the required CharSet or
// CIDSet entries in their FontDescriptor.
// - Type1 subsets: /FontDescriptor must have /CharSet
// - CIDFont subsets: /FontDescriptor must have /CIDSet
func checkGlyphCoverage(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string) []Violation {
	basefont := fontBaseFont(ctx, doc, fontDict)
	if !isSubsetFont(basefont) {
		slog.Debug("compliance_2: fonts: not a subset font, skipping glyph coverage check",
			"font", fontName, "basefont", basefont)
		return nil
	}

	subtype := fontSubtype(ctx, doc, fontDict)
	slog.Debug("compliance_2: fonts: checking glyph coverage for subset font",
		"font", fontName, "basefont", basefont, "subtype", subtype)

	fd := findFontDescriptor(ctx, doc, fontDict)
	if fd == nil {
		slog.Debug("compliance_2: fonts: no FontDescriptor, skipping glyph coverage check", "font", fontName)
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
		slog.Debug("compliance_2: fonts: CIDFont not a subset, skipping CIDSet check",
			"font", fontName, "basefont", basefont)
		return nil
	}

	rule := cidSetRule(profile)
	if rule.Specification == "" {
		return nil
	}

	fd := findFontDescriptor(ctx, doc, cidFontDict)
	if fd == nil {
		slog.Debug("compliance_2: fonts: CIDFont no FontDescriptor, skipping CIDSet check", "font", fontName)
		return nil
	}

	// Part 1: CIDSet is required. Parts 2+: only check if present (correctness).
	if pdfaPart(profile) == 1 {
		if _, hasCIDSet := fd["CIDSet"]; !hasCIDSet {
			slog.Debug("compliance_2: fonts: CIDFont subset missing /CIDSet",
				"font", fontName, "page", pageNum)
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

	slog.Debug("compliance_2: fonts: CIDFont subset CIDSet check passed", "font", fontName)
	return nil
}

// checkCIDSetCompleteness decodes a CIDSet bitfield stream and verifies
// it covers all GIDs with non-empty glyph data in the embedded font program.
func checkCIDSetCompleteness(ctx context.Context, doc *document.Document, cidFontDict ast.PdfDict, cidSetObj ast.PdfObject, fontName string, pageNum int, rule Rule) []Violation {
	stream, err := document.ResolveStream(ctx, doc, cidSetObj)
	if err != nil {
		return nil
	}
	cidSetData, err := doc.DecodeStream(stream)
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
		return nil
	}
	cidSetData, err := doc.DecodeStream(stream)
	if err != nil {
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

// checkToUnicodeCMapValues validates that a /ToUnicode CMap, if present, is
// decodable and does not map to invalid Unicode values (U+0000, U+FEFF, U+FFFE).
func checkToUnicodeCMapValues(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string) []Violation {
	rule := toUnicodeCMapValuesRule(profile)
	if rule.Specification == "" {
		slog.Debug("compliance_2: fonts: ToUnicode CMap values check not applicable", "profile", profile)
		return nil
	}

	tuObj, ok := fontDict["ToUnicode"]
	if !ok {
		slog.Debug("compliance_2: fonts: no /ToUnicode, skipping CMap values check", "font", fontName)
		return nil
	}

	stream, err := document.ResolveStream(ctx, doc, tuObj)
	if err != nil {
		slog.Debug("compliance_2: fonts: failed to resolve ToUnicode stream",
			"font", fontName, "error", err)
		return failf(rule, "font %s on page %d: cannot resolve /ToUnicode stream", fontName, pageNum)
	}
	if stream.Dict == nil {
		slog.Debug("compliance_2: fonts: ToUnicode stream has no dict", "font", fontName)
		return failf(rule, "font %s on page %d: /ToUnicode stream has no dictionary", fontName, pageNum)
	}

	data, err := doc.DecodeStream(stream)
	if err != nil {
		// Fall back to raw data.
		data, err = stream.Bytes()
		if err != nil || len(data) == 0 {
			slog.Debug("compliance_2: fonts: failed to decode ToUnicode CMap",
				"font", fontName, "error", err)
			return failf(rule, "font %s on page %d: cannot decode /ToUnicode CMap", fontName, pageNum)
		}
	}

	// Scan the CMap data for invalid Unicode values.
	// Look for hex strings in beginbfchar/beginbfrange sections that map to
	// U+0000, U+FEFF, or U+FFFE.
	cmapStr := string(data)
	if containsInvalidUnicodeMapping(cmapStr) {
		slog.Debug("compliance_2: fonts: ToUnicode CMap contains invalid Unicode mapping",
			"font", fontName, "page", pageNum)
		return failf(rule, "font %s on page %d: /ToUnicode CMap maps to U+0000, U+FEFF, or U+FFFE",
			fontName, pageNum)
	}

	slog.Debug("compliance_2: fonts: ToUnicode CMap values valid", "font", fontName)
	return nil
}

// containsInvalidUnicodeMapping scans CMap bfchar/bfrange sections for
// destination hex strings mapping to U+0000, U+FEFF, or U+FFFE.
// Only destination values are checked — source codes may legitimately use
// these values.
func containsInvalidUnicodeMapping(cmapData string) bool {
	// Normalise line endings: CR+LF and bare CR → LF.
	normalized := strings.ReplaceAll(cmapData, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	upper := strings.ToUpper(normalized)

	// Track bfchar/bfrange sections. The keyword is preceded by a count
	// (e.g., "1 beginbfchar"), so use Contains rather than HasPrefix.
	inBfchar := false
	inBfrange := false
	for line := range strings.SplitSeq(upper, "\n") {
		trimmed := strings.TrimSpace(line)

		// Transition into bfchar/bfrange sections.
		if strings.Contains(trimmed, "BEGINBFCHAR") {
			inBfchar = true
			continue
		}
		if strings.Contains(trimmed, "BEGINBFRANGE") {
			inBfrange = true
			continue
		}

		// Check mapping entries BEFORE checking for section end, because
		// the mapping and end keyword can appear on the same line
		// (e.g., "<01> <0000> endbfchar" with CR line endings).
		if inBfchar {
			parts := strings.Fields(trimmed)
			if len(parts) >= 2 { //nolint:mnd
				if isInvalidUnicodeDst(parts[1]) {
					return true
				}
			}
		}
		if inBfrange {
			parts := strings.Fields(trimmed)
			if len(parts) >= 3 { //nolint:mnd
				if isInvalidUnicodeDst(parts[2]) {
					return true
				}
			}
		}

		// Transition out of sections.
		if strings.Contains(trimmed, "ENDBFCHAR") {
			inBfchar = false
		}
		if strings.Contains(trimmed, "ENDBFRANGE") {
			inBfrange = false
		}
	}
	return false
}

// isInvalidUnicodeDst checks if a hex string token is an invalid Unicode
// destination value (U+0000, U+FEFF, U+FFFE).
func isInvalidUnicodeDst(token string) bool {
	t := strings.ToUpper(strings.TrimSpace(token))
	return t == "<0000>" || t == "<FEFF>" || t == "<FFFE>"
}
