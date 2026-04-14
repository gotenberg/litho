package mutations

import (
	"encoding/binary"
	"fmt"
	"sort"

	"github.com/gotenberg/litho/pkg/internal/typography/truetype"
)

// subsetTTF produces a TrueType font byte stream that retains only the
// glyphs in keepGIDs (plus GID 0 and any glyphs transitively referenced
// by composite glyph components). Unused glyph data is emptied rather
// than renumbered, so every original table that references glyph IDs
// (cmap, hmtx, ...) remains correct without modification. The resulting
// byte stream is dramatically smaller than the input because the glyf
// table shrinks to contain only the outlines the stamp actually needs.
//
// The tables that change: glyf (payload), loca (offsets), head
// (checkSumAdjustment). Everything else (cmap, hmtx, maxp, hhea, name,
// post, OS/2, cvt, fpgm, prep) is copied verbatim.
//
// Returns the subset bytes. On any parse failure, returns the original
// bytes unchanged so the caller can fall back to full embedding.
func subsetTTF(raw []byte, keepGIDs map[uint16]bool) ([]byte, error) {
	// Always keep .notdef.
	keep := make(map[uint16]bool, len(keepGIDs)+1)
	keep[0] = true
	for gid := range keepGIDs {
		keep[gid] = true
	}

	records, err := truetype.ParseTableDirectory(raw)
	if err != nil {
		return raw, fmt.Errorf("parse table directory: %w", err)
	}

	headData, err := truetype.TableData(raw, records, "head")
	if err != nil || len(headData) < 54 {
		return raw, fmt.Errorf("read head table: %w", err)
	}
	indexToLocFormat := int16(binary.BigEndian.Uint16(headData[50:52])) //nolint:gosec // signed by spec

	maxpData, err := truetype.TableData(raw, records, "maxp")
	if err != nil || len(maxpData) < 6 {
		return raw, fmt.Errorf("read maxp table: %w", err)
	}
	numGlyphs := int(binary.BigEndian.Uint16(maxpData[4:6]))

	locaData, err := truetype.TableData(raw, records, "loca")
	if err != nil {
		return raw, fmt.Errorf("read loca table: %w", err)
	}

	glyfData, err := truetype.TableData(raw, records, "glyf")
	if err != nil {
		return raw, fmt.Errorf("read glyf table: %w", err)
	}

	offsets, err := parseLocaOffsets(locaData, numGlyphs, indexToLocFormat)
	if err != nil {
		return raw, err
	}

	// Walk composite glyphs to expand the keep set with transitively
	// referenced components. Iterate to a fixed point because composites
	// can reference other composites.
	expandCompositeClosure(keep, glyfData, offsets)

	// Build the new glyf table: kept glyphs keep their original bytes,
	// unkept glyphs get zero-length entries. The order is preserved so
	// GIDs remain stable.
	newGlyf := make([]byte, 0, len(glyfData))
	newOffsets := make([]uint32, numGlyphs+1)
	glyfLen := uint32(len(glyfData)) //nolint:gosec // glyf table size fits in uint32 by spec
	for gid := range numGlyphs {
		newOffsets[gid] = uint32(len(newGlyf)) //nolint:gosec // bounded by glyfLen above
		if !keep[uint16(gid)] {                //nolint:gosec // numGlyphs fits in uint16 per spec
			continue
		}
		glyphStart := offsets[gid]
		glyphEnd := offsets[gid+1]
		if glyphEnd > glyfLen || glyphStart > glyphEnd {
			// Corrupt offsets, bail out.
			return raw, fmt.Errorf("corrupt loca offsets at gid %d", gid)
		}
		newGlyf = append(newGlyf, glyfData[glyphStart:glyphEnd]...)
		// Pad to 2-byte alignment (required for short loca format).
		if len(newGlyf)%2 != 0 {
			newGlyf = append(newGlyf, 0)
		}
	}
	newOffsets[numGlyphs] = uint32(len(newGlyf)) //nolint:gosec // bounded by glyfLen above

	// Decide loca format based on new offsets. Short format requires all
	// offsets to fit in uint16 when divided by 2 (so max 131070 bytes).
	useShortLoca := newOffsets[numGlyphs] <= 0x1FFFE
	newLoca := encodeLoca(newOffsets, useShortLoca)

	// Clone head and update indexToLocFormat; checkSumAdjustment is
	// recomputed after the full file is reassembled.
	newHead := append([]byte(nil), headData...)
	if useShortLoca {
		binary.BigEndian.PutUint16(newHead[50:52], 0)
	} else {
		binary.BigEndian.PutUint16(newHead[50:52], 1)
	}
	// Zero checkSumAdjustment before recomputing the file checksum.
	binary.BigEndian.PutUint32(newHead[8:12], 0)

	// Assemble the subset font. We keep every table but substitute
	// head / loca / glyf with our new versions.
	tables := make(map[string][]byte, len(records))
	for _, rec := range records {
		tag := rec.Tag
		data, err := truetype.TableData(raw, records, tag)
		if err != nil {
			return raw, fmt.Errorf("read table %q: %w", tag, err)
		}
		tables[tag] = data
	}
	tables["head"] = newHead
	tables["loca"] = newLoca
	tables["glyf"] = newGlyf

	subset := encodeTTF(tables)

	// Compute head.checkSumAdjustment = 0xB1B0AFBA - checksum(entire file).
	// The head checksum was computed with a zeroed checkSumAdjustment,
	// which is what we want. Now patch the final value into the head
	// table's bytes in-place.
	adjustment := uint32(0xB1B0AFBA) - tableChecksum(subset)
	headOffset := findTableOffset(subset, "head")
	if headOffset < 0 || headOffset+12 > len(subset) {
		return raw, fmt.Errorf("head table not found in subset for checksum adjustment")
	}
	binary.BigEndian.PutUint32(subset[headOffset+8:headOffset+12], adjustment)

	return subset, nil
}

// parseLocaOffsets decodes the loca table into a (numGlyphs+1)-length
// slice of absolute glyf offsets. Short format stores uint16 values that
// must be doubled; long format stores uint32 values directly.
func parseLocaOffsets(locaData []byte, numGlyphs int, indexToLocFormat int16) ([]uint32, error) {
	offsets := make([]uint32, numGlyphs+1)
	switch indexToLocFormat {
	case 0: // short
		if len(locaData) < (numGlyphs+1)*2 {
			return nil, fmt.Errorf("loca table too short for short format")
		}
		for i := range numGlyphs + 1 {
			offsets[i] = uint32(binary.BigEndian.Uint16(locaData[i*2:])) * 2
		}
	case 1: // long
		if len(locaData) < (numGlyphs+1)*4 {
			return nil, fmt.Errorf("loca table too short for long format")
		}
		for i := range numGlyphs + 1 {
			offsets[i] = binary.BigEndian.Uint32(locaData[i*4:])
		}
	default:
		return nil, fmt.Errorf("invalid indexToLocFormat %d", indexToLocFormat)
	}
	return offsets, nil
}

// encodeLoca serializes offsets into a loca table payload using either
// the short or long format.
func encodeLoca(offsets []uint32, useShort bool) []byte {
	if useShort {
		buf := make([]byte, len(offsets)*2)
		for i, off := range offsets {
			binary.BigEndian.PutUint16(buf[i*2:], uint16(off/2)) //nolint:gosec // constrained by useShort selection
		}
		return buf
	}
	buf := make([]byte, len(offsets)*4)
	for i, off := range offsets {
		binary.BigEndian.PutUint32(buf[i*4:], off)
	}
	return buf
}

// expandCompositeClosure walks the glyf table iteratively, expanding the
// keep set to include the transitively referenced components of every
// composite glyph currently in the set. Runs to a fixed point so
// composites that reference other composites are fully covered.
func expandCompositeClosure(keep map[uint16]bool, glyfData []byte, offsets []uint32) {
	glyfLen := uint32(len(glyfData)) //nolint:gosec // glyf table size fits in uint32
	for {
		added := false
		for gid := range keep {
			if int(gid)+1 >= len(offsets) {
				continue
			}
			start := offsets[gid]
			end := offsets[gid+1]
			if end > glyfLen || end <= start+10 {
				// Empty or too short to have a composite header.
				continue
			}
			data := glyfData[start:end]
			numberOfContours := int16(binary.BigEndian.Uint16(data[0:2])) //nolint:gosec // signed by spec
			if numberOfContours >= 0 {
				continue // simple glyph, no components
			}
			// Composite glyph header:
			//   int16 numberOfContours (-1)
			//   int16 xMin, yMin, xMax, yMax (8 bytes)
			//   then a sequence of components.
			//
			// Each component:
			//   uint16 flags
			//   uint16 glyphIndex
			//   (argument bytes, 1 or 2 based on flags)
			//   (optional transform bytes)
			//
			// We only need to walk components enough to extract
			// glyphIndex values. The MORE_COMPONENTS bit (0x0020)
			// indicates another component follows.
			pos := 10 // skip the 10-byte header
			for pos+4 <= len(data) {
				flags := binary.BigEndian.Uint16(data[pos : pos+2])
				componentGID := binary.BigEndian.Uint16(data[pos+2 : pos+4])
				if !keep[componentGID] {
					keep[componentGID] = true
					added = true
				}
				pos += 4
				// Argument bytes.
				if flags&0x0001 != 0 { // ARG_1_AND_2_ARE_WORDS
					pos += 4
				} else {
					pos += 2
				}
				// Transform bytes.
				switch {
				case flags&0x0008 != 0: // WE_HAVE_A_SCALE
					pos += 2
				case flags&0x0040 != 0: // WE_HAVE_AN_X_AND_Y_SCALE
					pos += 4
				case flags&0x0080 != 0: // WE_HAVE_A_TWO_BY_TWO
					pos += 8
				}
				if flags&0x0020 == 0 { // MORE_COMPONENTS
					break
				}
			}
		}
		if !added {
			return
		}
	}
}

// encodeTTF assembles a set of tables into a complete TrueType byte
// stream. Table directory order is alphabetical by tag (as is standard
// for OpenType fonts). Each table is written on a 4-byte boundary with
// zero-padding. Table checksums are computed on-the-fly.
func encodeTTF(tables map[string][]byte) []byte {
	// Sort tags for deterministic output.
	tags := make([]string, 0, len(tables))
	for tag := range tables {
		tags = append(tags, tag)
	}
	sort.Strings(tags)

	numTables := len(tags)
	headerSize := 12 + numTables*16

	// Compute each table's length, offset, and checksum.
	type tableInfo struct {
		tag      string
		data     []byte
		offset   uint32
		length   uint32
		checksum uint32
	}
	infos := make([]tableInfo, 0, numTables)
	offset := uint32(headerSize) //nolint:gosec // headerSize is tiny and bounded by numTables
	for _, tag := range tags {
		data := tables[tag]
		length := uint32(len(data)) //nolint:gosec // individual tables are bounded
		info := tableInfo{
			tag:      tag,
			data:     data,
			offset:   offset,
			length:   length,
			checksum: tableChecksum(data),
		}
		infos = append(infos, info)
		// Advance offset with 4-byte padding.
		offset += (length + 3) &^ 3
	}

	total := offset
	buf := make([]byte, total)

	// SFNT header.
	binary.BigEndian.PutUint32(buf[0:4], 0x00010000)        // version for TrueType
	binary.BigEndian.PutUint16(buf[4:6], uint16(numTables)) //nolint:gosec
	searchRange, entrySelector, rangeShift := sfntSearchParams(numTables)
	binary.BigEndian.PutUint16(buf[6:8], searchRange)
	binary.BigEndian.PutUint16(buf[8:10], entrySelector)
	binary.BigEndian.PutUint16(buf[10:12], rangeShift)

	// Table directory entries.
	for i, info := range infos {
		rec := buf[12+i*16 : 12+(i+1)*16]
		copy(rec[0:4], info.tag)
		binary.BigEndian.PutUint32(rec[4:8], info.checksum)
		binary.BigEndian.PutUint32(rec[8:12], info.offset)
		binary.BigEndian.PutUint32(rec[12:16], info.length)
	}

	// Table data.
	for _, info := range infos {
		copy(buf[info.offset:info.offset+info.length], info.data)
	}

	return buf
}

// tableChecksum computes the TrueType table checksum: the sum of each
// 4-byte big-endian word, with zero-padding on the tail if the length
// is not a multiple of 4.
func tableChecksum(data []byte) uint32 {
	var sum uint32
	i := 0
	for ; i+4 <= len(data); i += 4 {
		sum += binary.BigEndian.Uint32(data[i : i+4])
	}
	if i < len(data) {
		// Pad the tail with zeros before summing.
		var tail [4]byte
		copy(tail[:], data[i:])
		sum += binary.BigEndian.Uint32(tail[:])
	}
	return sum
}

// findTableOffset scans the SFNT table directory of data for a given
// tag and returns the byte offset of the table data, or -1 if absent.
func findTableOffset(data []byte, tag string) int {
	if len(data) < 12 {
		return -1
	}
	numTables := int(binary.BigEndian.Uint16(data[4:6]))
	if len(data) < 12+numTables*16 {
		return -1
	}
	for i := range numTables {
		rec := data[12+i*16 : 12+(i+1)*16]
		if string(rec[0:4]) == tag {
			return int(binary.BigEndian.Uint32(rec[8:12]))
		}
	}
	return -1
}

// sfntSearchParams computes the searchRange, entrySelector, and
// rangeShift fields of the SFNT header from the table count. These are
// a legacy optimization for binary-searching the table directory; most
// parsers ignore the values, but producing them correctly keeps
// validators happy.
func sfntSearchParams(numTables int) (searchRange, entrySelector, rangeShift uint16) {
	pow := 1
	shift := 0
	for pow*2 <= numTables {
		pow *= 2
		shift++
	}
	searchRange = uint16(pow * 16)                  //nolint:gosec
	entrySelector = uint16(shift)                   //nolint:gosec
	rangeShift = uint16(numTables*16) - searchRange //nolint:gosec
	return
}
