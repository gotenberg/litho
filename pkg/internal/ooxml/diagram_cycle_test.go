package ooxml

import (
	"fmt"
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
		{
			// Three squares of the box's width at sibSp -0.35: the chord
			// rule's 0.375 widths would draw each over the next, so they
			// stand 2/3 of a width out, where neighbours' boxes touch. The
			// block, 2 widths tall, fills the box: a node is 1599 wide, its
			// neighbours 924 to either side and a width down.
			name: "neighbours never overlap",
			layout: `<dgm:alg type="cycle"/><dgm:shape/><dgm:constrLst>` +
				`<dgm:constr type="w" for="ch" ptType="node" refType="w"/>` +
				`<dgm:constr type="sibSp" refType="w" refFor="ch" refPtType="node" fact="-0.35"/></dgm:constrLst>` +
				nodeList("rect", circle),
			texts: []string{"a", "b", "c"},
			w:     4800, h: 3200,
			want: []string{"background rect 0,0 4800x3200", "node rect 1600,0 1599x1599", "node rect 2523,1599 1599x1599", "node rect 676,1600 1599x1599"},
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

// TestDiagramCycleTurned states that nodes turned along the path stand
// where their boxes, upright, just touch, whatever sibSp, and turn by their
// angle on the circle: four square nodes one width from the centre, the
// block three widths across.
func TestDiagramCycleTurned(t *testing.T) {
	layout := `<dgm:alg type="cycle"><dgm:param type="rotPath" val="alongPath"/></dgm:alg><dgm:shape/><dgm:constrLst>` +
		`<dgm:constr type="w" for="ch" ptType="node" refType="w"/>` +
		`<dgm:constr type="sibSp" refType="w" refFor="ch" refPtType="node" fact="0.5"/></dgm:constrLst>` +
		nodeList("rect", `<dgm:constrLst><dgm:constr type="h" refType="w"/></dgm:constrLst>`)
	d := ParseDiagram(mustParse(t, diagramData("a", "b", "c", "d")), mustParse(t, diagramLayout(layout)), nil, nil)
	root := d.LayOut(3000, 3000, nil)
	want := []string{"background rect 0,0 3000x3000", "node rect 1000,0 1000x1000", "node rect 2000,1000 1000x1000", "node rect 1000,2000 1000x1000", "node rect 0,1000 1000x1000"}
	if got := boxes(root); strings.Join(got, "; ") != strings.Join(want, "; ") {
		t.Errorf("boxes\n got %q\nwant %q", got, want)
	}
	var rots []int64
	for _, k := range root.Kids[1:] {
		rots = append(rots, k.Rot)
	}
	if want := []int64{0, 90 * 60000, 180 * 60000, 270 * 60000}; fmt.Sprint(rots) != fmt.Sprint(want) {
		t.Errorf("turns %v, want %v", rots, want)
	}
}

// TestDiagramCompositeRatioPlacesAnUnplacedKid states that a composite's
// kid no constraint places takes the composite's box as its aspect ratio
// narrows it (21.4.7.49 ar): a square in the middle of a wide box.
func TestDiagramCompositeRatioPlacesAnUnplacedKid(t *testing.T) {
	layout := `<dgm:alg type="composite"><dgm:param type="ar" val="1"/></dgm:alg><dgm:shape/>` + nodeList("ellipse", "")
	d := ParseDiagram(mustParse(t, diagramData("a")), mustParse(t, diagramLayout(layout)), nil, nil)
	got := boxes(d.LayOut(3000, 1000, nil))
	want := []string{"background rect 0,0 3000x1000", "node ellipse 1000,0 1000x1000"}
	if strings.Join(got, "; ") != strings.Join(want, "; ") {
		t.Errorf("boxes\n got %q\nwant %q", got, want)
	}
}
