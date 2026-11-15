package imagefile

import (
	"context"
	"image"
	"image/draw"
)

// OrientImage returns img with the EXIF orientation o applied (see
// [EXIF]), img itself for 1 or a value out of range. The pixels are copied
// into a grey image for a grey source and an RGBA one otherwise. It checks
// ctx once a row and returns its error when it ends first.
func OrientImage(ctx context.Context, img image.Image, o int) (image.Image, error) {
	if o < 2 || o > 8 {
		return img, nil
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	var dst draw.Image
	if _, ok := img.(*image.Gray); ok {
		dst = image.NewGray(image.Rect(0, 0, dw, dh))
	} else {
		dst = image.NewRGBA(image.Rect(0, 0, dw, dh))
	}
	for y := range h {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		for x := range w {
			var dx, dy int
			switch o {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			default: // 8
				dx, dy = y, w-1-x
			}
			dst.Set(dx, dy, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return dst, nil
}
