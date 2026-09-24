package jbig2

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"testing"
)

// codeString renders line i's assigned prefix code, or "" when it has none.
func codeString(t *huffTable, i int) string {
	for length, codes := range t.codes {
		for code, line := range codes {
			if line == i {
				return fmt.Sprintf("%0*b", length, code)
			}
		}
	}
	return ""
}

// TestStandardTables pins the fifteen standard tables of ITU-T T.88 B.5 in
// two independent ways at once: the line transcription and the B.3 code
// assignment must together reproduce the prefix printed in each table's
// "Encoding" column, line by line in the order B.2 gives (normal lines, lower
// range, upper range, out-of-band).
func TestStandardTables(t *testing.T) {
	t.Parallel()

	want := map[int][]string{
		1:  {"0", "10", "110", "111"},
		2:  {"0", "10", "110", "1110", "11110", "111110", "111111"},
		3:  {"11111110", "0", "10", "110", "1110", "11110", "11111111", "1111110", "111110"},
		4:  {"0", "10", "110", "1110", "11110", "11111"},
		5:  {"1111110", "0", "10", "110", "1110", "11110", "1111111", "111110"},
		6:  {"11100", "1000", "1001", "1010", "11101", "11110", "1011", "00", "010", "011", "1100", "1101", "111110", "111111"},
		7:  {"1000", "000", "1001", "11010", "11011", "1010", "1011", "11100", "11101", "1100", "001", "010", "011", "11110", "11111"},
		8:  {"11111100", "111111100", "11111101", "111111101", "1111100", "1010", "00", "11010", "111010", "100", "111011", "1011", "1100", "11011", "11100", "111100", "1111101", "111101", "111111110", "111111111", "01"},
		9:  {"11111100", "111111100", "11111101", "111111101", "1111100", "1010", "010", "011", "11010", "111010", "100", "111011", "1011", "1100", "11011", "11100", "111100", "1111101", "111101", "111111110", "111111111", "00"},
		10: {"1111010", "11111100", "1111011", "11000", "00", "11001", "110110", "1111100", "11111101", "01", "11010", "110111", "111000", "111001", "111010", "111011", "111100", "1111101", "11111110", "11111111", "10"},
		11: {"0", "10", "1100", "1101", "11100", "11101", "111100", "1111010", "1111011", "1111100", "1111101", "1111110", "1111111"},
		12: {"0", "10", "110", "11100", "11101", "111100", "1111010", "1111011", "1111100", "1111101", "1111110", "11111110", "11111111"},
		13: {"0", "100", "1100", "11100", "1101", "101", "111010", "111011", "111100", "111101", "111110", "1111110", "1111111"},
		14: {"100", "101", "0", "110", "111"},
		15: {"1111100", "111100", "11100", "1100", "100", "0", "101", "1101", "11101", "111101", "1111101", "1111110", "1111111"},
	}
	for n := 1; n <= 15; n++ {
		table, err := standardTable(n)
		if err != nil {
			t.Fatalf("B.%d: %v", n, err)
		}
		var got []string
		for i := range table.lines {
			got = append(got, codeString(table, i))
		}
		if !slices.Equal(got, want[n]) {
			t.Errorf("B.%d codes\n  %v\nwant\n  %v", n, got, want[n])
		}
	}
}

// TestParseCodeTable_AnnexB4Example decodes the encoded table of ITU-T T.88
// B.4, which is Table B.1 sent as a code table segment, and checks it decodes
// values the way the standard Table B.1 does.
func TestParseCodeTable_AnnexB4Example(t *testing.T) {
	t.Parallel()

	sent, err := parseCodeTable([]byte{0x42, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x01, 0x10, 0x49, 0x23, 0x81, 0x80})
	if err != nil {
		t.Fatalf("parseCodeTable: %v", err)
	}
	var gotCodes []string
	for i := range sent.lines {
		gotCodes = append(gotCodes, codeString(sent, i))
	}
	// B.4: codes 0, 10, 110, none for the unused lower range line, 111.
	if want := []string{"0", "10", "110", "", "111"}; !slices.Equal(gotCodes, want) {
		t.Errorf("codes %v, want %v", gotCodes, want)
	}

	std, err := standardTable(1)
	if err != nil {
		t.Fatalf("standardTable: %v", err)
	}
	// 0101 -> 5; 10 00000011 -> 19; 110 0000000000000001 -> 273;
	// 111 + 32 bits of 2 -> 65810.
	data := []byte{0x2C, 0x07, 0x80, 0x00, 0x78, 0x00, 0x00, 0x00, 0x10}
	for _, table := range []*huffTable{sent, std} {
		r := &bitReader{data: data}
		for _, want := range []int64{5, 19, 273, 65810} {
			if got, err := table.value(r, "test value"); err != nil || got != want {
				t.Errorf("decoded %d (%v), want %d", got, err, want)
			}
		}
	}
}

// TestSymbolIDTable_Example1 decodes the symbol ID Huffman table of ITU-T
// T.88 7.4.3.1.7, EXAMPLE 1: 32 symbol ID code lengths sent as run codes.
func TestSymbolIDTable_Example1(t *testing.T) {
	t.Parallel()

	data := []byte{
		0x50, 0x03, 0x35, 0x32, 0x53, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x35, 0x0F,
		0x8B, 0x30, 0x9E, 0xB8, 0x5F, 0x1D, 0xD2, 0x83, 0x00,
	}
	r := &bitReader{data: data}
	table, err := decodeSymbolIDTable(r, 32)
	if err != nil {
		t.Fatalf("decodeSymbolIDTable: %v", err)
	}
	want := []int{0, 0, 0, 9, 6, 6, 6, 6, 3, 4, 4, 4, 4, 4, 4, 0, 7, 9, 8, 7, 5, 5, 5, 5, 5, 5, 3, 6, 7, 4, 7, 7}
	var got []int
	for _, l := range table.lines {
		got = append(got, l.prefLen)
	}
	if !slices.Equal(got, want) {
		t.Errorf("code lengths\n  %v\nwant\n  %v", got, want)
	}
	if r.pos != len(data) || r.bit != 0 {
		t.Errorf("reader stopped at byte %d bit %d, want the end of the %d bytes", r.pos, r.bit, len(data))
	}
}

// TestDecode_AnnexH1TextBothCodings decodes the symbol dictionaries and text
// region of Annex H.1 pages 1 and 2, which code the same data with Huffman
// tables and with arithmetic coding. Page 1 reaches the standard tables, MMR
// collective bitmaps and the run-coded symbol ID table; both pages also draw
// on the global dictionary, which is Huffman coded. The golden is jbig2dec
// 0.20's decode, and the two codings must agree with it and with each other.
func TestDecode_AnnexH1TextBothCodings(t *testing.T) {
	t.Parallel()

	raw := annexH1(t)
	want := readPBM(t, "testdata/t88_annex_h1_text.pbm")
	for _, tc := range []struct {
		name string
		segs []int
	}{
		{"page 1, Huffman", []int{1, 0, 2, 3}},
		{"page 2, arithmetic", []int{8, 0, 9, 10}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var stream []byte
			for _, i := range tc.segs {
				stream = append(stream, raw[i]...)
			}
			got, err := Decode(context.Background(), stream, nil, DefaultMaxBitmapPixels)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if got.Width != want.Width || got.Height != want.Height || !bytes.Equal(got.Data, want.Data) {
				t.Errorf("page is %dx%d and differs from the golden in %d bytes", got.Width, got.Height, differingBytes(got.Data, want.Data))
			}
		})
	}
}

// TestStandardTables_ValueRanges is a second transcription of the fifteen
// standard tables, from their "VAL" columns rather than their "Encoding"
// columns: each normal line's printed range must be exactly the range the
// line's lowest value and range length cover. TestStandardTables pins only the
// prefix codes, so a wrong range length or offset got past it.
func TestStandardTables_ValueRanges(t *testing.T) {
	t.Parallel()

	// Printed "VAL" ranges of each table's normal lines, in order, as [lo, hi].
	// B.7 prints "-64 ... -32" for a five-bit line starting at -64, which can
	// only reach -33; the next line starts at -32, so its range is taken as
	// -64 ... -33.
	printed := map[int][][2]int64{
		1:  {{0, 15}, {16, 271}, {272, 65807}},
		2:  {{0, 0}, {1, 1}, {2, 2}, {3, 10}, {11, 74}},
		3:  {{-256, -1}, {0, 0}, {1, 1}, {2, 2}, {3, 10}, {11, 74}},
		4:  {{1, 1}, {2, 2}, {3, 3}, {4, 11}, {12, 75}},
		5:  {{-255, 0}, {1, 1}, {2, 2}, {3, 3}, {4, 11}, {12, 75}},
		6:  {{-2048, -1025}, {-1024, -513}, {-512, -257}, {-256, -129}, {-128, -65}, {-64, -33}, {-32, -1}, {0, 127}, {128, 255}, {256, 511}, {512, 1023}, {1024, 2047}},
		7:  {{-1024, -513}, {-512, -257}, {-256, -129}, {-128, -65}, {-64, -33}, {-32, -1}, {0, 31}, {32, 63}, {64, 127}, {128, 255}, {256, 511}, {512, 1023}, {1024, 2047}},
		8:  {{-15, -8}, {-7, -6}, {-5, -4}, {-3, -3}, {-2, -2}, {-1, -1}, {0, 1}, {2, 2}, {3, 3}, {4, 19}, {20, 21}, {22, 37}, {38, 69}, {70, 133}, {134, 261}, {262, 389}, {390, 645}, {646, 1669}},
		9:  {{-31, -16}, {-15, -12}, {-11, -8}, {-7, -6}, {-5, -4}, {-3, -2}, {-1, 0}, {1, 2}, {3, 4}, {5, 6}, {7, 38}, {39, 42}, {43, 74}, {75, 138}, {139, 266}, {267, 522}, {523, 778}, {779, 1290}, {1291, 3338}},
		10: {{-21, -6}, {-5, -5}, {-4, -4}, {-3, -3}, {-2, 1}, {2, 2}, {3, 3}, {4, 4}, {5, 5}, {6, 69}, {70, 101}, {102, 133}, {134, 197}, {198, 325}, {326, 581}, {582, 1093}, {1094, 2117}, {2118, 4165}},
		11: {{1, 1}, {2, 3}, {4, 4}, {5, 6}, {7, 8}, {9, 12}, {13, 16}, {17, 20}, {21, 28}, {29, 44}, {45, 76}, {77, 140}},
		12: {{1, 1}, {2, 2}, {3, 4}, {5, 5}, {6, 7}, {8, 9}, {10, 10}, {11, 12}, {13, 16}, {17, 24}, {25, 40}, {41, 72}},
		13: {{1, 1}, {2, 2}, {3, 3}, {4, 4}, {5, 6}, {7, 14}, {15, 16}, {17, 20}, {21, 28}, {29, 44}, {45, 76}, {77, 140}},
		14: {{-2, -2}, {-1, -1}, {0, 0}, {1, 1}, {2, 2}},
		15: {{-24, -9}, {-8, -5}, {-4, -3}, {-2, -2}, {-1, -1}, {0, 0}, {1, 1}, {2, 2}, {3, 4}, {5, 8}, {9, 24}},
	}
	for n := 1; n <= 15; n++ {
		lines := standardTables[n].lines
		if len(lines) != len(printed[n]) {
			t.Errorf("B.%d has %d normal lines, the printed table %d", n, len(lines), len(printed[n]))
			continue
		}
		for i, l := range lines {
			lo, hi := l[0], l[0]+1<<uint(l[2])-1
			if lo != printed[n][i][0] || hi != printed[n][i][1] {
				t.Errorf("B.%d line %d covers %d..%d, printed %d..%d", n, i, lo, hi, printed[n][i][0], printed[n][i][1])
			}
		}
	}
}

// TestHuffTable_RangeLines pins B.4 steps 4 and 5 on the open-ended lines:
// the lower range line counts down from its RANGELOW, the upper counts up.
func TestHuffTable_RangeLines(t *testing.T) {
	t.Parallel()

	table, err := standardTable(3)
	if err != nil {
		t.Fatalf("standardTable: %v", err)
	}
	// B.3: lower 11111111 + 32 bits -> -257 - offset; upper 1111110 + 32
	// bits -> 75 + offset. Offsets 5 and 6.
	bits := "11111111" + fmt.Sprintf("%032b", 5) + "1111110" + fmt.Sprintf("%032b", 6)
	for len(bits)%8 != 0 {
		bits += "0"
	}
	data := make([]byte, len(bits)/8)
	for i := range data {
		v, err := strconv.ParseUint(bits[i*8:i*8+8], 2, 8)
		if err != nil {
			t.Fatalf("building test bits: %v", err)
		}
		data[i] = byte(v)
	}
	r := &bitReader{data: data}
	for _, want := range []int64{-262, 81} {
		if got, err := table.value(r, "range value"); err != nil || got != want {
			t.Errorf("decoded %d (%v), want %d", got, err, want)
		}
	}
}

// TestPickTable_UserTablesInReferralOrder pins 7.4.2.1.6 and 7.4.3.1.6:
// user-supplied tables are matched to the fields that select them in field
// order, taking the referred-to tables segments in the order they are
// referred to, and selecting more than were referred to is malformed.
func TestPickTable_UserTablesInReferralOrder(t *testing.T) {
	t.Parallel()

	first, errA := standardTable(2)
	second, errB := standardTable(3)
	if errA != nil || errB != nil {
		t.Fatalf("standardTable: %v %v", errA, errB)
	}
	user := []*huffTable{first, second}
	next := 0
	for i, want := range []*huffTable{first, second} {
		got, err := pickTable(3, map[int]int{0: 4}, 3, user, &next)
		if err != nil || got != want {
			t.Errorf("user table %d: got %p (%v), want %p", i, got, err, want)
		}
	}
	if _, err := pickTable(3, map[int]int{0: 4}, 3, user, &next); !errors.Is(err, ErrMalformed) {
		t.Errorf("third user selection over two tables: err = %v, want ErrMalformed", err)
	}
}

// TestHuffText_RefinementBlockIsByteAligned pins 6.4.11 steps 5 and 7: after
// a refinement's data size the reader skips to a byte boundary, the block is
// exactly that many bytes, and reading resumes right after it.
func TestHuffText_RefinementBlockIsByteAligned(t *testing.T) {
	t.Parallel()

	rsize, err := standardTable(1)
	if err != nil {
		t.Fatalf("standardTable: %v", err)
	}
	// B.1 "0 0011" codes the size 3 in five bits, which leaves three bits of
	// padding before the byte boundary the block must start on. Reading the
	// block without aligning would start it inside the first byte.
	data := []byte{0b00011_000, 0xAA, 0xBB, 0xCC, 0xDD}
	r := &bitReader{data: data}
	h := &huffText{r: r, rsize: rsize}
	block, err := h.refinementBlock()
	if err != nil {
		t.Fatalf("refinementBlock: %v", err)
	}
	if !bytes.Equal(block, []byte{0xAA, 0xBB, 0xCC}) {
		t.Errorf("block = % X, want AA BB CC", block)
	}
	if r.pos != 4 || r.bit != 0 {
		t.Errorf("reader at byte %d bit %d, want byte 4 bit 0", r.pos, r.bit)
	}
}

// TestParseCodeTable_NegativeRange pins B.2.2 and B.2.3: the lowest and
// highest values are signed. The table here covers -8 to 7 with one line, a
// prefix length of 1 and a range length of 4, and no range lines (HTPS 1,
// HTRS 3, flags 0x20).
func TestParseCodeTable_NegativeRange(t *testing.T) {
	t.Parallel()

	table, err := parseCodeTable([]byte{0x20, 0xFF, 0xFF, 0xFF, 0xF8, 0x00, 0x00, 0x00, 0x08, 0b1_100_0_0_00})
	if err != nil {
		t.Fatalf("parseCodeTable: %v", err)
	}
	// Prefix 0, then offset 0011: -8 + 3.
	if got, err := table.value(&bitReader{data: []byte{0b0_0011_000}}, "value"); err != nil || got != -5 {
		t.Errorf("decoded %d (%v), want -5", got, err)
	}
}
