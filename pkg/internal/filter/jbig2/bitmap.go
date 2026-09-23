package jbig2

import "fmt"

// maxBitmapPixels bounds the area of any bitmap this package allocates: a
// page, a region, or an auxiliary buffer. It is an anti-DoS guard, not
// spec-mandated: T.88 allows dimensions up to 2^32-1 each, so a 20-byte
// header could otherwise demand a multi-terabyte buffer. It matches
// maxImagePixels in pkg/internal/graphics/image, the cap every decoded image
// already meets, so no page this package produces is one the image decoder
// would then refuse. At one bit per pixel it is 12.5 MB.
const maxBitmapPixels = 100_000_000

// Bitmap is a bi-level image stored one bit per pixel, rows top to bottom,
// each row packed most significant bit first and padded to a whole byte. A 1
// bit is foreground: black in the typical interpretation of ITU-T T.88, 5.6
// NOTE 1, and what MMR black decodes to (6.2.6).
type Bitmap struct {
	Width  int
	Height int
	// Stride is the number of bytes per row.
	Stride int
	Data   []byte
}

// newBitmap allocates a width x height bitmap with every pixel 0. It refuses
// negative dimensions and any area above maxBitmapPixels before allocating.
func newBitmap(width, height int) (*Bitmap, error) {
	if width < 0 || height < 0 {
		return nil, fmt.Errorf("%w: bitmap of %dx%d pixels", ErrMalformed, width, height)
	}
	if areaExceeds(int64(width), int64(height), maxBitmapPixels) {
		return nil, fmt.Errorf("%w: bitmap of %dx%d pixels exceeds %d", ErrLimit, width, height, maxBitmapPixels)
	}
	stride := (width + 7) / 8
	return &Bitmap{Width: width, Height: height, Stride: stride, Data: make([]byte, stride*height)}, nil
}

// areaExceeds reports whether width x height is above limit, without forming
// the product: T.88 dimensions reach 2^32-1 each, whose product overflows an
// int64.
func areaExceeds(width, height, limit int64) bool {
	return width > 0 && height > limit/width
}

// pixel returns the pixel at (x, y), or 0 outside the bitmap. T.88, 6.2.5.2
// gives every pixel outside the bounds of a bitmap the value 0.
func (b *Bitmap) pixel(x, y int) uint32 {
	if x < 0 || y < 0 || x >= b.Width || y >= b.Height {
		return 0
	}
	return uint32(b.Data[y*b.Stride+x>>3]>>(7-uint(x&7))) & 1
}

// setPixel sets the pixel at (x, y) to v, which is 0 or 1. Coordinates outside
// the bitmap are ignored.
func (b *Bitmap) setPixel(x, y int, v uint32) {
	if x < 0 || y < 0 || x >= b.Width || y >= b.Height {
		return
	}
	i := y*b.Stride + x>>3
	mask := byte(0x80) >> uint(x&7)
	if v != 0 {
		b.Data[i] |= mask
	} else {
		b.Data[i] &^= mask
	}
}

// fill sets every pixel to v, which is 0 or 1.
func (b *Bitmap) fill(v uint32) {
	var fillByte byte
	if v != 0 {
		fillByte = 0xFF
	}
	for i := range b.Data {
		b.Data[i] = fillByte
	}
}

// combination operators, ITU-T T.88, 7.4.1.5. The page default operator of
// 7.4.8.5 uses the first four values.
const (
	combineOr = iota
	combineAnd
	combineXor
	combineXnor
	combineReplace
)

// combine draws src onto b with its top left corner at (x, y), merging each
// pixel with the operator op. Pixels of src that fall outside b are clipped.
func (b *Bitmap) combine(src *Bitmap, x, y int64, op int) error {
	if op < combineOr || op > combineReplace {
		return fmt.Errorf("%w: combination operator %d", ErrMalformed, op)
	}
	for sy := range src.Height {
		dy := y + int64(sy)
		if dy < 0 || dy >= int64(b.Height) {
			continue
		}
		for sx := range src.Width {
			dx := x + int64(sx)
			if dx < 0 || dx >= int64(b.Width) {
				continue
			}
			s := src.pixel(sx, sy)
			d := b.pixel(int(dx), int(dy))
			var v uint32
			switch op {
			case combineOr:
				v = d | s
			case combineAnd:
				v = d & s
			case combineXor:
				v = d ^ s
			case combineXnor:
				v = 1 ^ (d ^ s)
			case combineReplace:
				v = s
			}
			b.setPixel(int(dx), int(dy), v)
		}
	}
	return nil
}

// area returns a copy of the width x height part of b whose top left corner
// is (x, y). Pixels of that area outside b are 0.
func (b *Bitmap) area(x, y int64, width, height int) *Bitmap {
	out := &Bitmap{Width: width, Height: height, Stride: (width + 7) / 8}
	out.Data = make([]byte, out.Stride*height)
	for ay := range height {
		for ax := range width {
			if refPixel(b, x+int64(ax), y+int64(ay)) != 0 {
				out.setPixel(ax, ay, 1)
			}
		}
	}
	return out
}
