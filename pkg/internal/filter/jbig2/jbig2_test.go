package jbig2

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"slices"
	"testing"
)

// annexH1 returns the raw segments of the example datastream of ITU-T T.88,
// Annex H.1, stored in testdata with its 13-byte file header (D.4).
//
// Its pages 1 and 2 carry the same generic region, coded once with MMR
// (segment 4) and once with template 0 arithmetic coding and TPGD (segment
// 11), which is what lets the two decodes check each other.
func annexH1(t *testing.T) [][]byte {
	t.Helper()
	b, err := os.ReadFile("testdata/t88_annex_h1.jb2")
	if err != nil {
		t.Fatalf("reading Annex H.1 datastream: %v", err)
	}
	var raw [][]byte
	for off := 13; off < len(b); {
		_, n, err := parseSegment(b[off:])
		if err != nil {
			t.Fatalf("Annex H.1 segment at offset %d: %v", off, err)
		}
		raw = append(raw, b[off:off+n])
		off += n
	}
	if len(raw) != 21 {
		t.Fatalf("Annex H.1 holds %d segments, want 21", len(raw))
	}
	return raw
}

// annexH1Golden returns the 64x56 page that Annex H.1's generic region draws,
// as decoded by jbig2dec 0.20 and stored as a binary PBM, whose rows have the
// same layout as [Bitmap.Data].
func annexH1Golden(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/t88_annex_h1_generic.pbm")
	if err != nil {
		t.Fatalf("reading golden: %v", err)
	}
	header := []byte("P4\n64 56\n")
	if !bytes.HasPrefix(b, header) || len(b) != len(header)+8*56 {
		t.Fatalf("golden is not a 64x56 binary PBM")
	}
	return b[len(header):]
}

func concat(parts ...[]byte) []byte {
	return slices.Concat(parts...)
}

// TestParseSegment_T88Examples pins the segment header parser against the two
// worked examples of ITU-T T.88, 7.2.8. The examples stop before the data
// length field, so a zero length is appended to each.
func TestParseSegment_T88Examples(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		header   []byte
		number   uint32
		kind     int
		page     uint32
		referred []uint32
	}{
		{
			name:     "example 1, short form",
			header:   []byte{0x00, 0x00, 0x00, 0x20, 0x86, 0x6B, 0x02, 0x1E, 0x05, 0x04},
			number:   32,
			kind:     6,
			page:     4,
			referred: []uint32{2, 30, 5},
		},
		{
			name: "example 2, long form",
			header: []byte{
				0x00, 0x00, 0x02, 0x34, 0x40, 0xE0, 0x00, 0x00, 0x09, 0x02, 0xFD, 0x01, 0x00, 0x00, 0x02, 0x00,
				0x1E, 0x00, 0x05, 0x02, 0x00, 0x02, 0x01, 0x02, 0x02, 0x02, 0x03, 0x02, 0x04, 0x00, 0x00, 0x04,
				0x01,
			},
			number:   564,
			kind:     0,
			page:     1025,
			referred: []uint32{256, 2, 30, 5, 512, 513, 514, 515, 516},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			seg, n, err := parseSegment(concat(tc.header, []byte{0, 0, 0, 0}))
			if err != nil {
				t.Fatalf("parseSegment: %v", err)
			}
			if n != len(tc.header)+4 {
				t.Errorf("consumed %d bytes, want %d", n, len(tc.header)+4)
			}
			if seg.number != tc.number || seg.kind != tc.kind || seg.page != tc.page {
				t.Errorf("number/type/page = %d/%d/%d, want %d/%d/%d",
					seg.number, seg.kind, seg.page, tc.number, tc.kind, tc.page)
			}
			if !slices.Equal(seg.referred, tc.referred) {
				t.Errorf("referred-to segments = %v, want %v", seg.referred, tc.referred)
			}
		})
	}
}

// TestDecode_AnnexH1GenericRegion decodes the generic region of Annex H.1 in
// both codings and checks each against the golden and against the other.
func TestDecode_AnnexH1GenericRegion(t *testing.T) {
	t.Parallel()

	raw := annexH1(t)
	golden := annexH1Golden(t)
	for _, tc := range []struct {
		name          string
		page, generic int
	}{
		{"page 1, MMR", 1, 4},
		{"page 2, template 0 with TPGD", 8, 11},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			bm, err := Decode(context.Background(), concat(raw[tc.page], raw[tc.generic]), nil)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if bm.Width != 64 || bm.Height != 56 {
				t.Fatalf("page is %dx%d, want 64x56", bm.Width, bm.Height)
			}
			if !bytes.Equal(bm.Data, golden) {
				t.Errorf("page differs from the golden in %d bytes", differingBytes(bm.Data, golden))
			}
		})
	}
}

func differingBytes(a, b []byte) int {
	n := 0
	for i := range min(len(a), len(b)) {
		if a[i] != b[i] {
			n++
		}
	}
	return n + max(len(a), len(b)) - min(len(a), len(b))
}

// withUnknownLength rewrites an immediate generic region segment to carry the
// data length 0xFFFFFFFF of T.88, 7.2.7 and appends the trailing row count.
func withUnknownLength(t *testing.T, seg []byte, rows uint32) []byte {
	t.Helper()
	parsed, n, err := parseSegment(seg)
	if err != nil || n != len(seg) {
		t.Fatalf("parseSegment: %v", err)
	}
	header := slices.Clone(seg[:len(seg)-len(parsed.data)])
	header[4] = header[4]&^0x3F | typeImmediateGeneric
	binary.BigEndian.PutUint32(header[len(header)-4:], unknownDataLength)
	return concat(header, parsed.data, binary.BigEndian.AppendUint32(nil, rows))
}

// TestDecode_UnknownLengthGenericRegion pins 7.2.7 and 7.4.6.4: a region whose
// length is unknown ends at 0xFF 0xAC and a row count, and holds only that
// many rows.
func TestDecode_UnknownLengthGenericRegion(t *testing.T) {
	t.Parallel()

	raw := annexH1(t)
	golden := annexH1Golden(t)

	full := concat(raw[8], withUnknownLength(t, raw[11], 44))
	bm, err := Decode(context.Background(), full, nil)
	if err != nil {
		t.Fatalf("Decode with every row: %v", err)
	}
	if !bytes.Equal(bm.Data, golden) {
		t.Errorf("unknown-length region differs from the golden in %d bytes", differingBytes(bm.Data, golden))
	}

	// With 20 rows the region stops at page row 11+20; below it the page
	// keeps its default pixel value 0.
	short := concat(raw[8], withUnknownLength(t, raw[11], 20))
	bm, err = Decode(context.Background(), short, nil)
	if err != nil {
		t.Fatalf("Decode with 20 rows: %v", err)
	}
	want := slices.Clone(golden)
	clear(want[31*8:])
	if !bytes.Equal(bm.Data, want) {
		t.Errorf("20-row region differs from the golden's first 20 rows in %d bytes", differingBytes(bm.Data, want))
	}

	// A row count above the stated height is malformed.
	if _, err := Decode(context.Background(), concat(raw[8], withUnknownLength(t, raw[11], 45)), nil); !errors.Is(err, ErrMalformed) {
		t.Errorf("row count 45 over a 44-row region: err = %v, want ErrMalformed", err)
	}
}

// segmentBytes builds a segment with a one-byte page association and no
// referred-to segments.
func segmentBytes(number uint32, kind int, page uint8, data []byte) []byte {
	h := binary.BigEndian.AppendUint32(nil, number)
	h = append(h, byte(kind), 0x00, page)
	h = binary.BigEndian.AppendUint32(h, uint32(len(data)))
	return concat(h, data)
}

// TestDecode_StripedPageOfUnknownHeight pins 7.4.8.2 and 7.4.9: a page whose
// height is 0xFFFFFFFF takes its height from its last end of stripe segment.
func TestDecode_StripedPageOfUnknownHeight(t *testing.T) {
	t.Parallel()

	raw := annexH1(t)
	golden := annexH1Golden(t)

	pageInfo := slices.Clone(raw[8])
	parsed, _, err := parseSegment(pageInfo)
	if err != nil {
		t.Fatalf("parseSegment: %v", err)
	}
	dataAt := len(pageInfo) - len(parsed.data)
	binary.BigEndian.PutUint32(pageInfo[dataAt+4:], 0xFFFFFFFF)
	// Striped, maximum stripe size 56, as 7.4.8.6 requires of an unknown
	// height.
	binary.BigEndian.PutUint16(pageInfo[dataAt+17:], 0x8000|56)

	stream := concat(pageInfo, raw[11], segmentBytes(12, typeEndOfStripe, 2, binary.BigEndian.AppendUint32(nil, 55)))
	bm, err := Decode(context.Background(), stream, nil)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if bm.Width != 64 || bm.Height != 56 {
		t.Fatalf("page is %dx%d, want 64x56 from the end row 55", bm.Width, bm.Height)
	}
	if !bytes.Equal(bm.Data, golden) {
		t.Errorf("striped page differs from the golden in %d bytes", differingBytes(bm.Data, golden))
	}
}

// TestDecode_Refusals pins what is refused and with which sentinel.
func TestDecode_Refusals(t *testing.T) {
	t.Parallel()

	pageInfo := func(width, height uint32) []byte {
		d := binary.BigEndian.AppendUint32(nil, width)
		d = binary.BigEndian.AppendUint32(d, height)
		d = append(d, make([]byte, 11)...)
		return segmentBytes(0, typePageInformation, 1, d)
	}
	for _, tc := range []struct {
		name string
		data []byte
		want error
	}{
		{"no page information", segmentBytes(0, typeEndOfPage, 1, nil), ErrMalformed},
		{"page larger than the pixel cap", pageInfo(0xFFFFFFFF, 0xFFFFFFFE), ErrLimit},
		{"reserved segment type", concat(pageInfo(8, 8), segmentBytes(1, 1, 1, nil)), ErrMalformed},
		{"truncated pattern dictionary", concat(pageInfo(8, 8), segmentBytes(1, typePatternDictionary, 1, nil)), ErrMalformed},
		{"halftone region with no pattern dictionary", concat(pageInfo(8, 8), segmentBytes(1, typeImmediateHalftone, 1, make([]byte, 40))), ErrMalformed},
		{"user Huffman table with no tables segment", concat(pageInfo(8, 8), segmentBytes(1, typeSymbolDictionary, 1, []byte{0, 0x0D, 0, 0, 0, 1, 0, 0, 0, 1})), ErrMalformed},
		{"truncated symbol dictionary", concat(pageInfo(8, 8), segmentBytes(1, typeSymbolDictionary, 1, nil)), ErrMalformed},
		{"necessary extension", concat(pageInfo(8, 8), segmentBytes(1, typeExtension, 1, []byte{0xA0, 0, 0, 1})), ErrUnsupported},
		{"data length past the end", concat(pageInfo(8, 8), []byte{0, 0, 0, 1, typeEndOfPage, 0, 1, 0, 0, 0, 9}), ErrMalformed},
		{"referred-to count 5", []byte{0, 0, 0, 1, typeEndOfPage, 0xA0, 1, 0, 0, 0, 0}, ErrMalformed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if _, err := Decode(context.Background(), tc.data, nil); !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// TestDecode_ChargeBoundsTotalArea pins maxDecodedPixels: regions are charged
// before they are decoded, so a stream of many full-size regions is refused
// without decoding them.
func TestDecode_ChargeBoundsTotalArea(t *testing.T) {
	t.Parallel()

	b := &budget{}
	for i := range 4 {
		if err := b.charge(10_000, 10_000); err != nil {
			t.Fatalf("region %d of a page-sized area refused: %v", i+1, err)
		}
	}
	if err := b.charge(1, 1); !errors.Is(err, ErrLimit) {
		t.Errorf("pixel past four page-sized areas: err = %v, want ErrLimit", err)
	}
	if err := (&budget{}).charge(0xFFFFFFFF, 0xFFFFFFFF); !errors.Is(err, ErrLimit) {
		t.Errorf("2^32-1 square region: err = %v, want ErrLimit", err)
	}
}

// TestBitmapCombine pins the five combination operators of T.88, 7.4.1.5 on
// every pairing of page and region pixel.
func TestBitmapCombine(t *testing.T) {
	t.Parallel()

	// Page pixels 0 0 1 1, region pixels 0 1 0 1: each column is one of the
	// four pairings.
	for _, tc := range []struct {
		op   int
		want [4]uint32
	}{
		{combineOr, [4]uint32{0, 1, 1, 1}},
		{combineAnd, [4]uint32{0, 0, 0, 1}},
		{combineXor, [4]uint32{0, 1, 1, 0}},
		{combineXnor, [4]uint32{1, 0, 0, 1}},
		{combineReplace, [4]uint32{0, 1, 0, 1}},
	} {
		t.Run(fmt.Sprint(tc.op), func(t *testing.T) {
			t.Parallel()

			page, _ := newBitmap(4, 1)
			region, _ := newBitmap(4, 1)
			page.setPixel(2, 0, 1)
			page.setPixel(3, 0, 1)
			region.setPixel(1, 0, 1)
			region.setPixel(3, 0, 1)
			if err := page.combine(region, 0, 0, tc.op); err != nil {
				t.Fatalf("combine: %v", err)
			}
			for x, want := range tc.want {
				if got := page.pixel(x, 0); got != want {
					t.Errorf("pixel %d = %d, want %d", x, got, want)
				}
			}
		})
	}
}
