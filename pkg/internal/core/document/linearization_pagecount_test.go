package document

import (
	"bytes"
	"context"
	"fmt"
	"testing"
)

// zeroWidthHintHeader builds the 36-byte Page Offset Hint Table header: 13
// fields totalling 288 bits, every bit width zero.
//
// That shape is what reaches the per-page allocation. With zero widths neither
// per-page loop reads a bit, so the truncation flag never fires and the
// file-supplied page count alone decides how much is allocated. It is also why
// the decoded length cannot serve as a bound: a page costs zero bytes here.
func zeroWidthHintHeader() []byte {
	bw := &bitWriter{}
	bw.writeBits(1, 32) // item 1: min objects per page
	bw.writeBits(0, 32) // item 2: location of first page's objects
	bw.writeBits(0, 16) // item 3: bits for object count delta
	bw.writeBits(0, 32) // item 4: min page length
	bw.writeBits(0, 16) // item 5: bits for page length delta
	bw.writeBits(0, 32) // item 6
	bw.writeBits(0, 16) // item 7
	bw.writeBits(0, 32) // item 8
	bw.writeBits(0, 16) // item 9
	bw.writeBits(0, 16) // item 10
	bw.writeBits(0, 16) // item 11
	bw.writeBits(0, 16) // item 12
	bw.writeBits(0, 16) // item 13
	return bw.bytes()
}

// TestParsePageOffsetTable_RejectsImpossiblePageCount pins that the page count
// taken from the linearization dictionary's /N is validated before it sizes an
// allocation.
//
// /N is read straight out of the file, and the table is built with
// make([]PageOffsetEntry, numPages). Measured before this guard: a negative /N
// panicked with "makeslice: len out of range", and /N of 20,000,000 allocated
// 458 MB from a 40-byte hint stream, an amplification of roughly five orders of
// magnitude. A panicking make is itself a denial of service, and the allocation
// is reached from NewDocument before a caller can intervene.
func TestParsePageOffsetTable_RejectsImpossiblePageCount(t *testing.T) {
	t.Parallel()

	data := zeroWidthHintHeader()

	for _, tc := range []struct {
		name string
		n    int
	}{
		{"negative", -1},
		{"absurdly large", 20_000_000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if rec := recover(); rec != nil {
					t.Fatalf("page count %d panicked instead of being refused: %v", tc.n, rec)
				}
			}()
			entries, err := parsePageOffsetTable(data, tc.n)
			if err == nil {
				t.Fatalf("page count %d accepted, returning %d entries: the count is unvalidated",
					tc.n, len(entries))
			}
		})
	}
}

// TestParsePageOffsetTable_OrdinaryPageCountStillParses is the control. A guard
// that refused everything would satisfy the test above while breaking every
// linearized document, so these are counts real files carry.
//
// # Corpus evidence
//
//   - All corpora: the largest page count is 2322, with a median of 3.
func TestParsePageOffsetTable_OrdinaryPageCountStillParses(t *testing.T) {
	t.Parallel()

	data := zeroWidthHintHeader()
	for _, n := range []int{1, 3, 108, 2322} {
		entries, err := parsePageOffsetTable(data, n)
		if err != nil {
			t.Errorf("page count %d is ordinary, but it was refused: %v", n, err)
			continue
		}
		if len(entries) != n {
			t.Errorf("page count %d produced %d entries", n, len(entries))
		}
	}
}

// hintStreamPDF builds a linearized document whose /N is numPages and whose
// hint stream is an uncompressed zero-width header.
func hintStreamPDF(numPages int) []byte {
	hint := zeroWidthHintHeader()

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.4\n")
	linOff := buf.Len()
	fmt.Fprintf(&buf, "1 0 obj\n<< /Linearized 1 /L 0 /O 4 /N %d /E 0 /T 0 /H [%010d %d] >>\nendobj\n",
		numPages, 0, len(hint))
	hintOff := buf.Len()
	fmt.Fprintf(&buf, "2 0 obj\n<< /Length %d >>\nstream\n", len(hint))
	buf.Write(hint)
	buf.WriteString("\nendstream\nendobj\n")
	obj3 := buf.Len()
	buf.WriteString("3 0 obj\n<< /Type /Catalog /Pages 4 0 R >>\nendobj\n")
	obj4 := buf.Len()
	buf.WriteString("4 0 obj\n<< /Type /Pages /Kids [5 0 R] /Count 1 >>\nendobj\n")
	obj5 := buf.Len()
	buf.WriteString("5 0 obj\n<< /Type /Page /Parent 4 0 R /MediaBox [0 0 10 10] >>\nendobj\n")
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 6\n0000000000 65535 f \r\n%010d 00000 n \r\n%010d 00000 n \r\n%010d 00000 n \r\n%010d 00000 n \r\n%010d 00000 n \r\n",
		linOff, hintOff, obj3, obj4, obj5)
	fmt.Fprintf(&buf, "trailer\n<< /Size 6 /Root 3 0 R >>\nstartxref\n%d\n%%%%EOF\n", xref)

	data := buf.Bytes()
	return bytes.Replace(data,
		[]byte(fmt.Sprintf("/H [%010d", 0)),
		[]byte(fmt.Sprintf("/H [%010d", hintOff)), 1)
}

// TestNewDocument_RefusesHintTableLargerThanTheFile pins the natural bound: a
// page cannot occupy fewer bytes than its cross-reference entry, which ISO
// 32000-1, 7.5.4 fixes at exactly 20, so a /N that cannot fit the file is not a
// page count.
//
// The document still opens. NewDocument already discards a hint table whose
// stream fails to parse, so refusing an impossible count degrades to the
// behaviour any malformed hint stream already gets.
func TestNewDocument_RefusesHintTableLargerThanTheFile(t *testing.T) {
	t.Parallel()

	data := hintStreamPDF(20_000_000)
	doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	if got := len(doc.HintTable); got != 0 {
		t.Fatalf("a %d-byte file yielded a %d-entry hint table: /N is unvalidated",
			len(data), got)
	}
}

// TestNewDocument_KeepsAPlausibleHintTable is the control for the bound above:
// a page count the file could actually hold is still decoded.
func TestNewDocument_KeepsAPlausibleHintTable(t *testing.T) {
	t.Parallel()

	data := hintStreamPDF(1)
	doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	if got := len(doc.HintTable); got != 1 {
		t.Fatalf("hint table entries = %d, want 1: the bound rejects a real document", got)
	}
}
