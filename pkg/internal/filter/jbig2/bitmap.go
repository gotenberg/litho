package jbig2

import (
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/intconv"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// DefaultMaxBitmapBytes bounds the bytes of any bitmap this package decodes
// under the default image budget: a page, a region, a symbol, or an auxiliary
// buffer, counted as [Bitmap] holds it, each row padded to a whole byte. It is
// an anti-DoS guard, not spec-mandated: T.88 allows dimensions up to 2^32-1
// each, so a 20-byte header could otherwise demand a multi-terabyte buffer.
//
// It counts bytes, not pixels, because the image budget,
// [limits.DefaultMaxImagePixels], is set for samples of a byte or more, and a
// bitmap of one bit a pixel costs an eighth of an 8-bit image of the same
// area: a pixel cap refuses a bi-level page eight times smaller than the
// memory the budget grants. A bitmap may hold as many bytes as an 8-bit image
// at the budget holds samples, 100 MB. Padding counts, so a bitmap narrower
// than a byte pays a byte a pixel and is never admitted at more pixels than
// the budget names.
//
// The bytes bound memory, not time. Decoding a coded bitmap costs time in
// proportion to its pixels, so [Decode] charges it a unit a pixel against a
// total of maxDecodedAreas caps, 400 million units under the default budget:
// a coded region reaches 400 million pixels in 50 MB, and a crafted stream
// costs the time it did when the cap counted pixels. A text or halftone
// region is only filled and drawn into, eight pixels at once, so it is
// charged its bytes and reaches the whole cap.
//
// A bitmap past the pixel budget is larger than the image decoder admits as
// an image, and it refuses one before decoding. A mask past the budget is
// read row by row instead and reduced to the size it is drawn at, so its codec
// must deliver the whole bitmap once. CCITTFaxDecode, the other codec of one
// bit a pixel, is bounded by the bytes it outputs as well, under the stream
// budget.
//
// [Decode] takes the cap scaled with the document's budget
// ([limits.Limits.ScaleImageCap]).
//
// # Corpus evidence
//
//   - pdfjs issue18896.pdf: a 19926x16012 JBIG2 /Mask, 319 million pixels in
//     39.9 MB, which a cap of 100 million pixels refused, so its image was left
//     out where poppler draws it masked.
//   - pdfjs bomb_giant.pdf: three 100000x100000 pages, 1.25 GB each, refused.
//   - All corpora: the other 276 JBIG2 streams that decoded under the pixel
//     cap decode to the same bytes.
const DefaultMaxBitmapBytes = limits.DefaultMaxImagePixels

// maxAllocBytes bounds the bytes of any bitmap [newBitmap] allocates, whatever
// cap [Decode] was given: the bitmap cap at the ceiling of the image budget,
// 1 GiB. Every caller checks its bitmap against the cap Decode was given
// first, so this only keeps an allocation bounded should one caller not.
const maxAllocBytes = limits.MaxImagePixelsCeiling

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
// negative dimensions and any bitmap above maxAllocBytes before allocating.
func newBitmap(width, height int) (*Bitmap, error) {
	if width < 0 || height < 0 {
		return nil, fmt.Errorf("%w: bitmap of %dx%d pixels", ErrMalformed, width, height)
	}
	if bitmapExceeds(int64(width), int64(height), maxAllocBytes) {
		return nil, fmt.Errorf("%w: bitmap of %dx%d pixels exceeds %d bytes", ErrLimit, width, height, maxAllocBytes)
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

// bitmapExceeds reports whether a width x height [Bitmap] holds more than
// limit bytes, its rows padded to whole bytes, without forming the product as
// areaExceeds does. A width below one holds no byte: every caller refuses a
// negative one on its own.
func bitmapExceeds(width, height, limit int64) bool {
	return areaExceeds(rowBytes(width), height, limit)
}

// rowBytes returns the bytes one row of a width-pixel [Bitmap] holds, or 0 for
// a width below one.
func rowBytes(width int64) int64 {
	if width < 1 {
		return 0
	}
	return (width + 7) / 8
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
//
// It works a destination byte at a time, eight pixels at once, so drawing
// costs time in proportion to the bytes it covers, as the budget charges it
// (budget.chargeBuffer). The pixels of a byte outside the drawn span keep
// their value, so the padding bits of src never reach b.
func (b *Bitmap) combine(src *Bitmap, x, y int64, op int) error {
	if op < combineOr || op > combineReplace {
		return fmt.Errorf("%w: combination operator %d", ErrMalformed, op)
	}
	x0, x1 := max(x, 0), min(x+int64(src.Width), int64(b.Width))
	y0, y1 := max(y, 0), min(y+int64(src.Height), int64(b.Height))
	if x0 >= x1 || y0 >= y1 {
		return nil
	}
	for dy := y0; dy < y1; dy++ {
		srow := src.Data[(dy-y)*int64(src.Stride) : (dy-y+1)*int64(src.Stride)]
		drow := b.Data[dy*int64(b.Stride) : (dy+1)*int64(b.Stride)]
		for j := x0 >> 3; j <= (x1-1)>>3; j++ {
			lo, hi := max(x0, j*8)-j*8, min(x1, j*8+8)-j*8
			mask := byte(0xFF>>lo) & byte(0xFF<<(8-hi))
			s := rowBits(srow, j*8-x)
			d := drow[j]
			var v byte
			switch op {
			case combineOr:
				v = d | s
			case combineAnd:
				v = d & s
			case combineXor:
				v = d ^ s
			case combineXnor:
				v = ^(d ^ s)
			case combineReplace:
				v = s
			}
			drow[j] = d&^mask | v&mask
		}
	}
	return nil
}

// rowBits returns the eight pixels of row starting at pixel bit, most
// significant bit first, reading pixels outside row as 0.
func rowBits(row []byte, bit int64) byte {
	// An arithmetic shift floors a negative bit, and its low three bits are
	// then its offset in that byte.
	i, sh := bit>>3, uint(bit&7) // fits uint: below 8
	at := func(k int64) byte {
		if k < 0 || k >= int64(len(row)) {
			return 0
		}
		return row[k]
	}
	return at(i)<<sh | at(i+1)>>(8-sh)
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

// columns returns a copy of the columns x to x+width-1 of b, all its rows.
func (b *Bitmap) columns(x, width int64) (*Bitmap, error) {
	if x < 0 || width < 0 || x+width > int64(b.Width) {
		return nil, fmt.Errorf("%w: columns %d to %d of a %d-pixel-wide bitmap", ErrMalformed, x, x+width-1, b.Width)
	}
	out, err := newBitmap(intconv.Clamp(width), b.Height)
	if err != nil {
		return nil, err
	}
	for y := range b.Height {
		for cx := range intconv.Clamp(width) {
			if b.pixel(intconv.Clamp(x)+cx, y) != 0 {
				out.setPixel(cx, y, 1)
			}
		}
	}
	return out, nil
}
