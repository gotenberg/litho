package compliance

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/typography/cmap"
	"github.com/gotenberg/litho/pkg/internal/typography/encodings"
)

// toUnicodeMappedCodes returns the codes the /ToUnicode CMap of a simple font
// maps, by integer value ([sourceCodeValues]), or nil when the font has none,
// or one litho cannot read as a CMap: veraPDF then finds no value in it and
// falls back to the glyph name for every code.
func toUnicodeMappedCodes(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) map[int]bool {
	if !hasToUnicode(ctx, doc, fontDict) {
		return nil
	}
	tuObj := fontDict["ToUnicode"]
	stream, err := document.ResolveStream(ctx, doc, tuObj)
	if err != nil || stream.Dict == nil {
		return nil
	}
	data, err := doc.DecodeStream(ctx, stream)
	if err != nil {
		doc.Logger.Debug("compliance: fonts: failed to decode ToUnicode CMap", "error", err)
		return nil
	}
	parsed, err := cmap.Parse(ctx, data)
	if err != nil || parsed == nil {
		doc.Logger.Debug("compliance: fonts: failed to parse ToUnicode CMap", "error", err)
		return nil
	}
	return cmapSourceCodeValues(resolveUsedCMaps(ctx, doc, tuObj, stream.Dict, parsed))
}

// simpleEncoding is the encoding veraPDF names the character codes of a simple
// font that embeds no program by (PDFont.getEncodingMapping): a predefined
// table and the /Differences entries over it.
type simpleEncoding struct {
	// table is MacRomanEncoding, MacExpertEncoding, WinAnsiEncoding or
	// StandardEncoding, or empty when the encoding holds no table.
	table       string
	differences map[byte]string
}

// unembeddedSimpleEncoding returns the [simpleEncoding] veraPDF reads a simple
// font of the given subtype through, when the font embeds no program. It is
// the naming [checkUnembeddedNotdef] judges .notdef by, before the symbolic
// TrueType carve-out that rule alone makes: a /Differences entry names its
// code; otherwise veraPDF holds a table for MacRomanEncoding,
// MacExpertEncoding and WinAnsiEncoding alone, read from the /Encoding name or
// its /BaseEncoding ([type1EncodingTable]); and a Type1 font of the standard 14
// whose own encoding gives no table reads StandardEncoding from its metrics
// ([standardLatinEncodingApplies]).
func unembeddedSimpleEncoding(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, subtype string) simpleEncoding {
	enc := simpleEncoding{table: type1EncodingTable(ctx, doc, fontDict), differences: fontDifferencesNames(ctx, doc, fontDict)}
	if enc.table == "" && subtype != "TrueType" && standardLatinEncodingApplies(ctx, doc, fontDict) {
		enc.table = "StandardEncoding"
	}
	return enc
}

// glyph returns the name enc gives code and whether it gives one. fromTable
// reports a name the predefined table gives; the name itself is then left
// empty, since every name those tables define carries a Unicode value. A code
// the table leaves undefined is named .notdef (Encoding.getName, veraPDF-parser
// 1.30).
func (e simpleEncoding) glyph(code byte) (name string, fromTable, named bool) {
	if n, ok := e.differences[code]; ok {
		return n, false, true
	}
	if e.table == "" {
		return "", false, false
	}
	if encodingUndefinedCode(e.table, code) {
		return ".notdef", false, true
	}
	return "", true, true
}

// unembeddedSimpleFont reports whether fontDict is a simple font, Type1,
// MMType1 or TrueType, that embeds no program, and returns its subtype.
func unembeddedSimpleFont(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) (string, bool) {
	subtype := fontSubtype(ctx, doc, fontDict)
	if subtype != "Type1" && subtype != "MMType1" && subtype != "TrueType" {
		return "", false
	}
	if fd := document.ResolveFontDescriptor(ctx, doc, fontDict); fd != nil && extractFontProgramData(ctx, doc, fd) != nil {
		return "", false
	}
	return subtype, true
}

// unembeddedNameHasUnicode reports whether veraPDF finds a Unicode value for a
// glyph name other than .notdef: one its Adobe Glyph List carries, or a name
// of the Symbol font or, but for a Type1 font at PDF/A-1, of ZapfDingbats,
// which it answers with an empty value that passes both Unicode rules
// (PDSimpleFont.toUnicode, and PDType1Font.toUnicodePDFA1 at PDF/A-1, which
// consults no ZapfDingbats set). A uniXXXX spelling is not listed, so it
// carries none.
func unembeddedNameHasUnicode(name string, pdfa1Type1 bool) bool {
	if encodings.InAdobeGlyphList(name) {
		return true
	}
	if _, ok := encodings.SymbolGlyphsUnicode[name]; ok {
		return true
	}
	if pdfa1Type1 {
		return false
	}
	_, ok := encodings.DingbatsGlyphsUnicode[name]
	return ok
}

// unembeddedUnicodeFaults states the Unicode rules for the codes usage shows
// through a simple font that embeds no program, glyph by glyph, as veraPDF
// computes each glyph's Unicode value (PDSimpleFont.toUnicode): the value the
// /ToUnicode CMap maps the code to, which mapped holds the codes of; otherwise
// the value of the glyph name the font's encoding gives the code
// ([unembeddedSimpleEncoding]). With no name the glyph has no value, which
// fails the presence rule, ISO 19005-2, 6.2.11.7.2 t1 and its counterparts;
// the name .notdef has the value U+0000 in veraPDF's glyph list, which fails
// the value rule, 6.2.11.7.2 t2 and its counterparts, and passes the presence
// rule; and a name with no value ([unembeddedNameHasUnicode]) fails the
// presence rule. At PDF/A-1 a Type1 font's .notdef passes, since veraPDF only
// asks there whether the glyph list holds the name. The value the CMap maps a
// code to is judged by [checkToUnicodeCMapValues].
//
// presence is the rule a glyph with no value fails and value the rule a glyph
// valued U+0000 fails, either empty when the profile states none.
//
// Measured with veraPDF 1.30 at PDF/A-2u on eleven codes, 0, 1, 0x1F, 0x3C,
// 0x41, 0x7F, 0x80, 0x81, 0xA1, 0xAD and 0xE2, shown through Helvetica, a
// Helvetica with a /FontDescriptor, an Arial Type1, symbolic or not, and an
// Arial TrueType, symbolic or not, each under no /Encoding, /WinAnsiEncoding,
// /MacRomanEncoding, /MacExpertEncoding, /StandardEncoding, /Foo and three
// /Differences dictionaries, and through Symbol and ZapfDingbats: every count
// of 6.2.11.7.2 t1 and t2 is the one this naming gives, and so is every count
// of their counterparts at PDF/A-1a, PDF/A-4, PDF/UA-1 and PDF/UA-2.
// Helvetica with no /Encoding, or under /StandardEncoding or /Foo, fails t2
// for the seven codes StandardEncoding leaves undefined; an Arial under
// /StandardEncoding fails t1 for all eleven; a code /Differences names uni0041
// fails t1; one named a71 passes both.
//
// # Corpus evidence
//
//   - All corpora, at PDF/A-1b, PDF/A-2b and PDF/A-2u: naming the codes this
//     way moved the counts of ISO 19005-2, 6.2.11.7.2 t1 onto veraPDF's in 12
//     files and those of t2 in 5, and none away; their summed distance fell
//     from 35 and 792 to 0. No other count moved.
//   - pdfjs standard_fonts.pdf: shows the 32 control codes through each of
//     the twelve unembedded standard 14 Latin fonts, whose StandardEncoding
//     names them .notdef, and veraPDF states t2 384 times, where litho stated
//     12.
//   - pdfjs issue18208.pdf and poppler-90-0-fuzzed.pdf: show codes an
//     unembedded font's encoding names .notdef, and veraPDF states t2 466 and
//     22 times, where litho stated 70 and 0.
//   - govdocs thread_300_300507.pdf and thread_300_300517.pdf: show codes an
//     unembedded font's encoding leaves unnamed, and veraPDF states t1 6
//     times each, where litho stated none.
func unembeddedUnicodeFaults(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, subtype, fontName string, pageNum int, profile string, presence, value Rule, usage *fontUsageInfo, mapped map[int]bool) []Violation {
	if usage == nil || len(usage.charCodes) == 0 {
		return nil
	}
	enc := unembeddedSimpleEncoding(ctx, doc, fontDict, subtype)
	pdfa1Type1 := PDFAPart(profile) == 1 && subtype != "TrueType"
	missing, zero := newGlyphFaults(presence), newGlyphFaults(value)
	for _, code := range sortedByteKeys(usage.charCodes) {
		if mapped[int(code)] {
			continue
		}
		name, fromTable, named := enc.glyph(code)
		switch {
		case !named:
			missing.add(uint32(code), "font %s on page %d: code %d has no glyph name and no /ToUnicode entry", fontName, pageNum, code)
		case fromTable:
		case name == ".notdef":
			if !pdfa1Type1 {
				zero.add(uint32(code), "font %s on page %d: code %d is named .notdef, whose Unicode value is U+0000", fontName, pageNum, code)
			}
		case !unembeddedNameHasUnicode(name, pdfa1Type1):
			missing.add(uint32(code), "font %s on page %d: code %d is named %s, which carries no Unicode value", fontName, pageNum, code, name)
		}
	}
	var out []Violation
	if presence.Specification != "" {
		out = append(out, missing.violations()...)
	}
	if value.Specification != "" {
		out = append(out, zero.violations()...)
	}
	return out
}
