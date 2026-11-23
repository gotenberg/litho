package ooxml

import (
	"strings"
	"testing"
)

// TestDiagramShapeAdjustValues states that a layout shape's adjust values
// reach its preset (21.4.3.1 adj): an angle in degrees as 60000ths of a
// degree, a share of the size as 100000ths, an index the preset lacks
// left out.
func TestDiagramShapeAdjustValues(t *testing.T) {
	cases := []struct {
		preset, adj string
		want        []string
	}{
		{"pie", `<dgm:adj idx="1" val="270"/><dgm:adj idx="2" val="342"/>`, []string{"adj1=val 16200000", "adj2=val 20520000"}},
		{"roundRect", `<dgm:adj idx="1" val="0.1"/><dgm:adj idx="2" val="0.5"/>`, []string{"adj=val 10000"}},
	}
	for _, c := range cases {
		t.Run(c.preset, func(t *testing.T) {
			layout := `<dgm:alg type="lin"/><dgm:shape/><dgm:forEach name="each" axis="ch" ptType="node"><dgm:layoutNode name="node"><dgm:alg type="tx"/>` +
				`<dgm:shape type="` + c.preset + `"><dgm:adjLst>` + c.adj + `</dgm:adjLst></dgm:shape></dgm:layoutNode></dgm:forEach>`
			d := ParseDiagram(mustParse(t, diagramData("a")), mustParse(t, diagramLayout(layout)), nil, nil)
			node := d.LayOut(1000, 1000, nil).Kids[1]
			av := node.Element(NSDgm, node.Preset(), true).Find(NSA, "avLst")
			var got []string
			for _, g := range av.Kids() {
				got = append(got, attrOr(g, "name")+"="+attrOr(g, "fmla"))
			}
			if strings.Join(got, "; ") != strings.Join(c.want, "; ") {
				t.Errorf("avLst %q, want %q", got, c.want)
			}
		})
	}
}
