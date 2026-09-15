package compliance

import (
	"context"
	"testing"
)

// FuzzInlineImageScan feeds arbitrary content-stream bytes to the inline-image
// readers and asserts crash-freedom.
//
// Inline images are the one construct the content parser cannot hand over as
// operations: BI starts a dictionary followed by raw binary sample data, so
// these functions scan the bytes themselves, looking for the ID that ends the
// dictionary and tokenising what precedes it. The bytes are the page's decoded
// content stream, so they are entirely file-controlled, and the scan runs on
// every page of every PDF/A validation.
//
// Measured against the fuzz corpora, all four readers sat at zero coverage.
func FuzzInlineImageScan(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte("BI /W 1 /H 1 /BPC 8 /CS /G /F /AHx ID 00 EI"))
	f.Add([]byte("BI /W 1 /H 1 /F [/A85 /Fl] ID \x00\x01\x02 EI"))
	// BI with no ID at all: the scanner must not run off the end looking.
	f.Add([]byte("BI /W 1 /H 1"))
	// ID immediately at the end, so the sample data is empty.
	f.Add([]byte("BI ID"))
	// A dictionary key that looks like the terminator.
	f.Add([]byte("BI /ID /ID ID x EI"))
	f.Add([]byte("q 1 0 0 1 0 0 cm BT /F1 12 Tf (hi) Tj ET Q"))

	f.Fuzz(func(_ *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		for _, part := range []int{1, 2, 3, 4} {
			_ = checkInlineImageFilters(data, part, 1)
		}
		_ = tokenizeInlineDict(data)
		_ = extractInlineImageFilters(data)
		for _, start := range []int{0, len(data) / 2, len(data)} {
			_ = findInlineImageID(data, start)
		}
	})
}

// FuzzFileStructureBytes feeds arbitrary file bytes to the raw-buffer readers
// in the file-structure checks.
//
// These read the document's own bytes rather than parsed objects: the header
// check judges the first line and the binary comment that must follow it, and
// the stream scanners walk a window of the file looking for where stream data
// begins and whether the declared /Length lands on "endstream". Every byte they
// index comes from the file.
func FuzzFileStructureBytes(f *testing.F) {
	f.Add([]byte{})
	f.Add([]byte("%PDF-1.7\n%\xE2\xE3\xCF\xD3\n"))
	f.Add([]byte("%PDF-2.0\r\n%\x80\x81\x82\x83\r\n1 0 obj\n<< /Length 5 >>\nstream\nhello\nendstream\nendobj\n"))
	f.Add([]byte("%PDF-1.4")) // header with no EOL
	f.Add([]byte("garbage before %PDF-1.5\n"))
	f.Add([]byte("1 0 obj\n<< /Length 99999 >>\nstream\nshort\nendstream\n"))

	f.Fuzz(func(_ *testing.T, data []byte) {
		if len(data) > 1<<20 {
			return
		}
		ctx := context.Background()

		for _, profile := range []string{"PDF/A-1b", "PDF/A-2b", "PDF/A-4", "PDF/UA-1"} {
			_ = checkHeaderBytes(data, profile)
		}
		_ = findStreamDataStart(ctx, data)
		for _, off := range []int{0, 1, len(data) / 2} {
			_ = streamLengthMatches(data, off, 0)
			_ = streamLengthMatches(data, off, len(data))
		}
	})
}

// TestInlineImageSeedIsScanned pins that the inline-image seed reaches the
// dictionary tokeniser instead of being passed over as ordinary operators.
func TestInlineImageSeedIsScanned(t *testing.T) {
	t.Parallel()

	data := []byte("BI /W 1 /H 1 /BPC 8 /CS /G /F /AHx ID 00 EI")

	if id := findInlineImageID(data, 0); id < 0 {
		t.Fatal("the ID terminator was not found in the seed")
	}
	toks := tokenizeInlineDict(data)
	if len(toks) == 0 {
		t.Fatal("the seed dictionary tokenised to nothing")
	}
	filters := extractInlineImageFilters(data)
	if len(filters) == 0 {
		t.Error("the seed's /F entry yielded no filter names")
	}
}
