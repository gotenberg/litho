package compliance

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// type1EncodingTable names the predefined encoding a Type 1 font's /Encoding
// takes its codes from, as a name or as the /BaseEncoding of a dictionary, when
// it is one of the three ISO 32000-1, 9.6.6.1 lists for the entry:
// MacRomanEncoding, MacExpertEncoding or WinAnsiEncoding. It returns "" for
// anything else, StandardEncoding included. veraPDF's Encoding builds a table
// for those three names only, so under any other base a code /Differences
// leaves alone takes no name from the dictionary at all.
func type1EncodingTable(ctx context.Context, doc *document.Document, fontDict ast.PdfDict) string {
	encObj, ok := fontDict["Encoding"]
	if !ok {
		return ""
	}
	name, err := document.ResolveName(ctx, doc, encObj)
	if err != nil || name == "" {
		encDict, derr := document.ResolveDict(ctx, doc, encObj)
		if derr != nil || encDict == nil {
			return ""
		}
		name, _ = document.ResolveName(ctx, doc, encDict["BaseEncoding"])
	}
	switch name {
	case "MacRomanEncoding", "MacExpertEncoding", "WinAnsiEncoding":
		return string(name)
	}
	return ""
}

// macRomanUndefined holds the codes above the control range that
// MacRomanEncoding leaves undefined in ISO 32000-1, Annex D, D.2: DEL and the
// fifteen codes Mac OS Roman spends on symbols the PDF encoding does not carry
// (notequal, infinity, lessequal, greaterequal, partialdiff, summation, product,
// pi, integral, Omega, radical, approxequal, Delta, lozenge, apple). litho's
// MacRoman table maps them for text extraction; veraPDF's names them .notdef.
var macRomanUndefined = codeSet(0x7F, 0xAD, 0xB0, 0xB2, 0xB3, 0xB6, 0xB7, 0xB8, 0xB9, 0xBA, 0xBD, 0xC3, 0xC5, 0xC6, 0xD7, 0xF0)

// macExpertUndefined holds the codes above the control range that
// MacExpertEncoding leaves undefined in ISO 32000-1, Annex D, D.4, as veraPDF's
// table leaves them. litho's own MacExpert table stores runes and loses names,
// so it cannot answer this.
var macExpertUndefined = codeSet(
	0x3C, 0x3E, 0x40, 0x41, 0x42, 0x43, 0x45, 0x46, 0x50, 0x51, 0x52, 0x53, 0x54, 0x55, 0x5C, 0x7F,
	0x80, 0x83, 0x84, 0x85, 0x86, 0xA0, 0xA8, 0xAB, 0xAD, 0xB1, 0xB5, 0xB7, 0xB8, 0xBA, 0xC3, 0xC4,
	0xC5, 0xC6, 0xC7, 0xC8, 0xCA, 0xCB, 0xCC, 0xCD, 0xCE, 0xD2, 0xD3, 0xD4, 0xD5, 0xD7, 0xD9, 0xE3,
	0xE7, 0xE8, 0xEC, 0xED, 0xEE, 0xEF, 0xF0, 0xFC, 0xFD, 0xFE, 0xFF,
)

// codeSet returns the set of the given character codes.
func codeSet(codes ...byte) map[byte]bool {
	set := make(map[byte]bool, len(codes))
	for _, c := range codes {
		set[c] = true
	}
	return set
}

// type1UndefinedCode reports whether the predefined table [type1EncodingTable]
// names leaves code without a glyph, so a Type 1 font taking the code from it
// shows .notdef. None of the three defines a control code below 32 (ISO
// 32000-1, Annex D). WinAnsiEncoding maps every other unused code to the bullet
// (a note to Annex D, Table D.2), so it defines the rest. An empty table names
// nothing, so nothing is undefined in it.
func type1UndefinedCode(table string, code byte) bool {
	if table == "" {
		return false
	}
	if code < 0x20 {
		return true
	}
	switch table {
	case "MacRomanEncoding":
		return macRomanUndefined[code]
	case "MacExpertEncoding":
		return macExpertUndefined[code]
	}
	return false
}

// type1WidthGlyphName returns the glyph whose advance a Type 1 font's width for
// code is judged against, the way veraPDF picks it (ISO 32000-1, 9.6.6.1 and
// 9.6.6.2). A /Differences name stands, a code the predefined base table leaves
// undefined shows .notdef, and a code no encoding names shows the program's
// built-in glyph. A code the built-in vector does not list names .notdef when
// the program fills the vector with .notdef first (fillsNotdef), and nothing
// otherwise. A name the PDF encoding gives is preferred when the program
// defines it, with the program's built-in name for the code as the fallback,
// since subset fonts routinely map codes to synthetic G-names.
//
// absentIsMissing reports whether a name the program does not define, or no
// name at all, is taken at the /MissingWidth, as veraPDF takes it. That holds
// wherever the name is the one veraPDF reads too; a name from a table whose
// names litho does not hold the way veraPDF does (StandardEncoding, which
// veraPDF reads through the program instead, or MacExpertEncoding) leaves the
// code unjudged. An empty name with absentIsMissing false means the code
// cannot be resolved at all.
func type1WidthGlyphName(code byte, pdfEnc, builtin, differences map[byte]string, table string, fillsNotdef bool, defined map[string]float64) (name string, absentIsMissing bool) {
	if dn, ok := differences[code]; ok {
		return dn, true
	}
	if type1UndefinedCode(table, code) {
		return ".notdef", true
	}
	if name = pdfEnc[code]; name == "" {
		if builtin == nil {
			return "", false
		}
		if name = builtin[code]; name == "" && fillsNotdef {
			name = ".notdef"
		}
		return name, true
	}
	if _, ok := defined[name]; !ok && builtin[code] != "" {
		name = builtin[code]
	}
	return name, table == "WinAnsiEncoding" || table == "MacRomanEncoding"
}
