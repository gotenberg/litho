package image

import (
	"context"
	"fmt"
	"log/slog"
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/filter/jpx"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
)

// jpxColour is how litho reads the colours of a JPXDecode image whose
// dictionary has no /ColorSpace.
type jpxColour struct {
	// space is the device space the samples are read in: DeviceGray,
	// DeviceRGB or DeviceCMYK.
	space string
	// transform reads them through the ICC profile the JPEG 2000 data
	// carries, and is nil otherwise.
	transform *icc.Transform
	// ycc marks sYCC samples, which litho turns into sRGB before reading
	// them in DeviceRGB, and neutral holds the byte the JPXDecode filter
	// gives a zero Cb and Cr.
	ycc     bool
	neutral [2]float64
}

// jpxColorSpace returns how to read the colours of a JPXDecode image whose
// dictionary has no /ColorSpace. ISO 32000-1, 7.4.9 then uses the colour
// specifications of the JPEG 2000 data, trying them from the highest
// precedence and best approximation down, and when none is supported,
// DeviceGray, DeviceRGB or DeviceCMYK by its number of colour channels, 1, 3
// or 4. The JPXDecode filter hands the samples on at 8 bits per colour
// channel.
//
// Supported are the enumerated sRGB, greyscale, sYCC and CMYK spaces over as
// many colour channels as they have components, and an ICC profile, restricted
// (T.800, I.5.3.3) or any (ISO/IEC 15444-2, M.11.7.3.2), that litho can read
// colours through for that many channels.
//
// Enumerated CIEJab, which ISO 32000-1, 7.4.9 names, is not supported: ISO/IEC
// 15444-2, Table M.25 defines it by the CIE 97s colour appearance model, whose
// inverse needs viewing conditions the colour specification does not carry
// (its parameters are ranges and offsets alone, M.11.7.4.2). It falls to the
// next specification, as any other space litho lacks.
func jpxColorSpace(ctx context.Context, doc *document.Document, stream ast.PdfStream, intent icc.Intent) (jpxColour, error) {
	data, _, err := doc.DecodeStreamUpTo(ctx, stream, "JPXDecode")
	if err != nil {
		return jpxColour{}, fmt.Errorf("reading the JPEG 2000 data: %w", err)
	}
	img, err := jpx.Describe(data, doc.Limits().ScaleImageCap(jpx.DefaultMaxSamples))
	if err != nil {
		return jpxColour{}, fmt.Errorf("reading the JPEG 2000 header: %w", err)
	}
	channels, err := img.Channels()
	if err != nil {
		return jpxColour{}, fmt.Errorf("reading the JPEG 2000 channels: %w", err)
	}
	var colour []jpx.Channel
	for _, ch := range channels {
		if ch.Kind == jpx.ChannelColour {
			colour = append(colour, ch)
		}
	}
	n := len(colour)
	space := map[int]string{1: "DeviceGray", 3: "DeviceRGB", 4: "DeviceCMYK"}[n]
	if space == "" {
		return jpxColour{}, fmt.Errorf("%w: %d colour channels, which no device colour space has", jpx.ErrUnsupported, n)
	}
	for _, c := range img.Colours {
		switch {
		case c.ICC != nil:
			if t := embeddedTransform(doc, c.ICC, n, intent); t != nil {
				return jpxColour{space: space, transform: t}, nil
			}
		case c.Enumerated == jpx.ColourSRGB && n == 3,
			c.Enumerated == jpx.ColourGreyscale && n == 1,
			c.Enumerated == jpx.ColourCMYK && n == 4:
			return jpxColour{space: space}, nil
		case c.Enumerated == jpx.ColourSYCC && n == 3:
			if neutral, ok := chromaNeutral(img, colour); ok {
				return jpxColour{space: space, ycc: true, neutral: neutral}, nil
			}
		default:
			doc.Logger.Debug("skipping a JPEG 2000 colour specification litho does not support",
				slog.Int("enumerated", c.Enumerated), slog.Int("channels", n))
		}
	}
	return jpxColour{space: space}, nil
}

// chromaNeutral returns the byte the JPXDecode filter gives a zero Cb and Cr
// sample, the second and third colour channels: the middle of each channel's
// range, 2^(p-1) of a p-bit value, scaled onto 0 to 255 as the filter scales
// it (T.800, I.5.3.3). A signed channel stores zero as 0, which the filter
// moves to that same middle.
func chromaNeutral(img *jpx.Image, colour []jpx.Channel) ([2]float64, bool) {
	var neutral [2]float64
	for i, ch := range colour[1:3] {
		p := img.Depth(ch)
		if p < 1 || p > 62 {
			return neutral, false
		}
		top := int64(1)<<p - 1
		neutral[i] = float64((int64(1)<<(p-1)*255 + top/2) / top)
	}
	return neutral, true
}

// yccToRGB turns sYCC samples, three bytes a pixel, into sRGB in place, by
// the inverse of the luma and chroma transform IEC 61966-2-1/Amd 1 defines
// sYCC with. Values past the sRGB range clip.
func yccToRGB(data []byte, neutral [2]float64) {
	clip := func(v float64) byte {
		return byte(math.Round(min(max(v, 0), 255)))
	}
	for i := 0; i+2 < len(data); i += 3 {
		y := float64(data[i])
		cb, cr := float64(data[i+1])-neutral[0], float64(data[i+2])-neutral[1]
		data[i] = clip(y + 1.402*cr)
		data[i+1] = clip(y - 0.34414*cb - 0.71414*cr)
		data[i+2] = clip(y + 1.772*cb)
	}
}
