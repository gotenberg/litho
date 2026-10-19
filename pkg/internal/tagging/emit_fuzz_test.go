package tagging

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// FuzzPageSpans feeds arbitrary content bytes to the span planner and asserts
// crash-freedom.
//
// [pageSpans] decides where the auto-tagger will cut a page's content stream:
// it slices the bytes between the leaves it is given and marks the gaps as
// artifacts. The content is the page's own decoded stream, so it is
// file-controlled, while the leaf offsets come from the classifier that ran
// over it; the two are derived separately, so nothing guarantees the offsets
// still address the bytes. Every slice it takes is therefore a place where a
// stale or crafted offset lands out of range.
//
// The leaves here are derived from the input so they stay in range as the
// fuzzer shrinks the data, which keeps the planner reachable rather than
// rejected; the offsets are still arbitrary relative to the content's meaning.
// Measured against the fuzz corpora, all four readers in emit.go sat at zero.
func FuzzPageSpans(f *testing.F) {
	f.Add([]byte(""), 0)
	f.Add([]byte("BT /F1 12 Tf (hello) Tj ET"), 2)
	f.Add([]byte("q 1 0 0 1 0 0 cm /Fm0 Do Q BT (x) Tj ET"), 3)
	f.Add([]byte("   \t\r\n   "), 1) // all whitespace: every gap is blank
	f.Add([]byte("BT (a) Tj ET BT (b) Tj ET"), 4)

	f.Fuzz(func(_ *testing.T, data []byte, leafCount int) {
		if len(data) > 1<<20 {
			return
		}
		// Between zero and eight leaves, each a slice of the input. Without a
		// bound the fuzzer spends its time on leaf counts rather than content.
		if leafCount < 0 {
			leafCount = -leafCount
		}
		leafCount %= 9

		// Leaves must be well formed and non-descending, which is what the
		// classifier that feeds pageSpans in production guarantees: it walks the
		// page's operations in order, so a leaf never ends before it starts and
		// the next never begins before the last ended. Generating reversed
		// leaves tests a contract no caller can violate, and the span consumer
		// (injectMarks) rejects such input in any case.
		leaves := make([]leafEntry, 0, leafCount)
		cursor := 0
		for i := range leafCount {
			start, end := cursor, cursor
			if len(data) > 0 {
				start = cursor + (i*7)%(len(data)-cursor+1)
				end = start + i%(len(data)-start+1)
				cursor = end
			}
			leaves = append(leaves, leafEntry{
				span: markSpan{start: start, end: end, mcid: i, tag: RoleP},
				ref:  ast.PdfIndirectRef{ObjectNum: i + 1},
			})
		}

		textXObjects := map[string]bool{"Fm0": true, "Fm1": true}
		spans, refs := pageSpans(data, leaves, textXObjects, textObjectTokens(context.Background(), data))
		if len(refs) != len(leaves) {
			panic("pageSpans returned a ref array of the wrong length")
		}

		// Every span it plans must address the content it was given, since the
		// caller slices the bytes with them.
		for _, s := range spans {
			if s.start < 0 || s.end < s.start || s.end > len(data) {
				panic("pageSpans planned a span outside the content")
			}
		}

		_ = showsText(data)
		_ = isBlank(data)
		_ = paintsTextXObject(data, textXObjects)
		_ = paintsTextXObject(data, nil)
	})
}

// TestPageSpansPlansWithinContent pins the property the fuzz target asserts, on
// a fixed input: the planned spans address the content, and one ref comes back
// per leaf. The planner's output is fed straight to a slice expression, so an
// out-of-range span would panic in the caller rather than here.
func TestPageSpansPlansWithinContent(t *testing.T) {
	t.Parallel()

	data := []byte("BT /F1 12 Tf (hello) Tj ET q /Fm0 Do Q")
	leaves := []leafEntry{
		{span: markSpan{start: 0, end: 26, mcid: 0, tag: RoleP}, ref: ast.PdfIndirectRef{ObjectNum: 1}},
	}

	spans, refs := pageSpans(data, leaves, map[string]bool{"Fm0": true}, nil)
	if len(refs) != len(leaves) {
		t.Fatalf("got %d refs for %d leaves", len(refs), len(leaves))
	}
	if len(spans) == 0 {
		t.Fatal("no spans planned for content holding text and an XObject")
	}
	for _, s := range spans {
		if s.start < 0 || s.end < s.start || s.end > len(data) {
			t.Errorf("span [%d:%d] lies outside content of length %d", s.start, s.end, len(data))
		}
	}
}
