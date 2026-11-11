package limits

import "image/color"

// The costs below are what one unit of a structure several packages build
// costs a conversion's [Budget]. A cost only one package charges lives
// there, beside the site that charges it.

// XMLElementCost is what one parsed XML element of an Office part costs:
// the element, 96 bytes on a 64-bit platform, its attributes, its text and
// the slot its parent keeps. The tree lives as long as the converter reads
// it.
//
// # Corpus evidence
//
//   - libreoffice/ods fdo67682-2.ods: the 29,901 elements of content.xml
//     hold 9.8 MiB of live heap, about 340 bytes an element with its text.
//   - litho/pptx hostile-shape-flood: the 1,300,037 elements of a 47.6 MB
//     slide part hold about 104 MiB while it is parsed, about 80 bytes an
//     element without text.
const XMLElementCost = 256

// DecodedPixelCost is what one decoded pixel of a picture costs while the
// picture lives in the layouts most colour models decode to: four bytes,
// RGBA, NRGBA and CMYK. [PixelCost] gives every model its own.
const DecodedPixelCost = 4

// DecodedPixelCost16 is what one decoded pixel of a picture of 16 bits a
// channel costs: eight bytes, the RGBA64 and NRGBA64 layouts.
const DecodedPixelCost16 = 8

// DeepModel reports whether the Go decoders decode a picture of colour
// model m to 16 bits a channel, [DecodedPixelCost16] a pixel.
func DeepModel(m color.Model) bool {
	return m == color.RGBA64Model || m == color.NRGBA64Model
}

// PixelCost returns what one decoded pixel of colour model m costs, the
// bytes a pixel of the image type the Go decoders return for it takes: one
// for gray, alpha and paletted pictures, two for their 16-bit forms,
// three for Y'CbCr (its 4:4:4 layout, the most a JPEG or WebP picture
// samples its chroma at), four for Y'CbCr with alpha, CMYK and 8-bit RGBA,
// [DecodedPixelCost16] for 16-bit RGBA, and [DecodedPixelCost] for a model
// it does not know.
func PixelCost(m color.Model) int64 {
	if _, ok := m.(color.Palette); ok {
		return 1
	}
	switch m {
	case color.GrayModel, color.AlphaModel:
		return 1
	case color.Gray16Model, color.Alpha16Model:
		return 2
	case color.YCbCrModel:
		return 3
	case color.RGBA64Model, color.NRGBA64Model:
		return DecodedPixelCost16
	default:
		return DecodedPixelCost
	}
}
