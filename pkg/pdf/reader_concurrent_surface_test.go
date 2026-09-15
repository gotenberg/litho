package pdf

import (
	"bytes"
	"context"
	"sync"
	"sync/atomic"
	"testing"
)

// TestConcurrentReaderSurface hammers every context-only reader on one Reader
// from many goroutines at once, to back the concurrency contract with breadth
// rather than a sample.
//
// [Reader] documents itself as "safe for concurrent use by multiple
// goroutines", but -race only reports a race on a path some test actually runs
// concurrently, so that promise is worth exactly the surface the tests cover.
// TestConcurrentSemanticReaders covers the name tree, the outline and the form;
// this adds the remaining readers that take only a context, so every one of
// them is exercised under -race.
//
// Errors are deliberately ignored. Several of these readers answer "this
// document has none of that" for the fixture (it carries no signatures, no
// embedded files and no invoice), and the property under test is that
// concurrent calls do not race or panic, not what they return.
func TestConcurrentReaderSurface(t *testing.T) {
	t.Parallel()

	data := richReaderPDF(t)
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := r.Close(); closeErr != nil {
			t.Logf("closing reader: %v", closeErr)
		}
	})

	readers := []struct {
		name string
		call func(context.Context)
	}{
		{"Bookmarks", func(c context.Context) { _, _ = r.Bookmarks(c) }},
		{"DSSInfo", func(c context.Context) { _, _ = r.DSSInfo(c) }},
		{"EmbeddedFiles", func(c context.Context) { _, _ = r.EmbeddedFiles(c) }},
		{"ExtractFormulas", func(c context.Context) { _, _ = r.ExtractFormulas(c) }},
		{"ExtractPronunciationLexicons", func(c context.Context) { _, _ = r.ExtractPronunciationLexicons(c) }},
		{"ExtractSignatures", func(c context.Context) { _, _ = r.ExtractSignatures(c) }},
		{"FormData", func(c context.Context) { _, _ = r.FormData(c) }},
		{"FormFields", func(c context.Context) { _, _ = r.FormFields(c) }},
		{"HasAcroForm", func(c context.Context) { _ = r.HasAcroForm(c) }},
		{"HasFormFields", func(c context.Context) { _, _ = r.HasFormFields(c) }},
		{"IsPortfolio", func(c context.Context) { _, _ = r.IsPortfolio(c) }},
		{"Metadata", func(c context.Context) { _, _ = r.Metadata(c) }},
		{"PageCount", func(c context.Context) { _, _ = r.PageCount(c) }},
		{"UndecodableStreams", func(c context.Context) { _, _ = r.UndecodableStreams(c) }},
		{"ValidateFacturX", func(c context.Context) { _, _ = r.ValidateFacturX(c) }},
		{"ValidateFacturXReport", func(c context.Context) { _, _ = r.ValidateFacturXReport(c) }},
		{"XMPMetadata", func(c context.Context) { _, _ = r.XMPMetadata(c) }},
	}

	// Several rounds so a reader that caches on first use is hit both cold and
	// warm while its neighbours are still running.
	const rounds = 8

	// Counted, not assumed: a concurrency test that silently ran nothing would
	// pass under -race just as happily as one that exercised every reader.
	var calls atomic.Int64

	var wg sync.WaitGroup
	for range rounds {
		for _, rd := range readers {
			wg.Add(1)
			go func(call func(context.Context)) {
				defer wg.Done()
				call(context.Background())
				calls.Add(1)
			}(rd.call)
		}
	}
	wg.Wait()

	if want := int64(rounds * len(readers)); calls.Load() != want {
		t.Errorf("made %d reader calls, want %d", calls.Load(), want)
	}
}
