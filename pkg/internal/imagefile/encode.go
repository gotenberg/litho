package imagefile

import (
	"context"
	"encoding/binary"
	"image"
	"image/color"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// encodeOptions is how a decoded picture is written.
type encodeOptions struct {
	// no16 writes 16-bit samples at 8 bits: PDF 1.4 has no 16-bit
	// component (ISO 32000-1, Table 89).
	no16 bool
	// icc is the profile the colours are read through, nil for the device
	// space of their count. It applies to gray, RGB and CMYK pictures of
	// its own colour space and to an indexed picture's RGB palette.
	icc *profile
}

// encode writes img losslessly as an [typeset.ImageSource] in the smallest
// layout that holds every sample as it is: an indexed picture indexed, at
// the fewest bits that index its palette; a gray one at the fewest bits
// that hold its levels exactly; 16-bit samples at 16 bits unless each is
// an 8-bit level widened, or o.no16; an alpha that is not opaque
// everywhere as a soft mask of the picture's depth. Colours are written
// straight, not premultiplied, as a soft mask expects (ISO 32000-1,
// 11.6.5.3).
func encode(ctx context.Context, img image.Image, o encodeOptions) (*typeset.ImageSource, error) {
	b := img.Bounds()
	switch p := img.(type) {
	case *image.Paletted:
		return encodePaletted(ctx, p, o)
	case *image.Gray:
		return encodeGray(ctx, p, o)
	case *image.Gray16:
		if !o.no16 && !widened(p.Pix) {
			return typeset.EncodeLayout(ctx, b.Dx(), b.Dy(), typeset.ImageLayout{BitsPerComponent: 16, Components: 1, ICC: o.icc.forSpace(1)},
				func(y int, put func([]byte) error) error {
					off := p.PixOffset(b.Min.X, b.Min.Y+y)
					return put(p.Pix[off : off+2*b.Dx()])
				}, nil)
		}
		gray := image.NewGray(b)
		for i := range gray.Pix {
			gray.Pix[i] = p.Pix[2*i]
		}
		return encodeGray(ctx, gray, o)
	case *image.CMYK:
		return typeset.EncodeLayout(ctx, b.Dx(), b.Dy(), typeset.ImageLayout{BitsPerComponent: 8, Components: 4, ICC: o.icc.forSpace(4)},
			func(y int, put func([]byte) error) error {
				off := p.PixOffset(b.Min.X, b.Min.Y+y)
				return put(p.Pix[off : off+4*b.Dx()])
			}, nil)
	}
	if deep(img) && !o.no16 {
		return encodeRGB16(ctx, img, o)
	}
	return encodeRGB(ctx, img, o)
}

// widened reports whether every 16-bit big-endian sample of pix is an
// 8-bit level widened, its two bytes equal.
func widened(pix []byte) bool {
	for i := 0; i+1 < len(pix); i += 2 {
		if pix[i] != pix[i+1] {
			return false
		}
	}
	return true
}

// deep reports whether img holds 16-bit samples that are not all 8-bit
// levels widened.
func deep(img image.Image) bool {
	switch p := img.(type) {
	case *image.NRGBA64:
		return !widened(p.Pix)
	case *image.RGBA64:
		return !widened(p.Pix)
	}
	return false
}

// encodePaletted writes p as an indexed picture over its palette, at the
// fewest bits a sample that index it, with the palette's alpha as an 8-bit
// soft mask when an entry a pixel uses is not opaque.
func encodePaletted(ctx context.Context, p *image.Paletted, o encodeOptions) (*typeset.ImageSource, error) {
	b := p.Bounds()
	w, h := b.Dx(), b.Dy()
	maxIndex := 0
	for y := range h {
		off := p.PixOffset(b.Min.X, b.Min.Y+y)
		for _, v := range p.Pix[off : off+w] {
			maxIndex = max(maxIndex, int(v))
		}
	}
	// An index past the palette reads as black: the decoders that make a
	// paletted picture refuse one, so this only pads a palette shorter
	// than the depth's.
	n := max(len(p.Palette), maxIndex+1)
	palette := make([]byte, 3*n)
	alpha := make([]byte, n)
	used := make([]bool, n)
	for y := range h {
		off := p.PixOffset(b.Min.X, b.Min.Y+y)
		for _, v := range p.Pix[off : off+w] {
			used[v] = true
		}
	}
	translucent := false
	for i := range n {
		alpha[i] = 0xFF
		if i >= len(p.Palette) {
			continue
		}
		c := color.NRGBAModel.Convert(p.Palette[i]).(color.NRGBA) //nolint:forcetypeassert // NRGBAModel returns color.NRGBA
		palette[3*i], palette[3*i+1], palette[3*i+2] = c.R, c.G, c.B
		alpha[i] = c.A
		if c.A != 0xFF && used[i] {
			translucent = true
		}
	}
	bits := 1
	for 1<<bits < n {
		bits *= 2
	}
	l := typeset.ImageLayout{BitsPerComponent: bits, Components: 1, Palette: palette, ICC: o.icc.forSpace(3)}
	row := make([]byte, (w*bits+7)/8) // fits int: w is bounded by the pixel cap
	rows := func(y int, put func([]byte) error) error {
		off := p.PixOffset(b.Min.X, b.Min.Y+y)
		pack(row, p.Pix[off:off+w], bits)
		return put(row)
	}
	var alphaRows func(int, func([]byte) error) error
	if translucent {
		l.AlphaBits = 8
		arow := make([]byte, w)
		alphaRows = func(y int, put func([]byte) error) error {
			off := p.PixOffset(b.Min.X, b.Min.Y+y)
			for x, v := range p.Pix[off : off+w] {
				arow[x] = alpha[v]
			}
			return put(arow)
		}
	}
	return typeset.EncodeLayout(ctx, w, h, l, rows, alphaRows)
}

// encodeGray writes g at the fewest bits, 1, 2, 4 or 8, that hold each of
// its levels exactly: a level of a d-bit picture widened to 8 bits is a
// multiple of 255 / (2^d - 1) (ISO 32000-1, 8.9.5.2 reads a d-bit sample v
// as v / (2^d - 1)).
func encodeGray(ctx context.Context, g *image.Gray, o encodeOptions) (*typeset.ImageSource, error) {
	b := g.Bounds()
	w, h := b.Dx(), b.Dy()
	bits := 1
	for y := range h {
		off := g.PixOffset(b.Min.X, b.Min.Y+y)
		for _, v := range g.Pix[off : off+w] {
			for bits < 8 && v%byte(255/(1<<bits-1)) != 0 {
				bits *= 2
			}
		}
		if bits == 8 {
			break
		}
	}
	step := byte(255 / (1<<bits - 1))
	row := make([]byte, (w*bits+7)/8) // fits int: w is bounded by the pixel cap
	levels := make([]byte, w)
	return typeset.EncodeLayout(ctx, w, h, typeset.ImageLayout{BitsPerComponent: bits, Components: 1, ICC: o.icc.forSpace(1)},
		func(y int, put func([]byte) error) error {
			off := g.PixOffset(b.Min.X, b.Min.Y+y)
			for x, v := range g.Pix[off : off+w] {
				levels[x] = v / step
			}
			pack(row, levels, bits)
			return put(row)
		}, nil)
}

// pack packs values of the given bits each, 1, 2, 4 or 8, into row, most
// significant bits first, the last byte padded with zeros (ISO 32000-1,
// 8.9.3).
func pack(row, values []byte, bits int) {
	if bits == 8 {
		copy(row, values)
		return
	}
	clear(row)
	per := 8 / bits
	for i, v := range values {
		shift := 8 - bits*(i%per+1)
		row[i/per] |= v << shift
	}
}

// encodeRGB writes img as 8-bit RGB samples, its alpha, when a pixel is
// not opaque, as an 8-bit soft mask.
func encodeRGB(ctx context.Context, img image.Image, o encodeOptions) (*typeset.ImageSource, error) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	rgb := make([]byte, 3*w) // fits int: w is bounded by the pixel cap
	a := make([]byte, w)
	row := func(y int) {
		y += b.Min.Y
		for x := range w {
			c := straight8(img, b.Min.X+x, y)
			rgb[3*x], rgb[3*x+1], rgb[3*x+2], a[x] = c.R, c.G, c.B, c.A
		}
	}
	translucent := false
	for y := range h {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		row(y)
		for _, v := range a {
			if v != 0xFF {
				translucent = true
				break
			}
		}
		if translucent {
			break
		}
	}
	l := typeset.ImageLayout{BitsPerComponent: 8, Components: 3, ICC: o.icc.forSpace(3)}
	rows := func(y int, put func([]byte) error) error {
		row(y)
		return put(rgb)
	}
	var alpha func(int, func([]byte) error) error
	if translucent {
		l.AlphaBits = 8
		alpha = func(y int, put func([]byte) error) error {
			row(y)
			return put(a)
		}
	}
	return typeset.EncodeLayout(ctx, w, h, l, rows, alpha)
}

// encodeRGB16 writes img as 16-bit RGB samples, big-endian, its alpha,
// when a pixel is not opaque, as a 16-bit soft mask.
func encodeRGB16(ctx context.Context, img image.Image, o encodeOptions) (*typeset.ImageSource, error) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	rgb := make([]byte, 6*w) // fits int: w is bounded by the pixel cap
	a := make([]byte, 2*w)
	row := func(y int) {
		y += b.Min.Y
		for x := range w {
			c := color.NRGBA64Model.Convert(img.At(b.Min.X+x, y)).(color.NRGBA64) //nolint:forcetypeassert // NRGBA64Model returns color.NRGBA64
			put16(rgb[6*x:], c.R)
			put16(rgb[6*x+2:], c.G)
			put16(rgb[6*x+4:], c.B)
			put16(a[2*x:], c.A)
		}
	}
	translucent := false
	for y := range h {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		row(y)
		for i := 0; i < len(a); i += 2 {
			if a[i] != 0xFF || a[i+1] != 0xFF {
				translucent = true
				break
			}
		}
		if translucent {
			break
		}
	}
	l := typeset.ImageLayout{BitsPerComponent: 16, Components: 3, ICC: o.icc.forSpace(3)}
	rows := func(y int, put func([]byte) error) error {
		row(y)
		return put(rgb)
	}
	var alpha func(int, func([]byte) error) error
	if translucent {
		l.AlphaBits = 16
		alpha = func(y int, put func([]byte) error) error {
			row(y)
			return put(a)
		}
	}
	return typeset.EncodeLayout(ctx, w, h, l, rows, alpha)
}

// put16 writes v big-endian at the start of b.
func put16(b []byte, v uint16) {
	binary.BigEndian.PutUint16(b, v)
}

// straight8 returns the pixel of img at (x, y) as 8-bit straight colour:
// read from the picture's own layout where it stores one, else converted
// through its premultiplied 16-bit value.
func straight8(img image.Image, x, y int) color.NRGBA {
	switch p := img.(type) {
	case *image.NRGBA:
		i := p.PixOffset(x, y)
		return color.NRGBA{R: p.Pix[i], G: p.Pix[i+1], B: p.Pix[i+2], A: p.Pix[i+3]}
	case *image.YCbCr:
		yi, ci := p.YOffset(x, y), p.COffset(x, y)
		r, g, bl := color.YCbCrToRGB(p.Y[yi], p.Cb[ci], p.Cr[ci])
		return color.NRGBA{R: r, G: g, B: bl, A: 0xFF}
	case *image.NYCbCrA:
		yi, ci := p.YOffset(x, y), p.COffset(x, y)
		r, g, bl := color.YCbCrToRGB(p.Y[yi], p.Cb[ci], p.Cr[ci])
		return color.NRGBA{R: r, G: g, B: bl, A: p.A[p.AOffset(x, y)]}
	case *image.RGBA:
		i := p.PixOffset(x, y)
		if p.Pix[i+3] == 0xFF {
			return color.NRGBA{R: p.Pix[i], G: p.Pix[i+1], B: p.Pix[i+2], A: 0xFF}
		}
	}
	return color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA) //nolint:forcetypeassert // NRGBAModel returns color.NRGBA
}
