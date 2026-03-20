package cff

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/typography/encodings"
)

var (
	ErrInvalidCFF = errors.New("cff: invalid font data")
	ErrTruncated  = errors.New("cff: data truncated")
)

// GlyphToUnicode extracts a GID → Unicode rune mapping from CFF font data.
// It parses the charset to get GID → SID, resolves SIDs to glyph names via
// the standard + custom string tables, then maps glyph names to Unicode via
// the Adobe Glyph List.
func GlyphToUnicode(data []byte) (map[uint16]rune, error) {
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

	charsetOffset, charStringsOffset, isCID := parseTopDict(topDictEntries[0])

	// Parse String INDEX (custom strings beyond SID 390).
	stringEnd, stringEntries, err := parseINDEX(data, topDictEnd)
	if err != nil {
		return nil, fmt.Errorf("string INDEX: %w", err)
	}
	_ = stringEnd // not needed further

	// Build the string resolver.
	strings := &stringTable{custom: stringEntries}

	// Get number of glyphs from CharStrings INDEX.
	numGlyphs := 0
	if charStringsOffset > 0 && int(charStringsOffset) < len(data) {
		_, csEntries, err := parseINDEX(data, int(charStringsOffset))
		if err == nil {
			numGlyphs = len(csEntries)
		}
	}
	if numGlyphs == 0 {
		return nil, fmt.Errorf("%w: no charstrings", ErrInvalidCFF)
	}

	// Parse charset: GID → SID mapping.
	sids, err := parseCharset(data, int(charsetOffset), numGlyphs)
	if err != nil {
		return nil, fmt.Errorf("charset: %w", err)
	}

	// For CID fonts, SIDs are CIDs, not glyph name indices.
	// We can't map CIDs to Unicode without a predefined CMap (handled elsewhere).
	if isCID {
		return nil, nil
	}

	// Build GID → Unicode: GID → SID → glyph name → Unicode.
	result := make(map[uint16]rune, len(sids))
	for gid, sid := range sids {
		if gid == 0 {
			continue // skip .notdef
		}
		name := strings.get(sid)
		if name == "" || name == ".notdef" {
			continue
		}
		if r, ok := encodings.GlyphToRuneFull(name); ok {
			if _, exists := result[uint16(gid)]; !exists { //nolint:gosec // GID fits uint16
				result[uint16(gid)] = r //nolint:gosec
			}
		}
	}

	return result, nil
}

// --- INDEX parsing ---

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

// --- Top DICT parsing ---

// parseTopDict extracts charset offset, CharStrings offset, and CID flag
// from a Top DICT entry. Uses a minimal DICT parser that handles the
// operators we care about.
func parseTopDict(data []byte) (charsetOffset, charStringsOffset int32, isCID bool) {
	// Defaults per CFF spec.
	charsetOffset = 0 // 0 = ISOAdobe charset

	operands := make([]int32, 0, 4)
	for i := 0; i < len(data); {
		b := data[i]
		switch {
		case b >= 0 && b <= 21:
			// Operator.
			op := int(b)
			if b == 12 && i+1 < len(data) {
				i++
				op = 1200 + int(data[i])
			}
			switch op {
			case 15: // charset
				if len(operands) > 0 {
					charsetOffset = operands[len(operands)-1]
				}
			case 17: // CharStrings
				if len(operands) > 0 {
					charStringsOffset = operands[len(operands)-1]
				}
			case 1230: // ROS (12 30) — marks CID font
				isCID = true
			}
			operands = operands[:0]
			i++
		case b >= 32 && b <= 246:
			operands = append(operands, int32(b)-139)
			i++
		case b >= 247 && b <= 250:
			if i+1 >= len(data) {
				return
			}
			operands = append(operands, (int32(b)-247)*256+int32(data[i+1])+108)
			i += 2
		case b >= 251 && b <= 254:
			if i+1 >= len(data) {
				return
			}
			operands = append(operands, -(int32(b)-251)*256-int32(data[i+1])-108)
			i += 2
		case b == 28:
			if i+2 >= len(data) {
				return
			}
			operands = append(operands, int32(int16(binary.BigEndian.Uint16(data[i+1:i+3]))))
			i += 3
		case b == 29:
			if i+4 >= len(data) {
				return
			}
			operands = append(operands, int32(binary.BigEndian.Uint32(data[i+1:i+5])))
			i += 5
		case b == 30:
			// Real number — skip (not needed for our operators).
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
	return
}

// --- Charset parsing ---

// parseCharset reads the charset table and returns a GID → SID slice.
// GID 0 is always .notdef (SID 0).
func parseCharset(data []byte, pos int, numGlyphs int) ([]int, error) {
	if pos < 0 || pos >= len(data) {
		// Predefined charsets: 0=ISOAdobe, 1=Expert, 2=ExpertSubset.
		// For our purposes, return empty — these map to standard glyph names
		// which are already in the encoding tables.
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

// --- String table ---

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
