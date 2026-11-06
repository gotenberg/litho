package text

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// errGoTextUnsafe reports a font program that would make go-text's parser
// panic. Litho never recovers from a panic, so it refuses such a program
// before go-text reads it.
var errGoTextUnsafe = errors.New("font program go-text cannot parse safely")

// goTextTables are the tables of a face [checkGoTextSafe] reads, each nil
// when the face has none, and the length of its glyf table, 0 without one.
type goTextTables struct {
	cmap, head, maxp, loca, hhea, vhea []byte
	glyfLen                            int64
}

// checkGoTextSafe refuses a face whose tables hold a construct go-text
// v0.3.5 panics on instead of returning an error:
//
//   - a cmap format 4 segment whose idRangeOffset points before the
//     glyphIdArray (ISO/IEC 14496-22, cmap format 4): go-text indexes the
//     array with a negative offset;
//   - a loca entry that decreases or points past the glyf table (ISO/IEC
//     14496-22, loca table): go-text slices glyf with it unchecked;
//   - an hhea numberOfHMetrics or a vhea numOfLongVerMetrics above maxp's
//     numGlyphs (ISO/IEC 14496-22, hhea and vhea tables): go-text sizes the
//     side bearing array with the negative difference; or of 0 for a face
//     with glyphs, where the specification asks for at least one: go-text
//     reads the advance of the last long metric, which does not exist.
//
// A table litho cannot read here is left to go-text, which reports its own
// error.
func checkGoTextSafe(t goTextTables) error {
	cmap := t.cmap
	if err := checkLoca(t.head, t.maxp, t.loca, t.glyfLen); err != nil {
		return fmt.Errorf("%w: %w", errGoTextUnsafe, err)
	}
	if err := checkLongMetrics(t.maxp, t.hhea, "hhea"); err != nil {
		return fmt.Errorf("%w: %w", errGoTextUnsafe, err)
	}
	if err := checkLongMetrics(t.maxp, t.vhea, "vhea"); err != nil {
		return fmt.Errorf("%w: %w", errGoTextUnsafe, err)
	}
	if len(cmap) < 4 {
		return nil
	}
	numTables := int(binary.BigEndian.Uint16(cmap[2:]))
	for i := range numTables {
		rec := 4 + 8*i
		if rec+8 > len(cmap) {
			return nil
		}
		off := binary.BigEndian.Uint32(cmap[rec+4:])
		if uint64(off)+2 > uint64(len(cmap)) {
			continue
		}
		sub := cmap[off:]
		if binary.BigEndian.Uint16(sub) != 4 {
			continue
		}
		if err := checkCmap4(sub); err != nil {
			return fmt.Errorf("%w: cmap subtable %d: %w", errGoTextUnsafe, i, err)
		}
	}
	return nil
}

// checkLongMetrics checks that the long metrics count of header, an hhea or
// vhea table, lies between 1 and maxp's numGlyphs. Both tables share the
// layout: the count is the last uint16 of their 36 bytes.
func checkLongMetrics(maxp, header []byte, tag string) error {
	if len(maxp) < 6 || len(header) < 36 {
		return nil
	}
	numGlyphs := binary.BigEndian.Uint16(maxp[4:])
	if long := binary.BigEndian.Uint16(header[34:]); long > numGlyphs || (long == 0 && numGlyphs > 0) {
		return fmt.Errorf("%s counts %d long metrics for %d glyphs", tag, long, numGlyphs)
	}
	return nil
}

// checkCmap4 checks the idRangeOffset of every segment of the cmap format 4
// subtable sub against the segment's position.
func checkCmap4(sub []byte) error {
	if len(sub) < 14 {
		return nil
	}
	segCount := int(binary.BigEndian.Uint16(sub[6:]) / 2)
	start := 16 + 2*segCount
	ranges := 16 + 6*segCount
	if len(sub) < ranges+2*segCount {
		return nil
	}
	for i := range segCount {
		startCode := binary.BigEndian.Uint16(sub[start+2*i:])
		rangeOffset := int(binary.BigEndian.Uint16(sub[ranges+2*i:]))
		if startCode != 0xFFFF && rangeOffset != 0 && rangeOffset/2+i < segCount {
			return fmt.Errorf("segment %d: idRangeOffset %d points before the glyph array", i, rangeOffset)
		}
	}
	return nil
}

// checkLoca checks that every glyph's loca range lies within a glyf table
// of glyfLen bytes and does not run backwards. A table litho cannot read is
// left to go-text.
func checkLoca(head, maxp, loca []byte, glyfLen int64) error {
	if len(head) < 54 || len(maxp) < 6 || loca == nil {
		return nil
	}
	numGlyphs := int(binary.BigEndian.Uint16(maxp[4:]))
	long := int16(binary.BigEndian.Uint16(head[50:])) == 1 //nolint:gosec // reinterprets the indexToLocFormat bits as the int16 the table stores
	entry := func(i int) uint64 {
		if long {
			return uint64(binary.BigEndian.Uint32(loca[4*i:]))
		}
		return 2 * uint64(binary.BigEndian.Uint16(loca[2*i:]))
	}
	size := 2
	if long {
		size = 4
	}
	if len(loca) < size*(numGlyphs+1) {
		return nil
	}
	glyf := uint64(max(glyfLen, 0))
	for i := range numGlyphs {
		start, end := entry(i), entry(i+1)
		if start != end && (start > end || end > glyf) {
			return fmt.Errorf("glyph %d: loca range [%d, %d) outside a glyf table of %d bytes", i, start, end, glyf)
		}
	}
	return nil
}
