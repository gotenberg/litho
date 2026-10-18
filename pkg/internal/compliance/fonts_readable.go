package compliance

import (
	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/typography/cff"
	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
	"github.com/gotenberg/litho/pkg/internal/typography/type1"
)

// fontProgramReadable reports whether a decoded font program, read from the
// font file key that holds it, is one veraPDF's parser completes.
//
// ISO 19005-2, 6.2.11.4.1 t1 asks that a font program be embedded, and
// veraPDF answers it with GFPDFont.getcontainsFontFile, getFontProgram() !=
// null && fontProgramParsed. fontProgramParsed is the program's own
// isSuccessfulParsing, so bytes in the right container that the parser cannot
// finish embed nothing it reads: the font is reported as not embedded, and no
// rule weighs its glyphs, its widths or its cmap (GFGlyph leaves them null for
// a program that did not parse). The same holds for ISO 19005-1, 6.3.4,
// ISO 19005-4, 6.2.10.4.1, ISO 14289-1, 7.21.4.1 and ISO 14289-2, 8.4.5.5.1.
//
// Only the failures veraPDF-parser 1.30 raises before any glyph is read are
// judged, each where the program first needs a structure:
//
//   - A Type 1 program (/FontFile) must define a glyph in its CharStrings
//     dictionary: Type1FontProgram.parseFont throws "Type 1 font doesn't
//     contain charstrings" when its private parser met no /CharStrings
//     entry, an eexec section it could not reach included.
//   - A bare CFF (/FontFile3 /Type1C or /CIDFontType0C) must lead to a
//     CharStrings INDEX: CFFFontProgram.parseFont throws on an empty Top
//     DICT INDEX, and the font program then seeks the CharStrings offset,
//     the header when the Top DICT states none ([cff.CharStringsReachable]).
//     A program whose first byte is not the CFF major version 1 is left
//     alone: veraPDF reads such a version, and litho judges no other.
//   - A TrueType program (/FontFile2, or an sfnt in /FontFile3) must carry
//     an 'hhea' and an 'hmtx' table: TrueTypeFontParser.readTables throws
//     without either, where a missing 'head', 'cmap' or 'maxp' takes a
//     default. An OpenType program with CFF outlines reads its 'CFF ' table
//     instead (OpenTypeFontProgram.getCFFTable), so an "OTTO" program reads
//     when either path does.
//
// A program litho can read that veraPDF cannot, or one it reads past damage
// veraPDF stops at, stays embedded: the census below found none.
//
// Measured with veraPDF 1.30.2 at PDF/A-2b on a Type 1 program with no eexec
// section, one whose private part defines no /CharStrings, a CFF whose Top
// DICT INDEX is empty, one whose Top DICT states no CharStrings, a TrueType
// with no table, with no 'hhea' and with no 'hmtx', and an "OTTO" with no
// table: each is reported as not embedded. A CFF with an empty Name INDEX, one
// with an empty CharStrings INDEX, one whose first byte reads 2, and a
// TrueType with no 'head' or no 'cmap' are embedded.
//
// # Corpus evidence
//
//   - All corpora: of 11124 programs, the only one litho counted as
//     embedded and this rejects is the one veraPDF reports. The other twelve
//     it rejects already counted as unembedded: PFB containers, TrueType
//     Collections, and CIDFontType0 programs in /FontFile.
//   - govdocs thread_300_300851.pdf: embeds CMDCGH+AdvMT_RM, a Type 1 program
//     whose eexec section follows a form feed, so neither parser finds its
//     CharStrings. veraPDF reports it under 6.2.11.4.1 t1, which litho did
//     not.
func fontProgramReadable(key ast.PdfName, data []byte) bool {
	switch key {
	case "FontFile":
		return len(type1.ParseCharStringNames(data)) > 0
	case "FontFile2":
		return sfntMetricsReadable(data)
	case "FontFile3":
		if isSfntFontProgram(data) {
			if string(data[:4]) == "OTTO" && openTypeCFFReadable(data) {
				return true
			}
			return sfntMetricsReadable(data)
		}
		if len(data) > 0 && data[0] == 1 {
			return cff.CharStringsReachable(data)
		}
	}
	return true
}

// sfntMetricsReadable reports whether an sfnt's table directory reads and lists
// the 'hhea' and 'hmtx' tables veraPDF's TrueType parser cannot do without.
func sfntMetricsReadable(data []byte) bool {
	records, err := truetype.ParseTableDirectory(data)
	if err != nil {
		return false
	}
	var hhea, hmtx bool
	for _, r := range records {
		switch r.Tag {
		case "hhea":
			hhea = true
		case "hmtx":
			hmtx = true
		}
	}
	return hhea && hmtx
}

// openTypeCFFReadable reports whether an OpenType program carries a 'CFF '
// table that leads to its CharStrings ([cff.CharStringsReachable]).
func openTypeCFFReadable(data []byte) bool {
	records, err := truetype.ParseTableDirectory(data)
	if err != nil {
		return false
	}
	table, err := truetype.TableData(data, records, "CFF ")
	if err != nil {
		return false
	}
	return len(table) > 0 && table[0] == 1 && cff.CharStringsReachable(table)
}
