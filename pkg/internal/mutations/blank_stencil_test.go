package mutations

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"io"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// blankStencilFixture builds the image page of redactImageFixture whose /Im1 is
// a stencil mask: no /ColorSpace and one bit a sample, which blankDictFixture
// cannot express because it always writes a colour space at 8 bits. extra
// carries any entries beyond the defaults and must end with a space when
// non-empty.
func blankStencilFixture(t *testing.T, width, height int, extra, data string) (*document.Document, *document.Page) {
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
		"7 0 obj\n<< /Type /XObject /Subtype /Image /Width " + strconv.Itoa(width) +
		" /Height " + strconv.Itoa(height) + " /ImageMask true /BitsPerComponent 1 " + extra +
		"/Length " + strconv.Itoa(len(data)) + " >>\nstream\n" + data + "\nendstream\nendobj\n" +
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

// theLeftSampleRegion covers the left half of the image box imagePageContent
// places at (50,600)-(250,700), so for a two-sample-wide image it holds the
// sample whose centre lands at x=100 and not the one at x=200.
//
// Covering only one of the two is the point: a test where the region covers
// everything cannot tell blanking from wholesale replacement.
var theLeftSampleRegion = [][4]float64{{60, 610, 140, 690}}

// stencilBytes returns the decompressed samples of the replacement written for
// object 7, having first checked the replacement is still a stencil mask.
func stencilBytes(t *testing.T, alloc *writer.ObjAllocator) []byte {
	t.Helper()

	replaced, ok := alloc.Pending()[7].(ast.PdfStream)
	if !ok {
		t.Fatalf("object 7 was not replaced: %T", alloc.Pending()[7])
	}
	if mask, present := replaced.Dict["ImageMask"]; !present || mask != ast.PdfBool(true) {
		t.Errorf("ImageMask = %v (present %t), want true: the replacement stopped being a mask", mask, present)
	}
	if bpc := replaced.Dict["BitsPerComponent"]; bpc != ast.PdfInt(1) {
		t.Errorf("BitsPerComponent = %v, want 1: ISO 32000-1, 8.9.6.2 gives a mask one bit a sample", bpc)
	}
	// Table 89 does not allow /ColorSpace on a mask, and a carried-over /Decode
	// would apply a mapping the decode already folded into the samples.
	if cs, present := replaced.Dict["ColorSpace"]; present {
		t.Errorf("the replacement carries /ColorSpace %v, which Table 89 does not allow on a mask", cs)
	}
	if dec, present := replaced.Dict["Decode"]; present {
		t.Errorf("the replacement carries /Decode %v, which would apply the mapping twice", dec)
	}

	// Bytes returns the stream as stored, before filter decoding, which is the
	// /FlateDecode payload blanking wrote.
	raw, err := replaced.Bytes(context.Background())
	if err != nil {
		t.Fatalf("reading the replacement stream: %v", err)
	}
	zr, err := zlib.NewReader(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("opening the replacement samples: %v", err)
	}
	defer func() {
		if err := zr.Close(); err != nil {
			t.Errorf("closing the sample reader: %v", err)
		}
	}()
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("reading the replacement samples: %v", err)
	}
	return out
}

// TestBlankImage_StencilMaskIsBlankedAsAMask is the capability the refusal used
// to stand in for.
//
// A stencil supplies a shape, not a picture: ISO 32000-1, 8.9.6.2 paints its
// unmasked samples with the current fill colour. So it is cleared by staying a
// mask and setting the covered samples UNPAINTED. Re-encoding it to 8-bit
// DeviceRGB, which is how a picture is blanked, would stop it taking the fill
// colour; painting the covered samples black, which is what clearing a picture
// does, would mark the page in whatever colour was in force instead of removing
// the shape.
//
// Two samples, one covered. The expected byte carries the whole proof: bit 7 is
// the blanked sample and must read unpainted, bit 6 is the untouched one and
// must still read painted. A blanket rewrite fails the second half, and a
// no-op fails the first.
func TestBlankImage_StencilMaskIsBlankedAsAMask(t *testing.T) {
	// Both samples painted: under the default /Decode [0 1] a 0 bit paints, so
	// a 2x1 row is 0b00000000 with the six trailing bits pure padding.
	doc, page := blankStencilFixture(t, 2, 1, "", "\x00")
	alloc := writer.NewObjAllocator(20)

	if _, err := RedactPageRegions(context.Background(), alloc, doc, page, theLeftSampleRegion); err != nil {
		t.Fatalf("RedactPageRegions refused a stencil it can rewrite as a stencil: %v", err)
	}

	samples := stencilBytes(t, alloc)
	if len(samples) != 1 {
		t.Fatalf("samples = %d bytes, want 1: a 2-pixel row at one bit a sample pads to one byte", len(samples))
	}
	if samples[0] != 0x80 {
		t.Errorf("samples[0] = %#02x, want 0x80: bit 7 (x=0, covered) must read unpainted and bit 6 (x=1, outside the region) must still read painted",
			samples[0])
	}
}

// TestBlankImage_StencilMaskDropsAnInvertedDecode pins that the /Decode array
// is re-expressed rather than carried.
//
// The decode folds /Decode into the pixels, so a mask written [1 0] arrives with
// its sense already flipped. The replacement carries no array and writes the
// default [0 1] meaning directly, which means the SAME expected byte as the
// test above from the opposite source bits. Carrying the entry over would
// invert the whole mask; keeping the source bits would invert it too.
func TestBlankImage_StencilMaskDropsAnInvertedDecode(t *testing.T) {
	// /Decode [1 0] inverts, so the bits that mean "painted" are 1 here: 0xC0
	// is the same two painted samples as 0x00 is above.
	doc, page := blankStencilFixture(t, 2, 1, "/Decode [1 0] ", "\xC0")
	alloc := writer.NewObjAllocator(20)

	if _, err := RedactPageRegions(context.Background(), alloc, doc, page, theLeftSampleRegion); err != nil {
		t.Fatalf("RedactPageRegions refused a stencil whose /Decode the decoder applied: %v", err)
	}

	samples := stencilBytes(t, alloc)
	if len(samples) != 1 {
		t.Fatalf("samples = %d bytes, want 1", len(samples))
	}
	if samples[0] != 0x80 {
		t.Errorf("samples[0] = %#02x, want 0x80: an inverted source must come back under the default /Decode, not with the array carried over",
			samples[0])
	}
}

// theTopLeftSampleRegion covers one quarter of the image box, holding the
// sample whose centre lands at (100,675) and none of the other three.
//
// It cannot be theLeftSampleRegion: that one reaches down to y=610, which at two
// rows high covers the centres of BOTH rows (675 and 625) and so proves nothing
// about which row was written.
var theTopLeftSampleRegion = [][4]float64{{60, 650, 140, 690}}

// TestBlankImage_StencilMaskBlanksOneRowOnly covers the row stride, which every
// other stencil test here leaves untested.
//
// The others are one row high, so rowBytes is 1 and the row index never
// multiplies anything; blanking the whole of a real file's mask does not catch
// it either, because a uniformly cleared bitmap reads the same whatever stride
// wrote it. An off-by-one in packStencilBits' row term would therefore survive
// all of them and corrupt every multi-row mask, which is every real one.
//
// Four samples, one covered, and the two rows deliberately DIFFER. That last
// part is what gives the test teeth: with every sample painted, a stride that
// collapsed both rows onto the first byte would still produce the same two
// bytes, because a painted sample writes no bit and the second byte would stay
// zero either way. An unpainted bottom row has bits to misplace, so a collapsed
// stride ORs them into byte 0 and both assertions below fail.
func TestBlankImage_StencilMaskBlanksOneRowOnly(t *testing.T) {
	// 2x2. The top row is painted (0x00) and the bottom row is not (0xC0), one
	// bit a sample with each row padded to its own byte.
	doc, page := blankStencilFixture(t, 2, 2, "", "\x00\xC0")
	alloc := writer.NewObjAllocator(20)

	if _, err := RedactPageRegions(context.Background(), alloc, doc, page, theTopLeftSampleRegion); err != nil {
		t.Fatalf("RedactPageRegions refused a two-row stencil: %v", err)
	}

	samples := stencilBytes(t, alloc)
	if len(samples) != 2 {
		t.Fatalf("samples = %d bytes, want 2: each row of a 2-pixel mask pads to its own byte", len(samples))
	}
	if samples[0] != 0x80 {
		t.Errorf("row 0 = %#02x, want 0x80: the covered sample at (100,675) must read unpainted and its neighbour at (200,675) must not",
			samples[0])
	}
	if samples[1] != 0xC0 {
		t.Errorf("row 1 = %#02x, want 0xC0: the region never reached y=625, so the bottom row must come back exactly as it went in",
			samples[1])
	}
}

// TestBlankImage_StencilMaskWithSoftMaskRefuses pins the one stencil that is
// still refused.
//
// ISO 32000-1, Table 89 gives /SMask a stream of alpha values describing a
// picture's samples, and a mask has no samples for it to describe. Dropping it
// would change rendering for a viewer that honours it, and carrying it onto the
// one-bit replacement would assert a meaning no clause gives it, so this refuses
// rather than choose between them.
func TestBlankImage_StencilMaskWithSoftMaskRefuses(t *testing.T) {
	doc, page := blankStencilFixture(t, 2, 1, "/SMask 9 0 R ", "\x00")
	alloc := writer.NewObjAllocator(20)

	_, err := RedactPageRegions(context.Background(), alloc, doc, page, theLeftSampleRegion)
	if !errors.Is(err, ErrImageIsStencilMask) {
		t.Errorf("err = %v, want ErrImageIsStencilMask for a mask carrying /SMask", err)
	}
	if _, replaced := alloc.Pending()[7]; replaced {
		t.Error("the stencil was replaced although the region was refused")
	}
}
