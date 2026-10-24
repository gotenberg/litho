package mutations

import (
	"bytes"
	"context"
	"encoding/binary"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// yccTestProfile builds an RGB input profile whose lut16 A2B0 reads its
// components as Y, Cb and Cr over Lab: zero samples are L* 0, a* and b* -128,
// a saturated blue, and the space's black is Y 0 with Cb and Cr at 128.
func yccTestProfile() []byte {
	lut := []byte("mft2\x00\x00\x00\x00")
	lut = append(lut, 3, 3, 2, 0)
	for i := range 9 {
		v := uint32(0)
		if i%4 == 0 {
			v = 1 << 16
		}
		lut = binary.BigEndian.AppendUint32(lut, v)
	}
	lut = binary.BigEndian.AppendUint16(lut, 2)
	lut = binary.BigEndian.AppendUint16(lut, 2)
	ramp := func() {
		for range 3 {
			lut = binary.BigEndian.AppendUint16(lut, 0)
			lut = binary.BigEndian.AppendUint16(lut, 65535)
		}
	}
	ramp()
	for y := range 2 {
		for cb := range 2 {
			for cr := range 2 {
				lut = binary.BigEndian.AppendUint16(lut, uint16(0xFF00*y))
				lut = binary.BigEndian.AppendUint16(lut, uint16(0xFFFF*cr))
				lut = binary.BigEndian.AppendUint16(lut, uint16(0xFFFF*cb))
			}
		}
	}
	ramp()

	header := make([]byte, 128)
	header[8] = 2
	copy(header[12:], "scnr")
	copy(header[16:], "RGB ")
	copy(header[20:], "Lab ")
	copy(header[36:], "acsp")
	table := binary.BigEndian.AppendUint32(nil, 1)
	table = append(table, "A2B0"...)
	table = binary.BigEndian.AppendUint32(table, 128+4+12)
	table = binary.BigEndian.AppendUint32(table, uint32(len(lut)))
	out := append(append(header, table...), lut...)
	binary.BigEndian.PutUint32(out, uint32(len(out)))
	return out
}

// yccImageFixture builds a one-page document whose content is body, with
// /CS0 an ICCBased space over [yccTestProfile] and /Im0 a 4 x 1 image in it
// painted 40 x 20 at (100, 600), the placement [placeInline] uses.
func yccImageFixture(t *testing.T, body string, samples []byte) (*document.Document, *document.Page) {
	t.Helper()
	ctx := context.Background()
	profile := yccTestProfile()
	data := []byte("%PDF-1.7\n" +
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
		"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
		"5 0 obj\n<< /Font << /F1 4 0 R >> /XObject << /Im0 7 0 R >> /ColorSpace << /CS0 [/ICCBased 8 0 R] >> >>\nendobj\n" +
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
		"6 0 obj\n<< /Length " + strconv.Itoa(len(body)) + " >>\nstream\n" + body + "\nendstream\nendobj\n" +
		"7 0 obj\n<< /Type /XObject /Subtype /Image /Width 4 /Height 1 /ColorSpace [/ICCBased 8 0 R] /BitsPerComponent 8 /Length " +
		strconv.Itoa(len(samples)) + " >>\nstream\n" + string(samples) + "\nendstream\nendobj\n" +
		"8 0 obj\n<< /N 3 /Length " + strconv.Itoa(len(profile)) + " >>\nstream\n" + string(profile) + "\nendstream\nendobj\n" +
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
	return doc, pages[0]
}

// yccSamples are four light YCC samples, each its own colour.
var yccSamples = []byte{200, 100, 150, 201, 101, 151, 202, 102, 152, 203, 103, 153}

// checkYCCBlanked checks that samples hold yccSamples with the first two
// covered: Y cleared and Cb, Cr on the neutral axis, the space's black, rather
// than the zeros it shows blue.
func checkYCCBlanked(t *testing.T, samples []byte) {
	t.Helper()
	if len(samples) != len(yccSamples) {
		t.Fatalf("got %d samples, want %d", len(samples), len(yccSamples))
	}
	if !bytes.Equal(samples[6:], yccSamples[6:]) {
		t.Errorf("uncovered samples = %v, want %v", samples[6:], yccSamples[6:])
	}
	for px := range 2 {
		y, cb, cr := int(samples[3*px]), int(samples[3*px+1]), int(samples[3*px+2])
		if y > 8 || cb < 112 || cb > 144 || cr < 112 || cr > 144 {
			t.Errorf("covered sample %d = %d %d %d, want Y near 0 and Cb, Cr near 128", px, y, cb, cr)
		}
	}
}

func TestRedactPageRegions_BlanksAYCCImageToItsBlack(t *testing.T) {
	t.Parallel()

	doc, page := yccImageFixture(t, "q 40 0 0 20 100 600 cm /Im0 Do Q BT /F1 12 Tf 100 300 Td (KEEP) Tj ET", yccSamples)
	alloc := writer.NewObjAllocator(20)
	report, err := RedactPageRegions(context.Background(), alloc, doc, page, leftHalf)
	if err != nil {
		t.Fatalf("RedactPageRegions: %v", err)
	}
	if len(report.BlankedImages) != 1 {
		t.Fatalf("BlankedImages = %q, want the image", report.BlankedImages)
	}
	obj, ok := alloc.Pending()[7]
	if !ok {
		t.Fatal("the image was not rewritten")
	}
	stream, ok := obj.(ast.PdfStream)
	if !ok {
		t.Fatalf("object 7 is %T, want a stream", obj)
	}
	samples, err := doc.DecodeStream(context.Background(), stream)
	if err != nil {
		t.Fatalf("decoding the blanked image: %v", err)
	}
	checkYCCBlanked(t, samples)
}

func TestRedactPageRegions_BlanksAYCCInlineImageToItsBlack(t *testing.T) {
	t.Parallel()

	doc, page := yccImageFixture(t, placeInline("/W 4 /H 1 /CS /CS0 /BPC 8", yccSamples), yccSamples)
	alloc, _ := redactInline(t, doc, page)
	checkYCCBlanked(t, redactedInline(t, alloc, page).samples)
}
