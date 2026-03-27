package compliance

import (
	"context"
	"log/slog"
	"math"
	"strings"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/internal/typography"
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
	return baseEnc == "MacRomanEncoding" || baseEnc == "WinAnsiEncoding" || baseEnc == "MacExpertEncoding"
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

// collectVisibleFontNames parses the page content stream and returns the set
// of font resource names that are used for visible rendering (text rendering
// mode != 3). Returns nil if the content stream can't be parsed (in which case
// all fonts should be checked).
func collectVisibleFontNames(ctx context.Context, doc *document.Document, page ast.PdfDict) map[string]bool {
	streamData, err := getPageContentBytes(ctx, doc, page)
	if err != nil || len(streamData) == 0 {
		return nil
	}

	ops, err := content.ParseStreamData(streamData)
	if err != nil {
		return nil
	}

	visible := make(map[string]bool)
	currentFont := ""
	renderMode := 0 // default rendering mode is 0 (fill)

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
		case "Tj", "TJ", "'", "\"":
			// Text showing operator — mark current font as visible if mode != 3
			if currentFont != "" && renderMode != 3 { //nolint:mnd // rendering mode 3 = invisible
				visible[currentFont] = true
			}
		}
	}

	return visible
}

// --- Main check group ---

// checkFontGroup validates font-related compliance rules across all pages.
func checkFontGroup(ctx context.Context, doc *document.Document, catalog ast.PdfDict, profile string) []Result {
	slog.Debug("compliance_2: fonts: starting font checks", "profile", profile)

	pages := collectPages(ctx, doc, catalog)
	if len(pages) == 0 {
		slog.Debug("compliance_2: fonts: no pages found")
		return nil
	}

	var results []Result

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

		// Collect which fonts are used for visible rendering by Tf operators
		// in the page content stream. Fonts not referenced by Tf, or fonts
		// used exclusively with rendering mode 3 (invisible), are exempt
		// from width consistency and embedding checks per the spec.
		visibleFonts := collectVisibleFontNames(ctx, doc, page)

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
			isVisible := visibleFonts == nil || visibleFonts[fontKey]
			slog.Debug("compliance_2: fonts: checking font",
				"font", fontKey, "page", pageIdx+1, "visible", isVisible)

			results = append(results, checkSingleFont(ctx, doc, font, fontKey, pageIdx+1, profile, isVisible)...)
		}
	}

	slog.Debug("compliance_2: fonts: font checks complete",
		"violations", len(results), "fonts_checked", len(checked))
	return results
}

// checkSingleFont runs all font checks against a single font dictionary.
// If isVisible is false, width consistency and embedding checks are skipped
// (the font is not used for visible rendering).
func checkSingleFont(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, isVisible bool) []Result {
	var results []Result

	subtype := fontSubtype(ctx, doc, fontDict)
	basefont := fontBaseFont(ctx, doc, fontDict)

	slog.Debug("compliance_2: fonts: font details",
		"name", fontName, "subtype", subtype, "basefont", basefont, "page", pageNum)

	// Type 3 fonts have no font program — skip embedding checks.
	isType3 := isType3Font(ctx, doc, fontDict)

	// Type 0 (composite) fonts: check descendant CIDFonts.
	if subtype == "Type0" {
		results = append(results, checkType0Font(ctx, doc, fontDict, fontName, pageNum, profile)...)
		return results
	}

	// --- 1. Font embedding check ---
	// Only for fonts used for visible rendering.
	if isVisible {
		results = append(results, checkFontEmbedding(ctx, doc, fontDict, fontName, pageNum, profile, isType3)...)
	}

	// --- 2. ToUnicode mapping check ---
	results = append(results, checkToUnicode(ctx, doc, fontDict, fontName, pageNum, profile)...)

	// --- 7. TrueType encoding check ---
	if subtype == "TrueType" {
		results = append(results, checkTrueTypeEncoding(ctx, doc, fontDict, fontName, pageNum, profile)...)
	}

	// --- 8. Font name UTF-8 check ---
	results = append(results, checkFontNameUTF8(ctx, doc, fontDict, fontName, pageNum, profile)...)

	// --- 5. Glyph width consistency ---
	// Only for fonts used for visible rendering.
	if isVisible {
		results = append(results, checkGlyphWidths(ctx, doc, fontDict, fontName, pageNum, profile)...)
	}

	// --- 6. .notdef glyph check ---
	results = append(results, checkNotdefGlyph(ctx, doc, fontDict, fontName, pageNum, profile)...)

	// --- 9. Glyph coverage (CharSet for Type1 subsets) ---
	results = append(results, checkGlyphCoverage(ctx, doc, fontDict, fontName, pageNum, profile)...)

	// --- 10. ToUnicode CMap values ---
	results = append(results, checkToUnicodeCMapValues(ctx, doc, fontDict, fontName, pageNum, profile)...)

	return results
}

// --- Individual check implementations ---

// checkFontEmbedding validates that a font program is embedded (rule 1).
func checkFontEmbedding(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, isType3 bool) []Result {
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
func checkToUnicode(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string) []Result {
	rule := toUnicodeRule(profile)
	if rule.Specification == "" {
		slog.Debug("compliance_2: fonts: ToUnicode check not applicable", "profile", profile)
		return nil
	}

	// Skip Type3 fonts — they have their own glyph definitions.
	if isType3Font(ctx, doc, fontDict) {
		slog.Debug("compliance_2: fonts: skipping ToUnicode check for Type3", "font", fontName)
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
		// *+Symbol) is exempt because its character names are defined in
		// PDF Reference Appendix D with standard Unicode mappings.
		if isSymbolFontByName(ctx, doc, fontDict) && allGlyphNamesInSymbolEncoding(ctx, doc, fontDict) {
			slog.Debug("compliance_2: fonts: Symbol font with all Symbol-encoding glyph names, ToUnicode not required",
				"font", fontName)
			return nil
		}
	}

	// Exemption (c): Identity-H/Identity-V with CIDToGIDMap.
	if hasIdentityCMap(ctx, doc, fontDict) {
		slog.Debug("compliance_2: fonts: font uses Identity CMap, ToUnicode exemption may apply",
			"font", fontName)
		// Still need ToUnicode for non-CID simple fonts using Identity encoding;
		// for Type0 fonts this is checked in checkType0Font.
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
func checkTrueTypeEncoding(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string) []Result {
	var results []Result

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

						// Part 1 additionally forbids Differences in the encoding dict.
						if pdfaPart(profile) == 1 {
							if _, hasDiff := encDict["Differences"]; hasDiff {
								slog.Debug("compliance_2: fonts: non-symbolic TrueType has Differences in encoding dict (part 1)",
									"font", fontName, "page", pageNum)
								results = append(results, failf(rule, "font %s on page %d has Differences in encoding dict", fontName, pageNum)...)
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
func checkFontNameUTF8(_ context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string) []Result {
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
func checkGlyphWidths(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string) []Result {
	rule := glyphWidthRule(profile)
	if rule.Specification == "" {
		slog.Debug("compliance_2: fonts: glyph width check not applicable", "profile", profile)
		return nil
	}

	// Type3 fonts have their own width mechanism.
	if isType3Font(ctx, doc, fontDict) {
		slog.Debug("compliance_2: fonts: skipping glyph width check for Type3", "font", fontName)
		return nil
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

	subtype := fontSubtype(ctx, doc, fontDict)

	// Simple fonts (Type1, TrueType, MMType1): compare /Widths array using FirstChar offset.
	if subtype == "Type1" || subtype == "TrueType" || subtype == "MMType1" {
		return checkSimpleFontWidths(ctx, doc, fontDict, binaryWidths, unitsPerEm, fontName, pageNum, rule, charCodeToGID)
	}

	return nil
}

// checkSimpleFontWidths compares /Widths array against binary widths for simple fonts.
// charCodeToGID is the cmap-derived charCode → GID mapping (may be nil for non-TrueType).
func checkSimpleFontWidths(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, binaryWidths map[uint16]uint16, unitsPerEm uint16, fontName string, pageNum int, rule Rule, charCodeToGID map[uint16]uint16) []Result {
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
		// Skip non-outline glyphs: .notdef (GID 0) and control/whitespace
		// characters (charCode ≤ 0x20). PDF producers legitimately override
		// widths for these glyphs, and the spec only requires consistency
		// for glyphs with visible outlines.
		if charCode <= 0x20 {
			continue
		}
		// Map charCode to GID using the font's cmap table. If no cmap
		// is available (e.g., Type1 fonts), fall back to identity mapping.
		gid := uint16(charCode) //nolint:gosec
		if charCodeToGID != nil {
			mappedGID, ok := charCodeToGID[uint16(charCode)] //nolint:gosec
			if !ok || mappedGID == 0 {
				// The cmap doesn't map this charCode (or maps to .notdef).
				// The font likely uses a PDF-level encoding (Differences,
				// built-in Type1 encoding) that maps this charCode to a
				// glyph not reachable via the Unicode cmap. Skip the width
				// comparison rather than comparing against the wrong glyph.
				continue
			}
			gid = mappedGID
		}
		pdfWidth := document.GetFloat(wObj)
		if pdfWidth == 0 {
			continue
		}
		bw, ok := binaryWidths[gid]
		if !ok {
			continue
		}
		expectedWidth := float64(bw) * 1000.0 / float64(unitsPerEm)
		if math.Abs(pdfWidth-expectedWidth) > widthTolerance {
			slog.Debug("compliance_2: fonts: glyph width mismatch",
				"font", fontName, "charCode", charCode,
				"pdfWidth", pdfWidth, "expectedWidth", expectedWidth)
			return failf(rule, "font %s on page %d: char %d width mismatch (PDF=%.1f, font=%.1f)",
				fontName, pageNum, charCode, pdfWidth, expectedWidth)
		}
	}

	slog.Debug("compliance_2: fonts: glyph widths consistent", "font", fontName, "page", pageNum)
	return nil
}

// checkCIDFontWidths compares /W array against binary widths for CIDFonts.
func checkCIDFontWidths(ctx context.Context, doc *document.Document, cidFontDict ast.PdfDict, binaryWidths map[uint16]uint16, unitsPerEm uint16, fontName string, pageNum int, rule Rule) []Result {
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
func checkType0Font(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string) []Result {
	results := make([]Result, 0, 4) //nolint:mnd // estimated capacity for composite font checks.

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

	// --- 6. .notdef check ---
	results = append(results, checkNotdefGlyph(ctx, doc, fontDict, fontName, pageNum, profile)...)

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
func checkCIDToGIDMap(ctx context.Context, doc *document.Document, cidFontDict ast.PdfDict, cidSubtype, fontName string, pageNum int, profile string) []Result {
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
func checkCIDSystemInfo(ctx context.Context, doc *document.Document, type0Dict, cidFontDict ast.PdfDict, fontName string, pageNum int, profile string) []Result {
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

	// Get CMap CIDSystemInfo — the CMap may be a stream with its own CIDSystemInfo.
	encObj, ok := type0Dict["Encoding"]
	if !ok {
		slog.Debug("compliance_2: fonts: Type0 font missing Encoding", "font", fontName)
		return nil
	}

	cmapStream, err := document.ResolveStream(ctx, doc, encObj)
	if err != nil {
		slog.Debug("compliance_2: fonts: Encoding is not a stream (may be a name), skipping CIDSystemInfo check",
			"font", fontName)
		return nil
	}

	cmapSysInfo, err := document.ResolveDict(ctx, doc, cmapStream.Dict["CIDSystemInfo"])
	if err != nil || cmapSysInfo == nil {
		slog.Debug("compliance_2: fonts: CMap stream missing CIDSystemInfo",
			"font", fontName)
		return nil
	}

	cmapRegistry, _ := document.ResolveString(ctx, doc, cmapSysInfo["Registry"])
	cmapOrdering, _ := document.ResolveString(ctx, doc, cmapSysInfo["Ordering"])
	cmapSupplement, _ := document.ResolveInt(ctx, doc, cmapSysInfo["Supplement"])

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

	// Note: Supplement comparison is informational per ISO 32000. The CIDFont
	// Supplement does not need to match the CMap Supplement. A CIDFont with a
	// lower Supplement than its CMap simply means some CIDs may not be mapped,
	// which is valid. Only Registry and Ordering are required to be consistent.
	slog.Debug("compliance_2: fonts: CIDSystemInfo Supplement values (informational)",
		"font", fontName, "cidSupplement", cidSupplement, "cmapSupplement", cmapSupplement)

	slog.Debug("compliance_2: fonts: CIDSystemInfo is consistent", "font", fontName)
	return nil
}

// checkType0ToUnicode validates the ToUnicode requirement for composite fonts (rule 2).
func checkType0ToUnicode(ctx context.Context, doc *document.Document, type0Dict, cidFontDict ast.PdfDict, fontName string, pageNum int, profile string) []Result {
	rule := toUnicodeRule(profile)
	if rule.Specification == "" {
		slog.Debug("compliance_2: fonts: ToUnicode check not applicable for Type0",
			"profile", profile)
		return nil
	}

	// Exemption: Identity CMap with CIDToGIDMap.
	if hasIdentityCMap(ctx, doc, type0Dict) {
		if _, hasCIDToGID := cidFontDict["CIDToGIDMap"]; hasCIDToGID {
			slog.Debug("compliance_2: fonts: Type0 uses Identity CMap with CIDToGIDMap, ToUnicode not required",
				"font", fontName)
			return nil
		}
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

// checkNotdefGlyph checks for .notdef glyph references in font Encoding
// /Differences arrays (rule 6). A .notdef entry indicates a character code
// maps to the undefined glyph.
func checkNotdefGlyph(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string) []Result {
	rule := notdefRule(profile)
	if rule.Specification == "" {
		slog.Debug("compliance_2: fonts: .notdef check not applicable", "profile", profile)
		return nil
	}

	encObj, ok := fontDict["Encoding"]
	if !ok {
		slog.Debug("compliance_2: fonts: no /Encoding entry, skipping .notdef check", "font", fontName)
		return nil
	}

	encDict, err := document.ResolveDict(ctx, doc, encObj)
	if err != nil {
		slog.Debug("compliance_2: fonts: resolving font encoding dict", "font", fontName, "error", err)
	}
	if encDict == nil {
		slog.Debug("compliance_2: fonts: encoding is not a dict, skipping .notdef check", "font", fontName)
		return nil
	}

	diffObj, ok := encDict["Differences"]
	if !ok {
		slog.Debug("compliance_2: fonts: no /Differences in encoding, skipping .notdef check", "font", fontName)
		return nil
	}

	diffArr, err := document.ResolveArray(ctx, doc, diffObj)
	if err != nil {
		slog.Debug("compliance_2: fonts: resolving encoding differences array", "font", fontName, "error", err)
	}
	if diffArr == nil {
		return nil
	}

	for _, item := range diffArr {
		if n, ok := item.(ast.PdfName); ok && n == ".notdef" {
			slog.Debug("compliance_2: fonts: .notdef glyph found in /Differences",
				"font", fontName, "page", pageNum)
			return failf(rule, "font %s on page %d: .notdef glyph in /Differences", fontName, pageNum)
		}
	}

	slog.Debug("compliance_2: fonts: no .notdef references in /Differences", "font", fontName)
	return nil
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

// toUnicodeCMapValuesRule returns the ToUnicode CMap values rule.
func toUnicodeCMapValuesRule(profile string) Rule {
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
func checkGlyphCoverage(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string) []Result {
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
		if rule.Specification == "" {
			return nil
		}
		// Part 1: CharSet is required. Parts 2+: only check if present (correctness),
		// but we still flag missing for Part 1.
		if pdfaPart(profile) == 1 {
			if _, hasCharSet := fd["CharSet"]; !hasCharSet {
				slog.Debug("compliance_2: fonts: Type1 subset missing /CharSet",
					"font", fontName, "page", pageNum)
				return failf(rule, "font %s on page %d: Type1 subset missing /CharSet", fontName, pageNum)
			}
		}
		slog.Debug("compliance_2: fonts: Type1 subset CharSet check passed", "font", fontName)
		return nil
	}

	return nil
}

// checkCIDFontGlyphCoverage validates that CIDFont subsets have a /CIDSet entry
// in their FontDescriptor.
func checkCIDFontGlyphCoverage(ctx context.Context, doc *document.Document, cidFontDict ast.PdfDict, fontName string, pageNum int, profile string) []Result {
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

	slog.Debug("compliance_2: fonts: CIDFont subset CIDSet check passed", "font", fontName)
	return nil
}

// checkToUnicodeCMapValues validates that a /ToUnicode CMap, if present, is
// decodable and does not map to invalid Unicode values (U+0000, U+FEFF, U+FFFE).
func checkToUnicodeCMapValues(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string) []Result {
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
func containsInvalidUnicodeMapping(cmap string) bool {
	upper := strings.ToUpper(cmap)

	// Only check inside bfchar sections: each line has <src> <dst>.
	// We look for lines where the SECOND hex string is an invalid value.
	inBfchar := false
	inBfrange := false
	for line := range strings.SplitSeq(upper, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "BEGINBFCHAR") {
			inBfchar = true
			continue
		}
		if strings.HasPrefix(trimmed, "ENDBFCHAR") {
			inBfchar = false
			continue
		}
		if strings.HasPrefix(trimmed, "BEGINBFRANGE") {
			inBfrange = true
			continue
		}
		if strings.HasPrefix(trimmed, "ENDBFRANGE") {
			inBfrange = false
			continue
		}

		if inBfchar {
			// Format: <srcCode> <dstUnicode>
			// Find the second hex string.
			parts := strings.Fields(trimmed)
			if len(parts) >= 2 { //nolint:mnd
				dst := parts[1]
				if isInvalidUnicodeDst(dst) {
					return true
				}
			}
		}
		if inBfrange {
			// Format: <srcLo> <srcHi> <dstStart>
			parts := strings.Fields(trimmed)
			if len(parts) >= 3 { //nolint:mnd
				dst := parts[2]
				if isInvalidUnicodeDst(dst) {
					return true
				}
			}
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
