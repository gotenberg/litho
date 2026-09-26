package compliance

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"unicode/utf16"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/typography"
	"github.com/gotenberg/litho/pkg/internal/typography/cmap"
	"github.com/gotenberg/litho/pkg/internal/typography/encodings"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
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
func hasStandardBaseEncoding(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, usage *fontUsageInfo) bool {
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
	// A standard base encoding maps every base code to an AGL-resolvable name.
	// When /Differences remap some codes, the exemption still holds as long as
	// each remapped name behind a rendered code also resolves to Unicode (AGL or
	// the Symbol set): every used code then has a well-defined Unicode value
	// without /ToUnicode. Scoping to rendered codes matches ISO 19005-2
	// §6.2.11.7.2, which asks for "the map of all used character codes": an
	// override the content stream never shows maps no used code, so it cannot
	// leave one unmapped. This is font-type agnostic (veraPDF resolves per-glyph
	// Unicode for TrueType the same as Type1), so it also covers TrueType fonts,
	// which the Type1-only AGL exemption in checkToUnicode would otherwise miss.
	return usedDifferencesGlyphNamesResolvable(ctx, doc, fontDict, usage)
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

// fontEncodingIsDict reports whether the font's /Encoding is a dictionary, the
// form that states glyph names through /Differences rather than implying them
// from a predefined table.
func fontEncodingIsDict(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	encObj, ok := fontDict["Encoding"]
	if !ok {
		return false
	}
	dict, err := document.ResolveDict(ctx, doc, encObj)
	return err == nil && dict != nil
}

// standard14BuiltinEncodingApplies reports whether a /BaseFont naming one of
// the standard 14 still stands in for a font program litho can read an encoding
// from.
//
// ISO 32000-1, 9.6.2.2 makes /FirstChar, /LastChar, /Widths and /FontDescriptor
// all present or all absent: the standard 14 names are a shorthand for metrics a
// consumer already holds, and a dictionary stating its own /FontDescriptor has
// left the shorthand behind. veraPDF reads it the same way, loading the standard
// metrics only when the font dictionary carries no non-empty /FontDescriptor,
// and otherwise leaving every code the /Differences array skips unnamed.
func standard14BuiltinEncodingApplies(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	if !isStandard14Font(ctx, doc, fontDict) {
		return false
	}
	return len(document.ResolveFontDescriptor(ctx, doc, fontDict)) == 0
}

// fontNamesUnencodedCodes reports whether a simple font can name a character
// code its /Encoding leaves alone.
//
// ISO 32000-1, Table 114 makes the implicit base of a /Differences array the
// font program's built-in encoding, so a font embedding no program and earning
// no standard 14 substitute supplies no name at all, and 9.10.2 then gives the
// code no Unicode value.
//
// # Corpus evidence
//
//   - govdocs thread_000_000885.pdf: shows Palatino-Roman with a
//     /FontDescriptor, no program and a /Differences-only /Encoding, and
//     veraPDF reports ISO 14289-1 §7.21.7 t1 on every glyph it draws.
func fontNamesUnencodedCodes(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	return embedsFontProgram(ctx, doc, fontDict) ||
		standard14BuiltinEncodingApplies(ctx, doc, fontDict)
}

// allGlyphNamesAGLResolvable returns true if the glyph names a Type 1 font
// reaches carry Unicode values, through the glyph lists ISO 32000-1, 9.10.2
// admits. With the shown character codes on hand it judges those codes, through
// [everyUsedGlyphNameHasUnicode]. Without them it surveys the names the font
// mentions: a standard 14 font's built-in encoding, the /Encoding /Differences
// array, and /FontDescriptor /CharSet, which lists a Type 1 subset's glyphs.
//
// # Corpus evidence
//
//   - pdfjs js-buttons.pdf: shows code 0x34 through an unembedded ZapfDingbats
//     with no /ToUnicode inside a widget appearance, which veraPDF reports
//     under ISO 14289-1 §7.21.7 t1. It backs the ZapfDingbats exclusion.
//   - govdocs thread_300_300031.pdf: shows ZapfDingbats code 110, which its
//     /Differences does not name, and veraPDF reports it. It backs judging the
//     shown codes rather than surveying the names the font mentions.
func allGlyphNamesAGLResolvable(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, usage *fontUsageInfo) bool {
	// A standard 14 font carries a well-known built-in encoding whose glyph
	// names are all AGL-resolvable, so its base encoding needs no embedded
	// program to prove it. /Differences is outside that guarantee: it can bind a
	// rendered code to a name in neither the AGL nor the Symbol set, leaving
	// that code with no Unicode value at all. ISO 19005-2 §6.2.11.7.2 (with ISO
	// 14289-1 §7.21.7 and ISO 14289-2 §8.4.5.8) demands a Unicode value for
	// every used character code, so a standard /BaseFont name does not excuse an
	// unmappable override.
	// ZapfDingbats is the one standard 14 font this does not hold for. Its
	// built-in encoding names its glyphs a1 to a191, which are in neither the
	// Adobe Glyph List nor the Symbol set, and ISO 32000-1, 9.10.2 admits "the
	// Adobe standard Latin character set or the set of named characters in the
	// Symbol font" and no third set: a ZapfDingbats glyph therefore has no
	// Unicode value without a /ToUnicode CMap. litho already treats the pair as
	// special elsewhere (the stamp writer and the TrueType-encoding fixer both
	// exempt "Symbol and ZapfDingbats" from a standard encoding).
	//
	// Symbol with no /Encoding is excluded too. Its built-in encoding is not a
	// Latin one, and 9.10.2 admits Symbol names only through a /Differences
	// array, so an unembedded Symbol with no /Encoding names nothing its glyphs
	// could draw Unicode from. On one-font fixtures veraPDF reports every glyph
	// of such a font under ISO 14289-1 §7.21.7, ISO 19005-2 §6.2.11.7.2 and ISO
	// 19005-1 §6.3.8, and passes Helvetica in the same shape. An embedded
	// Symbol still names its glyphs through its program, which the per-code
	// check below reads.
	symbolWithoutEncoding := isSymbolFontByName(ctx, doc, fontDict) && fontDict["Encoding"] == nil
	if standard14BuiltinEncodingApplies(ctx, doc, fontDict) && !isZapfDingbatsByName(ctx, doc, fontDict) && !symbolWithoutEncoding {
		return usedDifferencesGlyphNamesResolvable(ctx, doc, fontDict, usage)
	}

	// With the shown codes on hand, judge those and nothing else. The name
	// surveys below answer a different question, "does every name this font
	// mentions resolve", and a font may mention none of the names behind the
	// codes it shows: /Differences naming a handful of codes says nothing about
	// the rest, which fall to an implicit base encoding, and a /CharSet lists an
	// embedded subset's glyphs rather than the encoding that reaches them. Both
	// surveys answered "resolvable" for fonts whose shown codes had no name at
	// all, and veraPDF reports exactly those codes.
	if usage != nil && len(usage.charCodes) > 0 {
		return everyUsedGlyphNameHasUnicode(ctx, doc, fontDict, usage)
	}

	// With no recorded usage, fall back to surveying the names the font
	// mentions. Check /Encoding /Differences if present.
	if encObj, ok := fontDict["Encoding"]; ok {
		encDict, err := document.ResolveDict(ctx, doc, encObj)
		if err == nil && encDict != nil {
			if diffObj, hasDiff := encDict["Differences"]; hasDiff {
				diffArr, err := document.ResolveArray(ctx, doc, diffObj)
				if err == nil {
					for _, item := range diffArr {
						name, ok := item.(ast.PdfName)
						if !ok {
							continue // integer code, skip
						}
						if string(name) == ".notdef" {
							continue
						}
						if !glyphNameHasUnicode(string(name)) {
							return false
						}
					}
					return true
				}
			}
		}
	}

	// Check /FontDescriptor /CharSet, a string of slash-separated glyph names.
	fd := document.ResolveFontDescriptor(ctx, doc, fontDict)
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
					if !glyphNameHasUnicode(name) {
						return false
					}
				}
				return true
			}
		}
	}

	return false
}

// isZapfDingbatsByName reports whether the font is ZapfDingbats, allowing the
// "ABCDEF+" subset prefix ISO 32000-1, 9.6.4 defines.
func isZapfDingbatsByName(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	base, _ := document.ResolveName(ctx, doc, fontDict["BaseFont"])
	name := string(base)
	if _, suffix, ok := strings.Cut(name, "+"); ok {
		name = suffix
	}
	return name == "ZapfDingbats"
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
			continue // integer code, skip
		}
		if string(name) == ".notdef" {
			continue
		}
		if !glyphNameHasUnicode(string(name)) {
			return false
		}
	}
	return true
}

// shownType3CodesWithoutUnicode returns, sorted, the codes a page shows through
// a Type 3 font whose glyph name, the one its /Encoding gives, carries no
// Unicode value.
//
// ISO 14289-1, 7.21.7 and ISO 19005-2, 6.2.11.7.2 judge each glyph shown, so a
// name /Differences assigns to a code nothing shows is out of scope. Per ISO
// 32000-1, 9.6.5 the /Differences array of a Type 3 font gives its complete
// encoding, so a shown code the array leaves out, or names with an empty name,
// has no glyph name and no Unicode value. A /BaseEncoding, which the clause does
// not provide for, still supplies a missing name, as it does for veraPDF. A
// shown .notdef is left to the Unicode-value and .notdef checks, since the glyph
// lists map it to U+0000.
//
// # Corpus evidence
//
//   - pdfjs issue5896.pdf: leaves empty names at two unshown codes, and veraPDF
//     passes its letters.
func shownType3CodesWithoutUnicode(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, usage *fontUsageInfo) []byte {
	encObj, ok := fontDict["Encoding"]
	if !ok {
		return nil
	}
	encDict, err := document.ResolveDict(ctx, doc, encObj)
	if err != nil || encDict == nil {
		return nil
	}
	names := differencesGlyphNames(ctx, doc, encDict)
	var base map[byte]string
	if baseName, err := document.ResolveName(ctx, doc, encDict["BaseEncoding"]); err == nil && baseName != "" {
		base = predefinedEncodingNames(string(baseName))
	}
	var out []byte
	for _, code := range sortedByteKeys(usage.charCodes) {
		name, ok := names[code]
		if !ok {
			name = base[code]
		}
		if name == ".notdef" {
			continue
		}
		if name == "" || !glyphNameHasUnicode(name) {
			out = append(out, code)
		}
	}
	return out
}

// glyphNameHasUnicode reports whether a glyph name yields a Unicode value on
// its own, with no /ToUnicode CMap: through the Symbol font character set (PDF
// Reference Appendix D, Table D.5), whose names are standard Adobe glyph names,
// or through the Adobe Glyph List, including the uniXXXX and subset spellings
// [encodings.GlyphToRuneFull] accepts.
//
// The names in [nonAGLGlyphNames] are excluded. [encodings.GlyphToRuneFull]
// serves text extraction too and maps names no published glyph list defines, so
// it answers a broader question than ISO 32000-1, 9.10.2 asks here.
//
// # Corpus evidence
//
//   - govdocs thread_300_300084.pdf: shows codes its embedded Type 1 program
//     names G4F and G56, which litho read as U+004F and U+0056, and veraPDF
//     reports those glyphs as having no Unicode value. It backs refusing the
//     synthetic code-point spellings.
func glyphNameHasUnicode(name string) bool {
	if _, ok := encodings.SymbolGlyphsUnicode[name]; ok {
		return true
	}
	// The typography tables reach past the AGL to serve text extraction; a
	// conformance exemption may not. See [nonAGLGlyphNames].
	if nonAGLGlyphNames[name] {
		return false
	}
	// A name on the list wins even if it happens to look synthetic. Otherwise
	// the code-point spellings producers invent are refused, since veraPDF
	// gives such a glyph no Unicode value.
	if _, ok := encodings.GlyphToRuneSimple(name); ok {
		return true
	}
	if encodings.IsSyntheticGlyphName(name) {
		return false
	}
	// TODO: veraPDF looks a name up in the Adobe Glyph List table alone
	// (AdobeGlyphList.get), which holds no uniXXXX or uXXXX spelling, so a
	// code named uni0044 has no Unicode value there: veraPDF 1.30 fails it at
	// PDF/A-2u through a Type 1 and through a Type 3 font. Measure the corpora
	// before refusing those spellings here.
	_, ok := encodings.GlyphToRuneFull(name)
	return ok
}

// shownCodesNamedByResolvableDifferences reports whether /Differences names
// every character code the content stream shows through this font, and every
// such name carries a Unicode value.
//
// It is deliberately stricter than [usedDifferencesGlyphNamesResolvable], which
// walks the /Differences entries and so says nothing about a shown code the
// array never names. Here a code left to the base encoding fails the test: a
// predefined table only implies a name, and for a TrueType ISO 32000-1, 9.6.6.4
// still resolves the code through the program's own cmap.
//
// Answering false with no usage on hand is the safe direction: without the shown
// codes there is nothing to prove.
func shownCodesNamedByResolvableDifferences(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, usage *fontUsageInfo) bool {
	if usage == nil || len(usage.charCodes) == 0 {
		return false
	}
	encObj, ok := fontDict["Encoding"]
	if !ok {
		return false
	}
	encDict, err := document.ResolveDict(ctx, doc, encObj)
	if err != nil || encDict == nil {
		return false
	}
	names := differencesGlyphNames(ctx, doc, encDict)
	for code := range usage.charCodes {
		name, named := names[code]
		if !named || name == "" {
			return false
		}
		if name == ".notdef" {
			continue
		}
		if !glyphNameHasUnicode(name) {
			return false
		}
	}
	return true
}

// usedDifferencesGlyphNamesResolvable reports whether every /Encoding
// /Differences override behind a rendered character code yields a Unicode
// value. Codes the array leaves alone keep the font's base encoding and are out
// of scope here. With no recorded usage the check is vacuous: the rules it
// backs are scoped to veraPDF's Glyph object, which exists only for a code the
// content stream actually shows, so an override nothing renders maps nothing.
func usedDifferencesGlyphNamesResolvable(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, usage *fontUsageInfo) bool {
	if usage == nil || len(usage.charCodes) == 0 {
		return true
	}
	encObj, ok := fontDict["Encoding"]
	if !ok {
		return true
	}
	encDict, err := document.ResolveDict(ctx, doc, encObj)
	if err != nil || encDict == nil {
		return true
	}
	for code, name := range differencesGlyphNames(ctx, doc, encDict) {
		if !usage.charCodes[code] || name == "" || name == ".notdef" {
			continue
		}
		if !glyphNameHasUnicode(name) {
			return false
		}
	}
	return true
}

// renderedNotdefDifferencesCode returns the lowest rendered character code whose
// /Encoding /Differences entry names .notdef, and whether one exists. Same
// subject and same scoping as [checkNotdefInDifferences], which reports the
// reference half of the construct under ISO 19005-2 §6.2.11.8.
func renderedNotdefDifferencesCode(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, usage *fontUsageInfo) (byte, bool) {
	if usage == nil {
		return 0, false
	}
	encObj, ok := fontDict["Encoding"]
	if !ok {
		return 0, false
	}
	encDict, err := document.ResolveDict(ctx, doc, encObj)
	if err != nil || encDict == nil {
		return 0, false
	}
	names := differencesGlyphNames(ctx, doc, encDict)
	for _, code := range sortedByteKeys(usage.charCodes) {
		if names[code] == ".notdef" {
			return code, true
		}
	}
	return 0, false
}

// predefinedEncodingTable returns the complete charCode to glyphName table a
// font's /Encoding names, or nil when the font's encoding is not fully given by
// a predefined table.
//
// Only a predefined encoding states a glyph name for every code it covers, so
// only there does the absence of a name mean the code names no glyph. An
// embedded program's built-in encoding is sparse by nature, naming just the
// subset's own codes, and reading a gap there as "no glyph" would condemn every
// subset font.
func predefinedEncodingTable(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) map[byte]string {
	encObj, ok := fontDict["Encoding"]
	if !ok {
		return nil
	}
	if name, err := document.ResolveName(ctx, doc, encObj); err == nil && name != "" {
		return predefinedEncodingNames(string(name))
	}
	encDict, err := document.ResolveDict(ctx, doc, encObj)
	if err != nil || encDict == nil {
		return nil
	}
	base, err := document.ResolveName(ctx, doc, encDict["BaseEncoding"])
	if err != nil || base == "" {
		return nil
	}
	table := predefinedEncodingNames(string(base))
	if len(table) == 0 {
		return nil
	}
	maps.Copy(table, differencesGlyphNames(ctx, doc, encDict))
	return table
}

// renderedUnencodedCode returns a character code the content stream shows that
// the font's predefined encoding leaves without a glyph name.
//
// A code the encoding does not name selects no glyph, so it stands for no
// character and carries no Unicode value, which ISO 19005-4, 6.2.10.7 (with
// ISO 19005-2, 6.2.11.7.2 and ISO 14289-1, 7.21.7) forbids for a shown glyph.
// This is the unnamed twin of an explicit /Differences .notdef. A simple font
// takes one byte per code and WinAnsiEncoding names nothing below 32, so
// two-byte text shown through a WinAnsiEncoding font reaches code 0.
//
// # Corpus evidence
//
//   - pdfjs issue18059.pdf: sets /Helvetica with /WinAnsiEncoding and then
//     shows <0044004500460047...>, whose every other byte is code 0, and
//     veraPDF reports the glyph at code 0.
//   - verapdf 6-3-8-t01-pass-b.pdf: a file built to conform, which litho
//     reported while it judged codes from 32 up, because its
//     MacExpertEncoding table omits the space at code 32 that Annex D
//     defines.
func renderedUnencodedCode(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, usage *fontUsageInfo) (byte, bool) {
	if usage == nil || len(usage.charCodes) == 0 {
		return 0, false
	}
	if fontSubtype(ctx, doc, fontDict) == "Type0" {
		return 0, false // A composite font's codes are not single bytes.
	}
	table := predefinedEncodingTable(ctx, doc, fontDict)
	if len(table) == 0 {
		return 0, false
	}
	for _, code := range sortedByteKeys(usage.charCodes) {
		// Only the control range is judged. Every predefined encoding of
		// ISO 32000-2, Annex D begins at 32, so a code below it names no glyph
		// in any of them and the conclusion holds whatever table litho carries.
		// Above 32 a gap may be litho's rather than the encoding's: its
		// MacExpertEncoding table omits the space at code 32 that Annex D
		// defines, and reading that gap as "no glyph" reports conforming files.
		if code >= firstEncodedCode {
			continue
		}
		if table[code] == "" {
			return code, true
		}
	}
	return 0, false
}

// firstEncodedCode is the lowest character code any predefined encoding of
// ISO 32000-2, Annex D names. Codes below it are the control range, which none
// of those encodings gives a glyph.
const firstEncodedCode = 32

// notdefUnicodeViolation reports a rendered /.notdef override as a forbidden
// Unicode value. ISO 32000-1 §9.6.6.1 gives .notdef the meaning "no glyph", so
// the code behind it stands for no character and the Unicode value it maps to is
// U+0000, which ISO 19005-2 §6.2.11.7.2 (and ISO 19005-3 §6.2.11.7.2, ISO
// 19005-4 §6.2.10.7, ISO 14289-1 §7.21.7, ISO 14289-2 §8.4.5.8) forbid. A
// /ToUnicode entry for the code supersedes the glyph name, since ISO 32000-1
// §9.10.2 makes the CMap the primary text-extraction mapping: mapping carries
// the CMap's entries and is nil when the font declares no /ToUnicode at all.
func notdefUnicodeViolation(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, rule Rule, usage *fontUsageInfo, mapping map[string]string) []Violation {
	if code, ok := renderedNotdefDifferencesCode(ctx, doc, fontDict, usage); ok {
		if _, mapped := mapping[string([]byte{code})]; !mapped {
			return glyphFailf(rule, uint32(code), "font %s on page %d: rendered code %d maps to .notdef via /Differences (renders U+0000)",
				fontName, pageNum, code)
		}
	}
	if code, ok := renderedUnencodedCode(ctx, doc, fontDict, usage); ok {
		if _, mapped := mapping[string([]byte{code})]; !mapped {
			return glyphFailf(rule, uint32(code), "font %s on page %d: rendered code %d names no glyph in the font's encoding (renders U+0000)",
				fontName, pageNum, code)
		}
	}
	return nil
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
	fd := document.ResolveFontDescriptor(ctx, doc, fontDict)
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

// everyUsedGlyphNameHasUnicode reports whether every character code the content
// stream shows resolves, through the font's encoding, to a glyph name carrying
// a Unicode value.
//
// ISO 14289-1, 7.21.7 (with ISO 19005-2, 6.2.11.7.2) scopes the requirement to
// the codes a page uses, which is also veraPDF's model: it raises the rule per
// Glyph object, and a Glyph exists only for a code something renders. A glyph
// merely present in an embedded subset is out of scope, so a Type 1 subset
// whose /CharSet lists a name with no Unicode value is still conforming when
// nothing shows that name's code.
//
// Unlike [usedGlyphNamesResolvable] this demands the encoding actually name
// every used code. A code the encoding leaves out has no glyph name to judge,
// and skipping it would exempt a font on the strength of the codes litho
// happened to read rather than on the codes the page shows.
//
// # Corpus evidence
//
//   - pdfjs bug1260585.pdf: embeds CMSY10 whose /CharSet names negationslash,
//     in neither the Adobe standard Latin set nor the Symbol font ISO 32000-1,
//     9.10.2 admits. The program's own encoding puts it at code 0x36, which the
//     page never shows, and veraPDF reports nothing.
func everyUsedGlyphNameHasUnicode(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, usage *fontUsageInfo) bool {
	if usage == nil || len(usage.charCodes) == 0 {
		return false
	}
	encoding := resolveSimpleFontEncoding(ctx, doc, fontDict)
	if len(encoding) == 0 {
		return false
	}
	for code := range usage.charCodes {
		glyphName, ok := encoding[code]
		if !ok || glyphName == "" {
			return false
		}
		if glyphName == ".notdef" {
			continue
		}
		if !glyphNameHasUnicode(glyphName) {
			return false
		}
	}
	return true
}

// usedGlyphNamesResolvable returns true if all glyph names referenced by the
// used character codes in a Type 1 font are resolvable to Unicode: either
// via the AGL or the Symbol encoding set.
//
// # Corpus evidence
//
//   - pdfjs issue14438.pdf: shows code 13 of a symbolic CMSY7 whose
//     /Differences name it circlecopyrt, a TeX spelling of the copyright sign
//     that GlyphToRuneFull resolves and the Adobe Glyph List does not carry,
//     and veraPDF reports the glyph. It backs judging names through
//     [glyphNameHasUnicode] rather than the typography tables.
func usedGlyphNamesResolvable(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, usage *fontUsageInfo) bool {
	encoding := resolveSimpleFontEncoding(ctx, doc, fontDict)
	if encoding == nil {
		return false
	}

	for code := range usage.charCodes {
		glyphName, ok := encoding[code]
		if !ok || glyphName == ".notdef" || glyphName == "" {
			continue // unmapped or .notdef, skip
		}
		// [glyphNameHasUnicode] rather than encodings.GlyphToRuneFull: the
		// typography tables serve text extraction and map names no published
		// glyph list defines, which ISO 32000-1, 9.10.2 does not admit here.
		// See [nonAGLGlyphNames].
		if glyphNameHasUnicode(glyphName) || symbolEncodingNames[glyphName] {
			continue
		}
		return false
	}
	return true
}

// resolveSimpleFontEncoding builds a charCode → glyphName mapping for a
// simple font (Type1, TrueType). Checks PDF-level /Encoding first, then
// falls back to the Type 1 font program's built-in encoding.
//
// # Corpus evidence
//
//   - pdfjs bug859204.pdf: writes /Encoding /NULL over an embedded Type 1
//     whose built-in names are all in the Adobe Glyph List, and veraPDF
//     derives a Unicode value for every one of the eleven codes the page
//     shows. It backs keeping the built-in encoding under an unknown name.
//   - govdocs thread_000_000364.pdf: a subset CFF that maps codes 9, 10, 13,
//     127 and 128 in /Differences and every printing code in the CFF. It
//     backs the program's built-in encoding as the implicit base.
//   - govdocs thread_300_300031.pdf: shows ZapfDingbats code 110 that its
//     /Differences leave alone, and veraPDF reports it under ISO 14289-1
//     §7.21.7 t1.
//   - pdfjs bug1727053.pdf: shows code 80 through an embedded Arial,Bold whose
//     three cmap subtables all map U+0050 to the glyph P, and veraPDF reports
//     nothing. It backs the StandardEncoding base for a non-symbolic TrueType.
func resolveSimpleFontEncoding(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) map[byte]string {
	// The PDF-level /Encoding takes priority over the program's own.
	if encObj, ok := fontDict["Encoding"]; ok {
		// Named encoding. A name no predefined encoding carries states nothing,
		// so the font's built-in encoding stays in force (ISO 32000-1, Table
		// 114, BaseEncoding). Reading the invalid name as an empty encoding
		// leaves the shown codes unnamed and reports ISO 14289-1, 7.21.7 t1
		// against them.
		if encName, err := document.ResolveName(ctx, doc, encObj); err == nil && encName != "" {
			if names := predefinedEncodingNames(string(encName)); len(names) > 0 {
				return names
			}
		}
		// Encoding dict with /Differences.
		if encDict, err := document.ResolveDict(ctx, doc, encObj); err == nil && encDict != nil {
			result := make(map[byte]string)
			// Start from BaseEncoding if present. Absent, /Differences describes
			// differences from an implicit base encoding, which for an embedded
			// program is that program's built-in encoding (ISO 32000-1, Table
			// 114, BaseEncoding). A subset CFF names only a handful of codes in
			// /Differences and leaves the rest to its own encoding, so dropping
			// the base leaves the shown codes unresolved and the width check
			// silent.
			if baseName, err := document.ResolveName(ctx, doc, encDict["BaseEncoding"]); err == nil && baseName != "" {
				maps.Copy(result, predefinedEncodingNames(string(baseName)))
			} else {
				builtin := builtinProgramEncoding(ctx, doc, fontDict)
				// ZapfDingbats is symbolic whether or not a font descriptor says
				// so, so Table 114 gives it its built-in encoding, never
				// StandardEncoding. Those built-in names (a1 to a191) carry no
				// Unicode value, so a code its /Differences leave alone stays
				// unnamed, and veraPDF reports it under ISO 14289-1 §7.21.7 t1.
				if len(builtin) == 0 && !isSymbolicFont(ctx, doc, document.ResolveFontDescriptor(ctx, doc, fontDict)) &&
					!isZapfDingbatsByName(ctx, doc, fontDict) && fontNamesUnencodedCodes(ctx, doc, fontDict) {
					// ISO 32000-1, 9.6.6.4 and Table 114: a non-symbolic font
					// whose program offers no built-in encoding takes
					// StandardEncoding as the implicit base. litho reads a
					// built-in encoding out of /FontFile and /FontFile3 only, so
					// a TrueType /FontFile2 lands here and every code the
					// /Differences array leaves alone stayed unnamed, which the
					// .notdef rule then read as a reference to glyph 0.
					builtin = predefinedEncodingNames("StandardEncoding")
				}
				maps.Copy(result, builtin)
			}
			maps.Copy(result, differencesGlyphNames(ctx, doc, encDict))
			if len(result) > 0 {
				return result
			}
		}
	}

	return builtinProgramEncoding(ctx, doc, fontDict)
}

// fontDifferencesNames returns only the charCode → glyphName overrides the
// font's /Encoding dictionary states in /Differences. Per ISO 32000-1, 9.6.6.1
// those override whatever base encoding is in force, so a caller that must know
// which name the file itself chose for a code reads them apart from the merged
// encoding [resolveSimpleFontEncoding] returns.
func fontDifferencesNames(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) map[byte]string {
	encObj, ok := fontDict["Encoding"]
	if !ok {
		return nil
	}
	encDict, err := document.ResolveDict(ctx, doc, encObj)
	if err != nil || encDict == nil {
		return nil
	}
	return differencesGlyphNames(ctx, doc, encDict)
}

// cffPredefinedEncodingFilled returns names plus the codes a predefined CFF
// encoding assigns that names leaves unnamed.
//
// [typography.ExtractCFFPredefinedEncodingNames] reports the whole table while
// [typography.ExtractCFFEncodingNames] reports only the codes whose glyph the
// subset embeds. That intersection is right for drawing and wrong for asking
// what a code means: a subset CFF declaring the Expert encoding leaves a code
// unnamed when its charset lacks the glyph the encoding plainly assigns it.
//
// The fill applies only where the built-in encoding governs. Per ISO 32000-1,
// Table 114 a named /Encoding or a /BaseEncoding replaces the program's own
// encoding outright, so a font naming one keeps resolving its codes through
// that table rather than the font's.
//
// # Corpus evidence
//
//   - pdfjs issue8586.pdf: embeds BHIACH+TimesNRExpertMT, a subset CFF
//     declaring the Expert encoding whose charset holds Gsmall and Ismall but
//     not Hsmall, so code 0x61 was left unnamed even though the encoding
//     assigns it Hsmall. That is the only code the page shows, and Hsmall
//     carries a PUA value, so ISO 19005-2 §6.2.11.7.3 went unreported while
//     veraPDF 1.30.2 failed it.
//   - verapdf: the nine fixtures that embed an Expert CFF all name
//     /WinAnsiEncoding or /MacExpertEncoding, which backs limiting the fill
//     to fonts whose built-in encoding governs.
func cffPredefinedEncodingFilled(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, names map[byte]string) map[byte]string {
	if !builtinEncodingGoverns(ctx, doc, fontDict) {
		return names
	}
	fd := document.ResolveFontDescriptor(ctx, doc, fontDict)
	if fd == nil {
		return names
	}
	fontFile3Obj, ok := fd["FontFile3"]
	if !ok {
		return names
	}
	stream, err := document.ResolveStream(ctx, doc, fontFile3Obj)
	if err != nil {
		return names
	}
	data, err := decodeFontProgram(ctx, doc, fontFile3Obj, stream)
	if err != nil {
		return names
	}
	predefined := typography.ExtractCFFPredefinedEncodingNames(data)
	if len(predefined) == 0 {
		return names
	}

	filled := make(map[byte]string, len(names)+len(predefined))
	maps.Copy(filled, names)
	for code, name := range predefined {
		if _, taken := filled[code]; !taken {
			filled[code] = name
		}
	}
	return filled
}

// builtinEncodingGoverns reports whether an embedded program's own encoding is
// the one in force, which per ISO 32000-1, Table 114 holds only when the font
// dictionary neither names an /Encoding nor states a /BaseEncoding.
func builtinEncodingGoverns(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) bool {
	encObj, ok := fontDict["Encoding"]
	if !ok {
		return true
	}
	if encName, err := document.ResolveName(ctx, doc, encObj); err == nil && encName != "" {
		return predefinedEncodingNames(string(encName)) == nil
	}
	encDict, err := document.ResolveDict(ctx, doc, encObj)
	if err != nil || encDict == nil {
		return true
	}
	baseName, err := document.ResolveName(ctx, doc, encDict["BaseEncoding"])
	return err != nil || baseName == ""
}

// builtinEncodingNames returns a Type 1 program's own charCode → glyphName
// mapping. [typography.ExtractType1BuiltinEncoding] reports a program that
// references a predefined encoding ("/Encoding StandardEncoding def") by
// repeating that encoding's name across every code, so expand the reference to
// the table's glyph names instead of reading the name as a glyph. Returns nil
// for a reference to an encoding litho holds no table for, leaving the codes
// unresolved rather than guessed.
func builtinEncodingNames(fontData []byte) map[byte]string {
	builtin := typography.ExtractType1BuiltinEncoding(fontData)
	switch builtin[0] {
	case "":
	case "StandardEncoding":
		return encodingGlyphNames(builtin[0], encodings.StandardEncoding)
	case "MacRomanEncoding", "WinAnsiEncoding", "MacExpertEncoding":
		return predefinedEncodingNames(builtin[0])
	default:
		// Every code carrying the same name is the reference form; litho holds
		// no table for the rest (ISOLatin1Encoding, SymbolEncoding).
		if builtin[0] == builtin[len(builtin)-1] {
			return nil
		}
	}
	result := make(map[byte]string, len(builtin))
	for code, name := range builtin {
		if name != "" {
			result[byte(code)] = name //nolint:gosec // the array is indexed by one byte of character code, 0-255
		}
	}
	return result
}

// builtinProgramEncoding returns the charCode → glyphName mapping an embedded
// Type 1 (/FontFile) or CFF (/FontFile3) program carries in its own encoding.
// Returns nil when the descriptor holds no such program or it cannot be read.
//
// # Corpus evidence
//
//   - pdfjs bug860632.pdf: embeds Helvetica as a PFB container with no
//     /Encoding. veraPDF, which cannot read that program, finds no name for
//     any code it shows and reports ISO 14289-1, 7.21.7 t1 on each, while
//     litho read StandardEncoding out of the PFB. It backs reading no
//     encoding from a /FontFile that holds no Type 1 program.
//   - govdocs thread_000_000341.pdf: embeds AvantGarde-Book with a program
//     that names a predefined encoding and binds 110 codes through
//     /Differences, leaving the other 146, the space and the digits among
//     them, to the reference.
func builtinProgramEncoding(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) map[byte]string {
	fd := document.ResolveFontDescriptor(ctx, doc, fontDict)
	if fd == nil {
		return nil
	}
	if fontFileObj, ok := fd["FontFile"]; ok {
		stream, err := document.ResolveStream(ctx, doc, fontFileObj)
		// A /FontFile that holds no Type 1 program in the form ISO 32000-1,
		// 9.9.1 defines embeds no encoding either: [streamHoldsFontProgram]
		// already leaves such a font unembedded, and reading names out of it
		// anyway credited glyphs with Unicode values from a program the font
		// was reported as lacking.
		if err == nil && streamHoldsFontProgram(ctx, doc, "FontFile", fontFileObj, stream) {
			data, err := decodeFontProgram(ctx, doc, fontFileObj, stream)
			if err == nil {
				if enc := typography.ExtractType1EncodingNames(data); len(enc) > 0 {
					return enc
				}
				// A program that names a predefined encoding rather than
				// building one ("/Encoding StandardEncoding def") carries no
				// per-code array for [typography.ExtractType1EncodingNames]
				// to read, so its codes would stay unnamed.
				// [builtinEncodingNames] expands the reference.
				if enc := builtinEncodingNames(data); len(enc) > 0 {
					return enc
				}
			}
		}
	}

	// A subset CFF with no PDF /Encoding still carries its own code→glyph
	// mapping; without it the code cannot be resolved and callers fall back
	// to an identity GID that reads the wrong glyph's metrics.
	if fontFile3Obj, ok := fd["FontFile3"]; ok {
		stream, err := document.ResolveStream(ctx, doc, fontFile3Obj)
		if err == nil {
			data, err := decodeFontProgram(ctx, doc, fontFile3Obj, stream)
			if err == nil {
				if enc := typography.ExtractCFFEncodingNames(data); len(enc) > 0 {
					return enc
				}
			}
		}
	}

	return nil
}

// differencesGlyphNames returns the charCode → glyphName overrides an
// /Encoding dictionary declares through its /Differences array. Per ISO 32000-1
// 9.6.6.1, each number in the array starts a run of names at that code.
func differencesGlyphNames(ctx context.Context, doc *document.Document, encDict ast.PdfDict) map[byte]string {
	diffArr, err := document.ResolveArray(ctx, doc, encDict["Differences"])
	if err != nil {
		return nil
	}
	// No capacity hint: the byte-keyed map holds at most 256 entries however
	// long the file-supplied array is.
	result := make(map[byte]string)
	code := 0
	for _, item := range diffArr {
		switch v := item.(type) {
		case ast.PdfInt:
			code = int(v)
		case ast.PdfName:
			if code >= 0 && code <= 255 {
				result[byte(code)] = string(v) //nolint:gosec // G115: code is bounded to 0..255 by the guard above.
			}
			code++
		}
	}
	return result
}

// winAnsiBulletFloor is the highest WinAnsiEncoding code that stays undefined
// when the table leaves it unused. Per ISO 32000-1, Annex D.2, every unused
// code above octal 40 maps to the bullet character instead.
const winAnsiBulletFloor = 0o40

// annexDGlyphNames holds the codes whose ISO 32000-1, Annex D.2 glyph name is
// not the Adobe Glyph List name for the Unicode value the same table assigns
// the code. Annex D defines the predefined encodings by glyph name, so
// recovering a name by round-tripping the code's rune through
// [encodings.RuneToGlyph] is only a shortcut: it agrees for every other code
// but yields a synthetic uniXXXX name here, which no font charset carries.
// WinAnsiEncoding encodes SPACE at 240 and HYPHEN at 255 on top of their usual
// codes, and MacRomanEncoding names its mathematical and symbol slots. The
// values match veraPDF's own table (org.verapdf.pd.font.truetype
// .TrueTypePredefined) glyph for glyph.
var annexDGlyphNames = map[string]map[byte]string{
	"WinAnsiEncoding": {
		0o240: "space",
		0o255: "hyphen",
	},
	"MacRomanEncoding": {
		0o255: "notequal",
		0o260: "infinity",
		0o262: "lessequal",
		0o263: "greaterequal",
		0o266: "partialdiff",
		0o267: "summation",
		0o270: "product",
		0o271: "pi",
		0o272: "integral",
		0o275: "Omega",
		0o303: "radical",
		0o305: "approxequal",
		0o306: "Delta",
		0o312: "space",
		0o327: "lozenge",
		0o333: "currency",
		0o360: "apple",
	},
}

// predefinedEncodingNames returns a charCode → glyphName mapping for a
// named PDF encoding.
func predefinedEncodingNames(name string) map[byte]string {
	switch name {
	case "WinAnsiEncoding":
		return encodingGlyphNames(name, encodings.WinAnsiEncoding)
	case "MacRomanEncoding":
		return encodingGlyphNames(name, encodings.MacRomanEncoding)
	case "MacExpertEncoding":
		return encodingGlyphNames(name, encodings.MacExpertEncoding)
	case "StandardEncoding":
		// ISO 32000-2, Table 112 lists only the three above as /Encoding names,
		// but producers write StandardEncoding too, and Table 114 makes it the
		// implicit base for a non-symbolic font with no built-in encoding, so
		// the table has to be reachable by name.
		return encodingGlyphNames(name, encodings.StandardEncoding)
	}
	return nil
}

// encodingGlyphNames converts one of Annex D's encoding tables from the runes
// litho stores it as into the glyph names the encoding actually assigns.
func encodingGlyphNames(name string, table map[byte]rune) map[byte]string {
	result := make(map[byte]string, len(table))
	for code, r := range table {
		if glyphName := encodings.RuneToGlyph(r); glyphName != "" {
			result[code] = glyphName
		}
	}
	maps.Copy(result, annexDGlyphNames[name])
	// Per ISO 32000-1, Annex D.2, all unused WinAnsiEncoding codes greater than
	// octal 40 map to the bullet character. Reading those codes as undefined
	// reported a .notdef reference where the encoding in fact selects a glyph.
	// The rule is specific to WinAnsiEncoding: MacRomanEncoding and
	// MacExpertEncoding leave their unused codes undefined.
	if name == "WinAnsiEncoding" {
		for code := winAnsiBulletFloor + 1; code <= 0xFF; code++ {
			if _, ok := result[byte(code)]; !ok {
				result[byte(code)] = "bullet"
			}
		}
	}
	return result
}

// checkToUnicode validates the ToUnicode mapping requirement (rule 2).
//
// # Corpus evidence
//
//   - govdocs thread_100_100601.pdf: shows code 1 of an embedded
//     MHHIBN+ZapfDingbats named /a73 with no /ToUnicode. veraPDF reports that
//     glyph at PDF/A-1a and passes it at PDF/A-2u, where the rule runs and
//     succeeds rather than going unapplied (142 passed rules against 121 at
//     1a). It backs the Part 1 only ZapfDingbats carve-out.
//   - All corpora: extending the ZapfDingbats carve-out to Parts 2 and later
//     reported four files veraPDF passes.
//   - pdfjs bug1727053.pdf: shows an unembedded Arial,Bold with no /ToUnicode,
//     and veraPDF reports neither 7.21.7 nor 8.4.5.8. It backs extending the
//     glyph-name exemption to TrueType.
//   - govdocs: veraPDF reports 16 files whose TrueType names /WinAnsiEncoding
//     or /MacRomanEncoding and shows codes the program leaves unmapped. These
//     files motivated the /Encoding dictionary restriction. All 16 show codes
//     /Differences never names, and they take the /BaseEncoding route, so they
//     never reach the StandardEncoding fallback.
//   - pdfjs issue8613.pdf: shows two unembedded MSSansSerif fonts whose
//     /Differences names the whole range, and veraPDF passes them.
//   - govdocs thread_300_300859.pdf: shows an embedded ZJLCSM+Symbol whose
//     only code is named /bullet, and veraPDF passes it.
//   - pdfjs bug1727053.pdf: shows 27 codes of an embedded Arial,Bold, 23 of
//     them named by the StandardEncoding base rather than by its 88-entry
//     /Differences. veraPDF passes all 27 (passedChecks=27) while litho
//     reported the font at PDF/A-2u, 2a, 3a and 3u and stayed silent at
//     PDF/UA-1 and UA-2. It backs not scoping the TrueType disjunct to PDF/UA.
func checkToUnicode(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, usage *fontUsageInfo) []Violation {
	rule := toUnicodeRule(profile)
	if rule.Specification == "" {
		return nil
	}

	// Type3 fonts: PDF/A-4 tolerates a missing /ToUnicode map, but PDF/UA
	// does not. ISO 14289-1 §7.21.7 and ISO 14289-2 §8.4.5.8 require every
	// glyph to map to a Unicode value, so a Type 3 glyph with no resolvable
	// Unicode fails under PDF/UA. PDF/A parts 1-3 (levels a/u) and PDF/UA
	// require either AGL-resolvable Differences names or a /ToUnicode CMap.
	//
	// Parts 2/3 §6.2.11.7.2 and PDF/UA accept any glyph-level Unicode
	// mechanism, including AGL-resolvable /Differences names, in place of a
	// /ToUnicode entry. PDF/A-1 §6.3.8 t1 is stricter: it requires a /ToUnicode
	// CMap entry specifically (its only exemptions are predefined encodings,
	// standard-Latin Type 1, and Adobe-CID Type 0, none of which a Type 3 font
	// meets), so an AGL-resolvable /Differences set does not substitute there.
	if isType3Font(ctx, doc, fontDict) {
		return checkType3ToUnicode(ctx, doc, fontDict, fontName, pageNum, profile, rule, usage)
	}

	// Exemption (a): standard encoding with Adobe Glyph List coverage.
	// This covers MacRomanEncoding, WinAnsiEncoding, and MacExpertEncoding.
	if hasStandardEncoding(ctx, doc, fontDict) {
		return nil
	}

	// Exemption (a) extended: encoding dictionary with standard BaseEncoding.
	if hasStandardBaseEncoding(ctx, doc, fontDict, usage) {
		return nil
	}

	// Exemption (b): symbolic font with single cmap and an explicit encoding.
	// Symbolic fonts are only exempt when they define a character mapping via
	// /Encoding; without it, /ToUnicode is still required. The mapping must also
	// actually resolve to Unicode: a symbolic font whose /Differences remap the
	// used codes to glyph names with no Unicode value (neither AGL- nor
	// Symbol-encoding-resolvable, e.g. MathematicalPi's "H20850") has no Unicode
	// mapping at all, which veraPDF reports per glyph. Codes left to the font's
	// built-in encoding are skipped by [usedGlyphNamesResolvable] (litho cannot
	// parse them), so this stays free of false positives; usage-less checks stay
	// exempt for the same reason. A /ToUnicode CMap vouches only for the codes it
	// maps, see [shownCodeOutsideToUnicode].
	// ZapfDingbats earns no name-based exemption, for the reason
	// [allGlyphNamesAGLResolvable] states: its glyphs are named a1 to a191, and
	// ISO 32000-1, 9.10.2 admits "the Adobe standard Latin character set or the
	// set of named characters in the Symbol font" and no third set, so those
	// names carry no Unicode value here. The Adobe Glyph List does map them,
	// which is why [glyphNameHasUnicode] answers true and every name survey
	// below would pass the font. [allGlyphNamesAGLResolvable] already excluded
	// the standard 14 spelling; an embedded subset reaches the surveys below
	// instead.
	//
	// Part 1 only, and measured rather than assumed: veraPDF answers the very
	// same test (`toUnicode != null` on a Glyph) differently by flavour,
	// reporting such a glyph at PDF/A-1a and passing it at PDF/A-2u, where the
	// rule runs and succeeds rather than going unapplied. Parts 2 and later
	// evidently admit the Adobe Glyph List entry that 9.10.2 does not, so the
	// exemption stands there.
	dingbats := PDFAPart(profile) == 1 && isZapfDingbatsByName(ctx, doc, fontDict)

	fd := document.ResolveFontDescriptor(ctx, doc, fontDict)
	if isSymbolicFont(ctx, doc, fd) && !dingbats {
		if _, hasEncoding := fontDict["Encoding"]; hasEncoding {
			if usage == nil || usedGlyphNamesResolvable(ctx, doc, fontDict, usage) {
				return nil
			}
			if hasToUnicode(ctx, doc, fontDict) {
				if len(shownCodesOutsideToUnicode(ctx, doc, fontDict, usage)) == 0 {
					return nil
				}
			}
		}
	}

	// Exemption (b2): a simple font whose character names are all resolvable
	// via the Adobe Glyph List. Per §6.3.8, if every glyph name in the
	// font's encoding (including /Differences) maps to a Unicode value
	// through the AGL, ToUnicode is not required.
	//
	// ISO 19005-1, 6.3.8 words its exemption for Type 1 fonts, but ISO 14289-1,
	// 7.21.7 and ISO 14289-2, 8.4.5.8 ask only that each shown glyph carry a
	// Unicode value, which ISO 32000-1, 9.10.2 derives from the glyph name
	// whatever the font's subtype. veraPDF computes that value per Glyph and so
	// passes a TrueType whose names resolve.
	//
	// A TrueType earns it only through an /Encoding dictionary, which states its
	// glyph names outright. ISO 32000-1, 9.6.6.4 resolves a TrueType code
	// through the font's own cmap, so a name a predefined table merely implies
	// yields Unicode only when the program actually carries that glyph, which
	// this check does not establish. veraPDF, which reads the cmap, still
	// reports a TrueType that names /WinAnsiEncoding or /MacRomanEncoding and
	// shows codes the program leaves unmapped.
	subtype := fontSubtype(ctx, doc, fontDict)

	// A /Differences array that names every code the font shows is the one
	// TrueType case the paragraph above does not argue against: the names are
	// stated outright rather than implied by a predefined table, so ISO 32000-1,
	// 9.10.2 derives Unicode from them without reading the program's cmap. The
	// restriction is aimed at codes /Differences never names. veraPDF asks
	// Glyph.toUnicode != null (the PDFA-1A profile's own test for 6.3.8 t1) and
	// so passes such a font, embedded or not.
	if subtype == "TrueType" && fontEncodingIsDict(ctx, doc, fontDict) &&
		shownCodesNamedByResolvableDifferences(ctx, doc, fontDict, usage) {
		return nil
	}

	// The TrueType disjunct is not scoped to PDF/UA. ISO 19005-2, 6.2.11.7.2,
	// ISO 14289-1, 7.21.7 and ISO 14289-2, 8.4.5.8 all ask the same thing, that
	// every shown code carry a Unicode value, and veraPDF answers it the same
	// way in every flavour: Glyph.toUnicode != null. Holding the A profiles to
	// /Differences alone reported a font whose remaining codes resolve through
	// the base encoding, which [resolveSimpleFontEncoding] derives for exactly
	// this shape (an embedded non-symbolic TrueType whose /Differences state no
	// /BaseEncoding takes StandardEncoding, ISO 32000-1, 9.6.6.4 and Table 114).
	//
	// The restriction the exemption above states still holds, because the base
	// encoding is only derived where its names are earned: that fallback is
	// gated on a non-symbolic font that is not ZapfDingbats and either embeds a
	// program or is one of the standard 14. A TrueType that names
	// /WinAnsiEncoding or /MacRomanEncoding takes the /BaseEncoding route and
	// never reaches it.
	//
	// The TrueType disjunct admits only a font whose shown codes were recorded.
	// [allGlyphNamesAGLResolvable] falls back to surveying the names the font
	// mentions when usage is empty, and that survey answers a different
	// question: /Differences naming a handful of codes says nothing about the
	// rest, so a font litho could not collect usage for would earn the
	// exemption on a name list alone.
	trueTypeWithUsage := subtype == "TrueType" && usage != nil && len(usage.charCodes) > 0 &&
		fontEncodingIsDict(ctx, doc, fontDict)

	if !dingbats && (subtype == "Type1" || subtype == "MMType1" || trueTypeWithUsage) {
		if allGlyphNamesAGLResolvable(ctx, doc, fontDict, usage) {
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

	// Exemption (c): Identity-H/Identity-V, only exempt for PDF/A, not PDF/UA.
	// For PDF/UA, a ToUnicode CMap is required for Identity-encoded Type0 fonts
	// unless glyph-level Unicode mapping is available through other means.
	if hasIdentityCMap(ctx, doc, fontDict) && !IsPDFUA(profile) {
		return nil
	}

	if !hasToUnicode(ctx, doc, fontDict) {
		return missingUnicodeFaults(rule, fontName, pageNum, shownCodesWithoutUnicodeName(ctx, doc, fontDict, usage))
	}
	faults := newGlyphFaults(rule)
	for _, out := range shownCodesOutsideToUnicode(ctx, doc, fontDict, usage) {
		if out.name == "" {
			faults.add(uint32(out.code), "font %s on page %d: code %d is outside /ToUnicode and the font embeds no program to name it",
				fontName, pageNum, out.code)
			continue
		}
		faults.add(uint32(out.code), "font %s on page %d: code %d is outside /ToUnicode and its glyph name %s has no Unicode value",
			fontName, pageNum, out.code, out.name)
	}
	return faults.violations()
}

// missingUnicodeFaults states the ToUnicode presence rule for a font that gives
// the codes it shows no /ToUnicode mapping: once for each code in codes, the
// shown codes left without a Unicode value, and once for the font, naming no
// glyph, when codes is empty. The rule judges each glyph (veraPDF's Glyph test
// toUnicode != null), but an empty list means the verdict rests on the font
// as a whole, through an exemption it failed, rather than on codes litho can
// name.
func missingUnicodeFaults(rule Rule, fontName string, pageNum int, codes []byte) []Violation {
	if len(codes) == 0 {
		return failf(rule, "font %s on page %d", fontName, pageNum)
	}
	faults := newGlyphFaults(rule)
	for _, c := range codes {
		faults.add(uint32(c), "font %s on page %d: code %d has no Unicode value", fontName, pageNum, c)
	}
	return faults.violations()
}

// shownCodesWithoutUnicodeName returns, sorted, the codes a simple font shows
// whose encoding gives them no glyph name a Unicode value derives from (ISO
// 32000-1, 9.10.2). A code named .notdef is left out: the glyph lists map it
// to U+0000, which the value rule judges, so it has a Unicode value.
func shownCodesWithoutUnicodeName(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, usage *fontUsageInfo) []byte {
	if usage == nil || len(usage.charCodes) == 0 {
		return nil
	}
	enc := resolveSimpleFontEncoding(ctx, doc, fontDict)
	var out []byte
	for _, c := range sortedByteKeys(usage.charCodes) {
		name := enc[c]
		if name == ".notdef" {
			continue
		}
		if name == "" || (!glyphNameHasUnicode(name) && !symbolEncodingNames[name]) {
			out = append(out, c)
		}
	}
	return out
}

// outsideCode is a shown code a /ToUnicode CMap leaves out, with the glyph name
// the encoding gives it, empty when it gives none.
type outsideCode struct {
	code byte
	name string
}

// shownCodesOutsideToUnicode returns, sorted, the codes a Type 1 font shows
// that its /ToUnicode CMap leaves out and its encoding names with a glyph name
// carrying no Unicode value.
//
// ISO 14289-1, 7.21.7 and ISO 19005-2, 6.2.11.7.2 ask that every shown code
// map to Unicode. A /ToUnicode CMap vouches only for the codes it maps: ISO
// 32000-1, 9.10.2 derives the value of any other code from its glyph name, and
// veraPDF's PDSimpleFont.toUnicode falls back the same way.
//
// A code left unnamed or named .notdef is skipped, because veraPDF then reads
// the program's own glyph name, which this check does not establish. A CMap
// that cannot be read is skipped too, leaving the font to the presence rule.
//
// # Corpus evidence
//
//   - govdocs thread_300_300068.pdf: shows code 2 of an MTSY font whose
//     /Differences name it circlecopyrt, a TeX spelling no glyph list carries,
//     while its /ToUnicode maps codes 1 and 3 only, and veraPDF reports the
//     glyph.
//   - pdfjs bug864847.pdf: shows an unembedded symbolic Arial-ItalicMT with no
//     /Encoding whose /ToUnicode maps only <20>, and veraPDF reports every
//     other code. It backs reporting an unnamed code when the font embeds no
//     program.
func shownCodesOutsideToUnicode(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, usage *fontUsageInfo) []outsideCode {
	if usage == nil || len(usage.charCodes) == 0 {
		return nil
	}
	// A font that embeds no program carries no built-in glyph names to fall back
	// on, so a code its encoding leaves unnamed has no Unicode source at all once
	// the /ToUnicode CMap omits it. That case is judged for a TrueType too; the
	// named-but-unresolvable case below stays Type 1 only, because a TrueType
	// resolves its codes through the program's own cmap (ISO 32000-1, 9.6.6.4).
	embedded := embedsFontProgram(ctx, doc, fontDict)
	st := fontSubtype(ctx, doc, fontDict)
	unembeddedTrueType := st == "TrueType" && !embedded
	if st != "Type1" && st != "MMType1" && !unembeddedTrueType {
		return nil
	}
	tuObj, ok := fontDict["ToUnicode"]
	if !ok {
		return nil
	}
	stream, err := document.ResolveStream(ctx, doc, tuObj)
	if err != nil || stream.Dict == nil {
		doc.Logger.Debug("compliance: fonts: ToUnicode is not a stream", "error", err)
		return nil
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		doc.Logger.Debug("compliance: fonts: failed to decode ToUnicode CMap", "error", err)
		return nil
	}
	parsed, err := cmap.Parse(ctx, data)
	if err != nil || parsed == nil || len(parsed.Mapping) == 0 {
		doc.Logger.Debug("compliance: fonts: failed to parse ToUnicode CMap", "error", err)
		return nil
	}

	mapped := cmapSourceCodeValues(parsed)

	enc := resolveSimpleFontEncoding(ctx, doc, fontDict)
	var out []outsideCode
	for _, c := range sortedByteKeys(usage.charCodes) {
		if mapped[int(c)] {
			continue
		}
		name := enc[c]
		if name == "" || name == ".notdef" {
			// veraPDF reads the program's own glyph name here, which this check
			// does not establish, so a named program keeps its exemption. With
			// no program there is nothing left to read.
			if !embedded {
				out = append(out, outsideCode{code: c})
			}
			continue
		}
		if glyphNameHasUnicode(name) || symbolEncodingNames[name] {
			continue
		}
		out = append(out, outsideCode{code: c, name: name})
	}
	return out
}

// cmapSourceCodeValues returns the integer value of every source code a parsed
// /ToUnicode CMap maps; see [sourceCodeValues].
func cmapSourceCodeValues(parsed *cmap.CMap) map[int]bool {
	return sourceCodeValues(parsed.Mapping)
}

// symbolicCmapViolation reports whether a symbolic TrueType font's cmap
// subtable set violates the conformance level's requirement. The permitted
// sets differ by part, mirroring the veraPDF test expressions exactly:
//
//   - PDF/A-1 §6.3.7 t3:              exactly one subtable.
//   - PDF/A-2, 3 §6.2.11.6 t4 and
//     PDF/UA-1 §7.21.6 t4:            one subtable, or a Microsoft Symbol (3,0).
//   - PDF/A-4 §6.2.10.6 t4 and
//     PDF/UA-2 §8.4.5.7 t4:           a Microsoft Symbol (3,0) or a Mac (1,0).
func symbolicCmapViolation(profile string, subs []truetype.CmapSubtable) bool {
	n := len(subs)
	var cmap30, cmap10 bool
	for _, s := range subs {
		switch {
		case s.PlatformID == 3 && s.EncodingID == 0:
			cmap30 = true
		case s.PlatformID == 1 && s.EncodingID == 0:
			cmap10 = true
		}
	}
	if IsPDFUA(profile) {
		if PDFUAPart(profile) == 2 {
			return !cmap30 && !cmap10
		}
		return n != 1 && !cmap30
	}
	switch PDFAPart(profile) {
	case 1:
		return n != 1
	case 4:
		return !cmap30 && !cmap10
	default:
		return n != 1 && !cmap30
	}
}

// nonSymbolicDifferencesProblem reports why a non-symbolic TrueType font's
// /Differences array may not stand, or "" when it may. ISO 19005-2, 6.2.11.6
// (and ISO 19005-4, 6.2.10.6, ISO 14289-1, 7.21.6, ISO 14289-2, 8.4.5.7) allow
// the array only when "all of the glyph names in the Differences array are
// listed in the Adobe Glyph List and the embedded font program contains at
// least the Microsoft Unicode (3,1 – Platform ID=3, Encoding ID=1) encoding in
// the 'cmap' table".
//
// Listed means the published list, which veraPDF's AdobeGlyphList holds with
// .notdef: a uniXXXX name, which the glyph list conventions derive rather than
// list, does not qualify, and veraPDF reports it. A font with no embedded
// program is left to the embedding rule, as veraPDF leaves it, and a program
// whose table directory will not parse is not held to the subtable half, since
// whether it carries the subtable is unknown.
//
// # Corpus evidence
//
//   - pdfjs issue13626.pdf: names uni0453 in /Differences, and veraPDF reports
//     it.
func nonSymbolicDifferencesProblem(ctx context.Context, doc *document.Document, fd ast.PdfDict, differences ast.PdfArray) string {
	fontData := extractFontProgramData(ctx, doc, fd)
	if fontData == nil {
		return ""
	}
	subs, err := truetype.CmapSubtables(fontData)
	switch {
	case errors.Is(err, truetype.ErrTableNotFound):
		return "Differences array over a program with no cmap table"
	case err == nil && !slices.Contains(subs, truetype.CmapSubtable{PlatformID: 3, EncodingID: 1}):
		return "Differences array over a program with no Microsoft Unicode (3,1) cmap subtable"
	}
	for _, item := range differences {
		name, ok := item.(ast.PdfName)
		if !ok || name == ".notdef" {
			continue
		}
		if !encodings.InAdobeGlyphList(string(name)) {
			return fmt.Sprintf("Differences glyph %q not in Adobe Glyph List", string(name))
		}
	}
	return ""
}

// nonSymbolicCmapViolation reports whether a non-symbolic TrueType font's cmap
// subtable set violates the conformance level's requirement. The permitted sets
// differ by part, mirroring the veraPDF test expressions exactly:
//
//   - PDF/A-1:                        no such requirement at all.
//   - PDF/A-2, 3 §6.2.11.6 t1 and
//     PDF/UA-1 §7.21.6 t1:            at least one subtable, and more than one
//     when a Microsoft Symbol (3,0) is present, since a symbol subtable alone
//     carries no non-symbolic lookup.
//   - PDF/A-4 §6.2.10.6 t1 and
//     PDF/UA-2 §8.4.5.7 t1:           a Microsoft Unicode (3,1) or a Macintosh
//     Roman (1,0) subtable, irrespective of the subtable count.
func nonSymbolicCmapViolation(profile string, subs []truetype.CmapSubtable) bool {
	var cmap30, cmap31, cmap10 bool
	for _, s := range subs {
		switch {
		case s.PlatformID == 3 && s.EncodingID == 0:
			cmap30 = true
		case s.PlatformID == 3 && s.EncodingID == 1:
			cmap31 = true
		case s.PlatformID == 1 && s.EncodingID == 0:
			cmap10 = true
		}
	}
	if IsPDFUA(profile) {
		if PDFUAPart(profile) == 2 {
			return !cmap31 && !cmap10
		}
	} else if PDFAPart(profile) == 4 {
		return !cmap31 && !cmap10
	}
	if cmap30 {
		return len(subs) <= 1
	}
	return len(subs) == 0
}

// checkTrueTypeEncoding validates TrueType-specific encoding rules (rule 7).
//
// # Corpus evidence
//
//   - pdfjs TrueType_without_cmap.pdf: embeds a non-symbolic TrueType whose
//     program has no cmap table, and veraPDF reports it.
//   - pdfjs issue215.pdf: names no /BaseEncoding and lists small-cap names
//     outside the Adobe Glyph List, and veraPDF counts one violation.
func checkTrueTypeEncoding(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string) []Violation {
	var results []Violation

	fd := document.ResolveFontDescriptor(ctx, doc, fontDict)
	symbolic := isSymbolicFont(ctx, doc, fd)

	if symbolic {
		// Symbolic TrueType must not specify /Encoding.
		rule := trueTypeEncodingSymbolicNoEncodingRule(profile)
		if rule.Specification != "" {
			if _, hasEnc := fontDict["Encoding"]; hasEnc {
				results = append(results, failf(rule, "font %s on page %d", fontName, pageNum)...)
			}
		}

		// The permitted cmap subtable set for a symbolic TrueType font depends
		// on the conformance level. PDF/A-1 §6.3.7 t3 requires exactly one
		// encoding; PDF/A-2 and 3 §6.2.11.6 t4 (and PDF/UA-1 §7.21.6 t4) also
		// accept any font whose cmap carries a Microsoft Symbol (3,0) encoding;
		// PDF/A-4 §6.2.10.6 t4 (and PDF/UA-2 §8.4.5.7 t4) accept a (3,0) or a
		// Macintosh (1,0) encoding irrespective of the subtable count.
		cmapRule := trueTypeSymbolicCmapRule(profile)
		if cmapRule.Specification != "" {
			if fontData := extractFontProgramData(ctx, doc, fd); len(fontData) > 0 {
				if subs, err := truetype.CmapSubtables(fontData); err == nil && symbolicCmapViolation(profile, subs) {
					results = append(results, failf(cmapRule, "font %s on page %d: symbolic TrueType cmap subtable set is not permitted (%d subtables)", fontName, pageNum, len(subs))...)
				}
			}
		}
	} else {
		// The permitted cmap subtable set for a non-symbolic TrueType font
		// depends on the conformance level the same way the symbolic set does.
		// PDF/A-1 states no such requirement, so the rule is empty there.
		if cmapRule := trueTypeNonSymbolicCmapRule(profile); cmapRule.Specification != "" {
			if fontData := extractFontProgramData(ctx, doc, fd); len(fontData) > 0 {
				subs, err := truetype.CmapSubtables(fontData)
				switch {
				case errors.Is(err, truetype.ErrTableNotFound):
					// The rule asks the program to carry "at least" a (3,1) or
					// a (1,0) subtable, so a program with no cmap table at all
					// fails it outright. Reading only the parsed-subtable set
					// let that case pass silently, since the missing table is
					// an error rather than an empty set, and veraPDF reports it.
					results = append(results, failf(cmapRule, "font %s on page %d: non-symbolic TrueType program carries no cmap table", fontName, pageNum)...)
				case err == nil && nonSymbolicCmapViolation(profile, subs):
					results = append(results, failf(cmapRule, "font %s on page %d: non-symbolic TrueType cmap subtable set is not permitted (%d subtables)", fontName, pageNum, len(subs))...)
				}
			}
		}

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
					// encName is empty. Fall through to check encoding dictionary.
					// Encoding is a dictionary. Check BaseEncoding.
					encDict, err := document.ResolveDict(ctx, doc, encObj)
					if err != nil || encDict == nil {
						results = append(results, failf(rule, "font %s on page %d has invalid Encoding type", fontName, pageNum)...)
					} else {
						// veraPDF states the rule as one test per font, so a font whose
						// base encoding and /Differences both fail it is reported once.
						baseEnc, _ := document.ResolveName(ctx, doc, encDict["BaseEncoding"])
						if baseEnc != "MacRomanEncoding" && baseEnc != "WinAnsiEncoding" {
							results = append(results, failf(rule, "font %s on page %d BaseEncoding=%s", fontName, pageNum, baseEnc)...)
						} else if diffObj, hasDiff := encDict["Differences"]; hasDiff {
							// Part 1: Differences is forbidden entirely. Parts 2+ allow
							// it on the terms [nonSymbolicDifferencesProblem] checks.
							if PDFAPart(profile) == 1 {
								results = append(results, failf(rule, "font %s on page %d has Differences in encoding dict", fontName, pageNum)...)
							} else if diffArr, diffErr := document.ResolveArray(ctx, doc, diffObj); diffErr == nil {
								if problem := nonSymbolicDifferencesProblem(ctx, doc, fd, diffArr); problem != "" {
									results = append(results, failf(rule, "font %s on page %d: %s", fontName, pageNum, problem)...)
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
// usage carries the codes the page shows through the font, or nil when unknown.
//
// # Corpus evidence
//
//   - pdfjs issue18466.pdf: shows 16 glyphs of a FZSSK--GBK1-0 encoded
//     UniGB-UTF16-H, the case the part 1 CJK exemption lapses for.
//   - pdfjs issue8061.pdf: ships 345 bytes of binary data as its /ToUnicode,
//     and veraPDF reports each glyph.
func checkType0ToUnicode(ctx context.Context, doc *document.Document, type0Dict, cidFontDict ast.PdfDict, fontName string, pageNum int, profile string, usage *fontUsageInfo) []Violation {
	rule := toUnicodeRule(profile)
	if rule.Specification == "" {
		return nil
	}

	// Exemption: known CJK character collections. A collection alone does not
	// carry it under part 1: the Unicode value comes from the CMap that reaches
	// the collection, and ISO 19005-1 is written against PDF 1.4, which
	// predefines no UTF-8, UTF-16 or UTF-32 CMap (see
	// [cmapNameIsUnicodeTransformFormat]). With one of those names the font
	// reaches its collection through a CMap the profile does not define, so no
	// Unicode value follows and the exemption lapses.
	//
	// Measured against veraPDF at PDF/A-1a over an Adobe-GB1 CIDFont with no
	// /ToUnicode: GBK-EUC-H and UniGB-UCS2-H report nothing, UniGB-UTF16-H
	// reports the glyph.
	cidSysInfo, err := document.ResolveDict(ctx, doc, cidFontDict["CIDSystemInfo"])
	if err == nil && cidSysInfo != nil {
		ordering, _ := document.ResolveString(ctx, doc, cidSysInfo["Ordering"])
		registry, _ := document.ResolveString(ctx, doc, cidSysInfo["Registry"])
		encName, _ := document.ResolveName(ctx, doc, type0Dict["Encoding"])
		undefinedHere := PDFAPart(profile) == 1 && cmapNameIsUnicodeTransformFormat(string(encName))
		if string(registry) == "Adobe" && !undefinedHere {
			switch string(ordering) {
			case "GB1", "CNS1", "Japan1", "Korea1":
				return nil
			}
		}
	}

	// With no map at all, or one that maps nothing, every glyph the font shows
	// goes without a Unicode value, and each is stated once; see
	// [fontJudgements.glyphs]. Codes litho cannot decode leave one statement
	// for the font.
	if !hasToUnicode(ctx, doc, type0Dict) {
		return type0MissingUnicode(ctx, doc, type0Dict, rule, usage, "font %s on page %d", fontName, pageNum)
	}

	// ISO 14289-1 §7.21.7 and ISO 19005-2 §6.2.11.7.2 ask for a ToUnicode CMap
	// that maps the codes a glyph shows, and ISO 32000-1 §9.10.3 makes the entry
	// a CMap stream. A stream that does not parse as one maps no code, so every
	// glyph the font shows goes without a Unicode value, and veraPDF reports each
	// glyph. A font no glyph uses is not judged, since both rules are scoped to
	// shown glyphs.
	if usage != nil && len(usage.charCodes) > 0 && toUnicodeMapsNothing(ctx, doc, type0Dict["ToUnicode"]) {
		return type0MissingUnicode(ctx, doc, type0Dict, rule, usage, "font %s on page %d: /ToUnicode is not a CMap mapping any code", fontName, pageNum)
	}

	return nil
}

// type0MissingUnicode states rule once for every CID a composite font's shown
// codes select, with the context format renders, or once for the font when
// those CIDs cannot be decoded.
func type0MissingUnicode(ctx context.Context, doc *document.Document, type0Dict ast.PdfDict, rule Rule, usage *fontUsageInfo, format, fontName string, pageNum int) []Violation {
	cids := type0UsedCIDs(ctx, doc, type0Dict, usage)
	if len(cids) == 0 {
		return failf(rule, format, fontName, pageNum)
	}
	faults := newGlyphFaults(rule)
	for _, cid := range sortedCIDs(cids) {
		faults.add(uint32(cid), format, fontName, pageNum)
	}
	return faults.violations()
}

// toUnicodeMapsNothing reports whether a /ToUnicode entry is a stream that does
// not read as a CMap mapping any code. An entry that is not a stream, such as the
// name /Identity-H some producers write, and a stream litho cannot read at all
// are left to other rules. A CMap carrying a token its syntax cannot produce
// maps nothing whatever its entries say: see [cmap.CMap.Malformed].
func toUnicodeMapsNothing(ctx context.Context, doc *document.Document, tuObj ast.PdfObject) bool {
	stream, err := document.ResolveStream(ctx, doc, tuObj)
	if err != nil || stream.Dict == nil {
		return false
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		if data, err = stream.Bytes(ctx); err != nil {
			return false
		}
	}
	parsed, err := cmap.Parse(ctx, data)
	return err != nil || parsed == nil || len(parsed.Mapping) == 0 || parsed.Malformed()
}

// checkToUnicodeCMapValues validates that a readable /ToUnicode CMap does not
// map to invalid Unicode values (U+0000, U+FEFF, U+FFFE).
// Per ISO 19005-4 §6.2.10.7, the requirement applies to the Unicode strings of
// glyphs that are actually shown: the destination of a CMap entry behind a
// character code that is never rendered cannot affect the displayed text, so it
// passes. usage carries the rendered character codes; with no recorded usage no
// glyph is shown and the check is vacuous. A /ToUnicode that cannot be read at
// all specifies no value and is likewise vacuous here.
func checkToUnicodeCMapValues(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, usage *fontUsageInfo) []Violation {
	rule := toUnicodeCMapValuesRule(profile)
	if rule.Specification == "" {
		return nil
	}

	if usage == nil || len(usage.charCodes) == 0 {
		return nil
	}

	// With no /ToUnicode the glyph name is the only mapping the font offers, so
	// a rendered .notdef override has nothing to override it.
	tuObj, hasToUnicode := fontDict["ToUnicode"]
	if !hasToUnicode || resourceEntryNull(ctx, doc, tuObj) {
		return notdefUnicodeViolation(ctx, doc, fontDict, fontName, pageNum, rule, usage, nil)
	}

	// A /ToUnicode that cannot be read as a CMap specifies no Unicode value at
	// all, so the value constraint (ISO 14289-1 §7.21.7 t2, ISO 19005-2
	// §6.2.11.7.2 and its part-3/4 counterparts, every one worded "the Unicode
	// values specified in the ToUnicode CMap") is vacuous. Reporting it here
	// diagnoses the wrong rule: a font left without a usable mapping is the
	// presence rule's business. The common shape is a producer writing the CMap
	// *name* /Identity-H where ISO 32000-1 §9.10.3 prescribes a stream.
	stream, err := document.ResolveStream(ctx, doc, tuObj)
	if err != nil {
		doc.Logger.Debug("compliance: fonts: failed to resolve ToUnicode stream",
			"font", fontName, "error", err)
		return nil
	}
	if stream.Dict == nil {
		return nil
	}

	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		// Fall back to raw data.
		data, err = stream.Bytes(ctx)
		if err != nil || len(data) == 0 {
			doc.Logger.Debug("compliance: fonts: failed to decode ToUnicode CMap",
				"font", fontName, "error", err)
			return nil
		}
	}

	parsed, err := cmap.Parse(ctx, data)
	if err != nil || parsed == nil || len(parsed.Mapping) == 0 {
		// Unparseable CMap: fall back to the destination-only byte scan
		// over the whole map rather than silently passing.
		if containsInvalidUnicodeMapping(string(data)) {
			return failf(rule, "font %s on page %d: /ToUnicode CMap maps to U+0000, U+FEFF, or U+FFFE",
				fontName, pageNum)
		}
		return nil
	}

	// Every rendered code is its own glyph, stated once; see
	// [fontJudgements.glyphs]. A composite font's code is its CID under an
	// Identity CMap, which is the only case its glyph is named by.
	faults := newGlyphFaults(rule)
	for _, code := range slices.Sorted(maps.Keys(parsed.Mapping)) {
		if !codeUsed(usage, code) {
			continue
		}
		if strings.ContainsAny(parsed.Mapping[code], "\u0000\uFEFF\uFFFE") {
			var value uint32
			for i := range len(code) {
				value = value<<8 | uint32(code[i])
			}
			faults.add(value, "font %s on page %d: /ToUnicode maps a rendered code to U+0000, U+FEFF, or U+FFFE",
				fontName, pageNum)
		}
	}

	// A rendered code the CMap leaves alone falls back to its glyph name, so an
	// explicit .notdef override still resolves to U+0000. This is font-type
	// agnostic, unlike the name-absence reasoning below.
	for _, v := range notdefUnicodeViolation(ctx, doc, fontDict, fontName, pageNum, rule, usage, parsed.Mapping) {
		faults.add(v.glyph.code, "%s", v.Context)
	}

	// A used code that is absent from the ToUnicode CMap and carries no
	// encoding glyph name renders .notdef, whose implied Unicode value is
	// U+0000, a forbidden destination under ISO 19005-2 §6.2.11.7.2. A code
	// with a real glyph name is skipped: its Unicode is derived from the name
	// via the Adobe Glyph List, so no /ToUnicode entry is needed. Type0 fonts
	// are excluded; their unmapped codes fail the per-glyph coverage rule
	// (t1 family) instead.
	if st := fontSubtype(ctx, doc, fontDict); st == "Type1" || st == "MMType1" {
		enc := resolveSimpleFontEncoding(ctx, doc, fontDict)
		for _, c := range sortedByteKeys(usage.charCodes) {
			if _, mapped := parsed.Mapping[string([]byte{c})]; mapped {
				continue
			}
			// Mirror checkNotdefGlyph's encoding semantics: a missing
			// explicit entry only implies .notdef in the control range,
			// since standard encodings provide implicit glyph names for
			// printable codes (0x20 and above). Those names resolve to
			// Unicode through the Adobe Glyph List, so the code is defined
			// and must not fire the value rule.
			name := enc[c]
			if name != "" && name != ".notdef" {
				continue
			}
			if name == "" && c >= 0x20 {
				continue
			}
			// TrueType fonts never reach here: a symbolic TrueType font
			// carries no PDF-level glyph names, so an unmapped code has no
			// implied Unicode value at all (not even U+0000), and a missing
			// /ToUnicode is therefore conformant. Only the Type1 family's
			// name chain bottoms out at .notdef, whose value is U+0000.
			faults.add(uint32(c), "font %s on page %d: rendered code %d has no ToUnicode mapping and no glyph name (renders U+0000)",
				fontName, pageNum, c)
		}
	} else if st == "TrueType" {
		// A TrueType code absent from an explicit /Encoding renders .notdef
		// (PDF 9.6.6.4, mirroring checkNotdefGlyph), whose implied Unicode is
		// U+0000, a forbidden destination when no ToUnicode entry overrides it.
		// The len(enc)>0 guard restricts this to fonts with an explicit
		// /Encoding: a symbolic TrueType font has no /Encoding and thus no
		// implied Unicode, so a missing /ToUnicode is conformant.
		if enc := resolveSimpleFontEncoding(ctx, doc, fontDict); len(enc) > 0 {
			for _, c := range sortedByteKeys(usage.charCodes) {
				if _, mapped := parsed.Mapping[string([]byte{c})]; mapped {
					continue
				}
				if enc[c] == "" {
					faults.add(uint32(c), "font %s on page %d: rendered code %d has no ToUnicode mapping and is absent from the explicit /Encoding (renders U+0000)",
						fontName, pageNum, c)
				}
			}
		}
	}
	return faults.violations()
}

// codeUsed reports whether the raw character-code bytes appear in the
// page's text operators: single-byte codes check the byte set, two-byte
// codes check consecutive pairs of the raw string operands (the 2-byte
// code convention of the CID width and .notdef checks).
func codeUsed(usage *fontUsageInfo, code string) bool {
	switch len(code) {
	case 1:
		return usage.charCodes[code[0]]
	case 2:
		for _, s := range usage.rawStrings {
			b := []byte(s)
			for i := 0; i+1 < len(b); i += 2 {
				if b[i] == code[0] && b[i+1] == code[1] {
					return true
				}
			}
		}
	}
	return false
}

// containsInvalidUnicodeMapping scans CMap bfchar/bfrange sections for
// destination hex strings mapping to U+0000, U+FEFF, or U+FFFE.
// Only destination values are checked. Source codes may legitimately use
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

// puaSourceCodes parses a ToUnicode CMap and returns the set of source character
// codes whose Unicode destination holds a Private Use Area code point, which is
// veraPDF's per-glyph unicodePUA (PUAHelper.containPUA over the glyph's whole
// Unicode string). bfchar entries map a single code; a bfrange maps a
// contiguous code range either to incrementing destinations or, per ISO
// 32000-1, 9.10.3, to an array naming each code's destination in turn, so each
// code's own destination is tested.
func puaSourceCodes(cmapData string) map[uint32]bool {
	out := make(map[uint32]bool)
	upper := strings.ToUpper(cmapData)
	collectPUASources(upper, "BEGINBFCHAR", "ENDBFCHAR", out, false)
	collectPUASources(upper, "BEGINBFRANGE", "ENDBFRANGE", out, true)
	return out
}

func collectPUASources(s, begin, end string, out map[uint32]bool, isRange bool) {
	for {
		i := strings.Index(s, begin)
		if i < 0 {
			return
		}
		s = s[i+len(begin):]
		region := s
		j := strings.Index(s, end)
		if j >= 0 {
			region = s[:j]
			s = s[j+len(end):]
		}
		tokens := cmapEntryTokens(region)
		if isRange {
			collectPUARanges(tokens, out)
		} else {
			for k := 0; k+1 < len(tokens); k += 2 {
				if hexHoldsPUA(tokens[k+1]) {
					if src, ok := hexVal(tokens[k]); ok {
						out[src] = true
					}
				}
			}
		}
		if j < 0 {
			return
		}
	}
}

// collectPUARanges reads bfrange entries, "<lo> <hi> <dst>" or
// "<lo> <hi> [<dst0> <dst1> ...]", from tokens produced by [cmapEntryTokens].
func collectPUARanges(tokens []string, out map[uint32]bool) {
	for k := 0; k+2 < len(tokens); {
		lo, okLo := hexVal(tokens[k])
		hi, okHi := hexVal(tokens[k+1])
		if !okLo || !okHi || hi < lo || hi-lo > 0xFFFF {
			k++
			continue
		}
		if tokens[k+2] == "[" {
			k += 3
			for c := lo; k < len(tokens) && tokens[k] != "]"; c++ {
				if c <= hi && hexHoldsPUA(tokens[k]) {
					out[c] = true
				}
				k++
			}
			k++ // past "]"
			continue
		}
		if dst, ok := hexVal(tokens[k+2]); ok {
			for c := lo; c <= hi; c++ {
				if isPUAScalar(dst + (c - lo)) {
					out[c] = true
				}
			}
		}
		k += 3
	}
}

// cmapEntryTokens returns the angle-bracketed hex tokens in s together with the
// "[" and "]" that delimit a bfrange array destination, in order.
func cmapEntryTokens(s string) []string {
	var out []string
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '[', ']':
			out = append(out, s[i:i+1])
		case '<':
			j := strings.IndexByte(s[i:], '>')
			if j < 0 {
				return out
			}
			out = append(out, s[i:i+j+1])
			i += j
		}
	}
	return out
}

// hexHoldsPUA reports whether a hex destination token, read as UTF-16BE, holds
// any Private Use Area code point, surrogate pairs decoded first.
func hexHoldsPUA(token string) bool {
	t := strings.TrimSpace(token)
	if len(t) < 3 || t[0] != '<' || t[len(t)-1] != '>' {
		return false
	}
	hex := t[1 : len(t)-1]
	if len(hex)%4 != 0 {
		return isPUAHex(token)
	}
	units := make([]uint16, 0, len(hex)/4)
	for i := 0; i+4 <= len(hex); i += 4 {
		v, ok := hexVal("<" + hex[i:i+4] + ">")
		if !ok {
			return false
		}
		units = append(units, uint16(v)) //nolint:gosec // four hex digits fit in 16 bits.
	}
	for _, r := range utf16.Decode(units) {
		if isPUAScalar(uint32(r)) { //nolint:gosec // a decoded rune is non-negative.
			return true
		}
	}
	return false
}

// hexVal parses an angle-bracketed hex token like "<0041>" into its integer
// value, decoding a UTF-16BE surrogate pair to its supplementary scalar.
func hexVal(token string) (uint32, bool) {
	t := strings.TrimSpace(token)
	if len(t) < 3 || t[0] != '<' || t[len(t)-1] != '>' {
		return 0, false
	}
	hex := t[1 : len(t)-1]
	val := uint32(0)
	for _, c := range hex {
		val <<= 4
		switch {
		case c >= '0' && c <= '9':
			val |= uint32(c - '0') //nolint:gosec // c bounded by case guard.
		case c >= 'A' && c <= 'F':
			val |= uint32(c-'A') + 10 //nolint:gosec // c bounded by case guard.
		default:
			return 0, false
		}
	}
	if len(hex) == 8 {
		hi, lo := val>>16, val&0xFFFF
		if hi >= 0xD800 && hi <= 0xDBFF && lo >= 0xDC00 && lo <= 0xDFFF {
			val = 0x10000 + (hi-0xD800)<<10 + (lo - 0xDC00)
		}
	}
	return val, true
}

// isPUAScalar reports whether a Unicode scalar lies in a Private Use Area.
func isPUAScalar(val uint32) bool {
	return (val >= 0xE000 && val <= 0xF8FF) ||
		(val >= 0xF0000 && val <= 0xFFFFD) ||
		(val >= 0x100000 && val <= 0x10FFFD)
}

// containsPUAMapping scans a ToUnicode CMap for destination values in a Unicode
// Private Use Area: BMP (U+E000-U+F8FF), Supplementary PUA-A (U+F0000-U+FFFFD),
// or PUA-B (U+100000-U+10FFFD), including supplementary values written as
// UTF-16BE surrogate pairs. bfchar entries pair <src> <dst>; bfrange entries are
// <lo> <hi> <dst>. A single line may pack several entries, so every destination
// token on the line is tested (the earlier parts[1]-only scan missed packed
// fixtures where the PUA destination is not the second field).
func containsPUAMapping(cmapData string) bool {
	// A begin/end marker may share a line with its entries
	// ("... <06> <0066> endbfchar"), so scan whole begin..end regions rather
	// than line by line. In a bfchar region destinations are the second token
	// of each <src> <dst> pair; in a bfrange region they are the third token of
	// each <lo> <hi> <dst> triple.
	upper := strings.ToUpper(cmapData)
	return scanBfRegions(upper, "BEGINBFCHAR", "ENDBFCHAR", 2, 1) ||
		scanBfRegions(upper, "BEGINBFRANGE", "ENDBFRANGE", 3, 2)
}

// scanBfRegions reports whether any destination token in a begin..end region of
// s is a PUA scalar. stride/offset select the destination token within each
// entry (bfchar: stride 2 offset 1; bfrange: stride 3 offset 2).
func scanBfRegions(s, begin, end string, stride, offset int) bool {
	for {
		i := strings.Index(s, begin)
		if i < 0 {
			return false
		}
		s = s[i+len(begin):]
		region := s
		j := strings.Index(s, end)
		if j >= 0 {
			region = s[:j]
			s = s[j+len(end):]
		}
		tokens := hexTokens(region)
		for k := offset; k < len(tokens); k += stride {
			if isPUAHex(tokens[k]) {
				return true
			}
		}
		if j < 0 {
			return false
		}
	}
}

// hexTokens returns the angle-bracketed hex tokens (e.g. "<E000>") in s, in order.
func hexTokens(s string) []string {
	var out []string
	for {
		i := strings.IndexByte(s, '<')
		if i < 0 {
			break
		}
		j := strings.IndexByte(s[i:], '>')
		if j < 0 {
			break
		}
		out = append(out, s[i:i+j+1])
		s = s[i+j+1:]
	}
	return out
}

// isPUAHex reports whether a hex token like "<E000>" denotes a Unicode Private
// Use Area scalar. An 8-nibble token forming a UTF-16BE surrogate pair (high
// D800-DBFF, low DC00-DFFF) is decoded to its supplementary scalar first, so
// PUA-A/PUA-B destinations written as surrogate pairs are recognized.
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
	if len(hex) == 8 {
		hi, lo := val>>16, val&0xFFFF
		if hi >= 0xD800 && hi <= 0xDBFF && lo >= 0xDC00 && lo <= 0xDFFF {
			val = 0x10000 + (hi-0xD800)<<10 + (lo - 0xDC00)
		}
	}
	return (val >= 0xE000 && val <= 0xF8FF) || // BMP PUA
		(val >= 0xF0000 && val <= 0xFFFFF) || // Supplementary PUA-A
		(val >= 0x100000 && val <= 0x10FFFD) // Supplementary PUA-B
}

// namedEncodingTable returns the predefined encoding a simple font declares,
// either as a direct /Encoding name or as the /BaseEncoding of an encoding
// dictionary, and an empty string when the font declares none. Per
// ISO 32000-1, 9.6.6.1 a named encoding replaces the font's built-in one, so a
// code that table leaves undefined selects no glyph.
func namedEncodingTable(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) string {
	encObj, ok := fontDict["Encoding"]
	if !ok {
		return ""
	}
	if encName, err := document.ResolveName(ctx, doc, encObj); err == nil && encName != "" {
		if predefinedEncodingNames(string(encName)) != nil {
			return string(encName)
		}
		return ""
	}
	encDict, err := document.ResolveDict(ctx, doc, encObj)
	if err != nil || encDict == nil {
		return ""
	}
	baseName, err := document.ResolveName(ctx, doc, encDict["BaseEncoding"])
	if err != nil || baseName == "" || predefinedEncodingNames(string(baseName)) == nil {
		return ""
	}
	return string(baseName)
}
