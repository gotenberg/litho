package remediation

import (
	"context"
	"log/slog"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/typography"
	"github.com/gotenberg/litho/pkg/internal/typography/cmap"
)

// symbolicProgramToUnicode returns the code-to-Unicode mapping the embedded
// program of a symbolic TrueType or Type 1 font that states no /Encoding gives
// the codes it characterises, and nil for any other font.
//
// ISO 32000-1, 9.6.6.4 takes a TrueType font's codes to glyphs through the
// program's (3,0) cmap subtable, or through a (1,0) subtable that copies the
// (3,1) one, and the program names each glyph: its post table, read through
// the Adobe Glyph List (9.10.2), or its Unicode subtable. A Type 1 font's codes
// select glyphs through its program's built-in encoding (9.6.6.1), a Type 1
// (/FontFile) or a Type1C (/FontFile3) program alike, which names the glyph
// each code selects, read through the same glyph lists.
// [typography.Font.ProgramGlyphRunes] holds those readings, the ones text
// extraction shows. A code the program leaves unnamed, names with a numeric
// name, or names with a Private Use Area or control value, gets no entry: what
// the font's default encoding reads there is a guess (9.6.6.1), and a guessed
// map turned "−∞ ∞" into "−1 1" (see [fixSimpleFontToUnicode]).
//
// The map carries litho's reading, not PDF.js's, which reads such a font's
// codes through MacRomanEncoding (see the typography package's
// applySymbolicTrueTypeGlyphs), so PDF.js reads the remediated file as litho
// reads the original.
//
// # Corpus evidence
//
//   - govdocs: 10 files carry WordPerfect and Symbol subsets of this kind
//     that get a map. On veraPDF 1.30.2 the remediated outputs of 8 lose
//     6.2.11.7.2 t1 at PDF/A-2u and PDF/A-3u, 7.21.7 t1 at PDF/UA-1 and
//     8.4.5.8 t1 at PDF/UA-2, the other 2 keep it for other fonts, none gains
//     a rule, and litho's extracted text is unchanged.
//   - govdocs thread_200_200393.pdf, thread_300_300082.pdf,
//     thread_300_300237.pdf: post names code 0xB7 bullet. PDF.js reads "∑"
//     from the original, its golden, and "•" from the remediated file.
//   - All corpora: 41 files show a symbolic Type 1 font that states no
//     /Encoding, and 48 of their font cells at PDF/A-2u, PDF/A-3u, PDF/UA-1
//     and PDF/UA-2 get a map. On veraPDF 1.30.2 the remediated outputs of
//     verapdf 6-2-11-7-2-t01-fail-a, pdfjs issue9552.pdf and
//     reduced_planck_constant.pdf, govdocs thread_400_400008.pdf lose the
//     rule at every level that states it, 1a, 2a and 3a included; govdocs
//     thread_300_300849.pdf loses 8.4.3 t1 at PDF/UA-2 and 6.2.11.7.3 t1 at
//     PDF/A-2a and PDF/A-3a. litho agrees with veraPDF on every cell, no rule
//     is gained, and litho's extracted text is unchanged in both modes.
//   - All corpora: 16 files show a symbolic Type1C font that states no
//     /Encoding and no /ToUnicode, and the remediated outputs of 9 change at
//     some of the 13 levels, in both modes. On veraPDF 1.30.2 those of verapdf
//     6-3-8-t01-fail-a lose the rule at every level that states it and pass
//     at PDF/A-1a, PDF/A-2a, PDF/A-2u, PDF/A-3a, PDF/A-3u, PDF/UA-1 and
//     PDF/UA-2; those of pdfjs issue3566.pdf lose 6.2.11.7.2 t2, 6.2.10.7 t1,
//     7.21.7 t2 and 8.4.5.8 t2, which litho did not report, so 18 cells of
//     disagreement close. No rule is gained, no disagreement opens, and
//     litho's own verdicts are unchanged apart from fail-a's. veraPDF reads
//     neither pdfjs link-annotation-border.pdf nor prefilled_f1040.pdf, the
//     originals included.
//   - All corpora: the fonts left unmapped name a shown code with a name no
//     glyph list carries (the "H11021" names of the Universal and
//     MathematicalPi subsets), or a numeric one (AdvOpti's "C42").
func symbolicProgramToUnicode(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) map[uint16]rune {
	// An unreadable /Subtype names no font with a program to read, which is
	// the answer.
	switch subtype, _ := document.ResolveName(ctx, doc, fontDict["Subtype"]); subtype {
	case "TrueType", "Type1", "MMType1":
	default:
		return nil
	}
	font, err := typography.ResolveFont(ctx, doc, fontDict)
	if err != nil || font == nil {
		doc.Logger.Debug("resolving a symbolic simple font for its /ToUnicode", slog.Any("err", err))
		return nil
	}
	// A font text extraction reads through a map of its own, one litho
	// synthesises from the font's name, is not read through its program, and a
	// map written from the program would change what the output reads.
	if len(font.ProgramGlyphRunes) == 0 || font.ToUnicode != nil {
		return nil
	}
	mapping := make(map[uint16]rune, len(font.ProgramGlyphRunes))
	for code, r := range font.ProgramGlyphRunes {
		if isValidUnicodeMapping(r) {
			mapping[uint16(code)] = r
		}
	}
	return mapping
}

// programType0ToUnicode builds the /ToUnicode CMap of a Type 0 font whose
// codes reach its glyphs through a /CIDToGIDMap stream, and returns nil when
// it cannot ground an entry for every code the font shows.
//
// The route has three links. The /Encoding CMap takes a code to a CID: under
// Identity-H or Identity-V the two-byte code is the CID (ISO 32000-1, 9.7.5.2,
// Table 118), and under any other CMap its cidchar and cidrange mappings give
// it ([typography.Font.MappedEncodingCodes]). The stream takes the CID to a
// glyph (9.7.4.2), and the program's Unicode cmap subtable or glyph names take
// the glyph to its character ([typography.ProgramCIDToUnicode]). Text
// extraction reads such a font through the same composition. A code that no
// link grounds, one the CMap maps only by bfchar or not at all, one whose
// glyph the program gives no Unicode value, leaves the CMap unwritten: a
// partial map satisfies litho's presence check while veraPDF reports the
// rule (see [fixSimpleFontToUnicode]).
//
// A descendant of the Adobe-GB1, Adobe-CNS1, Adobe-Japan1 or Adobe-Korea1
// collection is left alone: ISO 19005-2, 6.2.11.7.2 and ISO 14289-1, 7.21.7
// exempt it, since the collection's UCS2 CMap names its characters. An
// encoding CMap whose codespace mixes code widths is too: the mapping is keyed
// by a code's value, so a one-byte <41> and a two-byte <0041> would collide.
//
// # Corpus evidence
//
//   - pdfjs issue16553.pdf: its Identity-H Helvetica gets the map, and the
//     remediated output passes veraPDF 1.30.2 at PDF/A-2u, PDF/A-3u and
//     PDF/UA-2.
//   - litho cmapusestream_2b.pdf: under an embedded encoding CMap, the
//     remediated output passes veraPDF and litho at the seven levels that
//     state the rule, in both modes.
func programType0ToUnicode(ctx context.Context, doc *document.Document, fontDict, cidFont ast.PdfDict, shown []ast.PdfString) []byte {
	if len(shown) == 0 || adobeCJKCollection(ctx, doc, cidFont) {
		return nil
	}
	table := typography.ProgramCIDToUnicode(ctx, doc, fontDict)
	if len(table) == 0 {
		return nil
	}

	// An /Encoding that is no name is an embedded CMap stream, or nothing
	// litho reads, and either way not an Identity CMap.
	encName, _ := document.ResolveName(ctx, doc, fontDict["Encoding"])
	identity := encName == "Identity-H" || encName == "Identity-V"
	width := 2
	var font *typography.Font
	var spaces []cmap.CodeSpaceRange
	if !identity {
		var err error
		font, err = typography.ResolveFont(ctx, doc, fontDict)
		if err != nil || font == nil {
			doc.Logger.Debug("resolving a Type 0 font for its /ToUnicode", slog.Any("err", err))
			return nil
		}
		spaces = font.EncodingCodeSpaceRanges()
		var ok bool
		if width, ok = uniformCodeWidth(spaces); !ok {
			return nil
		}
	}

	mapping := make(map[uint16]rune)
	for _, s := range shown {
		codes, ok := shownType0Codes(font, []byte(s), identity)
		if !ok {
			return nil
		}
		for _, c := range codes {
			if !c.Mapped || len(c.Code) != width || c.CID < 0 || c.CID > 0xFFFF {
				return nil
			}
			r, ok := table[uint16(c.CID)]
			if !ok || !isValidUnicodeMapping(r) {
				return nil
			}
			key := uint16(c.Code[0])
			if width == 2 {
				key = key<<8 | uint16(c.Code[1])
			}
			mapping[key] = r
		}
	}
	return generateToUnicodeCMapIn(mapping, width, spaces)
}

// shownType0Codes splits a string a Type 0 font shows into its character
// codes: two bytes each under an Identity CMap, whose code is the CID (ISO
// 32000-1, 9.7.5.2), and by the encoding CMap's codespace ranges otherwise. ok
// is false for an odd byte left over by an Identity CMap and for an encoding
// that states no CIDs litho reads.
func shownType0Codes(font *typography.Font, data []byte, identity bool) ([]typography.EncodingCode, bool) {
	if !identity {
		return font.MappedEncodingCodes(data)
	}
	if len(data)%2 != 0 {
		return nil, false
	}
	codes := make([]typography.EncodingCode, 0, len(data)/2)
	for i := 0; i < len(data); i += 2 {
		codes = append(codes, typography.EncodingCode{Code: data[i : i+2], CID: int(data[i])<<8 | int(data[i+1]), Mapped: true})
	}
	return codes, true
}

// uniformCodeWidth returns the byte width every codespace range in spaces
// shares, when that width is one or two bytes, the widths a generated
// /ToUnicode CMap keys its codes by.
func uniformCodeWidth(spaces []cmap.CodeSpaceRange) (int, bool) {
	width := 0
	for _, r := range spaces {
		if len(r.Low) != len(r.High) || (width != 0 && len(r.Low) != width) {
			return 0, false
		}
		width = len(r.Low)
	}
	return width, width == 1 || width == 2
}

// adobeCJKCollection reports whether a CIDFont's /CIDSystemInfo names one of
// the Adobe character collections whose UCS2 CMaps give every CID a
// character ([adobeOrderings]).
func adobeCJKCollection(ctx context.Context, doc *document.Document, cidFont ast.PdfDict) bool {
	info, err := document.ResolveDict(ctx, doc, cidFont["CIDSystemInfo"])
	if err != nil || info == nil {
		return false
	}
	// An unreadable entry names no Adobe collection.
	registry, _ := document.ResolveString(ctx, doc, info["Registry"])
	ordering, _ := document.ResolveString(ctx, doc, info["Ordering"])
	return string(registry) == "Adobe" && adobeOrderings[string(ordering)]
}
