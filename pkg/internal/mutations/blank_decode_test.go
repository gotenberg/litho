package mutations

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// blankDictFixture builds the image page of redactImageFixture with a
// caller-chosen image dictionary, which the fixed fixture cannot express. extra
// carries any entries beyond the defaults and must end with a space when
// non-empty.
func blankDictFixture(t *testing.T, colorSpace, extra, data string) (*document.Document, *document.Page) {
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
		"7 0 obj\n<< /Type /XObject /Subtype /Image /Width 1 /Height 1 /ColorSpace " + colorSpace +
		" /BitsPerComponent 8 " + extra + "/Length " + strconv.Itoa(len(data)) + " >>\nstream\n" + data + "\nendstream\nendobj\n" +
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

// theImageRegion covers the single sample of the fixture's image, whose centre
// lands at (150,650) inside the box imagePageContent places at (50,600)-(250,700).
var theImageRegion = [][4]float64{{60, 610, 240, 690}}

// TestBlankImage_PlainDecodeArrayNoLongerRefuses pins that an image with a
// direct colour space and a /Decode array is blanked, and that the replacement
// drops /Decode.
//
// # Corpus evidence
//
//   - All corpora: refusing /Decode blocked 51% of sub-8-bpc images.
func TestBlankImage_PlainDecodeArrayNoLongerRefuses(t *testing.T) {
	// The relaxation. A direct colour space with /Decode is decoded with the
	// array already folded into the pixels, and the replacement dictionary does
	// not carry it, so the image renders as before and there is nothing to
	// refuse over. Inverted /Decode [1 0] is the routine case on bilevel scans,
	// which is why the refusal blocked so many sub-8-bpc images.
	doc, page := blankDictFixture(t, "/DeviceGray", "/Decode [1 0] ", " ")
	alloc := writer.NewObjAllocator(20)

	if _, err := RedactPageRegions(context.Background(), alloc, doc, page, theImageRegion); err != nil {
		t.Fatalf("RedactPageRegions refused an image whose /Decode the decoder applied: %v", err)
	}

	replaced, ok := alloc.Pending()[7].(ast.PdfStream)
	if !ok {
		t.Fatalf("object 7 was not replaced: %T", alloc.Pending()[7])
	}
	// The whole basis for allowing it: the entry is dropped, not carried onto
	// samples that already have the mapping baked in.
	if _, present := replaced.Dict["Decode"]; present {
		t.Errorf("the replacement carries /Decode, which would apply the mapping twice")
	}
}

func TestBlankImage_IndexedDecodeIsFolded(t *testing.T) {
	// An indexed image's /Decode remaps palette indices (ISO 32000-1, 8.9.5.2),
	// and the decoder maps the indices through it before the lookup, so the
	// decoded pixels carry the mapping and the entry is dropped like any other.
	doc, page := blankDictFixture(t,
		"[/Indexed /DeviceRGB 255 <FF0000 00FF00>]", "/Decode [0 255] ", " ")
	alloc := writer.NewObjAllocator(20)

	if _, err := RedactPageRegions(context.Background(), alloc, doc, page, theImageRegion); err != nil {
		t.Fatalf("RedactPageRegions refused an indexed image whose /Decode the decoder applied: %v", err)
	}
	replaced, ok := alloc.Pending()[7].(ast.PdfStream)
	if !ok {
		t.Fatalf("object 7 was not replaced: %T", alloc.Pending()[7])
	}
	if _, present := replaced.Dict["Decode"]; present {
		t.Errorf("the replacement carries /Decode, which would apply the mapping twice")
	}
}

// tinyJPEG returns the bytes of a 1x1 JPEG.
//
// Real JPEG data, so the test below exercises an image that genuinely decodes.
//
// The history is worth keeping. DecodeImage used to hand already-decoded samples
// back to image/jpeg, so no JPEG decoded at all; a JPEG was refused here on the
// grounds that its /Decode never reached its pixels. Reading a negative
// control's message rather than its verdict is what turned up that
// double-decode, and fixing it removed the reason for the refusal.
func tinyJPEG(t *testing.T) string {
	t.Helper()

	src := image.NewRGBA(image.Rect(0, 0, 1, 1))
	src.Set(0, 0, color.RGBA{R: 10, G: 20, B: 30, A: 255})
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, src, nil); err != nil {
		t.Fatalf("jpeg.Encode: %v", err)
	}
	return buf.String()
}

func TestBlankImage_JPEGDecodeNoLongerRefuses(t *testing.T) {
	// A JPEG's /Decode reaches its pixels now that DecodeImage no longer
	// double-decodes, so on re-encode the entry is redundant exactly as it is
	// for a Flate image and there is nothing left to refuse over. This refused
	// until that fix, on a premise that had quietly stopped being true.
	doc, page := blankDictFixture(t, "/DeviceRGB",
		"/Filter /DCTDecode /Decode [1 0 1 0 1 0] ", tinyJPEG(t))
	alloc := writer.NewObjAllocator(20)

	if _, err := RedactPageRegions(context.Background(), alloc, doc, page, theImageRegion); err != nil {
		t.Fatalf("RedactPageRegions refused a JPEG whose /Decode the decoder applied: %v", err)
	}

	replaced, ok := alloc.Pending()[7].(ast.PdfStream)
	if !ok {
		t.Fatalf("object 7 was not replaced: %T", alloc.Pending()[7])
	}
	if _, present := replaced.Dict["Decode"]; present {
		t.Errorf("the replacement carries /Decode, which would apply the mapping twice")
	}
}

func TestBlankImage_MaskStillRefuses(t *testing.T) {
	// /Mask says which samples are painted at all. The decoder does not apply
	// it and the replacement does not carry it, so blanking would make
	// masked-out picture opaque outside the region as well as inside.
	doc, page := blankDictFixture(t, "/DeviceGray", "/Mask [0 0] ", " ")
	alloc := writer.NewObjAllocator(20)

	_, err := RedactPageRegions(context.Background(), alloc, doc, page, theImageRegion)
	if !errors.Is(err, ErrImageCarriesSampleEntries) {
		t.Errorf("err = %v, want ErrImageCarriesSampleEntries for /Mask", err)
	}
}
