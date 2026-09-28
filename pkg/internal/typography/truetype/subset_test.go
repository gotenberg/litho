package truetype

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// subsetSource is a program of nine glyphs: .notdef, space, A, C, B (so the
// codes A to C reach glyphs out of order), acute, the composite Aacute drawing
// A and acute, and two symbol glyphs.
func subsetSource() []byte {
	return fontbuild.TrueType{
		PostScriptName: "TestSans",
		Family:         "Test Sans",
		Subfamily:      "Regular",
		UnitsPerEm:     2048,
		Glyphs: []fontbuild.Glyph{
			{Name: "space", Unicode: ' ', Advance: 569, Empty: true},
			{Name: "A", Unicode: 'A', Advance: 1366},
			{Name: "C", Unicode: 'C', Advance: 1479},
			{Name: "B", Unicode: 'B', Advance: 1400},
			{Name: "acute", Unicode: 0xB4, Advance: 682},
			{Name: "Aacute", Unicode: 0xC1, Advance: 1366, Components: []uint16{2, 5}},
			{Name: "bullet", Symbol: 0xF0B7, Advance: 717},
			{Name: "uniF0A7", Symbol: 0xF0A7, Advance: 900},
		},
		NotdefAdvance: 1536,
	}.Build()
}

func TestSubset_KeepsTheRequestedGlyphs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := subsetSource()
	out, newGID, err := Subset(ctx, src, []uint16{4, 2}) // B and A
	if err != nil {
		t.Fatalf("Subset: %v", err)
	}
	if len(out) >= len(src) {
		t.Errorf("subset of %d bytes, source %d", len(out), len(src))
	}
	want := map[uint16]uint16{0: 0, 2: 1, 4: 2}
	if len(newGID) != len(want) {
		t.Fatalf("newGID = %v, want %v", newGID, want)
	}
	for o, n := range want {
		if newGID[o] != n {
			t.Errorf("newGID[%d] = %d, want %d", o, newGID[o], n)
		}
	}
	if n, err := NumGlyphs(out); err != nil || n != 3 {
		t.Errorf("NumGlyphs = %d, %v, want 3", n, err)
	}
	widths, err := GlyphWidths(ctx, out)
	if err != nil {
		t.Fatalf("GlyphWidths: %v", err)
	}
	if widths[0] != 1536 || widths[1] != 1366 || widths[2] != 1400 {
		t.Errorf("widths %v", widths)
	}
	codes, err := CharCodeToGID(ctx, out)
	if err != nil {
		t.Fatalf("CharCodeToGID: %v", err)
	}
	if codes['A'] != 1 || codes['B'] != 2 || len(codes) != 2 {
		t.Errorf("cmap %v, want A and B alone", codes)
	}
	names, err := PostGlyphNames(ctx, out)
	if err != nil || names[1] != "A" || names[2] != "B" || names[0] != ".notdef" {
		t.Errorf("post names %v, %v", names, err)
	}
	info, err := ReadInfo(out)
	if err != nil || info.PostScriptName != "TestSans" || info.UnitsPerEm != 2048 || info.Outline != OutlineTrueType {
		t.Errorf("ReadInfo = %+v, %v", info, err)
	}
	if sum := tableChecksum(out); sum != checkSumMagic {
		t.Errorf("font checksum %#x, want %#x", sum, uint32(checkSumMagic))
	}
	records, err := ParseTableDirectory(out)
	if err != nil {
		t.Fatalf("ParseTableDirectory: %v", err)
	}
	for _, r := range records {
		data, err := TableData(out, records, r.Tag)
		if err != nil {
			t.Fatalf("table %q: %v", r.Tag, err)
		}
		dirSum := binary.BigEndian.Uint32(out[12+16*indexOfTag(records, r.Tag)+4:])
		if r.Tag != "head" && tableChecksum(data) != dirSum {
			t.Errorf("table %q checksum %#x, directory says %#x", r.Tag, tableChecksum(data), dirSum)
		}
	}
}

func indexOfTag(records []TableRecord, tag string) int {
	for i, r := range records {
		if r.Tag == tag {
			return i
		}
	}
	return -1
}

func TestSubset_KeepsCompositeComponents(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	out, newGID, err := Subset(ctx, subsetSource(), []uint16{6}) // Aacute
	if err != nil {
		t.Fatalf("Subset: %v", err)
	}
	// Aacute draws A (2) and acute (5), which the subset keeps.
	for _, gid := range []uint16{2, 5, 6} {
		if _, ok := newGID[gid]; !ok {
			t.Errorf("glyph %d dropped: %v", gid, newGID)
		}
	}
	records, err := ParseTableDirectory(out)
	if err != nil {
		t.Fatalf("ParseTableDirectory: %v", err)
	}
	head, _ := TableData(out, records, "head")
	loca, _ := TableData(out, records, "loca")
	glyf, _ := TableData(out, records, "glyf")
	offsets, err := glyphOffsets(loca, len(glyf), 4, binary.BigEndian.Uint16(head[50:52]) != 0)
	if err != nil {
		t.Fatalf("glyphOffsets: %v", err)
	}
	composite := glyf[offsets[newGID[6]]:offsets[newGID[6]+1]]
	var components []uint16
	if err := compositeComponents(composite, func(_ int, gid uint16) error {
		components = append(components, gid)
		return nil
	}); err != nil {
		t.Fatalf("compositeComponents: %v", err)
	}
	if len(components) != 2 || components[0] != newGID[2] || components[1] != newGID[5] {
		t.Errorf("components %v, want the new A %d and acute %d", components, newGID[2], newGID[5])
	}
	widths, err := GlyphWidths(ctx, out)
	if err != nil || widths[newGID[5]] != 682 {
		t.Errorf("acute width %v, %v", widths, err)
	}
}

func TestSubset_KeepsTheSymbolCmap(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	out, newGID, err := Subset(ctx, subsetSource(), []uint16{8})
	if err != nil {
		t.Fatalf("Subset: %v", err)
	}
	symbol, err := SymbolCharCodeToGID(ctx, out)
	if err != nil {
		t.Fatalf("SymbolCharCodeToGID: %v", err)
	}
	if len(symbol) != 1 || symbol[0xA7] != newGID[8] {
		t.Errorf("symbol cmap %v, want 0xA7 on glyph %d", symbol, newGID[8])
	}
	subtables, err := CmapSubtables(out)
	if err != nil || len(subtables) != 2 || subtables[0] != (CmapSubtable{3, 0}) || subtables[1] != (CmapSubtable{3, 1}) {
		t.Errorf("subtables %v, %v, want (3,0) and (3,1) as the source", subtables, err)
	}
}

func TestSubset_OutOfOrderCodes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// A, B and C are consecutive codes whose glyphs, 2, 4 and 3, are not, so
	// the format 4 segment reads them through glyphIdArray.
	out, _, err := Subset(ctx, subsetSource(), []uint16{2, 3, 4})
	if err != nil {
		t.Fatalf("Subset: %v", err)
	}
	codes, err := CharCodeToGID(ctx, out)
	if err != nil {
		t.Fatalf("CharCodeToGID: %v", err)
	}
	names, _ := PostGlyphNames(ctx, out)
	for _, c := range "ABC" {
		if names[codes[uint16(c)]] != string(c) {
			t.Errorf("code %c reaches glyph %d named %q", c, codes[uint16(c)], names[codes[uint16(c)]])
		}
	}
}

func TestSubset_Refuses(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := subsetSource()
	if _, _, err := Subset(ctx, src, []uint16{9}); !errors.Is(err, ErrInvalidFont) {
		t.Errorf("glyph past numGlyphs: %v", err)
	}
	otto := append([]byte("OTTO"), src[4:]...)
	if _, _, err := Subset(ctx, otto, nil); !errors.Is(err, ErrInvalidFont) {
		t.Errorf("OTTO program: %v", err)
	}
	if _, _, err := Subset(ctx, src[:len(src)/2], []uint16{1}); err == nil {
		t.Errorf("a truncated program was subset")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := Subset(cancelled, src, []uint16{1}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
}

func TestEncodeCmapSubtable_RoundTrips(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tc := range []struct {
		name     string
		platform uint16
		codes    map[uint32]uint16
		format   uint16
	}{
		{"mac bytes", 1, map[uint32]uint16{0x41: 3, 0x42: 1}, 0},
		{"mac wide glyphs", 1, map[uint32]uint16{0x41: 300}, 6},
		{"bmp", 3, map[uint32]uint16{0x41: 3, 0x42: 4, 0x43: 9, 0x2022: 2}, 4},
		{"astral", 3, map[uint32]uint16{0x41: 1, 0x1F600: 2, 0x1F601: 3}, 12},
		{"empty", 3, map[uint32]uint16{}, 4},
	} {
		sub := encodeCmapSubtable(tc.platform, 0, tc.codes)
		if f := binary.BigEndian.Uint16(sub); f != tc.format {
			t.Errorf("%s: format %d, want %d", tc.name, f, tc.format)
		}
		got, _, err := decodeCmapSubtable(ctx, sub)
		if err != nil {
			t.Fatalf("%s: decode: %v", tc.name, err)
		}
		if len(got) != len(tc.codes) {
			t.Errorf("%s: decoded %v, want %v", tc.name, got, tc.codes)
		}
		for c, gid := range tc.codes {
			if got[c] != gid {
				t.Errorf("%s: code %#x -> %d, want %d", tc.name, c, got[c], gid)
			}
		}
	}
}
