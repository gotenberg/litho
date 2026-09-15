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
