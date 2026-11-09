package build

// Port of truetype.go.
//
// Minimal TrueType font programs with a caller-chosen 'cmap' subtable set.
//
// ISO 19005-2, 6.2.11.6 tests 1 and 4 are scoped to `TrueTypeFontProgram`, so
// reaching them needs a real embedded program whose 'cmap' table carries
// specific (platformID, encodingID) subtables. No public font offers that on
// demand, and the corpus builders take no dependency beyond pikepdf, so this
// module emits the programs directly.
//
// Each font carries three glyphs, `.notdef` plus `A` and `B`, drawn as plain
// rectangles. That is enough for a viewer to render the fixture and for a
// validator to resolve every code the fixture shows, which keeps the glyph
// presence and width clauses quiet so the 'cmap' verdict stands alone.
//
// Only the tables a conforming consumer needs to read a glyph are emitted:
// head, hhea, maxp, hmtx, cmap, loca, glyf, name and post. Per ISO 32000-1,
// 9.9.2 a font program embedded in a PDF need not be independently
// installable, so the optional tables are left out.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"slices"
	"sort"
	"unicode/utf16"
)

// ttUnitsPerEm mirrors UNITS_PER_EM.
const ttUnitsPerEm = 1000

// ttAdvanceWidth mirrors ADVANCE_WIDTH.
const ttAdvanceWidth = 600

// ttGlyphNames mirrors GLYPH_NAMES. Treat it as read-only.
var ttGlyphNames = []string{".notdef", "A", "B"}

// ttSubtable is one (platformID, encodingID, {charCode: glyphName}) tuple of
// build's subtables argument.
type ttSubtable struct {
	PlatformID int
	EncodingID int
	Map        map[int]string
}

// ttResolvedSubtable is a subtable whose map gives glyph ids, the shape _cmap
// takes.
type ttResolvedSubtable struct {
	PlatformID int
	EncodingID int
	Map        map[int]int
}

// ttU16 appends v as struct's ">H" or ">h": its low 16 bits, big-endian.
func ttU16(b []byte, v int) []byte {
	return binary.BigEndian.AppendUint16(b, uint16(v)) //nolint:gosec // struct packing keeps the field width; fixture values fit.
}

// ttU32 appends v as struct's ">I" or ">i": its low 32 bits, big-endian.
func ttU32(b []byte, v int) []byte {
	return binary.BigEndian.AppendUint32(b, uint32(v)) //nolint:gosec // struct packing keeps the field width; fixture values fit.
}

// ttPad4 returns data padded with zeros to a multiple of four bytes.
func ttPad4(data []byte) []byte {
	out := slices.Clone(data)
	for len(out)%4 != 0 {
		out = append(out, 0)
	}
	return out
}

// ttTag4 mirrors struct's "4s": the tag truncated or zero-padded to 4 bytes.
func ttTag4(b []byte, tag string) []byte {
	t := []byte(tag)
	if len(t) > 4 {
		t = t[:4]
	}
	b = append(b, t...)
	for i := len(t); i < 4; i++ {
		b = append(b, 0)
	}
	return b
}

// ttGlyfRect is _glyf_rect: one simple glyph, a single closed contour around a
// rectangle.
func ttGlyfRect(height int) []byte {
	xs := []int{50, 550, 550, 50}
	ys := []int{0, 0, height, height}
	var out []byte
	out = ttU16(out, 1)
	out = ttU16(out, slices.Min(xs))
	out = ttU16(out, slices.Min(ys))
	out = ttU16(out, slices.Max(xs))
	out = ttU16(out, slices.Max(ys))
	out = ttU16(out, len(xs)-1) // endPtsOfContours
	out = ttU16(out, 0)         // no instructions
	for range xs {
		out = append(out, 0x01) // every point on-curve, 16-bit deltas
	}
	prev := 0
	for _, x := range xs {
		out = ttU16(out, x-prev)
		prev = x
	}
	prev = 0
	for _, y := range ys {
		out = ttU16(out, y-prev)
		prev = y
	}
	return ttPad4(out)
}

// ttCmapFormat4 is _cmap_format4: a format 4 subtable covering mapping, one
// segment per code.
func ttCmapFormat4(mapping map[int]int) []byte {
	codes := make([]int, 0, len(mapping))
	for c := range mapping {
		codes = append(codes, c)
	}
	sort.Ints(codes)
	type segment struct{ start, end, glyph int }
	segments := make([]segment, 0, len(codes)+1)
	for _, c := range codes {
		segments = append(segments, segment{c, c, mapping[c]})
	}
	segments = append(segments, segment{0xFFFF, 0xFFFF, 0}) // the required terminating segment
	segCount := len(segments)

	searchRange := 2
	entrySelector := 0
	for searchRange*2 <= segCount*2 {
		searchRange *= 2
		entrySelector++
	}

	endCodes := make([]byte, 0, 2*segCount)
	startCodes := make([]byte, 0, 2*segCount)
	deltas := make([]byte, 0, 2*segCount)
	rangeOffsets := make([]byte, 0, 2*segCount)
	for _, s := range segments {
		endCodes = ttU16(endCodes, s.end)
		startCodes = ttU16(startCodes, s.start)
		// idDelta maps the code straight onto the glyph id. It is a signed
		// field that consumers add modulo 65536, so it is emitted unsigned.
		// The final segment maps 0xFFFF to glyph 0, which a delta of 1
		// achieves by wrapping.
		if s.glyph != 0 {
			deltas = ttU16(deltas, (s.glyph-s.start)&0xFFFF)
		} else {
			deltas = ttU16(deltas, 1)
		}
		rangeOffsets = append(rangeOffsets, 0, 0)
	}

	var body []byte
	body = ttU16(body, segCount*2)
	body = ttU16(body, searchRange)
	body = ttU16(body, entrySelector)
	body = ttU16(body, segCount*2-searchRange)
	body = append(body, endCodes...)
	body = append(body, 0, 0)
	body = append(body, startCodes...)
	body = append(body, deltas...)
	body = append(body, rangeOffsets...)

	var out []byte
	out = ttU16(out, 4)
	out = ttU16(out, len(body)+6)
	out = ttU16(out, 0)
	return append(out, body...)
}

// ttCmap is _cmap: the 'cmap' table. subtables is a list of (platformID,
// encodingID, map).
func ttCmap(subtables []ttResolvedSubtable) []byte {
	encoded := make([][]byte, len(subtables))
	for i, s := range subtables {
		encoded[i] = ttCmapFormat4(s.Map)
	}
	var header []byte
	header = ttU16(header, 0)
	header = ttU16(header, len(subtables))
	offset := 4 + 8*len(subtables)
	var records []byte
	for i, s := range subtables {
		records = ttU16(records, s.PlatformID)
		records = ttU16(records, s.EncodingID)
		records = ttU32(records, offset)
		offset += len(encoded[i])
	}
	return slices.Concat(append([][]byte{header, records}, encoded...)...)
}

// ttName is _name: a 'name' table with the four records a consumer is likely
// to read.
func ttName() []byte {
	records := []struct {
		id    int
		value string
	}{{1, "LithoCmapProbe"}, {2, "Regular"}, {4, "LithoCmapProbe"}, {6, "LithoCmapProbe"}}
	storage := make([]byte, 0, 64)
	entries := make([]byte, 0, 12*len(records))
	for _, r := range records {
		var encoded []byte
		for _, u := range utf16.Encode([]rune(r.value)) {
			encoded = ttU16(encoded, int(u))
		}
		for _, v := range []int{3, 1, 0x0409, r.id, len(encoded), len(storage)} {
			entries = ttU16(entries, v)
		}
		storage = append(storage, encoded...)
	}
	var out []byte
	out = ttU16(out, 0)
	out = ttU16(out, len(records))
	out = ttU16(out, 6+12*len(records))
	out = append(out, entries...)
	return append(out, storage...)
}

// ttPost is _post: a 'post' table in format 2.0, which carries the glyph names.
//
// Format 3.0 would be shorter but stores no names, and a non-symbolic
// TrueType resolves a code through its glyph name when the 'cmap' offers no
// usable subtable (ISO 32000-1, 9.6.6.4). Without the names those lookups
// fail and the glyph presence clause fires, which would mask the 'cmap'
// verdict this module exists to isolate.
func ttPost(glyphNames []string) []byte {
	var header []byte
	header = ttU32(header, 0x00020000)
	header = ttU32(header, 0)
	header = ttU16(header, 0)
	header = ttU16(header, 0)
	for range 5 {
		header = ttU32(header, 0)
	}
	// Index 0 is the standard Macintosh name `.notdef`; the rest are appended
	// as Pascal strings, which the format numbers from 258 upwards.
	indices := make([]int, 1, len(glyphNames))
	names := make([]byte, 0, 8*len(glyphNames))
	for position, name := range glyphNames[1:] {
		indices = append(indices, 258+position)
		names = append(names, byte(len(name))) //nolint:gosec // Pascal string length byte, as Python's bytes([len(name)]); glyph names are short.
		names = append(names, ttASCII(name)...)
	}
	body := ttU16(nil, len(glyphNames))
	for _, i := range indices {
		body = ttU16(body, i)
	}
	return slices.Concat(header, body, names)
}

// ttASCII mirrors str.encode("ascii"), which raises on anything else.
func ttASCII(s string) []byte {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			panic(fmt.Sprintf("truetype: %q is not ASCII", s))
		}
	}
	return []byte(s)
}

// ttChecksum is _checksum: the sfnt table checksum, the sum of the
// zero-padded data's big-endian 32-bit words modulo 2^32.
func ttChecksum(data []byte) uint32 {
	padded := ttPad4(data)
	var sum uint32
	for i := 0; i < len(padded); i += 4 {
		sum += binary.BigEndian.Uint32(padded[i:])
	}
	return sum
}

// ttBuild is build: return the bytes of a TrueType program carrying exactly
// subtables.
//
// subtables is a list of (platformID, encodingID, {charCode: glyphName}).
// Passing several lets a fixture choose the count and the platform pairs the
// 6.2.11.6 tests turn on. glyphNames renames the three glyphs in the 'post'
// table, `.notdef` first; the outlines stay the same. A nil glyphNames means
// ttGlyphNames, Python's default.
func ttBuild(subtables []ttSubtable, glyphNames []string) []byte {
	if glyphNames == nil {
		glyphNames = ttGlyphNames
	}
	if len(glyphNames) != len(ttGlyphNames) || glyphNames[0] != ".notdef" {
		panic("truetype: glyph_names must hold three names, .notdef first")
	}
	resolved := make([]ttResolvedSubtable, len(subtables))
	for i, s := range subtables {
		m := make(map[int]int, len(s.Map))
		for code, name := range s.Map {
			gid := slices.Index(glyphNames, name)
			if gid < 0 {
				panic(fmt.Sprintf("truetype: %q is not in glyph_names", name))
			}
			m[code] = gid
		}
		resolved[i] = ttResolvedSubtable{s.PlatformID, s.EncodingID, m}
	}

	glyphs := [][]byte{{}, ttGlyfRect(700), ttGlyfRect(500)}
	var glyf, loca []byte
	running := 0
	for _, g := range glyphs {
		loca = ttU32(loca, running) // long format
		running += len(g)
		glyf = append(glyf, g...)
	}
	loca = ttU32(loca, running)

	var head []byte
	head = ttU32(head, 0x00010000) // version
	head = ttU32(head, 0x00010000) // fontRevision
	head = ttU32(head, 0)          // checkSumAdjustment, patched below
	head = ttU32(head, 0x5F0F3CF5) // magicNumber
	head = ttU16(head, 0b1011)     // flags
	head = ttU16(head, ttUnitsPerEm)
	head = binary.BigEndian.AppendUint64(head, 0) // created
	head = binary.BigEndian.AppendUint64(head, 0) // modified
	for _, v := range []int{0, -200, 550, 700} {  // bounding box
		head = ttU16(head, v)
	}
	head = ttU16(head, 0) // macStyle
	head = ttU16(head, 8) // lowestRecPPEM
	head = ttU16(head, 2) // fontDirectionHint
	head = ttU16(head, 1) // indexToLocFormat: long
	head = ttU16(head, 0) // glyphDataFormat

	hhea := ttU32(nil, 0x00010000)
	for _, v := range []int{800, -200, 0, ttAdvanceWidth, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0, len(ttGlyphNames)} {
		hhea = ttU16(hhea, v)
	}
	maxp := ttU32(nil, 0x00010000)
	for _, v := range []int{len(ttGlyphNames), 4, 1, 0, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0} {
		maxp = ttU16(maxp, v)
	}
	var hmtx []byte
	for range ttGlyphNames {
		hmtx = ttU16(hmtx, ttAdvanceWidth)
		hmtx = ttU16(hmtx, 50)
	}
	post := ttPost(glyphNames)

	tables := map[string][]byte{
		"cmap": ttCmap(resolved),
		"glyf": glyf,
		"head": head,
		"hhea": hhea,
		"hmtx": hmtx,
		"loca": loca,
		"maxp": maxp,
		"name": ttName(),
		"post": post,
	}

	tags := ttSortedTags(tables)
	num := len(tags)
	searchRange := 16
	entrySelector := 0
	for searchRange*2 <= num*16 {
		searchRange *= 2
		entrySelector++
	}

	directorySize := 12 + 16*num
	offset := directorySize
	var records, body []byte
	headOffset := -1
	for _, tag := range tags {
		data := tables[tag]
		if tag == "head" {
			headOffset = offset
		}
		records = ttTag4(records, tag)
		records = binary.BigEndian.AppendUint32(records, ttChecksum(data))
		records = ttU32(records, offset)
		records = ttU32(records, len(data))
		padded := ttPad4(data)
		body = append(body, padded...)
		offset += len(padded)
	}

	var font []byte
	font = ttU32(font, 0x00010000)
	for _, v := range []int{num, searchRange, entrySelector, num*16 - searchRange} {
		font = ttU16(font, v)
	}
	font = append(font, records...)
	font = append(font, body...)

	// head.checkSumAdjustment is 0xB1B0AFBA less the checksum of the whole file.
	adjustment := uint32(0xB1B0AFBA) - ttChecksum(font)
	binary.BigEndian.PutUint32(font[headOffset+8:], adjustment)
	return font
}

// ttSortedTags returns the keys of tables in byte order, as Python's sorted
// does on bytes keys.
func ttSortedTags(tables map[string][]byte) []string {
	tags := make([]string, 0, len(tables))
	for t := range tables {
		tags = append(tags, t)
	}
	sort.Strings(tags)
	return tags
}

// ttTablesOf is tables_of: split an sfnt into {tag: bytes}, the inverse of
// assemble. Each value is a copy, as Python's bytes slices are.
func ttTablesOf(font []byte) map[string][]byte {
	num := int(binary.BigEndian.Uint16(font[4:6]))
	out := make(map[string][]byte, num)
	for i := range num {
		rec := font[12+16*i : 28+16*i]
		tag := string(rec[0:4])
		off := int(binary.BigEndian.Uint32(rec[8:12]))     //nolint:gosec // fixture fonts are small.
		length := int(binary.BigEndian.Uint32(rec[12:16])) //nolint:gosec // fixture fonts are small.
		// Python slicing clamps to the end of the data.
		start, end := min(off, len(font)), min(off+length, len(font))
		out[tag] = bytes.Clone(font[start:end])
		if out[tag] == nil {
			out[tag] = []byte{}
		}
	}
	return out
}

// ttAssemble is assemble: lay {tag: bytes} out as an sfnt under the 4-byte
// version given.
//
// The directory search fields are left at zero and no checkSumAdjustment is
// patched: a fixture assembled here plants a structural defect, and no
// consumer the corpus measures reads either value.
func ttAssemble(version []byte, tables map[string][]byte) []byte {
	tags := ttSortedTags(tables)
	offset := 12 + 16*len(tags)
	var records, body []byte
	for _, tag := range tags {
		data := tables[tag]
		records = ttTag4(records, tag)
		records = binary.BigEndian.AppendUint32(records, ttChecksum(data))
		records = ttU32(records, offset)
		records = ttU32(records, len(data))
		padded := ttPad4(data)
		body = append(body, padded...)
		offset += len(padded)
	}
	out := slices.Clone(version)
	for _, v := range []int{len(tags), 16, 0, 0} {
		out = ttU16(out, v)
	}
	out = append(out, records...)
	return append(out, body...)
}
