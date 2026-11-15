package imagefile

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"

	"golang.org/x/image/bmp"
)

// readGIF reads the first image of a GIF file (GIF89a, 20 to 22), composed
// on its logical screen as a viewer shows the first frame: the image at
// its position, the rest of the screen transparent, as browsers leave it
// whatever the background colour index (GIF89a, 18, which makes the
// background a hint). A GIF states no resolution; its colours are sRGB,
// written as DeviceRGB.
func readGIF(ctx context.Context, data []byte, fn func(*Picture) error) error {
	cfg, err := gif.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return malformed("GIF header", err)
	}
	w, h := cfg.Width, cfg.Height
	if err := checkSize(ctx, w, h); err != nil {
		return err
	}
	// The frame decodes into a picture of its own rectangle, which the
	// decoder bounds by the screen, and the screen is composed beside it,
	// indexed or, with a full palette, in straight colour: four bytes a
	// pixel cover both.
	src, err := decoded(ctx, w, h, color.NRGBAModel, func() (image.Image, error) {
		frame, err := gif.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, malformed("GIF", err)
		}
		p, ok := frame.(*image.Paletted)
		if !ok {
			return nil, malformed("GIF", errors.New("the first frame is not indexed"))
		}
		return composeGIF(p, w, h), nil
	}, encodeOptions{})
	if err != nil {
		return err
	}
	return fn(&Picture{Source: src, Width: w, Height: h, Orientation: 1})
}

// composeGIF places the frame p on a screen of w by h pixels, the rest of
// it transparent: an indexed picture over p's palette with a transparent
// entry, the frame's own when it has one, else one added; a picture of
// straight colour when the palette is full.
func composeGIF(p *image.Paletted, w, h int) image.Image {
	screen := image.Rect(0, 0, w, h)
	if p.Rect == screen {
		return p
	}
	clear := -1
	for i, c := range p.Palette {
		if _, _, _, a := c.RGBA(); a == 0 {
			clear = i
			break
		}
	}
	palette := p.Palette
	if clear < 0 && len(palette) < 256 {
		palette = append(append(color.Palette{}, palette...), color.NRGBA{})
		clear = len(palette) - 1
	}
	if clear < 0 {
		out := image.NewNRGBA(screen)
		draw.Draw(out, p.Rect.Intersect(screen), p, p.Rect.Min, draw.Src)
		return out
	}
	out := image.NewPaletted(screen, palette)
	for i := range out.Pix {
		out.Pix[i] = byte(clear) //nolint:gosec // G115: clear indexes a palette of at most 256 entries
	}
	r := p.Rect.Intersect(screen)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		copy(out.Pix[out.PixOffset(r.Min.X, y):out.PixOffset(r.Max.X, y)], p.Pix[p.PixOffset(r.Min.X, y):p.PixOffset(r.Max.X, y)])
	}
	return out
}

// bmpInfoOffset is where a BMP's DIB header starts, past its 14-byte file
// header.
const bmpInfoOffset = 14

// readBMPFile reads the picture of a BMP file through golang.org/x/image/bmp,
// which reads a BITMAPINFOHEADER, BITMAPV4HEADER or BITMAPV5HEADER of 8,
// 24 or 32 bits a pixel, uncompressed: any other depth, a compressed
// bitmap and an OS/2 header fail with [ErrUnsupported]. Its resolution is
// biXPelsPerMeter and biYPelsPerMeter. Its colours are read as sRGB,
// written as DeviceRGB; a profile a BITMAPV5HEADER links or embeds is not
// read.
func readBMPFile(ctx context.Context, data []byte, fn func(*Picture) error) error {
	pic, err := readBMP(ctx, data, nil)
	if err != nil {
		return err
	}
	if len(data) >= bmpInfoOffset+32 {
		ppm := func(off int) float64 {
			return float64(int32(binary.LittleEndian.Uint32(data[off:]))) //nolint:gosec // G115: the field is a signed 32-bit LONG
		}
		pic.XDPI, pic.YDPI = Resolution(ppm(bmpInfoOffset+24)*0.0254, ppm(bmpInfoOffset+28)*0.0254)
	}
	return fn(pic)
}

// readBMP decodes the BMP data, its alpha, when mask is set, taken from
// mask as an icon's AND mask gives it.
func readBMP(ctx context.Context, data []byte, mask func(image.Image) image.Image) (*Picture, error) {
	cfg, err := bmp.DecodeConfig(bytes.NewReader(data))
	if errors.Is(err, bmp.ErrUnsupported) {
		return nil, fmt.Errorf("%w: a BMP of a depth or a compression litho does not read: %w", ErrUnsupported, err)
	}
	if err != nil {
		return nil, malformed("BMP header", err)
	}
	if err := checkSize(ctx, cfg.Width, cfg.Height); err != nil {
		return nil, err
	}
	src, err := decoded(ctx, cfg.Width, cfg.Height, cfg.ColorModel, func() (image.Image, error) {
		img, err := bmp.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, malformed("BMP", err)
		}
		if mask != nil {
			img = mask(img)
		}
		return img, nil
	}, encodeOptions{})
	if err != nil {
		return nil, err
	}
	return &Picture{Source: src, Width: cfg.Width, Height: cfg.Height, Orientation: 1}, nil
}
