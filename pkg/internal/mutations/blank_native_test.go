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

// TestBlankImage_NoneColorantStaysInvisible clears one pixel of a two-pixel
// image in a Separation space over the colorant None. ISO 32000-1, 8.6.6.4
// says such an image paints nothing, and its replacement keeps that space and
// its tints, where rewriting the transparent decode as DeviceRGB would paint
// the whole placement black.
func TestBlankImage_NoneColorantStaysInvisible(t *testing.T) {
	ctx := context.Background()
	body := imagePageContent
	data := string([]byte{100, 200})
	raw := "%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R >> /XObject << /Im1 7 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"7 0 obj\n<< /Type /XObject /Subtype /Image /Width 2 /Height 1" +
		" /ColorSpace [/Separation /None /DeviceGray 8 0 R]" +
		" /BitsPerComponent 8 /Length 2 >>\nstream\n" + data + "\nendstream\nendobj\n" +
		"8 0 obj\n<< /FunctionType 2 /Domain [0 1] /Range [0 1] /C0 [1] /C1 [0] /N 1 >>\nendobj\n" +
		"trailer\n<< /Size 9 /Root 1 0 R >>\n%%EOF\n"
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

	alloc := writer.NewObjAllocator(20)
	if _, err := RedactPageRegions(ctx, alloc, doc, pages[0], [][4]float64{{60, 610, 140, 690}}); err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
	replaced, ok := alloc.Pending()[7].(ast.PdfStream)
	if !ok {
		t.Fatalf("object 7 = %T, want the replaced image stream", alloc.Pending()[7])
	}
	cs, ok := replaced.Dict["ColorSpace"].(ast.PdfArray)
	if !ok || len(cs) < 2 || cs[1] != ast.PdfName("None") {
		t.Fatalf("ColorSpace = %v, want the Separation /None space kept", replaced.Dict["ColorSpace"])
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
	if want := []byte{0, 200}; !bytes.Equal(samples, want) {
		t.Errorf("samples = %v, want %v: the covered tint cleared, the other untouched", samples, want)
	}
}
