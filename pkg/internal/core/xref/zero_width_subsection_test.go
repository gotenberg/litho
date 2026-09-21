package xref

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestParseXRefStream_ZeroWidthSubsectionWalksNothing pins that a /W of three
// zero widths walks no entries, however large the /Index count.
//
// A zero width leaves its field absent and the decoder supplies a default, so
// /W [0 0 0] describes entries that all come out as type 1 at offset 0, and
// offset 0 can never locate an object. entrySize is then 0, which disables the
// walk's only exit: offset never advances, so offset+entrySize > len(data)
// never becomes true and the loop runs for the file-supplied count.
//
// Measured before this guard: an EMPTY stream with /Index [0 5000000] built
// 5,000,000 entries in 573ms, roughly 114ns each, from a 43-byte dictionary.
// At the 2147483647 a file may state that extrapolates past 100 GB, and the map
// it fills is the document's live index, reached from pdf.Open.
func TestParseXRefStream_ZeroWidthSubsectionWalksNothing(t *testing.T) {
	t.Parallel()

	stream := malformedStream(0, 0, 0,
		ast.PdfArray{ast.PdfInt(0), ast.PdfInt(5_000_000)}, []byte{})
	entries := make(map[int]XRefEntry)

	// Refusing would be wrong: zero widths are legal per Table 17, and
	// TestParseXRefStream_MalformedDictionaryIsRefusedNotPanicked already pins
	// that such a stream is accepted. The subsection is skipped, not rejected.
	if _, err := ParseXRefStream(context.Background(), stream, entries); err != nil {
		t.Fatalf("refused the stream instead of walking no entries: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("entries = %d, want 0: a zero-width subsection is still walked", len(entries))
	}
}

// TestParseXRefStream_RealWidthsStillPopulate is the control. A guard that
// skipped every subsection would satisfy the test above while emptying the
// cross-reference table of every real document.
func TestParseXRefStream_RealWidthsStillPopulate(t *testing.T) {
	t.Parallel()

	// One type-1 entry under /W [1 2 1]: type 1, offset 0x0A, generation 0.
	body := []byte{0x01, 0x00, 0x0A, 0x00}
	stream := malformedStream(1, 2, 1, ast.PdfArray{ast.PdfInt(0), ast.PdfInt(1)}, body)
	entries := make(map[int]XRefEntry)

	if _, err := ParseXRefStream(context.Background(), stream, entries); err != nil {
		t.Fatalf("refused a well-formed stream: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1: the guard skipped a real subsection", len(entries))
	}
	got := entries[0]
	if got.Type != XRefTypeNormal || got.Offset != 0x0A {
		t.Errorf("entry 0 = %+v, want type %d offset 10", got, XRefTypeNormal)
	}
}

// TestParseXRefStream_PartlyZeroWidthsStillWalk pins the edge of the guard: it
// fires only when ALL THREE widths are zero. A single zero width is ordinary
// (that field takes its default) and the entry still consumes bytes, so the
// data-length check keeps bounding the walk.
func TestParseXRefStream_PartlyZeroWidthsStillWalk(t *testing.T) {
	t.Parallel()

	// /W [0 2 1]: type defaults to 1, offset 0x0A, generation 0.
	body := []byte{0x00, 0x0A, 0x00}
	stream := malformedStream(0, 2, 1, ast.PdfArray{ast.PdfInt(0), ast.PdfInt(1)}, body)
	entries := make(map[int]XRefEntry)

	if _, err := ParseXRefStream(context.Background(), stream, entries); err != nil {
		t.Fatalf("refused a stream with one zero width: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries = %d, want 1: the guard fired on a partly zero /W", len(entries))
	}
}
