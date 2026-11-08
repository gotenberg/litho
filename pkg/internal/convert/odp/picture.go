package odp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	imgcolor "image/color"
	_ "image/gif"  // registers GIF with image.Decode
	_ "image/jpeg" // registers JPEG with image.Decode
	_ "image/png"  // registers PNG with image.Decode
	"strings"

	_ "golang.org/x/image/bmp"  // registers BMP with image.Decode
	_ "golang.org/x/image/tiff" // registers TIFF with image.Decode

	"github.com/gotenberg/litho/pkg/internal/chart"
	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/odf"
	"github.com/gotenberg/litho/pkg/internal/shape"
	"github.com/gotenberg/litho/pkg/internal/svg"
)

// The picture caps.
const (
	// maxImagePixels bounds one picture's pixels, 50 megapixels, and
	// maxTotalPixels a document's, 200 megapixels. Anti-DoS guard, not
	// spec-mandated: a decoded pixel costs four bytes, and a full-slide
	// photograph at 300 dpi holds eight megapixels.
	maxImagePixels = 50_000_000
	maxTotalPixels = 200_000_000
)

// picResult is one decoded picture, nil when it is not drawn.
type picResult struct {
	pic *layout.Picture
}

// picture decodes the picture an element holds through xlink:href or
// office:binary-data (ODF 1.3, Part 3, 10.4.4 draw:image, 16.42.6
// draw:fill-image, 16.34 text:list-level-style-image), nil with a
// warning for one it does not draw. A package entry is decoded once.
func (r *reader) picture(el *odf.Element) *layout.Picture {
	key := el.A(odf.NSXLink, "href")
	if key == "" {
		key = fmt.Sprintf("binary:%p", el)
	}
	if res, ok := r.pics[key]; ok {
		return res.pic
	}
	if len(r.pics) >= maxPictures {
		r.warn("too-many-pictures", fmt.Sprintf("pictures past %d are not drawn", maxPictures))
		return nil
	}
	res := &picResult{pic: r.decodePicture(el)}
	r.pics[key] = res
	return res.pic
}

// decodePicture reads and decodes one picture: PNG, JPEG, GIF, BMP, TIFF
// or SVG; the Windows and StarView metafiles are not drawn.
func (r *reader) decodePicture(el *odf.Element) *layout.Picture {
	data, err := r.pkg.PictureBytes(r.ctx, el)
	switch {
	case errors.Is(err, odf.ErrExternalPicture):
		r.warn("external-picture", "a linked picture outside the package is not fetched")
		return nil
	case err != nil:
		r.warn("missing-picture", fmt.Sprintf("a picture is not read: %v", err))
		return nil
	}
	if name := vectorFormat(data); name != "" {
		r.warn("picture-"+strings.ToLower(name), fmt.Sprintf("%s pictures are not drawn", name))
		return nil
	}
	if isSVG(data) {
		doc, err := svg.Parse(r.ctx, data, svg.Options{Fonts: r.fonts})
		if err != nil {
			r.warn("svg", fmt.Sprintf("an SVG picture is not drawn: %v", err))
			return nil
		}
		s := doc.Size()
		w, h := s.Width, s.Height
		if !s.HasWidth || !s.HasHeight || w <= 0 || h <= 0 {
			w, h = 300*css.Pt, 150*css.Pt
			if !(s.Ratio > 0) {
				// A drawing of neither size nor viewBox Impress fits by
				// its content's bounds, which the drawing does not give:
				// it fills the frame as the SVG viewport rules place it.
				return &layout.Picture{Width: w, Height: h, Vector: doc.Draw}
			}
			h = w / s.Ratio
		}
		return stretchedSVG(doc, w, h)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		r.warn("picture-format", "a picture in an unknown format is not drawn")
		return nil
	}
	px := int64(cfg.Width) * int64(cfg.Height)
	if px > maxImagePixels || r.pixels+px > maxTotalPixels {
		r.warn("picture-pixels", "a picture past the pixel caps is not drawn")
		return nil
	}
	r.pixels += px
	pic := &layout.Picture{Width: float64(cfg.Width) * css.Pt, Height: float64(cfg.Height) * css.Pt}
	if dx, dy, ok := density(data); ok {
		pic.Width, pic.Height = float64(cfg.Width)*72/dx, float64(cfg.Height)*72/dy
	}
	if format == "jpeg" && (cfg.ColorModel == imgcolor.GrayModel || cfg.ColorModel == imgcolor.YCbCrModel || cfg.ColorModel == imgcolor.CMYKModel) {
		pic.Source = &compose.ImageSource{JPEG: data}
		return pic
	}
	im, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		r.warn("picture-decode", fmt.Sprintf("a %s picture does not decode", format))
		return nil
	}
	pic.Source = &compose.ImageSource{Image: im}
	return pic
}

// stretchedSVG returns the SVG drawing doc of natural size w by h as a
// picture that, placed in an area, fills it whatever its proportions, as
// Impress stretches an SVG graphic to its frame like any other picture:
// the drawing laid out at its natural size, then scaled to the area.
//
// # Corpus evidence
//
//   - odp: sd/tdf105362's master frame of 28.3 by 21.1 cm holding a
//     square SVG of a viewBox alone prints black over the whole slide.
func stretchedSVG(doc *svg.Document, w, h float64) *layout.Picture {
	return &layout.Picture{Width: w, Height: h, NoRatio: true, Vector: func(ctx context.Context, area compose.Rect) []compose.Fragment {
		if w <= 0 || h <= 0 {
			return nil
		}
		m := compose.Matrix{A: area.Width / w, D: area.Height / h, E: area.X, F: area.Y}
		return []compose.Fragment{&compose.Group{Transform: m, Opacity: 1, Content: doc.Draw(ctx, compose.Rect{Width: w, Height: h})}}
	}}
}

// vectorFormat names the Windows and StarView metafiles: WMF (placeable
// or not), EMF and SVM; "" for anything else.
func vectorFormat(d []byte) string {
	switch {
	case bytes.HasPrefix(d, []byte{0xd7, 0xcd, 0xc6, 0x9a}), bytes.HasPrefix(d, []byte{0x01, 0x00, 0x09, 0x00}):
		return "WMF"
	case len(d) > 44 && bytes.Equal(d[40:44], []byte(" EMF")):
		return "EMF"
	case bytes.HasPrefix(d, []byte("VCLMTF")):
		return "SVM"
	}
	return ""
}

// isSVG reports an SVG document by its start.
func isSVG(d []byte) bool {
	head := bytes.TrimLeft(d[:min(len(d), 512)], "\xef\xbb\xbf \t\r\n")
	return bytes.HasPrefix(head, []byte("<svg")) || bytes.HasPrefix(head, []byte("<?xml")) && bytes.Contains(d[:min(len(d), 4096)], []byte("<svg"))
}

// cropped returns pic cut to fo:clip, "rect(top, right, bottom, left)"
// (ODF 1.3, Part 3, 20.186; XSL 1.0, 7.21.1): the insets in the
// picture's natural size; the picture keeps its frame's box and shows
// the rest. pic itself for no clip or one that does not parse.
func (r *reader) cropped(pic *layout.Picture, clip string) *layout.Picture {
	inner, ok := strings.CutPrefix(strings.TrimSpace(clip), "rect(")
	if !ok {
		return pic
	}
	inner = strings.TrimSuffix(inner, ")")
	parts := strings.FieldsFunc(inner, func(r rune) bool { return r == ',' || r == ' ' })
	if len(parts) != 4 {
		return pic
	}
	var in [4]float64
	for i, p := range parts {
		v, ok := odf.Length(p)
		if !ok && p != "auto" {
			return pic
		}
		in[i] = max(v, 0)
	}
	w := pic.Width - in[css.Left] - in[css.Right]
	h := pic.Height - in[css.Top] - in[css.Bottom]
	if w <= 0 || h <= 0 || in == [4]float64{} {
		return pic
	}
	src := pic
	return &layout.Picture{Width: w, Height: h, Vector: func(ctx context.Context, rect compose.Rect) []compose.Fragment {
		sx, sy := rect.Width/w, rect.Height/h
		full := compose.Rect{X: rect.X - in[css.Left]*sx, Y: rect.Y - in[css.Top]*sy, Width: src.Width * sx, Height: src.Height * sy}
		var content []compose.Fragment
		if src.Vector != nil {
			content = src.Vector(ctx, full)
		} else {
			content = []compose.Fragment{&compose.Image{Rect: full, Source: src.Source}}
		}
		return []compose.Fragment{&compose.Group{Clip: &compose.Path{Ops: shape.RectOps(rect)}, Content: content}}
	}}
}

// object returns the chart a draw:object embeds (ODF 1.3, Part 3,
// 10.4.6.2): the object directory its xlink:href names whose manifest
// media type is a chart's, or the inline office:document of a flat
// document that says it is one; nil for any other object, whose
// replacement picture is drawn instead.
func (r *reader) object(obj *odf.Element) *chart.Chart {
	var (
		c   *chart.Chart
		err error
	)
	if inline := obj.Child(odf.NSOffice, "document"); inline != nil {
		if inline.A(odf.NSOffice, "mimetype") != odf.MimeChart {
			r.warn("object", "embedded objects other than charts are drawn as their replacement picture")
			return nil
		}
		c, err = odf.ChartFromDocument(inline, inline)
	} else {
		dir := strings.TrimSuffix(strings.TrimPrefix(obj.A(odf.NSXLink, "href"), "./"), "/")
		if dir == "" || strings.Contains(dir, "://") || r.pkg.Flat {
			return nil
		}
		if r.pkg.MediaType(dir+"/") != odf.MimeChart && r.pkg.MediaType(dir) != odf.MimeChart {
			r.warn("object", "embedded objects other than charts are drawn as their replacement picture")
			return nil
		}
		c, err = odf.ParseChart(r.ctx, r.pkg, dir)
	}
	if err != nil {
		r.warn("chart-malformed", "a chart does not parse and is drawn as its replacement picture: "+err.Error())
		return nil
	}
	for _, w := range c.Warnings {
		r.warn(w.Code, w.Message)
	}
	return c
}

// translucent returns pic painted at opacity op, in [0, 1) (ODF 1.3, Part
// 3, 20.140 draw:image-opacity), as a picture that draws pic in a group
// of that opacity.
func translucent(pic *layout.Picture, op float64) *layout.Picture {
	if op <= 0 {
		return &layout.Picture{Width: pic.Width, Height: pic.Height, Vector: func(context.Context, compose.Rect) []compose.Fragment { return nil }}
	}
	src := pic
	return &layout.Picture{Width: pic.Width, Height: pic.Height, Vector: func(ctx context.Context, rect compose.Rect) []compose.Fragment {
		var content []compose.Fragment
		if src.Vector != nil {
			content = src.Vector(ctx, rect)
		} else {
			content = []compose.Fragment{&compose.Image{Rect: rect, Source: src.Source}}
		}
		return []compose.Fragment{&compose.Group{Opacity: op, Content: content}}
	}}
}
