package typography

import (
	"context"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// curveGlyph is one glyph of a label set along a curve, each glyph by a text
// matrix of its own: its origin, advance and baseline angle in degrees.
type curveGlyph struct {
	text    string
	x, y, w float64
	deg     float64
}

func curveElements(glyphs []curveGlyph, size float64, inkRight bool) []models.TextElement {
	elems := make([]models.TextElement, len(glyphs))
	for i, g := range glyphs {
		elems[i] = models.TextElement{
			Text: g.text, X: g.x, Y: g.y, Width: g.w, FontSize: size,
			Angle: g.deg * math.Pi / 180, Chunk: i + 1, InkRight: inkRight,
		}
	}
	return elems
}

// tankFarm is "Tank Farm" as govdocs thread_400_400902.pdf page 2 sets it,
// 9 points, along a path about 0.43 em above the baseline that turns toward
// the tops of the glyphs.
var tankFarm = []curveGlyph{
	{"T", 291.11, 155.32, 4.28, 90.2},
	{"a", 291.10, 158.94, 4.65, 91.0},
	{"n", 291.03, 163.71, 4.58, 92.7},
	{"k", 290.80, 168.48, 3.86, 95.7},
	{"F", 289.96, 175.36, 4.67, 107.2},
	{"a", 288.25, 180.65, 4.65, 125.8},
	{"r", 284.17, 185.45, 3.26, 148.4},
	{"m", 280.34, 187.78, 7.00, 163.9},
}

// verkinCreek is "Verkin Creek" as govdocs thread_200_200196.pdf page 6 sets
// it, 5.24 points, along a river on a path at the baseline: its letters sit
// 0.1 point apart whatever the turn, and its word space turns by 31.8°.
var verkinCreek = []curveGlyph{
	{"V", 223.57, 716.34, 3.50, 64.6},
	{"e", 225.01, 719.32, 2.91, 70.2},
	{"r", 225.92, 722.20, 2.04, 81.1},
	{"k", 226.25, 724.32, 2.91, 80.0},
	{"i", 226.79, 727.30, 1.46, 79.7},
	{"n", 227.08, 728.83, 3.20, 87.1},
	{"C", 226.75, 733.64, 3.20, 118.9},
	{"r", 225.12, 736.54, 2.04, 102.5},
	{"e", 224.63, 738.56, 2.91, 88.7},
	{"e", 224.73, 741.59, 2.91, 73.1},
	{"k", 225.65, 744.47, 2.91, 63.9},
}

// TestReconstructText_CurvedWordKeepsItsLetters pins that a word set glyph by
// glyph along a path above its baseline that turns toward the glyphs' tops
// reads whole: the baseline gaps open on the outside of the turn while the
// glyphs meet at the path. The word space before "Farm", 3 points at the
// baseline over a turn of 11.5°, stays.
func TestReconstructText_CurvedWordKeepsItsLetters(t *testing.T) {
	t.Parallel()

	got := ReconstructText(context.Background(), curveElements(tankFarm, 9, false), nil)
	if want := "Tank Farm"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestReconstructText_CurveOnItsBaselineKeepsWordSpace pins that a label
// whose path runs along its baseline keeps a word space set where the path
// turns sharply: its letter pairs, 0.1 point apart over turns of 7° and 11°,
// put the path at the baseline, so the turn closes the space by little.
func TestReconstructText_CurveOnItsBaselineKeepsWordSpace(t *testing.T) {
	t.Parallel()

	got := ReconstructText(context.Background(), curveElements(verkinCreek, 5.24, false), nil)
	if want := "Verkin Creek"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestReconstructText_CurveTurningDownKeepsBaselineGap pins that a curve
// turning toward the glyphs' feet reads its gaps at the baseline, where the
// wedge between two glyphs is narrowest: the same label with its glyphs inked
// on the other side of the advance turns away from their tops, so every gap
// past the tracking bound stays a space.
func TestReconstructText_CurveTurningDownKeepsBaselineGap(t *testing.T) {
	t.Parallel()

	elems := curveElements(tankFarm, 9, true)
	if r := curveRise(elems); r != 0 {
		t.Errorf("curveRise = %v for a curve turning toward the glyphs' feet, want 0", r)
	}
	if got, want := ReconstructText(context.Background(), elems, nil), "Tank Fa r m"; got != want {
		t.Errorf("got %q, want the baseline gaps %q", got, want)
	}
}

// TestCurveRise_OnePairReadsNothing pins that one turning pair does not set a
// path's height: it cannot tell a letter pair from a word gap.
func TestCurveRise_OnePairReadsNothing(t *testing.T) {
	t.Parallel()

	elems := curveElements(tankFarm[3:5], 9, false)
	if r := curveRise(elems); r != 0 {
		t.Errorf("curveRise = %v from one pair, want 0", r)
	}
	if r := curveRise(curveElements(tankFarm, 9, false)); r <= 0 || r > glyphBodyRatio {
		t.Errorf("curveRise = %v for Tank Farm, want within (0, %v]", r, glyphBodyRatio)
	}
}

// TestCurveClosing_StraightTextUnchanged pins that glyphs set in one
// direction, whatever it is, and vertical writing close in by nothing, so a
// straight line of text reads its gaps as before.
func TestCurveClosing_StraightTextUnchanged(t *testing.T) {
	t.Parallel()

	for _, deg := range []float64{0, 30, 90, 180, -90, -150} {
		a := models.TextElement{Text: "a", FontSize: 12, Angle: deg * math.Pi / 180}
		b := a
		if c := curveClosing(a, b, glyphBodyRatio); c != 0 {
			t.Errorf("%v°: curveClosing = %v, want 0", deg, c)
		}
		b.Angle += sameDirectionTolerance / 2
		if c := curveClosing(a, b, glyphBodyRatio); c != 0 {
			t.Errorf("%v° and a rounding turn: curveClosing = %v, want 0", deg, c)
		}
	}
	a := models.TextElement{Text: "a", FontSize: 12, Vertical: true}
	b := models.TextElement{Text: "b", FontSize: 12, Vertical: true, Angle: 0.3}
	if c := curveClosing(a, b, glyphBodyRatio); c != 0 {
		t.Errorf("vertical writing: curveClosing = %v, want 0", c)
	}
	b.Vertical = false
	a.Vertical = false
	if c := curveClosing(a, b, 0); c != 0 {
		t.Errorf("a line with no path height: curveClosing = %v, want 0", c)
	}
}

// TestCurveTurn_AcrossHalfTurn pins that a turn is read the short way round:
// from 170° to -170° the text turns 20° left, not 340° right.
func TestCurveTurn_AcrossHalfTurn(t *testing.T) {
	t.Parallel()

	a := models.TextElement{Text: "a", FontSize: 10, Angle: 170 * math.Pi / 180}
	b := models.TextElement{Text: "b", FontSize: 10, Angle: -170 * math.Pi / 180}
	if turn, ok := curveTurn(a, b); !ok || math.Abs(turn-20*math.Pi/180) > 1e-9 {
		t.Errorf("curveTurn = %v, %v, want 20° in radians", turn, ok)
	}
	if turn, ok := curveTurn(b, a); !ok || math.Abs(turn+20*math.Pi/180) > 1e-9 {
		t.Errorf("reverse curveTurn = %v, %v, want -20° in radians", turn, ok)
	}
}
