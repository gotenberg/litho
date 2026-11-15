package imagefile

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"  // registers GIF with image.Decode
	_ "image/jpeg" // registers JPEG with image.Decode
	_ "image/png"  // registers PNG with image.Decode
	"io"

	_ "golang.org/x/image/bmp"  // registers BMP with image.Decode
	"golang.org/x/image/tiff"   // registers TIFF with image.Decode
	_ "golang.org/x/image/webp" // registers WebP with image.Decode
)

// DecodeConfig returns the colour model and size of the picture data and
// its format's name, as image.DecodeConfig does for the PNG, JPEG, GIF,
// BMP, TIFF and WebP decoders it registers.
func DecodeConfig(data []byte) (image.Config, string, error) {
	return image.DecodeConfig(bytes.NewReader(data))
}

// Decode decodes the picture data as image.Decode does, through the
// decoders [DecodeConfig] names, its size checked by the caller first. A
// TIFF file is decoded through the guard of its first page: a strip of
// more rows than the page is read as the page's rows, which TIFF 6.0,
// section 8 makes the same image, so that golang.org/x/image/tiff bounds
// what one strip inflates to by the page's pixels; a tile far larger than
// the page is refused.
func Decode(data []byte) (image.Image, string, error) {
	if Sniff(data) != TIFF {
		return image.Decode(bytes.NewReader(data))
	}
	t, err := readTIFFHeader(data)
	if err != nil {
		return nil, "", malformed("TIFF header", err)
	}
	entries, _, err := t.ifd(t.first)
	if err != nil {
		return nil, "", malformed("TIFF", err)
	}
	view, err := t.guardedView(t.first, entries)
	if err != nil {
		return nil, "", err
	}
	img, err := tiff.Decode(view)
	return img, "tiff", err
}

// guardedView returns [tiffData.blockView]'s view of the page whose IFD
// lies at off.
func (t *tiffData) guardedView(off int64, entries []ifdEntry) (*io.SectionReader, error) {
	w, h := int(t.value(entries, tagImageWidth, 0)), int(t.value(entries, tagImageLength, 0)) // fits int: 32-bit values, checked positive next
	if w <= 0 || h <= 0 {
		return nil, malformed("TIFF", fmt.Errorf("a page of %d by %d pixels", w, h))
	}
	view, _, err := t.blockView(off, entries, w, h)
	return view, err
}
