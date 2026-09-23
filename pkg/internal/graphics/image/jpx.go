package image

import (
	"context"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/filter/jpx"
)

// jpxColorSpace returns the colour space of a JPXDecode image whose dictionary
// has no /ColorSpace. ISO 32000-1, 7.4.9 then uses the colour specification of
// the JPEG 2000 data, and when no supported one is found, DeviceGray,
// DeviceRGB or DeviceCMYK by its number of colour channels, 1, 3 or 4.
//
// The enumerated spaces map to their device counterparts, which is how the
// JPXDecode filter hands the samples on: 8 bits per colour channel. An ICC
// profile is read by its channel count, the fallback 7.4.9 gives, since the
// decoded samples feed pixel access rather than colour management. sYCC and
// CIEJab would need a conversion litho does not make, so they are refused
// rather than read as RGB.
func jpxColorSpace(ctx context.Context, doc *document.Document, stream ast.PdfStream) (string, error) {
	data, _, err := doc.DecodeStreamUpTo(ctx, stream, "JPXDecode")
	if err != nil {
		return "", fmt.Errorf("reading the JPEG 2000 data: %w", err)
	}
	img, err := jpx.Describe(data)
	if err != nil {
		return "", fmt.Errorf("reading the JPEG 2000 header: %w", err)
	}
	channels, err := img.Channels()
	if err != nil {
		return "", fmt.Errorf("reading the JPEG 2000 channels: %w", err)
	}
	n := 0
	for _, ch := range channels {
		if ch.Kind == jpx.ChannelColour {
			n++
		}
	}
	byCount := map[int]string{1: "DeviceGray", 3: "DeviceRGB", 4: "DeviceCMYK"}
	var space string
	switch img.Colour.Enumerated {
	case jpx.ColourSRGB:
		space = "DeviceRGB"
	case jpx.ColourGreyscale:
		space = "DeviceGray"
	case jpx.ColourCMYK:
		space = "DeviceCMYK"
	case 0:
		space = byCount[n]
	default:
		return "", fmt.Errorf("%w: JPEG 2000 enumerated colour space %d", jpx.ErrUnsupported, img.Colour.Enumerated)
	}
	if space == "" || byCount[n] != space {
		return "", fmt.Errorf("%w: %d colour channels for colour space %s", jpx.ErrUnsupported, n, space)
	}
	return space, nil
}
