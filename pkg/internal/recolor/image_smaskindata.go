package recolor

import (
	"context"
	"fmt"
	"log/slog"
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/filter"
	"github.com/gotenberg/litho/pkg/internal/filter/jpx"
)

// dataSoftMask is the soft mask a JPXDecode image carries in its JPEG 2000
// data (ISO 32000-1, 8.9.5, Table 89 /SMaskInData): its opacity channel on
// the image's grid, one byte a pixel, rows top to bottom.
type dataSoftMask struct {
	alpha []byte
	// premultiplied marks colour channels premultiplied by the opacity, which
	// a converter undoes before it converts them.
	premultiplied bool
	// identity reports that the image decoder reads the colour channels of a
	// JPXDecode image without /ColorSpace as they are, in DeviceGray or
	// DeviceRGB, so the painted samples are the data's and premultiplication
	// can be undone on them.
	identity bool
}

// readSMaskInData returns the soft mask a JPXDecode image of width x height
// pixels carries in its data, nil when it carries none.
//
// A converted image is re-encoded under FlateDecode or DCTDecode, neither of
// which carries an opacity channel (only JPXDecode data does, 7.4.9), so the
// mask is extracted into an explicit /SMask image instead, which Table 89
// lets a reader create from /SMaskInData 1 or 2 and which every reader of
// PDF 1.4 applies. Its source is the JPEG 2000 channel the Channel
// Definition box types as opacity or premultiplied opacity for the whole
// image (ITU-T T.800, I.5.3.6, Tables I.16 and I.17). /SMaskInData 2 says the
// colour channels were preblended (Table 89), as a premultiplied opacity
// channel does (Table I.16): either one marks them premultiplied, by an
// opacity whose zero is the data's zero, the background JPEG 2000
// premultiplies against.
//
// Refused are: a value that is no integer or one Table 89 does not define; an explicit /SMask beside a
// nonzero /SMaskInData, which Table 89 forbids, so the image names two
// masks; an opacity channel that applies to one colour only or more than one
// for the whole image, which one soft mask cannot hold; signed premultiplied
// colour channels, whose zero sits elsewhere than the filter's; and a grid
// that is not the image's. A nonzero /SMaskInData over data that holds no
// opacity channel carries no mask to keep, and the image converts without
// one, as a viewer paints it.
//
// # Corpus evidence
//
//   - pdfjs isssue18194.pdf, issue11306.pdf, issue16782.pdf: the only
//     /SMaskInData images of every corpus, all value 1 over straight RGBA
//     JP2 data, refused before and converted now. issue16782's converted
//     page renders within 0.03 levels on average of OpenJPEG's decoding
//     composited on white; issue11306's shows its page through the
//     translucent stroke, where the source draws an opaque black box in
//     litho, poppler 26.09.0 and PDFium alike, none of which applies the
//     data's mask.
func (im *Images) readSMaskInData(ctx context.Context, stream ast.PdfStream, width, height int) (*dataSoftMask, error) {
	dict := stream.Dict
	obj, present := dict["SMaskInData"]
	if !present || lastFilter(ctx, im.doc, dict) != "JPXDecode" {
		// Table 89 defines the entry for JPXDecode images only.
		return nil, nil
	}
	v, err := document.ResolveInt(ctx, im.doc, obj)
	if err != nil {
		// No integer says whether the data's mask is used.
		return nil, fmt.Errorf("%w: /SMaskInData: %w", ErrImageNotConverted, err)
	}
	if v == 0 {
		// Table 89: 0, the default, ignores the data's mask.
		return nil, nil
	}
	if v != 1 && v != 2 {
		return nil, fmt.Errorf("%w: /SMaskInData %d, which ISO 32000-1, Table 89 does not define", ErrImageNotConverted, v)
	}
	if sm, err := document.ResolveStream(ctx, im.doc, dict["SMask"]); err == nil && sm.Dict != nil {
		return nil, fmt.Errorf("%w: both /SMask and /SMaskInData %d", ErrImageNotConverted, v)
	}
	data, _, err := im.doc.DecodeStreamUpTo(ctx, stream, "JPXDecode")
	if err != nil {
		return nil, fmt.Errorf("%w: reading the JPEG 2000 data: %w", ErrImageNotConverted, err)
	}
	img, err := jpx.Describe(data, im.doc.Limits().ScaleImageCap(jpx.DefaultMaxSamples))
	if err != nil {
		return nil, fmt.Errorf("%w: reading the JPEG 2000 header: %w", ErrImageNotConverted, err)
	}
	channels, err := img.Channels()
	if err != nil {
		return nil, fmt.Errorf("%w: reading the JPEG 2000 channels: %w", ErrImageNotConverted, err)
	}
	var opacity, colour []jpx.Channel
	for _, ch := range channels {
		switch ch.Kind {
		case jpx.ChannelOpacity, jpx.ChannelPremultiplied:
			if ch.Association != 0 {
				return nil, fmt.Errorf("%w: its JPEG 2000 opacity applies to colour %d alone", ErrImageNotConverted, ch.Association)
			}
			opacity = append(opacity, ch)
		case jpx.ChannelColour:
			colour = append(colour, ch)
		}
	}
	switch len(opacity) {
	case 0:
		im.doc.Logger.Debug("recolor: /SMaskInData over JPEG 2000 data with no opacity channel", slog.Int64("value", int64(v)))
		return nil, nil
	case 1:
	default:
		return nil, fmt.Errorf("%w: %d JPEG 2000 opacity channels for the whole image", ErrImageNotConverted, len(opacity))
	}
	if w, h := img.DefaultSize(); w != width || h != height {
		return nil, fmt.Errorf("%w: the JPEG 2000 opacity is %dx%d, not %dx%d", ErrImageNotConverted, w, h, width, height)
	}
	mask := &dataSoftMask{premultiplied: v == 2 || opacity[0].Kind == jpx.ChannelPremultiplied}
	if mask.premultiplied {
		for _, ch := range colour {
			if img.Signed(ch) {
				return nil, fmt.Errorf("%w: signed JPEG 2000 colour premultiplied by its opacity", ErrImageNotConverted)
			}
		}
		mask.identity = identityReading(img, len(colour))
	}
	mask.alpha, err = img.Render(ctx, opacity, filter.JPXChannelByte)
	if err != nil {
		return nil, fmt.Errorf("%w: decoding the JPEG 2000 opacity: %w", ErrImageNotConverted, err)
	}
	return mask, nil
}

// identityReading reports whether the image decoder reads the n colour
// channels of a JPXDecode image without /ColorSpace as the data holds them:
// its first colour specification, the one ISO 32000-1, 7.4.9 tries first, is
// enumerated sRGB over three channels or greyscale over one, or there is
// none and one or three channels read in DeviceGray or DeviceRGB. Any other
// reading, an ICC profile, sYCC, CIELab or CMYK, is no linear map that keeps
// the data's zero, so premultiplication cannot be undone on its result.
func identityReading(img *jpx.Image, n int) bool {
	if n != 1 && n != 3 {
		return false
	}
	if len(img.Colours) == 0 {
		return true
	}
	c := img.Colours[0]
	if c.ICC != nil {
		return false
	}
	return (c.Enumerated == jpx.ColourSRGB && n == 3) || (c.Enumerated == jpx.ColourGreyscale && n == 1)
}

// unpremultiply divides 8-bit colour samples, pixels of comps components in
// rows, by the opacity of each pixel (ITU-T T.800, Table I.16), up to rows
// whole rows of width pixels. A pixel of no opacity has no colour, and takes
// zero, what it was premultiplied to.
func unpremultiply(samples []byte, alpha []byte, width, rows, comps int) {
	for i := range width * rows {
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

// attachDataMask gives a converted image's dictionary dict, from which
// /SMaskInData is dropped, the explicit /SMask that carries the soft mask
// its source's data held, when it held one.
func (im *Images) attachDataMask(dict ast.PdfDict, m *dataSoftMask, width, height int) error {
	if m == nil {
		return nil
	}
	mask, err := m.softMaskStream(width, height)
	if err != nil {
		return err
	}
	dict["SMask"] = im.alloc.Alloc(mask)
	return nil
}

// softMaskStream returns the explicit soft mask image of a data soft mask,
// DeviceGray at 8 bits (ISO 32000-1, 11.6.5.3, Table 146), compressed
// losslessly whatever the colour's encoding, since an opacity is a shape and
// a JPEG would blur its edges.
func (m *dataSoftMask) softMaskStream(width, height int) (ast.PdfStream, error) {
	encoded, parms, _, err := encodeSamples(m.alpha, width, 1, 8, -1, false)
	if err != nil {
		return ast.PdfStream{}, fmt.Errorf("%w: compressing the soft mask: %w", ErrImageNotConverted, err)
	}
	dict := ast.PdfDict{
		"Type":             ast.PdfName("XObject"),
		"Subtype":          ast.PdfName("Image"),
		"Width":            ast.PdfInt(width),
		"Height":           ast.PdfInt(height),
		"ColorSpace":       ast.PdfName("DeviceGray"),
		"BitsPerComponent": ast.PdfInt(8),
		"Filter":           ast.PdfName("FlateDecode"),
	}
	if parms != nil {
		dict["DecodeParms"] = parms
	}
	return ast.NewStream(dict, encoded), nil
}
