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
