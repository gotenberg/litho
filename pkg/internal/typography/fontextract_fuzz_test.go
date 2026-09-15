package typography

import (
	"context"
	"encoding/binary"
	"testing"
)

// FuzzExtractFontProgram feeds arbitrary font-program bytes to every Extract
// entry point and asserts crash-freedom.
//
// These readers take the embedded font program straight from the document's
// /FontFile, /FontFile2 or /FontFile3 stream, so their leading argument is
// entirely attacker-controlled, and the font rules call them on every page that
// declares the font. Between them they walk the sfnt table directory, the cmap
// in four subtable formats, hmtx, post, the CFF INDEX structures and the Type 1
// eexec section: all length-driven parsing of untrusted bytes, which is what the
// fuzzing rule exists for.
//
// [FuzzReadUnitsPerEm] beside this covers the head table alone; measured against
// the fuzz corpora, it was the only one of the 64 functions in font.go that any
// target reached.
func FuzzExtractFontProgram(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte("\x00\x01\x00\x00"))     // sfnt version alone
	f.Add([]byte("OTTO\x00\x00\x00\x00")) // CFF-flavoured, truncated
	f.Add([]byte("ttcf\x00\x01\x00\x00")) // collection header
	f.Add(make([]byte, 128))
	// A Type 1 program: the eexec readers need the clear-text header to find
	// their section, and no mutation of the seeds above would produce one.
	f.Add([]byte("%!PS-AdobeFont-1.0: Test\n/FontMatrix [0.001 0 0 0.001 0 0] readonly def\n" +
		"/Encoding 256 array\ndup 65 /A put\nreadonly def\ncurrentfile eexec\n"))
	// A structurally valid sfnt, so the table readers are entered rather than
	// rejected at the header.
	f.Add(sfntForExtract())

	f.Fuzz(func(_ *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		ctx := context.Background()

		_ = ExtractBinaryWidths(ctx, data)
		_ = ExtractCIDBinaryWidths(ctx, data)
		_ = ExtractCharCodeToGID(ctx, data)
		_ = ExtractSymbolCharCodeToGID(ctx, data)
		_ = ExtractGlyphNames(ctx, data)
		_ = ExtractEmptyGIDs(ctx, data)
		_ = ExtractNumGlyphs(data)
		_ = ExtractValidCIDs(data)
		_ = ExtractCFFEncodingNames(data)
		_ = ExtractType1AdvanceWidths(data)
		_, _ = ExtractType1FontMatrixScale(data)
		_ = ExtractType1BuiltinEncoding(data)
		_ = ExtractType1EncodingNames(data)
		_ = ExtractGIDToUnicode(ctx, data)
		_ = ExtractGIDToUnicodeAuthoritative(ctx, data)
	})
}

// sfntForExtract builds a small but structurally valid sfnt: a head for the
// scale, a maxp declaring the glyph count, an hhea/hmtx pair for the widths and
// a format 4 cmap for the code-to-glyph maps. Table records are sorted by tag,
// which cmap/head/hhea/hmtx/maxp already are.
func sfntForExtract() []byte {
	head := make([]byte, 54)
	binary.BigEndian.PutUint16(head[18:], 1000) // unitsPerEm
	binary.BigEndian.PutUint16(head[50:], 0)    // indexToLocFormat

	hhea := make([]byte, 36)
	binary.BigEndian.PutUint16(hhea[34:], 2) // numberOfHMetrics

	hmtx := make([]byte, 8)
	binary.BigEndian.PutUint16(hmtx[0:], 500)
	binary.BigEndian.PutUint16(hmtx[4:], 600)

	maxp := make([]byte, 6)
	binary.BigEndian.PutUint32(maxp[0:], 0x00005000) // version 0.5
	binary.BigEndian.PutUint16(maxp[4:], 2)          // numGlyphs

	// A format 4 cmap with one segment mapping 'A' to glyph 1, plus the
	// mandatory 0xFFFF terminator segment.
	const segs = 2
	sub := make([]byte, 16+segs*8)
	binary.BigEndian.PutUint16(sub[0:], 4)                // format
	binary.BigEndian.PutUint16(sub[2:], uint16(len(sub))) // length
	binary.BigEndian.PutUint16(sub[6:], segs*2)           // segCountX2
	endAt, startAt := 14, 14+segs*2+2
	deltaAt, rangeAt := startAt+segs*2, startAt+segs*4
	binary.BigEndian.PutUint16(sub[endAt:], 'A')
	binary.BigEndian.PutUint16(sub[endAt+2:], 0xFFFF)
	binary.BigEndian.PutUint16(sub[startAt:], 'A')
	binary.BigEndian.PutUint16(sub[startAt+2:], 0xFFFF)
	// idDelta is added to the code modulo 65536, so mapping 'A' (0x41) to glyph
	// 1 means 0x41 + 0xFFC0 = 0x10001, which truncates to 1.
	binary.BigEndian.PutUint16(sub[deltaAt:], 0xFFC0)
	binary.BigEndian.PutUint16(sub[deltaAt+2:], 1)
	binary.BigEndian.PutUint16(sub[rangeAt:], 0)
	binary.BigEndian.PutUint16(sub[rangeAt+2:], 0)

	const subtableOff = 4 + 8
	cmap := make([]byte, subtableOff, subtableOff+len(sub))
	binary.BigEndian.PutUint16(cmap[2:], 1) // numTables
	binary.BigEndian.PutUint16(cmap[4:], 3) // platformID: Windows
	binary.BigEndian.PutUint16(cmap[6:], 1) // encodingID: Unicode BMP
	binary.BigEndian.PutUint32(cmap[8:], subtableOff)
	cmap = append(cmap, sub...)

	tables := []struct {
		tag  string
		data []byte
	}{
		{"cmap", cmap}, {"head", head}, {"hhea", hhea}, {"hmtx", hmtx}, {"maxp", maxp},
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

// TestSfntForExtractIsRead pins that the seed reaches the table readers instead
// of being rejected at the header, which is the whole reason it exists.
func TestSfntForExtractIsRead(t *testing.T) {
	t.Parallel()

	data := sfntForExtract()
	ctx := context.Background()

	if n := ExtractNumGlyphs(data); n != 2 {
		t.Errorf("ExtractNumGlyphs = %d, want 2", n)
	}
	if got := ReadUnitsPerEm(data); got != 1000 {
		t.Errorf("ReadUnitsPerEm = %d, want 1000", got)
	}
	widths := ExtractBinaryWidths(ctx, data)
	if len(widths) == 0 {
		t.Error("ExtractBinaryWidths found no hmtx advances")
	}
	if gid, ok := ExtractCharCodeToGID(ctx, data)['A']; !ok || gid != 1 {
		t.Errorf("ExtractCharCodeToGID['A'] = %d, %v; want 1, true", gid, ok)
	}
}
