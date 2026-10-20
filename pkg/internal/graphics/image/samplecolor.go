package image

import (
	"context"
	"fmt"
	"image/color"
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/icc"
)

// maxSampleColorants bounds the colorants of a DeviceN space a [SampleColor]
// converts. ISO 32000-1, Annex C, Table C.1 makes 32 the implementation limit
// on DeviceN colorants, so a conforming space never names more; the bound is
// here because the count is read from the file and sizes a buffer.
const maxSampleColorants = 32

// SampleColor converts colours of one colour space to sRGB exactly as the
// decoder converts the samples of an 8-bit image in that space with no
// /Decode array: through the same default colour spaces, output intents, ICC
// profiles, CIE formulas, colour tables and tint transforms. A vector colour
// and an image in the same space then paint the same sRGB.
//
// A colour is first quantised to the byte an image sample would hold
// ([SampleColor.Samples]), then converted ([SampleColor.Convert]).
//
// SampleColor is not safe for concurrent use: its converters remember the
// colours they convert.
type SampleColor struct {
	// ranges holds a [lo hi] pair per component: the values a sample byte of
	// 0 and 255 stand for, which is the default /Decode array of the space
	// (ISO 32000-1, 8.9.5.2, Table 90).
	ranges []float64
	// index reports an Indexed space, whose one component is an integer index
	// clipped to [0, top] rather than a value scaled to a byte (8.6.6.3).
	index bool
	top   float64
	// none reports a Separation or DeviceN space over the colorant None only,
	// which paints nothing (8.6.6.4, 8.6.6.5).
	none    bool
	convert func(samples []byte) (color.RGBA, error)
}

// NewSampleColor returns the converter of the colour space csObj, a family
// name or a colour space array, painted with the resource dictionary
// resources (whose /DefaultGray, /DefaultRGB and /DefaultCMYK stand in for the
// device spaces, ISO 32000-1, 8.6.5.6) on the page dictionary page (whose
// /OutputIntents replace the document's, ISO 32000-2, 14.11.5), with the
// rendering intent intent. Either dictionary may be nil.
//
// It returns an error wrapping [ErrUnsupportedColorSpace] for a space the
// decoder cannot convert either. A Pattern space is one: its colour is a
// pattern, and an uncoloured pattern's colour is read in its base space.
func NewSampleColor(ctx context.Context, doc *document.Document, csObj ast.PdfObject, resources, page ast.PdfDict, intent icc.Intent) (*SampleColor, error) {
	cc := colorContext{resources: resources, page: page}
	dict := cc.withDefaults(ctx, doc, ast.PdfDict{"ColorSpace": csObj, "Intent": ast.PdfName(intent.String())})
	if pal, ok := resolveIndexedPalette(ctx, doc, dict, cc); ok {
		return &SampleColor{
			ranges:  []float64{0, 255},
			index:   true,
			top:     float64(min(pal.entries, maxPaletteEntries) - 1),
			convert: func(s []byte) (color.RGBA, error) { return pal.rgb(s[0]), nil },
		}, nil
	}
	if _, base, indexed := indexedColorSpace(ctx, doc, dict); indexed {
		return nil, fmt.Errorf("%w: indexed colour space over base %q has no readable colour table", ErrUnsupportedColorSpace, base)
	}
	if arr, family, colorants, ok := tintedColorSpace(ctx, doc, dict); ok {
		if colorants > maxSampleColorants {
			return nil, fmt.Errorf("%w: %s space of %d colorants, above the %d of ISO 32000-1, Table C.1",
				ErrUnsupportedColorSpace, family, colorants, maxSampleColorants)
		}
		sc := &SampleColor{ranges: unitRanges(colorants)}
		if paintsNothing(ctx, doc, arr, family) {
			sc.none = true
			return sc, nil
		}
		tc, err := newTintConverter(ctx, doc, arr, colorants, intent, cc)
		if err != nil {
			return nil, fmt.Errorf("%w: %s space: %w", ErrUnsupportedColorSpace, family, err)
		}
		sc.convert = tc.rgb
		return sc, nil
	}

	space := resolveColorSpace(ctx, doc, dict)
	if space == "Lab" {
		lab, ok := resolveLabParams(ctx, doc, dict["ColorSpace"])
		if !ok {
			return nil, fmt.Errorf("%w: Lab colour space with no usable /WhitePoint", ErrUnsupportedColorSpace)
		}
		return &SampleColor{
			// L* over [0, 100], a* and b* over /Range (8.6.5.4, Table 65).
			ranges: []float64{0, 100, lab.aMin, lab.aMax, lab.bMin, lab.bMax},
			convert: func(s []byte) (color.RGBA, error) {
				r, g, b := labEntryToRGB([4]byte{s[0], s[1], s[2]}, lab)
				return color.RGBA{R: r, G: g, B: b, A: 255}, nil
			},
		}, nil
	}
	n := imageComponents(space)
	if n == 0 {
		return nil, fmt.Errorf("%w: colour space %q", ErrUnsupportedColorSpace, space)
	}
	sc := &SampleColor{ranges: iccRanges(ctx, doc, dict["ColorSpace"], n)}
	conv := newConverter(colorTransform(ctx, doc, dict["ColorSpace"], space, n, intent, cc))
	cal, isCal := resolveCalSpace(ctx, doc, dict["ColorSpace"])
	isCal = isCal && calComponents(cal) == n
	switch {
	case conv != nil && n == 1:
		// A grey profile maps onto the neutral axis, where the decoder keeps
		// the green channel for all three.
		sc.convert = func(s []byte) (color.RGBA, error) {
			_, g, _ := conv.convert(s[:1])
			return color.RGBA{R: g, G: g, B: g, A: 255}, nil
		}
	case conv != nil:
		sc.convert = func(s []byte) (color.RGBA, error) {
			r, g, b := conv.convert(s[:n])
			return color.RGBA{R: r, G: g, B: b, A: 255}, nil
		}
	case isCal && n == 1:
		sc.convert = func(s []byte) (color.RGBA, error) {
			g := cal.convertGrey(s[0])
			return color.RGBA{R: g, G: g, B: g, A: 255}, nil
		}
	case isCal:
		sc.convert = func(s []byte) (color.RGBA, error) {
			r, g, b := cal.convertRGB(s[0], s[1], s[2])
			return color.RGBA{R: r, G: g, B: b, A: 255}, nil
		}
	default:
		// The device formulas a palette entry of the space is read with.
		pal := palette{base: space, comps: n, lookup: make([]byte, n), entries: 1}
		sc.convert = func(s []byte) (color.RGBA, error) {
			copy(pal.lookup, s[:n])
			return pal.rgb(0), nil
		}
	}
	return sc, nil
}

// unitRanges returns n [0 1] pairs.
func unitRanges(n int) []float64 {
	r := make([]float64, 2*n)
	for i := range n {
		r[2*i+1] = 1
	}
	return r
}

// iccRanges returns the /Range of an ICCBased space's stream when it states n
// ordered pairs, the default /Decode array of such a space (ISO 32000-1,
// Table 90), and n [0 1] pairs otherwise, which is also /Range's default
// (Table 66) and the range of every other space of n components.
func iccRanges(ctx context.Context, doc *document.Document, csObj ast.PdfObject, n int) []float64 {
	arr, err := document.ResolveArray(ctx, doc, csObj)
	if err != nil || len(arr) < 2 {
		return unitRanges(n)
	}
	if family, err := document.ResolveName(ctx, doc, arr[0]); err != nil || family != "ICCBased" {
		return unitRanges(n)
	}
	stream, err := document.ResolveStream(ctx, doc, arr[1])
	if err != nil || stream.Dict == nil {
		return unitRanges(n)
	}
	rng, ok := numbers(ctx, doc, stream.Dict["Range"], 2*n)
	if !ok {
		return unitRanges(n)
	}
	for i := range n {
		if !(rng[2*i] < rng[2*i+1]) {
			return unitRanges(n)
		}
	}
	return rng
}

// Components returns the number of components a colour in the space has.
func (sc *SampleColor) Components() int { return len(sc.ranges) / 2 }

// PaintsNothing reports a Separation or DeviceN space over the colorant None
// only, which never marks the page (ISO 32000-1, 8.6.6.4 and 8.6.6.5).
func (sc *SampleColor) PaintsNothing() bool { return sc.none }

// Samples quantises a colour to the bytes an 8-bit image sample in the space
// holds, written into dst, which has [SampleColor.Components] bytes. A
// component outside its range is clipped to it, as ISO 32000-1, 8.6 requires
// of every family; a missing one reads as the low end. An index is rounded
// and clipped to [0, hival] (8.6.6.3).
func (sc *SampleColor) Samples(components []float64, dst []byte) {
	for i := range dst {
		lo, hi := sc.ranges[2*i], sc.ranges[2*i+1]
		v := lo
		if i < len(components) && !math.IsNaN(components[i]) {
			v = min(max(components[i], lo), hi)
		}
		if sc.index {
			dst[i] = byte(min(math.Round(v), sc.top))
			continue
		}
		dst[i] = byte(math.Round((v - lo) / (hi - lo) * 255))
	}
}

// Convert converts one colour, as [SampleColor.Samples] quantised it, to sRGB.
// A space that paints nothing converts every colour to transparent black. The
// error reports a tint transform that fails to evaluate.
func (sc *SampleColor) Convert(samples []byte) (color.RGBA, error) {
	if sc.none {
		return color.RGBA{}, nil
	}
	return sc.convert(samples)
}
