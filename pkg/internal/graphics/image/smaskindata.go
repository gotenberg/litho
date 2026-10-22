package image

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"log/slog"
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/filter"
	"github.com/gotenberg/litho/pkg/internal/filter/jpx"
	"github.com/gotenberg/litho/pkg/internal/panicerr"
)

// DataSoftMask is the soft mask a JPXDecode image carries in its JPEG 2000
// data (ISO 32000-1, 8.9.5, Table 89 /SMaskInData): its opacity channel on
// the image's grid.
type DataSoftMask struct {
	// Alpha holds one byte a pixel, rows top to bottom, 0 transparent and
	// 255 opaque.
	Alpha []byte
	// Premultiplied marks colour channels premultiplied by the opacity,
	// against the data's zero, which a reader undoes before it reads them
	// as colour (see [Unpremultiply]).
	Premultiplied bool
}

// ReadSMaskInData returns the soft mask a JPXDecode image of width x height
// pixels carries in its data, nil when it carries none: /SMaskInData absent
// or 0, a last filter other than JPXDecode, or data that holds no opacity
// channel. It does not look at /SMask, whose precedence each caller decides.
//
// Its source is the JPEG 2000 channel the Channel Definition box types as
// opacity or premultiplied opacity for the whole image (ITU-T T.800, I.5.3.6,
// Tables I.16 and I.17), read at the 8 bits the JPXDecode filter hands its
// colour channels on at (7.4.9). /SMaskInData 2 says the colour channels were
// preblended (Table 89), as a premultiplied opacity channel does (Table I.16):
// either one marks them premultiplied, by an opacity whose zero is the data's
// zero, the background JPEG 2000 premultiplies against.
//
// It returns an error wrapping [ErrDataSoftMask] for: a value that is no
// integer or one Table 89 does not define; an opacity channel that applies to
// one colour only, or more than one for the whole image, which one soft mask
// cannot hold; signed premultiplied colour channels, whose zero sits
// elsewhere than the filter's; and an opacity grid that is not the image's.
//
// # Corpus evidence
//
//   - pdfjs isssue18194.pdf, issue11306.pdf, issue16782.pdf: the only
//     /SMaskInData images of every corpus, all value 1 over straight RGBA JP2
//     data.
func ReadSMaskInData(ctx context.Context, doc *document.Document, stream ast.PdfStream, width, height int) (*DataSoftMask, error) {
	dict := stream.Dict
	obj, present := dict["SMaskInData"]
	if !present || imageFilterName(ctx, doc, dict) != "JPXDecode" {
		// Table 89 defines the entry for JPXDecode images only.
		return nil, nil
	}
	v, err := document.ResolveInt(ctx, doc, obj)
	if err != nil {
		return nil, fmt.Errorf("%w: /SMaskInData: %w", ErrDataSoftMask, err)
	}
	if v == 0 {
		// Table 89: 0, the default, ignores the data's mask.
		return nil, nil
	}
	if v != 1 && v != 2 {
		return nil, fmt.Errorf("%w: /SMaskInData %d, which ISO 32000-1, Table 89 does not define", ErrDataSoftMask, v)
	}
	data, _, err := doc.DecodeStreamUpTo(ctx, stream, "JPXDecode")
	if err != nil {
		return nil, fmt.Errorf("%w: reading the JPEG 2000 data: %w", ErrDataSoftMask, err)
	}
	img, err := jpx.Describe(data, doc.Limits().ScaleImageCap(jpx.DefaultMaxSamples))
	if err != nil {
		return nil, fmt.Errorf("%w: reading the JPEG 2000 header: %w", ErrDataSoftMask, err)
	}
	channels, err := img.Channels()
	if err != nil {
		return nil, fmt.Errorf("%w: reading the JPEG 2000 channels: %w", ErrDataSoftMask, err)
	}
	var opacity, colour []jpx.Channel
	for _, ch := range channels {
		switch ch.Kind {
		case jpx.ChannelOpacity, jpx.ChannelPremultiplied:
			if ch.Association != 0 {
				return nil, fmt.Errorf("%w: its JPEG 2000 opacity applies to colour %d alone", ErrDataSoftMask, ch.Association)
			}
			opacity = append(opacity, ch)
		case jpx.ChannelColour:
			colour = append(colour, ch)
		}
	}
	switch len(opacity) {
	case 0:
		doc.Logger.Debug("/SMaskInData over JPEG 2000 data with no opacity channel", slog.Int64("value", int64(v)))
		return nil, nil
	case 1:
	default:
		return nil, fmt.Errorf("%w: %d JPEG 2000 opacity channels for the whole image", ErrDataSoftMask, len(opacity))
	}
	if w, h := img.DefaultSize(); w != width || h != height {
		return nil, fmt.Errorf("%w: the JPEG 2000 opacity is %dx%d, not %dx%d", ErrDataSoftMask, w, h, width, height)
	}
	mask := &DataSoftMask{Premultiplied: v == 2 || opacity[0].Kind == jpx.ChannelPremultiplied}
	if mask.Premultiplied {
		for _, ch := range colour {
			if img.Signed(ch) {
				return nil, fmt.Errorf("%w: signed JPEG 2000 colour premultiplied by its opacity", ErrDataSoftMask)
			}
		}
	}
	mask.Alpha, err = img.Render(ctx, opacity, filter.JPXChannelByte)
	if err != nil {
		// Render runs below the document, so a panic its workers recovered
		// is logged here, with the document logger.
		return nil, fmt.Errorf("%w: decoding the JPEG 2000 opacity: %w", ErrDataSoftMask, panicerr.Log(doc.Logger, err))
	}
	return mask, nil
}

// Unpremultiply divides 8-bit colour samples, pixels of comps components in
// rows, by the opacity of each pixel (ITU-T T.800, Table I.16), over the
// first pixels pixels, or as many as samples and alpha hold. A pixel of no
// opacity has no colour, and takes zero, what it was premultiplied to.
func Unpremultiply(samples, alpha []byte, pixels, comps int) {
	if comps < 1 {
		return
	}
	n := min(pixels, len(samples)/comps, len(alpha))
	for i := range n {
		a := int(alpha[i])
		px := samples[i*comps : (i+1)*comps]
		for c, s := range px {
			if a == 0 {
				px[c] = 0
				continue
			}
			px[c] = byte(min(math.Round(float64(s)*255/float64(a)), 255))
		}
	}
}

// paintedDataMask returns the soft mask a painted JPXDecode image carries in
// its data, nil when none applies. An explicit /SMask takes precedence:
// Table 89 says it should not be specified beside a nonzero /SMaskInData, and
// a reader that meets both keeps the mask the dictionary names, as one
// without /SMaskInData support does. A stencil mask paints no colour of its
// own (8.9.6.2), so its data's opacity has nothing to apply to. A mask that
// cannot be read is left out, and the image paints opaque, as it did before
// any reader applied /SMaskInData.
func paintedDataMask(ctx context.Context, doc *document.Document, name ast.PdfName, stream ast.PdfStream) *DataSoftMask {
	dict := stream.Dict
	if _, present := dict["SMaskInData"]; !present {
		return nil
	}
	if sm, err := document.ResolveStream(ctx, doc, dict["SMask"]); err == nil && sm.Dict != nil {
		return nil
	}
	// An /ImageMask that cannot be resolved counts as absent, as the decode
	// treats it.
	if isMask, _ := document.ResolveBool(ctx, doc, dict["ImageMask"]); isMask {
		return nil
	}
	width, err := resolveInt(ctx, doc, dict, "Width")
	if err != nil {
		return nil
	}
	height, err := resolveInt(ctx, doc, dict, "Height")
	if err != nil {
		return nil
	}
	if validateImageDimensions(width, height, doc.Limits().MaxImagePixels) != nil {
		return nil
	}
	m, err := ReadSMaskInData(ctx, doc, stream, width, height)
	if err != nil {
		doc.Logger.Debug("painting a JPEG 2000 image without the soft mask of its data",
			slog.String("image", string(name)), slog.Any("err", err))
		return nil
	}
	return m
}

// withDataAlpha returns img, the colour of an image, with the opacity of the
// soft mask its data carries as its alpha, straight: an *image.NRGBA. A
// decoder that dropped the data's opacity painted the colour of fully
// transparent pixels, black where the data premultiplies or leaves them
// unset.
func withDataAlpha(img image.Image, m *DataSoftMask) image.Image {
	b := img.Bounds()
	if len(m.Alpha) < b.Dx()*b.Dy() {
		return img
	}
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := range b.Dy() {
		row := out.Pix[y*out.Stride:]
		for x := range b.Dx() {
			o := 4 * x
			switch src := img.(type) {
			case *image.RGBA:
				i := src.PixOffset(b.Min.X+x, b.Min.Y+y)
				row[o], row[o+1], row[o+2] = src.Pix[i], src.Pix[i+1], src.Pix[i+2]
			case *image.Gray:
				v := src.Pix[src.PixOffset(b.Min.X+x, b.Min.Y+y)]
				row[o], row[o+1], row[o+2] = v, v, v
			default:
				c := color.NRGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
				row[o], row[o+1], row[o+2] = c.R, c.G, c.B
			}
			row[o+3] = m.Alpha[y*b.Dx()+x]
		}
	}
	return out
}
