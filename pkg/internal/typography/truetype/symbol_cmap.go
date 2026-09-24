package truetype

import (
	"context"
	"encoding/binary"
	"fmt"
)

// SymbolCharCodeToGID returns a character-code → glyph-ID map for a symbolic
// TrueType font. Per ISO 32000-1 §9.6.6.4, a symbolic TrueType font (one with
// no PDF /Encoding) resolves single-byte character codes through the font's own
// cmap: the (3,0) Microsoft Symbol subtable, whose codes live in one of the
// ranges [symbolCodeRanges] lists, or failing that the (1,0) Macintosh
// subtable. Returns nil when the font carries neither subtable, so the caller
// can decline to resolve rather than guess.
//
// # Corpus evidence
//
//   - govdocs thread_400_400573.pdf: its SymbolMT subset declares a 48-byte
//     (3,0) subtable where 42 bytes remain, the subtable holds only 0xF021
//     and 0xF022, and veraPDF reports no glyph-level finding for that font at
//     all.
func SymbolCharCodeToGID(ctx context.Context, data []byte) (map[uint16]uint16, error) {
	records, err := ParseTableDirectory(data)
	if err != nil {
		return nil, err
	}
	cmapData, err := TableData(data, records, "cmap")
	if err != nil {
		return nil, err
	}
	if len(cmapData) < 4 {
		return nil, fmt.Errorf("%w: cmap table too short", ErrInvalidFont)
	}
	numSubtables := int(binary.BigEndian.Uint16(cmapData[2:4]))
	if len(cmapData) < 4+numSubtables*8 {
		return nil, fmt.Errorf("%w: cmap subtable records truncated", ErrInvalidFont)
	}

	// Prefer the Microsoft Symbol subtable (3,0) over the Macintosh one (1,0).
	chosenPriority := 0
	var chosenOffset uint32
	for i := range numSubtables {
		off := 4 + i*8
		pid := binary.BigEndian.Uint16(cmapData[off : off+2])
		eid := binary.BigEndian.Uint16(cmapData[off+2 : off+4])
		var priority int
		switch {
		case pid == 3 && eid == 0:
			priority = 2
		case pid == 1 && eid == 0:
			priority = 1
		default:
			continue
		}
		if priority > chosenPriority {
			chosenPriority = priority
			chosenOffset = binary.BigEndian.Uint32(cmapData[off+4 : off+8])
		}
	}
	if chosenPriority == 0 {
		return nil, nil
	}
	if int(chosenOffset) >= len(cmapData) {
		return nil, fmt.Errorf("%w: cmap subtable offset out of bounds", ErrInvalidFont)
	}
	sub := cmapData[chosenOffset:]

	// A subtable whose declared length runs past the end of the cmap table is
	// malformed (the OpenType cmap length field is the subtable's byte size),
	// and its segments cannot be trusted to list every code the font maps. A
	// caller would read a code missing from such a partial map as a missing
	// glyph.
	if len(sub) >= 4 { //nolint:mnd // format and length fields
		if format := binary.BigEndian.Uint16(sub[0:2]); format == 4 || format == 6 {
			if declared := int(binary.BigEndian.Uint16(sub[2:4])); declared > len(sub) {
				return nil, fmt.Errorf("%w: cmap subtable declares %d bytes, %d remain", ErrInvalidFont, declared, len(sub))
			}
		}
	}

	codeToGID, err := cmapSubtableCodeToGID(ctx, sub)
	if err != nil {
		return nil, err
	}

	out := make(map[uint16]uint16, 256) //nolint:mnd // single-byte code space
	for code := range uint32(256) {
		for _, high := range symbolCodeRanges {
			if gid, ok := codeToGID[high+code]; ok && gid != 0 {
				out[uint16(code)] = gid //nolint:gosec // code < 256
				break
			}
		}
	}
	return out, nil
}

// symbolCodeRanges are the high bytes a symbolic font's single-byte codes are
// looked up under, in the order tried. ISO 32000-1, 9.6.6.4 places the codes
// of a (3,0) subtable in one of four ranges, 0x0000, 0xF000, 0xF100 or 0xF200,
// and prefixes each byte with the range's high byte. The 0xF000 range and the
// bare code come first, so a code either of them holds keeps its glyph.
var symbolCodeRanges = [...]uint32{0xF000, 0x0000, 0xF100, 0xF200}

// cmapSubtableCodeToGID decodes a single cmap subtable into a character-code →
// glyph-ID map. Unlike [parseCmap], which inverts the mapping to GID → rune and
// is lossy when several codes share a glyph, this keeps the native code → GID
// direction the symbol resolver needs. Supports the formats symbol subtables
// use in practice: 0 (byte table), 4 (segment mapping), and 6 (trimmed table).
func cmapSubtableCodeToGID(ctx context.Context, sub []byte) (map[uint32]uint16, error) {
	if len(sub) < 2 {
		return nil, fmt.Errorf("%w: cmap subtable too short", ErrInvalidFont)
	}
	format := binary.BigEndian.Uint16(sub[0:2])
	out := make(map[uint32]uint16)

	switch format {
	case 0:
		if len(sub) < 262 { //nolint:mnd // 6-byte header + 256 GID bytes
			return nil, fmt.Errorf("%w: format 0 data too short", ErrInvalidFont)
		}
		for code := range 256 {
			if gid := uint16(sub[6+code]); gid != 0 {
				out[uint32(code)] = gid
			}
		}

	case 6:
		if len(sub) < 10 { //nolint:mnd // minimum header size
			return nil, fmt.Errorf("%w: format 6 header too short", ErrInvalidFont)
		}
		firstCode := int(binary.BigEndian.Uint16(sub[6:8]))
		entryCount := int(binary.BigEndian.Uint16(sub[8:10]))
		if len(sub) < 10+entryCount*2 {
			return nil, fmt.Errorf("%w: format 6 data truncated", ErrInvalidFont)
		}
		for i := range entryCount {
			if gid := binary.BigEndian.Uint16(sub[10+i*2 : 10+i*2+2]); gid != 0 {
				out[uint32(firstCode+i)] = gid //nolint:gosec // firstCode and i derive from uint16 fields; the sum fits uint32
			}
		}

	case 4:
		if err := format4CodeToGID(ctx, sub, out); err != nil {
			return nil, err
		}

	default:
		return nil, fmt.Errorf("%w: unsupported symbol cmap format %d", ErrInvalidFont, format)
	}
	return out, nil
}

// format4CodeToGID fills out with the code → GID entries of a Format 4 subtable.
// It mirrors [parseCmapFormat4]'s segment arithmetic but stores the native
// code → GID direction.
func format4CodeToGID(ctx context.Context, data []byte, out map[uint32]uint16) error {
	if len(data) < 14 { //nolint:mnd // fixed header size
		return fmt.Errorf("%w: format 4 header too short", ErrInvalidFont)
	}
	segCount := int(binary.BigEndian.Uint16(data[6:8])) / 2
	headerLen := 14 + segCount*8 + 2 //nolint:mnd // arrays + reservedPad
	if len(data) < headerLen {
		return fmt.Errorf("%w: format 4 data truncated", ErrInvalidFont)
	}
	endCountOff := 14
	startCountOff := endCountOff + segCount*2 + 2 //nolint:mnd // +2 reservedPad
	idDeltaOff := startCountOff + segCount*2
	idRangeOffsetOff := idDeltaOff + segCount*2

	for i := range segCount {
		if err := ctx.Err(); err != nil {
			return err
		}
		endCode := binary.BigEndian.Uint16(data[endCountOff+i*2 : endCountOff+i*2+2])
		startCode := binary.BigEndian.Uint16(data[startCountOff+i*2 : startCountOff+i*2+2])
		idDelta := int16(binary.BigEndian.Uint16(data[idDeltaOff+i*2 : idDeltaOff+i*2+2])) //nolint:gosec // spec: signed delta
		rangeOff := binary.BigEndian.Uint16(data[idRangeOffsetOff+i*2 : idRangeOffsetOff+i*2+2])

		if startCode == 0xFFFF {
			break
		}
		for code := uint32(startCode); code <= uint32(endCode); code++ {
			if (code-uint32(startCode))%cmapCtxCheckEvery == 0 {
				if err := ctx.Err(); err != nil {
					return err
				}
			}
			var gid uint16
			if rangeOff == 0 {
				gid = uint16(int32(code) + int32(idDelta)) //nolint:gosec // spec: modular arithmetic
			} else {
				glyphIdx := int(rangeOff)/2 + int(code) - int(startCode) + i
				byteOff := idRangeOffsetOff + glyphIdx*2
				if byteOff+2 > len(data) {
					continue
				}
				gid = binary.BigEndian.Uint16(data[byteOff : byteOff+2])
				if gid != 0 {
					gid = uint16(int32(gid) + int32(idDelta)) //nolint:gosec // spec: modular arithmetic
				}
			}
			if gid != 0 {
				out[code] = gid
			}
		}
	}
	return nil
}
