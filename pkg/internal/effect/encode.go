package effect

import (
	"context"
	"fmt"
	"image"
	"image/color"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// Encode returns the RGBA surface s as a picture of the page model placed
// over its grid, nil when every pixel of s is transparent. The picture is
// DeviceRGB, 8 bits a channel, its colours straight, not premultiplied, as
// a soft mask expects; its alpha, when any pixel is not opaque, is a
// DeviceGray soft mask of 8 bits (ISO 32000-1, 11.6.5.3). Both streams are
// Flate-compressed (7.4.4) with no predictor ([typeset.EncodeImage]). A
// channel is unpremultiplied as c times 255 over the alpha, rounded half
// up, in integers: the same bytes on every machine. The picture has no
// structure parent, which the caller sets or leaves an artifact.
//
// It charges the decoded picture to the conversion's budget while it is
// encoded, as [typeset.ChargeImage] does, and leaves s as it is. It
// returns an error wrapping [ErrFormat] for a coverage surface, and the
// errors of [typeset.EncodeImage].
func Encode(ctx context.Context, s *Surface) (*typeset.Image, error) {
	if s.F != RGBA {
		return nil, fmt.Errorf("encode: %w", ErrFormat)
	}
	if err := typeset.ChargeImage(ctx, s.W, s.H, color.NRGBAModel); err != nil {
		return nil, err
	}
	img := image.NewNRGBA(image.Rect(0, 0, s.W, s.H))
	empty := true
	for i := 0; i < len(s.Pix); i += 4 {
		a := s.Pix[i+3]
		if a == 0 {
			continue
		}
		empty = false
		q := img.Pix[i : i+4 : i+4]
		for c := range 3 {
			q[c] = uint8(min((uint32(s.Pix[i+c])*255+uint32(a)/2)/uint32(a), 255)) //nolint:gosec // at most 255
		}
		q[3] = a
	}
	if empty {
		typeset.ReleaseImage(ctx, img)
		return nil, nil
	}
	src, err := typeset.EncodeImage(ctx, img)
	if err != nil {
		return nil, err
	}
	k := s.Grid.Scale
	return &typeset.Image{Rect: typeset.Rect{X: s.Grid.X, Y: s.Grid.Y, Width: float64(s.W) / k, Height: float64(s.H) / k}, Source: src}, nil
}
