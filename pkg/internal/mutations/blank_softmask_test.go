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

// TestRedactPageRegions_BlanksTheSoftMaskOfABlankedImage covers a 2x1 grey
// picture whose /SMask makes its left sample transparent, the shape a cover's
// lettering takes in pdfcpu WaldenFull.pdf. Kept as it was, the mask went on
// drawing that shape in the blank's black after the picture was cleared.
func TestRedactPageRegions_BlanksTheSoftMaskOfABlankedImage(t *testing.T) {
	ctx := context.Background()
	body := "q 200 0 0 100 50 600 cm /Im1 Do Q"
	data := []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"5 0 obj\n<< /XObject << /Im1 7 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"7 0 obj\n<< /Type /XObject /Subtype /Image /Width 2 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /SMask 8 0 R /Length 2 >>\nstream\n\x80\x80\nendstream\nendobj\n" +
		"8 0 obj\n<< /Type /XObject /Subtype /Image /Width 2 /Height 1 /ColorSpace /DeviceGray /BitsPerComponent 8 /Length 2 >>\nstream\n\x00\x80\nendstream\nendobj\n" +
		"trailer\n<< /Size 9 /Root 1 0 R >>\n%%EOF\n")
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
	alloc := writer.NewObjAllocator(20)

	// Covers the left sample's centre at (100,650) alone.
	if _, err := RedactPageRegions(ctx, alloc, doc, pages[0], [][4]float64{{40, 590, 140, 710}}); err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
	replaced, ok := alloc.Pending()[7].(ast.PdfStream)
	if !ok {
		t.Fatalf("object 7 = %T, want the blanked image", alloc.Pending()[7])
	}
	ref, ok := replaced.Dict["SMask"].(ast.PdfIndirectRef)
	if !ok || ref.ObjectNum == 8 {
		t.Fatalf("SMask = %v, want a new mask object", replaced.Dict["SMask"])
	}
	if _, edited := alloc.Pending()[8]; edited {
		t.Error("the source mask was edited in place; another picture may share it")
	}
	mask, ok := alloc.Pending()[ref.ObjectNum].(ast.PdfStream)
	if !ok {
		t.Fatalf("mask %d = %T, want a stream", ref.ObjectNum, alloc.Pending()[ref.ObjectNum])
	}
	raw, err := mask.Bytes(ctx)
	if err != nil {
		t.Fatalf("reading the mask: %v", err)
	}
	zr, err := zlib.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("opening the mask: %v", err)
	}
	defer func() { _ = zr.Close() }()
	samples, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("decompressing the mask: %v", err)
	}
	if !bytes.Equal(samples, []byte{0xff, 0x80}) {
		t.Errorf("mask samples = %v, want the covered one opaque and the other as it was", samples)
	}
}
