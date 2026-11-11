package compose

import (
	"bytes"
	"compress/zlib"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// EncodeImage returns img as an [ImageSource] of Flate-compressed 8-bit
// samples, lossless: DeviceGray for an [*image.Gray], DeviceRGB otherwise,
// with its alpha, when any pixel is not opaque, as a separate stream for
// the soft mask. Colours are stored straight, not premultiplied, as the
// soft mask expects (ISO 32000-1, 11.6.5.3).
//
// It reads img row by row and keeps nothing of it: the caller may drop img
// once it returns. It releases what img was charged to the conversion's
// memory budget ([ChargeImage]) and charges the compressed samples the
// document holds in memory, the ones a [PictureStore] on ctx does not take
// to its file; a refused charge fails with the budget's error, and a store
// that cannot write its file fails with that error. It returns an error
// wrapping [ErrInvalidImage] for an
// empty picture, one wrapping [ErrLimitExceeded] for a picture past the
// pixels a source may hold, and one wrapping ctx.Err() when ctx ends
// first.
func EncodeImage(ctx context.Context, img image.Image) (*ImageSource, error) {
	if img == nil {
		return nil, fmt.Errorf("%w: nil picture", ErrInvalidImage)
	}
	defer ReleaseImage(ctx, img)
	src, err := encodeImage(ctx, img)
	if err != nil {
		return nil, err
	}
	if err := keep(ctx, src); err != nil {
		return nil, fmt.Errorf("encoding image: %w", err)
	}
	return src, nil
}

// encodeImage is [EncodeImage] without the budget.
func encodeImage(ctx context.Context, img image.Image) (*ImageSource, error) {
	if img == nil {
		return nil, fmt.Errorf("%w: nil picture", ErrInvalidImage)
	}
	b := img.Bounds()
	width, height := b.Dx(), b.Dy()
	if err := checkPixels(width, height); err != nil {
		return nil, err
	}
	src := &ImageSource{Width: width, Height: height}
	if gray, ok := img.(*image.Gray); ok {
		src.Gray = true
		samples, err := deflateRows(ctx, height, func(y int, put func([]byte) error) error {
			off := gray.PixOffset(b.Min.X, b.Min.Y+y)
			return put(gray.Pix[off : off+width])
		})
		if err != nil {
			return nil, fmt.Errorf("encoding image: %w", err)
		}
		src.Samples = samples
		return src, nil
	}

	// A picture that says it is opaque skips the alpha stream: Opaque is
	// true only when every alpha is at its maximum, which straight alpha
	// keeps at 0xFF.
	opaque := false
	if o, ok := img.(interface{ Opaque() bool }); ok {
		opaque = o.Opaque()
	}
	rgb := make([]byte, 3*width) // fits int: width is at most maxImagePixels, checked above
	alphaRow := make([]byte, width)
	translucent := false
	row := func(y int) {
		y += b.Min.Y
		if n, ok := img.(*image.NRGBA); ok {
			pix := n.Pix[n.PixOffset(b.Min.X, y):]
			for x := range width {
				copy(rgb[3*x:3*x+3], pix[4*x:4*x+3])
				alphaRow[x] = pix[4*x+3]
			}
		} else {
			for x := range width {
				c := straight(img, b.Min.X+x, y)
				rgb[3*x], rgb[3*x+1], rgb[3*x+2] = c.R, c.G, c.B
				alphaRow[x] = c.A
			}
		}
		if !translucent {
			for _, a := range alphaRow {
				if a != 0xFF {
					translucent = true
					break
				}
			}
		}
	}

	var alpha bytes.Buffer
	var az *zlib.Writer
	if !opaque {
		az = newZlib(&alpha)
		defer releaseZlib(az)
	}
	samples, err := deflateRows(ctx, height, func(y int, put func([]byte) error) error {
		row(y)
		if az != nil {
			if _, err := az.Write(alphaRow); err != nil {
				return fmt.Errorf("compressing: %w", err)
			}
		}
		return put(rgb)
	})
	if err != nil {
		return nil, fmt.Errorf("encoding image: %w", err)
	}
	src.Samples = samples
	if az != nil && translucent {
		if err := az.Close(); err != nil {
			return nil, fmt.Errorf("encoding image mask: compressing: %w", err)
		}
		src.Alpha = bytes.Clone(alpha.Bytes())
	}
	return src, nil
}

// JPEGImage returns the JPEG file data as an [ImageSource] written as it
// is, decoded by DCTDecode (ISO 32000-1, 7.4.8). It reads the frame header
// alone and keeps data, which the caller must not modify, or copies it to
// the file of a [PictureStore] on ctx, as [EncodeImage] keeps samples: data
// kept in memory is charged to the conversion's memory budget, and a
// refused charge fails with the budget's error. It returns an error
// wrapping [ErrInvalidImage] for a header it cannot read or a colour model
// DCTDecode does not take, and one wrapping [ErrLimitExceeded] for a
// picture past the pixels a source may hold.
//
// An Adobe-marked CMYK JPEG stores its samples inverted, which DCTDecode
// hands on unchanged: it is not special-cased, so such a file renders as
// its negative. A converter that meets one decodes it and encodes the
// picture with [EncodeImage].
func JPEGImage(ctx context.Context, data []byte) (*ImageSource, error) {
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: JPEG header: %w", ErrInvalidImage, err)
	}
	if _, err := jpegColorSpace(cfg.ColorModel); err != nil {
		return nil, err
	}
	if err := checkPixels(cfg.Width, cfg.Height); err != nil {
		return nil, err
	}
	src := &ImageSource{Width: cfg.Width, Height: cfg.Height, JPEG: data}
	if err := keep(ctx, src); err != nil {
		return nil, fmt.Errorf("reading JPEG: %w", err)
	}
	return src, nil
}

// Decode returns the pixels of s: an [*image.Gray] for gray samples, an
// [*image.NRGBA] for colour ones, its alpha 0xFF where s has none, and
// what image/jpeg decodes for a JPEG. The picture is new on every call and
// belongs to the caller. It returns an error wrapping [ErrInvalidImage]
// for bytes that do not decode to the source's size, and one wrapping
// ctx.Err() when ctx ends first. The picture is charged to the
// conversion's memory budget before it is made, by the layout of its colour
// model ([limits.PixelCost]), and a refused charge fails
// with the budget's error: the caller gives the charge back with
// [EncodeImage] or [ReleaseImage] once it drops the picture.
func (s *ImageSource) Decode(ctx context.Context) (image.Image, error) {
	if err := checkPixels(s.Width, s.Height); err != nil {
		return nil, err
	}
	model := color.NRGBAModel
	switch {
	case s.IsJPEG():
		// image/jpeg decodes to the layout of the frame's components:
		// gray, Y'CbCr or CMYK.
		cfg, err := jpeg.DecodeConfig(s.jpegReader())
		if err != nil {
			return nil, fmt.Errorf("%w: JPEG header: %w", ErrInvalidImage, err)
		}
		model = cfg.ColorModel
	case s.Gray:
		model = color.GrayModel
	}
	if err := ChargeImage(ctx, s.Width, s.Height, model); err != nil {
		return nil, err
	}
	img, err := s.decode(ctx)
	if err != nil {
		limits.BudgetFrom(ctx).Release(int64(s.Width) * int64(s.Height) * pixelCost(model))
		return nil, err
	}
	return img, nil
}

// decode is [ImageSource.Decode] without the budget.
func (s *ImageSource) decode(ctx context.Context) (image.Image, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("decoding image: %w", err)
	}
	if err := checkPixels(s.Width, s.Height); err != nil {
		return nil, err
	}
	if s.IsJPEG() {
		img, err := jpeg.Decode(s.jpegReader())
		if err != nil {
			return nil, fmt.Errorf("%w: JPEG: %w", ErrInvalidImage, err)
		}
		return img, nil
	}
	rect := image.Rect(0, 0, s.Width, s.Height)
	if s.Gray {
		img := image.NewGray(rect)
		if err := inflateInto(s.samplesReader(), img.Pix); err != nil {
			return nil, err
		}
		return img, nil
	}
	img := image.NewNRGBA(rect)
	n := s.Width * s.Height // fits int: at most maxImagePixels, checked above
	rgb := make([]byte, 3*n)
	if err := inflateInto(s.samplesReader(), rgb); err != nil {
		return nil, err
	}
	for i := range n {
		copy(img.Pix[4*i:4*i+3], rgb[3*i:3*i+3])
		img.Pix[4*i+3] = 0xFF
	}
	if s.hasAlpha() {
		alpha := rgb[:n]
		if err := inflateInto(s.alphaReader(), alpha); err != nil {
			return nil, err
		}
		for i, a := range alpha {
			img.Pix[4*i+3] = a
		}
	}
	return img, nil
}

// checkPixels checks that a picture of width by height pixels has pixels
// and stays within [maxImagePixels].
func checkPixels(width, height int) error {
	if width <= 0 || height <= 0 {
		return fmt.Errorf("%w: %d by %d pixels", ErrInvalidImage, width, height)
	}
	// Dividing keeps the comparison free of the overflow width*height
	// risks on a 32-bit int.
	if width > maxImagePixels/height {
		return fmt.Errorf("%w: image of %d by %d pixels, at most %d pixels", ErrLimitExceeded, width, height, maxImagePixels)
	}
	return nil
}

// deflateRows compresses the rows put hands on, rows calls in all, and
// returns the zlib stream in a slice of its own length. ctx is checked
// once a row.
func deflateRows(ctx context.Context, rows int, row func(y int, put func([]byte) error) error) ([]byte, error) {
	var buf bytes.Buffer
	z := newZlib(&buf)
	defer releaseZlib(z)
	put := func(p []byte) error {
		if _, err := z.Write(p); err != nil {
			return fmt.Errorf("compressing: %w", err)
		}
		return nil
	}
	for y := range rows {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := row(y, put); err != nil {
			return nil, err
		}
	}
	if err := z.Close(); err != nil {
		return nil, fmt.Errorf("compressing: %w", err)
	}
	// The source outlives the conversion's other buffers: the clone drops
	// the spare capacity the buffer grew.
	return bytes.Clone(buf.Bytes()), nil
}

// inflateInto decompresses the zlib stream z into dst, which it must fill
// exactly.
func inflateInto(z io.Reader, dst []byte) error {
	r, err := zlib.NewReader(z)
	if err != nil {
		return fmt.Errorf("%w: samples: %w", ErrInvalidImage, err)
	}
	defer r.Close() //nolint:errcheck // Close reports nothing a read did not
	if _, err := io.ReadFull(r, dst); err != nil {
		return fmt.Errorf("%w: samples: %w", ErrInvalidImage, err)
	}
	var one [1]byte
	if n, _ := r.Read(one[:]); n > 0 { //nolint:errcheck // only the count matters: a byte past the size is the fault
		return fmt.Errorf("%w: samples past %d bytes", ErrInvalidImage, len(dst))
	}
	return nil
}

// image returns the XObject of src, writing it on first use: a source
// placed several times, on one page or many, is written once.
func (w *docWriter) image(src *ImageSource) (ast.PdfIndirectRef, error) {
	if ref, ok := w.images[src]; ok {
		return ref, nil
	}
	var stream ast.PdfStream
	if src.IsJPEG() {
		s, err := jpegXObject(src)
		if err != nil {
			return ast.PdfIndirectRef{}, err
		}
		stream = s
	} else {
		stream = w.pictureXObject(src)
	}
	ref := w.alloc.Alloc(stream)
	w.shared[ref.ObjectNum] = true
	w.images[src] = ref
	return ref, nil
}

// jpegXObject wraps the JPEG file of src as an image XObject decoded by
// DCTDecode (ISO 32000-1, 7.4.8 and 8.9.5), its bytes as they are, read
// from a [PictureStore]'s file when it keeps them. Width, height and colour
// space come from the frame header.
func jpegXObject(src *ImageSource) (ast.PdfStream, error) {
	cfg, err := jpeg.DecodeConfig(src.jpegReader())
	if err != nil {
		return ast.PdfStream{}, fmt.Errorf("%w: JPEG header: %w", ErrInvalidImage, err)
	}
	cs, err := jpegColorSpace(cfg.ColorModel)
	if err != nil {
		return ast.PdfStream{}, err
	}
	return ast.NewLazyStream(ast.PdfDict{
		"Type":             ast.PdfName("XObject"),
		"Subtype":          ast.PdfName("Image"),
		"Width":            ast.PdfInt(cfg.Width),
		"Height":           ast.PdfInt(cfg.Height),
		"ColorSpace":       cs,
		"BitsPerComponent": ast.PdfInt(8),
		"Filter":           ast.PdfName("DCTDecode"),
	}, src.loader(src.JPEG, func(st *storedPicture) writer.Blob { return st.jpeg })), nil
}

// jpegColorSpace maps the colour model image/jpeg reads from a frame's
// component count to the device space of that count (ISO 32000-1, 8.6.4):
// 1 to DeviceGray, 3 to DeviceRGB, 4 to DeviceCMYK.
func jpegColorSpace(m color.Model) (ast.PdfName, error) {
	switch m {
	case color.GrayModel:
		return "DeviceGray", nil
	case color.YCbCrModel, color.RGBAModel:
		return "DeviceRGB", nil
	case color.CMYKModel:
		return "DeviceCMYK", nil
	default:
		return "", fmt.Errorf("%w: JPEG colour model %T", ErrInvalidImage, m)
	}
}

// pictureXObject writes src's samples as an 8-bit FlateDecode image
// XObject (ISO 32000-1, 8.9.5), DeviceGray or DeviceRGB, with its alpha,
// when it has one, as a DeviceGray soft mask (11.6.5.3) the image names in
// /SMask. The streams share the source's bytes, nothing copied, or read
// them from a [PictureStore]'s file as the file is written.
func (w *docWriter) pictureXObject(src *ImageSource) ast.PdfStream {
	dict := func(cs ast.PdfName) ast.PdfDict {
		return ast.PdfDict{
			"Type":             ast.PdfName("XObject"),
			"Subtype":          ast.PdfName("Image"),
			"Width":            ast.PdfInt(src.Width),
			"Height":           ast.PdfInt(src.Height),
			"ColorSpace":       cs,
			"BitsPerComponent": ast.PdfInt(8),
			"Filter":           ast.PdfName("FlateDecode"),
		}
	}
	samples := src.loader(src.Samples, func(st *storedPicture) writer.Blob { return st.samples })
	if src.Gray {
		return ast.NewLazyStream(dict("DeviceGray"), samples)
	}
	main := dict("DeviceRGB")
	if src.hasAlpha() {
		alpha := src.loader(src.Alpha, func(st *storedPicture) writer.Blob { return st.alpha })
		mask := w.alloc.Alloc(ast.NewLazyStream(dict("DeviceGray"), alpha))
		w.shared[mask.ObjectNum] = true
		main["SMask"] = mask
	}
	return ast.NewLazyStream(main, samples)
}

// straight returns the pixel of img at (x, y) with straight alpha.
func straight(img image.Image, x, y int) color.NRGBA {
	r, g, b, a := img.At(x, y).RGBA()
	if a == 0 {
		return color.NRGBA{}
	}
	// RGBA returns 16-bit premultiplied channels; dividing by alpha
	// restores straight ones, each at most 0xFFFF, so the shift leaves a
	// byte.
	un := func(c uint32) uint8 {
		return uint8((c * 0xFFFF / a) >> 8) //nolint:gosec // G115: c <= a, so the quotient is at most 0xFFFF and the shift leaves 8 bits
	}
	return color.NRGBA{R: un(r), G: un(g), B: un(b), A: uint8(a >> 8)} //nolint:gosec // G115: a is at most 0xFFFF, so the shift leaves 8 bits
}
