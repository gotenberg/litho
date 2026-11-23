package ooxml

import (
	"strings"
	"testing"
)

// TestDiagramLinearKeepsStatedSpacers states that a spacer whose length
// the linear layout's constraints state keeps it, a negative one
// overlapping the kids it parts, and takes no share of the layout
// (21.4.2.6 constr refFor ch, refForName): two nodes of the box's width
// and a gap of -0.1 of a node fill 1.9 node widths.
func TestDiagramLinearKeepsStatedSpacers(t *testing.T) {
	layout := `<dgm:alg type="lin"/><dgm:shape/><dgm:constrLst>` +
		`<dgm:constr type="w" for="ch" forName="node" refType="w"/>` +
		`<dgm:constr type="w" for="ch" forName="gap" refType="w" refFor="ch" refForName="node" fact="-0.1"/></dgm:constrLst>` +
		`<dgm:forEach name="each" axis="ch" ptType="node"><dgm:layoutNode name="node"><dgm:alg type="tx"/><dgm:shape type="rect"/></dgm:layoutNode>` +
		`<dgm:forEach axis="followSib" ptType="sibTrans" cnt="1"><dgm:layoutNode name="gap"><dgm:alg type="sp"/><dgm:shape/></dgm:layoutNode></dgm:forEach></dgm:forEach>`
	data := strings.ReplaceAll(diagramData("a", "b"), `presName="sibTrans"`, `presName="gap"`)
	d := ParseDiagram(mustParse(t, data), mustParse(t, diagramLayout(layout)), nil, nil)
	got := boxes(d.LayOut(1900, 1000, nil))
	want := []string{"background rect 0,0 1900x1000", "node rect 0,0 1000x1000", "node rect 900,0 1000x1000"}
	if strings.Join(got, "; ") != strings.Join(want, "; ") {
		t.Errorf("boxes\n got %q\nwant %q", got, want)
	}
}
