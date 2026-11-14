package ooxml

import (
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

// rampEase is the exponent of the curve Office eases the run between two
// gradient stops by ([rampWeight]).
const rampEase = 1.6

// rampTolerance is the largest distance, in a component's [0, 1] range,
// between Office's blend and the straight line two neighbouring stops of
// [Ramp] paint: half a level of 255, so that a renderer drawing in eight
// bits draws the same levels.
const rampTolerance = 0.5 / 255

// rampDepth bounds the halvings of the run between two stops: at most 64
// pieces. The blend's steepest part, near a black stop, needs about
// twenty pieces to stay within [rampTolerance].
const rampDepth = 6

// Ramp returns stops that paint a DrawingML gradient as Office blends it,
// for a renderer that blends straight between neighbouring stops: the
// stops sorted by position and clamped to [0, 1]; for a gradient of two
// opaque stops of different colours at its ends ([blendsInLinearLight]),
// between them the stops of Office's blend, close enough that the
// straight line between two of them stays within half a level of it.
// Office blends those two stops in linear light (IEC 61966-2-1), its
// weight eased along the run by [rampWeight].
func Ramp(stops []RampStop) []RampStop {
	s := make([]RampStop, 0, len(stops)+2<<rampDepth)
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
	if !blendsInLinearLight(s) || sameRGB(s[0].Color, s[1].Color) {
		return s
	}
	a, b := s[0], s[1]
	return append(rampRun(s[:1], a, b, 0, 1, rampDepth), b)
}

// blendsInLinearLight reports whether Office blends the gradient of the
// sorted stops s in linear light, eased ([rampWeight]): one of two opaque
// stops, at 0 and at 1. Between the neighbouring stops of any other
// gradient it blends the sRGB components straight, as a PDF shading does.
// Two opaque stops away from the ends are blended straight: no Office
// drawing decides them.
//
// # Corpus evidence
//
//   - litho/pptx fill-gradient: PowerPoint writes each gradient of two
//     opaque stops at 0 and 100% as a sampled function on the eased
//     linear-light blend, and the gradients of six stops (FF0000 to
//     FF00FF) and of three (FFFFFF, FF0000, 000000) as stitched functions
//     straight between the stops.
//   - libreoffice/pptx oox/Scene3d_pureImage: the slide background of
//     four stops, ABC0E4 at 0 to 548235 at 100%, paints within one level
//     of the straight sRGB blend down its height (171,192,228 to
//     84,130,54); the eased linear-light blend is 28 levels away.
//   - poi/pptx slideshow/smartart-simple: a SmartArt fill of two stops at
//     0 and 100%, 01C3CC to 7D2AE7 at alpha 75%, is painted 1,195,204,
//     32,156,211, 63,117,217, 94,79,224 and 125,42,231 at even steps,
//     straight in sRGB; libreoffice/pptx chart2/tdf128345_ChartArea_CG_TS:
//     a chart area of FFFF00 at 20% and 4169E1 at 100%, both at alpha
//     30%, straight too.
//   - libreoffice/xlsx chart2/axis-label-rotation: Excel paints a chart
//     area of two opaque stops at 0 and 100%, 595959 to 262626, eased in
//     linear light (87, 84, 78 and 69 at an eighth to a half; the rule 88,
//     84, 78 and 70).
//   - litho/xlsx shapes_textboxes: Excel paints a shape's FFC000 to C00000
//     at 202,80,0, 226,141,0 and 247,179,0 at a quarter, a half and three
//     quarters: the rule gives 79, 141 and 179 green.
func blendsInLinearLight(s []RampStop) bool {
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
// b: each component blended in linear light by [rampWeight] at t, alpha
// straight by t.
func rampColor(a, b RampStop, t float64) css.Color {
	w := rampWeight(t)
	mix := func(x, y float64) float64 {
		return fromLinear(float64(toLinear(clamp01(x))*(1-w)) + float64(toLinear(clamp01(y))*w))
	}
	return css.Color{
		R: mix(a.Color.R, b.Color.R),
		G: mix(a.Color.G, b.Color.G),
		B: mix(a.Color.B, b.Color.B),
		A: a.Color.A + float64((b.Color.A-a.Color.A)*t),
	}
}

// rampWeight returns the share of the far stop at t in [0, 1] along a run
// of an Office gradient: t^k / (t^k + (1-t)^k) of k [rampEase], an S
// curve from 0 to 1 through 1/2 at the middle.
//
// # Corpus evidence
//
//   - litho/pptx background-gradient, background-theme, deck-pitch,
//     fill-gradient, text-fill-effects, text-warp-wordart; libreoffice/pptx
//     sd/slide-section-test, sd/tdf125551, sd/tdf125573_FontWorkScaleX,
//     oox/tdf139618_ImportWordArtGradient; openxmlsdk/pptx
//     2007MoCalHuman; litho/docx run-w14-effects, shapes-fills-lines,
//     shapes-wordart: PowerPoint and Word write each two-stop gradient as
//     a sampled function of 256 colours, every one on the linear-light
//     line between the stops (within two levels), at a share of the far
//     stop, the mean of the 14 distinct ramps, of 0.048, 0.145, 0.304,
//     0.506, 0.700, 0.858 and 0.953 at an eighth to seven eighths of the
//     run, the same on every channel, the ramps within 0.025 of each
//     other. The curve of k = 1.6 gives 0.043, 0.147, 0.306, 0.5, 0.694,
//     0.853 and 0.957; a blend of the sRGB components puts the middle of
//     4472C4 to FFC000 at 162,153,98 where PowerPoint paints 191,159,143,
//     the linear-light blend 193,159,143.
func rampWeight(t float64) float64 {
	if t <= 0 {
		return 0
	}
	if t >= 1 {
		return 1
	}
	p, q := detmath.Pow(t, rampEase), detmath.Pow(1-t, rampEase)
	return p / (p + q)
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
