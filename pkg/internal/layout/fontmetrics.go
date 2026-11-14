package layout

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/css"
	"github.com/gotenberg/litho/pkg/internal/text"
)

// pixelMetrics returns the ascent, descent and line gap of a face, in
// points, rounded to whole CSS pixels as Blink rounds them before any
// layout reads them: the ascent and descent to the nearest pixel, halves
// up (blink::FontMetrics::AscentDescentWithHacks, SkScalarRoundToScalar),
// and the line gap to the nearest, halves away from zero, the normal line
// height being their sum (blink::SimpleFontData::PlatformInit,
// SetLineSpacing(lroundf(ascent) + lroundf(descent) + lroundf(line_gap))).
// Blink keeps fractional metrics only for a canvas's tiny fonts
// (subpixel_ascent_descent, false for document text), so every line box
// of a document sits on whole pixels of font metrics.
func pixelMetrics(ascent, descent, gap float64) (float64, float64, float64) {
	px := func(v float64) float64 { return math.Floor(v/css.Pt+0.5) * css.Pt }
	return px(ascent), px(descent), math.Round(gap/css.Pt) * css.Pt
}

// roundMetrics returns m with its ascent, descent and line gap rounded as
// [pixelMetrics] rounds them.
func roundMetrics(m text.Metrics) text.Metrics {
	m.Ascent, m.Descent, m.LineGap = pixelMetrics(m.Ascent, m.Descent, m.LineGap)
	return m
}

// blinkLength returns the length v, in points, rounded to the nearest of
// Blink's layout units of 1/64 px, as Blink lays out a line-height length.
func blinkLength(v float64) float64 {
	return float64(math.Round(v/css.Pt*64) / 64 * css.Pt)
}

// blinkLineHeight returns the line height a number sets on a font of size
// points as Blink lays it out on its layout unit of 1/64 px: the size
// rounded to a unit, times the number in single precision as Blink takes
// it, floored to a unit. CSS 2.1, 10.8.1 computes the used value from the
// number and the font size alone; the units are Blink's, whose line boxes
// stack at these heights.
//
// # Corpus evidence
//
//   - probe, html: Chromium of gotenberg/gotenberg:9.4.1 stacks twenty
//     lines of a 15.33 px font at line-height 1.5 in 459.6875 px (22.984375
//     a line), of a 14 px font at 1.8 in 503.75 px (25.1875), at 137% in
//     383.75 px (19.1875) and at 20.3px in 405.9375 px (20.296875); 55
//     line heights (numbers, percentages, em, px, pt and calc() at five font
//     sizes) take the number rule or the length rule of [blinkLength] to
//     the unit.
func blinkLineHeight(size, number float64) float64 {
	fixed := float32(math.Round(size/css.Pt*64) / 64)
	return float64(math.Floor(float64(fixed*float32(number))*64) / 64 * css.Pt)
}
