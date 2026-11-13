package layout

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/css"
)

func TestShapePathData_ResolvesAgainstTheBox(t *testing.T) {
	// CSS Shapes 1, 3.2.1: a shape()'s horizontal lengths resolve against
	// the reference box's width, its vertical ones against its height;
	// each command draws the SVG path command of the same name.
	pct := func(v float64) css.Dimension { return css.Dimension{Kind: css.Percent, Value: v} }
	s := &css.Shape{Kind: css.ShapeShape, Commands: []css.ShapeCommand{
		{Op: 'M', Args: []css.Dimension{pct(0.5), pct(0.5)}, Vertical: []bool{false, true}},
		{Op: 'h', Args: []css.Dimension{css.Pts(px(10))}, Vertical: []bool{false}},
		{Op: 'A', Args: []css.Dimension{css.Pts(px(5)), css.Pts(px(5)), pct(1), pct(0)}, Vertical: []bool{false, true, false, true}, Rotate: 30, Sweep: true},
		{Op: 'Z'},
	}}
	if got, want := shapePathData(s, px(200), px(100)), "M 100 50 h 10 A 5 5 30 0 1 200 0 Z "; got != want {
		t.Errorf("path data %q, want %q", got, want)
	}
}

func TestShapePathData_OneArcRadiusOfTheDiagonal(t *testing.T) {
	// CSS Shapes 1, 3.1.1: an arc written with one radius resolves its
	// percentage against the box's diagonal over the square root of 2,
	// 500 / sqrt(2) for a box 400 by 300.
	pct := func(v float64) css.Dimension { return css.Dimension{Kind: css.Percent, Value: v} }
	s := &css.Shape{Kind: css.ShapeShape, Commands: []css.ShapeCommand{
		{Op: 'M', Args: []css.Dimension{css.Pts(0), css.Pts(0)}, Vertical: []bool{false, true}},
		{Op: 'A', Args: []css.Dimension{pct(0.5), pct(0.5), css.Pts(0), css.Pts(px(10))}, Vertical: []bool{false, true, false, true}, OneRadius: true},
	}}
	if got, want := shapePathData(s, px(400), px(300)), "M 0 0 A 176.77669529663686 176.77669529663686 0 0 0 0 10 "; got != want {
		t.Errorf("path data %q, want %q", got, want)
	}
}
