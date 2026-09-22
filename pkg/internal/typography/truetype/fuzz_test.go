package truetype

import (
	"context"
	"encoding/binary"
	"testing"
)

// FuzzTrueType feeds arbitrary bytes to the sfnt/TrueType table readers.
// Every entry point reads an embedded, attacker-controlled font program;
// the contract is crash-freedom, not a successful parse.
func FuzzTrueType(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte{0x00, 0x01, 0x00, 0x00, 0x00, 0x00}) // sfnt version + numTables 0
	f.Add(make([]byte, 128))
	// A structurally valid sfnt, so the fuzzer starts inside the table readers
	// instead of bouncing off the header. Without it every mutation has to
	// invent a whole table directory by chance: the target managed one new
	// coverage-expanding input in 19 million executions, against 523 for the
	// CFF target beside it.
	f.Add(minimalSfnt())
	// The seed above declares (3,0) Windows Symbol, which parseCmap's candidate
	// scoring matches no case for and drops, so its format 0 subtable reaches
	// the SYMBOL resolver and never parseCmapFormat0. This one adds a format 12
	// subtable and a post table naming a glyph.
	f.Add(richSfnt())
	// And this one reaches parseCmapFormat6, which neither seed above can:
	// richSfnt pairs its format 6 subtable with a (3,10) record, and (3,10)
	// outranks (1,0) four to one, so format 12 always wins the candidate sort.
	// A LONE (1,0) record has nothing to lose to.
	f.Add(macSfnt())

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		ctx := context.Background()
		records, _ := ParseTableDirectory(data)
		_, _ = TableData(data, records, "cmap")
		_, _ = CmapSubtables(data)
		_, _ = CmapSubtableCount(data)
		// NumGlyphs reads the maxp table straight out of the caller's bytes and
		// is a package entry point no other function here calls, so nothing else
		// in this target reaches it.
		_, _ = NumGlyphs(data)
		_, _ = GlyphWidths(ctx, data)
		// NumberOfHMetrics reads hhea out of the caller's bytes. Like NumGlyphs
		// above it is a package entry point no other function in this package
		// calls, so nothing else in this target reaches it.
		_, _ = NumberOfHMetrics(data)
		_, _ = EmptyGIDs(ctx, data)
		_, _ = CharCodeToGID(ctx, data)
		_, _ = GlyphToUnicode(ctx, data)
		_, _ = PostGlyphNames(ctx, data)
		_, _ = SymbolCharCodeToGID(ctx, data)
	})
}

// minimalSfnt builds the smallest font the table readers accept: an offset
// table naming a maxp and a cmap, a version 0.5 maxp declaring one glyph, and a
// format 0 cmap subtable mapping every code to glyph 0.
func minimalSfnt() []byte {
	maxp := make([]byte, 6)
	binary.BigEndian.PutUint32(maxp[0:], 0x00005000) // version 0.5
	binary.BigEndian.PutUint16(maxp[4:], 1)          // numGlyphs

	// cmap: header, one encoding record, one format 0 subtable.
	const subtableOff = 4 + 8
	cmap := make([]byte, subtableOff+262)
	binary.BigEndian.PutUint16(cmap[0:], 0) // version
	binary.BigEndian.PutUint16(cmap[2:], 1) // numTables
	binary.BigEndian.PutUint16(cmap[4:], 3) // platformID: Windows
	binary.BigEndian.PutUint16(cmap[6:], 0) // encodingID: symbol
	binary.BigEndian.PutUint32(cmap[8:], subtableOff)
	binary.BigEndian.PutUint16(cmap[subtableOff+0:], 0)   // format 0
	binary.BigEndian.PutUint16(cmap[subtableOff+2:], 262) // length
	binary.BigEndian.PutUint16(cmap[subtableOff+4:], 0)   // language

	tables := []struct {
		tag  string
		data []byte
	}{
		{"cmap", cmap},
		{"maxp", maxp},
	}

	const dirEntry = 16
	head := 12 + dirEntry*len(tables)
	out := make([]byte, head)
	binary.BigEndian.PutUint32(out[0:], 0x00010000)             // sfnt version
	binary.BigEndian.PutUint16(out[4:], uint16(len(tables)))    // numTables
	binary.BigEndian.PutUint16(out[6:], uint16(16*len(tables))) // searchRange
	binary.BigEndian.PutUint16(out[8:], 1)                      // entrySelector
	binary.BigEndian.PutUint16(out[10:], 0)                     // rangeShift

	for i, t := range tables {
		rec := 12 + i*dirEntry
		copy(out[rec:], t.tag)
		binary.BigEndian.PutUint32(out[rec+8:], uint32(len(out)))
		binary.BigEndian.PutUint32(out[rec+12:], uint32(len(t.data)))
		out = append(out, t.data...)
	}
	return out
}

// TestMinimalSfntIsAccepted pins that the seed [FuzzTrueType] starts from is a
// font the readers actually parse.
//
// The seed exists to put the fuzzer inside the table readers; if it were
// subtly malformed it would be rejected at the header like the other seeds and
// the target would stay inert while looking well seeded. Assert the readers
// find the tables it declares rather than inferring it from a coverage count.
func TestMinimalSfntIsAccepted(t *testing.T) {
	t.Parallel()

	data := minimalSfnt()

	records, err := ParseTableDirectory(data)
	if err != nil {
		t.Fatalf("ParseTableDirectory: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("parsed %d table records, want 2", len(records))
	}
	for _, tag := range []string{"cmap", "maxp"} {
		if _, err = TableData(data, records, tag); err != nil {
			t.Errorf("table %q: %v", tag, err)
		}
	}

	n, err := NumGlyphs(data)
	if err != nil {
		t.Fatalf("NumGlyphs: %v", err)
	}
	if n != 1 {
		t.Errorf("NumGlyphs = %d, want 1", n)
	}

	count, err := CmapSubtableCount(data)
	if err != nil {
		t.Fatalf("CmapSubtableCount: %v", err)
	}
	if count != 1 {
		t.Errorf("CmapSubtableCount = %d, want 1", count)
	}
}

// richSfnt builds a font carrying a format 6 trimmed mapping, a format 12
// grouped mapping, and a version 2.0 post table naming its glyphs.
//
// It reaches the format 12 reader and the post reader. It does NOT reach the
// format 6 reader, measured: parseCmap scores its candidates and takes only the
// best one, and this font's (3,10) record scores 4 against the (1,0) record's
// 1, so the format 6 subtable is present but permanently shadowed. [macSfnt]
// is the seed that reaches it. An earlier comment here claimed both readers
// were reached; TestRichSfntReachesTheOtherFormats asserted the subtable COUNT
// rather than which reader ran, so the claim stood untested.
func richSfnt() []byte {
	maxp := make([]byte, 6)
	binary.BigEndian.PutUint32(maxp[0:], 0x00005000)
	binary.BigEndian.PutUint16(maxp[4:], 2) // numGlyphs

	// Format 6: firstCode 65, one entry mapping to glyph 1.
	f6 := make([]byte, 12)
	binary.BigEndian.PutUint16(f6[0:], 6)
	binary.BigEndian.PutUint16(f6[2:], uint16(len(f6)))
	binary.BigEndian.PutUint16(f6[6:], 65) // firstCode
	binary.BigEndian.PutUint16(f6[8:], 1)  // entryCount
	binary.BigEndian.PutUint16(f6[10:], 1) // glyphIdArray[0]

	// Format 12: one group mapping U+0041 to glyph 1.
	f12 := make([]byte, 16+12)
	binary.BigEndian.PutUint16(f12[0:], 12)
	binary.BigEndian.PutUint32(f12[4:], uint32(len(f12))) // length
	binary.BigEndian.PutUint32(f12[12:], 1)               // numGroups
	binary.BigEndian.PutUint32(f12[16:], 0x41)            // startCharCode
	binary.BigEndian.PutUint32(f12[20:], 0x41)            // endCharCode
	binary.BigEndian.PutUint32(f12[24:], 1)               // startGlyphID

	// cmap header with two encoding records pointing at the subtables.
	const recs = 2
	head := 4 + recs*8
	cmap := make([]byte, head)
	binary.BigEndian.PutUint16(cmap[2:], recs)
	binary.BigEndian.PutUint16(cmap[4:], 1) // platformID: Macintosh
	binary.BigEndian.PutUint16(cmap[6:], 0)
	binary.BigEndian.PutUint32(cmap[8:], uint32(head))
	binary.BigEndian.PutUint16(cmap[12:], 3) // platformID: Windows
	binary.BigEndian.PutUint16(cmap[14:], 10)
	binary.BigEndian.PutUint32(cmap[16:], uint32(head+len(f6)))
	cmap = append(cmap, f6...)
	cmap = append(cmap, f12...)

	// post version 2.0: header, glyph count, name indices, then Pascal names.
	post := make([]byte, 34)
	binary.BigEndian.PutUint32(post[0:], 0x00020000)
	binary.BigEndian.PutUint16(post[32:], 2) // numGlyphs
	post = append(post, 0x00, 0x00)          // glyph 0 -> standard name 0 (.notdef)
	// Indices below 258 name a standard Macintosh glyph; 258 is the first
	// custom name, read from the Pascal strings that follow the index array.
	post = append(post, 0x01, 0x02) // glyph 1 -> custom name 0
	post = append(post, byte(len("Alpha")))
	post = append(post, []byte("Alpha")...)

	tables := []struct {
		tag  string
		data []byte
	}{
		{"cmap", cmap}, {"maxp", maxp}, {"post", post},
	}

	const dirEntry = 16
	out := make([]byte, 12+dirEntry*len(tables))
	binary.BigEndian.PutUint32(out[0:], 0x00010000)
	binary.BigEndian.PutUint16(out[4:], uint16(len(tables)))
	for i, t := range tables {
		rec := 12 + i*dirEntry
		copy(out[rec:], t.tag)
		binary.BigEndian.PutUint32(out[rec+8:], uint32(len(out)))
		binary.BigEndian.PutUint32(out[rec+12:], uint32(len(t.data)))
		out = append(out, t.data...)
	}
	return out
}

// TestRichSfntReachesTheOtherFormats pins that the second seed exercises what
// the first cannot: two cmap subtables and a post table that names a glyph.
func TestRichSfntReachesTheOtherFormats(t *testing.T) {
	t.Parallel()

	data := richSfnt()

	count, err := CmapSubtableCount(data)
	if err != nil {
		t.Fatalf("CmapSubtableCount: %v", err)
	}
	if count != 2 {
		t.Errorf("CmapSubtableCount = %d, want 2", count)
	}

	names, err := PostGlyphNames(context.Background(), data)
	if err != nil {
		t.Fatalf("PostGlyphNames: %v", err)
	}
	if names[1] != "Alpha" {
		t.Errorf("post name for glyph 1 = %q, want %q", names[1], "Alpha")
	}
}

// macSfnt builds a font whose ONLY cmap record is (1,0) Macintosh Roman,
// carrying a format 6 trimmed subtable.
//
// parseCmap keeps a single best candidate: (3,10) scores 4, (3,1) scores 3,
// platform 0 scores 2, (1,0) scores 1, and anything else is dropped. Every
// other seed and test in this package declares (3,1), (3,10) or (3,0), so the
// format 6 reader was reached by nothing at all. A lone (1,0) record has no
// higher-priority competitor, which is what makes it the way in.
func macSfnt() []byte {
	maxp := make([]byte, 6)
	binary.BigEndian.PutUint32(maxp[0:], 0x00005000) // version 0.5
	binary.BigEndian.PutUint16(maxp[4:], 3)          // numGlyphs

	return buildTableDirectory(map[string][]byte{
		"cmap": buildCmapTable(1, 0, buildCmapFormat6(65, []uint16{1, 2})),
		"maxp": maxp,
	})
}

// TestMacSfntReachesTheFormat6Reader asserts REACHABILITY, not shape.
//
// Its sibling TestRichSfntReachesTheOtherFormats checks CmapSubtableCount,
// which says a subtable EXISTS and nothing about which reader consumed it:
// that is why a seed advertised as reaching the format 6 reader sat at 0.0%
// coverage while its test passed. The mapping asserted below can only be
// produced by parseCmapFormat6 actually running.
func TestMacSfntReachesTheFormat6Reader(t *testing.T) {
	t.Parallel()

	gidMap, err := GlyphToUnicode(context.Background(), macSfnt())
	if err != nil {
		t.Fatalf("GlyphToUnicode: %v", err)
	}
	for gid, want := range map[uint16]rune{1: 'A', 2: 'B'} {
		if got, ok := gidMap[gid]; !ok || got != want {
			t.Errorf("GID %d = %q (present %t), want %q", gid, got, ok, want)
		}
	}
}
