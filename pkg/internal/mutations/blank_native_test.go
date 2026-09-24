package mutations

import (
	"bytes"
	"compress/zlib"
	"context"
	"io"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// TestBlankImage_KeepsTheColourSpace clears one pixel of a two-pixel CMYK
// picture and leaves the other exactly as it was: the replacement stays in
// DeviceCMYK, where transcoding to RGB used to convert the pixel the region
// never touched.
func TestBlankImage_KeepsTheColourSpace(t *testing.T) {
	ctx := context.Background()
	body := imagePageContent
	data := string([]byte{10, 20, 30, 40, 50, 60, 70, 80})
	raw := "%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R >> /XObject << /Im1 7 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"7 0 obj\n<< /Type /XObject /Subtype /Image /Width 2 /Height 1 /ColorSpace /DeviceCMYK" +
		" /BitsPerComponent 8 /Length 8 >>\nstream\n" + data + "\nendstream\nendobj\n" +
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

	// The image spans (50,600)-(250,700); the left pixel's centre is at
	// (100,650), the right one's at (200,650).
	alloc := writer.NewObjAllocator(20)
	if _, err := RedactPageRegions(ctx, alloc, doc, pages[0], [][4]float64{{60, 610, 140, 690}}); err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
	replaced, ok := alloc.Pending()[7].(ast.PdfStream)
	if !ok {
		t.Fatalf("object 7 = %T, want the replaced image stream", alloc.Pending()[7])
	}
	if cs := replaced.Dict["ColorSpace"]; cs != ast.PdfName("DeviceCMYK") {
		t.Errorf("ColorSpace = %v, want DeviceCMYK", cs)
	}
	stored, err := replaced.Bytes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zlib.NewReader(bytes.NewReader(stored))
	if err != nil {
		t.Fatal(err)
	}
	samples, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	if want := []byte{0, 0, 0, 255, 50, 60, 70, 80}; !bytes.Equal(samples, want) {
		t.Errorf("samples = %v, want %v: black ink where cleared, the other pixel untouched", samples, want)
	}
}
