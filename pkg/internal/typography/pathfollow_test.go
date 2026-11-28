package typography

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// pathPoint returns the point and the baseline direction, in radians, of a
// path at the arc length s.
type pathPoint func(s float64) (x, y, angle float64)

// circlePath runs clockwise round a circle of radius r centred on (cx, cy),
// from the angle start, in degrees: glyphs set along it have their tops
// outside the circle, as on the top of an arch.
func circlePath(cx, cy, r, start float64) pathPoint {
	return func(s float64) (float64, float64, float64) {
		a := start*math.Pi/180 - s/r
		return cx + r*math.Cos(a), cy + r*math.Sin(a), a - math.Pi/2
	}
}

// wavePath runs along a sine wave of amplitude amp and wavelength length,
// from (x0, y0), sampled finely enough that its arc length is read within a
// hundredth of a point.
func wavePath(x0, y0, amp, length float64) pathPoint {
	const step = 0.01
	return func(s float64) (float64, float64, float64) {
		x, run := 0.0, 0.0
		slope := func(x float64) float64 { return amp * 2 * math.Pi / length * math.Cos(2*math.Pi*x/length) }
		for run < s {
			d := step / math.Hypot(1, slope(x))
			x += d
			run += step
		}
		return x0 + x, y0 + amp*math.Sin(2*math.Pi*x/length), math.Atan(slope(x))
	}
}

// onPath sets text along path at size, each glyph by a text matrix of its
// own with its centre on the path, as a converter sets warped text: every
// glyph is advance em wide and the glyphs of a word abut, and a word space
// is space em along the path.
func onPath(text string, path pathPoint, size, advance, space float64) []models.TextElement {
	var elems []models.TextElement
	s := 0.0
	w := advance * size
	for _, r := range text {
		if r == ' ' {
			s += space * size
			continue
		}
		cx, cy, a := path(s + w/2)
		elems = append(elems, models.TextElement{
			Text: string(r), X: cx - w/2*math.Cos(a), Y: cy - w/2*math.Sin(a),
			Width: w, FontSize: size, Angle: math.Remainder(a, 2*math.Pi), Chunk: len(elems) + 1, MCID: len(elems),
		})
		s += w
	}
	return elems
}

// TestReconstructText_ReadsWordsAlongAPath pins that glyphs set one by one
// along a path, each turned to it, read as the words the path shows: on an
// arch, most of the way round a circle, whose glyphs turn through every quarter, and
// along a wave, which turns one way and then the other. A word space of a
// quarter em along the path reads as a space; the glyphs of a word, which
// abut along the path, read as one word.
func TestReconstructText_ReadsWordsAlongAPath(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		text string
		path pathPoint
		size float64
	}{
		{"arch", "Fontwork on an arch", circlePath(300, 200, 120, 170), 36},
		{"circle", "Round the whole circle we go", circlePath(300, 300, 80, 90), 30},
		{"wave", "Riding the waves of a sine curve", wavePath(50, 400, 60, 600), 36},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			elems := onPath(tc.text, tc.path, tc.size, 0.5, 0.25)
			if got := ReconstructText(context.Background(), elems, nil); got != tc.text {
				t.Errorf("got %q, want %q", got, tc.text)
			}
		})
	}
}

// TestReconstructText_TurnedWordsApartStayApart pins that two words, each
// turned along a path of its own, stay two where the second does not start
// where the first ends: set a font size across the end of the first, or
// past a jump along it, it starts a word of its own.
func TestReconstructText_TurnedWordsApartStayApart(t *testing.T) {
	t.Parallel()

	first := onPath("River", circlePath(300, 200, 120, 120), 12, 0.5, 0.25)
	last := first[len(first)-1]
	endX := last.X + last.Width*math.Cos(last.Angle)
	endY := last.Y + last.Width*math.Sin(last.Angle)
	nx, ny := -math.Sin(last.Angle), math.Cos(last.Angle)
	for _, tc := range []struct {
		name   string
		origin func() (x, y float64)
	}{
		{"across", func() (float64, float64) { return endX + 12*nx, endY + 12*ny }},
		{"along", func() (float64, float64) {
			return endX + 12*math.Cos(last.Angle), endY + 12*math.Sin(last.Angle)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			x, y := tc.origin()
			second := onPath("Road", func(s float64) (float64, float64, float64) {
				a := last.Angle - 0.3 - s/150
				return x + 150*(math.Sin(last.Angle-0.3)-math.Sin(a)), y - 150*(math.Cos(last.Angle-0.3)-math.Cos(a)), a
			}, 12, 0.5, 0.25)
			elems := append(append([]models.TextElement{}, first...), second...)
			got := ReconstructText(context.Background(), elems, nil)
			if words := strings.Fields(got); len(words) != 2 || words[0] != "River" || words[1] != "Road" {
				t.Errorf("got %q, want the words River and Road apart", got)
			}
		})
	}
}

// TestFollowsOnPath_LeavesStraightLines pins that two glyphs set in one
// direction, upright or rotated, are left to the rules of a straight line:
// only a pair whose baselines turn is read along a path.
func TestFollowsOnPath_LeavesStraightLines(t *testing.T) {
	t.Parallel()

	for _, deg := range []float64{0, 30, 90, 180, -150} {
		a := models.TextElement{Text: "a", FontSize: 12, Width: 6, Angle: deg * math.Pi / 180}
		b := a
		b.X, b.Y = a.X+6*math.Cos(a.Angle), a.Y+6*math.Sin(a.Angle)
		if followsOnPath(a, b) {
			t.Errorf("%v°: a straight pair read along a path", deg)
		}
		b.Angle += 10 * math.Pi / 180
		if !followsOnPath(a, b) {
			t.Errorf("%v°: a pair turned by 10° where the first ends not read along a path", deg)
		}
	}
}
