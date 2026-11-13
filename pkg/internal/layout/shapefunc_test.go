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
