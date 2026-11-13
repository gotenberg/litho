package css

import "testing"

// TestShapeFunction_Commands holds shape() to CSS Shapes 1, 3.2.1: from a
// position, then commands that draw the SVG path commands of the same
// name, "to" absolute and "by" relative; a malformed command makes the
// value invalid.
func TestShapeFunction_Commands(t *testing.T) {
	t.Parallel()
	tests := []struct {
		value string
		ops   string
	}{
		{"shape(from 0 0, line to 10px 0, close)", "MLZ"},
		{"shape(evenodd from 0 0, hline by 10px, vline to bottom, close)", "MhVZ"},
		{"shape(from 0 0, curve to 10px 10px with 5px 0 / 10px 5px)", "MC"},
		{"shape(from 0 0, curve by 10px 10px with 5px 0)", "Mq"},
		{"shape(from 0 0, smooth to 10px 10px)", "MT"},
		{"shape(from 0 0, arc to 10px 0 of 5px cw large rotate 10deg)", "MA"},
		{"shape(from 0 0, move by 1px 1px, line to 50% 50%)", "MmL"},
		{"shape(from 0 0)", ""},
		{"shape(0 0, line to 1px 1px)", ""},
		{"shape(from 0 0, line 1px 1px)", ""},
		{"shape(from 0 0, arc to 1px 1px)", ""},
	}
	for _, tt := range tests {
		c := styleOf(t, "clip-path: "+tt.value).ClipPath
		var ops string
		if c.Shape != nil {
			for _, cmd := range c.Shape.Commands {
				ops += string(cmd.Op)
			}
		}
		if ops != tt.ops {
			t.Errorf("%s = %q, want %q", tt.value, ops, tt.ops)
		}
	}
}

// TestShapeFunction_OneArcRadius locks CSS Shapes 1, 3.1.1: an arc of one
// radius uses it for both and marks it for the box's diagonal; an arc of
// two takes the width and the height.
func TestShapeFunction_OneArcRadius(t *testing.T) {
	t.Parallel()
	for v, want := range map[string]bool{"of 10%": true, "of 10% 20%": false} {
		c := styleOf(t, "clip-path: shape(from 0 0, arc to 10px 0 "+v+")").ClipPath
		if c.Shape == nil || len(c.Shape.Commands) != 2 || c.Shape.Commands[1].OneRadius != want {
			t.Errorf("arc %s: %+v, want OneRadius %v", v, c.Shape, want)
		}
	}
}
