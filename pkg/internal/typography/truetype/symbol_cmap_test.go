package truetype

import (
	"context"
	"encoding/binary"
	"testing"
)

func TestCmapSubtableCodeToGID_Format0(t *testing.T) {
	sub := make([]byte, 262)
	binary.BigEndian.PutUint16(sub[0:2], 0) // format 0
	sub[6+65] = 12                          // code 'A' -> GID 12
	sub[6+0x20] = 3                         // code space -> GID 3
	got, err := cmapSubtableCodeToGID(context.Background(), sub)
	if err != nil {
		t.Fatal(err)
	}
	if got[65] != 12 || got[0x20] != 3 {
		t.Errorf("format 0: got %v, want code 65->12, 32->3", got)
	}
	if _, ok := got[66]; ok {
		t.Errorf("unmapped code 66 should be absent, got %d", got[66])
	}
}

func TestCmapSubtableCodeToGID_Format6(t *testing.T) {
	sub := make([]byte, 10+3*2)
	binary.BigEndian.PutUint16(sub[0:2], 6) // format 6
	binary.BigEndian.PutUint16(sub[6:8], 100)
	binary.BigEndian.PutUint16(sub[8:10], 3) // 3 entries starting at 100
	binary.BigEndian.PutUint16(sub[10:12], 7)
	binary.BigEndian.PutUint16(sub[12:14], 0) // GID 0 skipped
	binary.BigEndian.PutUint16(sub[14:16], 9)
	got, err := cmapSubtableCodeToGID(context.Background(), sub)
	if err != nil {
		t.Fatal(err)
	}
	if got[100] != 7 || got[102] != 9 {
		t.Errorf("format 6: got %v, want 100->7, 102->9", got)
	}
	if _, ok := got[101]; ok {
		t.Errorf("code 101 (GID 0) should be absent")
	}
}

// format4Segment builds a minimal Format 4 subtable mapping one code to one GID
// via idDelta (idRangeOffset == 0), plus the required 0xFFFF terminator segment.
func format4Segment(code, gid uint16) []byte {
	const segCount = 2
	b := make([]byte, 14+segCount*8+2)
	binary.BigEndian.PutUint16(b[0:2], 4)           // format
	binary.BigEndian.PutUint16(b[6:8], segCount*2)  // segCountX2
	end := 14                                       // endCode[]
	binary.BigEndian.PutUint16(b[end:], code)       // seg 0 end
	binary.BigEndian.PutUint16(b[end+2:], 0xFFFF)   // seg 1 end (terminator)
	start := end + segCount*2 + 2                   // +2 reservedPad
	binary.BigEndian.PutUint16(b[start:], code)     // seg 0 start
	binary.BigEndian.PutUint16(b[start+2:], 0xFFFF) // seg 1 start
	delta := start + segCount*2
	binary.BigEndian.PutUint16(b[delta:], gid-code) // seg 0 idDelta -> gid = code + delta
	binary.BigEndian.PutUint16(b[delta+2:], 1)      // seg 1 idDelta
	// idRangeOffset all zero.
	return b
}

func TestCmapSubtableCodeToGID_Format4(t *testing.T) {
	got, err := cmapSubtableCodeToGID(context.Background(), format4Segment(0xF041, 50))
	if err != nil {
		t.Fatal(err)
	}
	if got[0xF041] != 50 {
		t.Errorf("format 4: got %v, want 0xF041->50", got)
	}
}

// minimalSymbolFont builds a one-table (cmap) sfnt whose sole subtable is a
// (3,0) Microsoft Symbol Format 4 table.
func minimalSymbolFont(subtable []byte) []byte {
	// cmap table: version(2) numTables(2) + one record(8) + subtable.
	cmap := make([]byte, 4+8)
	binary.BigEndian.PutUint16(cmap[2:4], 1)   // numTables
	binary.BigEndian.PutUint16(cmap[4:6], 3)   // platformID 3
	binary.BigEndian.PutUint16(cmap[6:8], 0)   // encodingID 0 (Symbol)
	binary.BigEndian.PutUint32(cmap[8:12], 12) // subtable offset
	cmap = append(cmap, subtable...)

	// sfnt: offset table(12) + one table record(16) + cmap table.
	font := make([]byte, 12+16)
	binary.BigEndian.PutUint32(font[0:4], 0x00010000) // sfnt version
	binary.BigEndian.PutUint16(font[4:6], 1)          // numTables
	copy(font[12:16], "cmap")
	binary.BigEndian.PutUint32(font[20:24], 12+16)             // offset
	binary.BigEndian.PutUint32(font[24:28], uint32(len(cmap))) // length
	font = append(font, cmap...)
	return font
}

func TestSymbolCharCodeToGID(t *testing.T) {
	// Symbol cmap places code 'A' (0x41) at 0xF041 -> GID 50.
	font := minimalSymbolFont(format4Segment(0xF041, 50))
	got, err := SymbolCharCodeToGID(context.Background(), font)
	if err != nil {
		t.Fatalf("SymbolCharCodeToGID: %v", err)
	}
	if got[0x41] != 50 {
		t.Errorf("code 0x41 via 0xF041 offset: got %d, want 50", got[0x41])
	}
	if _, ok := got[0x42]; ok {
		t.Errorf("unmapped code 0x42 should be absent")
	}
}

// TestSymbolCharCodeToGID_F100Range pins that a (3,0) subtable placing its
// codes in the 0xF100 range resolves them. ISO 32000-1, 9.6.6.4 allows the
// 0x0000, 0xF000, 0xF100 and 0xF200 ranges, and only the first two were tried.
func TestSymbolCharCodeToGID_F100Range(t *testing.T) {
	font := minimalSymbolFont(format4Segment(0xF141, 60))
	got, err := SymbolCharCodeToGID(context.Background(), font)
	if err != nil {
		t.Fatalf("SymbolCharCodeToGID: %v", err)
	}
	if got[0x41] != 60 {
		t.Errorf("code 0x41 via 0xF141: got %d, want 60", got[0x41])
	}
}

func TestSymbolCharCodeToGID_NoSymbolSubtable(t *testing.T) {
	// A font with no (3,0)/(1,0) subtable yields a nil map so callers defer.
	cmap := make([]byte, 4+8)
	binary.BigEndian.PutUint16(cmap[2:4], 1)
	binary.BigEndian.PutUint16(cmap[4:6], 3)
	binary.BigEndian.PutUint16(cmap[6:8], 1) // (3,1) Unicode, not a symbol subtable
	binary.BigEndian.PutUint32(cmap[8:12], 12)
	cmap = append(cmap, format4Segment(0x41, 50)...)
	font := make([]byte, 12+16)
	binary.BigEndian.PutUint32(font[0:4], 0x00010000)
	binary.BigEndian.PutUint16(font[4:6], 1)
	copy(font[12:16], "cmap")
	binary.BigEndian.PutUint32(font[20:24], 28)
	binary.BigEndian.PutUint32(font[24:28], uint32(len(cmap)))
	font = append(font, cmap...)

	got, err := SymbolCharCodeToGID(context.Background(), font)
	if err != nil {
		t.Fatalf("SymbolCharCodeToGID: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for a font without a (3,0)/(1,0) subtable, got %v", got)
	}
}
