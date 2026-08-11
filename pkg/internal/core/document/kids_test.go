package document

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
)

// pageTreePDF builds a one-page document whose page tree node carries the given
// /Kids spelling, so a test can vary just that entry.
func pageTreePDF(kids string) []byte {
	body := "%PDF-1.4\n"
	offs := map[int]int{}
	add := func(num int, payload string) {
		offs[num] = len(body)
		body += fmt.Sprintf("%d 0 obj\n%s\nendobj\n", num, payload)
	}
	add(1, "<< /Type /Catalog /Pages 2 0 R >>")
	add(2, "<< /Type /Pages /Kids "+kids+" /Count 1 >>")
	add(3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>")

	xrefOffset := len(body)
	var table strings.Builder
	table.WriteString("xref\n0 4\n0000000000 65535 f \n")
	for _, num := range []int{1, 2, 3} {
		fmt.Fprintf(&table, "%010d 00000 n \n", offs[num])
	}

	return []byte(body + table.String() +
		fmt.Sprintf("trailer\n<< /Size 4 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xrefOffset))
}

func pageCount(t *testing.T, data []byte) int {
	t.Helper()
	ctx := context.Background()
	doc, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := ResolvePages(ctx, doc, catalog)
	if err != nil {
		t.Fatalf("ResolvePages: %v", err)
	}
	return len(pages)
}

// TestKidsAsSingleReference checks a page tree survives a producer that writes
// /Kids as a lone reference instead of an array.
//
// ISO 32000-1, 7.7.3.2 requires the array, so the file is non-conforming, but
// the page is plainly there and poppler reads it. litho used to answer zero
// pages: the recovery meant to tolerate this sat behind a helper that reports a
// non-array as an error, so it never ran.
func TestKidsAsSingleReference(t *testing.T) {
	if got := pageCount(t, pageTreePDF("3 0 R")); got != 1 {
		t.Errorf("page count with /Kids as a single reference = %d, want 1", got)
	}
}

// TestKidsAsDirectDictionary checks the same tolerance for a kid written inline
// rather than through a reference.
func TestKidsAsDirectDictionary(t *testing.T) {
	data := pageTreePDF("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>")
	if got := pageCount(t, data); got != 1 {
		t.Errorf("page count with an inline /Kids dictionary = %d, want 1", got)
	}
}

// TestKidsAsArrayUnaffected checks the ordinary spelling still works, so the
// tolerance did not displace the conforming path.
func TestKidsAsArrayUnaffected(t *testing.T) {
	if got := pageCount(t, pageTreePDF("[3 0 R]")); got != 1 {
		t.Errorf("page count with a conforming /Kids array = %d, want 1", got)
	}
}

// TestKidsEmptyArrayIsZeroPages checks an empty array still means zero kids
// rather than an error, which is what a legitimately empty node says.
func TestKidsEmptyArrayIsZeroPages(t *testing.T) {
	if got := pageCount(t, pageTreePDF("[]")); got != 0 {
		t.Errorf("page count with an empty /Kids array = %d, want 0", got)
	}
}
