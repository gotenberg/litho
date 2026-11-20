package imagefile

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"

	"github.com/gotenberg/litho/pkg/internal/filter/jpx"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// readJPX reads a JPEG 2000 file (ITU-T T.800 | ISO/IEC 15444-1), a JP2
// file or a bare codestream. Its colours are what its Colour
// Specification box states: sRGB or greyscale by enumeration, or an ICC
// profile; a bare codestream's are gray for one component and sRGB for
// three. Any other colour space, a CMYK, sYCC or CIELab one, fails with
// [ErrUnsupported].
//
// A JP2 file is written as it is, decoded by JPXDecode (ISO 32000-1,
// 7.4.9), when the PDF version has that filter, PDF 1.5 on, and the file
// holds what PDF/A-2 lets JPXDecode carry (ISO 19005-2, 6.2.8.3): one
// colour specification, one or three colour channels of one depth, no
// palette and no opacity channel. Any other is decoded and written
// losslessly at 8 bits a sample, with its first opacity channel as a soft
// mask: a component deeper than 8 bits is rounded to 8. A JPEG 2000 file
// states its resolution in an optional box litho does not read.
func readJPX(ctx context.Context, data []byte, o *Options, fn func(*Picture) error) error {
	mem := limits.BudgetFrom(ctx)
	img, err := jpx.Describe(data, limits.ScaleImageCap(mem.Limits(), jpx.DefaultMaxSamples))
	if errors.Is(err, jpx.ErrLimit) {
		return fmt.Errorf("%w: JPEG 2000: %w", limits.ErrPictureTooLarge, err)
	}
	if err != nil {
		return malformed("JPEG 2000", err)
	}
	w, h := img.DefaultSize()
	if err := checkSize(ctx, w, h); err != nil {
		return err
	}
	chs, err := img.Channels()
	if err != nil {
		return malformed("JPEG 2000", err)
	}
	var colours, opacity []jpx.Channel
	palette := false
	for _, ch := range chs {
		palette = palette || ch.PaletteColumn >= 0
		switch {
		case ch.Kind == jpx.ChannelColour && ch.Association >= 1:
			colours = append(colours, ch)
		case ch.Kind == jpx.ChannelOpacity:
			opacity = append(opacity, ch)
		}
	}
	n := len(colours)
	var spec jpx.Colour
	if len(img.Colours) > 0 {
		spec = img.Colours[0]
	}
	var icc *profile
	switch {
	case spec.ICC != nil:
		var reason string
		icc, reason = readProfile(spec.ICC, o.PDF14)
		if icc == nil || icc.components != n {
			return fmt.Errorf("%w: JPEG 2000 colours in a profile litho does not take: %s", ErrUnsupported, reason)
		}
	case len(img.Colours) == 0, spec.Enumerated == jpx.ColourSRGB && n == 3, spec.Enumerated == jpx.ColourGreyscale && n == 1:
	default:
		return fmt.Errorf("%w: JPEG 2000 colours of enumerated space %d over %d channels", ErrUnsupported, spec.Enumerated, n)
	}
	if n != 1 && n != 3 {
		return fmt.Errorf("%w: JPEG 2000 of %d colour channels", ErrUnsupported, n)
	}
	if !o.PDF14 && jpx.IsJP2(data) && len(img.Colours) == 1 && !palette && len(opacity) == 0 && sameDepth(img, colours) {
		src, err := typeset.JPXImage(ctx, data, w, h, typeset.ImageLayout{Components: n})
		if err != nil {
			return malformed("JPEG 2000", err)
		}
		return fn(&Picture{Source: src, Width: w, Height: h, Orientation: 1})
	}
	render := colours
	if len(opacity) > 0 {
		render = append(append([]jpx.Channel{}, colours...), opacity[0])
	}
	src, err := decoded(ctx, w, h, color.NRGBAModel, func() (image.Image, error) {
		pix, err := img.Render(ctx, render, to8)
		if err != nil {
			return nil, malformed("JPEG 2000", err)
		}
		return jpxPicture(pix, w, h, n, len(render) > n), nil
	}, encodeOptions{icc: icc})
	if err != nil {
		return err
	}
	return fn(&Picture{Source: src, Width: w, Height: h, Orientation: 1})
}

// sameDepth reports whether every channel of chs is unsigned and of one
// depth.
func sameDepth(img *jpx.Image, chs []jpx.Channel) bool {
	for _, ch := range chs {
		if img.Signed(ch) || img.Depth(ch) != img.Depth(chs[0]) {
			return false
		}
	}
	return true
}

// to8 maps a sample v of precision bits, signed or not, to 8 bits,
// rounding to the nearest level: a signed sample is offset by half its
// range first (ITU-T T.800, G.1.2 reads it centred on zero).
func to8(v int32, precision int, signed bool) byte {
	if precision < 1 {
		return 0
	}
	top := int64(1)<<precision - 1
	x := int64(v)
	if signed {
		x += int64(1) << (precision - 1)
	}
	x = min(max(x, 0), top)
	return byte((x*255 + top/2) / top) //nolint:gosec // G115: at most 255
}

// jpxPicture builds the picture of the interleaved 8-bit pixels pix, n
// colour channels and, when alpha is set, one opacity channel after them.
func jpxPicture(pix []byte, w, h, n int, alpha bool) image.Image {
	stride := n
	if alpha {
		stride++
	}
	if n == 1 && !alpha {
		return &image.Gray{Pix: pix, Stride: w, Rect: image.Rect(0, 0, w, h)}
	}
	out := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := range w * h {
		p := pix[stride*i:]
		q := out.Pix[4*i:]
		if n == 1 {
			q[0], q[1], q[2] = p[0], p[0], p[0]
		} else {
			q[0], q[1], q[2] = p[0], p[1], p[2]
		}
		q[3] = 0xFF
		if alpha {
			q[3] = p[n]
		}
	}
	return out
}
