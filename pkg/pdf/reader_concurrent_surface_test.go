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
		{"DeclaredPDFX", func(c context.Context) { _, _ = r.DeclaredPDFX(c) }},
		{"DSSInfo", func(c context.Context) { _, _ = r.DSSInfo(c) }},
		{"EmbeddedFiles", func(c context.Context) { _, _ = r.EmbeddedFiles(c) }},
		{"ExtractFormulas", func(c context.Context) { _, _ = r.ExtractFormulas(c) }},
		{"ExtractPronunciationLexicons", func(c context.Context) { _, _ = r.ExtractPronunciationLexicons(c) }},
		{"ExtractSignatures", func(c context.Context) { _, _ = r.ExtractSignatures(c) }},
		{"FormData", func(c context.Context) { _, _ = r.FormData(c) }},
		{"FormFields", func(c context.Context) { _, _ = r.FormFields(c) }},
		{"HasAcroForm", func(c context.Context) { _ = r.HasAcroForm(c) }},
		{"HasFormFields", func(c context.Context) { _, _ = r.HasFormFields(c) }},
		{"HasXFAForm", func(c context.Context) { _ = r.HasXFAForm(c) }},
		{"IsPortfolio", func(c context.Context) { _, _ = r.IsPortfolio(c) }},
		{"Metadata", func(c context.Context) { _, _ = r.Metadata(c) }},
		{"PageCount", func(c context.Context) { _, _ = r.PageCount(c) }},
		{"UndecodableStreams", func(c context.Context) { _, _ = r.UndecodableStreams(c) }},
		{"SalvagedStreams", func(c context.Context) { _, _ = r.SalvagedStreams(c) }},
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

// TestConcurrentPageReadersSurface hammers every page-scoped reader across both
// pages at once.
//
// These are the readers worth racing hardest: unlike the context-only ones they
// build per-page and per-font state (glyph maps, decoded content, image
// dictionaries) on first use, so two goroutines asking different pages for
// different things share more machinery than any single-reader test reaches.
// TestConcurrentSemanticReaders covers ExtractAnnotations alone; this covers all
// ten, on both pages simultaneously.
//
// Errors are ignored for the same reason as in [TestConcurrentReaderSurface]:
// the fixture has no 3D models, geospatial data or redactions, and "this page
// has none of that" is a correct answer. The property under test is the absence
// of a race, not the content.
func TestConcurrentPageReadersSurface(t *testing.T) {
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

	readers := []func(context.Context, int){
		func(c context.Context, p int) { _, _ = r.Extract3DModels(c, p) },
		func(c context.Context, p int) { _, _ = r.ExtractAnnotations(c, p) },
		func(c context.Context, p int) { _, _ = r.ExtractColorSpaces(c, p) },
		func(c context.Context, p int) { _, _ = r.ExtractGeoData(c, p) },
		func(c context.Context, p int) { _, _ = r.ExtractImages(c, p) },
		func(c context.Context, p int) { _, _ = r.ExtractLines(c, p) },
		func(c context.Context, p int) { _, _ = r.ExtractMeasurements(c, p) },
		func(c context.Context, p int) { _, _ = r.ExtractRedactedText(c, p) },
		func(c context.Context, p int) { _, _ = r.ExtractTables(c, p) },
		func(c context.Context, p int) { _, _ = r.ExtractTextElements(c, p) },
		func(c context.Context, p int) { _, _ = r.Page(c, p) },
	}

	// richReaderPDF builds two pages; racing both at once is the point, since a
	// per-page cache keyed wrongly would only show up across pages.
	const pages = 2
	const rounds = 4

	var calls atomic.Int64
	var wg sync.WaitGroup
	for range rounds {
		for page := 1; page <= pages; page++ {
			for _, call := range readers {
				wg.Add(1)
				go func(fn func(context.Context, int), p int) {
					defer wg.Done()
					fn(context.Background(), p)
					calls.Add(1)
				}(call, page)
			}
		}
	}
	wg.Wait()

	if want := int64(rounds * pages * len(readers)); calls.Load() != want {
		t.Errorf("made %d page-reader calls, want %d", calls.Load(), want)
	}
}
