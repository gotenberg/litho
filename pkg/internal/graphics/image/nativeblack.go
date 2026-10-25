package image

import (
	"context"
	"fmt"
	"math"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// nativeBlackLevel is the largest 8-bit sRGB channel the conventional black
// of a colour space may convert to and still be taken as its black: 0 on
// every component, or full black ink and no colour ink for four. A CMYK
// black that colour management maps to a dark grey stays well under it. A
// space whose conventional black converts above it, a YCC or Lab-like ICC
// profile whose zero samples are a strong colour, is searched for its
// darkest sample instead. Not spec-mandated: ISO 32000-1, 8.6 defines no
// black for a colour space, so this is litho's line between a black and a
// colour.
const nativeBlackLevel = 64

// nativeBlackGrid is the number of evenly spaced levels per component the
// search for a space's darkest sample starts from, 0 and 255 included. Six
// levels make 216 conversions for three components and 1,296 for four, and
// put a start point within 26 levels of any sample, from which the
// refinement converges. Not spec-mandated.
const nativeBlackGrid = 6

// maxNativeBlackSteps caps the refinement passes of the search for a space's
// darkest sample. Anti-DoS guard, not spec-mandated: each pass that improves
// lowers the objective, so the walk ends long before the cap on any profile,
// and the cap only bounds a conversion that does not behave as a function of
// its samples.
const maxNativeBlackSteps = 1024

// maxNativeBlackComponents caps the components of a space searched for its
// darkest sample. Anti-DoS guard, not spec-mandated: the grid holds
// [nativeBlackGrid] to the power of the component count, 1,296 conversions
// for four, and a DeviceN of 32 colorants (ISO 32000-1, Annex C, Table C.1)
// would ask for 6^32. Every space that reaches the search today has at most
// four, since [DecodeStreamNative] admits only device, CIE-based, ICC-based
// and None-only DeviceN spaces; a wider one is refused, so blanking it fails
// closed rather than hanging.
const maxNativeBlackComponents = 4

// nativeBlackCheckEvery is how many grid points the search converts between
// checks of its context.
const nativeBlackCheckEvery = 256

// NativeBlack returns the darkest sample of the colour space an image
// dictionary names, one byte per component as [NativeSamples] holds them
// (/Decode applied), read against page's resources and dictionary exactly as
// the image's own samples are.
//
// The conventional black, 0 on every component or full black ink alone for
// four components, is returned whenever it converts to a dark colour, so the
// device, CIE-based and most ICC-based spaces keep it. ISO 32000-1, 8.6.5.5
// lets an ICC profile give a sample any meaning, though, and zero is not
// black in all of them: in a YCC profile it is a saturated green or blue.
// For such a space the result is the sample whose colour is darkest, found by
// a grid search refined one component at a time. Darkest is read in CIELAB,
// before sRGB clips the colour, as the colour nearest black (see
// [sampleDarkness]), so the black is the space's neutral one rather than a
// dark hue sRGB happens to clip to zero. A space that paints nothing
// (8.6.6.4) returns zeros, the tint of no ink.
//
// It returns an error when the space cannot be converted, and when its
// component count is not components.
func NativeBlack(ctx context.Context, doc *document.Document, page *document.Page, dict ast.PdfDict, components int) ([]byte, error) {
	black := make([]byte, components)
	if components == 4 {
		black[3] = 255
	}
	sc, err := NewSampleColor(ctx, doc, dict["ColorSpace"], page.Resources, page.Dict, renderingIntent(ctx, doc, dict))
	if err != nil {
		return nil, fmt.Errorf("reading the image's colour space: %w", err)
	}
	if sc.Components() != components {
		return nil, fmt.Errorf("the image's colour space has %d components, its samples %d", sc.Components(), components)
	}
	if sc.PaintsNothing() {
		return make([]byte, components), nil
	}
	c, err := sc.Convert(black)
	if err != nil {
		return nil, fmt.Errorf("converting the conventional black: %w", err)
	}
	if max(c.R, c.G, c.B) <= nativeBlackLevel {
		return black, nil
	}
	return darkestSample(ctx, sc, components)
}

// darkness orders colours by how far from black they show, then by a
// tie-break; see [sampleDarkness] for the measures.
type darkness struct {
	far, tie float64
}

func (d darkness) less(o darkness) bool {
	return d.far < o.far || d.far == o.far && d.tie < o.tie
}

// sampleDarkness reads how dark the sample s of sc shows. A space an ICC
// transform converts is read in CIELAB as the colour difference from black,
// L* a* b* 0 0 0 (CIE 1976 ΔE*ab), a lightness below 0 counting as 0: a
// profile whose neutral black is a dark grey can reach a lower L* only off
// the neutral axis, in a strong hue, which is further from black than the
// grey. Any other space is read by the brightest channel of its sRGB colour,
// then the sum of the three.
//
// # Corpus evidence
//
//   - pdfjs issue2856: the version 4 YCC profile's neutral black, Y 0 with
//     Cb and Cr 128, is L* 10.9; Y 0, Cb 153, Cr 84 is L* 8.6 but a* -44,
//     b* -23, which poppler and PDFium draw teal (0, 23, 44).
func sampleDarkness(sc *SampleColor, s []byte) (darkness, error) {
	if sc.lab != nil {
		l, a, b := sc.lab(s)
		return darkness{far: math.Sqrt(max(l, 0)*max(l, 0) + a*a + b*b)}, nil
	}
	c, err := sc.Convert(s)
	if err != nil {
		return darkness{}, fmt.Errorf("converting a sample: %w", err)
	}
	return darkness{far: float64(max(c.R, c.G, c.B)), tie: float64(c.R) + float64(c.G) + float64(c.B)}, nil
}

// darkestSample searches the samples of sc for the darkest one (see
// [sampleDarkness]): the best point of a [nativeBlackGrid] grid, then a
// descent that moves one component at a time by a halving step while the
// colour gets darker. Ties keep the first sample found, so the result is
// deterministic.
func darkestSample(ctx context.Context, sc *SampleColor, n int) ([]byte, error) {
	if n < 1 || n > maxNativeBlackComponents {
		return nil, fmt.Errorf("searching the darkest sample of a space of %d components, past the %d this search bounds", n, maxNativeBlackComponents)
	}
	best := make([]byte, n)
	bestD, err := sampleDarkness(sc, best)
	if err != nil {
		return nil, err
	}
	cur := make([]byte, n)
	points := 1
	for range n {
		points *= nativeBlackGrid
	}
	for p := range points {
		if p%nativeBlackCheckEvery == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}
		for i, q := 0, p; i < n; i++ {
			cur[i] = byte(q % nativeBlackGrid * 255 / (nativeBlackGrid - 1))
			q /= nativeBlackGrid
		}
		d, err := sampleDarkness(sc, cur)
		if err != nil {
			return nil, err
		}
		if d.less(bestD) {
			bestD = d
			copy(best, cur)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	steps := 0
	for step := 255 / (nativeBlackGrid - 1); step >= 1; step /= 2 {
		for improved := true; improved && steps < maxNativeBlackSteps; steps++ {
			improved = false
			for i := range n {
				for _, delta := range [2]int{-step, step} {
					v := int(best[i]) + delta
					if v < 0 || v > 255 {
						continue
					}
					copy(cur, best)
					cur[i] = byte(v)
					d, err := sampleDarkness(sc, cur)
					if err != nil {
						return nil, err
					}
					if d.less(bestD) {
						bestD = d
						copy(best, cur)
						improved = true
					}
				}
			}
		}
	}
	return best, nil
}
