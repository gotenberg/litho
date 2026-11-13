package chart

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/numeric"
)

// office reports a chart drawn by Office's rules.
func (rd *renderer) office() bool { return rd.c.Rules == OfficeRules }

// officeOrigin returns the value Office counts the ticks of the linear
// scale sc from: its minimum, so that a minimum the file fixes off the
// major unit's multiples still bears the first label.
//
// # Corpus evidence
//
//   - litho/xlsx: an axis fixed from 50 to 200 with an automatic unit of
//     20 is labelled 50, 70, ..., 190 by Excel.
func officeOrigin(sc scale) float64 {
	if sc.log > 0 {
		return 0
	}
	return sc.min
}

// officeIntervals returns the most major intervals Office's automatic
// scale gives a value axis length points long whose labels are lineH
// points high: as many labels as fit end to end along it, one interval
// fewer, at least one and at most maxAutoTicks. Each label takes its
// line height rounded to a whole pixel at 96 pixels an inch.
//
// The rounding is the one rule found that fits Excel's and PowerPoint's
// choices together: a height in proportion to the size fits either
// application alone, not both, and floor or ceiling rounding fit
// neither.
//
// # Corpus evidence
//
//   - probe, xlsx: on 80 column charts of one series, frames 5 to 30 rows
//     high, labels of 8, 10, 14 and 18 pt Calibri, Excel's choice is the
//     smallest step of 1, 2 and 5 times a power of ten whose labels fit
//     by this count, on every chart. The tightest charts bound the height
//     of a 10 pt label to (11.77, 12.22] pt and of a 14 pt one to more
//     than 17.08 pt (five labels refused on 85.42 pt). Charts too short
//     for two labels keep one interval.
//   - probe, pptx: on 12 column charts with 10 pt labels, PowerPoint's
//     choice follows the same count with a label of at most 12.13 pt (six
//     labels on 72.8 pt), so a height of 1.2207 ems, Calibri's line,
//     misses one chart there, and 1.20 ems misses Excel's 14 pt chart.
//     The rounded height (12.0 pt at 10 pt, 17.25 pt at 14 pt) fits all
//     92.
func officeIntervals(length, lineH float64) int {
	px := math.Round(lineH * 96 / 72)
	if !numeric.Positive(px) {
		return maxAutoTicks
	}
	n := math.Floor(length/(px*72/96)) - 1
	return int(math.Max(1, math.Min(n, maxAutoTicks)))
}
