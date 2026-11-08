package odf

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/shape"
)

func FuzzPathData(f *testing.F) {
	f.Add("M0 0h10v10zm1 1c1 2 3 4 5 6s1 1 2 2q1 1 2 2t3 3a5 5 30 1 0 10 10L-1e3.5", "0,0 10,10 5,1e2")
	f.Add("M0 0A", "0,")
	f.Fuzz(func(t *testing.T, d, points string) {
		view := compose.Rect{Width: 100, Height: 100}
		r := compose.Rect{Width: 50, Height: 20}
		for _, c := range []*shape.Custom{PathData(d, view), Points(points, view, true), Points(points, view, false)} {
			if c == nil {
				continue
			}
			g := &shape.Geometry{Custom: c}
			_ = shape.Check(g)
			for _, p := range shape.Paths(g, r) {
				if len(p.Ops) > 0 && p.Ops[0].Kind != compose.MoveTo {
					t.Fatal("a path does not start with a move")
				}
			}
			if _, ok := Bounds(c.Paths[0].Cmds); !ok {
				t.Fatal("a geometry without a point")
			}
		}
	})
}
