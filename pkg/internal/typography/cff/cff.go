package cff

import (
	"context"
	"encoding/binary"
	"fmt"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/typography/encodings"
)

// maxCharstringStack caps the operand stack inside a Type 2 charstring.
// CFF1 specifies 48; CFF2 specifies 513 to accommodate variation-store
// "blend" subroutines. Using the larger ceiling lets legitimate CFF2
// charstrings through while rejecting adversarial fonts that push
// thousands of operands between stack-clearing operators to force an
// unbounded slice growth.
const maxCharstringStack = 513

// ExtractEncoding extracts the CFF encoding as a byte code → glyph name map.
// For fonts with a custom encoding (like TeX OT1), this maps byte positions
// to their glyph names, enabling correct text extraction when no PDF-level
// /Encoding is present. Predefined encodings (0=Standard, 1=Expert) are
// resolved by iterating the font's charset and pairing each glyph name with
// its position in the predefined encoding table. The result is a valid
// byte→name map even for subset fonts that only include a handful of
// Expert glyphs.
func ExtractEncoding(data []byte) map[byte]string {
	if len(data) < 4 || data[0] != 1 {
		return nil
	}
	hdrSize := max(int(data[2]), 4)
	nameEnd, err := skipINDEX(data, hdrSize)
	if err != nil {
		return nil
	}
	topDictEnd, topDictEntries, err := parseINDEX(data, nameEnd)
	if err != nil || len(topDictEntries) == 0 {
		return nil
	}
	info := parseTopDict(topDictEntries[0])

	// Parse custom strings.
	_, stringEntries, err := parseINDEX(data, topDictEnd)
	if err != nil {
		return nil
	}
	strings := &stringTable{custom: stringEntries}

	// Get number of glyphs for charset parsing.
	numGlyphs := 0
	if info.charStringsOffset > 0 && int(info.charStringsOffset) < len(data) {
		_, csEntries, err := parseINDEX(data, int(info.charStringsOffset))
		if err == nil {
			numGlyphs = len(csEntries)
		}
	}
	if numGlyphs == 0 {
		return nil
	}

	// Parse charset: GID → SID.
	sids, err := resolveCharset(data, int(info.charsetOffset), numGlyphs)
	if err != nil {
		return nil
	}

	// Predefined encodings: intersect the standard byte→name table with
	// the font's glyph set. Subset fonts that declare Expert (encoding 1)
	// typically ship only a handful of glyphs, and a naive byte→name
	// lookup on the full Expert table would surface names the font never
	// contains. Iterating the charset first ensures only the positions
	// whose glyph actually lives in this font populate the result.
	if info.encodingOffset == 0 || info.encodingOffset == 1 {
		var predefined []string
		if info.encodingOffset == 1 {
			predefined = expertEncodingNames[:]
		} else {
			predefined = standardEncodingNames[:]
		}
		result := make(map[byte]string)
		for gid, sid := range sids {
			if gid == 0 {
				continue // .notdef
			}
			name := strings.get(sid)
			if name == "" || name == ".notdef" {
				continue
			}
			for code, expertName := range predefined {
				if expertName == name {
					result[byte(code)] = name //nolint:gosec // code in [0,255]
					break
				}
			}
		}
		if len(result) == 0 {
			return nil
		}
		return result
	}

	// Parse CFF Encoding at encodingOffset.
	// Format 0: nCodes(1), code[nCodes](1 each). code[i] maps to GID i+1.
	// Format 1: nRanges(1), {first(1), nLeft(1)}[nRanges], range mappings.
	encOff := int(info.encodingOffset)
	if encOff < 0 || encOff >= len(data) {
		// A negative offset is malformed (predefined 0/1 are handled above);
		// guard it before indexing to avoid a panic on crafted input.
		return nil
	}
	formatByte := data[encOff]
	format := formatByte & 0x7F
	hasSupplement := formatByte&0x80 != 0
	encOff++

	result := make(map[byte]string)

	switch format {
	case 0:
		if encOff >= len(data) {
			return nil
		}
		nCodes := int(data[encOff])
		encOff++
		for i := 0; i < nCodes && encOff < len(data); i++ {
			code := data[encOff]
			encOff++
			gid := i + 1 // GID 0 = .notdef
			if gid < len(sids) {
				name := strings.get(sids[gid])
				if name != "" && name != ".notdef" {
					result[code] = name
				}
			}
		}
	case 1:
		if encOff >= len(data) {
			return nil
		}
		nRanges := int(data[encOff])
		encOff++
		gid := 1 // start after .notdef
		for i := 0; i < nRanges && encOff+1 < len(data); i++ {
			first := data[encOff]
			nLeft := int(data[encOff+1])
			encOff += 2
			for j := 0; j <= nLeft; j++ {
				code := first + byte(j) //nolint:gosec // j bounded by nLeft < 256
				if gid < len(sids) {
					name := strings.get(sids[gid])
					if name != "" && name != ".notdef" {
						result[code] = name
					}
				}
				gid++
			}
		}
	}

	// Supplemental encoding: when bit 7 of the format byte was set, a
	// supplement table follows the main encoding. Each entry maps an
	// additional byte code directly to a glyph SID, independent of
	// GID order. TeX CMR fonts rely on this to expose ligatures (ff,
	// fi, fl, ffi) at positions like 0xAE outside the primary range.
	if hasSupplement && encOff < len(data) {
		nSupp := int(data[encOff])
		encOff++
		for i := 0; i < nSupp && encOff+2 < len(data); i++ {
			code := data[encOff]
			sid := uint16(data[encOff+1])<<8 | uint16(data[encOff+2])
			encOff += 3
			name := strings.get(int(sid))
			if name != "" && name != ".notdef" {
				result[code] = name
			}
		}
	}

	return result
}

// PredefinedEncodingNames returns the complete byte code → glyph name mapping
// of the predefined encoding a CFF declares in its Top DICT: offset 0 selects
// the Standard encoding, offset 1 the Expert encoding. Returns nil when the
// program builds its own encoding, which [ExtractEncoding] already reports in
// full.
//
// Unlike [ExtractEncoding] this does not intersect the table with the font's
// charset. An encoding states which glyph a code selects whether or not a
// subset embeds that glyph, so a caller asking what a code MEANS wants the
// whole table; whether the glyph is present is a separate question, and
// intersecting the two hides a code whose meaning is perfectly well defined.
func PredefinedEncodingNames(data []byte) map[byte]string {
	if len(data) < 4 || data[0] != 1 {
		return nil
	}
	hdrSize := max(int(data[2]), 4)
	nameEnd, err := skipINDEX(data, hdrSize)
	if err != nil {
		return nil
	}
	_, topDictEntries, err := parseINDEX(data, nameEnd)
	if err != nil || len(topDictEntries) == 0 {
		return nil
	}

	var predefined []string
	switch parseTopDict(topDictEntries[0]).encodingOffset {
	case 0:
		predefined = standardEncodingNames[:]
	case 1:
		predefined = expertEncodingNames[:]
	default:
		return nil
	}

	result := make(map[byte]string, len(predefined))
	for code, name := range predefined {
		if name != "" {
			result[byte(code)] = name //nolint:gosec // code indexes a 256-entry table, 0-255
		}
	}
	return result
}

// GlyphToUnicode extracts a GID → Unicode rune mapping from CFF font data.
// It parses the charset to get GID → SID, resolves SIDs to glyph names via
// the standard + custom string tables, then maps glyph names to Unicode via
// the Adobe Glyph List.
func GlyphToUnicode(ctx context.Context, data []byte) (map[uint16]rune, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("%w: too short", ErrInvalidCFF)
	}

	// CFF header: major(1), minor(1), hdrSize(1), offSize(1).
	if data[0] != 1 {
		return nil, fmt.Errorf("%w: unsupported CFF major version %d", ErrInvalidCFF, data[0])
	}
	hdrSize := max(int(data[2]), 4)

	pos := hdrSize

	// Skip Name INDEX.
	nameEnd, err := skipINDEX(data, pos)
	if err != nil {
		return nil, fmt.Errorf("name INDEX: %w", err)
	}

	// Parse Top DICT INDEX to find charset and charstring offsets.
	topDictEnd, topDictEntries, err := parseINDEX(data, nameEnd)
	if err != nil {
		return nil, fmt.Errorf("top dict INDEX: %w", err)
	}
	if len(topDictEntries) == 0 {
		return nil, fmt.Errorf("%w: empty top dict INDEX", ErrInvalidCFF)
	}

	info := parseTopDict(topDictEntries[0])

	// Parse String INDEX (custom strings beyond SID 390).
	_, stringEntries, err := parseINDEX(data, topDictEnd)
	if err != nil {
		return nil, fmt.Errorf("string INDEX: %w", err)
	}

	// Build the string resolver.
	strings := &stringTable{custom: stringEntries}

	// Get number of glyphs from CharStrings INDEX.
	numGlyphs := 0
	if info.charStringsOffset > 0 && int(info.charStringsOffset) < len(data) {
		_, csEntries, err := parseINDEX(data, int(info.charStringsOffset))
		if err == nil {
			numGlyphs = len(csEntries)
		}
	}
	if numGlyphs == 0 {
		return nil, fmt.Errorf("%w: no charstrings", ErrInvalidCFF)
	}

	// Parse charset: GID → SID mapping.
	sids, err := resolveCharset(data, int(info.charsetOffset), numGlyphs)
	if err != nil {
		return nil, fmt.Errorf("charset: %w", err)
	}

	// For CID fonts, SIDs are CIDs, not glyph name indices.
	// We can't map CIDs to Unicode without a predefined CMap (handled elsewhere).
	if info.isCID {
		return nil, nil
	}

	// Build GID → Unicode: GID → SID → glyph name → Unicode.
	result := make(map[uint16]rune, len(sids))
	for gid, sid := range sids {
		if gid%1000 == 0 && gid > 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		if gid == 0 {
			continue // skip .notdef
		}
		name := strings.get(sid)
		if name == "" || name == ".notdef" {
			continue
		}
		if r, ok := encodings.GlyphToRuneFull(name); ok {
			if _, exists := result[uint16(gid)]; !exists { //nolint:gosec // GID fits uint16
				result[uint16(gid)] = r //nolint:gosec // gid < numGlyphs, itself a CFF INDEX Card16 count
			}
		}
	}

	return result, nil
}

// GlyphNames extracts a GID → glyph name mapping from CFF font data.
// Returns nil for CID-keyed fonts (which use CIDs, not glyph names).
func GlyphNames(ctx context.Context, data []byte) (map[uint16]string, error) {
	if len(data) < 4 || data[0] != 1 {
		return nil, fmt.Errorf("%w: invalid CFF header", ErrInvalidCFF)
	}
	hdrSize := max(int(data[2]), 4)
	pos := hdrSize

	nameEnd, err := skipINDEX(data, pos)
	if err != nil {
		return nil, err
	}
	topDictEnd, topDictEntries, err := parseINDEX(data, nameEnd)
	if err != nil || len(topDictEntries) == 0 {
		return nil, err
	}
	info := parseTopDict(topDictEntries[0])
	if info.isCID {
		return nil, nil
	}
	_, stringEntries, err := parseINDEX(data, topDictEnd)
	if err != nil {
		return nil, err
	}
	strings := &stringTable{custom: stringEntries}

	numGlyphs := 0
	if info.charStringsOffset > 0 && int(info.charStringsOffset) < len(data) {
		_, csEntries, err := parseINDEX(data, int(info.charStringsOffset))
		if err == nil {
			numGlyphs = len(csEntries)
		}
	}
	if numGlyphs == 0 {
		return nil, nil
	}

	sids, err := resolveCharset(data, int(info.charsetOffset), numGlyphs)
	if err != nil {
		return nil, err
	}

	result := make(map[uint16]string, len(sids))
	for gid, sid := range sids {
		if gid%500 == 0 && gid > 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		name := strings.get(sid)
		if name != "" {
			result[uint16(gid)] = name //nolint:gosec // gid < numGlyphs, itself a CFF INDEX Card16 count
		}
	}
	return result, nil
}

// ValidCIDs returns the set of CIDs present in a CID-keyed CFF font's charset.
// For non-CID fonts, returns nil. In a CID-keyed CFF, the charset maps
// GID → CID; this function inverts that to return all valid CIDs.
func ValidCIDs(data []byte) (map[uint16]bool, error) {
	if len(data) < 4 || data[0] != 1 {
		return nil, fmt.Errorf("%w: invalid CFF header", ErrInvalidCFF)
	}
	hdrSize := max(int(data[2]), 4)
	pos := hdrSize

	nameEnd, err := skipINDEX(data, pos)
	if err != nil {
		return nil, err
	}
	_, topDictEntries, err := parseINDEX(data, nameEnd)
	if err != nil || len(topDictEntries) == 0 {
		return nil, err
	}
	info := parseTopDict(topDictEntries[0])
	if !info.isCID {
		return nil, nil // Not a CID-keyed font.
	}

	numGlyphs := 0
	if info.charStringsOffset > 0 && int(info.charStringsOffset) < len(data) {
		_, csEntries, err := parseINDEX(data, int(info.charStringsOffset))
		if err == nil {
			numGlyphs = len(csEntries)
		}
	}
	if numGlyphs == 0 {
		return nil, nil
	}

	// In CID-keyed CFF, the charset entries are CIDs (not SIDs).
	cids, err := resolveCharset(data, int(info.charsetOffset), numGlyphs)
	if err != nil {
		return nil, err
	}

	result := make(map[uint16]bool, len(cids))
	for _, cid := range cids {
		result[uint16(cid)] = true //nolint:gosec // charset CIDs are read as uint16 (Card16)
	}
	return result, nil
}

// parseINDEX reads a CFF INDEX structure and returns the end offset and
// the data entries as byte slices.
func parseINDEX(data []byte, pos int) (endPos int, entries [][]byte, err error) {
	if pos+2 > len(data) {
		return 0, nil, ErrTruncated
	}
	count := int(binary.BigEndian.Uint16(data[pos : pos+2]))
	pos += 2

	if count == 0 {
		return pos, nil, nil
	}

	if pos >= len(data) {
		return 0, nil, ErrTruncated
	}
	offSize := int(data[pos])
	pos++

	if offSize < 1 || offSize > 4 {
		return 0, nil, fmt.Errorf("%w: invalid offSize %d", ErrInvalidCFF, offSize)
	}

	offsetArraySize := (count + 1) * offSize
	if pos+offsetArraySize > len(data) {
		return 0, nil, ErrTruncated
	}

	// Read offsets (1-based per CFF spec).
	offsets := make([]int, count+1)
	for i := range count + 1 {
		offsets[i] = readOffset(data[pos+i*offSize:], offSize) - 1 // convert to 0-based
	}
	pos += offsetArraySize

	dataStart := pos
	entries = make([][]byte, count)
	for i := range count {
		start := dataStart + offsets[i]
		end := dataStart + offsets[i+1]
		if start < 0 || end > len(data) || start > end {
			entries[i] = nil
			continue
		}
		entries[i] = data[start:end]
	}

	endPos = dataStart + offsets[count]
	return endPos, entries, nil
}

// skipINDEX advances past a CFF INDEX without reading entries.
func skipINDEX(data []byte, pos int) (int, error) {
	end, _, err := parseINDEX(data, pos)
	return end, err
}

func readOffset(data []byte, size int) int {
	switch size {
	case 1:
		return int(data[0])
	case 2:
		return int(binary.BigEndian.Uint16(data[:2]))
	case 3:
		return int(data[0])<<16 | int(data[1])<<8 | int(data[2])
	case 4:
		return int(binary.BigEndian.Uint32(data[:4]))
	}
	return 0
}

// topDictInfo holds key offsets extracted from the CFF Top DICT.
type topDictInfo struct {
	charsetOffset     int32
	encodingOffset    int32 // CFF Encoding (byte code → GID)
	charStringsOffset int32
	privateDictOffset int32
	privateDictSize   int32
	fdArrayOffset     int32 // CID fonts: INDEX of per-FD font dicts (op 12 36)
	fdSelectOffset    int32 // CID fonts: per-glyph FD selector (op 12 37)
	isCID             bool
}

// parseTopDict extracts key offsets from a Top DICT entry.
func parseTopDict(data []byte) topDictInfo {
	var info topDictInfo

	operands := make([]int32, 0, 4)
	for i := 0; i < len(data); {
		b := data[i]
		switch {
		case b <= 21:
			// Operator.
			op := int(b)
			if b == 12 && i+1 < len(data) {
				i++
				op = 1200 + int(data[i])
			}
			switch op {
			case 15: // charset
				if len(operands) > 0 {
					info.charsetOffset = operands[len(operands)-1]
				}
			case 16: // Encoding
				if len(operands) > 0 {
					info.encodingOffset = operands[len(operands)-1]
				}
			case 17: // CharStrings
				if len(operands) > 0 {
					info.charStringsOffset = operands[len(operands)-1]
				}
			case 18: // Private (size, offset)
				if len(operands) >= 2 {
					info.privateDictSize = operands[len(operands)-2]
					info.privateDictOffset = operands[len(operands)-1]
				}
			case 1230: // ROS (12 30), marks CID font
				info.isCID = true
			case 1236: // FDArray (12 36)
				if len(operands) > 0 {
					info.fdArrayOffset = operands[len(operands)-1]
				}
			case 1237: // FDSelect (12 37)
				if len(operands) > 0 {
					info.fdSelectOffset = operands[len(operands)-1]
				}
			}
			operands = operands[:0]
			i++
		case b >= 32 && b <= 246:
			operands = append(operands, int32(b)-139)
			i++
		case b >= 247 && b <= 250:
			if i+1 >= len(data) {
				return info
			}
			operands = append(operands, (int32(b)-247)*256+int32(data[i+1])+108)
			i += 2
		case b >= 251 && b <= 254:
			if i+1 >= len(data) {
				return info
			}
			operands = append(operands, -(int32(b)-251)*256-int32(data[i+1])-108)
			i += 2
		case b == 28:
			if i+2 >= len(data) {
				return info
			}
			operands = append(operands, int32(int16(binary.BigEndian.Uint16(data[i+1:i+3])))) //nolint:gosec // intentional two's-complement conversion for CFF binary parsing
			i += 3
		case b == 29:
			if i+4 >= len(data) {
				return info
			}
			operands = append(operands, int32(binary.BigEndian.Uint32(data[i+1:i+5]))) //nolint:gosec // intentional two's-complement conversion for CFF binary parsing
			i += 5
		case b == 30:
			// Real number: skip (not needed for our operators).
			i++
			for i < len(data) {
				nibble := data[i]
				i++
				if nibble&0x0F == 0x0F || nibble>>4 == 0x0F {
					break
				}
			}
		default:
			i++ // skip unknown
		}
	}
	return info
}

// parseCharset reads the charset table and returns a GID → SID slice.
// GID 0 is always .notdef (SID 0).
func parseCharset(data []byte, pos int, numGlyphs int) ([]int, error) {
	if pos < 0 || pos >= len(data) {
		// Predefined charset offsets (0/1/2) are resolved by [resolveCharset]
		// before reaching here, so pos is a byte offset into data. A pos past
		// the end is a malformed offset: return no charset rather than read
		// out of bounds.
		return nil, nil
	}

	sids := make([]int, numGlyphs)
	sids[0] = 0 // .notdef

	format := data[pos]
	pos++
	remaining := numGlyphs - 1 // exclude .notdef
	gid := 1

	switch format {
	case 0:
		// Format 0: array of SIDs.
		for i := 0; i < remaining && pos+1 < len(data); i++ {
			sids[gid] = int(binary.BigEndian.Uint16(data[pos : pos+2]))
			pos += 2
			gid++
		}
	case 1:
		// Format 1: ranges with uint8 count.
		for gid <= remaining && pos+2 < len(data) {
			first := int(binary.BigEndian.Uint16(data[pos : pos+2]))
			nLeft := int(data[pos+2])
			pos += 3
			for i := 0; i <= nLeft && gid < numGlyphs; i++ {
				sids[gid] = first + i
				gid++
			}
		}
	case 2:
		// Format 2: ranges with uint16 count.
		for gid <= remaining && pos+3 < len(data) {
			first := int(binary.BigEndian.Uint16(data[pos : pos+2]))
			nLeft := int(binary.BigEndian.Uint16(data[pos+2 : pos+4]))
			pos += 4
			for i := 0; i <= nLeft && gid < numGlyphs; i++ {
				sids[gid] = first + i
				gid++
			}
		}
	default:
		return nil, fmt.Errorf("%w: unknown charset format %d", ErrInvalidCFF, format)
	}

	return sids, nil
}

// stringTable resolves SIDs to glyph names. SIDs 0-390 are the CFF standard
// strings; SIDs 391+ index into the custom string INDEX.
type stringTable struct {
	custom [][]byte
}

func (st *stringTable) get(sid int) string {
	if sid >= 0 && sid < len(cffStandardStrings) {
		return cffStandardStrings[sid]
	}
	idx := sid - len(cffStandardStrings)
	if idx >= 0 && idx < len(st.custom) && st.custom[idx] != nil {
		return string(st.custom[idx])
	}
	return ""
}

// ReadUnitsPerEm extracts the effective units-per-em from a CFF font's
// FontMatrix in the Top DICT. Default is 1000 (FontMatrix [0.001 ...]).
// Returns 0 if the data is not a valid CFF font.
func ReadUnitsPerEm(data []byte) uint16 {
	if len(data) < 4 || data[0] != 1 {
		return 0
	}
	hdrSize := max(int(data[2]), 4)
	nameEnd, err := skipINDEX(data, hdrSize)
	if err != nil {
		return 1000 //nolint:mnd // The default CFF FontMatrix is 0.001, i.e. 1000 units per em (CFF spec, Tech Note #5176, Table 9).
	}
	_, topDictEntries, err := parseINDEX(data, nameEnd)
	if err != nil || len(topDictEntries) == 0 {
		return 1000 //nolint:mnd // Default FontMatrix 0.001 => 1000 upm (CFF spec, Table 9).
	}

	scale := parseFontMatrixScale(topDictEntries[0])
	if scale <= 0 {
		// A CID-keyed CFF may carry its FontMatrix in the FDArray font dict
		// rather than the Top DICT (CFF spec, TN 5176 §18). Fall back to the
		// first FD's FontMatrix so a non-1000-upm CID subset scales correctly;
		// otherwise widths read against a default 1000 upm fire spurious
		// mismatches against the /W array.
		if info := parseTopDict(topDictEntries[0]); info.isCID && info.fdArrayOffset > 0 && int(info.fdArrayOffset) < len(data) {
			if _, fdEntries, err := parseINDEX(data, int(info.fdArrayOffset)); err == nil && len(fdEntries) > 0 {
				scale = parseFontMatrixScale(fdEntries[0])
			}
		}
	}
	if scale <= 0 {
		return 1000 //nolint:mnd // default CFF FontMatrix
	}
	// +0.5 rounds to nearest; scale is positive here, and a FontMatrix small
	// enough to overflow uint16 is rejected by the upm == 0 check below.
	upm := uint16(1.0/scale + 0.5) //nolint:gosec,mnd // Bounded: scale > 0 was checked above, and 0 is rejected below.
	if upm == 0 {
		return 1000 //nolint:mnd // Default FontMatrix 0.001 => 1000 upm (CFF spec, Table 9).
	}
	return upm
}

// parseFontMatrixScale scans a CFF Top DICT for the FontMatrix operator (12 7)
// and returns the first element (horizontal scale). Returns 0 if absent.
// Handles both integer and real (nibble-encoded) number operands.
func parseFontMatrixScale(dictData []byte) float64 {
	// We need to track the last few operands as float64 to handle reals.
	operands := make([]float64, 0, 6) //nolint:mnd // A CFF DICT operator takes at most six operands (FontMatrix); a capacity hint, not a limit.

	for i := 0; i < len(dictData); {
		b := dictData[i]
		switch {
		case b <= 21:
			op := int(b)
			if b == 12 && i+1 < len(dictData) {
				i++
				op = 1200 + int(dictData[i])
			}
			if op == 1207 && len(operands) >= 1 { //nolint:mnd // FontMatrix (12 7)
				return operands[0]
			}
			operands = operands[:0]
			i++
		case b >= 32 && b <= 246: //nolint:mnd // CFF spec (Tech Note #5176) Table 3: b0 32-246 is a one-byte operand, value b0-139.
			operands = append(operands, float64(int32(b)-139))
			i++
		case b >= 247 && b <= 250: //nolint:mnd // CFF spec Table 3: b0 247-250 is a two-byte positive operand, (b0-247)*256 + b1 + 108.
			if i+1 >= len(dictData) {
				return 0
			}
			operands = append(operands, float64((int32(b)-247)*256+int32(dictData[i+1])+108))
			i += 2
		case b >= 251 && b <= 254: //nolint:mnd // CFF spec Table 3: b0 251-254 is a two-byte negative operand, -(b0-251)*256 - b1 - 108.
			if i+1 >= len(dictData) {
				return 0
			}
			operands = append(operands, float64(-(int32(b)-251)*256-int32(dictData[i+1])-108))
			i += 2
		case b == 28: //nolint:mnd // CFF spec Table 3: b0 28 introduces a 16-bit two's-complement operand.
			if i+2 >= len(dictData) {
				return 0
			}
			operands = append(operands, float64(int16(binary.BigEndian.Uint16(dictData[i+1:i+3])))) //nolint:gosec // intentional two's-complement conversion
			i += 3
		case b == 29: //nolint:mnd // CFF spec Table 3: b0 29 introduces a 32-bit two's-complement operand.
			if i+4 >= len(dictData) {
				return 0
			}
			operands = append(operands, float64(int32(binary.BigEndian.Uint32(dictData[i+1:i+5])))) //nolint:gosec // intentional two's-complement conversion
			i += 5
		case b == 30: //nolint:mnd // CFF spec Table 3: b0 30 introduces a nibble-encoded real number.
			// Real number: nibble-encoded.
			val, end := parseRealNumber(dictData, i+1)
			operands = append(operands, val)
			i = end
		default:
			i++
		}
	}
	return 0
}

// parseRealNumber decodes a CFF nibble-encoded real number starting at pos.
// Returns the value and the position after the last byte consumed.
func parseRealNumber(data []byte, pos int) (float64, int) {
	var buf [32]byte
	n := 0
	for pos < len(data) {
		b := data[pos]
		pos++
		for _, shift := range []uint{4, 0} {
			nibble := (b >> shift) & 0x0F
			if nibble == 0x0F {
				goto done
			}
			if n >= len(buf)-1 {
				goto done
			}
			switch {
			case nibble <= 9: //nolint:mnd // CFF spec Table 5: nibbles 0-9 are the decimal digits.
				buf[n] = '0' + nibble
				n++
			case nibble == 0x0A: //nolint:mnd // CFF spec Table 5: nibble 0xA is the decimal point.
				buf[n] = '.'
				n++
			case nibble == 0x0B: //nolint:mnd // CFF spec Table 5: nibble 0xB is E.
				buf[n] = 'E'
				n++
			case nibble == 0x0C: //nolint:mnd // CFF spec Table 5: nibble 0xC is E-minus.
				buf[n] = 'E'
				n++
				buf[n] = '-'
				n++
			case nibble == 0x0E: //nolint:mnd // CFF spec Table 5: nibble 0xE is the minus sign.
				buf[n] = '-'
				n++
			}
		}
	}
done:
	if n == 0 {
		return 0, pos
	}
	// Parse the string as float64.
	val := 0.0
	neg := false
	i := 0
	if buf[0] == '-' {
		neg = true
		i = 1
	}
	// Integer part.
	for i < n && buf[i] >= '0' && buf[i] <= '9' {
		val = val*10 + float64(buf[i]-'0')
		i++
	}
	// Fractional part.
	if i < n && buf[i] == '.' {
		i++
		frac := 0.1
		for i < n && buf[i] >= '0' && buf[i] <= '9' {
			val += float64(buf[i]-'0') * frac
			frac /= 10
			i++
		}
	}
	// Exponent.
	if i < n && buf[i] == 'E' {
		i++
		expNeg := false
		if i < n && buf[i] == '-' {
			expNeg = true
			i++
		}
		exp := 0
		for i < n && buf[i] >= '0' && buf[i] <= '9' {
			exp = exp*10 + int(buf[i]-'0')
			i++
		}
		if expNeg {
			exp = -exp
		}
		// Clamp the exponent before raising 10 to it. exp is parsed from the
		// font's bytes; an unbounded value would spin the power loop billions
		// of times on crafted input (a DoS). A font-matrix scale never needs a
		// power beyond double precision, so ±308 covers every representable
		// value and larger exponents just saturate.
		if exp > 308 {
			exp = 308
		} else if exp < -308 {
			exp = -308
		}
		mult := 1.0
		for e := 0; e < exp; e++ {
			mult *= 10
		}
		for e := 0; e > exp; e-- {
			mult /= 10
		}
		val *= mult
	}
	if neg {
		val = -val
	}
	return val, pos
}

// GlyphWidths extracts advance widths from CFF charstring data.
// It parses each CharString to find the optional width operand, then applies
// the nominalWidthX/defaultWidthX from the Private DICT.
// Returns a GID → width map in 1/1000 units (like TrueType).
func GlyphWidths(ctx context.Context, data []byte) (map[uint16]float64, error) {
	if len(data) < 4 || data[0] != 1 {
		return nil, fmt.Errorf("%w: not a CFF font", ErrInvalidCFF)
	}

	hdrSize := max(int(data[2]), 4)
	pos := hdrSize

	nameEnd, err := skipINDEX(data, pos)
	if err != nil {
		return nil, err
	}
	topDictEnd, topDictEntries, err := parseINDEX(data, nameEnd)
	if err != nil || len(topDictEntries) == 0 {
		return nil, fmt.Errorf("%w: no top dict", ErrInvalidCFF)
	}
	info := parseTopDict(topDictEntries[0])

	// The Global Subr INDEX follows the String INDEX (CFF spec §16). topDictEnd
	// marks where the String INDEX begins; skip it to reach the global subrs.
	// A charstring may call a subroutine before its first stack-clearing
	// operator, so the width scanner must be able to expand those subrs.
	var globalSubrs [][]byte
	if stringEnd, serr := skipINDEX(data, topDictEnd); serr == nil {
		_, globalSubrs, _ = parseINDEX(data, stringEnd)
	}

	// Parse Private DICT for nominalWidthX, defaultWidthX, and local subrs.
	var nominalWidth, defaultWidth float64
	var localSubrs [][]byte
	if info.privateDictOffset > 0 && info.privateDictSize > 0 {
		pdStart := int(info.privateDictOffset)
		pdEnd := pdStart + int(info.privateDictSize)
		if pdEnd <= len(data) {
			var subrsRel int32
			nominalWidth, defaultWidth, subrsRel = parsePrivateDict(data[pdStart:pdEnd])
			if subrsRel > 0 {
				// The Subrs offset (op 19) is relative to the Private DICT start.
				if sp := pdStart + int(subrsRel); sp > 0 && sp < len(data) {
					_, localSubrs, _ = parseINDEX(data, sp)
				}
			}
		}
	}

	// Parse CharStrings INDEX.
	if info.charStringsOffset <= 0 || int(info.charStringsOffset) >= len(data) {
		return nil, fmt.Errorf("%w: no charstrings", ErrInvalidCFF)
	}
	_, csEntries, err := parseINDEX(data, int(info.charStringsOffset))
	if err != nil || len(csEntries) == 0 {
		return nil, fmt.Errorf("%w: no charstrings", ErrInvalidCFF)
	}

	// CID-keyed fonts carry no top-level Private DICT: each glyph's
	// nominalWidthX/defaultWidthX comes from the FDArray font dict its
	// FDSelect entry picks (CFF spec §18). Without this, every width in a
	// CID font is computed against nominal/default 0 and the /W repair
	// writes values veraPDF rejects.
	fdWidths := parseFDWidths(data, info, len(csEntries))

	result := make(map[uint16]float64, len(csEntries))
	for gid, cs := range csEntries {
		if gid%500 == 0 && gid > 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		nomW, defW := nominalWidth, defaultWidth
		local := localSubrs
		if fdWidths != nil {
			nomW, defW = fdWidths.widthsFor(gid)
			local = fdWidths.subrsFor(gid)
		}
		w := extractCharstringWidth(cs, local, globalSubrs)
		var width float64
		if w != nil {
			width = nomW + *w
		} else {
			width = defW
		}
		if width < 0 {
			width = 0
		}
		result[uint16(gid)] = width
	}

	return result, nil
}

// CIDWidths extracts advance widths keyed by CID for a CID-keyed CFF font.
// The charset table maps each GID to its CID (the SID slot holds CIDs in
// CID-keyed fonts, CFF spec §18), so callers comparing against a PDF /W
// array, which is CID-indexed, must use this instead of [GlyphWidths]:
// subset fonts renumber GIDs densely while keeping sparse CIDs, and the
// identity assumption reads the wrong charstrings. Non-CID fonts return the
// GID-keyed widths unchanged (GID == code index there).
func CIDWidths(ctx context.Context, data []byte) (map[uint16]float64, error) {
	byGID, err := GlyphWidths(ctx, data)
	if err != nil {
		return nil, err
	}
	if len(data) < 4 || data[0] != 1 {
		return byGID, nil
	}
	hdrSize := max(int(data[2]), 4)
	nameEnd, err := skipINDEX(data, hdrSize)
	if err != nil {
		return byGID, nil //nolint:nilerr // GlyphWidths already validated the font; fall back to GID keys
	}
	_, topDictEntries, err := parseINDEX(data, nameEnd)
	if err != nil || len(topDictEntries) == 0 {
		return byGID, nil //nolint:nilerr // same fallback
	}
	info := parseTopDict(topDictEntries[0])
	if !info.isCID {
		return byGID, nil
	}
	gidToCID, err := resolveCharset(data, int(info.charsetOffset), len(byGID))
	if err != nil || gidToCID == nil {
		return byGID, nil //nolint:nilerr // same fallback
	}
	// Invert GID→CID to CID→width. A subset CFF charset can map several GIDs to
	// the same CID (a duplicate glyph); ranging the byGID map would then let a
	// random iteration order decide which glyph's width a CID inherits, making
	// the result nondeterministic. Walk GIDs in ascending order and keep the
	// first (lowest-GID) glyph for each CID: that is the canonical glyph a
	// consumer selects for the CID (matching veraPDF), and it is stable.
	gids := make([]uint16, 0, len(byGID))
	for gid := range byGID {
		gids = append(gids, gid)
	}
	slices.Sort(gids)
	byCID := make(map[uint16]float64, len(byGID))
	for _, gid := range gids {
		if int(gid) >= len(gidToCID) {
			continue
		}
		cid := gidToCID[gid]
		if cid < 0 || cid > 0xFFFF {
			continue
		}
		if _, exists := byCID[uint16(cid)]; !exists {
			byCID[uint16(cid)] = byGID[gid]
		}
	}
	return byCID, nil
}

// fdWidthTable resolves per-glyph nominal/default widths for CID-keyed CFF
// fonts: fdForGID maps each GID to an FDArray index whose Private DICT
// supplied the widths.
type fdWidthTable struct {
	fdForGID   []byte
	nominal    []float64
	defaults   []float64
	localSubrs [][][]byte // per-FD local subroutine INDEX
}

// widthsFor returns the nominal/default width pair for a GID, falling back
// to FD 0 when FDSelect lacks the glyph.
func (t *fdWidthTable) widthsFor(gid int) (float64, float64) {
	fd := 0
	if gid >= 0 && gid < len(t.fdForGID) {
		fd = int(t.fdForGID[gid])
	}
	if fd >= len(t.nominal) {
		fd = 0
	}
	if len(t.nominal) == 0 {
		return 0, 0
	}
	return t.nominal[fd], t.defaults[fd]
}

// subrsFor returns the local subroutine INDEX for a GID's FDArray font dict,
// falling back to FD 0. Used to expand callsubr while recovering the leading
// width operand.
func (t *fdWidthTable) subrsFor(gid int) [][]byte {
	fd := 0
	if gid >= 0 && gid < len(t.fdForGID) {
		fd = int(t.fdForGID[gid])
	}
	if fd < 0 || fd >= len(t.localSubrs) {
		return nil
	}
	return t.localSubrs[fd]
}

// parseFDWidths builds the per-FD width table for a CID-keyed CFF font.
// Returns nil for non-CID fonts or when the FDArray is unreadable, in which
// case the caller falls back to the top-level Private DICT values.
func parseFDWidths(data []byte, info topDictInfo, numGlyphs int) *fdWidthTable {
	if !info.isCID || info.fdArrayOffset <= 0 || int(info.fdArrayOffset) >= len(data) {
		return nil
	}
	_, fdEntries, err := parseINDEX(data, int(info.fdArrayOffset))
	if err != nil || len(fdEntries) == 0 {
		return nil
	}

	t := &fdWidthTable{
		nominal:    make([]float64, len(fdEntries)),
		defaults:   make([]float64, len(fdEntries)),
		localSubrs: make([][][]byte, len(fdEntries)),
	}
	for i, fdDict := range fdEntries {
		fdInfo := parseTopDict(fdDict)
		if fdInfo.privateDictOffset > 0 && fdInfo.privateDictSize > 0 {
			pdStart := int(fdInfo.privateDictOffset)
			pdEnd := pdStart + int(fdInfo.privateDictSize)
			if pdEnd <= len(data) && pdStart >= 0 && pdStart < pdEnd {
				var subrsRel int32
				t.nominal[i], t.defaults[i], subrsRel = parsePrivateDict(data[pdStart:pdEnd])
				if subrsRel > 0 {
					if sp := pdStart + int(subrsRel); sp > 0 && sp < len(data) {
						_, t.localSubrs[i], _ = parseINDEX(data, sp)
					}
				}
			}
		}
	}

	t.fdForGID = parseFDSelect(data, int(info.fdSelectOffset), numGlyphs)
	return t
}

// parseFDSelect reads an FDSelect structure (format 0 or 3, CFF spec §19)
// and returns the FD index per GID. A missing or malformed FDSelect yields
// all zeroes (FD 0), the spec default for single-FD fonts.
func parseFDSelect(data []byte, offset, numGlyphs int) []byte {
	out := make([]byte, numGlyphs)
	if offset <= 0 || offset >= len(data) {
		return out
	}
	switch data[offset] {
	case 0:
		for gid := 0; gid < numGlyphs && offset+1+gid < len(data); gid++ {
			out[gid] = data[offset+1+gid]
		}
	case 3:
		if offset+5 > len(data) {
			return out
		}
		nRanges := int(binary.BigEndian.Uint16(data[offset+1 : offset+3]))
		pos := offset + 3
		// Each range is (first uint16, fd byte); a trailing sentinel
		// uint16 closes the last range.
		for r := 0; r < nRanges && pos+4 < len(data); r++ {
			first := int(binary.BigEndian.Uint16(data[pos : pos+2]))
			fd := data[pos+2]
			next := int(binary.BigEndian.Uint16(data[pos+3 : pos+5]))
			for gid := first; gid < next && gid < numGlyphs; gid++ {
				if gid >= 0 {
					out[gid] = fd
				}
			}
			pos += 3
		}
	}
	return out
}

// parsePrivateDict extracts nominalWidthX (op 21), defaultWidthX (op 20), and
// the local Subrs offset (op 19, relative to the Private DICT start) from a CFF
// Private DICT.
func parsePrivateDict(data []byte) (nominalWidthX, defaultWidthX float64, subrsOffset int32) {
	operands := make([]float64, 0, 4)
	for i := 0; i < len(data); {
		b := data[i]
		switch {
		case b <= 21:
			op := int(b)
			if b == 12 && i+1 < len(data) {
				i++
				op = 1200 + int(data[i])
			}
			switch op {
			case 19: // Subrs (local subroutine INDEX, offset from Private DICT)
				if len(operands) > 0 {
					subrsOffset = int32(operands[len(operands)-1])
				}
			case 20: // defaultWidthX
				if len(operands) > 0 {
					defaultWidthX = operands[len(operands)-1]
				}
			case 21: // nominalWidthX
				if len(operands) > 0 {
					nominalWidthX = operands[len(operands)-1]
				}
			}
			operands = operands[:0]
			i++
		case b >= 32 && b <= 246:
			operands = append(operands, float64(b)-139)
			i++
		case b >= 247 && b <= 250:
			if i+1 >= len(data) {
				return
			}
			operands = append(operands, (float64(b)-247)*256+float64(data[i+1])+108)
			i += 2
		case b >= 251 && b <= 254:
			if i+1 >= len(data) {
				return
			}
			operands = append(operands, -(float64(b)-251)*256-float64(data[i+1])-108)
			i += 2
		case b == 28:
			if i+2 >= len(data) {
				return
			}
			operands = append(operands, float64(int16(binary.BigEndian.Uint16(data[i+1:i+3])))) //nolint:gosec // intentional two's-complement conversion
			i += 3
		case b == 29:
			if i+4 >= len(data) {
				return
			}
			operands = append(operands, float64(int32(binary.BigEndian.Uint32(data[i+1:i+5])))) //nolint:gosec // intentional two's-complement conversion
			i += 5
		case b == 30:
			// Real number (nibble-encoded). A Computer Modern subset writes
			// nominalWidthX as 384.778, and rounding it shifts every advance the
			// charstrings derive from it by three quarters of a unit.
			val, end := parseRealNumber(data, i+1)
			operands = append(operands, val)
			i = end
		default:
			i++
		}
	}
	return
}

// fixed1616Scale converts a Type 2 charstring's 32-bit fixed operand (CFF
// spec, Type 2 Charstring Format, 3.2) into the real number it encodes: the
// operand is 16.16, so its low 16 bits carry the fraction.
const fixed1616Scale = 65536.0

// maxSubrNesting bounds Type 2 subroutine recursion while recovering a glyph
// width. The CFF spec caps subroutine nesting at 10; deeper is malformed or
// adversarial. Anti-DoS guard, not a spec metric.
const maxSubrNesting = 10

// subrBias returns the Type 2 subroutine index bias for an INDEX of n subrs
// (CFF spec, Type 2 Charstring Format, "Subroutine Operators").
func subrBias(n int) int32 {
	switch {
	case n < 1240:
		return 107
	case n < 33900:
		return 1131
	default:
		return 32768
	}
}

// widthScanner walks a Type 2 charstring far enough to recover the optional
// leading width operand, expanding local/global subroutines. The width, if
// present, precedes the first stack-clearing operator (CFF spec, Type 2
// Charstring Format §3.1). A callsubr/callgsubr before that operator, or the
// implicit-vstem hintmask/cntrmask form, splits or defers the operand; a
// scanner that clears the stack on those loses the width and misreports
// defaultWidthX.
type widthScanner struct {
	local, global [][]byte
	lbias, gbias  int32
	stack         []float64
	width         *float64
	done          bool
}

// push appends an operand, stopping the scan if the operand stack exceeds the
// Type 2 ceiling (adversarial input pushing operands without a clearing op).
func (s *widthScanner) push(v float64) {
	if len(s.stack) >= maxCharstringStack {
		s.done = true
		return
	}
	s.stack = append(s.stack, v)
}

// takeWidth records the leading operand as the width when an extra operand is
// present, then stops the scan (the first stack-clearing operator settles it).
func (s *widthScanner) takeWidth(extra bool) {
	if extra && len(s.stack) > 0 {
		w := s.stack[0]
		s.width = &w
	}
	s.done = true
}

// callSubr expands the subroutine indexed by the top operand (after bias),
// sharing the operand stack with the calling frame.
func (s *widthScanner) callSubr(subrs [][]byte, bias int32, depth int) {
	if len(s.stack) == 0 {
		s.done = true
		return
	}
	idx := int32(s.stack[len(s.stack)-1]) + bias
	s.stack = s.stack[:len(s.stack)-1]
	if idx < 0 || int(idx) >= len(subrs) || subrs[idx] == nil {
		s.done = true
		return
	}
	s.scan(subrs[idx], depth+1)
}

// scan interprets one charstring (or subroutine) body.
func (s *widthScanner) scan(cs []byte, depth int) {
	if depth > maxSubrNesting {
		s.done = true
		return
	}
	for i := 0; i < len(cs) && !s.done; {
		b := cs[i]
		switch {
		case b == 28:
			if i+2 >= len(cs) {
				s.done = true
				return
			}
			s.push(float64(int16(binary.BigEndian.Uint16(cs[i+1 : i+3])))) //nolint:gosec // intentional two's-complement conversion
			i += 3
		case b >= 32 && b <= 246:
			s.push(float64(b) - 139)
			i++
		case b >= 247 && b <= 250:
			if i+1 >= len(cs) {
				s.done = true
				return
			}
			s.push((float64(b)-247)*256 + float64(cs[i+1]) + 108)
			i += 2
		case b >= 251 && b <= 254:
			if i+1 >= len(cs) {
				s.done = true
				return
			}
			s.push(-(float64(b)-251)*256 - float64(cs[i+1]) - 108)
			i += 2
		case b == 255:
			// 32-bit fixed point (16.16). The fraction belongs to the advance:
			// a Computer Modern subset writes 707.167 there, and truncating it
			// reads 707, hiding a mismatch against a /Widths entry of 706.
			if i+4 >= len(cs) {
				s.done = true
				return
			}
			s.push(float64(int32(binary.BigEndian.Uint32(cs[i+1:i+5]))) / fixed1616Scale) //nolint:gosec // reinterprets the 16.16 fixed operand's bits, no range change
			i += 5
		default:
			op := int(b)
			i++
			if b == 12 {
				// Two-byte escape (arithmetic/flex); never precedes the first
				// width-determining operator in a well-formed glyph. Clear the
				// stack and continue, matching a conforming interpreter.
				if i < len(cs) {
					i++
				}
				s.stack = s.stack[:0]
				continue
			}
			switch op {
			case 10: // callsubr (local)
				s.callSubr(s.local, s.lbias, depth)
			case 29: // callgsubr (global)
				s.callSubr(s.global, s.gbias, depth)
			case 11: // return: leave this subroutine frame
				return
			case 1, 3, 18, 23: // hstem, vstem, hstemhm, vstemhm (even operands)
				s.takeWidth(len(s.stack)%2 != 0)
			case 19, 20: // hintmask, cntrmask: implicit vstem hints, odd ⇒ width
				s.takeWidth(len(s.stack)%2 != 0)
			case 4, 22: // vmoveto, hmoveto (1 operand)
				s.takeWidth(len(s.stack) > 1)
			case 21: // rmoveto (2 operands)
				s.takeWidth(len(s.stack) > 2)
			case 14: // endchar (0 operands, or 4 for deprecated seac; +1 ⇒ width)
				s.takeWidth(len(s.stack) == 1 || len(s.stack) == 5)
			default:
				// Non-width operator: clear the stack and keep scanning.
				s.stack = s.stack[:0]
			}
		}
	}
}

// extractCharstringWidth parses a Type 2 charstring and returns the optional
// width operand (the first "extra" operand before the first stack-clearing
// operator), expanding any subroutines it calls first. Returns nil if no
// explicit width is present (the glyph then uses defaultWidthX).
func extractCharstringWidth(cs []byte, localSubrs, globalSubrs [][]byte) *float64 {
	s := &widthScanner{
		local:  localSubrs,
		global: globalSubrs,
		lbias:  subrBias(len(localSubrs)),
		gbias:  subrBias(len(globalSubrs)),
		stack:  make([]float64, 0, 48), //nolint:mnd // typical charstring stack depth
	}
	s.scan(cs, 0)
	return s.width
}
