package image

import (
	"context"
	"log/slog"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestDecodeImage_UndoesMatte recovers the colours of an image preblended with
// a black matte: a sample of 64 under a half-opaque mask was 128 before
// blending, and a pixel the mask leaves fully transparent is painted white.
// DecodeImageSamples keeps the samples as stored.
func TestDecodeImage_UndoesMatte(t *testing.T) {
	t.Parallel()

	smask := ast.NewStream(ast.PdfDict{
		"Subtype":          ast.PdfName("Image"),
		"Width":            ast.PdfInt(2),
		"Height":           ast.PdfInt(1),
		"ColorSpace":       ast.PdfName("DeviceGray"),
		"BitsPerComponent": ast.PdfInt(8),
		"Matte":            ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(0)},
	}, []byte{128, 0})
	stream := ast.NewStream(ast.PdfDict{
		"Subtype":          ast.PdfName("Image"),
		"Width":            ast.PdfInt(2),
		"Height":           ast.PdfInt(1),
		"ColorSpace":       ast.PdfName("DeviceRGB"),
		"BitsPerComponent": ast.PdfInt(8),
		"SMask":            smask,
	}, []byte{64, 0, 0, 10, 10, 10})
	doc := &document.Document{Logger: slog.New(slog.DiscardHandler)}
	page := &document.Page{Resources: ast.PdfDict{"XObject": ast.PdfDict{"Im0": stream}}}

	img, err := DecodeImage(context.Background(), doc, page, "Im0")
	if err != nil {
		t.Fatalf("DecodeImage: %v", err)
	}
	assertPixels(t, img, [][3]int{{128, 0, 0}, {255, 255, 255}})

	samples, err := DecodeImageSamples(context.Background(), doc, page, "Im0")
	if err != nil {
		t.Fatalf("DecodeImageSamples: %v", err)
	}
	assertPixels(t, samples, [][3]int{{64, 0, 0}, {10, 10, 10}})
}

// TestDecodeImage_MatteMaskTakesItsImageDimensions undoes the matte of an image
// whose soft mask states a name for /Height and no /Width at all. ISO 32000-1,
// 11.6.5.3 gives a matted mask its image's dimensions, so the entries have one
// value they can hold; leaving the matte undone kept the preblended samples.
func TestDecodeImage_MatteMaskTakesItsImageDimensions(t *testing.T) {
	t.Parallel()

	smask := ast.NewStream(ast.PdfDict{
		"Subtype":          ast.PdfName("Image"),
		"Height":           ast.PdfName("Length"),
		"ColorSpace":       ast.PdfName("DeviceGray"),
		"BitsPerComponent": ast.PdfInt(8),
		"Matte":            ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(0)},
	}, []byte{128, 0})
	stream := ast.NewStream(ast.PdfDict{
		"Subtype":          ast.PdfName("Image"),
		"Width":            ast.PdfInt(2),
		"Height":           ast.PdfInt(1),
		"ColorSpace":       ast.PdfName("DeviceRGB"),
		"BitsPerComponent": ast.PdfInt(8),
		"SMask":            smask,
	}, []byte{64, 0, 0, 10, 10, 10})
	doc := &document.Document{Logger: slog.New(slog.DiscardHandler)}
	page := &document.Page{Resources: ast.PdfDict{"XObject": ast.PdfDict{"Im0": stream}}}

	img, err := DecodeImage(context.Background(), doc, page, "Im0")
	if err != nil {
		t.Fatalf("DecodeImage: %v", err)
	}
	assertPixels(t, img, [][3]int{{128, 0, 0}, {255, 255, 255}})
	if _, ok := smask.Dict["Width"]; ok {
		t.Error("the document's soft mask dictionary was written into")
	}
}
