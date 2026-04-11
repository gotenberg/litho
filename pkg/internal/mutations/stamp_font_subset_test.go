package mutations

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// --- parseLocaOffsets ---

func TestParseLocaOffsets_Short(t *testing.T) {
	// Short loca stores uint16 values that must be doubled on read.
	// Four entries = three glyphs: offsets 0, 20, 36, 50.
	data := make([]byte, 8)
	binary.BigEndian.PutUint16(data[0:], 0)
	binary.BigEndian.PutUint16(data[2:], 10) // 10 * 2 = 20
	binary.BigEndian.PutUint16(data[4:], 18) // 18 * 2 = 36
	binary.BigEndian.PutUint16(data[6:], 25) // 25 * 2 = 50

	offsets, err := parseLocaOffsets(data, 3, 0)
	if err != nil {
		t.Fatalf("parseLocaOffsets: %v", err)
	}
	want := []uint32{0, 20, 36, 50}
	for i, got := range offsets {
		if got != want[i] {
			t.Errorf("offsets[%d] = %d, want %d", i, got, want[i])
		}
	}
}

func TestParseLocaOffsets_Long(t *testing.T) {
	// Long loca stores uint32 values directly.
	data := make([]byte, 16)
	binary.BigEndian.PutUint32(data[0:], 0)
	binary.BigEndian.PutUint32(data[4:], 100)
	binary.BigEndian.PutUint32(data[8:], 250)
	binary.BigEndian.PutUint32(data[12:], 400)

	offsets, err := parseLocaOffsets(data, 3, 1)
	if err != nil {
		t.Fatalf("parseLocaOffsets: %v", err)
	}
	want := []uint32{0, 100, 250, 400}
	for i, got := range offsets {
		if got != want[i] {
			t.Errorf("offsets[%d] = %d, want %d", i, got, want[i])
		}
	}
}

func TestParseLocaOffsets_ShortTooShort(t *testing.T) {
	// (numGlyphs+1)*2 bytes required. Provide only 4 bytes for 3 glyphs.
	_, err := parseLocaOffsets(make([]byte, 4), 3, 0)
	if err == nil {
		t.Error("expected error for short-format loca with insufficient data")
	}
}

func TestParseLocaOffsets_LongTooShort(t *testing.T) {
	// (numGlyphs+1)*4 bytes required. Provide only 8 bytes for 3 glyphs.
	_, err := parseLocaOffsets(make([]byte, 8), 3, 1)
	if err == nil {
		t.Error("expected error for long-format loca with insufficient data")
	}
}

func TestParseLocaOffsets_InvalidFormat(t *testing.T) {
	_, err := parseLocaOffsets(make([]byte, 16), 3, 2)
	if err == nil {
		t.Error("expected error for invalid indexToLocFormat")
	}
}

// --- encodeLoca ---

func TestEncodeLoca_ShortRoundTrip(t *testing.T) {
	offsets := []uint32{0, 20, 36, 50}
	encoded := encodeLoca(offsets, true)
	if len(encoded) != 8 {
		t.Fatalf("short encoded length: got %d, want 8", len(encoded))
	}
	decoded, err := parseLocaOffsets(encoded, 3, 0)
	if err != nil {
		t.Fatalf("parseLocaOffsets: %v", err)
	}
	for i, got := range decoded {
		if got != offsets[i] {
			t.Errorf("round-trip short[%d]: got %d, want %d", i, got, offsets[i])
		}
	}
}

func TestEncodeLoca_LongRoundTrip(t *testing.T) {
	offsets := []uint32{0, 100, 250, 400, 0x20000}
	encoded := encodeLoca(offsets, false)
	if len(encoded) != 20 {
		t.Fatalf("long encoded length: got %d, want 20", len(encoded))
	}
	decoded, err := parseLocaOffsets(encoded, 4, 1)
	if err != nil {
		t.Fatalf("parseLocaOffsets: %v", err)
	}
	for i, got := range decoded {
		if got != offsets[i] {
			t.Errorf("round-trip long[%d]: got %d, want %d", i, got, offsets[i])
		}
	}
}

// --- expandCompositeClosure ---

// buildSimpleGlyph returns glyf bytes for a simple glyph (non-composite).
// numberOfContours >= 0 identifies it as simple; the actual contour
// data is not inspected by expandCompositeClosure.
func buildSimpleGlyph() []byte {
	// numberOfContours=1, then 8 bytes of xMin/yMin/xMax/yMax, then
	// one dummy endPts entry. The composite walker only looks at
	// numberOfContours[0:2] so the rest can be zeros.
	b := make([]byte, 12)
	binary.BigEndian.PutUint16(b[0:], 1) // 1 contour
	return b
}

// buildCompositeGlyph returns glyf bytes for a composite glyph that
// references the given component GIDs. flags0 controls whether
// MORE_COMPONENTS and ARG_*_ARE_WORDS are set on the first component.
func buildCompositeGlyph(components []uint16) []byte {
	// Header: int16 numberOfContours (-1) + 4x int16 bbox = 10 bytes.
	var b bytes.Buffer
	hdr := make([]byte, 10)
	binary.BigEndian.PutUint16(hdr[0:], 0xFFFF) // -1 as uint16
	b.Write(hdr)
	// Each component: flags (2), glyphIndex (2), args (2 with default
	// ARG_1_AND_2_ARE_BYTES, i.e. flag bit 0x0001 clear).
	for i, gid := range components {
		var flags uint16
		if i < len(components)-1 {
			flags |= 0x0020 // MORE_COMPONENTS
		}
		entry := make([]byte, 6)
		binary.BigEndian.PutUint16(entry[0:], flags)
		binary.BigEndian.PutUint16(entry[2:], gid)
		// args: 2 dummy bytes
		b.Write(entry)
	}
	return b.Bytes()
}

func TestExpandCompositeClosure_SimpleGlyph(t *testing.T) {
	// Two glyphs: GID 0 (.notdef, simple) and GID 1 (simple).
	glyf := append(buildSimpleGlyph(), buildSimpleGlyph()...)
	offsets := []uint32{0, 12, 24}

	keep := map[uint16]bool{1: true}
	expandCompositeClosure(keep, glyf, offsets)
	if _, ok := keep[5]; ok {
		t.Error("simple glyph should not add unrelated GIDs")
	}
	if len(keep) != 1 {
		t.Errorf("keep set: got %d, want 1", len(keep))
	}
}

func TestExpandCompositeClosure_SingleComposite(t *testing.T) {
	// Build: GID 0 simple (12 bytes), GID 1 composite referencing GID 2,
	// GID 2 simple. Composite size = 10 header + 6 per-component = 16.
	simple0 := buildSimpleGlyph()
	composite := buildCompositeGlyph([]uint16{2})
	simple2 := buildSimpleGlyph()
	glyf := append([]byte{}, simple0...)
	glyf = append(glyf, composite...)
	glyf = append(glyf, simple2...)
	offsets := []uint32{
		0,
		uint32(len(simple0)),
		uint32(len(simple0) + len(composite)),
		uint32(len(simple0) + len(composite) + len(simple2)),
	}

	keep := map[uint16]bool{1: true}
	expandCompositeClosure(keep, glyf, offsets)

	if !keep[1] {
		t.Error("composite GID 1 should remain in keep set")
	}
	if !keep[2] {
		t.Error("component GID 2 should be added by composite closure")
	}
}

func TestExpandCompositeClosure_NestedComposite(t *testing.T) {
	// GID 0 simple, GID 1 composite→GID 2, GID 2 composite→GID 3, GID 3 simple.
	simple0 := buildSimpleGlyph()
	c1 := buildCompositeGlyph([]uint16{2})
	c2 := buildCompositeGlyph([]uint16{3})
	simple3 := buildSimpleGlyph()

	glyf := append([]byte{}, simple0...)
	glyf = append(glyf, c1...)
	glyf = append(glyf, c2...)
	glyf = append(glyf, simple3...)

	offsets := []uint32{
		0,
		uint32(len(simple0)),
		uint32(len(simple0) + len(c1)),
		uint32(len(simple0) + len(c1) + len(c2)),
		uint32(len(simple0) + len(c1) + len(c2) + len(simple3)),
	}

	// Seed only with GID 1. Closure must expand to {1, 2, 3}.
	keep := map[uint16]bool{1: true}
	expandCompositeClosure(keep, glyf, offsets)

	for _, gid := range []uint16{1, 2, 3} {
		if !keep[gid] {
			t.Errorf("nested closure: GID %d missing", gid)
		}
	}
}

func TestExpandCompositeClosure_MultipleComponents(t *testing.T) {
	// GID 1 composite → references GID 2 AND GID 3.
	simple0 := buildSimpleGlyph()
	c1 := buildCompositeGlyph([]uint16{2, 3})
	simple2 := buildSimpleGlyph()
	simple3 := buildSimpleGlyph()

	glyf := append([]byte{}, simple0...)
	glyf = append(glyf, c1...)
	glyf = append(glyf, simple2...)
	glyf = append(glyf, simple3...)
	offsets := []uint32{
		0,
		uint32(len(simple0)),
		uint32(len(simple0) + len(c1)),
		uint32(len(simple0) + len(c1) + len(simple2)),
		uint32(len(simple0) + len(c1) + len(simple2) + len(simple3)),
	}

	keep := map[uint16]bool{1: true}
	expandCompositeClosure(keep, glyf, offsets)

	if !keep[2] || !keep[3] {
		t.Errorf("multi-component closure: expected GID 2 and 3, keep=%v", keep)
	}
}

// --- encodeTTF / tableChecksum / ttfChecksum / findTableOffset ---

func TestTableChecksum_AlignmentPadding(t *testing.T) {
	// A 5-byte table is treated as 8 bytes with 3 trailing zeros for
	// checksum computation. Verify the expected sum matches what we
	// would compute by hand.
	data := []byte{0x01, 0x02, 0x03, 0x04, 0x05}
	// Word 1 = 0x01020304, word 2 = 0x05000000.
	want := uint32(0x01020304) + uint32(0x05000000)
	if got := tableChecksum(data); got != want {
		t.Errorf("checksum: got %08x, want %08x", got, want)
	}
}

func TestTableChecksum_Aligned(t *testing.T) {
	data := []byte{0xDE, 0xAD, 0xBE, 0xEF, 0xCA, 0xFE, 0xBA, 0xBE}
	// Sum wraps naturally at uint32; compute that way instead of via a
	// constant expression that would overflow Go's untyped literal.
	var want uint32
	want += 0xDEADBEEF
	want += 0xCAFEBABE
	if got := tableChecksum(data); got != want {
		t.Errorf("checksum: got %08x, want %08x", got, want)
	}
}

func TestEncodeTTF_TableDirectoryAlignment(t *testing.T) {
	// Tables with odd-length payloads must be aligned to 4-byte
	// boundaries in the assembled SFNT stream. Verify that the table
	// directory offsets reflect the padding.
	tables := map[string][]byte{
		"head": bytes.Repeat([]byte{0}, 54), // 54 bytes
		"cmap": bytes.Repeat([]byte{0}, 7),  // 7 bytes (pads to 8)
		"glyf": bytes.Repeat([]byte{0}, 13), // 13 bytes (pads to 16)
	}
	out := encodeTTF(tables)

	// After encoding, every table offset from the directory must be
	// divisible by 4.
	headerSize := 12 + len(tables)*16
	for i := range len(tables) {
		rec := out[12+i*16 : 12+(i+1)*16]
		offset := int(binary.BigEndian.Uint32(rec[8:12]))
		if offset < headerSize {
			t.Errorf("table %d offset %d is inside the header", i, offset)
		}
		if offset%4 != 0 {
			t.Errorf("table %d offset %d is not 4-byte aligned", i, offset)
		}
	}
}

func TestFindTableOffset_FoundAndMissing(t *testing.T) {
	tables := map[string][]byte{
		"head": bytes.Repeat([]byte{1}, 54),
		"cmap": bytes.Repeat([]byte{2}, 32),
	}
	out := encodeTTF(tables)

	headOff := findTableOffset(out, "head")
	if headOff <= 0 {
		t.Errorf("findTableOffset(head): got %d, want > 0", headOff)
	}
	cmapOff := findTableOffset(out, "cmap")
	if cmapOff <= 0 {
		t.Errorf("findTableOffset(cmap): got %d, want > 0", cmapOff)
	}
	missing := findTableOffset(out, "none")
	if missing != -1 {
		t.Errorf("findTableOffset(missing): got %d, want -1", missing)
	}
}

func TestTTFChecksum_HeadAdjustment(t *testing.T) {
	// After encodeTTF and a head.checkSumAdjustment patch of the form
	//   adjustment = 0xB1B0AFBA - checksum(file_with_zero_adjustment)
	// the whole-file checksum should equal 0xB1B0AFBA.
	//
	// We verify this invariant by building a tiny font and running the
	// patch manually, matching the logic in subsetTTF lines 133-142.
	head := make([]byte, 54)
	// Zero head.checkSumAdjustment is already the default.
	tables := map[string][]byte{
		"head": head,
		"cmap": bytes.Repeat([]byte{0x55}, 32),
		"maxp": bytes.Repeat([]byte{0x77}, 6),
	}
	out := encodeTTF(tables)

	adjustment := uint32(0xB1B0AFBA) - ttfChecksum(out)
	headOffset := findTableOffset(out, "head")
	binary.BigEndian.PutUint32(out[headOffset+8:headOffset+12], adjustment)

	if got := ttfChecksum(out); got != 0xB1B0AFBA {
		t.Errorf("post-patch file checksum: got %08x, want %08x", got, uint32(0xB1B0AFBA))
	}
}

// --- subsetTTF integration ---

func TestSubsetTTF_FallbackOnCorruptInput(t *testing.T) {
	// Garbage bytes cannot be parsed as a TTF. subsetTTF must return
	// the original slice unchanged plus an error so the caller can
	// log and fall back to full embedding.
	garbage := []byte("this is definitely not a TTF")
	out, err := subsetTTF(garbage, map[uint16]bool{1: true})
	if err == nil {
		t.Error("expected error on garbage input")
	}
	if !bytes.Equal(out, garbage) {
		t.Error("expected fallback to return original bytes unchanged")
	}
}

func TestSubsetTTF_RoundTripMinimalTTF(t *testing.T) {
	// Build a minimal TTF fixture, subset it keeping only GID 0 and 1,
	// and confirm the result still parses via parseTTF.
	raw := buildSubsetTestTTF(t)

	subset, err := subsetTTF(raw, map[uint16]bool{1: true})
	if err != nil {
		t.Fatalf("subsetTTF: %v", err)
	}
	if _, err := parseTTF(subset); err != nil {
		t.Fatalf("subset re-parse: %v", err)
	}
	// Subset should be the same size or smaller than the input (it
	// may be equal if all glyphs are kept, or smaller if some are
	// zeroed out).
	if len(subset) > len(raw) {
		t.Errorf("subset larger than input: %d > %d", len(subset), len(raw))
	}
}

func TestSubsetTTF_HeadChecksumAdjustment(t *testing.T) {
	// After subsetting, the file-level checksum invariant must hold:
	//   0xB1B0AFBA == ttfChecksum(subset after patching)
	// This is the canonical TTF well-formedness test.
	raw := buildSubsetTestTTF(t)
	subset, err := subsetTTF(raw, map[uint16]bool{1: true})
	if err != nil {
		t.Fatalf("subsetTTF: %v", err)
	}

	// Re-read head.checkSumAdjustment, zero it, recompute, verify.
	headOff := findTableOffset(subset, "head")
	if headOff < 0 {
		t.Fatal("head table not found in subset")
	}
	adjustment := binary.BigEndian.Uint32(subset[headOff+8 : headOff+12])

	// Rebuild with zero adjustment to compute the base checksum.
	testBuf := append([]byte(nil), subset...)
	binary.BigEndian.PutUint32(testBuf[headOff+8:headOff+12], 0)
	expected := uint32(0xB1B0AFBA) - ttfChecksum(testBuf)
	if adjustment != expected {
		t.Errorf("head.checkSumAdjustment: got %08x, want %08x", adjustment, expected)
	}
}

// buildSubsetTestTTF reuses the minimalTTF fixture pattern from the
// public-facing stamp test. It is kept here as a package-private helper
// to avoid a cross-package test dependency and to document the minimum
// table set required by [subsetTTF].
func buildSubsetTestTTF(t *testing.T) []byte {
	t.Helper()

	head := make([]byte, 54)
	binary.BigEndian.PutUint32(head[0:], 0x00010000) // version
	binary.BigEndian.PutUint16(head[18:], 1000)      // unitsPerEm
	binary.BigEndian.PutUint16(head[36:], 0)         // xMin
	binary.BigEndian.PutUint16(head[38:], 0)         // yMin
	binary.BigEndian.PutUint16(head[40:], 1000)      // xMax
	binary.BigEndian.PutUint16(head[42:], 1000)      // yMax
	binary.BigEndian.PutUint16(head[50:], 0)         // indexToLocFormat short

	hhea := make([]byte, 36)
	binary.BigEndian.PutUint16(hhea[4:], 800) // ascent
	binary.BigEndian.PutUint16(hhea[6:], 200) // descent (unsigned form)
	binary.BigEndian.PutUint16(hhea[34:], 2)  // numberOfHMetrics

	maxp := make([]byte, 6)
	binary.BigEndian.PutUint16(maxp[4:], 2) // numGlyphs=2

	hmtx := make([]byte, 8)
	binary.BigEndian.PutUint16(hmtx[0:], 500)
	binary.BigEndian.PutUint16(hmtx[4:], 600)

	// Minimal cmap format 4 with a single U+0041 → GID 1 mapping.
	cmapSub := make([]byte, 32)
	binary.BigEndian.PutUint16(cmapSub[0:], 4)       // format
	binary.BigEndian.PutUint16(cmapSub[2:], 32)      // length
	binary.BigEndian.PutUint16(cmapSub[6:], 4)       // segCountX2 = 2*2
	binary.BigEndian.PutUint16(cmapSub[14:], 0x0041) // endCount[0]
	binary.BigEndian.PutUint16(cmapSub[16:], 0xFFFF) // endCount[1] (sentinel)
	binary.BigEndian.PutUint16(cmapSub[20:], 0x0041) // startCount[0]
	binary.BigEndian.PutUint16(cmapSub[22:], 0xFFFF) // startCount[1]
	// delta stored as 16-bit two's complement: 1 - 0x0041 = -64 → 0xFFC0.
	binary.BigEndian.PutUint16(cmapSub[24:], 0xFFC0)
	binary.BigEndian.PutUint16(cmapSub[26:], 1) // delta[1] sentinel
	binary.BigEndian.PutUint16(cmapSub[28:], 0) // idRangeOffset[0]
	binary.BigEndian.PutUint16(cmapSub[30:], 0) // idRangeOffset[1]

	cmap := make([]byte, 12)
	binary.BigEndian.PutUint16(cmap[0:], 0)
	binary.BigEndian.PutUint16(cmap[2:], 1)
	binary.BigEndian.PutUint16(cmap[4:], 3)
	binary.BigEndian.PutUint16(cmap[6:], 1)
	binary.BigEndian.PutUint32(cmap[8:], 12)
	cmap = append(cmap, cmapSub...)

	post := make([]byte, 32)
	binary.BigEndian.PutUint32(post[0:], 0x00030000) // format 3.0

	name := make([]byte, 6)
	binary.BigEndian.PutUint16(name[4:], 6)

	// loca: three uint16 offsets (short format), two glyphs of 4 bytes
	// each.
	loca := make([]byte, 6)
	binary.BigEndian.PutUint16(loca[0:], 0)
	binary.BigEndian.PutUint16(loca[2:], 2) // 2*2 = 4
	binary.BigEndian.PutUint16(loca[4:], 4) // 4*2 = 8

	// glyf: two 4-byte simple-glyph records (numberOfContours=0 header
	// + 2 bytes padding).
	glyf := make([]byte, 8)

	tables := map[string][]byte{
		"head": head,
		"hhea": hhea,
		"maxp": maxp,
		"hmtx": hmtx,
		"cmap": cmap,
		"post": post,
		"name": name,
		"loca": loca,
		"glyf": glyf,
	}
	return encodeTTF(tables)
}
