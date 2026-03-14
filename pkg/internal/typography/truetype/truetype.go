// Package truetype provides a lightweight binary parser for TrueType/OpenType
// font files, extracting only the cmap (character-to-glyph mapping) and post
// (PostScript glyph names) tables needed for Unicode fallback in PDF text
// extraction. Glyph outlines are not parsed.
package truetype

import (
	"encoding/binary"
	"errors"
	"fmt"
)

var (
	// ErrInvalidFont indicates the font data is structurally invalid.
	ErrInvalidFont = errors.New("invalid TrueType/OpenType font")

	// ErrTableNotFound indicates a required table was not found.
	ErrTableNotFound = errors.New("font table not found")
)

// tableRecord is an entry in the TrueType/OpenType table directory.
type tableRecord struct {
	Tag    string
	Offset uint32
	Length uint32
}

// parseTableDirectory reads the font's table directory to locate tables by tag.
func parseTableDirectory(data []byte) ([]tableRecord, error) {
	if len(data) < 12 {
		return nil, fmt.Errorf("%w: data too short for header", ErrInvalidFont)
	}

	numTables := int(binary.BigEndian.Uint16(data[4:6]))
	headerSize := 12 + numTables*16
	if len(data) < headerSize {
		return nil, fmt.Errorf("%w: data too short for table directory", ErrInvalidFont)
	}

	records := make([]tableRecord, numTables)
	for i := range numTables {
		off := 12 + i*16
		records[i] = tableRecord{
			Tag:    string(data[off : off+4]),
			Offset: binary.BigEndian.Uint32(data[off+8 : off+12]),
			Length: binary.BigEndian.Uint32(data[off+12 : off+16]),
		}
	}
	return records, nil
}

// tableData extracts the raw bytes for a table by tag.
func tableData(data []byte, records []tableRecord, tag string) ([]byte, error) {
	for _, r := range records {
		if r.Tag == tag {
			end := uint64(r.Offset) + uint64(r.Length)
			if end > uint64(len(data)) {
				return nil, fmt.Errorf("%w: table %q extends beyond data", ErrInvalidFont, tag)
			}
			return data[r.Offset:end], nil
		}
	}
	return nil, fmt.Errorf("%w: %q", ErrTableNotFound, tag)
}

// GlyphToUnicode extracts a glyph ID → Unicode rune mapping from embedded
// TrueType/OpenType font data. It parses the cmap table looking for a
// Unicode-compatible subtable (platform 3/encoding 1 Format 4, or
// platform 3/encoding 10 Format 12, or platform 0 subtables).
//
// The returned map keys are glyph IDs (GIDs) and values are Unicode runes.
func GlyphToUnicode(data []byte) (map[uint16]rune, error) {
	records, err := parseTableDirectory(data)
	if err != nil {
		return nil, err
	}

	cmapData, err := tableData(data, records, "cmap")
	if err != nil {
		return nil, err
	}

	return parseCmap(cmapData)
}

// PostGlyphNames extracts glyph names from the post (PostScript) table.
// Returns a map of glyph ID → glyph name. Only post table formats 1.0 and
// 2.0 are supported; other formats return an empty map.
func PostGlyphNames(data []byte) (map[uint16]string, error) {
	records, err := parseTableDirectory(data)
	if err != nil {
		return nil, err
	}

	postData, err := tableData(data, records, "post")
	if err != nil {
		// post table is optional for our purposes.
		if errors.Is(err, ErrTableNotFound) {
			return nil, nil
		}
		return nil, err
	}

	return parsePost(postData)
}

// parseCmap processes the cmap table and returns a GID → rune map.
func parseCmap(data []byte) (map[uint16]rune, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("%w: cmap table too short", ErrInvalidFont)
	}

	numSubtables := int(binary.BigEndian.Uint16(data[2:4]))
	if len(data) < 4+numSubtables*8 {
		return nil, fmt.Errorf("%w: cmap subtable records truncated", ErrInvalidFont)
	}

	// Prefer platform 3/encoding 10 (Format 12, full Unicode), then
	// platform 3/encoding 1 (Format 4, BMP), then platform 0.
	type candidate struct {
		platformID uint16
		encodingID uint16
		offset     uint32
		priority   int
	}

	var candidates []candidate
	for i := range numSubtables {
		off := 4 + i*8
		pid := binary.BigEndian.Uint16(data[off : off+2])
		eid := binary.BigEndian.Uint16(data[off+2 : off+4])
		subtableOff := binary.BigEndian.Uint32(data[off+4 : off+8])

		var priority int
		switch {
		case pid == 3 && eid == 10:
			priority = 3 // best: full Unicode
		case pid == 3 && eid == 1:
			priority = 2 // good: BMP
		case pid == 0:
			priority = 1 // acceptable: Unicode platform
		default:
			continue
		}

		candidates = append(candidates, candidate{pid, eid, subtableOff, priority})
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("%w: no Unicode-compatible cmap subtable found", ErrInvalidFont)
	}

	// Sort by priority descending.
	best := candidates[0]
	for _, c := range candidates[1:] {
		if c.priority > best.priority {
			best = c
		}
	}

	if int(best.offset) >= len(data) {
		return nil, fmt.Errorf("%w: cmap subtable offset out of bounds", ErrInvalidFont)
	}

	subtable := data[best.offset:]
	if len(subtable) < 2 {
		return nil, fmt.Errorf("%w: cmap subtable too short", ErrInvalidFont)
	}

	format := binary.BigEndian.Uint16(subtable[0:2])

	switch format {
	case 4:
		return parseCmapFormat4(subtable)
	case 12:
		return parseCmapFormat12(subtable)
	default:
		return nil, fmt.Errorf("%w: unsupported cmap format %d", ErrInvalidFont, format)
	}
}

// parseCmapFormat4 decodes a Format 4 "Segment mapping to delta values" subtable.
// This maps BMP character codes (U+0000–U+FFFF) to glyph IDs.
func parseCmapFormat4(data []byte) (map[uint16]rune, error) {
	if len(data) < 14 {
		return nil, fmt.Errorf("%w: format 4 header too short", ErrInvalidFont)
	}

	segCount := int(binary.BigEndian.Uint16(data[6:8])) / 2
	headerLen := 14 + segCount*8 + 2 // +2 for reservedPad
	if len(data) < headerLen {
		return nil, fmt.Errorf("%w: format 4 data truncated", ErrInvalidFont)
	}

	// Arrays start after the fixed header (14 bytes).
	endCountOff := 14
	// +2 for reservedPad after endCount array
	startCountOff := endCountOff + segCount*2 + 2
	idDeltaOff := startCountOff + segCount*2
	idRangeOffsetOff := idDeltaOff + segCount*2

	result := make(map[uint16]rune)

	for i := range segCount {
		endCode := binary.BigEndian.Uint16(data[endCountOff+i*2 : endCountOff+i*2+2])
		startCode := binary.BigEndian.Uint16(data[startCountOff+i*2 : startCountOff+i*2+2])
		idDelta := int16(binary.BigEndian.Uint16(data[idDeltaOff+i*2 : idDeltaOff+i*2+2])) //nolint:gosec // TrueType spec: delta is signed
		rangeOff := binary.BigEndian.Uint16(data[idRangeOffsetOff+i*2 : idRangeOffsetOff+i*2+2])

		if startCode == 0xFFFF {
			break
		}

		for code := uint32(startCode); code <= uint32(endCode); code++ {
			var gid uint16
			if rangeOff == 0 {
				gid = uint16(int32(code) + int32(idDelta)) //nolint:gosec // TrueType spec: modular arithmetic
			} else {
				// Index into glyphIdArray:
				// rangeOff/2 + (code - startCode) + &idRangeOffset[i] - &idRangeOffset[0]
				glyphIdx := int(rangeOff)/2 + int(code) - int(startCode) + i
				byteOff := idRangeOffsetOff + glyphIdx*2
				if byteOff+2 > len(data) {
					continue
				}
				gid = binary.BigEndian.Uint16(data[byteOff : byteOff+2])
				if gid != 0 {
					gid = uint16(int32(gid) + int32(idDelta)) //nolint:gosec // TrueType spec: modular arithmetic
				}
			}

			if gid != 0 {
				// Map GID → Unicode code point. Only store first mapping per GID.
				if _, exists := result[gid]; !exists {
					result[gid] = rune(code) //nolint:gosec // BMP code point fits in int32
				}
			}
		}
	}

	return result, nil
}

// parseCmapFormat12 decodes a Format 12 "Segmented coverage" subtable.
// This handles the full Unicode range (beyond BMP).
func parseCmapFormat12(data []byte) (map[uint16]rune, error) {
	if len(data) < 16 {
		return nil, fmt.Errorf("%w: format 12 header too short", ErrInvalidFont)
	}

	numGroups := int(binary.BigEndian.Uint32(data[12:16]))
	if len(data) < 16+numGroups*12 {
		return nil, fmt.Errorf("%w: format 12 data truncated", ErrInvalidFont)
	}

	result := make(map[uint16]rune)

	for i := range numGroups {
		off := 16 + i*12
		startCharCode := binary.BigEndian.Uint32(data[off : off+4])
		endCharCode := binary.BigEndian.Uint32(data[off+4 : off+8])
		startGlyphID := binary.BigEndian.Uint32(data[off+8 : off+12])

		for code := startCharCode; code <= endCharCode; code++ {
			gid := uint16(startGlyphID + code - startCharCode) //nolint:gosec // GID narrowing is expected per spec
			if gid != 0 {
				if _, exists := result[gid]; !exists {
					result[gid] = rune(code) //nolint:gosec // Unicode code point fits in int32
				}
			}
		}
	}

	return result, nil
}

// Standard Macintosh glyph names for post format 1.0 (258 names).
// Per the Apple TrueType spec, format 1.0 uses this fixed order.
var macGlyphNames = [258]string{
	".notdef", ".null", "nonmarkingreturn", "space", "exclam", "quotedbl", "numbersign",
	"dollar", "percent", "ampersand", "quotesingle", "parenleft", "parenright", "asterisk",
	"plus", "comma", "hyphen", "period", "slash", "zero", "one", "two", "three", "four",
	"five", "six", "seven", "eight", "nine", "colon", "semicolon", "less", "equal",
	"greater", "question", "at", "A", "B", "C", "D", "E", "F", "G", "H", "I", "J", "K",
	"L", "M", "N", "O", "P", "Q", "R", "S", "T", "U", "V", "W", "X", "Y", "Z",
	"bracketleft", "backslash", "bracketright", "asciicircum", "underscore", "grave",
	"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l", "m", "n", "o", "p",
	"q", "r", "s", "t", "u", "v", "w", "x", "y", "z", "braceleft", "bar", "braceright",
	"asciitilde", "Adieresis", "Aring", "Ccedilla", "Eacute", "Ntilde", "Odieresis",
	"Udieresis", "aacute", "agrave", "acircumflex", "adieresis", "atilde", "aring",
	"ccedilla", "eacute", "egrave", "ecircumflex", "edieresis", "iacute", "igrave",
	"icircumflex", "idieresis", "ntilde", "oacute", "ograve", "ocircumflex", "odieresis",
	"otilde", "uacute", "ugrave", "ucircumflex", "udieresis", "dagger", "degree", "cent",
	"sterling", "section", "bullet", "paragraph", "germandbls", "registered", "copyright",
	"trademark", "acute", "dieresis", "notequal", "AE", "Oslash", "infinity", "plusminus",
	"lessequal", "greaterequal", "yen", "mu", "partialdiff", "summation", "product", "pi",
	"integral", "ordfeminine", "ordmasculine", "Omega", "ae", "oslash", "questiondown",
	"exclamdown", "logicalnot", "radical", "florin", "approxequal", "Delta", "guillemotleft",
	"guillemotright", "ellipsis", "nonbreakingspace", "Agrave", "Atilde", "Otilde", "OE",
	"oe", "endash", "emdash", "quotedblleft", "quotedblright", "quoteleft", "quoteright",
	"divide", "lozenge", "ydieresis", "Ydieresis", "fraction", "currency", "guilsinglleft",
	"guilsinglright", "fi", "fl", "daggerdbl", "periodcentered", "quotesinglbase",
	"quotedblbase", "perthousand", "Acircumflex", "Ecircumflex", "Aacute", "Edieresis",
	"Egrave", "Iacute", "Icircumflex", "Idieresis", "Igrave", "Oacute", "Ocircumflex",
	"apple", "Ograve", "Uacute", "Ucircumflex", "Ugrave", "dotlessi", "circumflex",
	"tilde", "macron", "breve", "dotaccent", "ring", "cedilla", "hungarumlaut", "ogonek",
	"caron", "Lslash", "lslash", "Scaron", "scaron", "Zcaron", "zcaron", "brokenbar",
	"Eth", "eth", "Yacute", "yacute", "Thorn", "thorn", "minus", "multiply", "onesuperior",
	"twosuperior", "threesuperior", "onehalf", "onequarter", "threequarters", "franc",
	"Gbreve", "gbreve", "Idotaccent", "Scedilla", "scedilla", "Cacute", "cacute", "Ccaron",
	"ccaron", "dcroat",
}

// parsePost extracts glyph names from the post table.
func parsePost(data []byte) (map[uint16]string, error) {
	if len(data) < 32 {
		return nil, fmt.Errorf("%w: post table too short", ErrInvalidFont)
	}

	// Version is a Fixed (16.16).
	major := binary.BigEndian.Uint16(data[0:2])
	minor := binary.BigEndian.Uint16(data[2:4])

	switch {
	case major == 1 && minor == 0:
		// Format 1.0: uses the standard 258 Macintosh glyph names.
		result := make(map[uint16]string, 258)
		for i, name := range macGlyphNames {
			result[uint16(i)] = name
		}
		return result, nil

	case major == 2 && minor == 0:
		return parsePostFormat2(data)

	default:
		// Format 3.0 or other: no names available.
		return nil, nil
	}
}

// parsePostFormat2 decodes post table format 2.0.
func parsePostFormat2(data []byte) (map[uint16]string, error) {
	if len(data) < 34 {
		return nil, fmt.Errorf("%w: post format 2.0 too short", ErrInvalidFont)
	}

	numGlyphs := int(binary.BigEndian.Uint16(data[32:34]))
	indexArrayEnd := 34 + numGlyphs*2
	if len(data) < indexArrayEnd {
		return nil, fmt.Errorf("%w: post format 2.0 glyph index array truncated", ErrInvalidFont)
	}

	// Read the name index array.
	nameIndices := make([]uint16, numGlyphs)
	for i := range numGlyphs {
		nameIndices[i] = binary.BigEndian.Uint16(data[34+i*2 : 34+i*2+2])
	}

	// Read the string data (Pascal strings) that follow the index array.
	var extraNames []string
	pos := indexArrayEnd
	for pos < len(data) {
		nameLen := int(data[pos])
		pos++
		if pos+nameLen > len(data) {
			break
		}
		extraNames = append(extraNames, string(data[pos:pos+nameLen]))
		pos += nameLen
	}

	result := make(map[uint16]string, numGlyphs)
	for i, idx := range nameIndices {
		if idx < 258 {
			result[uint16(i)] = macGlyphNames[idx]
		} else {
			extraIdx := int(idx) - 258
			if extraIdx < len(extraNames) {
				result[uint16(i)] = extraNames[extraIdx]
			}
		}
	}

	return result, nil
}
