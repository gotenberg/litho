package ooxml

import (
	"strings"
	"testing"
)

// blockList is the Basic Block List's snake (default): a node of the
// snake's width, 0.6 as tall as wide, a transition and a spacing a tenth
// of the node's width, with the given algorithm parameters.
func blockList(params string) string {
	return `<dgm:alg type="snake">` + params + `</dgm:alg><dgm:shape/><dgm:constrLst>` +
		`<dgm:constr type="w" for="ch" forName="node" refType="w"/>` +
		`<dgm:constr type="h" for="ch" forName="node" refType="w" refFor="ch" refForName="node" fact="0.6"/>` +
		`<dgm:constr type="w" for="ch" forName="sibTrans" refType="w" refFor="ch" refForName="node" fact="0.1"/>` +
		`<dgm:constr type="sp" refType="w" refFor="ch" refForName="sibTrans"/></dgm:constrLst>` +
		`<dgm:forEach name="each" axis="ch" ptType="node"><dgm:layoutNode name="node"><dgm:alg type="tx"/><dgm:shape type="rect"/></dgm:layoutNode>` +
		`<dgm:forEach axis="followSib" ptType="sibTrans" cnt="1"><dgm:layoutNode name="sibTrans"><dgm:alg type="sp"/><dgm:shape/></dgm:layoutNode></dgm:forEach></dgm:forEach>`
}

// TestDiagramSnake states the snake rule (ECMA-376 Part 1, 21.4.2.1 alg
// snake): the nodes, the transitions between them and the spacing between
// lines scale together to the largest size that fits, a line holding as
// many nodes as make them largest; the block sits in the middle of the
// box, a short last line in the middle of the block; grDir, flowDir,
// contDir and bkpt turn and break the lines.
func TestDiagramSnake(t *testing.T) {
	cases := []struct {
		name   string
		texts  []string
		params string
		w, h   int64
		want   []string
	}{
		{
			// Two a row: 1000 / 2.1 = 476 wide, 285 tall, a gap of 47; the
			// block, 618 tall, sits 191 down. One a row (581 / 1.3 ... 4 rows)
			// or four (1000 / 4.3) give smaller nodes.
			name:  "four nodes two a row in the middle of the box",
			texts: []string{"a", "b", "c", "d"},
			w:     1000, h: 1000,
			want: []string{"background rect 0,0 1000x1000", "node rect 0,190 476x285", "node rect 523,190 476x285", "node rect 0,523 476x285", "node rect 523,523 476x285"},
		},
		{
			name:   "a short last row sits in the middle under off ctr",
			texts:  []string{"a", "b", "c"},
			params: `<dgm:param type="off" val="ctr"/>`,
			w:      2100, h: 1300,
			want: []string{"background rect 0,0 2100x1300", "node rect 0,0 1000x600", "node rect 1100,0 1000x600", "node rect 550,700 1000x600"},
		},
		{
			name:  "a wide box takes one row, centred across",
			texts: []string{"a", "b"},
			w:     2100, h: 1000,
			want: []string{"background rect 0,0 2100x1000", "node rect 0,200 1000x600", "node rect 1100,200 1000x600"},
		},
		{
			name:   "grDir tR starts at the right",
			texts:  []string{"a", "b", "c"},
			params: `<dgm:param type="grDir" val="tR"/><dgm:param type="off" val="ctr"/>`,
			w:      2100, h: 1300,
			want: []string{"background rect 0,0 2100x1300", "node rect 1100,0 1000x600", "node rect 0,0 1000x600", "node rect 550,700 1000x600"},
		},
		{
			name:  "a short last row starts the row with no off",
			texts: []string{"a", "b", "c"},
			w:     2100, h: 1300,
			want: []string{"background rect 0,0 2100x1300", "node rect 0,0 1000x600", "node rect 1100,0 1000x600", "node rect 0,700 1000x600"},
		},
		{
			name:   "contDir revDir runs the second row back",
			texts:  []string{"a", "b", "c", "d"},
			params: `<dgm:param type="contDir" val="revDir"/>`,
			w:      2100, h: 1300,
			want: []string{"background rect 0,0 2100x1300", "node rect 0,0 1000x600", "node rect 1100,0 1000x600", "node rect 1100,700 1000x600", "node rect 0,700 1000x600"},
		},
		{
			name:   "flowDir col fills a column first, the nodes of a column touching",
			texts:  []string{"a", "b", "c", "d"},
			params: `<dgm:param type="flowDir" val="col"/>`,
			w:      2100, h: 1300,
			want: []string{"background rect 0,0 2100x1300", "node rect 0,50 1000x600", "node rect 0,650 1000x600", "node rect 1100,50 1000x600", "node rect 1100,650 1000x600"},
		},
		{
			name:   "bkpt fixed takes bkPtFixedVal a row",
			texts:  []string{"a", "b", "c"},
			params: `<dgm:param type="bkpt" val="fixed"/><dgm:param type="bkPtFixedVal" val="1"/>`,
			w:      2000, h: 2000,
			want: []string{"background rect 0,0 2000x2000", "node rect 500,0 1000x600", "node rect 500,700 1000x600", "node rect 500,1400 1000x600"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := ParseDiagram(mustParse(t, diagramData(c.texts...)), mustParse(t, diagramLayout(blockList(c.params))), nil, nil)
			got := boxes(d.LayOut(c.w, c.h, nil))
			if strings.Join(got, "; ") != strings.Join(c.want, "; ") {
				t.Errorf("boxes\n got %q\nwant %q", got, c.want)
			}
		})
	}
}

// TestDiagramSnakeSpacing states where a snake's lengths come from: a node
// whose own layout node sets its height from its width (a composite, as
// bList2's), the transitions a row apart by their point type's width and
// the rows by sp, each a factor of the node's width.
func TestDiagramSnakeSpacing(t *testing.T) {
	layout := `<dgm:alg type="snake"/><dgm:shape/><dgm:constrLst>` +
		`<dgm:constr type="w" for="ch" forName="node" refType="w"/>` +
		`<dgm:constr type="w" for="ch" ptType="sibTrans" refType="w" refFor="ch" refForName="node" fact="0.08"/>` +
		`<dgm:constr type="sp" refType="w" refFor="ch" refForName="node" fact="0.16"/></dgm:constrLst>` +
		`<dgm:forEach name="each" axis="ch" ptType="node"><dgm:layoutNode name="node"><dgm:alg type="composite"/><dgm:shape type="rect"/>` +
		`<dgm:constrLst><dgm:constr type="w" val="1"/><dgm:constr type="h" refType="w" fact="1.06"/></dgm:constrLst></dgm:layoutNode>` +
		`<dgm:forEach axis="followSib" ptType="sibTrans" cnt="1"><dgm:layoutNode name="sibTrans"><dgm:alg type="sp"/><dgm:shape type="rect" hideGeom="1"/></dgm:layoutNode></dgm:forEach></dgm:forEach>`
	d := ParseDiagram(mustParse(t, diagramData("a", "b", "c", "d", "e", "f")), mustParse(t, diagramLayout(layout)), nil, nil)
	// Three a row, two rows: 2.28 node widths tall fill 2280, so a node is
	// 1000 by 1060, 80 apart across and 160 down; the 3160-wide block sits
	// 420 from the left.
	got := boxes(d.LayOut(4000, 2280, nil))
	want := []string{"background rect 0,0 4000x2280",
		"node rect 420,0 1000x1060", "sibTrans  1420,0 80x1060", "node rect 1500,0 1000x1060", "sibTrans  2500,0 80x1060", "node rect 2580,0 1000x1060",
		"sibTrans  3580,0 80x1060", "node rect 420,1220 1000x1060", "sibTrans  1420,1220 80x1060", "node rect 1500,1220 1000x1060",
		"sibTrans  2500,1220 80x1060", "node rect 2580,1220 1000x1060"}
	if strings.Join(got, "; ") != strings.Join(want, "; ") {
		t.Errorf("boxes\n got %q\nwant %q", got, want)
	}
}

// TestDiagramSnakeSizesFromTheStart lays out a snake whose node height is a
// factor of its font size: the node is sized from the size the font starts
// at, whatever size the text settles at.
func TestDiagramSnakeSizesFromTheStart(t *testing.T) {
	layout := `<dgm:alg type="snake"/><dgm:shape/><dgm:constrLst>` +
		`<dgm:constr type="primFontSz" for="ch" forName="node" val="40"/>` +
		`<dgm:constr type="w" for="ch" forName="node" refType="w"/>` +
		`<dgm:constr type="h" for="ch" forName="node" refType="primFontSz" refFor="ch" refForName="node" fact="1.5"/></dgm:constrLst>` +
		`<dgm:forEach name="each" axis="ch" ptType="node"><dgm:layoutNode name="node"><dgm:alg type="tx"/><dgm:shape type="rect"/>` + noMargins +
		`<dgm:ruleLst><dgm:rule type="primFontSz" val="5"/></dgm:ruleLst></dgm:layoutNode></dgm:forEach>`
	d := ParseDiagram(mustParse(t, diagramData("abcdefghijkl")), mustParse(t, diagramLayout(layout)), nil, nil)
	// 60 mm tall at 40 points; twelve characters at half an em fit the 100
	// point width at 16 points, which leaves the node as it is.
	k := d.LayOut(100*12700, 3000000, charFit).Kids[1]
	if k.FontSize != 16 || k.H != 2160000 {
		t.Errorf("node %d EMU tall at %v points, want 2160000 at 16", k.H, k.FontSize)
	}
}
