package cff

import (
	"encoding/binary"
	"fmt"
)

// Top DICT operators read by [ReadInfo] (Adobe Technical Note #5176, Table 9
// and Table 10).
const (
	opFontBBox     = 5
	opIsFixedPitch = 1201
	opItalicAngle  = 1202
	opFontMatrix   = 1207
	opROS          = 1230
)

// maxDictOperands is the DICT operand stack limit, 48 entries (Adobe Technical
// Note #5176, Appendix B).
const maxDictOperands = 48

// Info is what the header of a bare CFF font program states about its first
// font: its name, whether it is CID-keyed, and the Top DICT metrics a font
// descriptor carries.
type Info struct {
	// FontName is the first entry of the Name INDEX.
	FontName string
	// CID reports a CID-keyed font, one whose Top DICT starts with ROS.
	CID bool
	// BBox is the Top DICT FontBBox, in glyph space units.
	BBox [4]float64
	// ItalicAngle is the Top DICT ItalicAngle, in degrees.
	ItalicAngle float64
	// FixedPitch is the Top DICT isFixedPitch.
	FixedPitch bool
	// Glyphs is the number of entries of the CharStrings INDEX.
	Glyphs int
}

// ReadInfo reads the Name INDEX and the Top DICT of a bare CFF font program
// (Adobe Technical Note #5176, sections 4 to 9). It fails on data that does
// not start with a CFF version 1 header, and on a font whose CharStrings INDEX
// holds no glyph.
func ReadInfo(data []byte) (Info, error) {
	if len(data) < 4 || data[0] != 1 {
		return Info{}, fmt.Errorf("%w: not a CFF version 1 header", ErrInvalidCFF)
	}
	nameEnd, names, err := parseINDEX(data, max(int(data[2]), 4))
	if err != nil {
		return Info{}, fmt.Errorf("reading the Name INDEX: %w", err)
	}
	if len(names) == 0 {
		return Info{}, fmt.Errorf("%w: empty Name INDEX", ErrInvalidCFF)
	}
	_, topDicts, err := parseINDEX(data, nameEnd)
	if err != nil {
		return Info{}, fmt.Errorf("reading the Top DICT INDEX: %w", err)
	}
	if len(topDicts) == 0 {
		return Info{}, fmt.Errorf("%w: empty Top DICT INDEX", ErrInvalidCFF)
	}
	info := Info{FontName: string(names[0])}
	scanDict(topDicts[0], func(op int, operands []float64) {
		switch op {
		case opFontBBox:
			if len(operands) >= 4 {
				copy(info.BBox[:], operands[len(operands)-4:])
			}
		case opIsFixedPitch:
			if len(operands) > 0 {
				info.FixedPitch = operands[len(operands)-1] != 0
			}
		case opItalicAngle:
			if len(operands) > 0 {
				info.ItalicAngle = operands[len(operands)-1]
			}
		case opROS:
			info.CID = true
		}
	})
	top := parseTopDict(topDicts[0])
	if top.charStringsOffset <= 0 || int(top.charStringsOffset) >= len(data) {
		return Info{}, fmt.Errorf("%w: no CharStrings INDEX", ErrInvalidCFF)
	}
	_, charStrings, err := parseINDEX(data, int(top.charStringsOffset))
	if err != nil {
		return Info{}, fmt.Errorf("reading the CharStrings INDEX: %w", err)
	}
	if len(charStrings) == 0 {
		return Info{}, fmt.Errorf("%w: empty CharStrings INDEX", ErrInvalidCFF)
	}
	info.Glyphs = len(charStrings)
	return info, nil
}

// CharStringsReachable reports whether a bare CFF font program leads from its
// header to a CharStrings INDEX that reads: the Name INDEX and the Top DICT
// INDEX read, the Top DICT INDEX holds a font, and the offset its first Top
// DICT gives the CharStrings operator (17) opens an INDEX inside the data
// (Adobe Technical Note #5176, sections 6 to 9 and Table 9). A Top DICT that
// states no such offset leaves it at 0, the header. Unlike [ReadInfo] this
// accepts an empty Name INDEX and an empty CharStrings INDEX, which state no
// font name or no glyph but still read.
func CharStringsReachable(data []byte) bool {
	if len(data) < 4 {
		return false
	}
	nameEnd, err := skipINDEX(data, max(int(data[2]), 4))
	if err != nil {
		return false
	}
	_, topDicts, err := parseINDEX(data, nameEnd)
	if err != nil || len(topDicts) == 0 {
		return false
	}
	offset := parseTopDict(topDicts[0]).charStringsOffset
	if offset < 0 || int(offset) >= len(data) {
		return false
	}
	_, _, err = parseINDEX(data, int(offset))
	return err == nil
}

// EmptyCharStrings reports whether a bare CFF font program reaches a
// CharStrings INDEX ([CharStringsReachable]) that holds no charstring at all,
// so the program defines no glyph, .notdef included (Adobe Technical Note
// #5176, 14, makes GID 0 the first entry of that INDEX). cidKeyed reports a
// Top DICT carrying the ROS operator (section 18). For a name-keyed program,
// defaultWidthX is the default width its Private DICT states, in font units,
// 0 when it states none (Table 23).
func EmptyCharStrings(data []byte) (defaultWidthX float64, cidKeyed, ok bool) {
	if len(data) < 4 || data[0] != 1 {
		return 0, false, false
	}
	nameEnd, err := skipINDEX(data, max(int(data[2]), 4))
	if err != nil {
		return 0, false, false
	}
	_, topDicts, err := parseINDEX(data, nameEnd)
	if err != nil || len(topDicts) == 0 {
		return 0, false, false
	}
	top := parseTopDict(topDicts[0])
	if top.charStringsOffset < 0 || int(top.charStringsOffset) >= len(data) {
		return 0, false, false
	}
	_, charStrings, err := parseINDEX(data, int(top.charStringsOffset))
	if err != nil || len(charStrings) != 0 {
		return 0, false, false
	}
	if top.isCID {
		return 0, true, true
	}
	if top.privateDictOffset > 0 && top.privateDictSize > 0 {
		start := int(top.privateDictOffset)
		if end := start + int(top.privateDictSize); end <= len(data) {
			_, defaultWidthX, _ = parsePrivateDict(data[start:end])
		}
	}
	return defaultWidthX, false, true
}

// scanDict calls fn with each operator of a CFF DICT and the operands before
// it, integers and reals alike, as float64 (Adobe Technical Note #5176, Table
// 3). Two-byte operators come as 1200 plus their second byte. A number cut off
// by the end of the data ends the scan.
func scanDict(dict []byte, fn func(op int, operands []float64)) {
	operands := make([]float64, 0, maxDictOperands)
	for i := 0; i < len(dict); {
		b := dict[i]
		// Operands past the stack limit belong to no operator a conforming
		// DICT writes; dropping them keeps the slice bounded.
		if len(operands) >= maxDictOperands && b > 21 {
			operands = operands[:0]
		}
		switch {
		case b <= 21:
			op := int(b)
			if b == 12 && i+1 < len(dict) {
				i++
				op = 1200 + int(dict[i])
			}
			fn(op, operands)
			operands = operands[:0]
			i++
		case b >= 32 && b <= 246:
			operands = append(operands, float64(int32(b)-139))
			i++
		case b >= 247 && b <= 250:
			if i+1 >= len(dict) {
				return
			}
			operands = append(operands, float64((int32(b)-247)*256+int32(dict[i+1])+108))
			i += 2
		case b >= 251 && b <= 254:
			if i+1 >= len(dict) {
				return
			}
			operands = append(operands, float64(-(int32(b)-251)*256-int32(dict[i+1])-108))
			i += 2
		case b == 28:
			if i+2 >= len(dict) {
				return
			}
			operands = append(operands, float64(int16(binary.BigEndian.Uint16(dict[i+1:i+3])))) //nolint:gosec // G115: a two-byte DICT operand is a two's-complement int16.
			i += 3
		case b == 29:
			if i+4 >= len(dict) {
				return
			}
			operands = append(operands, float64(int32(binary.BigEndian.Uint32(dict[i+1:i+5])))) //nolint:gosec // G115: a four-byte DICT operand is a two's-complement int32.
			i += 5
		case b == 30:
			val, end := parseRealNumber(dict, i+1)
			operands = append(operands, val)
			i = end
		default:
			i++
		}
	}
}
