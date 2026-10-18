package remediation

import (
	"context"
	"fmt"
	"strconv"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/typography/encodings"
)

// fixType3FontToUnicode generates a /ToUnicode CMap for a Type 3 font that
// has none, from the glyph names its /Encoding /Differences array gives the
// codes, returning what it repaired.
//
// ISO 19005-1, 6.3.8 asks a PDF/A-1 level A font for a /ToUnicode CMap, Type 3
// fonts included, and ISO 19005-2 and ISO 19005-3, 6.2.11.7.2, ISO 14289-1,
// 7.21.7 and ISO 14289-2, 8.4.5.8 ask that every shown code map to Unicode. A
// Type 3 font names its glyphs only through /Differences (ISO 32000-1, 9.6.5),
// so a name is the one grounded statement of what a glyph means: the value
// comes from it alone ([groundedGlyphUnicode]), never from the glyph
// procedures, and a code whose name gives none stays unmapped. The CMap is
// written only when it covers every code the font shows
// ([attachSimpleToUnicode]); a font with a /ToUnicode of its own is left as
// the producer wrote it.
//
// A /BaseEncoding, which 9.6.5 does not provide for in a Type 3 font, supplies
// no name here: a code only it names stays unmapped, so such a font is not
// repaired.
//
// # Corpus evidence
//
//   - All corpora: of the 118 files holding a Type 3 font, remediated at
//     PDF/A-1a, 2u, 3u, PDF/UA-1 and PDF/UA-2 in both modes, 120 outputs
//     change. On veraPDF 1.30.2 they gain no rule and lose 36 rule classes,
//     every one 6.3.8 t1 at PDF/A-1a, and 16 more outputs fully fix; the
//     2u, 3u and UA outputs keep their verdicts, since veraPDF reads those
//     names there already. Extracted text against the PDF.js golden moves on
//     none.
//   - litho type3names_2u.pdf: a font naming a code g12 gets no map, and
//     its findings stay on both tools.
func fixType3FontToUnicode(ctx context.Context, doc *document.Document, alloc *writer.ObjAllocator, fontDict ast.PdfDict, fontRef ast.PdfIndirectRef, shownCodes map[byte]bool) []string {
	if _, mapped := fontDict["ToUnicode"]; mapped || fontRef.ObjectNum == 0 || len(shownCodes) == 0 {
		return nil
	}
	mapping := make(map[uint16]rune)
	for code, name := range compliance.DifferencesGlyphNames(ctx, doc, fontDict) {
		if r, ok := groundedGlyphUnicode(name); ok && isValidUnicodeMapping(r) {
			mapping[uint16(code)] = r
		}
	}
	if len(mapping) == 0 {
		return nil
	}
	fixes := attachSimpleToUnicode(alloc, fontDict, fontRef, mapping, shownCodes)
	if len(fixes) == 0 {
		return nil
	}
	return []string{fmt.Sprintf("generated /ToUnicode CMap for Type 3 font %s from its glyph names", fontBaseName(fontDict))}
}

// groundedGlyphUnicode returns the Unicode value a glyph name states, from a
// published list or a spelling the Adobe Glyph List Specification defines,
// and nothing it would have to guess.
//
// The lists are the ones ISO 32000-1, 9.10.2 and veraPDF's PDSimpleFont
// consult: the Adobe Glyph List (glyphlist.txt), then the named characters of
// the Symbol font (Annex D, Table D.5), then the ZapfDingbats list
// (zapfdingbats.txt). The mapping procedure of the Adobe Glyph List
// Specification adds two spellings that carry their code point: "uni"
// followed by four uppercase hexadecimal digits, and "u" followed by four to
// six. A name that spells
// several code points, a name with a .suffix and an underscore-joined
// ligature map to more than one character or to a guess, so they stay
// unmapped. An AGL name litho's glyph tables hold no single value for stays
// unmapped as well.
func groundedGlyphUnicode(name string) (rune, bool) {
	if encodings.InAdobeGlyphList(name) {
		if r, ok := encodings.GlyphToRuneSimple(name); ok {
			return r, true
		}
	}
	if r, ok := encodings.SymbolGlyphsUnicode[name]; ok {
		return r, true
	}
	if r, ok := encodings.DingbatsGlyphsUnicode[name]; ok {
		return r, true
	}
	switch {
	case len(name) == len("uniXXXX") && name[:3] == "uni":
		return aglCodePoint(name[3:])
	case len(name) >= len("uXXXX") && len(name) <= len("uXXXXXX") && name[0] == 'u':
		return aglCodePoint(name[1:])
	}
	return 0, false
}

// aglCodePoint parses the hexadecimal digits of a uniXXXX or uXXXX[XX] glyph
// name. The AGL Specification admits uppercase digits only, and a value that
// is a surrogate or past U+10FFFF names no character.
func aglCodePoint(digits string) (rune, bool) {
	for i := range len(digits) {
		if c := digits[i]; (c < '0' || c > '9') && (c < 'A' || c > 'F') {
			return 0, false
		}
	}
	v, err := strconv.ParseUint(digits, 16, 32)
	if err != nil || v > 0x10FFFF || (v >= 0xD800 && v <= 0xDFFF) {
		return 0, false
	}
	return rune(v), true //nolint:gosec // bounded to 0x10FFFF above
}
