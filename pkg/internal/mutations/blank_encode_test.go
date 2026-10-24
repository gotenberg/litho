package mutations

import (
	"bytes"
	"context"
	"io"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/filter"
	"github.com/gotenberg/litho/pkg/internal/filter/dct"
	pdfimage "github.com/gotenberg/litho/pkg/internal/graphics/image"
)

// TestBlankImage_PredictsAPhotographLosslessly blanks part of a JPEG
// photograph and decodes the replacement as a reader would, through its
// /DecodeParms: every covered sample is black exactly, every other one is
// the source's decoded value exactly, and the predicted encoding is smaller
// than the plain Flate blanking used to write. A lossy re-encoding fails the
// first two, and a plain one the third.
func TestBlankImage_PredictsAPhotographLosslessly(t *testing.T) {
	ctx := context.Background()
	// No sample centre lands on the region's edges: x = 50 + 200(px+0.5)/240
	// and y = 600 + 100(1-(py+0.5)/120) are never 60, 140, 610 or 690.
	const w, h = 240, 120
	samples := make([]byte, w*h*3)
	seed := uint32(11)
	for i := range samples {
		seed = seed*1664525 + 1013904223
		px := i / 3
		x, y := px%w, px/w
		samples[i] = byte(x*(i%3+1)/2+y) + byte(seed>>30) //nolint:gosec // Wrapping is the point.
	}
	jpeg, err := dct.Encode(ctx, samples, w, h, 3, dct.EncodeOptions{Quality: 90})
	if err != nil {
		t.Fatal(err)
	}

	body := imagePageContent
	raw := "%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R >> /XObject << /Im1 7 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"7 0 obj\n<< /Type /XObject /Subtype /Image /Width " + strconv.Itoa(w) + " /Height " + strconv.Itoa(h) +
		" /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length " + strconv.Itoa(len(jpeg)) +
		" >>\nstream\n" + string(jpeg) + "\nendstream\nendobj\n" +
		"trailer\n<< /Size 8 /Root 1 0 R >>\n%%EOF\n"
	doc, err := document.NewDocument(ctx, bytes.NewReader([]byte(raw)), int64(len(raw)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) == 0 {
		t.Fatalf("ResolvePages: %v", err)
	}
	source, err := document.ResolveStream(ctx, doc, ast.PdfIndirectRef{ObjectNum: 7})
	if err != nil {
		t.Fatal(err)
	}
	before, ok, err := pdfimage.DecodeStreamNative(ctx, doc, pages[0], "Im1", source)
	if err != nil || !ok {
		t.Fatalf("decoding the source: ok %v, %v", ok, err)
	}

	alloc := writer.NewObjAllocator(20)
	if _, err := RedactPageRegions(ctx, alloc, doc, pages[0], [][4]float64{{60, 610, 140, 690}}); err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
	replaced, ok := alloc.Pending()[7].(ast.PdfStream)
	if !ok {
		t.Fatalf("object 7 = %T, want the blanked image", alloc.Pending()[7])
	}
	parms, ok := replaced.Dict["DecodeParms"].(ast.PdfDict)
	if !ok || parms["Predictor"] != ast.PdfInt(15) || parms["Colors"] != ast.PdfInt(3) || parms["Columns"] != ast.PdfInt(w) {
		t.Fatalf("DecodeParms = %v, want PNG prediction over %d columns of 3 colours", replaced.Dict["DecodeParms"], w)
	}

	r, err := filter.NewStreamReader(ctx, replaced)
	if err != nil {
		t.Fatal(err)
	}
	after, err := io.ReadAll(r)
	if closeErr := r.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatalf("decoding the replacement: %v", err)
	}
	if len(after) != len(before.Data) {
		t.Fatalf("replacement holds %d bytes, want %d", len(after), len(before.Data))
	}
	covered := 0
	for py := range h {
		for px := range w {
			x := 50 + 200*(float64(px)+0.5)/w
			y := 600 + 100*(1-(float64(py)+0.5)/h)
			i := (py*w + px) * 3
			got := after[i : i+3]
			if x > 60 && x < 140 && y > 610 && y < 690 {
				covered++
				if !bytes.Equal(got, blankedPicture) {
					t.Fatalf("covered sample (%d, %d) = %v, want black exactly", px, py, got)
				}
				continue
			}
			if want := before.Data[i : i+3]; !bytes.Equal(got, want) {
				t.Fatalf("uncovered sample (%d, %d) = %v, want the source's %v", px, py, got, want)
			}
		}
	}
	if covered == 0 {
		t.Fatal("the region covers no sample: the test checks less than it says")
	}

	stored, err := replaced.Bytes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	plain, _, err := filter.CompressFlate(after, -1)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) >= len(plain) {
		t.Errorf("the replacement takes %d bytes, plain Flate %d: prediction did not pay", len(stored), len(plain))
	}
}
