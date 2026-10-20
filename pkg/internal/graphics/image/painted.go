package image

import (
	"context"
	"image"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// DecodePainted decodes the image stream a content stream paints, read
// against that content stream's resources and the dictionary of the page it
// lies on: the default colour spaces resources holds stand in for the device
// spaces (ISO 32000-1, 8.6.5.6), the page's output intents are the page's
// (ISO 32000-2, 14.11.5), and a /Matte is undone as [DecodeImage] undoes it.
// name labels the image in errors and logs. A form XObject, a pattern or an
// annotation appearance passes its own resources, which alone cover it (ISO
// 32000-2, 8.6.5.7).
//
// stream may be an inline image whose dictionary has been written out in
// full (8.9.7, Tables 93 and 94) over its data.
func DecodePainted(ctx context.Context, doc *document.Document, name ast.PdfName, stream ast.PdfStream, resources, page ast.PdfDict) (image.Image, error) {
	return decodeImageStream(ctx, doc, name, stream, colorContext{resources: resources, page: page})
}

// DecodeMask decodes a mask stream: the /SMask of an image (ISO 32000-1,
// 11.6.5.3), a DeviceGray image whose grey level is opacity, or a stencil
// mask, the /Mask stream of an image or an image mask (8.9.6.2, 8.9.6.3),
// which reads as DeviceGray with its /Decode applied. A mask holds opacity,
// not colour, so no default colour space or output intent applies to it.
func DecodeMask(ctx context.Context, doc *document.Document, name ast.PdfName, stream ast.PdfStream) (image.Image, error) {
	return decodeImagePixels(ctx, doc, name, stream, colorContext{})
}
