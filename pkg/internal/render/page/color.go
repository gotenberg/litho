package page

import (
	"context"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
)

// Converter converts colour components in one colour space to sRGB channels
// in [0, 1]. It has the shape of [shade.Converter], so one value serves a
// fill colour and a shading.
type Converter interface {
	RGB(components []float64) (r, g, b float32)
}

// ColorProvider returns the [Converter] for a colour space, read against the
// resources of the content stream that uses it (their default colour spaces
// stand in for the device spaces, ISO 32000-1, 8.6.5.6).
type ColorProvider interface {
	For(ctx context.Context, doc *document.Document, space graphics.ColorSpace, resources ast.PdfDict) (Converter, error)
}

// midGrey is what the stand-in paints a colour space it does not convert in.
const midGrey = 0.5

// NaiveColors is a TEMPORARY [ColorProvider], the stand-in until the colour
// conversion package joins the renderer. It converts with the device formulas
// of ISO 32000-1, 10.4.2: DeviceGray and CalGray as grey, DeviceRGB and
// CalRGB as RGB, DeviceCMYK as 1 - min(1, C + K) per channel, and an
// ICCBased space by its component count as the device space of that count
// (8.6.5.5 names it the alternate). Every other space paints mid-grey. It
// ignores default colour spaces, rendering intents and ICC profiles.
type NaiveColors struct{}

// For returns the stand-in converter for space.
func (NaiveColors) For(_ context.Context, _ *document.Document, space graphics.ColorSpace, _ ast.PdfDict) (Converter, error) {
	switch space.Family {
	case graphics.FamilyDeviceGray, graphics.FamilyCalGray:
		return grayConverter{}, nil
	case graphics.FamilyDeviceRGB, graphics.FamilyCalRGB:
		return rgbConverter{}, nil
	case graphics.FamilyDeviceCMYK:
		return cmykConverter{}, nil
	case graphics.FamilyICCBased:
		switch space.Components {
		case 1:
			return grayConverter{}, nil
		case 3:
			return rgbConverter{}, nil
		case 4:
			return cmykConverter{}, nil
		}
	}
	return greyConverter{}, nil
}

// component returns c[i] clamped to [0, 1], 0 when c is too short.
func component(c []float64, i int) float32 {
	if i >= len(c) {
		return 0
	}
	v := c[i]
	switch {
	case !(v > 0):
		return 0
	case v >= 1:
		return 1
	}
	return float32(v)
}

type grayConverter struct{}

func (grayConverter) RGB(c []float64) (r, g, b float32) {
	v := component(c, 0)
	return v, v, v
}

type rgbConverter struct{}

func (rgbConverter) RGB(c []float64) (r, g, b float32) {
	return component(c, 0), component(c, 1), component(c, 2)
}

// cmykConverter applies ISO 32000-1, 10.4.2.4: red = 1 - min(1, C + K), and
// so on.
type cmykConverter struct{}

func (cmykConverter) RGB(c []float64) (r, g, b float32) {
	k := component(c, 3)
	return 1 - min(1, component(c, 0)+k), 1 - min(1, component(c, 1)+k), 1 - min(1, component(c, 2)+k)
}

type greyConverter struct{}

func (greyConverter) RGB([]float64) (r, g, b float32) {
	return midGrey, midGrey, midGrey
}
