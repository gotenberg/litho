package compliance

import (
	"context"
	"log/slog"
	"maps"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/typography"
	"github.com/gotenberg/litho/pkg/internal/typography/encodings"
)

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
						if code >= 0 && code <= 255 {
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
				data, err := doc.DecodeStream(ctx, stream)
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

// checkToUnicode validates the ToUnicode mapping requirement (rule 2).
func checkToUnicode(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, usage *fontUsageInfo) []Violation {
	rule := toUnicodeRule(profile)
	if rule.Specification == "" {
		return nil
	}

	// Type3 fonts: for PDF/A-4 and PDF/UA, VeraPDF exempts Type3 from
	// ToUnicode checks. For PDF/A parts 1–3 (levels a/u), Type3 fonts
	// must have resolvable Unicode mappings — either all glyph names in
	// /Encoding /Differences are AGL-resolvable or a /ToUnicode CMap is present.
	if isType3Font(ctx, doc, fontDict) {
		part := PDFAPart(profile)
		if part == 4 || IsPDFUA(profile) {
			return nil
		}
		// For parts 1–3: check if all Differences glyph names are AGL-resolvable.
		if allDifferencesGlyphNamesAGLResolvable(ctx, doc, fontDict) {
			return nil
		}
		// Fall through to the ToUnicode presence check below.
		if !hasToUnicode(fontDict) {
			return failf(rule, "font %s on page %d", fontName, pageNum)
		}
		return nil
	}

	// Exemption (a): standard encoding with Adobe Glyph List coverage.
	// This covers MacRomanEncoding, WinAnsiEncoding, and MacExpertEncoding.
	if hasStandardEncoding(ctx, doc, fontDict) {
		return nil
	}

	// Exemption (a) extended: encoding dictionary with standard BaseEncoding.
	if hasStandardBaseEncoding(ctx, doc, fontDict) {
		return nil
	}

	// Exemption (b): symbolic font with single cmap and an explicit encoding.
	// Symbolic fonts are only exempt when they define a character mapping via
	// /Encoding; without it, /ToUnicode is still required.
	fd := findFontDescriptor(ctx, doc, fontDict)
	if isSymbolicFont(ctx, doc, fd) {
		if _, hasEncoding := fontDict["Encoding"]; hasEncoding {
			return nil
		}
	}

	// Exemption (b2): Type 1 fonts whose character names are all resolvable
	// via the Adobe Glyph List. Per §6.3.8, if every glyph name in the
	// font's encoding (including /Differences) maps to a Unicode value
	// through the AGL, ToUnicode is not required.
	subtype := fontSubtype(ctx, doc, fontDict)
	if subtype == "Type1" || subtype == "MMType1" {
		if allGlyphNamesAGLResolvable(ctx, doc, fontDict) {
			return nil
		}
		// Exemption (b3): The Symbol font itself (BaseFont = Symbol or
		// *+Symbol) is exempt when all CharSet names are in the standard
		// Symbol encoding set.
		if isSymbolFontByName(ctx, doc, fontDict) && allGlyphNamesInSymbolEncoding(ctx, doc, fontDict) {
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
				return nil
			}
		}
	}

	// Exemption (c): Identity-H/Identity-V — only exempt for PDF/A, not PDF/UA.
	// For PDF/UA, a ToUnicode CMap is required for Identity-encoded Type0 fonts
	// unless glyph-level Unicode mapping is available through other means.
	if hasIdentityCMap(ctx, doc, fontDict) && !IsPDFUA(profile) {
		return nil
	}

	if !hasToUnicode(fontDict) {
		return failf(rule, "font %s on page %d", fontName, pageNum)
	}

	return nil
}

// checkTrueTypeEncoding validates TrueType-specific encoding rules (rule 7).
func checkTrueTypeEncoding(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string) []Violation {
	var results []Violation

	fd := findFontDescriptor(ctx, doc, fontDict)
	symbolic := isSymbolicFont(ctx, doc, fd)

	if symbolic {
		// Symbolic TrueType must not specify /Encoding.
		rule := trueTypeEncodingSymbolicNoEncodingRule(profile)
		if rule.Specification != "" {
			if _, hasEnc := fontDict["Encoding"]; hasEnc {
				results = append(results, failf(rule, "font %s on page %d", fontName, pageNum)...)
			}
		}

		// Symbolic TrueType cmap subtable check — requires font program parsing.
		// Full verification requires parsing the embedded TrueType program.
		_ = trueTypeSymbolicCmapRule(profile)
	} else {
		// Non-symbolic TrueType must use MacRomanEncoding or WinAnsiEncoding.
		rule := trueTypeEncodingNonSymbolicRule(profile)
		if rule.Specification != "" {
			encObj, hasEnc := fontDict["Encoding"]
			if !hasEnc {
				results = append(results, failf(rule, "font %s on page %d has no Encoding", fontName, pageNum)...)
			} else {
				// Encoding may be a name (direct) or dict with BaseEncoding.
				encName, _ := document.ResolveName(ctx, doc, encObj)
				switch encName {
				case "MacRomanEncoding", "WinAnsiEncoding":
				case "":
					// encName is empty — fall through to check encoding dictionary.
					// Encoding is a dictionary — check BaseEncoding.
					encDict, err := document.ResolveDict(ctx, doc, encObj)
					if err != nil || encDict == nil {
						results = append(results, failf(rule, "font %s on page %d has invalid Encoding type", fontName, pageNum)...)
					} else {
						baseEnc, _ := document.ResolveName(ctx, doc, encDict["BaseEncoding"])
						if baseEnc != "MacRomanEncoding" && baseEnc != "WinAnsiEncoding" {
							results = append(results, failf(rule, "font %s on page %d BaseEncoding=%s", fontName, pageNum, baseEnc)...)
						}

						// Part 1: Differences is forbidden entirely.
						// Parts 2+: Differences is allowed only if all glyph names
						// are in the Adobe Glyph List.
						if diffObj, hasDiff := encDict["Differences"]; hasDiff {
							if PDFAPart(profile) == 1 {
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
				default:
					// Invalid encoding name.
					results = append(results, failf(rule, "font %s on page %d encoding=%s", fontName, pageNum, encName)...)
				}
			}
		}
	}

	return results
}

// checkType0ToUnicode validates the ToUnicode requirement for composite fonts (rule 2).
func checkType0ToUnicode(ctx context.Context, doc *document.Document, type0Dict, cidFontDict ast.PdfDict, fontName string, pageNum int, profile string) []Violation {
	rule := toUnicodeRule(profile)
	if rule.Specification == "" {
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
				return nil
			}
		}
	}

	if !hasToUnicode(type0Dict) {
		return failf(rule, "font %s on page %d", fontName, pageNum)
	}

	return nil
}

// checkToUnicodeCMapValues validates that a /ToUnicode CMap, if present, is
// decodable and does not map to invalid Unicode values (U+0000, U+FEFF, U+FFFE).
func checkToUnicodeCMapValues(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string) []Violation {
	rule := toUnicodeCMapValuesRule(profile)
	if rule.Specification == "" {
		return nil
	}

	tuObj, ok := fontDict["ToUnicode"]
	if !ok {
		return nil
	}

	stream, err := document.ResolveStream(ctx, doc, tuObj)
	if err != nil {
		slog.Debug("compliance: fonts: failed to resolve ToUnicode stream",
			"font", fontName, "error", err)
		return failf(rule, "font %s on page %d: cannot resolve /ToUnicode stream", fontName, pageNum)
	}
	if stream.Dict == nil {
		return failf(rule, "font %s on page %d: /ToUnicode stream has no dictionary", fontName, pageNum)
	}

	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		// Fall back to raw data.
		data, err = stream.Bytes()
		if err != nil || len(data) == 0 {
			slog.Debug("compliance: fonts: failed to decode ToUnicode CMap",
				"font", fontName, "error", err)
			return failf(rule, "font %s on page %d: cannot decode /ToUnicode CMap", fontName, pageNum)
		}
	}

	// Scan the CMap data for invalid Unicode values.
	// Look for hex strings in beginbfchar/beginbfrange sections that map to
	// U+0000, U+FEFF, or U+FFFE.
	cmapStr := string(data)
	if containsInvalidUnicodeMapping(cmapStr) {
		return failf(rule, "font %s on page %d: /ToUnicode CMap maps to U+0000, U+FEFF, or U+FFFE",
			fontName, pageNum)
	}

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
			if len(parts) >= 2 {
				if isInvalidUnicodeDst(parts[1]) {
					return true
				}
			}
		}
		if inBfrange {
			parts := strings.Fields(trimmed)
			if len(parts) >= 3 {
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
			if len(parts) >= 2 {
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
	if len(t) < 3 || t[0] != '<' || t[len(t)-1] != '>' {
		return false
	}
	hex := t[1 : len(t)-1]
	val := uint32(0)
	for _, c := range hex {
		val <<= 4
		switch {
		case c >= '0' && c <= '9':
			val |= uint32(c - '0') //nolint:gosec // G115: c is guaranteed to be in ['0','9'] by the case guard.
		case c >= 'A' && c <= 'F':
			val |= uint32(c-'A') + 10 //nolint:gosec // G115: c is guaranteed to be in ['A','F'] by the case guard.
		default:
			return false
		}
	}
	return (val >= 0xE000 && val <= 0xF8FF) || (val >= 0xF0000 && val <= 0xFFFFF)
}
