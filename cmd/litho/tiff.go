package main

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"io"
	"math"
	"os"
)

// errTIFFTooLarge reports a raster whose bytes do not fit the 32-bit offsets
// of a baseline TIFF file.
var errTIFFTooLarge = errors.New("image too large for a TIFF file")

// TIFF tag numbers and field types, from the TIFF 6.0 specification, section
// 2 (Image File Directory) and the baseline and separated-image fields of
// sections 8 and 16.
const (
	tiffShort    = 3
	tiffLong     = 4
	tiffRational = 5

	tagImageWidth                = 256
	tagImageLength               = 257
	tagBitsPerSample             = 258
	tagCompression               = 259
	tagPhotometricInterpretation = 262
	tagStripOffsets              = 273
	tagSamplesPerPixel           = 277
	tagRowsPerStrip              = 278
	tagStripByteCounts           = 279
	tagXResolution               = 282
	tagYResolution               = 283
	tagPlanarConfiguration       = 284
	tagResolutionUnit            = 296
	tagInkSet                    = 332
)

// tiffResolutionScale is the denominator a resolution is written over, so a
// fractional resolution keeps two decimals.
const tiffResolutionScale = 100

// writeCMYKTIFF writes img to the file name as an uncompressed separated TIFF
// (TIFF 6.0, section 16): four 8-bit inks a pixel, cyan, magenta, yellow and
// black, in one strip, its resolution dpi pixels per inch.
func writeCMYKTIFF(name string, img *image.CMYK, dpi float64) (err error) {
	f, err := os.Create(name) //nolint:gosec // the output path is the caller's argument
	if err != nil {
		return fmt.Errorf("creating %s: %w", name, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("closing %s: %w", name, cerr)
		}
	}()
	w := bufio.NewWriter(f)
	if err := encodeCMYKTIFF(w, img, dpi); err != nil {
		return fmt.Errorf("encoding %s: %w", name, err)
	}
	if err := w.Flush(); err != nil {
		return fmt.Errorf("writing %s: %w", name, err)
	}
	return nil
}

// encodeCMYKTIFF writes img to w as [writeCMYKTIFF] describes: a
// little-endian header, one image file directory, its out-of-line values,
// then the pixels.
func encodeCMYKTIFF(w io.Writer, img *image.CMYK, dpi float64) error {
	b := img.Bounds()
	width, height := b.Dx(), b.Dy()
	pixels := uint64(width) * uint64(height) * 4 //nolint:gosec // image dimensions are never negative
	const entries = 14
	const ifdOffset = 8
	const ifdSize = 2 + entries*12 + 4
	const bitsOffset = ifdOffset + ifdSize
	const xresOffset = bitsOffset + 8
	const yresOffset = xresOffset + 8
	const dataOffset = yresOffset + 8
	// The pixel bytes bound both sides, each at least 1 in a rendered page.
	if pixels > math.MaxUint32-dataOffset {
		return fmt.Errorf("%w: %dx%d", errTIFFTooLarge, width, height)
	}
	res := uint32(0)
	if v := math.Round(dpi * tiffResolutionScale); v > 0 && v <= math.MaxUint32 {
		res = uint32(v)
	}
	le := binary.LittleEndian
	head := make([]byte, 0, dataOffset)
	head = append(head, 'I', 'I', 42, 0)
	head = le.AppendUint32(head, ifdOffset)
	head = le.AppendUint16(head, entries)
	entry := func(tag, typ uint16, count, value uint32) {
		head = le.AppendUint16(head, tag)
		head = le.AppendUint16(head, typ)
		head = le.AppendUint32(head, count)
		if typ == tiffShort && count == 1 {
			// A SHORT value sits left-justified in the 4-byte field.
			head = le.AppendUint16(head, uint16(value)) //nolint:gosec // every SHORT written here is a small constant
			head = le.AppendUint16(head, 0)
			return
		}
		head = le.AppendUint32(head, value)
	}
	// Section 2 sorts the entries of a directory by tag.
	entry(tagImageWidth, tiffLong, 1, uint32(width))   //nolint:gosec // bounded above
	entry(tagImageLength, tiffLong, 1, uint32(height)) //nolint:gosec // bounded above
	entry(tagBitsPerSample, tiffShort, 4, bitsOffset)
	entry(tagCompression, tiffShort, 1, 1)
	// 5 is Separated, the inks of InkSet 1: cyan, magenta, yellow, black.
	entry(tagPhotometricInterpretation, tiffShort, 1, 5)
	entry(tagStripOffsets, tiffLong, 1, dataOffset)
	entry(tagSamplesPerPixel, tiffShort, 1, 4)
	entry(tagRowsPerStrip, tiffLong, 1, uint32(height)) //nolint:gosec // bounded above
	entry(tagStripByteCounts, tiffLong, 1, uint32(pixels))
	entry(tagXResolution, tiffRational, 1, xresOffset)
	entry(tagYResolution, tiffRational, 1, yresOffset)
	entry(tagPlanarConfiguration, tiffShort, 1, 1)
	// 2 is inches.
	entry(tagResolutionUnit, tiffShort, 1, 2)
	entry(tagInkSet, tiffShort, 1, 1)
	head = le.AppendUint32(head, 0)
	for range 4 {
		head = le.AppendUint16(head, 8)
	}
	for range 2 {
		head = le.AppendUint32(head, res)
		head = le.AppendUint32(head, tiffResolutionScale)
	}
	if _, err := w.Write(head); err != nil {
		return err
	}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		i := img.PixOffset(b.Min.X, y)
		if _, err := w.Write(img.Pix[i : i+4*width]); err != nil {
			return err
		}
	}
	return nil
}
