package typography

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/typography/encodings"
	"github.com/gotenberg/litho/pkg/internal/typography/type1"
)

// widthOutsideWidths reports whether [Font.GlyphWidth] gives code 0 because
// the simple font's /Widths array does not cover it and its descriptor
// states no /MissingWidth: ISO 32000-1, 9.6.2.1, Table 111 gives /Widths the
// codes /FirstChar to /LastChar only, and Table 122 gives every other code
// the /MissingWidth, 0 by default. An entry the array leaves unreadable falls
// the same way ([Font.simpleWidth]).
func (f *Font) widthOutsideWidths(code int) bool {
	if f.Type3Widths != nil || f.CIDWidths != nil || f.widthStated == nil || f.MissingWidth > 0 {
		return false
	}
	_, ok := f.simpleWidth(code)
	return !ok
}

// programGapWidth returns the advance, in 1/1000 text space units, the
// embedded program gives the glyph code selects, for a code the font gives
// no advance ([Font.widthOutsideWidths]), and whether it has one.
func (f *Font) programGapWidth(code int) (float64, bool) {
	if code < 0 || code > 255 || f.programGapWidths == nil {
		return 0, false
	}
	w, ok := f.programGapWidths[byte(code)]
	return w, ok
}

// recordProgramGapWidths fills [Font.programGapWidths] for a simple font
// whose program is embedded: for each code outside /Widths that names a
// glyph, the advance the program gives that glyph.
//
// The advance of such a code stays 0, as ISO 32000-1, 9.6.2.1 and Table 122
// have it, and the text position moves by 0 (9.4.4). Producers that set each
// glyph by a text-positioning operator of its own still lay the glyphs out
// at the program's advances, so the gap between two of them, measured from
// a glyph 0 wide, reads every letter as a word. Text reconstruction measures
// such a gap from the program's advance instead ([runFont] gapWidth); ISO
// 32000-1, 14.8.2.5 leaves word breaks to the consumer. Only a gap moves:
// positions, widths and ink boxes keep the advance of 0.
//
// The glyph is the one the code selects through the font's encoding: a
// Type 1 program by glyph name (9.6.6.2), the /Differences name first, then
// the name of the character the encoding gives the code, then the program's
// built-in encoding; a CFF program by glyph name through its charset; a
// TrueType program through its (3,0) or (1,0) cmap for a symbolic font, and
// through the Unicode value of the code's character otherwise (9.6.6.4).
//
// # Corpus evidence
//
//   - pdfjs bug1157493.pdf: an embedded Courier whose /Widths covers code 0
//     alone sets each glyph by a TD of its own, 10.87 units apart against
//     the 11.87 the program gives each glyph at 19.79 points. Measured from
//     a 0 advance, "net:" read "n e t :".
//   - govdocs thread_300_300705.pdf pages 9, 10, 11, 15 and 16: a figure
//     label's "ψ←" read "ψ ←".
//   - All corpora: those two files are the only text that moves; no file's
//     words or characters match PDF.js moves either way, in govdocs, pdfjs,
//     pdfcpu, facturx, pdf20 or litho.
func recordProgramGapWidths(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, font *Font) {
	switch font.Subtype {
	case "Type1", "MMType1", "TrueType":
	default:
		return
	}
	var codes []byte
	for c := range 256 {
		if !font.widthOutsideWidths(c) {
			continue
		}
		code := byte(c) //nolint:gosec // c < 256, a one-byte character code
		_, named := font.differenceNames[code]
		_, mapped := font.Encoding[code]
		_, programMapped := font.ProgramGlyphRunes[code]
		if named || mapped || programMapped {
			codes = append(codes, code)
		}
	}
	if len(codes) == 0 {
		return
	}
	fdDict := document.ResolveFontDescriptor(ctx, doc, fontDict)
	if fdDict == nil {
		return
	}
	var widths map[byte]float64
	if data := decodeFontStream(ctx, doc, fdDict, "FontFile"); data != nil {
		widths = type1ProgramGapWidths(font, data, codes)
	} else if data := decodeFontStream(ctx, doc, fdDict, "FontFile3"); data != nil {
		widths = cffProgramGapWidths(ctx, font, data, codes)
	} else if data := decodeFontStream(ctx, doc, fdDict, "FontFile2"); data != nil {
		widths = trueTypeProgramGapWidths(ctx, font, data, codes, fontIsSymbolicByFlags(ctx, doc, fontDict))
	}
	if len(widths) > 0 {
		font.programGapWidths = widths
	}
}

// gapGlyphNames returns the glyph names code may select, in the order they
// are tried: the /Differences name, then the name of the character the
// encoding gives the code.
func (f *Font) gapGlyphNames(code byte) []string {
	var names []string
	if name, ok := f.differenceNames[code]; ok {
		names = append(names, name)
	}
	if r, ok := f.Encoding[code]; ok {
		if name := encodings.RuneToGlyph(r); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// type1ProgramGapWidths returns the charstring advance of the glyph each of
// codes selects in the raw Type 1 program data, scaled to 1/1000 text space
// units by its /FontMatrix.
func type1ProgramGapWidths(font *Font, data []byte, codes []byte) map[byte]float64 {
	advances := type1.ParseCharStringWidths(data)
	if len(advances) == 0 {
		return nil
	}
	scale, _ := type1.FontMatrixScale(data) // An unreadable matrix reads as the 0.001 default.
	builtin := type1.ParseEncoding(data)
	out := make(map[byte]float64)
	for _, code := range codes {
		for _, name := range append(font.gapGlyphNames(code), builtin[code]) {
			if w, ok := advances[name]; ok && w > 0 && name != ".notdef" {
				out[code] = w * scale * 1000
				break
			}
		}
	}
	return out
}

// cffProgramGapWidths returns the advance of the glyph each of codes selects
// by name in the CFF (or OpenType CFF) program data.
func cffProgramGapWidths(ctx context.Context, font *Font, data []byte, codes []byte) map[byte]float64 {
	if font.HmtxWidths == nil || font.UnitsPerEm == 0 {
		return nil
	}
	names := ExtractGlyphNames(ctx, data)
	if len(names) == 0 {
		return nil
	}
	nameToGID := make(map[string]uint16, len(names))
	for gid, name := range names {
		if prev, ok := nameToGID[name]; !ok || gid < prev {
			nameToGID[name] = gid
		}
	}
	builtin := ExtractCFFEncodingNames(data)
	out := make(map[byte]float64)
	for _, code := range codes {
		for _, name := range append(font.gapGlyphNames(code), builtin[code]) {
			gid, ok := nameToGID[name]
			if !ok || name == ".notdef" {
				continue
			}
			if w := font.HmtxWidths[gid]; w > 0 {
				out[code] = float64(w) * 1000 / float64(font.UnitsPerEm)
				break
			}
		}
	}
	return out
}

// trueTypeProgramGapWidths returns the hmtx advance of the glyph each of
// codes selects in the TrueType program data: through the symbol cmap for a
// symbolic font, and through the Unicode value of the code's character
// otherwise (ISO 32000-1, 9.6.6.4).
func trueTypeProgramGapWidths(ctx context.Context, font *Font, data []byte, codes []byte, symbolic bool) map[byte]float64 {
	if font.HmtxWidths == nil || font.UnitsPerEm == 0 {
		return nil
	}
	var symbolGIDs map[uint16]uint16
	if symbolic {
		symbolGIDs = ExtractSymbolCharCodeToGID(ctx, data)
	}
	var unicodeGIDs map[uint16]uint16
	if symbolGIDs == nil {
		unicodeGIDs = ExtractCharCodeToGID(ctx, data)
	}
	out := make(map[byte]float64)
	for _, code := range codes {
		gid, ok := symbolGIDs[uint16(code)]
		if !ok {
			r, mapped := font.Encoding[code]
			if !mapped || r < 0 || r > 0xFFFF {
				continue
			}
			if gid, ok = unicodeGIDs[uint16(r)]; !ok { //nolint:gosec // r bounds-checked above
				continue
			}
		}
		if w := font.HmtxWidths[gid]; gid != 0 && w > 0 {
			out[code] = float64(w) * 1000 / float64(font.UnitsPerEm)
		}
	}
	return out
}
