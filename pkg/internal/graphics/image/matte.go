package image

import (
	"context"
	"image"
	"image/color"
	"log/slog"
	"maps"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// unpreblend undoes the preblending of an image whose soft mask carries a
// /Matte array, and returns img with its colours restored.
//
// Per ISO 32000-1, 11.6.5.3 such an image holds each colour already blended
// with the matte colour m by its soft-mask value a, c' = m + a(c - m), so its
// samples darken toward black, the usual matte, wherever the mask is partly
// transparent; the colour is c = m + (c' - m)/a. Like PDF.js it is recovered
// in RGB, and a pixel the mask leaves fully transparent, whose colour the
// formula does not define, is painted white. Reading the preblended samples as
// the colour darkens the image against PDF.js, its transparent margins black
// where PDF.js paints them white.
//
// An indexed image is left as it is: its components are indices, which a
// matte colour cannot be blended with.
//
// # Corpus evidence
//
//   - pdfjs: reading the preblended samples as the colour darkened 32 images of
//     eight documents against PDF.js, their transparent margins black where it
//     paints them white.
func unpreblend(ctx context.Context, doc *document.Document, name ast.PdfName, dict ast.PdfDict, cc colorContext, img image.Image) image.Image {
	smaskObj, ok := dict["SMask"]
	if !ok {
		return img
	}
	smask, err := document.ResolveStream(ctx, doc, smaskObj)
	if err != nil || smask.Dict == nil {
		return img
	}
	matte, ok := allNumbers(ctx, doc, smask.Dict["Matte"])
	if !ok {
		return img
	}
	if _, _, indexed := indexedColorSpace(ctx, doc, dict); indexed {
		return img
	}
	mr, mg, mb, ok := matteRGB(ctx, doc, cc.effectiveSpaces(ctx, doc, dict), cc, matte)
	if !ok {
		doc.Logger.Debug("ignoring a /Matte that does not fit the image's colour space",
			slog.String("image", string(name)), slog.Int("components", len(matte)))
		return img
	}
	smask.Dict = matteMaskDimensions(ctx, doc, smask.Dict, img.Bounds())
	// A soft mask holds opacity, not colour, so no default colour space or
	// output intent applies to it.
	alphaImg, err := decodeImagePixels(ctx, doc, name+" soft mask", smask, colorContext{})
	if err != nil {
		doc.Logger.Debug("decoding the soft mask to undo a matte", slog.String("image", string(name)), slog.Any("err", err))
		return img
	}
	// A soft mask with a /Matte has the dimensions of its image
	// (ISO 32000-1, 11.6.5.3); one that does not cannot be applied pixel by
	// pixel.
	b := img.Bounds()
	alpha, ok := alphaImg.(*image.Gray)
	if !ok || alpha.Bounds() != b {
		doc.Logger.Debug("ignoring a /Matte whose soft mask does not match the image", slog.String("image", string(name)))
		return img
	}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			a := alpha.GrayAt(x, y).Y
			switch m := img.(type) {
			case *image.RGBA:
				i := m.PixOffset(x, y)
				m.Pix[i] = unblend(m.Pix[i], mr, a)
				m.Pix[i+1] = unblend(m.Pix[i+1], mg, a)
				m.Pix[i+2] = unblend(m.Pix[i+2], mb, a)
			case *image.Gray:
				m.SetGray(x, y, color.Gray{Y: unblend(m.GrayAt(x, y).Y, mg, a)})
			}
		}
	}
	return img
}

// unblend recovers one component preblended with matte component m by the
// soft-mask value a, both on 0 to 255, painting a fully transparent one white.
func unblend(c, m, a uint8) uint8 {
	if a == 0 {
		return 255
	}
	v := float64(m) + (float64(c)-float64(m))*255/float64(a)
	return uint8(min(max(v+0.5, 0), 255))
}

// matteRGB converts a /Matte array, written in the image's colour space, to
// the RGB the decoded image is in.
func matteRGB(ctx context.Context, doc *document.Document, dict ast.PdfDict, cc colorContext, matte []float64) (uint8, uint8, uint8, bool) {
	comp := make([]uint8, len(matte))
	for i, v := range matte {
		comp[i] = uint8(min(max(v*255+0.5, 0), 255))
	}
	// The matte is converted as the image's samples are, so a colour-managed
	// image unblends against the colour its pixels were blended with.
	space := resolveColorSpace(ctx, doc, dict)
	if conv := newConverter(colorTransform(ctx, doc, dict["ColorSpace"], space, len(comp), cc.renderingIntent(ctx, doc, dict), cc)); conv != nil {
		r, g, b := conv.convert(comp)
		return r, g, b, true
	}
	cal, isCal := resolveCalSpace(ctx, doc, dict["ColorSpace"])
	switch {
	case len(comp) == 1 && isCal && cal.grey:
		g := cal.convertGrey(comp[0])
		return g, g, g, true
	case len(comp) == 1:
		return comp[0], comp[0], comp[0], true
	case len(comp) == 3 && isCal && !cal.grey:
		r, g, b := cal.convertRGB(comp[0], comp[1], comp[2])
		return r, g, b, true
	case len(comp) == 3:
		return comp[0], comp[1], comp[2], true
	case len(comp) == 4:
		k := 1 - float64(comp[3])/255
		ch := func(v uint8) uint8 { return uint8((1 - float64(v)/255) * k * 255) }
		return ch(comp[0]), ch(comp[1]), ch(comp[2]), true
	}
	return 0, 0, 0, false
}

// allNumbers resolves obj as a non-empty array of numbers.
func allNumbers(ctx context.Context, doc *document.Document, obj ast.PdfObject) ([]float64, bool) {
	if obj == nil {
		return nil, false
	}
	arr, err := document.ResolveArray(ctx, doc, obj)
	if err != nil || len(arr) == 0 {
		return nil, false
	}
	return numbers(ctx, doc, arr, len(arr))
}

// matteMaskDimensions returns the dictionary of a soft mask carrying a /Matte,
// its /Width or /Height taken from the image it masks where the entry is not
// a positive integer.
//
// Per ISO 32000-1, 11.6.5.3 a soft mask with a /Matte has the dimensions of its
// parent image, so an entry that states none still has exactly one value it
// can hold. Reading it as absent left the matte undone and the image dark
// where its mask is transparent. PDF.js reads such an entry the same way,
// taking the parent's dimensions for a mask whose own are invalid. A copy is
// returned, since the dictionary belongs to the document.
//
// # Corpus evidence
//
//   - pdfjs issue19611.pdf: the soft mask of IM19 states /Height /Length, and
//     leaving the matte undone painted the image near black where PDF.js
//     paints it white.
func matteMaskDimensions(ctx context.Context, doc *document.Document, dict ast.PdfDict, parent image.Rectangle) ast.PdfDict {
	var out ast.PdfDict
	for key, want := range map[ast.PdfName]int{"Width": parent.Dx(), "Height": parent.Dy()} {
		if v, err := resolveInt(ctx, doc, dict, key); err == nil && v > 0 {
			continue
		}
		if out == nil {
			out = maps.Clone(dict)
		}
		out[key] = ast.PdfInt(want)
	}
	if out == nil {
		return dict
	}
	doc.Logger.Debug("taking a matted soft mask's dimensions from its image")
	return out
}

// SoftMaskDict returns the dictionary of the soft mask dict of an image of
// width x height pixels as the decoder reads it: a mask carrying a /Matte
// takes a /Width or /Height that is no positive integer from its image, as
// [DecodeImage] takes it to undo the matte (ISO 32000-1, 11.6.5.3). A painter
// masking the image then reads the mask the decoder unmatted it with.
func SoftMaskDict(ctx context.Context, doc *document.Document, dict ast.PdfDict, width, height int) ast.PdfDict {
	if _, ok := allNumbers(ctx, doc, dict["Matte"]); !ok {
		return dict
	}
	return matteMaskDimensions(ctx, doc, dict, image.Rect(0, 0, width, height))
}
