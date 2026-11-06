package text

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
)

// errGoTextUnsafe reports a font program that would make go-text's parser
// panic. Litho never recovers from a panic, so it refuses such a program
// before go-text reads it.
var errGoTextUnsafe = errors.New("font program go-text cannot parse safely")

// checkGoTextSafe refuses the face whose table directory starts at dir in
// data when it holds a construct go-text v0.3.5 panics on instead of
// returning an error:
//
//   - a cmap format 4 segment whose idRangeOffset points before the
//     glyphIdArray (ISO/IEC 14496-22, cmap format 4): go-text indexes the
//     array with a negative offset;
//   - a loca entry that decreases or points past the glyf table (ISO/IEC
//     14496-22, loca table): go-text slices glyf with it unchecked.
//
// A program litho cannot read here is left to go-text, which reports its
// own error.
func checkGoTextSafe(data []byte, dir int64) error {
	if dir < 0 || dir >= int64(len(data)) {
		return nil
	}
	records, err := truetype.ParseTableDirectory(data[dir:])
	if err != nil {
		return nil //nolint:nilerr // go-text parses the directory itself and reports the error
	}
	if err := checkLoca(data, records); err != nil {
		return fmt.Errorf("%w: %w", errGoTextUnsafe, err)
	}
	cmap, err := truetype.TableData(data, records, "cmap")
	if err != nil || len(cmap) < 4 {
		return nil //nolint:nilerr // a missing or truncated cmap is go-text's error to report
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

// checkLoca checks that every glyph's loca range lies within glyf and does
// not run backwards. A table litho cannot read is left to go-text.
func checkLoca(data []byte, records []truetype.TableRecord) error {
	head, err := truetype.TableData(data, records, "head")
	if err != nil || len(head) < 54 {
		return nil //nolint:nilerr // go-text reports a missing or short head itself
	}
	maxp, err := truetype.TableData(data, records, "maxp")
	if err != nil || len(maxp) < 6 {
		return nil //nolint:nilerr // go-text reports a missing or short maxp itself
	}
	loca, err := truetype.TableData(data, records, "loca")
	if err != nil {
		return nil //nolint:nilerr // a font without loca has no glyf for go-text to slice
	}
	glyf, err := truetype.TableData(data, records, "glyf")
	if err != nil {
		glyf = nil
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
	for i := range numGlyphs {
		start, end := entry(i), entry(i+1)
		if start != end && (start > end || end > uint64(len(glyf))) {
			return fmt.Errorf("glyph %d: loca range [%d, %d) outside a glyf table of %d bytes", i, start, end, len(glyf))
		}
	}
	return nil
}
