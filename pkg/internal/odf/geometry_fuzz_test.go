package odf

import (
	"encoding/xml"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/shape"
)

func FuzzParseGeometry(f *testing.F) {
	f.Add(`M ?f0 0 L 21600 ?f1 C 1 2 3 4 5 6 Z N U 10800 10800 10800 10800 0 360 B 0 0 20 20 20 10 0 10 X 1 1 G 1 1 90 90 H`, `if(?f1,sin(pi/4)*$0,atan2(1,2))`, "100 50")
	f.Add(`M 0`, `(((`, "")
	f.Fuzz(func(t *testing.T, path, formula, mods string) {
		a := func(local, v string) xml.Attr { return xml.Attr{Name: xml.Name{Space: NSDraw, Local: local}, Value: v} }
		eq := func(name, formula string) *Element {
			return &Element{Name: xml.Name{Space: NSDraw, Local: "equation"}, Attr: []xml.Attr{a("name", name), a("formula", formula)}}
		}
		el := &Element{
			Name:     xml.Name{Space: NSDraw, Local: "enhanced-geometry"},
			Attr:     []xml.Attr{a("enhanced-path", path), a("modifiers", mods), a("text-areas", path)},
			Children: []*Element{eq("f0", formula), eq("f1", "?f0+"+formula)},
		}
		g := ParseGeometry(el)
		_ = shape.Check(g)
		r := compose.Rect{Width: 100, Height: 50}
		for _, p := range shape.Paths(g, r) {
			if len(p.Ops) > 0 && p.Ops[0].Kind != compose.MoveTo {
				t.Fatal("a path does not start with a move")
			}
		}
		_ = shape.TextRect(g, r)
	})
}
