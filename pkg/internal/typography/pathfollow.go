package typography

import (
	"math"

	"github.com/gotenberg/litho/pkg/internal/detmath"
	"github.com/gotenberg/litho/pkg/models"
)

// followsOnPath reports whether curr continues the line of prev, the last
// glyph that painted before it, because the two are set one after the other
// along one path, each by a text matrix of its own turned to the path (ISO
// 32000-1, 9.4.2 and 9.4.4): text on a circle, an arch or a wave. The pair
// turns by more than [sameDirectionTolerance] and at most [maxCurveTurn], and
// the origin of curr sits where the advance of prev ends, as the producer
// spaces glyphs along a path: measured from that end along the mean of the
// two baselines, it lies after a move back of less than
// [negativeRunGapFactor] of the size of prev and before a jump past
// [spaceInFlowMaxFactor] of it, and across that mean by no more than
// [verticalShiftRatio] of it. Those are the bounds within which PDF.js
// continues a text item on a straight line (src/core/evaluator.js,
// compareWithLastPosition), here read along the path instead of along one
// baseline, so the glyphs of a turned word read as one word whatever each
// one's rotation, where reading each turned glyph against a straight
// baseline split it ("F|o|ntw|ork"). Two glyphs in one direction, any
// straight line, are left to the rules of that line, and so are vertical
// writing and two glyphs inked on opposite sides of their advance
// ([curveTurn]). The test reads the two glyphs alone, so its cost is fixed
// per glyph and a page of turned glyphs costs no more than one of upright
// ones. ISO 32000-1, 14.8.2.5 leaves line breaks to the consumer, so the
// bounds are the reference's, not the specification's.
//
// # Corpus evidence
//
//   - libreoffice/pptx tdf125582_TextOnCircle, tdf116350-texteffects,
//     tdf125573_FontWorkScaleX: litho sets each glyph of warped text by a
//     text matrix of its own, centred on the path. Inside a word the origin
//     of a glyph sits within 0.10 em of the end of the advance before it
//     along the mean baseline and within 0.04 em across it, at turns of up
//     to 29°; a word space sits 0.22 to 0.30 em along. Read against one
//     baseline, a pair 2.4 units apart across it at 35.9 points broke the
//     line.
func followsOnPath(prev, curr models.TextElement) bool {
	along, across, ok := pathOffset(prev, curr)
	if !ok {
		return false
	}
	size := math.Abs(prev.FontSize)
	return along > negativeRunGapFactor*size && along <= spaceInFlowMaxFactor*size && math.Abs(across) <= verticalShiftRatio*size
}

// pathOffset returns where the origin of curr sits from the end of the
// advance of prev, along the mean direction of their two baselines and
// across it, for two glyphs that turn by more than [sameDirectionTolerance]
// and at most [maxCurveTurn] ([curveTurn]), and false for any other pair.
// Two glyphs centred on a circle, their baselines tangent to it, sit along
// the chord between their centres, and the chord runs along the mean of
// their tangents: the offset across it is half the difference of their
// widths times the sine of half the turn, near zero, where measured across
// either baseline it grows with the turn.
func pathOffset(prev, curr models.TextElement) (along, across float64, ok bool) {
	if prev.Width < 0 {
		return 0, 0, false
	}
	turn, ok := curveTurn(prev, curr)
	if !ok || math.Abs(turn) <= sameDirectionTolerance || math.Abs(turn) > maxCurveTurn {
		return 0, 0, false
	}
	endX := prev.X + float64(prev.Width*detmath.Cos(prev.Angle))
	endY := prev.Y + float64(prev.Width*detmath.Sin(prev.Angle))
	dx, dy := curr.X-endX, curr.Y-endY
	cos, sin := meanDirection(prev, curr)
	return float64(dx*cos) + float64(dy*sin), float64(-dx*sin) + float64(dy*cos), true
}

// meanDirection returns the cosine and sine of the direction halfway
// between the baselines of prev and curr, the short way round, so two
// glyphs either side of a half turn, at 179° and -179°, point at 180° and
// not at 0°.
func meanDirection(prev, curr models.TextElement) (cos, sin float64) {
	mean := prev.Angle + float64(math.Remainder(curr.Angle-prev.Angle, 2*math.Pi)/2)
	return detmath.Cos(mean), detmath.Sin(mean)
}

// pathChord returns how far the origin of curr sits from the origin of prev
// along the mean of their baselines ([meanDirection]): along the chord
// between two glyphs set on one path.
func pathChord(prev, curr models.TextElement) float64 {
	cos, sin := meanDirection(prev, curr)
	return float64((curr.X-prev.X)*cos) + float64((curr.Y-prev.Y)*sin)
}
