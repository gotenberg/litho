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
	// convert, when set, turns the samples of an enumerated CIE-based space,
	// three bytes a pixel, into sRGB in place before they are read in
	// DeviceRGB.
	convert func([]byte)
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
// many colour channels as they have components, enumerated CIELab and
// ROMM-RGB over three (see [jpxCIELab] and [jpxROMM]), and an ICC profile,
// restricted (T.800, I.5.3.3) or any (ISO/IEC 15444-2, M.11.7.3.2), that
// litho can read colours through for that many channels.
//
// Enumerated CIEJab, which ISO 32000-1, 7.4.9 names, is not supported: ISO/IEC
// 15444-2, Table M.25 defines it by the CIE 97s colour appearance model, whose
// inverse needs viewing conditions the colour specification does not carry
// (its parameters are ranges and offsets alone, M.11.7.4.2). Nor are e-sRGB
// and e-sYCC: Table M.25 defines them by reference to PIMA 7667, whose integer
// encoding of the extended range, the scale and offset of each bit depth, the
// JPEG 2000 text does not restate, so litho has no grounded reading of their
// codes. Each falls to the next specification, as any other space litho
// lacks, and to the device space of its channel count when none is left.
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
		case c.Enumerated == jpx.ColourCIELab && n == 3 && c.Lab != nil:
			if convert, ok := jpxCIELab(img, colour, *c.Lab); ok {
				return jpxColour{space: space, convert: convert}, nil
			}
			doc.Logger.Debug("skipping a JPEG 2000 CIELab specification litho cannot read",
				slog.Uint64("illuminant", uint64(c.Lab.Illuminant)))
		case c.Enumerated == jpx.ColourROMMRGB && n == 3:
			return jpxColour{space: space, convert: jpxROMM()}, nil
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

// d50White is the XYZ of CIE illuminant D50, the white of ICC profile
// connection space, of ROMM RGB (ISO 22028-2) and of the default CIELab
// specification (ISO/IEC 15444-2, M.11.7.4.1).
var d50White = [3]float64{0.9642, 1.0, 0.8249}

// jpxCIELab returns the conversion of enumerated CIELab samples to sRGB, or
// false when the specification cannot be read: an illuminant other than D50
// or D65, a signed channel, or a depth the arithmetic cannot hold.
//
// Per ISO/IEC 15444-2, M.11.7.4.1 a p-bit sample v of channel c stands for
// Range[c] * (v - Offset[c]) / (2^p - 1) of L*, a* and b* in turn, under the
// illuminant IL, and a box without EP fields takes the defaults of ITU-T
// T.42 (see [jpx.DefaultLab]). OpenJPEG reads the fields the same way. The
// JPXDecode filter hands each sample on scaled onto 0 to 255, so a byte b
// stands for Range * (b/255 - Offset/(2^p - 1)). L*a*b* goes to XYZ relative
// to the illuminant by the CIE 1976 inverse, then to sRGB as a CIE-based PDF
// space does (see [adaptToD65]).
//
// The offsets are defined against unsigned samples. A signed channel has no
// stated reading, so it is not guessed at.
func jpxCIELab(img *jpx.Image, colour []jpx.Channel, params jpx.LabParams) (func([]byte), bool) {
	var white [3]float64
	switch params.Illuminant {
	case jpx.IlluminantD50:
		white = d50White
	case jpx.IlluminantD65:
		white = d65White
	default:
		return nil, false
	}
	var depths [3]int
	for i, ch := range colour[:3] {
		depths[i] = img.Depth(ch)
		if depths[i] < 1 || depths[i] > 62 || img.Signed(ch) {
			return nil, false
		}
	}
	if params.Default {
		params = jpx.DefaultLab(depths)
	}
	var lut [3][256]float64
	for c := range 3 {
		top := float64(int64(1)<<depths[c] - 1)
		for b := range 256 {
			lut[c][b] = params.Range[c] * (float64(b)/255 - params.Offset[c]/top)
		}
	}
	m := mul3(xyzToLinearS, adaptToD65(white))
	f := func(t float64) float64 {
		if t > 6.0/29.0 {
			return t * t * t
		}
		return 3 * (6.0 / 29.0) * (6.0 / 29.0) * (t - 4.0/29.0)
	}
	return func(data []byte) {
		for i := 0; i+2 < len(data); i += 3 {
			l, a, b := lut[0][data[i]], lut[1][data[i+1]], lut[2][data[i+2]]
			fy := (l + 16) / 116
			xyz := [3]float64{white[0] * f(fy+a/500), white[1] * f(fy), white[2] * f(fy-b/200)}
			rgb := apply3(m, xyz)
			data[i], data[i+1], data[i+2] = encodeSRGB(rgb[0]), encodeSRGB(rgb[1]), encodeSRGB(rgb[2])
		}
	}, true
}

// rommToXYZ takes linear ROMM RGB to XYZ relative to D50, ISO 22028-2.
var rommToXYZ = [9]float64{
	0.7977, 0.1352, 0.0313,
	0.2880, 0.7119, 0.0001,
	0.0000, 0.0000, 0.8249,
}

// jpxROMM returns the conversion of enumerated ROMM-RGB samples (ISO/IEC
// 15444-2, Table M.25) to sRGB. ISO 22028-2 defines the encoding: a
// non-linear value E' on 0 to 1 is E'/16 below 16 Et, Et being 1/512, and
// E'^1.8 above it, and the linear values mix to XYZ relative to D50 by
// [rommToXYZ]. The JPXDecode filter hands each sample on scaled onto 0 to
// 255, which is E' times 255 whatever the channel's depth.
func jpxROMM() func([]byte) {
	const et = 1.0 / 512
	var lin [256]float64
	for b := range 256 {
		e := float64(b) / 255
		if e < 16*et {
			lin[b] = e / 16
		} else {
			lin[b] = math.Pow(e, 1.8)
		}
	}
	m := mul3(xyzToLinearS, mul3(adaptToD65(d50White), rommToXYZ))
	return func(data []byte) {
		for i := 0; i+2 < len(data); i += 3 {
			rgb := apply3(m, [3]float64{lin[data[i]], lin[data[i+1]], lin[data[i+2]]})
			data[i], data[i+1], data[i+2] = encodeSRGB(rgb[0]), encodeSRGB(rgb[1]), encodeSRGB(rgb[2])
		}
	}
}
