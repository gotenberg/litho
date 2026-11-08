package sheet

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
)

// TestConditional_IconSet locks which icon a cell of an icon set shows:
// the highest threshold its number reaches, or passes when the threshold
// asks, the first icon below them all, in reverse when asked.
func TestConditional_IconSet(t *testing.T) {
	set := iconSets["3Arrows"]
	tests := []struct {
		name     string
		above    []bool
		reversed bool
		custom   []string
		want     []icon
	}{
		{"reached", nil, false, nil, []icon{set[0], set[1], set[1], set[2], set[2]}},
		{"passed", []bool{false, true, true}, false, nil, []icon{set[0], set[0], set[1], set[1], set[2]}},
		{"reversed", nil, true, nil, []icon{set[2], set[1], set[1], set[0], set[0]}},
		{"custom", nil, false, []string{"3Flags:2", "", "NoIcons:0"}, []icon{iconSets["3Flags"][2], set[1], set[1], nil, nil}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var cells []Cell
			for _, v := range []float64{1, 2, 3, 4, 5} {
				cells = append(cells, numCell(0, v, nil))
			}
			sh := column(cells...)
			sh.Conditional = []Conditional{{Ranges: []Range{{LastRow: 9}}, Rules: []Rule{{
				Kind: RuleIconSet, Icons: "3Arrows", Values: []Value{num(1), num(2), num(4)},
				IconsAbove: tc.above, IconsReversed: tc.reversed, IconsCustom: tc.custom,
			}}}}
			g := newGrid(newDoc(sh), sh)
			for i := range cells {
				got, _ := g.iconAt(i, 0, g.cell(i, 0))
				if (got == nil) != (tc.want[i] == nil) || (got != nil && fragmentsOf(got) != fragmentsOf(tc.want[i])) {
					t.Errorf("A%d shows another icon", i+1)
				}
			}
		})
	}
}

// fragmentsOf returns a fingerprint of the drawing of ic.
func fragmentsOf(ic icon) string {
	out := ""
	for _, f := range ic() {
		switch f := f.(type) {
		case *compose.Path:
			if f.Fill != nil {
				out += colorKey(f.Fill.Color)
			}
		case *compose.Group:
			for _, k := range f.Content {
				if p, ok := k.(*compose.Path); ok && p.Fill != nil {
					out += colorKey(p.Fill.Color)
				}
			}
		}
	}
	return out
}

// colorKey returns a colour as text.
func colorKey(c compose.Color) string {
	return string(rune('a'+int(c.R*20))) + string(rune('a'+int(c.G*20))) + string(rune('a'+int(c.B*20)))
}

// TestConvert_IconSetDrawing locks where a page draws an icon: at its
// cell's bottom-left corner, 72/254 of the font's size square, and the
// value left out when the rule shows the icon alone.
//
// # Corpus evidence
//
//   - litho/pdf: a flat ODS of 3Arrows icon sets in 10 and 24 point cells,
//     converted by Gotenberg 9.0.0, prints 2.835 and 6.803 point icons
//     0.227 points right of and 0.255 points above the cells' bottom-left
//     corners, whatever their vertical alignment.
func TestConvert_IconSetDrawing(t *testing.T) {
	sh := column(numCell(0, 1, nil), numCell(0, 3, nil))
	sh.Conditional = []Conditional{{Ranges: []Range{{LastRow: 1}}, Rules: []Rule{{
		Kind: RuleIconSet, Icons: "3Arrows", Values: []Value{num(1), num(2), num(3)}, IconsOnly: true,
	}}}}
	out := convert(t, newDoc(sh), office.Options{})
	var groups []*compose.Group
	var walk func([]compose.Fragment)
	walk = func(fs []compose.Fragment) {
		for _, f := range fs {
			if g, ok := f.(*compose.Group); ok {
				if near(g.Transform.A, 10.0*72/254/16, 1e-9) {
					groups = append(groups, g)
				}
				walk(g.Content)
			}
		}
	}
	walk(out.Pages[0].Content)
	if len(groups) != 2 {
		t.Fatalf("%d icons drawn, want 2", len(groups))
	}
	if g := groups[0]; !near(g.Transform.E, iconInsetX, 1e-6) || !near(g.Transform.F, 12.78-iconInsetY-10.0*72/254, 1e-6) {
		t.Errorf("first icon at (%v, %v)", g.Transform.E, g.Transform.F)
	}
	if rs := runs(out.Pages[0]); len(rs) != 0 {
		t.Errorf("values printed beside the icons alone: %v", rs)
	}
}
