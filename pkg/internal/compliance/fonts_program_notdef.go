package compliance

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/typography"
)

// programNotdefCodes returns the codes usage shows through an embedded Type 1
// font that the font program names .notdef where the font's own encoding names
// nothing, leaving out the codes its /ToUnicode CMap maps.
//
// ISO 32000-1, 9.10.2 derives a simple font's Unicode value from the glyph name
// a code selects, and Table 114 makes the program's built-in encoding the base
// of an /Encoding that names no table. veraPDF computes the value that way
// (PDSimpleFont.toUnicode): the name the encoding gives the code, as
// [unembeddedSimpleEncoding] reads it, and, when it gives none, the name the
// program gives it (FontProgram.getGlyphName). The Adobe Glyph List it reads
// maps .notdef to U+0000, so such a glyph fails the value rule, ISO 19005-2,
// 6.2.11.7.2 t2, ISO 19005-4, 6.2.10.7 t1, ISO 14289-1, 7.21.7 t2 and ISO
// 14289-2, 8.4.5.8 t2, and passes the presence rule, 6.2.11.7.2 t1 and its
// counterparts. At PDF/A-1 it passes ISO 19005-1, 6.3.8 t1 too, since
// PDType1Font.toUnicodePDFA1 only asks whether the glyph list holds the name.
//
// The program names a code .notdef ([programGlyphName]) when it is a Type 1
// program (/FontFile) whose built-in encoding is StandardEncoding and leaves the
// code undefined, or whose encoding vector was filled with .notdef before its
// codes were listed; or a bare CFF (/FontFile3 /Type1C) whose encoding gives
// the code no glyph, which CFFType1FontProgram.getGlyphName names .notdef. A
// vector left unfilled names an unlisted code nothing, and the code then has
// no Unicode value at all.
//
// Measured with veraPDF 1.30.2 at PDF/A-1a, PDF/A-2b, PDF/A-2u, PDF/A-4,
// PDF/UA-1 and PDF/UA-2 on a code a filled vector, a StandardEncoding program
// and a CFF leave unnamed, under no /Encoding, a /Differences-only dictionary
// and /StandardEncoding, symbolic and not: each fails the value rule alone, and
// nothing at PDF/A-1a and PDF/A-2b. A code an unfilled vector leaves unnamed
// fails the presence rule.
//
// # Corpus evidence
//
//   - All corpora, at every profile litho validates: the reading moved the
//     cells of two files, every one onto veraPDF's count.
//   - pdfjs issue6901.pdf: shows the space, code 32, through LSXICB+CMR9, a
//     symbolic subset with no /Encoding whose filled vector leaves the code
//     out. veraPDF reports 6.2.11.7.2 t2 where litho reported t1, and nothing
//     at PDF/A-1a, where litho reported 6.3.8 t1.
//   - pdfjs issue6721_reduced.pdf: shows code 224 through NimbusRomNo9L-Regu,
//     whose program refers to StandardEncoding, which leaves the code
//     undefined, and whose /ToUnicode does not map it. veraPDF reports
//     6.2.11.7.2 t2, which litho did not.
func programNotdefCodes(ctx context.Context, doc *document.Document, fontDict ast.PdfDict, usage *fontUsageInfo) map[byte]bool {
	if usage == nil || len(usage.charCodes) == 0 {
		return nil
	}
	subtype := fontSubtype(ctx, doc, fontDict)
	if subtype != "Type1" && subtype != "MMType1" {
		return nil
	}
	name := programGlyphName(ctx, doc, fontDict)
	if name == nil {
		return nil
	}
	enc := simpleEncoding{table: type1EncodingTable(ctx, doc, fontDict), differences: fontDifferencesNames(ctx, doc, fontDict)}
	mapped := toUnicodeMappedCodes(ctx, doc, fontDict)
	var out map[byte]bool
	for _, code := range sortedByteKeys(usage.charCodes) {
		if mapped[int(code)] {
			continue
		}
		if _, _, named := enc.glyph(code); named {
			continue
		}
		if name(code) == ".notdef" {
			if out == nil {
				out = make(map[byte]bool)
			}
			out[code] = true
		}
	}
	return out
}

// programGlyphName returns the name an embedded Type 1 font's program gives a
// code, as veraPDF's FontProgram.getGlyphName reads it, or nil when the font
// embeds no program veraPDF reads (PDType1Font.getFontProgram tries /FontFile,
// then a /FontFile3 of /Subtype /Type1C) or one whose encoding litho cannot
// read. An empty name is a code the program names nothing.
func programGlyphName(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) func(byte) string {
	fd := document.ResolveFontDescriptor(ctx, doc, fontDict)
	if fd == nil || !isFontEmbedded(ctx, doc, fd, fontSubtype(ctx, doc, fontDict), 0) {
		return nil
	}
	if obj, ok := fd["FontFile"]; ok {
		data := readableProgram(ctx, doc, "FontFile", obj)
		if data == nil {
			return nil
		}
		return type1ProgramGlyphName(data)
	}
	obj, ok := fd["FontFile3"]
	if !ok {
		return nil
	}
	stream, err := document.ResolveStream(ctx, doc, obj)
	if err != nil {
		return nil
	}
	if sub, _ := document.ResolveName(ctx, doc, stream.Dict["Subtype"]); sub != "Type1C" {
		return nil
	}
	data := readableProgram(ctx, doc, "FontFile3", obj)
	if data == nil || isSfntFontProgram(data) {
		return nil
	}
	return cffProgramGlyphName(data)
}

// readableProgram returns the decoded program a font file entry holds, or nil
// when it holds none [streamHoldsFontProgram] accepts.
func readableProgram(ctx context.Context, doc *document.Document, key ast.PdfName, obj ast.PdfObject) []byte {
	stream, err := document.ResolveStream(ctx, doc, obj)
	if err != nil || !streamHoldsFontProgram(ctx, doc, key, obj, stream) {
		return nil
	}
	data, err := decodeFontProgram(ctx, doc, obj, stream)
	if err != nil || len(data) == 0 {
		return nil
	}
	return data
}

// type1ProgramGlyphName names a code through a Type 1 program's built-in
// encoding as Type1FontProgram.getGlyphName does: the StandardEncoding table,
// whose undefined codes it names .notdef, when the program refers to it, and
// otherwise the vector the program builds, whose unlisted codes are .notdef
// when the program filled it with .notdef first and nothing when it did not.
// A reference to any other predefined encoding builds no vector veraPDF reads,
// so it names no code.
func type1ProgramGlyphName(data []byte) func(byte) string {
	builtin := typography.ExtractType1BuiltinEncoding(data)
	if builtin[0] != "" && builtin[0] == builtin[len(builtin)-1] && isPredefinedReference(builtin) {
		if builtin[0] != "StandardEncoding" {
			return func(byte) string { return "" }
		}
		standard := predefinedEncodingNames("StandardEncoding")
		return func(code byte) string {
			if name, ok := standard[code]; ok {
				return name
			}
			return ".notdef"
		}
	}
	filled := typography.ExtractType1NotdefFilled(data)
	return func(code byte) string {
		if name := builtin[code]; name != "" {
			return name
		}
		if filled {
			return ".notdef"
		}
		return ""
	}
}

// isPredefinedReference reports whether builtin is the form
// [typography.ExtractType1BuiltinEncoding] gives a program that refers to a
// predefined encoding: the encoding's name at every code.
func isPredefinedReference(builtin [256]string) bool {
	for _, name := range builtin {
		if name != builtin[0] {
			return false
		}
	}
	switch builtin[0] {
	case "StandardEncoding", "ISOLatin1Encoding", "MacRomanEncoding", "WinAnsiEncoding", "MacExpertEncoding", "SymbolEncoding":
		return true
	}
	return false
}

// cffProgramGlyphName names a code through a bare CFF's encoding as
// CFFType1FontProgram.getGlyphName does: the Standard or Expert table the Top
// DICT selects, read whole, or the program's own encoding; a code either
// leaves without a glyph is .notdef. It returns nil when the program states no
// encoding litho can read.
func cffProgramGlyphName(data []byte) func(byte) string {
	names := typography.ExtractCFFPredefinedEncodingNames(data)
	if names == nil {
		names = typography.ExtractCFFEncodingNames(data)
	}
	if len(names) == 0 {
		return nil
	}
	return func(code byte) string {
		if name, ok := names[code]; ok && name != "" {
			return name
		}
		return ".notdef"
	}
}
