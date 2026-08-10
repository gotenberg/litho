package image

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// multiImagePDF is a page whose /XObject dict holds several images, enough for
// Go's randomised map order to show.
func multiImagePDF() []byte {
	return []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R " +
		"/Resources << /XObject << /ImA 5 0 R /ImB 6 0 R /ImC 7 0 R /ImD 8 0 R /ImE 9 0 R >> >> >>\nendobj\n" +
		"4 0 obj\n<< /Length 1 >>\nstream\n \nendstream\nendobj\n" +
		"5 0 obj\n<< /Type /XObject /Subtype /Image /Width 11 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /Length 1 >>\nstream\n \nendstream\nendobj\n" +
		"6 0 obj\n<< /Type /XObject /Subtype /Image /Width 22 /Height 2 /ColorSpace /DeviceGray /BitsPerComponent 8 /Length 1 >>\nstream\n \nendstream\nendobj\n" +
		"7 0 obj\n<< /Type /XObject /Subtype /Image /Width 33 /Height 3 /ColorSpace /DeviceGray /BitsPerComponent 8 /Length 1 >>\nstream\n \nendstream\nendobj\n" +
		"8 0 obj\n<< /Type /XObject /Subtype /Image /Width 44 /Height 4 /ColorSpace /DeviceGray /BitsPerComponent 8 /Length 1 >>\nstream\n \nendstream\nendobj\n" +
		"9 0 obj\n<< /Type /XObject /Subtype /Image /Width 55 /Height 5 /ColorSpace /DeviceGray /BitsPerComponent 8 /Length 1 >>\nstream\n \nendstream\nendobj\n" +
		"trailer\n<< /Size 10 /Root 1 0 R >>\n%%EOF\n")
}

// TestExtractImages_OrderIsStable pins that the same page always yields its
// images in the same order.
//
// The resource dictionary is a Go map, and ranging it directly takes the
// randomised iteration order: `litho images` answered with a different ordering
// on every run, so anyone diffing two runs saw changes that were not there.
// litho's own rule is that ordered output never comes from a map walk.
func TestExtractImages_OrderIsStable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := multiImagePDF()

	var first []string
	for run := range 12 {
		doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
		if err != nil {
			t.Fatalf("NewDocument: %v", err)
		}
		catalog, err := document.ResolveCatalog(ctx, doc)
		if err != nil {
			t.Fatalf("ResolveCatalog: %v", err)
		}
		pages, err := document.ResolvePages(ctx, doc, catalog)
		if err != nil || len(pages) == 0 {
			t.Fatalf("ResolvePages: %v (%d pages)", err, len(pages))
		}

		got := make([]string, 0, 5)
		for _, img := range ExtractImages(ctx, doc, pages[0]) {
			got = append(got, img.Filename)
		}
		if len(got) != 5 {
			t.Fatalf("run %d found %d images, want 5", run, len(got))
		}
		if first == nil {
			first = got
			continue
		}
		for i := range got {
			if got[i] != first[i] {
				t.Fatalf("run %d ordered images %v, first run gave %v", run, got, first)
			}
		}
	}
	// Sorted by resource name, which is what makes it reproducible.
	want := []string{"ImA", "ImB", "ImC", "ImD", "ImE"}
	for i := range want {
		if first[i] != want[i] {
			t.Errorf("order = %v, want %v", first, want)
			break
		}
	}
}
