package document

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/xref"
)

// formatViolationPDF builds a file whose objects each carry the given body,
// written as "N 0 obj\n<body>endobj\n" so that endobj is preceded by the last
// byte of the body. A body that does not end with an EOL is an §6.1.8/§6.1.9
// defect and makes validateObjectHeaderFormat report it.
func formatViolationPDF(t *testing.T, bodies ...string) ([]byte, []int64) {
	t.Helper()
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offsets := make([]int64, len(bodies)+1) // 1-based
	for i, body := range bodies {
		offsets[i+1] = int64(buf.Len())
		fmt.Fprintf(&buf, "%d 0 obj\n%sendobj\n", i+1, body)
	}
	return buf.Bytes(), offsets
}

// formatViolationDoc builds a Document over data with a hand-written index, so
// no construction-time resolution warms the cache or spends the augmentation
// sync.Once before the test runs.
func formatViolationDoc(data []byte, offsets []int64) *Document {
	entries := make(map[int]xref.XRefEntry, len(offsets)-1)
	for objNum := 1; objNum < len(offsets); objNum++ {
		entries[objNum] = xref.XRefEntry{Type: xref.XRefTypeNormal, Offset: offsets[objNum]}
	}
	return &Document{
		r:           bytes.NewReader(data),
		size:        int64(len(data)),
		xrefEntries: entries,
		Index: xref.Index{
			Trailer: ast.PdfDict{"Size": ast.PdfInt(len(offsets)), "Root": ast.PdfIndirectRef{ObjectNum: 1}},
		},
		objectCache:  make(map[int]ast.PdfObject),
		fontCache:    make(map[int]any),
		cmapCache:    make(map[int]any),
		maxCacheSize: 50_000,
		Logger:       NopLogger(),
	}
}

// TestRecordFormatViolations_UnparseableObjectRecordedOnce pins that an object
// whose body does not parse contributes its format violations once however
// often it is resolved. A failed parse is never cached, so every resolution
// re-reads the header, and appending per parse made the reported count a
// function of how many times some check happened to ask for the object.
//
// # Corpus evidence
//
//   - pdfjs issue1045.pdf: object 56's body runs into the endobj keyword with
//     no EOL between them, the shape the fixture's second object reproduces.
func TestRecordFormatViolations_UnparseableObjectRecordedOnce(t *testing.T) {
	t.Parallel()
	// The body runs into the keyword with no EOL between them, and "@@@endobj"
	// lexes as one keyword the parser rejects. A bare number in that shape
	// ("20184endobj") no longer serves here: lexer.splitNumberEndobj now reads
	// it as the number the producer meant, which is what makes the /Length of
	// a stream written that way resolvable.
	data, offsets := formatViolationPDF(t, "<< /Type /Catalog >>\n", "@@@")
	doc := formatViolationDoc(data, offsets)
	doc.SetComplianceMode(true)

	ctx := context.Background()
	if _, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: 2}); err == nil {
		t.Fatal("object 2 parsed; the fixture must be an object the parser rejects")
	}
	for range 8 {
		_, _ = doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: 2}) // Re-resolving a failed parse must not re-record.
	}

	got := doc.FormatViolations()
	if len(got) != 1 {
		t.Fatalf("recorded %d violations after 9 resolutions, want 1: %v", len(got), got)
	}
	if !strings.Contains(got[0].Error(), "endobj not preceded by EOL") {
		t.Errorf("violation = %q, want the endobj EOL defect", got[0])
	}
}

// TestRecordFormatViolations_SurvivesCacheEviction pins that re-parsing an
// object after the object cache was evicted does not record its violations a
// second time. The resolver drops the whole cache once it exceeds
// maxCacheSize, so on a large file any object can be parsed repeatedly.
func TestRecordFormatViolations_SurvivesCacheEviction(t *testing.T) {
	t.Parallel()
	data, offsets := formatViolationPDF(t,
		"<< /Type /Catalog >>\n",
		"<< /A 1 >>", // no EOL before endobj: one violation
		"<< /B 2 >>\n",
		"<< /C 3 >>\n",
	)
	doc := formatViolationDoc(data, offsets)
	doc.maxCacheSize = 1 // Evict on every insertion.
	doc.SetComplianceMode(true)

	ctx := context.Background()
	for range 5 {
		for objNum := 1; objNum <= 4; objNum++ {
			if _, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: objNum}); err != nil {
				t.Fatalf("resolve object %d: %v", objNum, err)
			}
		}
	}

	got := doc.FormatViolations()
	if len(got) != 1 {
		t.Fatalf("recorded %d violations across 20 resolutions with eviction, want 1: %v", len(got), got)
	}
}

// TestRecordFormatViolations_ConcurrentResolveRecordsOnce pins that goroutines
// that all miss the object cache for the same object record its violations
// once between them. Both goroutines parse, and appending per parse counted
// the object twice.
func TestRecordFormatViolations_ConcurrentResolveRecordsOnce(t *testing.T) {
	t.Parallel()
	data, offsets := formatViolationPDF(t, "<< /Type /Catalog >>\n", "20184", "<< /A 1 >>")
	doc := formatViolationDoc(data, offsets)
	doc.SetComplianceMode(true)

	ctx := context.Background()
	const goroutines = 16
	var wg sync.WaitGroup
	for range goroutines {
		wg.Go(func() {
			for objNum := 1; objNum <= 3; objNum++ {
				_, _ = doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: objNum}) // Object 2 never parses; resolve errors are the point.
			}
		})
	}
	wg.Wait()

	got := doc.FormatViolations()
	if len(got) != 2 {
		t.Fatalf("recorded %d violations from %d goroutines, want 2 (one per defective object): %v",
			len(got), goroutines, got)
	}
}

// TestFormatViolations_OrderedByObjectNumber pins that the violations come out
// in object-number order whatever order they were recorded in, so a report
// built from them does not reorder between runs.
func TestFormatViolations_OrderedByObjectNumber(t *testing.T) {
	t.Parallel()
	doc := &Document{Logger: NopLogger()}
	doc.RecordFormatViolations(9, fmt.Errorf("nine a"), fmt.Errorf("nine b"))
	doc.RecordFormatViolations(2, fmt.Errorf("two"))
	doc.RecordFormatViolations(30, fmt.Errorf("thirty"))
	doc.RecordFormatViolations(1, fmt.Errorf("one"))

	want := []string{"one", "two", "nine a", "nine b", "thirty"}
	got := doc.FormatViolations()
	if len(got) != len(want) {
		t.Fatalf("got %d violations, want %d: %v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Error() != w {
			t.Errorf("violation %d = %q, want %q", i, got[i], w)
		}
	}
}

// TestRecordFormatViolations_EmptyRecordMarksObject pins that an object
// recorded with no violations is still marked, so a later parse that reads
// different bytes for it cannot add any.
func TestRecordFormatViolations_EmptyRecordMarksObject(t *testing.T) {
	t.Parallel()
	doc := &Document{Logger: NopLogger()}
	doc.RecordFormatViolations(4)
	doc.RecordFormatViolations(4, fmt.Errorf("late finding"))
	if got := doc.FormatViolations(); len(got) != 0 {
		t.Errorf("got %v, want no violations", got)
	}
}

// TestResetFormatViolations_ClearsRecord pins that the reset returns the
// ordered violations and clears the record, so the objects re-parsed after it
// record again. ClearCaches resets both together for that reason.
func TestResetFormatViolations_ClearsRecord(t *testing.T) {
	t.Parallel()
	data, offsets := formatViolationPDF(t, "<< /Type /Catalog >>\n", "<< /A 1 >>")
	doc := formatViolationDoc(data, offsets)
	doc.SetComplianceMode(true)

	ctx := context.Background()
	if _, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: 2}); err != nil {
		t.Fatalf("resolve object 2: %v", err)
	}
	first := doc.ResetFormatViolations()
	if len(first) != 1 {
		t.Fatalf("reset returned %d violations, want 1: %v", len(first), first)
	}
	if got := doc.FormatViolations(); len(got) != 0 {
		t.Fatalf("record not cleared: %v", got)
	}

	doc.ClearCaches()
	if _, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: 2}); err != nil {
		t.Fatalf("re-resolve object 2: %v", err)
	}
	if got := doc.FormatViolations(); len(got) != 1 {
		t.Fatalf("re-parse after a cache reset recorded %d violations, want 1: %v", len(got), got)
	}
}

// TestClearCaches_ResetsFormatViolations pins that dropping the object cache
// drops the record with it. Keeping the record while the cache is gone would
// leave violations no re-parse could reproduce; keeping the cache while the
// record is gone would lose them for good.
func TestClearCaches_ResetsFormatViolations(t *testing.T) {
	t.Parallel()
	doc := &Document{Logger: slog.New(slog.DiscardHandler)}
	doc.RecordFormatViolations(1, fmt.Errorf("finding"))
	doc.ClearCaches()
	if got := doc.FormatViolations(); len(got) != 0 {
		t.Errorf("ClearCaches left %v, want the record dropped", got)
	}
}
