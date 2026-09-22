package truetype

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"
)

// buildTableDirectory creates a minimal TrueType font header with the given table records.
func buildTableDirectory(tables map[string][]byte) []byte {
	numTables := len(tables)
	headerSize := 12 + numTables*16

	// Calculate total size.
	totalSize := headerSize
	for _, data := range tables {
		totalSize += len(data)
	}

	buf := make([]byte, totalSize)

	// Offset subtable: sfVersion=0x00010000, numTables, searchRange, entrySelector, rangeShift.
	binary.BigEndian.PutUint32(buf[0:4], 0x00010000)
	binary.BigEndian.PutUint16(buf[4:6], uint16(numTables))
	// searchRange, entrySelector, rangeShift are not validated by our parser, leave as 0.

	offset := uint32(headerSize)
	i := 0
	for tag, data := range tables {
		recOff := 12 + i*16
		copy(buf[recOff:recOff+4], tag)
		// checksum (not validated): leave as 0
		binary.BigEndian.PutUint32(buf[recOff+8:recOff+12], offset)
		binary.BigEndian.PutUint32(buf[recOff+12:recOff+16], uint32(len(data)))
		copy(buf[offset:], data)
		offset += uint32(len(data))
		i++
	}

	return buf
}

func TestNumGlyphs(t *testing.T) {
	t.Parallel()
	maxp := make([]byte, 6)
	binary.BigEndian.PutUint16(maxp[4:6], 1674)
	got, err := NumGlyphs(buildTableDirectory(map[string][]byte{"maxp": maxp}))
	if err != nil {
		t.Fatalf("NumGlyphs: %v", err)
	}
	if got != 1674 {
		t.Errorf("NumGlyphs = %d, want 1674", got)
	}
	if _, err := NumGlyphs(buildTableDirectory(map[string][]byte{"head": make([]byte, 54)})); err == nil {
		t.Error("NumGlyphs without a maxp table: want error, got nil")
	}
}

// buildCmapFormat4 creates a Format 4 cmap subtable for the given char→GID mappings.
// Only handles simple contiguous segments.
func buildCmapFormat4(mappings map[uint16]uint16) []byte {
	type segment struct {
		startCode uint16
		endCode   uint16
		delta     int16
	}

	// Build segments from mappings (simple: one segment per mapping for test).
	var segs []segment
	for charCode, gid := range mappings {
		segs = append(segs, segment{
			startCode: charCode,
			endCode:   charCode,
			delta:     int16(gid) - int16(charCode),
		})
	}
	// Add the mandatory 0xFFFF sentinel segment.
	segs = append(segs, segment{startCode: 0xFFFF, endCode: 0xFFFF, delta: 1})

	segCount := len(segs)
	length := 14 + segCount*8 + 2 // header + 4 arrays + reservedPad

	data := make([]byte, length)
	binary.BigEndian.PutUint16(data[0:2], 4)                  // format
	binary.BigEndian.PutUint16(data[2:4], uint16(length))     // length
	binary.BigEndian.PutUint16(data[6:8], uint16(segCount*2)) // segCountX2

	endCountOff := 14
	startCountOff := endCountOff + segCount*2 + 2
	idDeltaOff := startCountOff + segCount*2
	idRangeOffOff := idDeltaOff + segCount*2

	for i, seg := range segs {
		binary.BigEndian.PutUint16(data[endCountOff+i*2:], seg.endCode)
		binary.BigEndian.PutUint16(data[startCountOff+i*2:], seg.startCode)
		binary.BigEndian.PutUint16(data[idDeltaOff+i*2:], uint16(seg.delta))
		binary.BigEndian.PutUint16(data[idRangeOffOff+i*2:], 0) // rangeOffset=0
	}

	return data
}

// buildCmapTable wraps a subtable with the cmap table header.
func buildCmapTable(platformID, encodingID uint16, subtable []byte) []byte {
	// cmap header: version(2) + numSubtables(2) + record(8) + subtable data.
	header := make([]byte, 4+8)
	binary.BigEndian.PutUint16(header[0:2], 0) // version
	binary.BigEndian.PutUint16(header[2:4], 1) // numSubtables
	binary.BigEndian.PutUint16(header[4:6], platformID)
	binary.BigEndian.PutUint16(header[6:8], encodingID)
	binary.BigEndian.PutUint32(header[8:12], uint32(len(header))) // offset to subtable

	return append(header, subtable...)
}

// buildCmapTableMultiple builds a cmap table containing several subtables.
// The records are emitted in the same order as ids; each record's offset
// points at the supplied subtable bytes appended after the records block.
func buildCmapTableMultiple(ids []CmapSubtable, subtables [][]byte) []byte {
	if len(ids) != len(subtables) {
		panic("buildCmapTableMultiple: ids and subtables length mismatch")
	}
	headerSize := 4 + 8*len(ids)
	out := make([]byte, headerSize)
	binary.BigEndian.PutUint16(out[0:2], 0)                // version
	binary.BigEndian.PutUint16(out[2:4], uint16(len(ids))) //nolint:gosec // small
	offset := uint32(headerSize)
	for i, id := range ids {
		recOff := 4 + i*8
		binary.BigEndian.PutUint16(out[recOff:recOff+2], id.PlatformID)
		binary.BigEndian.PutUint16(out[recOff+2:recOff+4], id.EncodingID)
		binary.BigEndian.PutUint32(out[recOff+4:recOff+8], offset)
		offset += uint32(len(subtables[i]))
	}
	for _, sub := range subtables {
		out = append(out, sub...)
	}
	return out
}

func TestGlyphToUnicode_Format4(t *testing.T) {
	// Build a font with a Format 4 cmap: 'A'(65)→GID 1, 'B'(66)→GID 2, 'z'(122)→GID 50.
	charToGID := map[uint16]uint16{
		65:  1,
		66:  2,
		122: 50,
	}

	subtable := buildCmapFormat4(charToGID)
	cmapTable := buildCmapTable(3, 1, subtable)

	fontData := buildTableDirectory(map[string][]byte{
		"cmap": cmapTable,
	})

	gidMap, err := GlyphToUnicode(context.Background(), fontData)
	if err != nil {
		t.Fatalf("GlyphToUnicode failed: %v", err)
	}

	tests := []struct {
		gid  uint16
		want rune
	}{
		{1, 'A'},
		{2, 'B'},
		{50, 'z'},
	}

	for _, tc := range tests {
		r, ok := gidMap[tc.gid]
		if !ok {
			t.Errorf("GID %d: not found in map", tc.gid)
		} else if r != tc.want {
			t.Errorf("GID %d: got %U, want %U", tc.gid, r, tc.want)
		}
	}
}

// TestCmapSubtables_Enumerates exposes every (platform, encoding) record
// declared by the font so the typography layer can pick MacRomanEncoding
// when only the Mac Roman subtable is present (or alongside MS Unicode).
// The returned order mirrors the order in the cmap table.
func TestCmapSubtables_Enumerates(t *testing.T) {
	subA := buildCmapFormat4(map[uint16]uint16{65: 1})
	subB := buildCmapFormat4(map[uint16]uint16{66: 2})
	cmap := buildCmapTableMultiple(
		[]CmapSubtable{{PlatformID: 1, EncodingID: 0}, {PlatformID: 3, EncodingID: 1}},
		[][]byte{subA, subB},
	)
	font := buildTableDirectory(map[string][]byte{"cmap": cmap})

	got, err := CmapSubtables(font)
	if err != nil {
		t.Fatalf("CmapSubtables: %v", err)
	}
	want := []CmapSubtable{{PlatformID: 1, EncodingID: 0}, {PlatformID: 3, EncodingID: 1}}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (got=%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("subtable[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestGlyphToUnicode_Format12(t *testing.T) {
	// Build a Format 12 subtable with one group: U+1F600 (😀) → GID 100.
	numGroups := 1
	subtable := make([]byte, 16+numGroups*12)
	binary.BigEndian.PutUint16(subtable[0:2], 12)                    // format
	binary.BigEndian.PutUint32(subtable[4:8], uint32(len(subtable))) // length
	binary.BigEndian.PutUint32(subtable[12:16], uint32(numGroups))
	// Group: startCharCode=0x1F600, endCharCode=0x1F600, startGlyphID=100.
	binary.BigEndian.PutUint32(subtable[16:20], 0x1F600)
	binary.BigEndian.PutUint32(subtable[20:24], 0x1F600)
	binary.BigEndian.PutUint32(subtable[24:28], 100)

	cmapTable := buildCmapTable(3, 10, subtable)
	fontData := buildTableDirectory(map[string][]byte{
		"cmap": cmapTable,
	})

	gidMap, err := GlyphToUnicode(context.Background(), fontData)
	if err != nil {
		t.Fatalf("GlyphToUnicode failed: %v", err)
	}

	if r, ok := gidMap[100]; !ok || r != 0x1F600 {
		t.Errorf("GID 100: got %U (ok=%v), want U+1F600", r, ok)
	}
}

func TestGlyphToUnicode_PreferFormat12(t *testing.T) {
	// Font with both Format 4 (platform 3/1) and Format 12 (platform 3/10).
	// Format 12 should be preferred.

	// Format 4: 'X'(88)→GID 5
	fmt4 := buildCmapFormat4(map[uint16]uint16{88: 5})

	// Format 12: U+1F4A9→GID 42
	fmt12 := make([]byte, 16+12)
	binary.BigEndian.PutUint16(fmt12[0:2], 12)
	binary.BigEndian.PutUint32(fmt12[4:8], uint32(len(fmt12)))
	binary.BigEndian.PutUint32(fmt12[12:16], 1) // numGroups
	binary.BigEndian.PutUint32(fmt12[16:20], 0x1F4A9)
	binary.BigEndian.PutUint32(fmt12[20:24], 0x1F4A9)
	binary.BigEndian.PutUint32(fmt12[24:28], 42)

	// Build cmap table with two subtables.
	cmapData := make([]byte, 4+2*8)
	binary.BigEndian.PutUint16(cmapData[2:4], 2) // numSubtables

	// Record 0: platform 3, encoding 1 (Format 4).
	binary.BigEndian.PutUint16(cmapData[4:6], 3)
	binary.BigEndian.PutUint16(cmapData[6:8], 1)
	subtable0Off := uint32(len(cmapData))
	binary.BigEndian.PutUint32(cmapData[8:12], subtable0Off)

	// Record 1: platform 3, encoding 10 (Format 12).
	binary.BigEndian.PutUint16(cmapData[12:14], 3)
	binary.BigEndian.PutUint16(cmapData[14:16], 10)
	subtable1Off := subtable0Off + uint32(len(fmt4))
	binary.BigEndian.PutUint32(cmapData[16:20], subtable1Off)

	cmapData = append(cmapData, fmt4...)
	cmapData = append(cmapData, fmt12...)

	fontData := buildTableDirectory(map[string][]byte{"cmap": cmapData})

	gidMap, err := GlyphToUnicode(context.Background(), fontData)
	if err != nil {
		t.Fatalf("GlyphToUnicode failed: %v", err)
	}

	// Should have the Format 12 mapping.
	if r, ok := gidMap[42]; !ok || r != 0x1F4A9 {
		t.Errorf("GID 42: got %U (ok=%v), want U+1F4A9", r, ok)
	}
}

func TestGlyphToUnicode_NoCmap(t *testing.T) {
	fontData := buildTableDirectory(map[string][]byte{
		"post": make([]byte, 32), // no cmap table
	})

	_, err := GlyphToUnicode(context.Background(), fontData)
	if !errors.Is(err, ErrTableNotFound) {
		t.Errorf("expected ErrTableNotFound, got: %v", err)
	}
}

func TestGlyphToUnicode_InvalidData(t *testing.T) {
	_, err := GlyphToUnicode(context.Background(), []byte{0, 1, 2})
	if !errors.Is(err, ErrInvalidFont) {
		t.Errorf("expected ErrInvalidFont, got: %v", err)
	}
}

// buildCmapFormat12 builds a Format 12 subtable from raw groups so
// adversarial-range tests can construct ranges no legitimate font
// would emit.
func buildCmapFormat12(groups [][3]uint32) []byte {
	subtable := make([]byte, 16+len(groups)*12)
	binary.BigEndian.PutUint16(subtable[0:2], 12)
	binary.BigEndian.PutUint32(subtable[4:8], uint32(len(subtable)))
	binary.BigEndian.PutUint32(subtable[12:16], uint32(len(groups)))
	for i, g := range groups {
		off := 16 + i*12
		binary.BigEndian.PutUint32(subtable[off:off+4], g[0])
		binary.BigEndian.PutUint32(subtable[off+4:off+8], g[1])
		binary.BigEndian.PutUint32(subtable[off+8:off+12], g[2])
	}
	return subtable
}

func TestGlyphToUnicode_Format12_ClampsHugeRange(t *testing.T) {
	// startCharCode=0, endCharCode=0xFFFFFFFE: without the clamp the
	// parser would attempt ~4 billion map entries. The fix caps the
	// range at U+10FFFF.
	subtable := buildCmapFormat12([][3]uint32{{0, 0xFFFFFFFE, 1}})
	fontData := buildTableDirectory(map[string][]byte{
		"cmap": buildCmapTable(3, 10, subtable),
	})

	gidMap, err := GlyphToUnicode(context.Background(), fontData)
	if err != nil {
		t.Fatalf("GlyphToUnicode failed: %v", err)
	}
	if got, max := len(gidMap), maxUnicodeCodePoint+1; got > max {
		t.Errorf("clamp violated: %d entries, want <= %d", got, max)
	}
}

func TestGlyphToUnicode_Format12_SkipsReversedGroup(t *testing.T) {
	// startCharCode > endCharCode is a malformed group; expect zero
	// contributions instead of a wrap-around or a panic.
	subtable := buildCmapFormat12([][3]uint32{
		{0x2000, 0x1000, 1}, // reversed
		{0x41, 0x41, 100},   // valid: 'A' -> GID 100
	})
	fontData := buildTableDirectory(map[string][]byte{
		"cmap": buildCmapTable(3, 10, subtable),
	})

	gidMap, err := GlyphToUnicode(context.Background(), fontData)
	if err != nil {
		t.Fatalf("GlyphToUnicode failed: %v", err)
	}
	if r := gidMap[100]; r != 0x41 {
		t.Errorf("GID 100: got %U, want U+0041", r)
	}
	if len(gidMap) != 1 {
		t.Errorf("got %d entries, want 1 (reversed group should contribute none)", len(gidMap))
	}
}

func TestGlyphToUnicode_Format12_RespectsContextCancel(t *testing.T) {
	// One group spanning the full Unicode range. With a pre-cancelled
	// context the inner loop must return ctx.Err() rather than
	// running to completion.
	subtable := buildCmapFormat12([][3]uint32{{0, maxUnicodeCodePoint, 1}})
	fontData := buildTableDirectory(map[string][]byte{
		"cmap": buildCmapTable(3, 10, subtable),
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := GlyphToUnicode(ctx, fontData); !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

func TestGlyphToUnicode_Format4_RespectsContextCancel(t *testing.T) {
	// Build a Format 4 cmap covering the full BMP via a single
	// segment, then pre-cancel the context.
	mappings := map[uint16]uint16{}
	for code := range uint16(0xFFFE) {
		mappings[code] = code + 1
	}
	subtable := buildCmapFormat4(mappings)
	fontData := buildTableDirectory(map[string][]byte{
		"cmap": buildCmapTable(3, 1, subtable),
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := GlyphToUnicode(ctx, fontData); !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
}

func TestPostGlyphNames_Format1(t *testing.T) {
	// Post format 1.0: version=1.0 (0x00010000), then 28 bytes of fixed data.
	postData := make([]byte, 32)
	binary.BigEndian.PutUint16(postData[0:2], 1) // major=1
	binary.BigEndian.PutUint16(postData[2:4], 0) // minor=0

	fontData := buildTableDirectory(map[string][]byte{"post": postData})

	names, err := PostGlyphNames(context.Background(), fontData)
	if err != nil {
		t.Fatalf("PostGlyphNames failed: %v", err)
	}

	// GID 0 should be ".notdef".
	if names[0] != ".notdef" {
		t.Errorf("GID 0: got %q, want .notdef", names[0])
	}
	// GID 3 should be "space".
	if names[3] != "space" {
		t.Errorf("GID 3: got %q, want space", names[3])
	}
	// GID 36 (A) should be "A".
	if names[36] != "A" {
		t.Errorf("GID 36: got %q, want A", names[36])
	}
}

func TestPostGlyphNames_Format2(t *testing.T) {
	// Post format 2.0: custom glyph names.
	// 2 glyphs: GID 0 → ".notdef" (index 0), GID 1 → "myGlyph" (index 258).
	postData := make([]byte, 32) // fixed header
	binary.BigEndian.PutUint16(postData[0:2], 2)
	binary.BigEndian.PutUint16(postData[2:4], 0)

	// numGlyphs
	numGlyphs := make([]byte, 2)
	binary.BigEndian.PutUint16(numGlyphs, 2)
	postData = append(postData, numGlyphs...)

	// Glyph name indices: GID 0→0 (standard), GID 1→258 (first extra name).
	indices := make([]byte, 4)
	binary.BigEndian.PutUint16(indices[0:2], 0)
	binary.BigEndian.PutUint16(indices[2:4], 258)
	postData = append(postData, indices...)

	// Extra names: Pascal string "myGlyph".
	postData = append(postData, 7) // length
	postData = append(postData, "myGlyph"...)

	fontData := buildTableDirectory(map[string][]byte{"post": postData})

	names, err := PostGlyphNames(context.Background(), fontData)
	if err != nil {
		t.Fatalf("PostGlyphNames failed: %v", err)
	}

	if names[0] != ".notdef" {
		t.Errorf("GID 0: got %q, want .notdef", names[0])
	}
	if names[1] != "myGlyph" {
		t.Errorf("GID 1: got %q, want myGlyph", names[1])
	}
}

func TestPostGlyphNames_NoPostTable(t *testing.T) {
	fontData := buildTableDirectory(map[string][]byte{
		"cmap": make([]byte, 4), // no post table
	})

	names, err := PostGlyphNames(context.Background(), fontData)
	if err != nil {
		t.Fatalf("PostGlyphNames failed: %v", err)
	}
	if names != nil {
		t.Errorf("expected nil for missing post table, got %v", names)
	}
}

func TestGlyphToUnicode_Format4_RangeSegment(t *testing.T) {
	// Build a Format 4 with a contiguous range: 'A'(65)–'D'(68) → GID 10–13.
	segCount := 2 // one range + sentinel
	length := 14 + segCount*8 + 2
	data := make([]byte, length)
	binary.BigEndian.PutUint16(data[0:2], 4)
	binary.BigEndian.PutUint16(data[2:4], uint16(length))
	binary.BigEndian.PutUint16(data[6:8], uint16(segCount*2))

	endCountOff := 14
	startCountOff := endCountOff + segCount*2 + 2
	idDeltaOff := startCountOff + segCount*2
	idRangeOffOff := idDeltaOff + segCount*2

	// Segment 0: start=65, end=68, delta=10-65=-55.
	binary.BigEndian.PutUint16(data[endCountOff:], 68)
	binary.BigEndian.PutUint16(data[startCountOff:], 65)
	binary.BigEndian.PutUint16(data[idDeltaOff:], uint16(65481)) // int16(-55) as uint16
	binary.BigEndian.PutUint16(data[idRangeOffOff:], 0)

	// Sentinel.
	binary.BigEndian.PutUint16(data[endCountOff+2:], 0xFFFF)
	binary.BigEndian.PutUint16(data[startCountOff+2:], 0xFFFF)
	binary.BigEndian.PutUint16(data[idDeltaOff+2:], 1)
	binary.BigEndian.PutUint16(data[idRangeOffOff+2:], 0)

	cmapTable := buildCmapTable(3, 1, data)
	fontData := buildTableDirectory(map[string][]byte{"cmap": cmapTable})

	gidMap, err := GlyphToUnicode(context.Background(), fontData)
	if err != nil {
		t.Fatalf("GlyphToUnicode failed: %v", err)
	}

	for gid := uint16(10); gid <= 13; gid++ {
		want := rune(65 + int(gid) - 10) // 'A' + offset
		if r, ok := gidMap[gid]; !ok || r != want {
			t.Errorf("GID %d: got %U (ok=%v), want %U", gid, r, ok, want)
		}
	}
}

// buildCmapFormat0 builds a Format 0 "byte encoding" subtable: a six-byte
// header followed by exactly 256 one-byte glyph IDs indexed by character code.
func buildCmapFormat0(codeToGID map[uint8]uint8) []byte {
	const size = 6 + 256
	data := make([]byte, size)
	binary.BigEndian.PutUint16(data[0:2], 0)    // format
	binary.BigEndian.PutUint16(data[2:4], size) // length
	binary.BigEndian.PutUint16(data[4:6], 0)    // language
	for code, gid := range codeToGID {
		data[6+int(code)] = gid
	}
	return data
}

// buildCmapFormat6 builds a Format 6 "trimmed table" subtable mapping
// len(gids) contiguous character codes starting at firstCode.
func buildCmapFormat6(firstCode uint16, gids []uint16) []byte {
	size := 10 + len(gids)*2
	data := make([]byte, size)
	binary.BigEndian.PutUint16(data[0:2], 6)                  // format
	binary.BigEndian.PutUint16(data[2:4], uint16(size))       //nolint:gosec // test subtable, far below 2^16
	binary.BigEndian.PutUint16(data[4:6], 0)                  // language
	binary.BigEndian.PutUint16(data[6:8], firstCode)          // firstCode
	binary.BigEndian.PutUint16(data[8:10], uint16(len(gids))) //nolint:gosec // test subtable, far below 2^16
	for i, gid := range gids {
		binary.BigEndian.PutUint16(data[10+i*2:], gid)
	}
	return data
}

// TestGlyphToUnicode_Format0 reaches the Format 0 reader, which nothing in this
// repository did: measured at 0.0% across the whole test suite before this.
//
// parseCmap keeps only its best-scoring candidate, and every other test here
// declares (3,1) or (3,10) while the minimalSfnt fuzz seed declares (3,0),
// which matches no case in the scoring switch and is dropped entirely. A lone
// (1,0) Macintosh Roman record is the only route to this reader.
func TestGlyphToUnicode_Format0(t *testing.T) {
	subtable := buildCmapFormat0(map[uint8]uint8{65: 1, 66: 2})
	fontData := buildTableDirectory(map[string][]byte{
		"cmap": buildCmapTable(1, 0, subtable),
	})

	gidMap, err := GlyphToUnicode(context.Background(), fontData)
	if err != nil {
		t.Fatalf("GlyphToUnicode: %v", err)
	}
	for gid, want := range map[uint16]rune{1: 'A', 2: 'B'} {
		if got, ok := gidMap[gid]; !ok || got != want {
			t.Errorf("GID %d = %q (present %t), want %q", gid, got, ok, want)
		}
	}
	// Every unmapped code holds glyph 0, and the reader skips a zero glyph ID:
	// zero means "no glyph", not "maps to .notdef".
	if _, ok := gidMap[0]; ok {
		t.Error("GID 0 is present; a zero glyph ID must not be recorded")
	}
}

// TestGlyphToUnicode_Format6 reaches the Format 6 reader, also at 0.0% before
// this. See TestGlyphToUnicode_Format0 for why a (1,0) record is required.
func TestGlyphToUnicode_Format6(t *testing.T) {
	// firstCode 65 with three entries: 'A'->1, 'B'->2, 'C'->0 (unmapped).
	subtable := buildCmapFormat6(65, []uint16{1, 2, 0})
	fontData := buildTableDirectory(map[string][]byte{
		"cmap": buildCmapTable(1, 0, subtable),
	})

	gidMap, err := GlyphToUnicode(context.Background(), fontData)
	if err != nil {
		t.Fatalf("GlyphToUnicode: %v", err)
	}
	for gid, want := range map[uint16]rune{1: 'A', 2: 'B'} {
		if got, ok := gidMap[gid]; !ok || got != want {
			t.Errorf("GID %d = %q (present %t), want %q", gid, got, ok, want)
		}
	}
	if _, ok := gidMap[0]; ok {
		t.Error("GID 0 is present; the third entry's zero glyph ID must be skipped")
	}
}
