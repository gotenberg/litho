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

// blankStencilFixture builds the image page of redactImageFixture whose /Im1 is
// a stencil mask: no /ColorSpace and one bit a sample, which blankDictFixture
// cannot express because it always writes a colour space at 8 bits.
func blankStencilFixture(t *testing.T, data string) (*document.Document, *document.Page) {
	t.Helper()
	ctx := context.Background()

	body := imagePageContent
	raw := "%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R >> /XObject << /Im1 7 0 R >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"7 0 obj\n<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ImageMask true" +
		" /BitsPerComponent 1 /Length " + strconv.Itoa(len(data)) + " >>\nstream\n" + data + "\nendstream\nendobj\n" +
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
		t.Fatalf("ResolvePages: %v (%d pages)", err, len(pages))
	}
	return doc, pages[0]
}

// TestBlankImage_StencilMaskRefusesByName pins the refusal that has to be
// STATED once the decoder reads stencils correctly.
//
// A mask supplies a shape, not a picture: ISO 32000-1, 8.9.6.2 paints its
// unmasked samples with the current fill colour. Blanking re-encodes to 8-bit
// DeviceRGB under a dictionary carrying no /ImageMask, which would stop it
// being a mask and change what the page renders.
//
// Before the decoder fix this refused by ACCIDENT: a stencil was read as
// three-component colour, so a complete mask failed a length check as "decoded
// data too short" while one whose data happened to be long enough was blanked
// after its mask bits had been read as colour. Both are gone now, so the
// refusal has to be its own statement or redaction would silently rewrite a
// stencil.
func TestBlankImage_StencilMaskRefusesByName(t *testing.T) {
	doc, page := blankStencilFixture(t, " ")
	alloc := writer.NewObjAllocator(20)

	_, err := RedactPageRegions(context.Background(), alloc, doc, page, theImageRegion)
	if !errors.Is(err, ErrImageIsStencilMask) {
		t.Errorf("err = %v, want ErrImageIsStencilMask for an /ImageMask image", err)
	}
	if _, replaced := alloc.Pending()[7]; replaced {
		t.Error("the stencil was replaced although the region was refused")
	}
}
