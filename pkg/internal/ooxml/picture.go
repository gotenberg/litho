package ooxml

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif"  // registers GIF with image.Decode
	_ "image/jpeg" // registers JPEG with image.Decode
	_ "image/png"  // registers PNG with image.Decode

	_ "golang.org/x/image/bmp"  // registers BMP with image.Decode
	_ "golang.org/x/image/tiff" // registers TIFF with image.Decode

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/limits"
	"github.com/gotenberg/litho/pkg/internal/svg"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// minPictureSide is the smallest side a picture takes, in points: the
// layout refuses a picture without a positive size.
const minPictureSide = 1e-3

// PictureFormat names the format of picture data from its signature:
// "png", "jpeg", "gif", "bmp", "tiff", "svg", "emf", "wmf", or "" when
// unknown.
func PictureFormat(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG")):
		return "png"
	case bytes.HasPrefix(data, []byte{0xFF, 0xD8}):
		return "jpeg"
	case bytes.HasPrefix(data, []byte("GIF8")):
		return "gif"
	case bytes.HasPrefix(data, []byte("BM")):
		return "bmp"
	case bytes.HasPrefix(data, []byte("II*\x00")), bytes.HasPrefix(data, []byte("MM\x00*")):
		return "tiff"
	case len(data) >= 44 && binary.LittleEndian.Uint32(data) == 1 && string(data[40:44]) == " EMF":
		return "emf"
	case bytes.HasPrefix(data, []byte{0xD7, 0xCD, 0xC6, 0x9A}), bytes.HasPrefix(data, []byte{1, 0, 9, 0}), bytes.HasPrefix(data, []byte{2, 0, 9, 0}):
		return "wmf"
	}
	head := bytes.TrimSpace(data[:min(len(data), 1024)])
	head = bytes.TrimPrefix(head, []byte("\xef\xbb\xbf"))
	if bytes.HasPrefix(head, []byte("<?xml")) || bytes.HasPrefix(head, []byte("<svg")) || bytes.Contains(head, []byte("<svg")) {
		return "svg"
	}
	return ""
}

// DecodePicture decodes picture data into a layout picture at its natural
// size (96 pixels an inch for a raster picture, as Word and LibreOffice
// size one without a resolution). JPEG pictures keep their bytes, other
// raster pictures are encoded as they decode and keep no decoded pixels,
// SVG draws as vectors with fonts. EMF and WMF fail with
// [ErrUnsupportedPicture], as does any other format; a raster picture past
// the conversion's Limits.MaxImagePixels fails too. The decoded pixels are
// charged to the conversion's memory budget before they are decoded, and
// a refused charge fails with the budget's error.
func DecodePicture(ctx context.Context, data []byte, fonts *text.Fonts) (*layout.Picture, error) {
	switch f := PictureFormat(data); f {
	case "svg":
		doc, err := svg.Parse(ctx, data, svg.Options{Fonts: fonts})
		if err != nil {
			return nil, fmt.Errorf("parsing the SVG picture: %w", err)
		}
		return vectorPicture(doc), nil
	case "emf", "wmf", "":
		if f == "" {
			f = "unknown"
		}
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedPicture, f)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decoding the picture header: %w", err)
	}
	mem := limits.BudgetFrom(ctx)
	err = mem.CheckPicture(cfg.Width, cfg.Height)
	switch {
	case errors.Is(err, limits.ErrPictureTooLarge):
		return nil, fmt.Errorf("%w: %w", ErrUnsupportedPicture, err)
	case err != nil:
		return nil, err
	}
	pic := &layout.Picture{Width: float64(cfg.Width) * css.Pt, Height: float64(cfg.Height) * css.Pt}
	if format == "jpeg" && (cfg.ColorModel == color.GrayModel || cfg.ColorModel == color.YCbCrModel || cfg.ColorModel == color.CMYKModel) {
		src, err := compose.JPEGImage(data)
		if err != nil {
			return nil, fmt.Errorf("reading the JPEG picture: %w", err)
		}
		pic.Source = src
		return pic, nil
	}
	charged, err := mem.ChargePicture(cfg.Width, cfg.Height, limits.DeepModel(cfg.ColorModel))
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		mem.Release(charged)
		return nil, fmt.Errorf("decoding the %s picture: %w", format, err)
	}
	src, err := compose.EncodeImage(ctx, img)
	if err != nil {
		return nil, fmt.Errorf("encoding the %s picture: %w", format, err)
	}
	pic.Source = src
	return pic, nil
}

// vectorPicture returns the layout picture of an SVG drawing at its
// natural size, 300 by 150 pixels when it has none (CSS Images 3, 4.3).
func vectorPicture(doc *svg.Document) *layout.Picture {
	s := doc.Size()
	w, h := 300*css.Pt, 150*css.Pt
	switch {
	case s.HasWidth && s.HasHeight:
		w, h = s.Width, s.Height
	case s.HasWidth && s.Ratio > 0:
		w = s.Width
		h = w / s.Ratio
	case s.HasHeight && s.Ratio > 0:
		h = s.Height
		w = h * s.Ratio
	case s.Ratio > 0:
		h = w / s.Ratio
	}
	return &layout.Picture{Width: max(w, minPictureSide), Height: max(h, minPictureSide), Vector: doc.Draw}
}

// Cropped returns pic with the fractions crop (indexed by css.Side) cut off
// its sides, as a vector picture that draws the whole picture scaled past
// its box through a clip (ECMA-376 Part 1, 20.1.8.55). pic is returned
// as it is when crop cuts nothing or leaves nothing.
func Cropped(pic *layout.Picture, crop [4]float64) *layout.Picture {
	if crop == [4]float64{} || pic == nil {
		return pic
	}
	fw := 1 - crop[css.Left] - crop[css.Right]
	fh := 1 - crop[css.Top] - crop[css.Bottom]
	if fw <= 0.001 || fh <= 0.001 {
		return pic
	}
	inner := *pic
	out := &layout.Picture{Width: max(pic.Width*fw, minPictureSide), Height: max(pic.Height*fh, minPictureSide)}
	out.Vector = func(ctx context.Context, r compose.Rect) []compose.Fragment {
		full := compose.Rect{
			X: r.X - crop[css.Left]*r.Width/fw, Y: r.Y - crop[css.Top]*r.Height/fh,
			Width: r.Width / fw, Height: r.Height / fh,
		}
		var content []compose.Fragment
		if inner.Vector != nil {
			content = inner.Vector(ctx, full)
		} else if inner.Source != nil {
			content = []compose.Fragment{&compose.Image{Rect: full, Source: inner.Source}}
		}
		return []compose.Fragment{&compose.Group{Clip: RectPath(r), Content: content}}
	}
	return out
}

// RectPath returns the closed path of the rectangle r.
func RectPath(r compose.Rect) *compose.Path {
	return &compose.Path{Ops: []compose.PathOp{
		{Kind: compose.MoveTo, P: [3]compose.Point{{X: r.X, Y: r.Y}}},
		{Kind: compose.LineTo, P: [3]compose.Point{{X: r.X + r.Width, Y: r.Y}}},
		{Kind: compose.LineTo, P: [3]compose.Point{{X: r.X + r.Width, Y: r.Y + r.Height}}},
		{Kind: compose.LineTo, P: [3]compose.Point{{X: r.X, Y: r.Y + r.Height}}},
		{Kind: compose.ClosePath},
	}}
}
