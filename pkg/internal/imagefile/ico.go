package imagefile

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
)

// icoEntry is one entry of an icon directory: an image's size as the
// directory states it, its bit count, and where its data lies.
type icoEntry struct {
	width, height int
	bits          int
	data          []byte
}

// icoDirEntryBytes is the size of one ICONDIRENTRY.
const icoDirEntryBytes = 16

// readICO reads the largest image of an ICO file, the Windows icon
// directory: a 6-byte header of a reserved 0, type 1 and a count, then
// one 16-byte entry an image, its width and height (0 for 256), its colour
// count, its planes, its bit count, its size and its offset. A viewer
// shows one image of an icon, and the largest: Chromium's ICO decoder
// orders the entries by size, then by bit count, and draws the first
// (third_party/blink/renderer/platform/image-decoders/ico). An entry is a
// PNG file or a DIB, a BMP without its file header whose height counts
// the colour bitmap and the AND mask after it; a DIB's alpha is its own
// for 32 bits a pixel, unless every pixel's alpha is 0, else the AND
// mask's (1 transparent). An entry whose data lies outside the file is
// left out; a file without one fails with [ErrMalformed]. An icon states
// no resolution.
func readICO(ctx context.Context, data []byte, o *Options, fn func(*Picture) error) error {
	count := int(binary.LittleEndian.Uint16(data[4:]))
	if 6+icoDirEntryBytes*count > len(data) {
		return malformed("ICO", fmt.Errorf("a directory of %d entries in %d bytes", count, len(data)))
	}
	var best *icoEntry
	for i := range count {
		e := data[6+icoDirEntryBytes*i:]
		w, h := int(e[0]), int(e[1])
		if w == 0 {
			w = 256
		}
		if h == 0 {
			h = 256
		}
		size, off := int64(binary.LittleEndian.Uint32(e[8:])), int64(binary.LittleEndian.Uint32(e[12:]))
		if size < 1 || off < 0 || off+size > int64(len(data)) {
			o.omit(fmt.Sprintf("icon image %d lies outside the file and is left out", i+1))
			continue
		}
		entry := icoEntry{width: w, height: h, bits: int(binary.LittleEndian.Uint16(e[6:])), data: data[off : off+size]}
		if best == nil || entry.width*entry.height > best.width*best.height ||
			entry.width*entry.height == best.width*best.height && entry.bits > best.bits {
			best = &entry
		}
	}
	if best == nil {
		return malformed("ICO", errors.New("no image lies inside the file"))
	}
	var pic *Picture
	var err error
	if bytes.HasPrefix(best.data, []byte("\x89PNG\r\n\x1a\n")) {
		m := readPNGMeta(best.data)
		pic, err = readPNG(ctx, best.data, m, o)
	} else {
		pic, err = readDIB(ctx, best.data)
	}
	if err != nil {
		return fmt.Errorf("ICO: %w", err)
	}
	pic.Orientation = 1
	return fn(pic)
}

// dibHeaderBytes is the size of a BITMAPINFOHEADER; dibV4HeaderBytes the
// size of a BITMAPV4HEADER, which golang.org/x/image/bmp reads the alpha of a
// 32-bit bitmap under.
const (
	dibHeaderBytes   = 40
	dibV4HeaderBytes = 108
)

// readDIB decodes the DIB of an icon entry: it gives the bitmap the BMP
// file header it lacks and the height of its colour bitmap, decodes it,
// and takes its alpha from the AND mask that follows it.
func readDIB(ctx context.Context, dib []byte) (*Picture, error) {
	if len(dib) < dibHeaderBytes || binary.LittleEndian.Uint32(dib) != dibHeaderBytes {
		return nil, fmt.Errorf("%w: an icon bitmap without a BITMAPINFOHEADER", ErrUnsupported)
	}
	width := int64(int32(binary.LittleEndian.Uint32(dib[4:])))      //nolint:gosec // G115: a signed 32-bit LONG
	height := int64(int32(binary.LittleEndian.Uint32(dib[8:])) / 2) //nolint:gosec // G115: as above; the colour bitmap is the upper half
	bits := int64(binary.LittleEndian.Uint16(dib[14:]))
	colors := int64(binary.LittleEndian.Uint32(dib[32:]))
	if width <= 0 || height <= 0 {
		return nil, malformed("icon bitmap", fmt.Errorf("%d by %d pixels", width, height))
	}
	if err := checkSize(ctx, int(width), int(height)); err != nil { // fits int: positive 32-bit values
		return nil, err
	}
	if bits == 8 && colors == 0 {
		colors = 256
	}
	if bits != 8 {
		colors = 0
	}
	if colors > 256 {
		return nil, malformed("icon bitmap", fmt.Errorf("a palette of %d colours", colors))
	}
	header := int64(dibHeaderBytes)
	if bits == 32 {
		header = dibV4HeaderBytes
	}
	pixels := dibHeaderBytes + 4*colors
	rowBytes := (width*bits + 31) / 32 * 4
	colorBytes := rowBytes * height
	maskRow := (width + 31) / 32 * 4
	if pixels+colorBytes > int64(len(dib)) {
		return nil, malformed("icon bitmap", errors.New("the colour bitmap runs past the entry"))
	}
	// The BMP file: its 14-byte header, the DIB header with the colour
	// bitmap's height, padded to a BITMAPV4HEADER for 32 bits a pixel,
	// the palette and the colour bitmap.
	bmpFile := make([]byte, 0, bmpInfoOffset+header+4*colors+colorBytes)
	bmpFile = append(bmpFile, 'B', 'M')
	bmpFile = binary.LittleEndian.AppendUint32(bmpFile, uint32(bmpInfoOffset+header+4*colors+colorBytes)) //nolint:gosec // G115: within an entry of the file
	bmpFile = append(bmpFile, 0, 0, 0, 0)
	bmpFile = binary.LittleEndian.AppendUint32(bmpFile, uint32(bmpInfoOffset+header+4*colors)) //nolint:gosec // G115: as above
	hdr := make([]byte, header)
	copy(hdr, dib[:dibHeaderBytes])
	binary.LittleEndian.PutUint32(hdr, uint32(header))     //nolint:gosec // G115: 40 or 108
	binary.LittleEndian.PutUint32(hdr[8:], uint32(height)) //nolint:gosec // G115: a positive 32-bit value
	bmpFile = append(bmpFile, hdr...)
	bmpFile = append(bmpFile, dib[dibHeaderBytes:pixels+colorBytes]...)
	var mask []byte
	if end := pixels + colorBytes + maskRow*height; end <= int64(len(dib)) {
		mask = dib[pixels+colorBytes : end]
	}
	w, h := int(width), int(height) // fits int: positive 32-bit values, checked against the pixel cap
	apply := func(img image.Image) image.Image {
		return iconAlpha(img, mask, int(maskRow), bits == 32) // fits int: at most the entry's length
	}
	pic, err := readBMP(ctx, bmpFile, apply)
	if err != nil {
		return nil, err
	}
	if pic.Width != w || pic.Height != h {
		return nil, malformed("icon bitmap", fmt.Errorf("%d by %d pixels read as %d by %d", w, h, pic.Width, pic.Height))
	}
	return pic, nil
}

// iconAlpha returns img with its alpha: a 32-bit bitmap's own unless every
// pixel's is 0, else the AND mask's, rows of 1 bit a pixel from the
// bottom, 1 transparent, when the entry holds one.
func iconAlpha(img image.Image, mask []byte, maskRow int, own bool) image.Image {
	b := img.Bounds()
	out := image.NewNRGBA(b)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			out.SetNRGBA(x, y, straight8(img, x, y))
		}
	}
	if own {
		for i := 3; i < len(out.Pix); i += 4 {
			if out.Pix[i] != 0 {
				return out
			}
		}
	}
	if mask == nil {
		for i := 3; i < len(out.Pix); i += 4 {
			out.Pix[i] = 0xFF
		}
		return out
	}
	h := b.Dy()
	for y := range h {
		row := mask[(h-1-y)*maskRow:]
		for x := range b.Dx() {
			a := uint8(0xFF)
			if row[x/8]&(0x80>>(x%8)) != 0 {
				a = 0
			}
			out.Pix[out.PixOffset(b.Min.X+x, b.Min.Y+y)+3] = a
		}
	}
	return out
}
