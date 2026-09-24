package document

import (
	"testing"
)

func TestBitReader_ReadBits(t *testing.T) {
	// 0xAB = 10101011, 0xCD = 11001101
	data := []byte{0xAB, 0xCD}
	br := newBitReader(data)

	// Read 4 bits: 1010 = 10
	got := br.readBits(4)
	if got != 10 {
		t.Errorf("readBits(4): got %d, want 10", got)
	}

	// Read 8 bits: 1011_1100 = 0xBC = 188
	got = br.readBits(8)
	if got != 0xBC {
		t.Errorf("readBits(8): got %d, want %d", got, 0xBC)
	}

	// Read 4 bits: 1101 = 13
	got = br.readBits(4)
	if got != 13 {
		t.Errorf("readBits(4): got %d, want 13", got)
	}
}

func TestBitReader_AlignToByte(t *testing.T) {
	data := []byte{0xFF, 0x00}
	br := newBitReader(data)

	// Read 3 bits.
	br.readBits(3)

	// Align to byte boundary. Should skip remaining 5 bits of byte 0.
	br.alignToByte()

	// Next 8 bits should be 0x00.
	got := br.readBits(8)
	if got != 0 {
		t.Errorf("after align, readBits(8): got %d, want 0", got)
	}
}

func TestBitReader_AlignNoop(t *testing.T) {
	data := []byte{0xFF, 0xAA}
	br := newBitReader(data)

	// Read exactly 8 bits, already aligned.
	br.readBits(8)
	br.alignToByte() // should be a no-op

	got := br.readBits(8)
	if got != 0xAA {
		t.Errorf("after noop align, readBits(8): got %d, want %d", got, 0xAA)
	}
}

func TestBitReader_PastEnd(t *testing.T) {
	data := []byte{0xFF}
	br := newBitReader(data)

	// Read 8 bits to exhaust data.
	br.readBits(8)

	// Reading past end should return 0.
	got := br.readBits(8)
	if got != 0 {
		t.Errorf("past end readBits(8): got %d, want 0", got)
	}
}

func TestParsePageOffsetTable(t *testing.T) {
	// Build a minimal hint table header + 2-page data.
	// Header: 13 fields, set as:
	//   minObjsPerPage = 5
	//   location = 0
	//   bitsObjCountDelta = 3
	//   minPageLength = 1000
	//   bitsPageLenDelta = 4
	//   remaining fields = 0
	//
	// Then arrays:
	//   obj count deltas: page0=2 (010), page1=7 (111) → 0101_11xx
	//   page length deltas, 4 bits each: page0=5 (0101), page1=10 (1010) = 0101_1010

	// Build header manually with a bitWriter helper.
	bw := &bitWriter{}
	bw.writeBits(5, 32)    // minObjsPerPage
	bw.writeBits(0, 32)    // location
	bw.writeBits(3, 16)    // bitsObjCountDelta
	bw.writeBits(1000, 32) // minPageLength
	bw.writeBits(4, 16)    // bitsPageLenDelta
	bw.writeBits(0, 32)    // item 6
	bw.writeBits(0, 16)    // item 7
	bw.writeBits(0, 32)    // item 8
	bw.writeBits(0, 16)    // item 9
	bw.writeBits(0, 16)    // item 10
	bw.writeBits(0, 16)    // item 11
	bw.writeBits(0, 16)    // item 12
	bw.writeBits(0, 16)    // item 13

	// Array 1: obj count deltas (3 bits each, 2 pages)
	bw.writeBits(2, 3) // page 0: delta=2
	bw.writeBits(7, 3) // page 1: delta=7
	bw.alignToByte()

	// Array 2: page length deltas (4 bits each, 2 pages)
	bw.writeBits(5, 4)  // page 0: delta=5
	bw.writeBits(10, 4) // page 1: delta=10

	data := bw.bytes()

	entries, err := parsePageOffsetTable(data, 2)
	if err != nil {
		t.Fatalf("parsePageOffsetTable: %v", err)
	}

	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	// Page 0: objCount = 5 + 2 = 7
	if entries[0].ObjCount != 7 {
		t.Errorf("page 0 ObjCount: got %d, want 7", entries[0].ObjCount)
	}
	// Page 1: objCount = 5 + 7 = 12
	if entries[1].ObjCount != 12 {
		t.Errorf("page 1 ObjCount: got %d, want 12", entries[1].ObjCount)
	}

	// Page 0: pageLength = 1000 + 5 = 1005
	if entries[0].PageLength != 1005 {
		t.Errorf("page 0 PageLength: got %d, want 1005", entries[0].PageLength)
	}
	// Page 1: pageLength = 1000 + 10 = 1010
	if entries[1].PageLength != 1010 {
		t.Errorf("page 1 PageLength: got %d, want 1010", entries[1].PageLength)
	}
}

func TestParsePageOffsetTable_ZeroBitDeltas(t *testing.T) {
	bw := &bitWriter{}
	bw.writeBits(3, 32)   // minObjsPerPage
	bw.writeBits(0, 32)   // location
	bw.writeBits(0, 16)   // bitsObjCountDelta = 0 (all pages have same obj count)
	bw.writeBits(500, 32) // minPageLength
	bw.writeBits(0, 16)   // bitsPageLenDelta = 0
	bw.writeBits(0, 32)   // item 6
	bw.writeBits(0, 16)   // item 7
	bw.writeBits(0, 32)   // item 8
	bw.writeBits(0, 16)   // item 9
	bw.writeBits(0, 16)   // item 10
	bw.writeBits(0, 16)   // item 11
	bw.writeBits(0, 16)   // item 12
	bw.writeBits(0, 16)   // item 13

	// No arrays needed since bit widths are 0.

	data := bw.bytes()

	entries, err := parsePageOffsetTable(data, 3)
	if err != nil {
		t.Fatalf("parsePageOffsetTable: %v", err)
	}

	for i, e := range entries {
		if e.ObjCount != 3 {
			t.Errorf("page %d ObjCount: got %d, want 3", i, e.ObjCount)
		}
		if e.PageLength != 500 {
			t.Errorf("page %d PageLength: got %d, want 500", i, e.PageLength)
		}
	}
}

func TestParsePageOffsetTable_TooShort(t *testing.T) {
	_, err := parsePageOffsetTable([]byte{0, 1, 2}, 1)
	if err == nil {
		t.Error("expected error for short data, got nil")
	}
}

func TestProbeLinearization_NonLinearized(t *testing.T) {
	// A minimal non-linearized PDF header.
	data := []byte("%PDF-1.4\n1 0 obj\n<< /Type /Catalog >>\nendobj\n")
	r := newTestReaderAt(data)

	params := probeLinearization(r, int64(len(data)), 0)
	if params != nil {
		t.Error("expected nil for non-linearized PDF")
	}
}

func TestProbeLinearization_Linearized(t *testing.T) {
	// A minimal linearized PDF first object. /L must match the reported
	// file size for probeLinearization to accept it.
	data := []byte("%PDF-1.4\n1 0 obj\n<< /Linearized 1 /L 54321 /H [123 456] /O 4 /N 10 /E 9876 /T 50000 >>\nendobj\n")
	r := newTestReaderAt(data)

	// Pass the /L value as the file size so the validation accepts it.
	params := probeLinearization(r, 54321, 0)
	if params == nil {
		t.Fatal("expected linearization params, got nil")
	}

	if params.FileLength != 54321 {
		t.Errorf("FileLength: got %d, want 54321", params.FileLength)
	}
	if params.HintStreamOffset != 123 {
		t.Errorf("HintStreamOffset: got %d, want 123", params.HintStreamOffset)
	}
	if params.HintStreamLength != 456 {
		t.Errorf("HintStreamLength: got %d, want 456", params.HintStreamLength)
	}
	if params.FirstPageObjNum != 4 {
		t.Errorf("FirstPageObjNum: got %d, want 4", params.FirstPageObjNum)
	}
	if params.NumPages != 10 {
		t.Errorf("NumPages: got %d, want 10", params.NumPages)
	}
	if params.FirstPageEndOffset != 9876 {
		t.Errorf("FirstPageEndOffset: got %d, want 9876", params.FirstPageEndOffset)
	}
	if params.FirstPageXRefOffset != 50000 {
		t.Errorf("FirstPageXRefOffset: got %d, want 50000", params.FirstPageXRefOffset)
	}
}

// testReaderAt wraps a byte slice as an io.ReaderAt.
type testReaderAt struct {
	data []byte
}

func newTestReaderAt(data []byte) *testReaderAt {
	return &testReaderAt{data: data}
}

func (r *testReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if int(off) >= len(r.data) {
		return 0, nil
	}
	n := copy(p, r.data[off:])
	return n, nil
}

// bitWriter builds bit-packed data for tests.
type bitWriter struct {
	data []byte
	bit  int // bit position within current byte (0=MSB)
}

func (bw *bitWriter) writeBits(val uint64, n int) {
	for i := n - 1; i >= 0; i-- {
		if bw.bit == 0 {
			bw.data = append(bw.data, 0)
		}
		if (val>>i)&1 == 1 {
			bw.data[len(bw.data)-1] |= 1 << (7 - bw.bit)
		}
		bw.bit++
		if bw.bit > 7 {
			bw.bit = 0
		}
	}
}

func (bw *bitWriter) alignToByte() {
	if bw.bit > 0 {
		bw.bit = 0
	}
}

func (bw *bitWriter) bytes() []byte {
	return bw.data
}
