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
// is left alone: veraPDF (PDFont.cMapToUnicode) maps every code through a
// name such as /Identity-H, and litho cannot tell which codes an undecodable
// stream maps. A stream that decodes to no mapping maps no code.
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
// the strict view [cmap.CMap.StrictMapping] gives, with the CMaps it builds on
// ([resolveUsedCMaps]). A CMap carrying a token its syntax cannot produce maps
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
	parsed = resolveUsedCMaps(ctx, doc, tuObj, stream.Dict, parsed)
	return sourceCodeValues(parsed.StrictMapping()), true
}

// resolveUsedCMaps returns parsed, the /ToUnicode CMap tuObj names, with the
// mappings of the CMaps it builds on, merged the way veraPDF (PDCMap, CMap)
// merges them. The usecmap operator (ISO 32000-1, 9.7.5.4) names a predefined
// CMap, whose single-code mappings join the program's: veraPDF merges only
// those (CMap.useCMap copies the base's bfchar entries and none of its bfrange
// intervals). A /UseCMap entry in the stream dictionary (ISO 32000-1, 9.7.5.3,
// Table 120) names a predefined CMap or an embedded CMap stream, whose every
// mapping supplies a code the program leaves out, followed down its own
// /UseCMap entries.
//
// The /UseCMap chain is bounded by [maxUseCMapChainDepth] and stops at a stream
// it has already met; veraPDF raises "Loop inside CMap" there and abandons the
// whole validation. An embedded base supplies its values only to codes the
// program leaves out, where veraPDF also lets the base's single-code mappings
// replace the program's own values; that changes the value of a code both map,
// never whether it maps.
//
// Measured with veraPDF 1.30 at PDF/A-2u on a Type 3 font showing codes 41,
// 60, 61 and 65 hexadecimal: a /ToUnicode mapping none of them fails four; one
// whose program runs /Adobe-Korea1-UCS2 usecmap fails 41 and 65, which that
// CMap maps only through bfrange; one whose dictionary names that CMap with
// /UseCMap fails none; one whose /UseCMap names a stream mapping 41 and 65,
// or a stream that in turn names one mapping 41 while mapping 65 itself, fails
// 60 and 61. A program that maps 60 to U+0000 before its usecmap and 61 to
// U+0000 after it fails the value rule for 61 alone.
//
// # Corpus evidence
//
//   - All corpora: pdfjs issue5010.pdf is the one file whose /ToUnicode builds
//     on another CMap, through usecmap; none names one with /UseCMap.
//   - pdfjs issue5010.pdf: its /ToUnicode runs /Adobe-Korea1-UCS2 usecmap and
//     maps a handful of codes itself. veraPDF fails at PDF/A-2u exactly the
//     five shown codes that CMap reaches only through bfrange; litho states
//     those five where, not loading the base, it stated ten.
func resolveUsedCMaps(ctx context.Context, doc *document.Document, tuObj ast.PdfObject, dict ast.PdfDict, parsed *cmap.CMap) *cmap.CMap {
	visited := make(map[int]bool)
	if ref, ok := tuObj.(ast.PdfIndirectRef); ok {
		visited[ref.ObjectNum] = true
	}
	return resolveUsedCMapsAt(ctx, doc, dict, parsed, visited, 0)
}

// resolveUsedCMapsAt is [resolveUsedCMaps] for a CMap depth links down the
// /UseCMap chain, visited holding the streams already met.
func resolveUsedCMapsAt(ctx context.Context, doc *document.Document, dict ast.PdfDict, parsed *cmap.CMap, visited map[int]bool, depth int) *cmap.CMap {
	if name := parsed.UseCMap(); name != "" {
		if chars, _ := cmap.PredefinedUnicode(name); chars != nil {
			parsed = parsed.WithBase(chars, nil, false)
		}
	}
	baseObj, ok := dict["UseCMap"]
	if !ok || depth >= maxUseCMapChainDepth || ctx.Err() != nil {
		return parsed
	}
	if name, err := document.ResolveName(ctx, doc, baseObj); err == nil && name != "" {
		// A predefined base is merged after the program has run, so its
		// single-code mappings replace the program's own (PDCMap.getCMapFile).
		chars, all := cmap.PredefinedUnicode(string(name))
		if all == nil {
			return parsed
		}
		return parsed.WithBase(chars, all, true)
	}
	if ref, ok := baseObj.(ast.PdfIndirectRef); ok {
		if visited[ref.ObjectNum] {
			return parsed
		}
		visited[ref.ObjectNum] = true
	}
	stream, err := document.ResolveStream(ctx, doc, baseObj)
	if err != nil || stream.Dict == nil {
		return parsed
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		doc.Logger.Debug("compliance: fonts: failed to decode /UseCMap base CMap", "error", err)
		return parsed
	}
	base, err := cmap.Parse(ctx, data)
	if err != nil || base == nil || base.Malformed() {
		return parsed
	}
	base = resolveUsedCMapsAt(ctx, doc, stream.Dict, base, visited, depth+1)
	return parsed.WithBase(nil, base.StrictMapping(), true)
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
