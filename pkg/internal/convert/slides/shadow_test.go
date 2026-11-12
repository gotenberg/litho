package slides

import (
	"math"
	"testing"
)

// TestTurnedShadow_TurnsOffsetsWithTheShape locks ECMA-376 Part 1,
// 20.1.8.45 outerShdw rotWithShape: a shadow that turns with its shape
// has its offsets turned clockwise by the shape's rotation, one that does
// not keeps the slide's.
//
// # Corpus evidence
//
//   - litho/pptx: effects-shadow prints in PowerPoint the shadow of its
//     right arrow turned 90 degrees, 15 pt toward 0 degrees with
//     rotWithShape 1, 15 pt below the arrow.
func TestTurnedShadow_TurnsOffsetsWithTheShape(t *testing.T) {
	t.Parallel()
	near := func(a, b float64) bool { return math.Abs(a-b) < 1e-9 }
	s := &Shape{Rotation: 90, Shadow: &Shadow{OffsetX: 15, RotateWithShape: true}}
	got := turnedShadow(s)
	if !near(got.Shadow.OffsetX, 0) || !near(got.Shadow.OffsetY, 15) {
		t.Errorf("offsets %v, %v, want 0, 15", got.Shadow.OffsetX, got.Shadow.OffsetY)
	}
	if s.Shadow.OffsetX != 15 || s.Shadow.OffsetY != 0 {
		t.Errorf("the shape's own shadow changed: %+v", s.Shadow)
	}
	fixed := &Shape{Rotation: 30, Shadow: &Shadow{OffsetX: 15}}
	if turnedShadow(fixed) != fixed {
		t.Error("a shadow that does not turn with its shape moved")
	}
}
