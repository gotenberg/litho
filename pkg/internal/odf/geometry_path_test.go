package odf

import (
	"math"
	"strconv"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/shape"
)

// cmdString writes commands as a kind letter and their rounded operands.
func cmdString(cmds []shape.Cmd) string {
	letters := map[shape.CmdKind]string{shape.CmdMove: "M", shape.CmdLine: "L", shape.CmdCubic: "C", shape.CmdQuad: "Q", shape.CmdClose: "Z"}
	s := ""
	for _, c := range cmds {
		if s != "" {
			s += " "
		}
		s += letters[c.Kind]
		for _, a := range c.Args {
			v := math.Round(a.Value*1000) / 1000
			if v == 0 {
				v = 0 // no negative zero
			}
			s += " " + trimFloat(v)
		}
	}
	return s
}

func trimFloat(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func TestPathData_Commands(t *testing.T) {
	tests := []struct{ name, d, want string }{
		{"absolute", "M0 0L10 0 10 10Z", "M 0 0 L 10 0 L 10 10 Z"},
		{"relative", "m1 1l2 0v2h-2z", "M 1 1 L 3 1 L 3 3 L 1 3 Z"},
		{"implicit lines after a move", "M0,0 5,5 10,0", "M 0 0 L 5 5 L 10 0"},
		{"compact numbers", "M.5.5L-1-1", "M 0.5 0.5 L -1 -1"},
		{"exponent", "M1e1 2E-1", "M 10 0.2"},
		{"cubic and smooth cubic", "M0 0C0 1 1 1 1 0S2 -1 2 0", "M 0 0 C 0 1 1 1 1 0 C 1 -1 2 -1 2 0"},
		{"quadratic and smooth", "M0 0Q1 1 2 0T4 0", "M 0 0 Q 1 1 2 0 Q 3 -1 4 0"},
		{"half circle arc", "M0 0A1 1 0 0 1 2 0", "M 0 0 C 0 -0.552 0.448 -1 1 -1 C 1.552 -1 2 -0.552 2 0"},
		{"compact arc flags", "M0 0a1 1 0 012 0", "M 0 0 C 0 -0.552 0.448 -1 1 -1 C 1.552 -1 2 -0.552 2 0"},
		{"zero radius arc is a line", "M0 0A0 1 0 0 1 2 0", "M 0 0 L 2 0"},
		{"error keeps the commands before", "M0 0L1 1L2", "M 0 0 L 1 1"},
		{"subpath start after close", "M1 1h1zl1 0", "M 1 1 L 2 1 Z L 2 1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := PathData(tt.d, compose.Rect{Width: 10, Height: 10})
			if c == nil {
				t.Fatal("no geometry")
			}
			if got := cmdString(c.Paths[0].Cmds); got != tt.want {
				t.Errorf("commands = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestPathData_Empty(t *testing.T) {
	for _, d := range []string{"", "  ", "L1 1", "Z", "x"} {
		if c := PathData(d, compose.Rect{}); c != nil {
			t.Errorf("PathData(%q) = %+v, want nil", d, c)
		}
	}
}

func TestPoints_Outline(t *testing.T) {
	tests := []struct {
		name, points string
		closed       bool
		want         string
		fill         shape.FillMode
	}{
		{"polygon", "0,0 10,0 10,10", true, "M 0 0 L 10 0 L 10 10 Z", shape.FillNorm},
		{"polyline", "0,0 10,10", false, "M 0 0 L 10 10", shape.FillNone},
		{"bad pair ends the list", "0,0 10,10 x,1", false, "M 0 0 L 10 10", shape.FillNone},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Points(tt.points, compose.Rect{Width: 10, Height: 10}, tt.closed)
			if c == nil {
				t.Fatal("no geometry")
			}
			if got := cmdString(c.Paths[0].Cmds); got != tt.want || c.Paths[0].Fill != tt.fill {
				t.Errorf("commands = %q fill %d, want %q fill %d", got, c.Paths[0].Fill, tt.want, tt.fill)
			}
		})
	}
	if c := Points("1,1", compose.Rect{}, true); c != nil {
		t.Errorf("one point = %+v, want nil", c)
	}
}

func TestRegularPolygon_Corners(t *testing.T) {
	tests := []struct {
		name      string
		corners   int
		concave   bool
		sharpness float64
		want      string
	}{
		{"square sits on a corner", 4, false, 0, "M 0 -1 L 1 0 L 0 1 L -1 0 Z"},
		// An even star starts at an inner corner on top, an odd one at an
		// outer corner (ODF 1.3 Part 3, 19.211).
		{"four-pointed star", 4, true, 50, "M -0.707 -0.707 L 0 -0.5 L 0.707 -0.707 L 0.5 0 L 0.707 0.707 L 0 0.5 L -0.707 0.707 L -0.5 0 Z"},
		{"triangle star", 3, true, 100, "M 0 -1 L 0 0 L 0.866 0.5 L 0 0 L -0.866 0.5 L 0 0 Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := RegularPolygon(tt.corners, tt.concave, tt.sharpness)
			if c == nil {
				t.Fatal("no geometry")
			}
			if got := cmdString(c.Paths[0].Cmds); got != tt.want {
				t.Errorf("commands = %q, want %q", got, tt.want)
			}
		})
	}
	if RegularPolygon(2, false, 0) != nil || RegularPolygon(maxPolygonCorners+1, false, 0) != nil {
		t.Error("a polygon out of range has a geometry")
	}
}
