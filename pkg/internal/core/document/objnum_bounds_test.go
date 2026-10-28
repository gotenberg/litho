package document

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
)

// mislabelledSubsectionPDF builds a three-object PDF whose second xref
// subsection header claims to start at 4294967296. Objects 2 and 3 are in the
// file but the header files them under numbers past 2^32.
//
// # Corpus evidence
//
//   - pdfjs GHOSTSCRIPT-698804-1-fuzzed.pdf: the shape this fixture
//     reproduces.
func mislabelledSubsectionPDF(trailerSize int64) []byte {
	body := "%PDF-1.4\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\n"

	off := func(header string) int { return strings.Index(body, header) }
	xrefOffset := len(body)

	table := fmt.Sprintf("xref\n0 2\n0000000000 65535 f \n%010d 00000 n \n", off("1 0 obj")) +
		fmt.Sprintf("4294967296 2\n%010d 00000 n \n%010d 00000 n \n", off("2 0 obj"), off("3 0 obj"))
	trailer := fmt.Sprintf("trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n",
		trailerSize, xrefOffset)

	return []byte(body + table + trailer)
}

// TestPruneUnrepresentableEntries checks that an object number the file is far
// too small to define never reaches the index, because every later write takes
// its next object number from the highest one there.
func TestPruneUnrepresentableEntries(t *testing.T) {
	data := mislabelledSubsectionPDF(4)
	doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	for objNum := range doc.xrefEntries {
		if int64(objNum) > int64(len(data)) {
			t.Errorf("index kept object %d in a %d-byte file", objNum, len(data))
		}
	}
}

// TestHighestObjectNumberUsesTrailerSize checks that an object left out of the
// index still reserves its number, so an incremental update cannot append a
// second definition that shadows it.
func TestHighestObjectNumberUsesTrailerSize(t *testing.T) {
	data := mislabelledSubsectionPDF(4)
	doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	// The mislabelled entries are gone, so the index alone stops at object 1
	// while the file defines 3. Allocating 2 or 3 would shadow real bytes.
	if got := doc.HighestObjectNumber(context.Background()); got != 3 {
		t.Errorf("HighestObjectNumber() = %d, want 3 (trailer /Size 4 covers the objects the index missed)", got)
	}
}

// TestHighestObjectNumberIgnoresImpossibleSize checks that /Size is bounded like
// any other file-derived count: trusting a fuzzed one would put the allocator
// right back where the pruning left off.
func TestHighestObjectNumberIgnoresImpossibleSize(t *testing.T) {
	data := mislabelledSubsectionPDF(4294967300)
	doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	got := doc.HighestObjectNumber(context.Background())
	if int64(got) > int64(len(data)) {
		t.Errorf("HighestObjectNumber() = %d in a %d-byte file, want a number the file could hold", got, len(data))
	}
}

// TestHighestObjectNumberIntactFile checks the ordinary case is untouched: a
// file whose xref names every object answers from the index.
func TestHighestObjectNumberIntactFile(t *testing.T) {
	data := minimalPDF()
	doc, err := NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}

	if got := doc.HighestObjectNumber(context.Background()); got != 2 {
		t.Errorf("HighestObjectNumber() = %d, want 2", got)
	}
}
