package truetype

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
)

// TableRecord is an entry in the TrueType/OpenType table directory.
type TableRecord struct {
	Tag    string
	Offset uint32
	Length uint32
}

// ParseTableDirectory reads the font's table directory to locate tables by tag.
func ParseTableDirectory(data []byte) ([]TableRecord, error) {
	if len(data) < 12 {
		return nil, fmt.Errorf("%w: data too short for header", ErrInvalidFont)
	}

	numTables := int(binary.BigEndian.Uint16(data[4:6]))
	headerSize := 12 + numTables*16
	if len(data) < headerSize {
		return nil, fmt.Errorf("%w: data too short for table directory", ErrInvalidFont)
	}

	records := make([]TableRecord, numTables)
	for i := range numTables {
		off := 12 + i*16
		records[i] = TableRecord{
			Tag:    string(data[off : off+4]),
			Offset: binary.BigEndian.Uint32(data[off+8 : off+12]),
			Length: binary.BigEndian.Uint32(data[off+12 : off+16]),
		}
	}
	return records, nil
}

// TableData extracts the raw bytes for a table by tag.
func TableData(data []byte, records []TableRecord, tag string) ([]byte, error) {
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

// GlyphWidths extracts advance widths from the hmtx (Horizontal Metrics)
// table using the numberOfHMetrics field from the hhea (Horizontal Header)
// table. Returns a GID → advance width map in font design units.
func GlyphWidths(ctx context.Context, data []byte) (map[uint16]uint16, error) {
	records, err := ParseTableDirectory(data)
	if err != nil {
		return nil, err
	}

	hheaData, err := TableData(data, records, "hhea")
	if err != nil {
		return nil, err
	}
	if len(hheaData) < 36 {
		return nil, fmt.Errorf("%w: hhea table too short", ErrInvalidFont)
	}
	numberOfHMetrics := int(binary.BigEndian.Uint16(hheaData[34:36]))

	hmtxData, err := TableData(data, records, "hmtx")
	if err != nil {
		return nil, err
	}

	// Each long horizontal metric is 4 bytes: uint16 advanceWidth + int16 lsb.
	if len(hmtxData) < numberOfHMetrics*4 {
		return nil, fmt.Errorf("%w: hmtx table too short", ErrInvalidFont)
	}

	result := make(map[uint16]uint16, numberOfHMetrics)
	var lastAdvance uint16
	for i := range numberOfHMetrics {
		advance := binary.BigEndian.Uint16(hmtxData[i*4 : i*4+2])
		result[uint16(i)] = advance //nolint:gosec // GID fits in uint16 for TrueType
		lastAdvance = advance
	}

	// Glyphs beyond numberOfHMetrics share the last advance width.
	// Detect total glyph count from maxp table if available.
	if maxpData, err := TableData(data, records, "maxp"); err == nil && len(maxpData) >= 6 {
		numGlyphs := int(binary.BigEndian.Uint16(maxpData[4:6]))
		for gid := numberOfHMetrics; gid < numGlyphs; gid++ {
			result[uint16(gid)] = lastAdvance //nolint:gosec // GID fits in uint16 for TrueType
		}
	}

	return result, nil
}

// EmptyGIDs returns the set of GIDs that have zero-length glyph data in
// a TrueType font. It parses the loca table (using the indexToLocFormat
// from the head table) to find GIDs where loca[gid] == loca[gid+1],
// indicating an empty/absent glyph outline (renders as .notdef).
func EmptyGIDs(ctx context.Context, data []byte) (map[uint16]bool, error) {
	records, err := ParseTableDirectory(data)
	if err != nil {
		return nil, err
	}

	headData, err := TableData(data, records, "head")
	if err != nil {
		return nil, err
	}
	if len(headData) < 54 { //nolint:mnd
		return nil, fmt.Errorf("%w: head table too short", ErrInvalidFont)
	}
	indexToLocFormat := int16(binary.BigEndian.Uint16(headData[50:52])) //nolint:gosec // intentional two's-complement conversion

	locaData, err := TableData(data, records, "loca")
	if err != nil {
		return nil, err
	}

	maxpData, err := TableData(data, records, "maxp")
	if err != nil {
		return nil, err
	}
	if len(maxpData) < 6 { //nolint:mnd
		return nil, fmt.Errorf("%w: maxp table too short", ErrInvalidFont)
	}
	numGlyphs := int(binary.BigEndian.Uint16(maxpData[4:6]))

	result := make(map[uint16]bool)
	switch indexToLocFormat {
	case 0: // Short format: offsets are uint16, actual offset = value * 2.
		if len(locaData) < (numGlyphs+1)*2 {
			return nil, fmt.Errorf("%w: loca table too short for short format", ErrInvalidFont)
		}
		for gid := range numGlyphs {
			off0 := binary.BigEndian.Uint16(locaData[gid*2:])
			off1 := binary.BigEndian.Uint16(locaData[gid*2+2:])
			if off0 == off1 {
				result[uint16(gid)] = true //nolint:gosec
			}
		}
	case 1: // Long format: offsets are uint32.
		if len(locaData) < (numGlyphs+1)*4 {
			return nil, fmt.Errorf("%w: loca table too short for long format", ErrInvalidFont)
		}
		for gid := range numGlyphs {
			off0 := binary.BigEndian.Uint32(locaData[gid*4:])
			off1 := binary.BigEndian.Uint32(locaData[gid*4+4:])
			if off0 == off1 {
				result[uint16(gid)] = true //nolint:gosec
			}
		}
	}
	return result, nil
}

// CharCodeToGID extracts a character code → glyph ID mapping from the cmap
// table. This is the inverse of GlyphToUnicode: it maps Unicode code points
// (used as character codes in PDF TrueType fonts) to glyph IDs.
func CharCodeToGID(ctx context.Context, data []byte) (map[uint16]uint16, error) {
	gidToRune, err := GlyphToUnicode(ctx, data)
	if err != nil {
		return nil, err
	}

	result := make(map[uint16]uint16, len(gidToRune))
	for gid, r := range gidToRune {
		if r >= 0 && r <= 0xFFFF {
			charCode := uint16(r) //nolint:gosec // BMP code point fits in uint16
			// Only store first mapping per charCode.
			if _, exists := result[charCode]; !exists {
				result[charCode] = gid
			}
		}
	}
	return result, nil
}

// GlyphToUnicode extracts a glyph ID → Unicode rune mapping from embedded
// TrueType/OpenType font data. It parses the cmap table looking for a
// Unicode-compatible subtable (platform 3/encoding 1 Format 4, or
// platform 3/encoding 10 Format 12, or platform 0 subtables).
//
// The returned map keys are glyph IDs (GIDs) and values are Unicode runes.
func GlyphToUnicode(ctx context.Context, data []byte) (map[uint16]rune, error) {
	records, err := ParseTableDirectory(data)
	if err != nil {
		return nil, err
	}

	cmapData, err := TableData(data, records, "cmap")
	if err != nil {
		return nil, err
	}

	return parseCmap(ctx, cmapData)
}

// PostGlyphNames extracts glyph names from the post (PostScript) table.
// Returns a map of glyph ID → glyph name. Only post table formats 1.0 and
// 2.0 are supported; other formats return an empty map.
func PostGlyphNames(ctx context.Context, data []byte) (map[uint16]string, error) {
	records, err := ParseTableDirectory(data)
	if err != nil {
		return nil, err
	}

	postData, err := TableData(data, records, "post")
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
func parseCmap(ctx context.Context, data []byte) (map[uint16]rune, error) {
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
			priority = 4 // best: full Unicode (Format 12)
		case pid == 3 && eid == 1:
			priority = 3 // good: Windows BMP (Format 4)
		case pid == 0:
			priority = 2 // acceptable: Unicode platform
		case pid == 1 && eid == 0:
			priority = 1 // fallback: Macintosh Roman (Format 0/6)
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
	case 0:
		return parseCmapFormat0(subtable)
	case 4:
		return parseCmapFormat4(ctx, subtable)
	case 6:
		return parseCmapFormat6(subtable)
	case 12:
		return parseCmapFormat12(ctx, subtable)
	default:
		return nil, fmt.Errorf("%w: unsupported cmap format %d", ErrInvalidFont, format)
	}
}

// parseCmapFormat0 decodes a Format 0 "Byte encoding table" subtable.
// It contains exactly 256 GID entries, one per byte value. This is the simplest
// cmap format, commonly found in platform 1 (Macintosh) subtables.
func parseCmapFormat0(data []byte) (map[uint16]rune, error) {
	// Format 0: uint16 format, uint16 length, uint16 language, uint8[256] glyphIdArray
	if len(data) < 262 { //nolint:mnd // 6 byte header + 256 GID bytes
		return nil, fmt.Errorf("%w: format 0 data too short", ErrInvalidFont)
	}

	result := make(map[uint16]rune)
	for code := range 256 {
		gid := uint16(data[6+code])
		if gid != 0 {
			if _, exists := result[gid]; !exists {
				result[gid] = rune(code)
			}
		}
	}
	return result, nil
}

// parseCmapFormat6 decodes a Format 6 "Trimmed table mapping" subtable.
// It maps a contiguous range of character codes starting at firstCode,
// with entryCount entries in the glyphIdArray.
func parseCmapFormat6(data []byte) (map[uint16]rune, error) {
	// Format 6: uint16 format, uint16 length, uint16 language,
	//           uint16 firstCode, uint16 entryCount, uint16[] glyphIdArray
	if len(data) < 10 { //nolint:mnd // minimum header size
		return nil, fmt.Errorf("%w: format 6 header too short", ErrInvalidFont)
	}

	firstCode := int(binary.BigEndian.Uint16(data[6:8]))
	entryCount := int(binary.BigEndian.Uint16(data[8:10]))

	if len(data) < 10+entryCount*2 {
		return nil, fmt.Errorf("%w: format 6 data truncated", ErrInvalidFont)
	}

	result := make(map[uint16]rune)
	for i := range entryCount {
		gid := binary.BigEndian.Uint16(data[10+i*2 : 10+i*2+2])
		if gid != 0 {
			code := firstCode + i
			if _, exists := result[gid]; !exists {
				result[gid] = rune(code) //nolint:gosec // code is within Unicode range
			}
		}
	}
	return result, nil
}

// parseCmapFormat4 decodes a Format 4 "Segment mapping to delta values" subtable.
// This maps BMP character codes (U+0000–U+FFFF) to glyph IDs.
func parseCmapFormat4(ctx context.Context, data []byte) (map[uint16]rune, error) {
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
		if err := ctx.Err(); err != nil {
			return nil, err
		}
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
func parseCmapFormat12(ctx context.Context, data []byte) (map[uint16]rune, error) {
	if len(data) < 16 {
		return nil, fmt.Errorf("%w: format 12 header too short", ErrInvalidFont)
	}

	numGroups := int(binary.BigEndian.Uint32(data[12:16]))
	if len(data) < 16+numGroups*12 {
		return nil, fmt.Errorf("%w: format 12 data truncated", ErrInvalidFont)
	}

	result := make(map[uint16]rune)

	for i := range numGroups {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
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

// CmapSubtableCount returns the number of encoding subtables in the cmap
// table of a TrueType font program. Per ISO 19005-1 §6.3.7, symbolic
// TrueType fonts must contain exactly one encoding.
func CmapSubtableCount(data []byte) (int, error) {
	records, err := ParseTableDirectory(data)
	if err != nil {
		return 0, err
	}
	cmapData, err := TableData(data, records, "cmap")
	if err != nil {
		return 0, err
	}
	if len(cmapData) < 4 {
		return 0, fmt.Errorf("%w: cmap table too short", ErrInvalidFont)
	}
	return int(binary.BigEndian.Uint16(cmapData[2:4])), nil
}
