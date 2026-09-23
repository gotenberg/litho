package filter

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/filter/jpx"
)

// decodeJPX decodes a JPXDecode stream (ISO 32000-1, 7.4.9) into one byte per
// colour channel per pixel, pixel by pixel in rows, on the image's default
// grid (ITU-T T.800, I.5.3.1.1). dict is the image dictionary: its /ColorSpace
// entry, if present, names the space the samples are read in and ISO 32000-1,
// 7.4.9 says the colour specification in the JPEG 2000 data is then ignored.
//
// Opacity channels are left out: ISO 32000-1, 7.4.9 matches the colour
// channels, not the channels, to the components of the colour space, and a
// soft mask carried in the data (SMaskInData) is a separate image.
func decodeJPX(ctx context.Context, data []byte, dict ast.PdfDict) ([]byte, error) {
	img, err := jpx.Decode(ctx, data)
	if err != nil {
		if errors.Is(err, jpx.ErrUnsupported) {
			return nil, fmt.Errorf("%w: JPXDecode: %w", ErrUnsupportedFilter, err)
		}
		return nil, fmt.Errorf("JPXDecode: %w", err)
	}
	channels, err := jpxColourChannels(img, dict)
	if err != nil {
		return nil, fmt.Errorf("JPXDecode: %w", err)
	}
	w, h := img.DefaultSize()
	indexed := isIndexedSpace(dict["ColorSpace"])
	out := make([]byte, w*h*len(channels))
	for i, ch := range channels {
		samples, precision, signed, err := img.ChannelSamples(ch)
		if err != nil {
			return nil, fmt.Errorf("JPXDecode: %w", err)
		}
		samples = img.Resample(samples, ch.Component)
		for p, v := range samples {
			out[p*len(channels)+i] = jpxByte(v, precision, signed, indexed)
		}
	}
	return out, nil
}

// jpxColourChannels returns the colour channels of img in the order of the
// colours they carry (T.800, I.5.3.6). Under an /Indexed colour space the
// samples are palette indices (ISO 32000-1, 8.6.6.3), so a channel the JP2
// file would look up in its own palette yields the component instead: the
// PDF's colour space governs and the JPEG 2000 palette, part of its colour
// specification (T.800, I.3.4), is ignored (ISO 32000-1, 7.4.9).
func jpxColourChannels(img *jpx.Image, dict ast.PdfDict) ([]jpx.Channel, error) {
	all, err := img.Channels()
	if err != nil {
		return nil, err
	}
	var colour []jpx.Channel
	for _, ch := range all {
		if ch.Kind == jpx.ChannelColour {
			colour = append(colour, ch)
		}
	}
	if len(colour) == 0 {
		return nil, fmt.Errorf("%w: no colour channel", jpx.ErrMalformed)
	}
	slices.SortStableFunc(colour, func(a, b jpx.Channel) int { return a.Association - b.Association })
	if !isIndexedSpace(dict["ColorSpace"]) {
		return colour, nil
	}
	var indices []jpx.Channel
	for _, ch := range colour {
		ch.PaletteColumn = -1
		if !slices.ContainsFunc(indices, func(c jpx.Channel) bool { return c.Component == ch.Component }) {
			indices = append(indices, ch)
		}
	}
	return indices, nil
}

// isIndexedSpace reports whether a /ColorSpace value, resolved as the document
// resolves it for a JPXDecode stream, names an Indexed space.
func isIndexedSpace(cs ast.PdfObject) bool {
	arr, ok := cs.(ast.PdfArray)
	if !ok || len(arr) == 0 {
		return false
	}
	name, ok := arr[0].(ast.PdfName)
	return ok && (name == "Indexed" || name == "I")
}

// jpxByte converts a sample of the given depth to the 8 bits the JPXDecode
// filter hands on. An index stays as it is: an Indexed space has at most 256
// entries. Other samples scale linearly onto 0 to 255, the mapping T.800,
// I.5.3.3 gives for an enumerated colour space, after a signed sample is moved
// to the unsigned range.
func jpxByte(v int32, precision int, signed, indexed bool) byte {
	if indexed {
		return byte(min(max(v, 0), 255))
	}
	x := int64(v)
	if signed {
		x += 1 << (precision - 1)
	}
	top := int64(1)<<precision - 1
	x = min(max(x, 0), top)
	return byte(min(max((x*255+top/2)/top, 0), 255))
}
