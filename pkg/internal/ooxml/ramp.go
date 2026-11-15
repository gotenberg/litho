package ooxml

import (
	"math"
	"slices"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/detmath"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// RampStop is one stop of a DrawingML gradient (ECMA-376 Part 1, 20.1.8.36
// gs): its position in [0, 1] and its colour.
type RampStop struct {
	Pos   float64
	Color css.Color
}

// rampSigma is the standard deviation, in shares of the run, of the
// normal distribution whose cumulative curve eases the run between two
// gradient stops ([rampWeight]).
const rampSigma = 0.25

// rampGamma is the exponent of the power curve Office blends the two
// stops of a run on ([rampColor]).
const rampGamma = 2.2

// rampTolerance is the largest distance, in a component's [0, 1] range,
// between Office's blend and the straight line two neighbouring stops of
// [Ramp] paint: half a level of 255, so that a renderer drawing in eight
// bits draws the same levels.
const rampTolerance = 0.5 / 255

// rampDepth bounds the halvings of the run between two stops: at most 256
// pieces. The power curve is steepest where a channel nears 0, which
// needs pieces of 1/256 of the run there to stay within [rampTolerance];
// a run takes about twenty pieces in all.
const rampDepth = 8

// rampPieces is the room [Ramp] makes for the stops of a run: about what
// one takes.
const rampPieces = 32

// Ramp returns stops that paint a DrawingML gradient as Office blends it,
// for a renderer that blends straight between neighbouring stops: the
// stops sorted by position and clamped to [0, 1]; for a gradient of two
// opaque stops of different colours at its ends ([blendsEased]),
// between them the stops of Office's blend, close enough that the
// straight line between two of them stays within half a level of it.
// Office blends those two stops on a power curve of [rampGamma], its
// weight eased along the run by [rampWeight].
func Ramp(stops []RampStop) []RampStop {
	s := make([]RampStop, 0, len(stops)+rampPieces)
	for _, st := range stops {
		st.Pos = clamp01(st.Pos)
		s = append(s, st)
	}
	slices.SortStableFunc(s, func(a, b RampStop) int {
		switch {
		case a.Pos < b.Pos:
			return -1
		case a.Pos > b.Pos:
			return 1
		}
		return 0
	})
	if !blendsEased(s) || sameRGB(s[0].Color, s[1].Color) {
		return s
	}
	a, b := s[0], s[1]
	return append(rampRun(s[:1], a, b, 0, 1, rampDepth), b)
}

// blendsEased reports whether Office blends the gradient of the
// sorted stops s on its power curve, eased ([rampColor]): one of two opaque
// stops, at 0 and at 1. Between the neighbouring stops of any other
// gradient it blends the sRGB components straight, as a PDF shading does.
//
// # Corpus evidence
//
//   - probe, pptx: PowerPoint writes 4472C4 at 25% to FFC000 at 75% as
//     flat ends and a straight run between them; 0000FF, FFFF00 at 30%
//     and FF0000 as two straight runs; C00000 at alpha 100% to 0 as one
//     colour under a straight soft mask.
//   - litho/pptx fill-gradient: PowerPoint writes each gradient of two
//     opaque stops at 0 and 100% as a sampled function on the eased
//     blend, and the gradients of six stops (FF0000 to
//     FF00FF) and of three (FFFFFF, FF0000, 000000) as stitched functions
//     straight between the stops.
//   - libreoffice/pptx oox/Scene3d_pureImage: the slide background of
//     four stops, ABC0E4 at 0 to 548235 at 100%, paints within one level
//     of the straight sRGB blend down its height (171,192,228 to
//     84,130,54); the eased blend is 28 levels away.
//   - poi/pptx slideshow/smartart-simple: a SmartArt fill of two stops at
//     0 and 100%, 01C3CC to 7D2AE7 at alpha 75%, is painted 1,195,204,
//     32,156,211, 63,117,217, 94,79,224 and 125,42,231 at even steps,
//     straight in sRGB; libreoffice/pptx chart2/tdf128345_ChartArea_CG_TS:
//     a chart area of FFFF00 at 20% and 4169E1 at 100%, both at alpha
//     30%, straight too.
//   - libreoffice/xlsx chart2/axis-label-rotation: Excel paints a chart
//     area of two opaque stops at 0 and 100%, 595959 to 262626, eased
//     (87, 84, 78 and 69 at an eighth to a half; the rule gives the
//     same).
//   - litho/xlsx shapes_textboxes: Excel paints a shape's FFC000 to C00000
//     at 202,80,0, 226,141,0 and 247,179,0 at a quarter, a half and three
//     quarters: the rule gives 79, 140 and 179 green.
func blendsEased(s []RampStop) bool {
	return len(s) == 2 && s[0].Pos == 0 && s[1].Pos == 1 && s[0].Color.A >= 1 && s[1].Color.A >= 1
}

// rampRun appends to out the stops strictly inside the part [t0, t1] of
// the run from a to b, halving it while the straight line between its ends
// strays from Office's blend by more than rampTolerance, at most depth
// times.
func rampRun(out []RampStop, a, b RampStop, t0, t1 float64, depth int) []RampStop {
	c0, c1 := rampColor(a, b, t0), rampColor(a, b, t1)
	far := false
	for _, f := range [...]float64{0.25, 0.5, 0.75} {
		t := t0 + float64((t1-t0)*f)
		c := rampColor(a, b, t)
		for _, d := range [...]float64{
			c.R - (c0.R + float64((c1.R-c0.R)*f)),
			c.G - (c0.G + float64((c1.G-c0.G)*f)),
			c.B - (c0.B + float64((c1.B-c0.B)*f)),
		} {
			if d > rampTolerance || d < -rampTolerance {
				far = true
			}
		}
	}
	if !far || depth == 0 {
		return out
	}
	tm := (t0 + t1) / 2
	out = rampRun(out, a, b, t0, tm, depth-1)
	out = append(out, RampStop{Pos: a.Pos + float64((b.Pos-a.Pos)*tm), Color: rampColor(a, b, tm)})
	return rampRun(out, a, b, tm, t1, depth-1)
}

// rampColor returns Office's colour at t in [0, 1] along the run from a to
// b: each component raised to [rampGamma], blended by [rampWeight] at t
// and raised back, alpha straight by t. Office holds the blend on the
// power curve in steps of 1/1023, which puts the samples of a channel
// near 0 on 0, 11, 15, 18 and so on; litho blends it smooth, which parts
// from Office's samples by more than a level only there, by up to 8 over
// the last few samples before 0.
//
// # Corpus evidence
//
//   - probe, pptx: PowerPoint writes a gradient of 000000 at 0 to FFFFFF
//     at 100% as 256 samples: 0, 11, 15, 18, 21 at the first five, 63,
//     106, 148, 187, 217, 238 and 250 at an eighth to seven eighths. The
//     power curve of 2.2 under [rampWeight] in steps of 1/1023 gives all
//     256 exactly, smooth 233 and the others within a level; the sRGB
//     curve under the weight t^1.6 / (t^1.6 + (1-t)^1.6) is up to 17
//     levels away (5 at an eighth). Read on the power curve the samples
//     are symmetric about the middle within a level, on the sRGB curve
//     they are not by up to 5. A circle path gradient of the same stops
//     has the same samples, from the centre out.
//   - litho/pptx, litho/docx, litho/xlsx, libreoffice/pptx,
//     libreoffice/docx, libreoffice/xlsx, openxmlsdk/pptx,
//     openxmlsdk/docx, openxmlsdk/xlsx, poi/pptx: of the 38 distinct
//     sampled gradients of two colours in Office's references, the rule
//     in steps of 1/1023 gives every sample of 22 exactly, of 15 within a
//     level, of 2 within 2; smooth, 30 within a level, 3 within 3, and 6
//     within 8 at their last samples before 0. The sRGB curve under t^1.6
//     is up to 17 levels away. Word and Excel blend as PowerPoint does.
func rampColor(a, b RampStop, t float64) css.Color {
	w := rampWeight(t)
	mix := func(x, y float64) float64 {
		return detmath.Pow(float64(detmath.Pow(clamp01(x), rampGamma)*(1-w))+float64(detmath.Pow(clamp01(y), rampGamma)*w), 1/rampGamma)
	}
	return css.Color{
		R: mix(a.Color.R, b.Color.R),
		G: mix(a.Color.G, b.Color.G),
		B: mix(a.Color.B, b.Color.B),
		A: a.Color.A + float64((b.Color.A-a.Color.A)*t),
	}
}

// rampWeight returns the share of the far stop at t in [0, 1] along a run
// of an Office gradient: the cumulative normal distribution of standard
// deviation [rampSigma] centred on the middle of the run, scaled so that
// it runs from 0 at the near stop to 1 at the far one, 1/2 at the middle.
// Its slope is 0.23 at the ends and 1.67 in the middle.
func rampWeight(t float64) float64 {
	if t <= 0 {
		return 0
	}
	if t >= 1 {
		return 1
	}
	// Φ((t - 1/2) / σ) = (1 + erf((t - 1/2) / (σ√2))) / 2, normalised by
	// Φ(±1/(2σ)).
	k := 1 / (rampSigma * math.Sqrt2)
	edge := detmath.Erf(0.5 * k)
	return 0.5 + float64(detmath.Erf((t-0.5)*k)/edge*0.5)
}

// sameRGB reports whether a and b have the same colour, alpha aside.
func sameRGB(a, b css.Color) bool {
	return a.R == b.R && a.G == b.G && a.B == b.B
}

// rampStops returns typeset stops that paint a gradient as Office blends
// them ([Ramp]).
func rampStops(stops []typeset.ColorStop) []typeset.ColorStop {
	in := make([]RampStop, len(stops))
	for i, st := range stops {
		in[i] = RampStop{Pos: st.Offset, Color: css.Color{R: st.Color.R, G: st.Color.G, B: st.Color.B, A: st.Color.A}}
	}
	out := Ramp(in)
	res := make([]typeset.ColorStop, len(out))
	for i, st := range out {
		res[i] = typeset.ColorStop{Offset: st.Pos, Color: typeset.Color{R: st.Color.R, G: st.Color.G, B: st.Color.B, A: st.Color.A}}
	}
	return res
}

// rampTextStops returns the stops of a text fill that paint it as Word
// blends them ([Ramp]).
func rampTextStops(stops []css.OfficeStop) []css.OfficeStop {
	in := make([]RampStop, len(stops))
	for i, st := range stops {
		in[i] = RampStop{Pos: st.Pos, Color: st.Color}
	}
	out := Ramp(in)
	res := make([]css.OfficeStop, len(out))
	for i, st := range out {
		res[i] = css.OfficeStop{Pos: st.Pos, Color: st.Color}
	}
	return res
}
