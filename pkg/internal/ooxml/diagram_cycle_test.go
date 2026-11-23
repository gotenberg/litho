package ooxml

import (
	"strings"
	"testing"
)

// TestDiagramCycle states Office's cycle (ECMA-376 Part 1, 21.4.2.1 alg
// cycle): nodes of the size the constraints set, neighbours their width
// plus sibSp apart along the chord, a centre node's ring at the centre's
// half width, sp and the node's half width when that is farther, the block
// scaled to the box and centred.
func TestDiagramCycle(t *testing.T) {
	circle := `<dgm:constrLst><dgm:constr type="h" refType="w"/></dgm:constrLst>`
	cases := []struct {
		name   string
		layout string
		texts  []string
		w, h   int64
		want   []string
	}{
		{
			// Four nodes of the box's width, 1.5 widths apart centre to
			// centre: a radius of 1.0607 widths, the block 3.1213 widths
			// across, so a node is 1000 wide in a 3122-tall box, the
			// 3121-wide block 1439 from the left of the 6000-wide box.
			name: "neighbours a width and sibSp apart, the block scaled to the box",
			layout: `<dgm:alg type="cycle"/><dgm:shape/><dgm:constrLst>` +
				`<dgm:constr type="w" for="ch" ptType="node" refType="w"/>` +
				`<dgm:constr type="sibSp" refType="w" refFor="ch" refPtType="node" fact="0.5"/></dgm:constrLst>` +
				nodeList("ellipse", circle),
			texts: []string{"a", "b", "c", "d"},
			w:     6000, h: 3122,
			want: []string{"background rect 0,0 6000x3122", "node ellipse 2499,0 1000x1000", "node ellipse 3560,1060 1000x1000", "node ellipse 2499,2121 1000x1000", "node ellipse 1439,1060 1000x1000"},
		},
		{
			// Four nodes of one width w, sp and sibSp -0.2 w: the ring stands
			// w - 0.2 w = 0.8 w from the centre node, farther than the chord
			// rule's 0.8 w / (2 sin 60 degrees) = 0.46 w. The block, 2.39 w
			// tall, fills the box: w is 1676.
			name: "a centre node keeps its ring at its half width and sp",
			layout: `<dgm:alg type="cycle"><dgm:param type="ctrShpMap" val="fNode"/></dgm:alg><dgm:shape/><dgm:constrLst>` +
				`<dgm:constr type="w" for="ch" forName="node" refType="w" fact="0.5"/>` +
				`<dgm:constr type="sp" refType="w" refFor="ch" refForName="node" fact="-0.2"/>` +
				`<dgm:constr type="sibSp" refType="w" refFor="ch" refForName="node" fact="-0.2"/></dgm:constrLst>` +
				nodeList("ellipse", circle),
			texts: []string{"ctr", "a", "b", "c"},
			w:     4000, h: 4000,
			want: []string{"background rect 0,0 4000x4000", "node ellipse 1161,1496 1676x1676", "node ellipse 1161,155 1676x1676", "node ellipse 2323,2167 1676x1676", "node ellipse 0,2167 1676x1676"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := ParseDiagram(mustParse(t, diagramData(c.texts...)), mustParse(t, diagramLayout(c.layout)), nil, nil)
			got := boxes(d.LayOut(c.w, c.h, nil))
			if strings.Join(got, "; ") != strings.Join(c.want, "; ") {
				t.Errorf("boxes\n got %q\nwant %q", got, c.want)
			}
		})
	}
}
