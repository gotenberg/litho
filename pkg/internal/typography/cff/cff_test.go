package cff

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"slices"
	"testing"
)

// encodeINDEX builds a CFF INDEX from a slice of byte entries with the
// smallest possible offSize.
func encodeINDEX(t *testing.T, entries [][]byte) []byte {
	t.Helper()
	count := len(entries)
	if count == 0 {
		return []byte{0x00, 0x00}
	}

	totalSize := 0
	for _, e := range entries {
		totalSize += len(e)
	}
	// Pick the smallest offSize that fits totalSize+1 (offsets are 1-based).
	maxOff := totalSize + 1
	var offSize int
	switch {
	case maxOff < 1<<8:
		offSize = 1
	case maxOff < 1<<16:
		offSize = 2
	case maxOff < 1<<24:
		offSize = 3
	default:
		offSize = 4
	}

	out := make([]byte, 0, 3+(count+1)*offSize+totalSize)
	out = append(out, byte(count>>8), byte(count))
	out = append(out, byte(offSize))

	offset := 1 // 1-based per CFF spec
	out = appendOffset(out, offset, offSize)
	for _, e := range entries {
		offset += len(e)
		out = appendOffset(out, offset, offSize)
	}
	for _, e := range entries {
		out = append(out, e...)
	}
	return out
}

func appendOffset(out []byte, val, size int) []byte {
	switch size {
	case 1:
		return append(out, byte(val))
	case 2:
		return append(out, byte(val>>8), byte(val))
	case 3:
		return append(out, byte(val>>16), byte(val>>8), byte(val))
	case 4:
		return append(out, byte(val>>24), byte(val>>16), byte(val>>8), byte(val))
	}
	return out
}

// encodeCFFInt encodes a small integer using CFF DICT encoding.
// Supports the full int32 range via the 28/29 escape forms.
func encodeCFFInt(v int32) []byte {
	switch {
	case v >= -107 && v <= 107:
		return []byte{byte(v + 139)}
	case v >= 108 && v <= 1131:
		v -= 108
		return []byte{byte(v>>8) + 247, byte(v & 0xFF)}
	case v >= -1131 && v <= -108:
		v = -v - 108
		return []byte{byte(v>>8) + 251, byte(v & 0xFF)}
	case v >= -32768 && v <= 32767:
		return []byte{28, byte(v >> 8), byte(v)}
	default:
		b := make([]byte, 5)
		b[0] = 29
		binary.BigEndian.PutUint32(b[1:], uint32(v))
		return b
	}
}

func TestReadOffset(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		size int
		want int
	}{
		{"size 1", []byte{0x42}, 1, 0x42},
		{"size 2", []byte{0x12, 0x34}, 2, 0x1234},
		{"size 3", []byte{0x12, 0x34, 0x56}, 3, 0x123456},
		{"size 4", []byte{0x12, 0x34, 0x56, 0x78}, 4, 0x12345678},
		{"unknown", []byte{0x00}, 5, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := readOffset(tc.data, tc.size); got != tc.want {
				t.Errorf("readOffset: got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestParseINDEX_RoundTrip(t *testing.T) {
	entries := [][]byte{
		[]byte("hello"),
		[]byte("world"),
		[]byte("foo"),
	}
	idx := encodeINDEX(t, entries)

	end, got, err := parseINDEX(idx, 0)
	if err != nil {
		t.Fatalf("parseINDEX: %v", err)
	}
	if end != len(idx) {
		t.Errorf("end pos: got %d, want %d", end, len(idx))
	}
	if len(got) != len(entries) {
		t.Fatalf("entries: got %d, want %d", len(got), len(entries))
	}
	for i, e := range entries {
		if string(got[i]) != string(e) {
			t.Errorf("entry %d: got %q, want %q", i, got[i], e)
		}
	}
}

func TestParseINDEX_Empty(t *testing.T) {
	end, entries, err := parseINDEX([]byte{0x00, 0x00}, 0)
	if err != nil {
		t.Fatalf("parseINDEX: %v", err)
	}
	if end != 2 {
		t.Errorf("end: got %d, want 2", end)
	}
	if len(entries) != 0 {
		t.Errorf("entries: got %d, want 0", len(entries))
	}
}

func TestParseINDEX_Truncated(t *testing.T) {
	tests := []struct {
		name string
		data []byte
	}{
		{"missing count", []byte{0x00}},
		{"missing offSize", []byte{0x00, 0x01}},
		{"missing offsets", []byte{0x00, 0x01, 0x01}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := parseINDEX(tc.data, 0)
			if !errors.Is(err, ErrTruncated) {
				t.Errorf("expected ErrTruncated, got %v", err)
			}
		})
	}
}

func TestParseINDEX_InvalidOffSize(t *testing.T) {
	data := []byte{0x00, 0x01, 0x05} // count=1, offSize=5 (invalid)
	_, _, err := parseINDEX(data, 0)
	if !errors.Is(err, ErrInvalidCFF) {
		t.Errorf("expected ErrInvalidCFF, got %v", err)
	}
}

func TestParseRealNumber(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want float64
	}{
		// CFF spec example: -2.25 → 0xE2 0xA2 0x5F
		// Nibbles: e=- 2 . 2 5 EOF
		{"-2.25", []byte{0xE2, 0xA2, 0x5F}, -2.25},
		// 0.001 → 0xA0 0x01 0xFF (.001 EOF)
		{"0.001", []byte{0xA0, 0x01, 0xFF}, 0.001},
		// 1000.0 → nibbles 1,0,0,0,F → 0x10 0x00 0xFF
		{"1000", []byte{0x10, 0x00, 0xFF}, 1000.0},
		// Exponent: 1.5E2 → 0x1A, 0x5B, 0x2F → 1, ., 5, E, 2, EOF → 150
		{"1.5E2", []byte{0x1A, 0x5B, 0x2F}, 150.0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, _ := parseRealNumber(tc.data, 0)
			if math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("parseRealNumber(%v): got %v, want %v", tc.data, got, tc.want)
			}
		})
	}
}

func TestParsePrivateDict(t *testing.T) {
	// defaultWidthX (op 20) = 500, nominalWidthX (op 21) = 250
	dict := []byte{}
	dict = append(dict, encodeCFFInt(500)...)
	dict = append(dict, 20)
	dict = append(dict, encodeCFFInt(250)...)
	dict = append(dict, 21)

	nominal, defaultW := parsePrivateDict(dict)
	if defaultW != 500 {
		t.Errorf("defaultWidthX: got %d, want 500", defaultW)
	}
	if nominal != 250 {
		t.Errorf("nominalWidthX: got %d, want 250", nominal)
	}
}

func TestParsePrivateDict_Empty(t *testing.T) {
	nominal, defaultW := parsePrivateDict(nil)
	if nominal != 0 || defaultW != 0 {
		t.Errorf("expected zero values, got nominal=%d default=%d", nominal, defaultW)
	}
}

func TestParseTopDict_OffsetOperators(t *testing.T) {
	// charset (op 15) = 100, Encoding (op 16) = 200, CharStrings (op 17) = 300
	// Private (op 18) = size 50, offset 400
	// ROS (op 12 30) marks CID
	dict := []byte{}
	dict = append(dict, encodeCFFInt(100)...)
	dict = append(dict, 15)
	dict = append(dict, encodeCFFInt(200)...)
	dict = append(dict, 16)
	dict = append(dict, encodeCFFInt(300)...)
	dict = append(dict, 17)
	dict = append(dict, encodeCFFInt(50)...)
	dict = append(dict, encodeCFFInt(400)...)
	dict = append(dict, 18)
	// ROS: needs three operands (registry SID, ordering SID, supplement)
	dict = append(dict, encodeCFFInt(391)...)
	dict = append(dict, encodeCFFInt(392)...)
	dict = append(dict, encodeCFFInt(0)...)
	dict = append(dict, 12, 30)

	info := parseTopDict(dict)
	if info.charsetOffset != 100 {
		t.Errorf("charsetOffset: got %d, want 100", info.charsetOffset)
	}
	if info.encodingOffset != 200 {
		t.Errorf("encodingOffset: got %d, want 200", info.encodingOffset)
	}
	if info.charStringsOffset != 300 {
		t.Errorf("charStringsOffset: got %d, want 300", info.charStringsOffset)
	}
	if info.privateDictSize != 50 {
		t.Errorf("privateDictSize: got %d, want 50", info.privateDictSize)
	}
	if info.privateDictOffset != 400 {
		t.Errorf("privateDictOffset: got %d, want 400", info.privateDictOffset)
	}
	if !info.isCID {
		t.Error("isCID: got false, want true")
	}
}

func TestParseFontMatrixScale_Default(t *testing.T) {
	// FontMatrix [0.001 0 0 0.001 0 0] → first element 0.001
	// Real number 0.001 → 0xA0 0x01 0xFF
	dict := []byte{
		30, 0xA0, 0x01, 0xFF, // 0.001
		30, 0x0F, // 0
		30, 0x0F, // 0
		30, 0xA0, 0x01, 0xFF, // 0.001
		30, 0x0F, // 0
		30, 0x0F, // 0
		12, 7, // FontMatrix
	}
	got := parseFontMatrixScale(dict)
	if math.Abs(got-0.001) > 1e-9 {
		t.Errorf("parseFontMatrixScale: got %v, want 0.001", got)
	}
}

func TestParseFontMatrixScale_Missing(t *testing.T) {
	if got := parseFontMatrixScale(nil); got != 0 {
		t.Errorf("expected 0, got %v", got)
	}
}

func TestParseCharset_Format0(t *testing.T) {
	// Format 0: array of SIDs (uint16). 3 glyphs (.notdef + 2 entries).
	data := []byte{
		0,          // format
		0x00, 0x05, // SID 5
		0x00, 0x06, // SID 6
	}
	sids, err := parseCharset(data, 0, 3)
	if err != nil {
		t.Fatalf("parseCharset: %v", err)
	}
	want := []int{0, 5, 6}
	if len(sids) != len(want) {
		t.Fatalf("sids length: got %d, want %d", len(sids), len(want))
	}
	for i, s := range want {
		if sids[i] != s {
			t.Errorf("sids[%d]: got %d, want %d", i, sids[i], s)
		}
	}
}

func TestParseCharset_Format1(t *testing.T) {
	// Format 1: ranges with uint8 nLeft.
	// Range: first=10, nLeft=3 → SIDs 10,11,12,13
	data := []byte{
		1,          // format
		0x00, 0x0A, // first = 10
		0x03, // nLeft = 3
	}
	sids, err := parseCharset(data, 0, 5)
	if err != nil {
		t.Fatalf("parseCharset: %v", err)
	}
	want := []int{0, 10, 11, 12, 13}
	for i, s := range want {
		if sids[i] != s {
			t.Errorf("sids[%d]: got %d, want %d", i, sids[i], s)
		}
	}
}

func TestParseCharset_Format2(t *testing.T) {
	// Format 2: ranges with uint16 nLeft.
	data := []byte{
		2,          // format
		0x00, 0x14, // first = 20
		0x00, 0x02, // nLeft = 2
	}
	sids, err := parseCharset(data, 0, 4)
	if err != nil {
		t.Fatalf("parseCharset: %v", err)
	}
	want := []int{0, 20, 21, 22}
	for i, s := range want {
		if sids[i] != s {
			t.Errorf("sids[%d]: got %d, want %d", i, sids[i], s)
		}
	}
}

func TestParseCharset_OutOfBoundsReturnsNil(t *testing.T) {
	// parseCharset decodes an in-font charset table at a byte offset. A pos at
	// or past the end of data is a malformed offset (predefined charsets are
	// resolved by resolveCharset before reaching here); it returns no charset
	// rather than reading out of bounds.
	sids, err := parseCharset(nil, 0, 3)
	if err != nil {
		t.Fatalf("parseCharset: %v", err)
	}
	if sids != nil {
		t.Errorf("expected nil for an out-of-bounds charset offset")
	}
}

func TestResolveCharset_Predefined(t *testing.T) {
	// A charset offset of 0/1/2 selects a predefined charset (Adobe TN #5176
	// Appendix C) regardless of the data bytes. Pass a non-empty data slice
	// whose byte 0 is a valid charset format (0) to prove the offset, not the
	// data, drives the predefined dispatch.
	data := []byte{0, 0x00, 0x05}

	// ISOAdobe (0): identity, GID i -> SID i.
	iso, err := resolveCharset(data, 0, 5)
	if err != nil {
		t.Fatalf("resolveCharset ISOAdobe: %v", err)
	}
	if want := []int{0, 1, 2, 3, 4}; !slices.Equal(iso, want) {
		t.Errorf("ISOAdobe: got %v, want %v", iso, want)
	}

	// Expert (1) and ExpertSubset (2): GID 1 is space (SID 1); GID 2 is the
	// first expert-specific glyph.
	exp, err := resolveCharset(data, 1, 3)
	if err != nil {
		t.Fatalf("resolveCharset Expert: %v", err)
	}
	if want := []int{0, 1, 229}; !slices.Equal(exp, want) { // .notdef, space, exclamsmall
		t.Errorf("Expert: got %v, want %v", exp, want)
	}
	sub, err := resolveCharset(data, 2, 3)
	if err != nil {
		t.Fatalf("resolveCharset ExpertSubset: %v", err)
	}
	if want := []int{0, 1, 231}; !slices.Equal(sub, want) { // .notdef, space, dollaroldstyle
		t.Errorf("ExpertSubset: got %v, want %v", sub, want)
	}

	// A GID beyond a predefined table's length carries no name (SID 0).
	over, err := resolveCharset(data, 2, len(expertSubsetCharsetSIDs)+2)
	if err != nil {
		t.Fatalf("resolveCharset overflow: %v", err)
	}
	if over[len(expertSubsetCharsetSIDs)] != 0 || over[len(expertSubsetCharsetSIDs)+1] != 0 {
		t.Errorf("surplus glyphs should map to SID 0, got %v", over[len(expertSubsetCharsetSIDs):])
	}
}

// TestPredefinedCharsets pins the generated Expert/ExpertSubset SID tables so a
// transcription slip surfaces: the lengths match Appendix C, every SID resolves
// to a standard string, and known anchor glyphs land where the spec places them.
func TestPredefinedCharsets(t *testing.T) {
	if len(expertCharsetSIDs) != 166 {
		t.Errorf("Expert charset: got %d glyphs, want 166", len(expertCharsetSIDs))
	}
	if len(expertSubsetCharsetSIDs) != 87 {
		t.Errorf("ExpertSubset charset: got %d glyphs, want 87", len(expertSubsetCharsetSIDs))
	}
	st := &stringTable{}
	for name, table := range map[string][]int{"Expert": expertCharsetSIDs[:], "ExpertSubset": expertSubsetCharsetSIDs[:]} {
		for gid, sid := range table {
			if sid < 0 || sid >= len(cffStandardStrings) {
				t.Errorf("%s GID %d: SID %d out of standard-string range", name, gid, sid)
				continue
			}
			if st.get(sid) == "" {
				t.Errorf("%s GID %d: SID %d resolves to empty name", name, gid, sid)
			}
		}
		if got := st.get(table[0]); got != ".notdef" {
			t.Errorf("%s GID 0: got %q, want .notdef", name, got)
		}
		if got := st.get(table[1]); got != "space" {
			t.Errorf("%s GID 1: got %q, want space", name, got)
		}
	}
	if got := st.get(expertCharsetSIDs[2]); got != "exclamsmall" {
		t.Errorf("Expert GID 2: got %q, want exclamsmall", got)
	}
	if got := st.get(expertSubsetCharsetSIDs[2]); got != "dollaroldstyle" {
		t.Errorf("ExpertSubset GID 2: got %q, want dollaroldstyle", got)
	}
}

func TestParseCharset_UnknownFormat(t *testing.T) {
	data := []byte{99} // unknown format
	_, err := parseCharset(data, 0, 3)
	if !errors.Is(err, ErrInvalidCFF) {
		t.Errorf("expected ErrInvalidCFF, got %v", err)
	}
}

func TestStringTable_Standard(t *testing.T) {
	st := &stringTable{}
	if got := st.get(0); got != ".notdef" {
		t.Errorf("SID 0: got %q, want .notdef", got)
	}
	if got := st.get(1); got != "space" {
		t.Errorf("SID 1: got %q, want space", got)
	}
}

func TestStringTable_Custom(t *testing.T) {
	st := &stringTable{custom: [][]byte{[]byte("MyGlyph")}}
	want := "MyGlyph"
	if got := st.get(391); got != want {
		t.Errorf("SID 391: got %q, want %q", got, want)
	}
}

func TestStringTable_OutOfRange(t *testing.T) {
	st := &stringTable{}
	if got := st.get(99999); got != "" {
		t.Errorf("expected empty string for out-of-range SID, got %q", got)
	}
}

func TestExtractCharstringWidth_Endchar(t *testing.T) {
	// width 100, endchar (op 14)
	cs := append(encodeCFFInt(100), 14)
	w := extractCharstringWidth(cs)
	if w == nil {
		t.Fatal("expected width, got nil")
	}
	if *w != 100 {
		t.Errorf("width: got %d, want 100", *w)
	}
}

func TestExtractCharstringWidth_NoWidthEndchar(t *testing.T) {
	cs := []byte{14}
	if w := extractCharstringWidth(cs); w != nil {
		t.Errorf("expected nil, got %d", *w)
	}
}

func TestExtractCharstringWidth_EndcharSeacNoWidth(t *testing.T) {
	// endchar with 4 operands is the deprecated seac accent composition
	// (adx ady bchar achar) and carries NO width: the glyph uses defaultWidthX.
	// Reading the first operand as a width is a bug (produced 496 instead of a
	// defaulted 500 on real Univers-Oblique accented glyphs).
	cs := append(encodeCFFInt(111), encodeCFFInt(0)...)
	cs = append(cs, encodeCFFInt(101)...)
	cs = append(cs, encodeCFFInt(194)...)
	cs = append(cs, 14)
	if w := extractCharstringWidth(cs); w != nil {
		t.Errorf("seac endchar (4 operands) carries no width; got %d", *w)
	}
}

func TestExtractCharstringWidth_EndcharSeacWithWidth(t *testing.T) {
	// endchar with 5 operands is width + seac (adx ady bchar achar).
	cs := append(encodeCFFInt(250), encodeCFFInt(111)...)
	cs = append(cs, encodeCFFInt(0)...)
	cs = append(cs, encodeCFFInt(101)...)
	cs = append(cs, encodeCFFInt(194)...)
	cs = append(cs, 14)
	w := extractCharstringWidth(cs)
	if w == nil {
		t.Fatal("expected width, got nil")
	}
	if *w != 250 {
		t.Errorf("width: got %d, want 250", *w)
	}
}

func TestExtractCharstringWidth_HMoveto(t *testing.T) {
	// width 200, dx 50, hmoveto (op 22)
	cs := append(encodeCFFInt(200), encodeCFFInt(50)...)
	cs = append(cs, 22)
	w := extractCharstringWidth(cs)
	if w == nil {
		t.Fatal("expected width, got nil")
	}
	if *w != 200 {
		t.Errorf("width: got %d, want 200", *w)
	}
}

func TestExtractCharstringWidth_HStem_OddOperands(t *testing.T) {
	// hstem expects an even number of operands. Add one extra → that's the width.
	// width 300, [y, dy] pair, hstem (op 1)
	cs := append(encodeCFFInt(300), encodeCFFInt(0)...)
	cs = append(cs, encodeCFFInt(50)...)
	cs = append(cs, 1)
	w := extractCharstringWidth(cs)
	if w == nil {
		t.Fatal("expected width, got nil")
	}
	if *w != 300 {
		t.Errorf("width: got %d, want 300", *w)
	}
}

func TestExtractCharstringWidth_NegativeWidth(t *testing.T) {
	cs := append(encodeCFFInt(-50), 14)
	w := extractCharstringWidth(cs)
	if w == nil {
		t.Fatal("expected width, got nil")
	}
	if *w != -50 {
		t.Errorf("width: got %d, want -50", *w)
	}
}

func TestReadUnitsPerEm_DefaultOnInvalid(t *testing.T) {
	if got := ReadUnitsPerEm(nil); got != 0 {
		t.Errorf("nil data: got %d, want 0", got)
	}
	if got := ReadUnitsPerEm([]byte{0xFF, 0, 0, 0}); got != 0 {
		t.Errorf("bad version: got %d, want 0", got)
	}
}

func TestGlyphToUnicode_InvalidHeader(t *testing.T) {
	ctx := context.Background()
	_, err := GlyphToUnicode(ctx, []byte{0xFF, 0, 0, 0})
	if !errors.Is(err, ErrInvalidCFF) {
		t.Errorf("expected ErrInvalidCFF, got %v", err)
	}
}

func TestGlyphToUnicode_TooShort(t *testing.T) {
	ctx := context.Background()
	_, err := GlyphToUnicode(ctx, []byte{0x01})
	if !errors.Is(err, ErrInvalidCFF) {
		t.Errorf("expected ErrInvalidCFF, got %v", err)
	}
}

func TestGlyphNames_InvalidHeader(t *testing.T) {
	ctx := context.Background()
	_, err := GlyphNames(ctx, []byte{0xFF, 0, 0, 0})
	if !errors.Is(err, ErrInvalidCFF) {
		t.Errorf("expected ErrInvalidCFF, got %v", err)
	}
}

func TestValidCIDs_NotCID(t *testing.T) {
	// A minimal non-CID font should return nil, nil.
	data := buildMinimalCFF(t)
	cids, err := ValidCIDs(data)
	if err != nil {
		t.Fatalf("ValidCIDs: %v", err)
	}
	if cids != nil {
		t.Errorf("expected nil for non-CID font, got %d entries", len(cids))
	}
}

func TestExtractEncoding_PredefinedStandard(t *testing.T) {
	// With encodingOffset == 0 (Standard predefined), the encoder
	// intersects the font's charset with the Standard encoding table
	// and returns byte→glyph-name pairs only for glyphs that actually
	// ship in the font. The minimal fixture declares glyph "A" at GID 1,
	// so byte 0x41 should map to "A".
	data := buildMinimalCFF(t)
	enc := ExtractEncoding(data)
	if got, want := enc[0x41], "A"; got != want {
		t.Errorf("Standard[0x41]: got %q, want %q", got, want)
	}
	if _, present := enc[0x42]; present {
		t.Errorf("Standard[0x42] should be absent (font has no B); got %q", enc[0x42])
	}
}

// buildMinimalCFF constructs a minimal but well-formed CFF font with two
// glyphs (.notdef and "A"). It exercises the public APIs without depending
// on a binary fixture.
func buildMinimalCFF(t *testing.T) []byte {
	t.Helper()

	// Layout we want to produce:
	//   header (4 bytes)
	//   Name INDEX
	//   Top DICT INDEX (referencing offsets we patch in)
	//   String INDEX (one custom entry: "MyFont")
	//   Global Subr INDEX (empty)
	//   CharStrings INDEX (2 entries, minimal endchar charstrings)
	//   Charset (format 0, one SID)
	//   Private DICT
	//
	// To avoid the chicken-and-egg of self-referential offsets we build the
	// later sections first and then construct the Top DICT with the right
	// absolute offsets.

	header := []byte{0x01, 0x00, 0x04, 0x01}

	nameINDEX := encodeINDEX(t, [][]byte{[]byte("MyFont")})
	stringINDEX := encodeINDEX(t, [][]byte{[]byte("A")}) // SID 391 → "A"

	// Each charstring: just an endchar with no width.
	charString := []byte{14}
	charStringsINDEX := encodeINDEX(t, [][]byte{charString, charString})

	globalSubrINDEX := []byte{0x00, 0x00}

	// Charset format 0: one entry pointing at SID 391 (custom "A").
	charset := []byte{0, 0x01, 0x87} // 0x0187 == 391

	// Private DICT: defaultWidthX = 0, nominalWidthX = 0 (omitted).
	privateDict := []byte{}

	// We need to know absolute offsets to reference from the Top DICT.
	// Build the Top DICT with placeholder offsets first to learn its size,
	// then rebuild with the real offsets.
	build := func(charsetOff, charStringsOff, privateOff int32) []byte {
		dict := []byte{}
		dict = append(dict, encodeCFFInt(charsetOff)...)
		dict = append(dict, 15) // charset
		dict = append(dict, encodeCFFInt(charStringsOff)...)
		dict = append(dict, 17) // CharStrings
		dict = append(dict, encodeCFFInt(int32(len(privateDict)))...)
		dict = append(dict, encodeCFFInt(privateOff)...)
		dict = append(dict, 18) // Private
		return dict
	}

	// First pass with worst-case 32-bit offsets so size is stable.
	placeholder := build(0x10000000, 0x10000000, 0x10000000)
	topDictINDEX := encodeINDEX(t, [][]byte{placeholder})

	// Compute absolute offsets given the planned layout.
	pos := len(header) + len(nameINDEX) + len(topDictINDEX) + len(stringINDEX) + len(globalSubrINDEX)
	charStringsOff := int32(pos)
	pos += len(charStringsINDEX)
	charsetOff := int32(pos)
	pos += len(charset)
	privateOff := int32(pos)

	// Rebuild Top DICT with real offsets, padded to the original size.
	dict := build(charsetOff, charStringsOff, privateOff)
	for len(dict) < len(placeholder) {
		// Pad with no-op operands that get discarded by the next operator.
		// Use integer 0 (single byte 139) followed by an unknown operator
		// is risky; instead, append a leading 0 operand and re-emit the
		// last operator. Simpler: just push extra integer operands that
		// the operator preceding will overwrite. We append a harmless
		// integer 0 before the next operator slot, but since the dict
		// is already complete, prepend.
		dict = append([]byte{139}, dict...)
	}
	topDictINDEX = encodeINDEX(t, [][]byte{dict})

	// Final assembly.
	out := []byte{}
	out = append(out, header...)
	out = append(out, nameINDEX...)
	out = append(out, topDictINDEX...)
	out = append(out, stringINDEX...)
	out = append(out, globalSubrINDEX...)
	out = append(out, charStringsINDEX...)
	out = append(out, charset...)
	out = append(out, privateDict...)

	return out
}

func TestBuildMinimalCFF_GlyphNames(t *testing.T) {
	ctx := context.Background()
	data := buildMinimalCFF(t)

	names, err := GlyphNames(ctx, data)
	if err != nil {
		t.Fatalf("GlyphNames: %v", err)
	}
	// Expect GID 1 → "A". GID 0 (.notdef) is intentionally included.
	if got := names[1]; got != "A" {
		t.Errorf("GID 1: got %q, want %q", got, "A")
	}
}

func TestBuildMinimalCFF_GlyphWidths(t *testing.T) {
	ctx := context.Background()
	data := buildMinimalCFF(t)

	widths, err := GlyphWidths(ctx, data)
	if err != nil {
		t.Fatalf("GlyphWidths: %v", err)
	}
	if len(widths) != 2 {
		t.Errorf("widths count: got %d, want 2", len(widths))
	}
}

func TestBuildMinimalCFF_GlyphToUnicode(t *testing.T) {
	ctx := context.Background()
	data := buildMinimalCFF(t)

	mapping, err := GlyphToUnicode(ctx, data)
	if err != nil {
		t.Fatalf("GlyphToUnicode: %v", err)
	}
	if r := mapping[1]; r != 'A' {
		t.Errorf("GID 1: got %q, want %q", r, 'A')
	}
}

func TestGlyphToUnicode_ContextCancel(t *testing.T) {
	data := buildMinimalCFF(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// With only 2 glyphs the loop completes before the context check
	// kicks in, but the function should still return successfully when
	// not cancelled mid-iteration. This guards against accidental ctx
	// nil-derefs.
	if _, err := GlyphToUnicode(ctx, data); err != nil && !errors.Is(err, context.Canceled) {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestExtractCharstringWidth_RejectsStackOverflow(t *testing.T) {
	// Bytes 32-246 are single-byte operand pushes per CFF Type 2.
	// A charstring made of just these never hits a stack-clearing
	// operator, so without the cap the stack would grow to len(cs).
	cs := make([]byte, maxCharstringStack+10)
	for i := range cs {
		cs[i] = 100 // any operand byte in [32, 246]
	}
	if got := extractCharstringWidth(cs); got != nil {
		t.Errorf("extractCharstringWidth on adversarial stack overflow returned %d, want nil", *got)
	}
}
