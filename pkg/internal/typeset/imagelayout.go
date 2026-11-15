package typeset

import (
	"bytes"
	"context"
	"fmt"
	"image/color"
	"image/jpeg"
	"io"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/filter/jpx"
)

// MaxICCBytes caps the ICC profile an [ImageLayout] carries. Anti-DoS
// guard, not spec-mandated: ICC.1 bounds a profile at 2^32 bytes, and the
// profile of a camera or a scanner, a matrix and curves or a CMYK table,
// holds a few kilobytes to a few hundred; the largest press profiles stay
// under 4 MiB.
const MaxICCBytes = 4 << 20

// maxPaletteColors is the largest palette an indexed image holds: an
// Indexed colour space's hival is at most 255 (ISO 32000-1, 8.6.6.3).
const maxPaletteColors = 256

// ImageLayout describes the samples of an [ImageSource] that are not 8-bit
// DeviceGray or DeviceRGB, or the colour space a JPEG or JPEG 2000 file is
// read in: the image dictionary entries of ISO 32000-1, 8.9.5.1, Table 89.
// The constructors [EncodeLayout], [JPEGLayout] and [JPXImage] check it.
type ImageLayout struct {
	// BitsPerComponent is the depth of each sample of Samples: 1, 2, 4, 8
	// or 16. PDF 1.4 has no 16 (ISO 32000-1, Table 89 marks it PDF 1.5).
	// It is unset for a JPEG, which is 8, and a JPEG 2000 file, whose
	// codestream states its depth (7.4.9).
	BitsPerComponent int
	// Components is the samples of a pixel: 1 for gray, 3 for RGB, 4 for
	// CMYK, and 1, an index, with Palette.
	Components int
	// Palette, when set, makes each sample an index into it: an Indexed
	// colour space over RGB (8.6.6.3), three bytes an entry, 1 to 256
	// entries.
	Palette []byte
	// ICC, when set, is the ICC profile the colours are read through, an
	// ICCBased colour space (8.6.5.5) of Components components, or of 3
	// as the base of Palette. Without it the colours are DeviceGray,
	// DeviceRGB or DeviceCMYK by their count.
	ICC []byte
	// Invert reads every component inverted, a Decode array of [1 0] a
	// component (8.9.5.2): the samples of an Adobe CMYK JPEG.
	Invert bool
	// AlphaBits is the depth of Alpha, 8 or 16, and 0 when the source has
	// no alpha.
	AlphaBits int
	// JPX marks a source whose JPEG field holds a JP2 file decoded by
	// JPXDecode (7.4.9), its colour space the one the file specifies when
	// ICC is unset (Table 89).
	JPX bool
}

// check reports a layout no image dictionary describes.
func (l *ImageLayout) check(samples bool) error {
	if samples {
		switch l.BitsPerComponent {
		case 1, 2, 4, 8, 16:
		default:
			return fmt.Errorf("%w: %d bits a component", ErrInvalidImage, l.BitsPerComponent)
		}
	}
	switch l.Components {
	case 1, 3, 4:
	default:
		return fmt.Errorf("%w: %d components", ErrInvalidImage, l.Components)
	}
	if l.Palette != nil {
		n := len(l.Palette) / 3
		if l.Components != 1 || len(l.Palette)%3 != 0 || n < 1 || n > maxPaletteColors || l.BitsPerComponent > 8 {
			return fmt.Errorf("%w: palette of %d bytes over %d components of %d bits", ErrInvalidImage, len(l.Palette), l.Components, l.BitsPerComponent)
		}
	}
	if len(l.ICC) > MaxICCBytes {
		return fmt.Errorf("%w: ICC profile of %d bytes, at most %d", ErrLimitExceeded, len(l.ICC), MaxICCBytes)
	}
	switch l.AlphaBits {
	case 0, 8, 16:
	default:
		return fmt.Errorf("%w: %d bits of alpha", ErrInvalidImage, l.AlphaBits)
	}
	return nil
}

// rowBytes returns the bytes of one row of width pixels laid out as l says,
// rows padded to a whole byte (ISO 32000-1, 8.9.3); width is at most
// [maxImagePixels], so the product fits an int64.
func (l *ImageLayout) rowBytes(width int) int64 {
	return (int64(width)*int64(l.Components)*int64(l.BitsPerComponent) + 7) / 8
}

// EncodeLayout returns an [ImageSource] of width by height pixels whose
// samples are laid out as l says, Flate-compressed: rows hands each row's
// bytes to put, top row first, alpha each row of the alpha at l.AlphaBits,
// one sample a pixel, or is nil for an opaque picture. Each row is the
// length the layout gives a row of width pixels; a row of another length
// fails with [ErrInvalidImage]. The samples are charged and kept as
// [EncodeImage] keeps them; the palette and the profile stay in memory,
// charged with them. It returns an error wrapping [ErrInvalidImage] for a
// layout no image dictionary describes, one wrapping [ErrLimitExceeded]
// for a picture past the pixels a source may hold, and one wrapping
// ctx.Err() when ctx ends first.
func EncodeLayout(ctx context.Context, width, height int, l ImageLayout, rows, alpha func(y int, put func([]byte) error) error) (*ImageSource, error) {
	if err := checkPixels(width, height); err != nil {
		return nil, err
	}
	if l.JPX {
		return nil, fmt.Errorf("%w: a JPEG 2000 layout over samples", ErrInvalidImage)
	}
	if (alpha == nil) != (l.AlphaBits == 0) {
		return nil, fmt.Errorf("%w: %d bits of alpha for an alpha that is set: %t", ErrInvalidImage, l.AlphaBits, alpha != nil)
	}
	if err := l.check(true); err != nil {
		return nil, err
	}
	samples, err := deflateRows(ctx, height, sized(rows, l.rowBytes(width)))
	if err != nil {
		return nil, fmt.Errorf("encoding image: %w", err)
	}
	src := &ImageSource{Width: width, Height: height, Samples: samples, Layout: &l}
	if alpha != nil {
		mask := ImageLayout{BitsPerComponent: l.AlphaBits, Components: 1}
		a, err := deflateRows(ctx, height, sized(alpha, mask.rowBytes(width)))
		if err != nil {
			return nil, fmt.Errorf("encoding image mask: %w", err)
		}
		src.Alpha = a
	}
	if err := keep(ctx, src); err != nil {
		return nil, fmt.Errorf("encoding image: %w", err)
	}
	return src, nil
}

// sized wraps rows so that a row of other than n bytes fails.
func sized(rows func(y int, put func([]byte) error) error, n int64) func(y int, put func([]byte) error) error {
	return func(y int, put func([]byte) error) error {
		return rows(y, func(p []byte) error {
			if int64(len(p)) != n {
				return fmt.Errorf("%w: row %d of %d bytes, the layout gives %d", ErrInvalidImage, y, len(p), n)
			}
			return put(p)
		})
	}
}

// JPEGLayout returns the JPEG file data as [JPEGImage] does, read in the
// colour space l names: its ICC profile and its inversion. l's
// Components must be the frame's component count, and its depth, palette
// and alpha unset.
func JPEGLayout(ctx context.Context, data []byte, l ImageLayout) (*ImageSource, error) {
	if l.JPX || l.Palette != nil || l.AlphaBits != 0 || l.BitsPerComponent != 0 {
		return nil, fmt.Errorf("%w: a JPEG has 8-bit samples, no palette and no alpha", ErrInvalidImage)
	}
	if err := l.check(false); err != nil {
		return nil, err
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: JPEG header: %w", ErrInvalidImage, err)
	}
	if n := jpegComponents(cfg.ColorModel); n != l.Components {
		return nil, fmt.Errorf("%w: a JPEG of %d components read as %d", ErrInvalidImage, n, l.Components)
	}
	src, err := JPEGImage(ctx, data)
	if err != nil {
		return nil, err
	}
	src.Layout = &l
	return src, nil
}

// JPXImage returns the JP2 file data as an [ImageSource] of width by height
// pixels written as it is, decoded by JPXDecode (ISO 32000-1, 7.4.9), which
// PDF 1.5 introduced. It reads the file's header alone and keeps data as
// [JPEGImage] keeps a JPEG. l's Components must be the file's colour
// channels and its depth, palette, inversion and alpha unset. It returns
// an error wrapping [ErrInvalidImage] for a file that is not a JP2 file
// of that size, and one wrapping [ErrLimitExceeded] for a picture past the
// pixels a source may hold.
func JPXImage(ctx context.Context, data []byte, width, height int, l ImageLayout) (*ImageSource, error) {
	if l.Palette != nil || l.AlphaBits != 0 || l.BitsPerComponent != 0 || l.Invert {
		return nil, fmt.Errorf("%w: a JPEG 2000 file states its own depth, palette and alpha", ErrInvalidImage)
	}
	if err := checkPixels(width, height); err != nil {
		return nil, err
	}
	l.JPX = true
	if err := l.check(false); err != nil {
		return nil, err
	}
	if err := checkJPX(data, width, height); err != nil {
		return nil, err
	}
	src := &ImageSource{Width: width, Height: height, JPEG: data, Layout: &l}
	if err := keep(ctx, src); err != nil {
		return nil, fmt.Errorf("reading JPEG 2000: %w", err)
	}
	return src, nil
}

// checkJPX checks that data is a JP2 file of width by height pixels.
func checkJPX(data []byte, width, height int) error {
	if !jpx.IsJP2(data) {
		return fmt.Errorf("%w: not a JP2 file", ErrInvalidImage)
	}
	img, err := jpx.Describe(data, jpx.DefaultMaxSamples)
	if err != nil {
		return fmt.Errorf("%w: JPEG 2000: %w", ErrInvalidImage, err)
	}
	if w, h := img.DefaultSize(); w != width || h != height {
		return fmt.Errorf("%w: JPEG 2000 of %d by %d pixels in a source of %d by %d", ErrInvalidImage, w, h, width, height)
	}
	return nil
}

// jpegComponents returns the component count of the colour model
// image/jpeg reads from a frame header.
func jpegComponents(m color.Model) int {
	switch m {
	case color.GrayModel:
		return 1
	case color.CMYKModel:
		return 4
	}
	return 3
}

// layoutXObject writes src, a source with a layout, as an image XObject
// (ISO 32000-1, 8.9.5): its samples Flate-compressed at the layout's depth,
// a JPEG by DCTDecode or a JP2 file by JPXDecode, in the colour space the
// layout names, with its alpha, when it has one, as a soft mask of the
// layout's alpha depth (11.6.5.3).
func (w *docWriter) layoutXObject(src *ImageSource) (ast.PdfStream, error) {
	l := src.Layout
	dict := ast.PdfDict{
		"Type":    ast.PdfName("XObject"),
		"Subtype": ast.PdfName("Image"),
		"Width":   ast.PdfInt(src.Width),
		"Height":  ast.PdfInt(src.Height),
	}
	if !l.JPX || l.ICC != nil {
		cs, err := w.layoutColorSpace(l)
		if err != nil {
			return ast.PdfStream{}, err
		}
		dict["ColorSpace"] = cs
	}
	if l.Invert {
		decode := make(ast.PdfArray, 0, 2*l.Components)
		for range l.Components {
			decode = append(decode, ast.PdfInt(1), ast.PdfInt(0))
		}
		dict["Decode"] = decode
	}
	switch {
	case l.JPX:
		// Table 89: BitsPerComponent is ignored for JPXDecode; the
		// codestream states each component's depth.
		dict["Filter"] = ast.PdfName("JPXDecode")
		return ast.NewLazyStream(dict, src.loader(src.JPEG, func(st *storedPicture) writer.Blob { return st.jpeg })), nil
	case src.IsJPEG():
		dict["BitsPerComponent"] = ast.PdfInt(8)
		dict["Filter"] = ast.PdfName("DCTDecode")
		return ast.NewLazyStream(dict, src.loader(src.JPEG, func(st *storedPicture) writer.Blob { return st.jpeg })), nil
	}
	dict["BitsPerComponent"] = ast.PdfInt(l.BitsPerComponent)
	dict["Filter"] = ast.PdfName("FlateDecode")
	if src.hasAlpha() {
		alpha := src.loader(src.Alpha, func(st *storedPicture) writer.Blob { return st.alpha })
		mask := w.alloc.Alloc(ast.NewLazyStream(ast.PdfDict{
			"Type":             ast.PdfName("XObject"),
			"Subtype":          ast.PdfName("Image"),
			"Width":            ast.PdfInt(src.Width),
			"Height":           ast.PdfInt(src.Height),
			"ColorSpace":       ast.PdfName("DeviceGray"),
			"BitsPerComponent": ast.PdfInt(l.AlphaBits),
			"Filter":           ast.PdfName("FlateDecode"),
		}, alpha))
		w.shared[mask.ObjectNum] = true
		dict["SMask"] = mask
	}
	return ast.NewLazyStream(dict, src.loader(src.Samples, func(st *storedPicture) writer.Blob { return st.samples })), nil
}

// layoutColorSpace returns the colour space l names: Indexed over its
// palette's base, ICCBased over its profile, else the device space of its
// component count (ISO 32000-1, 8.6.4, 8.6.5.5 and 8.6.6.3). A profile is
// written once however many images name it.
func (w *docWriter) layoutColorSpace(l *ImageLayout) (ast.PdfObject, error) {
	n := l.Components
	if l.Palette != nil {
		n = 3
	}
	var base ast.PdfObject
	if l.ICC != nil {
		ref, err := w.iccProfile(l.ICC, n)
		if err != nil {
			return nil, err
		}
		base = ast.PdfArray{ast.PdfName("ICCBased"), ref}
	} else {
		base = deviceSpace(n)
	}
	if l.Palette == nil {
		return base, nil
	}
	return ast.PdfArray{ast.PdfName("Indexed"), base, ast.PdfInt(len(l.Palette)/3 - 1), ast.PdfString(l.Palette)}, nil
}

// deviceSpace returns the device colour space of n components.
func deviceSpace(n int) ast.PdfName {
	switch n {
	case 1:
		return "DeviceGray"
	case 4:
		return "DeviceCMYK"
	}
	return "DeviceRGB"
}

// iccProfile returns the ICC profile stream of profile, n components
// (ISO 32000-1, 8.6.5.5, Table 66), writing it on first use.
func (w *docWriter) iccProfile(profile []byte, n int) (ast.PdfIndirectRef, error) {
	key := string(profile)
	if ref, ok := w.profiles[key]; ok {
		return ref, nil
	}
	stream, err := w.flateStream(ast.PdfDict{"N": ast.PdfInt(n), "Alternate": deviceSpace(n)}, profile)
	if err != nil {
		return ast.PdfIndirectRef{}, err
	}
	ref := w.alloc.Alloc(stream)
	w.shared[ref.ObjectNum] = true
	if w.profiles == nil {
		w.profiles = make(map[string]ast.PdfIndirectRef)
	}
	w.profiles[key] = ref
	return ref, nil
}

// checkLayoutSource checks a source with a layout: its layout, and the
// header of its JPEG or JPEG 2000 file against its size.
func checkLayoutSource(src *ImageSource) error {
	l := src.Layout
	if err := l.check(!src.IsJPEG()); err != nil {
		return err
	}
	if !src.IsJPEG() {
		if !src.hasSamples() {
			return fmt.Errorf("%w: source holds neither a file nor samples", ErrInvalidImage)
		}
		if src.hasAlpha() != (l.AlphaBits != 0) {
			return fmt.Errorf("%w: alpha of %d bits and an alpha that is set: %t", ErrInvalidImage, l.AlphaBits, src.hasAlpha())
		}
		return nil
	}
	if l.JPX {
		data, err := io.ReadAll(src.jpegReader())
		if err != nil {
			return fmt.Errorf("%w: JPEG 2000: %w", ErrInvalidImage, err)
		}
		return checkJPX(data, src.Width, src.Height)
	}
	cfg, err := jpeg.DecodeConfig(src.jpegReader())
	if err != nil {
		return fmt.Errorf("%w: JPEG header: %w", ErrInvalidImage, err)
	}
	if cfg.Width != src.Width || cfg.Height != src.Height || jpegComponents(cfg.ColorModel) != l.Components {
		return fmt.Errorf("%w: JPEG of %d by %d pixels in a source of %d by %d", ErrInvalidImage, cfg.Width, cfg.Height, src.Width, src.Height)
	}
	return nil
}
