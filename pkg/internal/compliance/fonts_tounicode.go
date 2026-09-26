package compliance

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/typography/cmap"
)

// checkType3ToUnicode states rule, the ToUnicode presence rule of profile, for
// each code a Type 3 font shows without a Unicode value.
//
// ISO 19005-2 and ISO 19005-3, 6.2.11.7.2, ISO 14289-1, 7.21.7 and ISO 14289-2,
// 8.4.5.8 ask that every shown code map to Unicode, and ISO 32000-1, 9.10.2
// derives that value from the /ToUnicode CMap first and from the glyph name
// only for a code the CMap leaves out. A /ToUnicode CMap therefore vouches only
// for the codes it maps: veraPDF (PDSimpleFont.toUnicode) reads a Type 3 code
// the CMap leaves out through its glyph name, and fails it when the name has
// no Unicode value. ISO 19005-1, 6.3.8 admits no glyph name in place of the
// map, and veraPDF's PDF/A-1 flavour reads a Type 3 glyph's value from the
// CMap alone (GFGlyph.getToUnicodePDFA1). ISO 19005-4 sets no presence rule.
//
// A /ToUnicode entry that is not a stream, or whose stream cannot be decoded,
// is left alone: a name such as /Identity-H maps every code for veraPDF, and
// litho cannot tell which codes an undecodable stream maps. A stream that
// decodes to no mapping maps no code.
//
// Measured with veraPDF 1.30 at PDF/A-2u, PDF/A-3u and PDF/UA-1 on a Type 3
// font showing four codes named zzq, zzr, A and zzs whose /ToUnicode maps the
// second only: the first and fourth fail. At PDF/A-1a every code but the
// second fails.
//
// # Corpus evidence
//
//   - All corpora, at PDF/A-2u: the counts of ISO 19005-2, 6.2.11.7.2 t1
//     moved toward veraPDF's in 5 files and away in none; each now equals
//     veraPDF's, a summed distance of 254 before.
//   - govdocs thread_200_200408.pdf: most of its glyphs without a Unicode
//     value are shown through Type 3 fonts whose /ToUnicode leaves their codes
//     out; litho states 181 of them, as veraPDF does, where it stated 15.
func checkType3ToUnicode(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, fontName string, pageNum int, profile string, rule Rule, usage *fontUsageInfo) []Violation {
	if PDFAPart(profile) == 4 {
		return nil
	}
	cmapOnly := PDFAPart(profile) == 1
	if usage == nil || len(usage.charCodes) == 0 {
		// With no shown codes to judge, the font is judged on the names its
		// /Differences lists, and stated once as a whole.
		if !cmapOnly && allDifferencesGlyphNamesAGLResolvable(ctx, doc, fontDict) {
			return nil
		}
		if !hasToUnicode(ctx, doc, fontDict) {
			return failf(rule, "font %s on page %d", fontName, pageNum)
		}
		return nil
	}
	codes := sortedByteKeys(usage.charCodes)
	if !cmapOnly {
		codes = shownType3CodesWithoutUnicode(ctx, doc, fontDict, usage)
	}
	if len(codes) == 0 {
		return nil
	}
	if !hasToUnicode(ctx, doc, fontDict) {
		return missingUnicodeFaults(rule, fontName, pageNum, codes)
	}
	mapped, ok := toUnicodeCodeValues(ctx, doc, fontDict["ToUnicode"])
	if !ok {
		return nil
	}
	faults := newGlyphFaults(rule)
	for _, c := range codes {
		if mapped[int(c)] {
			continue
		}
		if cmapOnly {
			faults.add(uint32(c), "font %s on page %d: code %d is outside /ToUnicode", fontName, pageNum, c)
			continue
		}
		faults.add(uint32(c), "font %s on page %d: code %d is outside /ToUnicode and its glyph name gives no Unicode value",
			fontName, pageNum, c)
	}
	return faults.violations()
}

// toUnicodeCodeValues returns the value of every code the /ToUnicode CMap
// tuObj maps, read as veraPDF reads it for a simple font: by integer value, in
// the strict view [cmap.CMap.StrictMapping] gives. A CMap carrying a token its syntax cannot produce maps
// nothing ([cmap.CMap.Malformed]). ok is false when tuObj is not a stream or
// its stream cannot be decoded.
func toUnicodeCodeValues(ctx context.Context, doc *document.Document, tuObj ast.PdfObject) (map[int]bool, bool) {
	stream, err := document.ResolveStream(ctx, doc, tuObj)
	if err != nil || stream.Dict == nil {
		return nil, false
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		doc.Logger.Debug("compliance: fonts: failed to decode ToUnicode CMap", "error", err)
		return nil, false
	}
	parsed, err := cmap.Parse(ctx, data)
	if err != nil || parsed == nil || parsed.Malformed() {
		return map[int]bool{}, true
	}
	return sourceCodeValues(parsed.StrictMapping()), true
}

// sourceCodeValues returns the integer value of every source code of a
// /ToUnicode mapping. veraPDF looks a simple font's code up in the CMap by its
// integer value, so a producer that declares a two-byte codespace still maps
// code 2 with <0002>; a source longer than two bytes names no simple-font code.
func sourceCodeValues(mapping map[string]string) map[int]bool {
	mapped := make(map[int]bool, len(mapping))
	for src := range mapping {
		if len(src) > 2 {
			continue
		}
		v := 0
		for i := range len(src) {
			v = v<<8 | int(src[i])
		}
		mapped[v] = true
	}
	return mapped
}
