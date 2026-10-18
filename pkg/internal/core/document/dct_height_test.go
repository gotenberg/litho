package document

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestDecodeStream_DCTIndirectHeight pins that a DCTDecode image whose
// /Height is an indirect reference decodes to the height the reference
// gives when its frame declares more lines, a placeholder a streaming
// encoder never corrected.
//
// The DCTDecode filter reads /Height from the dictionary alone, which it
// cannot resolve, so the bound was lost: the frame's 65535 lines were decoded
// in full, and a large image went past the codec's sample cap.
//
// # Corpus evidence
//
//   - pdfjs issue15492.pdf: writes /Height 5 0 R for a 10800x10320 JPEG
//     whose frame declares 65000 lines.
func TestDecodeStream_DCTIndirectHeight(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	img := image.NewRGBA(image.Rect(0, 0, 64, 16))
	for x := range 64 {
		for y := range 16 {
			img.Set(x, y, color.RGBA{R: uint8(x * 4), G: uint8(y * 16), B: 90, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	tall := buf.Bytes()
	sof := bytes.Index(tall, []byte{0xFF, 0xC0})
	tall[sof+5], tall[sof+6] = 0xFF, 0xFF

	for _, tc := range []struct {
		name   string
		height string
	}{
		{"indirect", "3 0 R"},
		{"direct", "16"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := buildDestPDF(
				"<< /Type /Catalog /Pages 4 0 R >>",
				streamObject("/Type /XObject /Subtype /Image /Width 64 /Height "+tc.height+
					" /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode", tall),
				"16",
				"<< /Type /Pages /Kids [] /Count 0 >>",
			)
			doc, err := NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
			if err != nil {
				t.Fatalf("NewDocument: %v", err)
			}
			stream, err := ResolveStream(ctx, doc, ast.PdfIndirectRef{ObjectNum: 2})
			if err != nil {
				t.Fatalf("resolve stream: %v", err)
			}
			got, err := doc.DecodeStream(ctx, stream)
			if err != nil {
				t.Fatalf("DecodeStream: %v", err)
			}
			if want := 64 * 16 * 3; len(got) != want {
				t.Errorf("decoded %d bytes, want %d: the frame's placeholder height was not bounded", len(got), want)
			}
		})
	}
}
