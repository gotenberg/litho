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

// TestDiagramLinearOwnRatio states that a linear kid's own constraint sets
// its height from its width once the layout has sized it (21.4.2.6 constr
// for self), the kid sitting in the middle across.
func TestDiagramLinearOwnRatio(t *testing.T) {
	layout := `<dgm:alg type="lin"/><dgm:shape/>` + nodeList("chevron", `<dgm:constrLst><dgm:constr type="h" refType="w" op="equ" fact="0.4"/></dgm:constrLst>`)
	d := ParseDiagram(mustParse(t, diagramData("a", "b")), mustParse(t, diagramLayout(layout)), nil, nil)
	got := boxes(d.LayOut(2000, 1000, nil))
	want := []string{"background rect 0,0 2000x1000", "node chevron 0,300 1000x400", "node chevron 1000,300 1000x400"}
	if strings.Join(got, "; ") != strings.Join(want, "; ") {
		t.Errorf("boxes\n got %q\nwant %q", got, want)
	}
}

// TestDiagramLinearSizesByPointType states that a linear layout's width
// constraint for its children by point type alone sizes each kid of that
// type (21.4.2.6 constr ptType): a transition a quarter of a node wide,
// 2.25 nodes filling the box (the transition truncated to 249).
func TestDiagramLinearSizesByPointType(t *testing.T) {
	layout := `<dgm:alg type="lin"/><dgm:shape/><dgm:constrLst>` +
		`<dgm:constr type="w" for="ch" forName="node" refType="w"/>` +
		`<dgm:constr type="w" for="ch" ptType="sibTrans" refType="w" refFor="ch" refForName="node" fact="0.25"/></dgm:constrLst>` +
		`<dgm:forEach name="each" axis="ch" ptType="node"><dgm:layoutNode name="node"><dgm:alg type="tx"/><dgm:shape type="rect"/></dgm:layoutNode>` +
		`<dgm:forEach axis="followSib" ptType="sibTrans" cnt="1"><dgm:layoutNode name="arrow"><dgm:alg type="sp"/><dgm:shape type="rightArrow"/></dgm:layoutNode></dgm:forEach></dgm:forEach>`
	data := strings.ReplaceAll(diagramData("a", "b"), `presName="sibTrans"`, `presName="arrow"`)
	d := ParseDiagram(mustParse(t, data), mustParse(t, diagramLayout(layout)), nil, nil)
	got := boxes(d.LayOut(2250, 1000, nil))
	want := []string{"background rect 0,0 2250x1000", "node rect 0,0 1000x1000", "arrow rightArrow 1000,0 249x1000", "node rect 1249,0 1000x1000"}
	if strings.Join(got, "; ") != strings.Join(want, "; ") {
		t.Errorf("boxes\n got %q\nwant %q", got, want)
	}
}

// TestDiagramLinearNodesByPointType states venn3's row: nodes as wide as
// the layout and as tall as wide by their point type, spaces -0.2 of a
// node, scaled to the box: five circles of 1000 / 4.2 = 238, 191 apart
// (the space truncated to -47), in the middle across.
func TestDiagramLinearNodesByPointType(t *testing.T) {
	layout := `<dgm:alg type="lin"/><dgm:shape/><dgm:constrLst>` +
		`<dgm:constr type="w" for="ch" ptType="node" refType="w"/>` +
		`<dgm:constr type="h" for="ch" ptType="node" refType="w" refFor="ch" refPtType="node"/>` +
		`<dgm:constr type="w" for="ch" forName="space" refType="w" refFor="ch" refPtType="node" fact="-0.2"/></dgm:constrLst>` +
		`<dgm:forEach name="each" axis="ch" ptType="node"><dgm:layoutNode name="node"><dgm:alg type="tx"/><dgm:shape type="ellipse"/></dgm:layoutNode>` +
		`<dgm:forEach axis="followSib" ptType="sibTrans" cnt="1"><dgm:layoutNode name="space"><dgm:alg type="sp"/><dgm:shape/></dgm:layoutNode></dgm:forEach></dgm:forEach>`
	data := strings.ReplaceAll(diagramData("a", "b", "c", "d", "e"), `presName="sibTrans"`, `presName="space"`)
	d := ParseDiagram(mustParse(t, data), mustParse(t, diagramLayout(layout)), nil, nil)
	got := boxes(d.LayOut(1000, 600, nil))
	want := []string{"background rect 0,0 1000x600",
		"node ellipse 0,181 238x238", "node ellipse 191,181 238x238", "node ellipse 382,181 238x238", "node ellipse 573,181 238x238", "node ellipse 764,181 238x238"}
	if strings.Join(got, "; ") != strings.Join(want, "; ") {
		t.Errorf("boxes\n got %q\nwant %q", got, want)
	}
}
