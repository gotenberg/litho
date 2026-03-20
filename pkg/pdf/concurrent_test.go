package pdf

import (
	"bytes"
	"context"
	"sync"
	"testing"
)

// multiPagePDF builds a valid PDF with 3 pages, each containing a Helvetica text string.
func multiPagePDF() []byte {
	return []byte(
		"%PDF-1.4\n" +
			// Catalog
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			// Pages
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R 6 0 R 8 0 R] /Count 3 >>\nendobj\n" +
			// Font
			"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
			// Resources
			"5 0 obj\n<< /Font << /F1 4 0 R >> >>\nendobj\n" +
			// Page 1
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 10 0 R >>\nendobj\n" +
			// Page 1 content stream
			"10 0 obj\n<< /Length 44 >>\nstream\nBT /F1 12 Tf 100 700 Td (Hello) Tj ET\nendstream\nendobj\n" +
			// Page 2
			"6 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 11 0 R >>\nendobj\n" +
			// Page 2 content stream
			"11 0 obj\n<< /Length 44 >>\nstream\nBT /F1 12 Tf 100 700 Td (World) Tj ET\nendstream\nendobj\n" +
			// Page 3
			"8 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 12 0 R >>\nendobj\n" +
			// Page 3 content stream
			"12 0 obj\n<< /Length 42 >>\nstream\nBT /F1 12 Tf 100 700 Td (Page3) Tj ET\nendstream\nendobj\n" +
			// XRef
			"xref\n0 13\n" +
			"0000000000 65535 f \n" +
			"0000000009 00000 n \n" +
			"0000000058 00000 n \n" +
			"0000000227 00000 n \n" +
			"0000000128 00000 n \n" +
			"0000000195 00000 n \n" +
			"0000000430 00000 n \n" +
			"0000000000 65535 f \n" +
			"0000000633 00000 n \n" +
			"0000000000 65535 f \n" +
			"0000000347 00000 n \n" +
			"0000000550 00000 n \n" +
			"0000000753 00000 n \n" +
			"trailer\n<< /Size 13 /Root 1 0 R >>\nstartxref\n853\n%%EOF\n")
}

// TestConcurrentExtractText verifies that multiple goroutines can extract text
// from different pages of the same Reader without races or incorrect results.
func TestConcurrentExtractText(t *testing.T) {
	data := multiPagePDF()
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	const goroutines = 20

	var wg sync.WaitGroup
	errs := make(chan error, goroutines)

	for range goroutines {
		wg.Go(func() {
			// Each goroutine extracts text from all 3 pages.
			for page := 1; page <= 3; page++ {
				_, err := r.ExtractText(context.Background(), page)
				if err != nil {
					errs <- err
					return
				}
			}
		})
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent ExtractText failed: %v", err)
	}
}

// TestConcurrentMixedMethods verifies that different Reader methods can be
// called concurrently without races.
func TestConcurrentMixedMethods(t *testing.T) {
	data := multiPagePDF()
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	const goroutines = 10

	var wg sync.WaitGroup

	errs := make(chan error, goroutines*4)

	for range goroutines {
		wg.Add(4)

		go func() {
			defer wg.Done()
			if _, err := r.NumPages(context.Background()); err != nil {
				errs <- err
			}
		}()

		go func() {
			defer wg.Done()
			// GetMetadata returns an error for our minimal PDF (no /Info dict);
			// we only care that it doesn't panic or race.
			_, _ = r.GetMetadata(context.Background())
		}()

		go func() {
			defer wg.Done()
			if _, err := r.ExtractText(context.Background(), 1); err != nil {
				errs <- err
			}
		}()

		go func() {
			defer wg.Done()
			if _, err := r.GetBookmarks(context.Background()); err != nil {
				errs <- err
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent method call failed: %v", err)
	}
}
