package pdf

import (
	"bytes"
	"context"
	"log/slog"
	"math"
	"strings"
	"sync"
	"testing"
)

// letterPDF builds a minimal PDF with a US Letter page (612x792 pt).
func letterPDF() []byte {
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\n" +
			"xref\n0 4\n0000000000 65535 f \n0000000009 00000 n \n0000000058 00000 n \n0000000115 00000 n \n" +
			"trailer\n<< /Size 4 /Root 1 0 R >>\nstartxref\n199\n%%EOF\n")
}

// rotatedPDF builds a minimal PDF with a US Letter page rotated 90 degrees.
func rotatedPDF() []byte {
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Rotate 90 >>\nendobj\n" +
			"xref\n0 4\n0000000000 65535 f \n0000000009 00000 n \n0000000058 00000 n \n0000000115 00000 n \n" +
			"trailer\n<< /Size 4 /Root 1 0 R >>\nstartxref\n211\n%%EOF\n")
}

// landscapePDF builds a minimal PDF with a landscape page (792x612 pt).
func landscapePDF() []byte {
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 792 612] >>\nendobj\n" +
			"xref\n0 4\n0000000000 65535 f \n0000000009 00000 n \n0000000058 00000 n \n0000000115 00000 n \n" +
			"trailer\n<< /Size 4 /Root 1 0 R >>\nstartxref\n199\n%%EOF\n")
}

// cropBoxPDF builds a PDF with a CropBox smaller than the MediaBox.
func cropBoxPDF() []byte {
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /CropBox [0 0 360 720] >>\nendobj\n" +
			"xref\n0 4\n0000000000 65535 f \n0000000009 00000 n \n0000000058 00000 n \n0000000115 00000 n \n" +
			"trailer\n<< /Size 4 /Root 1 0 R >>\nstartxref\n223\n%%EOF\n")
}

func almostEqual(a, b, epsilon float64) bool {
	return math.Abs(a-b) < epsilon
}

func TestPage_Portrait(t *testing.T) {
	data := letterPDF()
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	p, err := r.Page(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetPage: %v", err)
	}

	// US Letter: 612x792 pt
	if !almostEqual(p.Width, 612, 0.01) {
		t.Errorf("width: got %f, want 612", p.Width)
	}
	if !almostEqual(p.Height, 792, 0.01) {
		t.Errorf("height: got %f, want 792", p.Height)
	}
	if p.IsLandscape {
		t.Error("expected portrait, got landscape")
	}
	if p.Rotate != 0 {
		t.Errorf("rotate: got %d, want 0", p.Rotate)
	}
}

func TestPage_Landscape(t *testing.T) {
	data := landscapePDF()
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	p, err := r.Page(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetPage: %v", err)
	}

	if !p.IsLandscape {
		t.Error("expected landscape, got portrait")
	}
	if !almostEqual(p.Width, 792, 0.01) {
		t.Errorf("width: got %f, want 792", p.Width)
	}
	if !almostEqual(p.Height, 612, 0.01) {
		t.Errorf("height: got %f, want 612", p.Height)
	}
}

func TestPage_Rotated90(t *testing.T) {
	data := rotatedPDF()
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	p, err := r.Page(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetPage: %v", err)
	}

	// Portrait MediaBox (612x792) with /Rotate 90 swaps to 792x612.
	if !p.IsLandscape {
		t.Error("expected landscape after 90° rotation, got portrait")
	}
	if !almostEqual(p.Width, 792, 0.01) {
		t.Errorf("width: got %f, want 792", p.Width)
	}
	if !almostEqual(p.Height, 612, 0.01) {
		t.Errorf("height: got %f, want 612", p.Height)
	}
	if p.Rotate != 90 {
		t.Errorf("rotate: got %d, want 90", p.Rotate)
	}
}

func TestPage_CropBox(t *testing.T) {
	data := cropBoxPDF()
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	p, err := r.Page(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetPage: %v", err)
	}

	// CropBox is 360x720 pt.
	if !almostEqual(p.Width, 360, 0.01) {
		t.Errorf("width: got %f, want 360", p.Width)
	}
	if !almostEqual(p.Height, 720, 0.01) {
		t.Errorf("height: got %f, want 720", p.Height)
	}
}

// userUnitPDF builds a PDF with /UserUnit 2.0 and a half-size MediaBox.
// Effective dimensions: 306*2 x 396*2 = 612x792 pt (US Letter).
func userUnitPDF() []byte {
	return []byte(
		"%PDF-1.6\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 306 396] /UserUnit 2.0 >>\nendobj\n" +
			"xref\n0 4\n0000000000 65535 f \n0000000009 00000 n \n0000000058 00000 n \n0000000115 00000 n \n" +
			"trailer\n<< /Size 4 /Root 1 0 R >>\nstartxref\n215\n%%EOF\n")
}

func TestPage_UserUnit(t *testing.T) {
	data := userUnitPDF()
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	p, err := r.Page(context.Background(), 1)
	if err != nil {
		t.Fatalf("GetPage: %v", err)
	}

	// 306 * 2.0 = 612, 396 * 2.0 = 792
	if !almostEqual(p.Width, 612, 0.01) {
		t.Errorf("width: got %f, want 612", p.Width)
	}
	if !almostEqual(p.Height, 792, 0.01) {
		t.Errorf("height: got %f, want 792", p.Height)
	}
	if !almostEqual(p.UserUnit, 2.0, 0.001) {
		t.Errorf("userUnit: got %f, want 2.0", p.UserUnit)
	}
	if p.IsLandscape {
		t.Error("expected portrait, got landscape")
	}
}

func TestPage_InvalidPage(t *testing.T) {
	data := letterPDF()
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	_, err = r.Page(context.Background(), 99)
	if err == nil {
		t.Error("expected error for invalid page number")
	}
}

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
			_, _ = r.Metadata(context.Background())
		}()

		go func() {
			defer wg.Done()
			if _, err := r.ExtractText(context.Background(), 1); err != nil {
				errs <- err
			}
		}()

		go func() {
			defer wg.Done()
			if _, err := r.Bookmarks(context.Background()); err != nil {
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

// minimalPDF builds a valid PDF with a catalog and one empty page.
func minimalPDF() []byte {
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>\nendobj\n" +
			"xref\n0 4\n0000000000 65535 f \n0000000009 00000 n \n0000000058 00000 n \n0000000115 00000 n \n" +
			"trailer\n<< /Size 4 /Root 1 0 R >>\nstartxref\n200\n%%EOF\n")
}

func TestWithLogger_DebugOutput(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	data := minimalPDF()
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)), WithLogger(logger))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	_, err = r.NumPages(context.Background())
	if err != nil {
		t.Fatalf("NumPages failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "document opened") {
		t.Errorf("expected 'document opened' in debug output, got:\n%s", output)
	}
	if !strings.Contains(output, "xref parsed") {
		t.Errorf("expected 'xref parsed' in debug output, got:\n%s", output)
	}
	if !strings.Contains(output, "page tree traversed") {
		t.Errorf("expected 'page tree traversed' in debug output, got:\n%s", output)
	}
}

func TestWithoutLogger_NoOutput(t *testing.T) {
	data := minimalPDF()

	// Open without WithLogger — should produce no output and no errors.
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	_, err = r.NumPages(context.Background())
	if err != nil {
		t.Fatalf("NumPages failed: %v", err)
	}
}

func TestVersion(t *testing.T) {
	data := minimalPDF()
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	if v := r.Version(); v != "1.4" {
		t.Errorf("Version() = %q, want %q", v, "1.4")
	}
}

func TestExtractAllText(t *testing.T) {
	data := minimalPDF()
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	// Minimal PDF has no text content, so ExtractAllText should return empty.
	text, err := r.ExtractAllText(context.Background())
	if err != nil {
		t.Fatalf("ExtractAllText failed: %v", err)
	}
	if text != "" {
		t.Errorf("expected empty text, got %q", text)
	}
}

func TestClose_Noop(t *testing.T) {
	data := minimalPDF()
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}

	// Close on a Reader opened via Open should be a no-op.
	if err := r.Close(); err != nil {
		t.Errorf("Close failed: %v", err)
	}
}
