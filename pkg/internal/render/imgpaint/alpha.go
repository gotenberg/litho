package imgpaint

import (
	"fmt"
	"image"
	"image/color"

	"github.com/gotenberg/litho/pkg/internal/filter"
)

// maxColorKeyComponents bounds the components a colour-key mask compares per
// pixel. ISO 32000-1, Annex C, Table C.1 limits a DeviceN space to 32
// colorants, the most components an image can carry, so no conforming image
// exceeds it.
const maxColorKeyComponents = 32

// Alpha is an opacity plane on a sample grid of its own: Width × Height
// values, rows top to bottom, 0 transparent and 255 opaque. It maps onto the
// unit square of the image it masks whatever its dimensions (ISO 32000-1,
// 8.9.6.3 and 11.6.5.3). Interpolate is the plane's own /Interpolate, used
// when its grid differs from the image's.
type Alpha struct {
	Width, Height int
	A             []uint8
	Interpolate   bool
}

// valid reports whether a describes a raster it holds the values of.
func (a *Alpha) valid() error {
	if a.Width <= 0 || a.Height <= 0 || int64(len(a.A))/int64(a.Height) < int64(a.Width) {
		return fmt.Errorf("%w: %dx%d plane holds %d values", ErrBadMask, a.Width, a.Height, len(a.A))
	}
	return nil
}

// at returns the value of sample (i, j) on 0 to 1.
func (a *Alpha) at(i, j int) float32 {
	return float32(a.A[j*a.Width+i]) * (1.0 / 255)
}

// SoftMask returns the plane of a decoded soft-mask image, its grey level
// read as opacity (ISO 32000-1, 11.6.5.3). img is the decoder's reading of the
// /SMask stream, its /Decode applied.
func SoftMask(img image.Image, interpolate bool) *Alpha {
	return greyPlane(img, interpolate, false)
}

// StencilMask returns the plane of a decoded stencil mask, whether an
// /ImageMask image painted by [DrawStencil] or the stencil an image's /Mask
// entry names. The decoder reads a stencil as DeviceGray with its /Decode
// applied, so a 0 sample paints and a 1 sample masks out (ISO 32000-1, 8.9.6.2
// and 8.9.6.3): opacity is the inverted grey level.
func StencilMask(img image.Image, interpolate bool) *Alpha {
	return greyPlane(img, interpolate, true)
}

func greyPlane(img image.Image, interpolate, invert bool) *Alpha {
	b := img.Bounds()
	out := &Alpha{Width: b.Dx(), Height: b.Dy(), A: make([]uint8, b.Dx()*b.Dy()), Interpolate: interpolate}
	g, isGray := img.(*image.Gray)
	for y := range out.Height {
		for x := range out.Width {
			var v uint8
			if isGray {
				v = g.Pix[g.PixOffset(b.Min.X+x, b.Min.Y+y)]
			} else {
				v = color.GrayModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.Gray).Y
			}
			if invert {
				v = 255 - v
			}
			out.A[y*out.Width+x] = v
		}
	}
	return out
}

// ColorKey returns the plane of a colour-key mask (ISO 32000-1, 8.9.6.4): a
// pixel whose every component falls inside its [min max] pair of ranges is
// masked out, any other pixel painted.
//
// data holds the image's samples as the stream's filters leave them: rows of
// width × components samples of bpc bits, each row padded to a byte (8.9.3).
// The comparison is on these raw values, before /Decode, as 8.9.6.4 states
// the ranges in the range 0 to 2^bpc − 1. An indexed image compares its
// indices. A row data does not hold is painted, as the decoder paints short
// data's missing rows rather than refusing them. maxPixels bounds width ×
// height, since both come from the file.
func ColorKey(data []byte, width, height, components, bpc int, ranges []int, maxPixels int64) (*Alpha, error) {
	if width <= 0 || height <= 0 || int64(width) > maxPixels/int64(height) {
		return nil, fmt.Errorf("%w: colour key over a %dx%d image", ErrBadMask, width, height)
	}
	if components <= 0 || components > maxColorKeyComponents || len(ranges) != 2*components {
		return nil, fmt.Errorf("%w: %d colour-key ranges for %d components", ErrBadMask, len(ranges), components)
	}
	switch bpc {
	case 1, 2, 4, 8, 16:
	default:
		// ISO 32000-1, 8.9.5.1, Table 89 permits these depths only.
		return nil, fmt.Errorf("%w: colour key over %d bits per component", ErrBadMask, bpc)
	}
	rowBytes64 := (int64(width)*int64(components)*int64(bpc) + 7) / 8
	out := &Alpha{Width: width, Height: height, A: make([]uint8, width*height)}
	for i := range out.A {
		out.A[i] = 255
	}
	rows := min(int64(height), int64(len(data))/rowBytes64)
	rowBytes := int(rowBytes64)
	mask := byte(1<<min(bpc, 8) - 1)
	for y := range int(rows) {
		row := data[y*rowBytes : (y+1)*rowBytes]
		for x := range width {
			keyed := true
			for c := range components {
				s := x*components + c
				var v int
				switch bpc {
				case 8:
					v = int(row[s])
				case 16:
					v = int(row[2*s])<<8 | int(row[2*s+1])
				default:
					v = int(filter.BitSample(row, s, bpc, mask))
				}
				if v < ranges[2*c] || v > ranges[2*c+1] {
					keyed = false
					break
				}
			}
			if keyed {
				out.A[y*width+x] = 0
			}
		}
	}
	return out, nil
}
