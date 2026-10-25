package mutations

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// stretchedImageFixture returns a page painting one image, written with dict
// entries and samples, stretched over (50,600)-(250,700): a one-sample image's
// centre lands at (150,650).
func stretchedImageFixture(t *testing.T, dict, samples string) (*document.Document, *document.Page) {
	t.Helper()
	ctx := context.Background()
	const body = "q 200 0 0 100 50 600 cm /Im1 Do Q"
	data := []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /XObject << /Im1 5 0 R >> >> /Contents 4 0 R >>\nendobj\n" +
		"4 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"5 0 obj\n<< /Type /XObject /Subtype /Image " + dict + " /Length " + strconv.Itoa(len(samples)) +
		" >>\nstream\n" + samples + "\nendstream\nendobj\n" +
		"trailer\n<< /Size 6 /Root 1 0 R >>\n%%EOF\n")
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
	return doc, pages[0]
}

func TestRedactPageRegions_LeavesASampleAlreadyBlank(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, dict, samples string
		want                error
	}{
		// Read in its own space, against the space's black.
		{"grey black", "/Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8", "\x00", nil},
		{"grey picture", "/Width 1 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8", "\x80", ErrImageRegionCoversNoSample},
		// Read as sRGB pixels, against the picture blank.
		{"indexed black", "/Width 1 /Height 1 /ColorSpace [/Indexed /DeviceRGB 1 <000000FFFFFF>] /BitsPerComponent 8", "\x00", nil},
		{"indexed white", "/Width 1 /Height 1 /ColorSpace [/Indexed /DeviceRGB 1 <000000FFFFFF>] /BitsPerComponent 8", "\x01", ErrImageRegionCoversNoSample},
		// A stencil's blank is the unpainted sample: 1 under the default
		// /Decode (ISO 32000-1, 8.9.6.2).
		{"stencil unpainted", "/Width 1 /Height 1 /ImageMask true /BitsPerComponent 1", "\x80", nil},
		{"stencil painted", "/Width 1 /Height 1 /ImageMask true /BitsPerComponent 1", "\x00", ErrImageRegionCoversNoSample},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, page := stretchedImageFixture(t, tc.dict, tc.samples)
			alloc := writer.NewObjAllocator(20)
			// The lower-left corner, clear of the centre at (150,650).
			report, err := RedactPageRegions(context.Background(), alloc, doc, page, [][4]float64{{55, 605, 100, 640}})
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
			if err != nil {
				return
			}
			if len(report.BlankedImages) != 0 {
				t.Errorf("BlankedImages = %v, want none: the sample held the blank", report.BlankedImages)
			}
			if n := len(alloc.Pending()); n != 0 {
				t.Errorf("registered %d objects, want none: nothing changed", n)
			}
		})
	}
}
