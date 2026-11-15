package imagefile

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"

	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// Errors a read fails with.
var (
	// ErrMalformed is returned, wrapped, for a file whose bytes do not
	// read as a file of its format.
	ErrMalformed = errors.New("malformed image file")
	// ErrUnsupported is returned, wrapped, for a file of a format, or a
	// variant of one, this package does not read.
	ErrUnsupported = errors.New("unsupported image file")
)

// Options is how a file's pictures are read.
type Options struct {
	// PDF14 writes for PDF 1.4, which has no JPXDecode filter and no
	// 16-bit component (ISO 32000-1, 7.4.9 and Table 89, both PDF 1.5):
	// a JPEG 2000 file is decoded and written losslessly at 8 bits, a
	// 16-bit sample at 8 bits, an ICC profile of a major version past 2 is
	// left out.
	PDF14 bool
	// MaxPictures caps the pictures of a file read; a file of more fails
	// with an error wrapping limits.ErrTooManyPages. Zero is no cap.
	MaxPictures int
	// Warn receives what a file states that the output does not keep, one
	// sentence each, omitted set for content left out rather than printed
	// differently; nil for none.
	Warn func(msg string, omitted bool)
}

// warn reports msg, content printed differently, through o.Warn.
func (o *Options) warn(msg string) {
	if o.Warn != nil && msg != "" {
		o.Warn(msg, false)
	}
}

// omit reports msg, content left out, through o.Warn.
func (o *Options) omit(msg string) {
	if o.Warn != nil {
		o.Warn(msg, true)
	}
}

// Picture is one picture of a file, ready to be placed on a page.
type Picture struct {
	// Source holds the picture's samples, as the file stores them or
	// decoded and written losslessly.
	Source *typeset.ImageSource
	// Width and Height are the picture's size in pixels, as stored,
	// before Orientation turns it.
	Width, Height int
	// XDPI and YDPI are the resolution the file states, in pixels an
	// inch, along the stored rows and columns; both 0 when it states none
	// or one [Resolution] refuses.
	XDPI, YDPI float64
	// Orientation is the EXIF or TIFF orientation, 1 to 8 (see [EXIF]).
	Orientation int
	// Description is what the format defines as a description of the
	// picture, "" for none: a PNG text chunk of keyword Description (PNG
	// 3rd edition, 11.3.3.2).
	Description string
}

// Info is what a file states of itself as a whole.
type Info struct {
	// Title is what the format defines as the title of the picture, ""
	// for none: a PNG text chunk of keyword Title (PNG 3rd edition,
	// 11.3.3.2), the EXIF ImageDescription of a JPEG, a TIFF or a WebP
	// file (EXIF 2.32, 4.6.5).
	Title string
}

// Read reads the pictures of data, a file of format f, and hands each to
// fn in the order a reader shows them: the one picture of a PNG, a JPEG,
// a BMP, a JPEG 2000 file; the first frame of a GIF or a WebP file,
// composed on its canvas as a viewer shows it; every page of a TIFF file,
// its reduced-resolution images left out; the largest picture of an ICO
// file. fn may keep the picture; it is decoded before fn is called and
// its decoded pixels are dropped before the next one is decoded.
//
// Every picture is checked against the pixel cap of ctx's memory budget
// before any byte of it is decoded, and its decoded pixels are charged to
// that budget while they live. Read returns an error wrapping
// [ErrMalformed] for bytes that do not read as their format,
// [ErrUnsupported] for a variant this package does not read, one
// wrapping limits.ErrPictureTooLarge for a picture past the pixel cap,
// limits.ErrTooManyPages past o.MaxPictures, the budget's refusal, fn's
// error, and ctx's error when ctx ends first. An SVG file is not read
// here: it fails with [ErrUnsupported].
func Read(ctx context.Context, data []byte, f Format, o Options, fn func(*Picture) error) (Info, error) {
	if err := ctx.Err(); err != nil {
		return Info{}, err
	}
	switch f {
	case PNG:
		return readPNGFile(ctx, data, &o, fn)
	case JPEG:
		return readJPEGFile(ctx, data, &o, fn)
	case GIF:
		return Info{}, readGIF(ctx, data, fn)
	case WebP:
		return readWebP(ctx, data, &o, fn)
	case BMP:
		return Info{}, readBMPFile(ctx, data, fn)
	case TIFF:
		return readTIFF(ctx, data, &o, fn)
	case JPEG2000:
		return Info{}, readJPX(ctx, data, &o, fn)
	case ICO:
		return Info{}, readICO(ctx, data, &o, fn)
	case Unknown, SVG:
	}
	return Info{}, fmt.Errorf("%w: %s", ErrUnsupported, f)
}

// decoded decodes a picture of w by h pixels of colour model m with
// decode, charging its pixels to ctx's budget while it lives, and writes
// it with encode. The charge is given back once the picture is written.
func decoded(ctx context.Context, w, h int, m color.Model, decode func() (image.Image, error), o encodeOptions) (*typeset.ImageSource, error) {
	mem := limits.BudgetFrom(ctx)
	charged, err := mem.ChargePicture(w, h, m)
	if err != nil {
		return nil, err
	}
	defer mem.Release(charged)
	img, err := decode()
	if err != nil {
		return nil, err
	}
	if b := img.Bounds(); b.Dx() != w || b.Dy() != h {
		return nil, fmt.Errorf("%w: a picture of %d by %d pixels decodes to %d by %d", ErrMalformed, w, h, b.Dx(), b.Dy())
	}
	return encode(ctx, img, o)
}

// checkSize checks a picture of w by h pixels against the pixel cap of
// ctx's budget before anything of it is allocated.
func checkSize(ctx context.Context, w, h int) error {
	return limits.BudgetFrom(ctx).CheckPicture(w, h)
}

// malformed wraps err, a decoder's, in [ErrMalformed] with what was read,
// keeping a limit or a context error as it is.
func malformed(what string, err error) error {
	if limits.Stops(err) || errors.Is(err, limits.ErrPictureTooLarge) || errors.Is(err, ErrUnsupported) || errors.Is(err, ErrMalformed) {
		return fmt.Errorf("%s: %w", what, err)
	}
	return fmt.Errorf("%w: %s: %w", ErrMalformed, what, err)
}
