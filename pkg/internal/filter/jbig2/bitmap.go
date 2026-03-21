package jbig2

// Bitmap represents a bi-level (1 bit-per-pixel) image.
// Pixels are packed 8 per byte, MSB first. 0 = white, 1 = black.
type Bitmap struct {
	width     int
	height    int
	rowStride int
	data      []byte
}

// NewBitmap creates a blank bitmap (all white) with the given dimensions.
func NewBitmap(width, height int) *Bitmap {
	rowStride := (width + 7) >> 3
	return &Bitmap{
		width:     width,
		height:    height,
		rowStride: rowStride,
		data:      make([]byte, height*rowStride),
	}
}

// Width returns the bitmap width in pixels.
func (b *Bitmap) Width() int { return b.width }

// Height returns the bitmap height in pixels.
func (b *Bitmap) Height() int { return b.height }

// RowStride returns the number of bytes per row.
func (b *Bitmap) RowStride() int { return b.rowStride }

// Data returns the raw pixel data. The caller must not modify it.
func (b *Bitmap) Data() []byte { return b.data }

// GetPixel returns the value of the pixel at (x, y). Returns 0 or 1.
func (b *Bitmap) GetPixel(x, y int) byte {
	byteIdx := b.ByteIndex(x, y)
	bitOffset := x & 0x07
	return (b.data[byteIdx] >> uint(7-bitOffset)) & 0x01
}

// SetPixel sets the value of the pixel at (x, y) to v (0 or 1).
func (b *Bitmap) SetPixel(x, y int, v byte) {
	byteIdx := b.ByteIndex(x, y)
	bitOffset := x & 0x07
	shift := uint(7 - bitOffset)
	if v&1 == 1 {
		b.data[byteIdx] |= 1 << shift
	} else {
		b.data[byteIdx] &^= 1 << shift
	}
}

// GetByte returns the byte at the given index.
func (b *Bitmap) GetByte(index int) byte {
	return b.data[index]
}

// GetByteAsInt returns the byte at the given index as an unsigned int.
func (b *Bitmap) GetByteAsInt(index int) int {
	return int(b.data[index]) & 0xff
}

// SetByte sets the byte at the given index.
func (b *Bitmap) SetByte(index int, v byte) {
	b.data[index] = v
}

// ByteIndex returns the byte array index containing pixel (x, y).
func (b *Bitmap) ByteIndex(x, y int) int {
	return y*b.rowStride + (x >> 3)
}

// Fill fills the entire bitmap with the given byte value.
func (b *Bitmap) Fill(v byte) {
	for i := range b.data {
		b.data[i] = v
	}
}

// CombinationOperator defines how bitmaps are composited.
type CombinationOperator int

const (
	CombinationOR      CombinationOperator = 0
	CombinationAND     CombinationOperator = 1
	CombinationXOR     CombinationOperator = 2
	CombinationXNOR    CombinationOperator = 3
	CombinationReplace CombinationOperator = 4
)

// CombineBytes combines two bytes using the specified operator.
func CombineBytes(oldByte, newByte byte, op CombinationOperator) byte {
	switch op {
	case CombinationOR:
		return newByte | oldByte
	case CombinationAND:
		return newByte & oldByte
	case CombinationXOR:
		return newByte ^ oldByte
	case CombinationXNOR:
		return ^(oldByte ^ newByte)
	case CombinationReplace:
		return newByte
	default:
		return newByte
	}
}

// Blit composites src onto dst at position (x, y) using the given operator.
func Blit(src, dst *Bitmap, x, y int, op CombinationOperator) {
	// Use pixel-by-pixel fallback for correctness. This handles all alignment
	// cases including negative offsets and partial overlaps.
	for sy := 0; sy < src.height; sy++ {
		dy := y + sy
		if dy < 0 || dy >= dst.height {
			continue
		}
		for sx := 0; sx < src.width; sx++ {
			dx := x + sx
			if dx < 0 || dx >= dst.width {
				continue
			}
			srcPx := src.GetPixel(sx, sy)
			dstPx := dst.GetPixel(dx, dy)
			result := CombineBytes(dstPx, srcPx, op)
			dst.SetPixel(dx, dy, result)
		}
	}
}

// Extract returns a sub-bitmap from src defined by the rectangle (rx, ry, rw, rh).
func Extract(src *Bitmap, rx, ry, rw, rh int) *Bitmap {
	dst := NewBitmap(rw, rh)
	for y := range rh {
		srcY := ry + y
		if srcY < 0 || srcY >= src.height {
			continue
		}
		for x := range rw {
			srcX := rx + x
			if srcX < 0 || srcX >= src.width {
				continue
			}
			px := src.GetPixel(srcX, srcY)
			if px != 0 {
				dst.SetPixel(x, y, px)
			}
		}
	}
	return dst
}
