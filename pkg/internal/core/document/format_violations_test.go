package document

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/parser"
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

// TestCheckObjectFormat_ObjectCachedBeforeComplianceMode pins that an object
// resolved before compliance mode was turned on still gets its format
// violations recorded, once, and that the record is the one a first parse in
// compliance mode makes. pdf.Open resolves the catalog and the root of the
// page tree before any validation, and a resolution that hits the cache parses
// nothing, so a bad hex string in either was never reported.
func TestCheckObjectFormat_ObjectCachedBeforeComplianceMode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data, offsets := formatViolationPDF(t,
		"<< /Type /Catalog /Foo <414> /Bar <41Z4> >>\n",
		"<< /A 1 >>", // no EOL before endobj: a header finding
	)

	fresh := formatViolationDoc(data, offsets)
	fresh.SetComplianceMode(true)
	for objNum := 1; objNum <= 2; objNum++ {
		if _, err := fresh.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: objNum}); err != nil {
			t.Fatalf("resolve object %d: %v", objNum, err)
		}
	}
	want := fresh.FormatViolations()
	if len(want) != 3 {
		t.Fatalf("a first parse in compliance mode recorded %d violations, want 3: %v", len(want), want)
	}

	doc := formatViolationDoc(data, offsets)
	cached := make(map[int]ast.PdfObject)
	for objNum := 1; objNum <= 2; objNum++ {
		obj, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: objNum})
		if err != nil {
			t.Fatalf("resolve object %d: %v", objNum, err)
		}
		cached[objNum] = obj
	}
	doc.SetComplianceMode(true)
	for objNum := 1; objNum <= 2; objNum++ {
		if _, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: objNum}); err != nil {
			t.Fatalf("resolve object %d: %v", objNum, err)
		}
	}
	if got := doc.FormatViolations(); len(got) != 0 {
		t.Fatalf("a cache hit recorded %v; the fixture must reach the objects through the cache", got)
	}

	for range 3 {
		for objNum := 1; objNum <= 2; objNum++ {
			doc.CheckObjectFormat(ctx, objNum)
		}
	}
	got := doc.FormatViolations()
	if len(got) != len(want) {
		t.Fatalf("recorded %d violations, want the %d a first parse in compliance mode records: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i].Error() != want[i].Error() {
			t.Errorf("violation %d = %q, want %q", i, got[i], want[i])
		}
	}
	if !errors.Is(got[0], parser.ErrHexStringOddDigits) {
		t.Errorf("violation 0 = %v, want the odd-digit hex string", got[0])
	}

	for objNum := 1; objNum <= 2; objNum++ {
		obj, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: objNum})
		if err != nil {
			t.Fatalf("resolve object %d: %v", objNum, err)
		}
		if fmt.Sprintf("%p", obj) != fmt.Sprintf("%p", cached[objNum]) {
			t.Errorf("object %d: the format check replaced the cached value", objNum)
		}
	}
}

// TestCheckObjectFormat_RecordedObjectIsNotParsedAgain pins that an object a
// compliance-mode parse already recorded keeps that record, empty or not, so
// an object contributes its findings once.
func TestCheckObjectFormat_RecordedObjectIsNotParsedAgain(t *testing.T) {
	t.Parallel()
	data, offsets := formatViolationPDF(t, "<< /Type /Catalog >>\n", "<< /A <414> >>\n")
	doc := formatViolationDoc(data, offsets)
	doc.RecordFormatViolations(2)

	doc.CheckObjectFormat(context.Background(), 2)
	if got := doc.FormatViolations(); len(got) != 0 {
		t.Errorf("got %v, want the empty record kept", got)
	}
}

// TestCheckObjectFormat_RecordsNothingOutsideTheIndex pins that an object
// number the index lacks, a free entry and a member of an object stream that
// cannot be read, here because object 1 is no stream, record nothing: no parse
// of their bytes runs, so none has a record to complete.
func TestCheckObjectFormat_RecordsNothingOutsideTheIndex(t *testing.T) {
	t.Parallel()
	data, offsets := formatViolationPDF(t, "<< /Type /Catalog /Foo <414> >>\n")
	doc := formatViolationDoc(data, offsets)
	doc.xrefEntries[2] = xref.XRefEntry{Free: true}
	doc.xrefEntries[3] = xref.XRefEntry{Type: xref.XRefTypeCompressed, ObjStmNum: 1}

	ctx := context.Background()
	for _, objNum := range []int{2, 3, 4} {
		doc.CheckObjectFormat(ctx, objNum)
	}
	if got := doc.FormatViolations(); len(got) != 0 {
		t.Errorf("got %v, want nothing recorded", got)
	}
}

// cancelAfterCtx is a context whose Err reports cancellation from its
// (after+1)th call on, so a test can cancel a parse at a chosen checkpoint.
type cancelAfterCtx struct {
	context.Context
	calls atomic.Int32
	after int32
}

func (c *cancelAfterCtx) Err() error {
	if c.calls.Add(1) > c.after {
		return context.Canceled
	}
	return nil
}

// TestCheckObjectFormat_CancelledParseRecordsNothing pins that a format check
// the context cancelled leaves no record behind, whether the cancellation
// comes before the parse or in the middle of it. The first recording for an
// object wins, so a record of the header alone would stand for the object in
// every later validation of the Document.
func TestCheckObjectFormat_CancelledParseRecordsNothing(t *testing.T) {
	t.Parallel()
	for _, after := range []int32{0, 1} {
		data, offsets := formatViolationPDF(t, "<< /Type /Catalog /Foo <414> >>")
		doc := formatViolationDoc(data, offsets)

		doc.CheckObjectFormat(&cancelAfterCtx{Context: context.Background(), after: after}, 1)
		if got := doc.FormatViolations(); len(got) != 0 {
			t.Fatalf("cancelled after %d checkpoints: recorded %v, want nothing", after, got)
		}

		doc.CheckObjectFormat(context.Background(), 1)
		if got := doc.FormatViolations(); len(got) != 2 {
			t.Errorf("cancelled after %d checkpoints: the next check recorded %d violations, want 2 (the endobj EOL and the hex string): %v", after, len(got), got)
		}
	}
}

// TestCheckObjectFormat_ConcurrentWithResolveRecordsOnce pins that format
// checks racing compliance-mode resolutions of the same objects leave one
// record per object, the same one whichever goroutine made it.
func TestCheckObjectFormat_ConcurrentWithResolveRecordsOnce(t *testing.T) {
	t.Parallel()
	data, offsets := formatViolationPDF(t,
		"<< /Type /Catalog /Foo <414> >>\n",
		"<< /A <41Z4> >>",
		"<< /B 2 >>\n",
	)
	doc := formatViolationDoc(data, offsets)
	ctx := context.Background()
	if _, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: 1}); err != nil {
		t.Fatalf("resolve object 1: %v", err)
	}
	doc.SetComplianceMode(true)

	const goroutines = 16
	var wg sync.WaitGroup
	for i := range goroutines {
		wg.Go(func() {
			for objNum := 1; objNum <= 3; objNum++ {
				if i%2 == 0 {
					doc.CheckObjectFormat(ctx, objNum)
					continue
				}
				if _, err := doc.Resolve(ctx, ast.PdfIndirectRef{ObjectNum: objNum}); err != nil {
					t.Errorf("resolve object %d: %v", objNum, err)
				}
			}
		})
	}
	wg.Wait()

	got := doc.FormatViolations()
	if len(got) != 3 {
		t.Fatalf("recorded %d violations from %d goroutines, want 3 (the two hex strings and the endobj EOL): %v",
			len(got), goroutines, got)
	}
}
