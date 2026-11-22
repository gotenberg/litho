package ooxml

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

// hierNode is a data point of a test hierarchy: its text, its parent's
// index (-1 for the document) and whether it is an assistant.
type hierNode struct {
	text   string
	parent int
	asst   bool
}

// hierData returns the data model of a hierarchy with the presentation
// points hierLayout lays out: per point a hierRoot (asstRoot for an
// assistant), its comp showing the text, its kids container and its
// assts container; with links, a conn point for each parent transition.
func hierData(nodes []hierNode, links bool) string {
	var pts, cxns strings.Builder
	pts.WriteString(`<dgm:pt modelId="0" type="doc"/>`)
	pts.WriteString(`<dgm:pt modelId="p0" type="pres"><dgm:prSet presAssocID="0" presName="root"/><dgm:spPr/></dgm:pt>`)
	cxns.WriteString(`<dgm:cxn modelId="c0" type="presOf" srcId="0" destId="p0"/>`)
	for i, n := range nodes {
		typ, root := "", "hierRoot"
		if n.asst {
			typ, root = ` type="asst"`, "asstRoot"
		}
		fmt.Fprintf(&pts, `<dgm:pt modelId="n%d"%s><dgm:prSet/><dgm:spPr/><dgm:t><a:bodyPr/><a:p><a:r><a:t>%s</a:t></a:r></a:p></dgm:t></dgm:pt>`, i, typ, n.text)
		src, holder := "0", "p0"
		if n.parent >= 0 {
			src = fmt.Sprintf("n%d", n.parent)
			holder = fmt.Sprintf("k%d", n.parent)
			if n.asst {
				holder = fmt.Sprintf("a%d", n.parent)
			}
		}
		fmt.Fprintf(&cxns, `<dgm:cxn modelId="x%d" srcId="%s" destId="n%d" srcOrd="%d" parTransId="t%d"/>`, i, src, i, i, i)
		fmt.Fprintf(&pts, `<dgm:pt modelId="t%d" type="parTrans" cxnId="x%d"/>`, i, i)
		if links {
			fmt.Fprintf(&pts, `<dgm:pt modelId="l%d" type="pres"><dgm:prSet presAssocID="t%d" presName="conn"/><dgm:spPr/></dgm:pt>`, i, i)
			fmt.Fprintf(&cxns, `<dgm:cxn modelId="lo%d" type="presOf" srcId="t%d" destId="l%d"/><dgm:cxn modelId="lr%d" type="presParOf" srcId="%s" destId="l%d" srcOrd="%d"/>`, i, i, i, i, holder, i, i)
		}
		for _, p := range []struct{ id, name string }{{"h", root}, {"c", "comp"}, {"k", "kids"}, {"a", "assts"}} {
			fmt.Fprintf(&pts, `<dgm:pt modelId="%s%d" type="pres"><dgm:prSet presAssocID="n%d" presName="%s"/><dgm:spPr/></dgm:pt>`, p.id, i, i, p.name)
		}
		fmt.Fprintf(&cxns, `<dgm:cxn modelId="o%d" type="presOf" srcId="n%d" destId="c%d"/>`, i, i, i)
		fmt.Fprintf(&cxns, `<dgm:cxn modelId="r%d" type="presParOf" srcId="%s" destId="h%d" srcOrd="%d"/>`, i, holder, i, i)
		for j, p := range []string{"c", "k", "a"} {
			fmt.Fprintf(&cxns, `<dgm:cxn modelId="r%d%s" type="presParOf" srcId="h%d" destId="%s%d" srcOrd="%d"/>`, i, p, i, p, i, j)
		}
	}
	return `<dgm:dataModel ` + dgmNS + `><dgm:ptLst>` + pts.String() + `</dgm:ptLst><dgm:cxnLst>` + cxns.String() + `</dgm:cxnLst></dgm:dataModel>`
}

// hierLayout returns an organisation chart in the manner of orgChart1:
// every length on the root, a box ten times the frame wide and half as
// tall, rows and siblings 0.2 of the box apart; the children of a root
// under rootAlg's root, in a container of kidsAlg.
func hierLayout(rootAlg, kidsAlg, rootCons string) string {
	comp := `<dgm:layoutNode name="comp"><dgm:alg type="tx"/><dgm:shape type="rect"/><dgm:presOf axis="self"/></dgm:layoutNode>`
	assts := `<dgm:layoutNode name="assts"><dgm:alg type="hierChild"><dgm:param type="linDir" val="fromL"/><dgm:param type="secLinDir" val="fromT"/></dgm:alg><dgm:shape/>` +
		`<dgm:forEach name="asstEach" axis="ch" ptType="asst"><dgm:layoutNode name="asstRoot"><dgm:alg type="hierRoot"/><dgm:shape/>` + comp + `</dgm:layoutNode></dgm:forEach></dgm:layoutNode>`
	return diagramLayout(`<dgm:alg type="hierChild"/><dgm:shape/><dgm:constrLst>` +
		`<dgm:constr type="w" for="des" forName="comp" refType="w" fact="10"/>` +
		`<dgm:constr type="h" for="des" forName="comp" refType="w" refFor="des" refForName="comp" fact="0.5"/>` +
		`<dgm:constr type="sp" for="des" forName="hierRoot" refType="w" refFor="des" refForName="comp" fact="0.2"/>` +
		`<dgm:constr type="sp" for="des" forName="asstRoot" refType="sp" refFor="des" refForName="hierRoot"/>` +
		`<dgm:constr type="sibSp" refType="w" refFor="des" refForName="comp" fact="0.2"/></dgm:constrLst>` +
		`<dgm:forEach name="roots" axis="ch" ptType="node"><dgm:layoutNode name="hierRoot">` + rootAlg + `<dgm:shape/><dgm:constrLst>` + rootCons + `</dgm:constrLst>` + comp +
		`<dgm:layoutNode name="kids">` + kidsAlg + `<dgm:shape/><dgm:forEach name="links" axis="ch" ptType="node"><dgm:layoutNode name="conn"><dgm:alg type="conn"><dgm:param type="connRout" val="bend"/></dgm:alg>` +
		`<dgm:shape type="conn" zOrderOff="-99999"/></dgm:layoutNode></dgm:forEach><dgm:forEach ref="roots"/></dgm:layoutNode>` + assts +
		`</dgm:layoutNode></dgm:forEach>`)
}

// compBoxes lists the boxes of the comp shapes under s, in the diagram's
// coordinates, as "text x,y wxh".
func compBoxes(s *DiagramShape, x, y int64, out *[]string) {
	x, y = x+s.X, y+s.Y
	if s.Name == "comp" {
		text := ""
		for _, p := range s.Paragraphs {
			for _, r := range p.Source.childrenOf(NSA, "r") {
				text += r.Child(NSA, "t").Text
			}
		}
		*out = append(*out, fmt.Sprintf("%s %d,%d %dx%d", text, x, y, s.W, s.H))
	}
	for _, k := range s.Kids {
		compBoxes(k, x, y, out)
	}
}

// TestDiagramHierarchy states Office's organisation chart layout
// (21.4.2.1 hierRoot and hierChild): the boxes at the sizes the
// constraints give, sp between rows and sibSp between siblings, a row of
// children packed against the outline of the subtrees before it, the root
// centred between its first and last child (hierAlign tCtrCh) or its
// children alignOff of its width from its left (tL, a column of chAlign
// l), assistants either side of the line down the root half sibSp from
// it, the whole tree scaled to the frame and centred in it.
func TestDiagramHierarchy(t *testing.T) {
	std := `<dgm:alg type="hierRoot"/>`
	row := `<dgm:alg type="hierChild"/>`
	cases := []struct {
		name             string
		nodes            []hierNode
		rootAlg, kidsAlg string
		rootCons         string
		w, h             int64
		want             []string
	}{
		{
			// At the constraints' sizes (box 144000 by 72000, gaps 28800):
			// E1 and E2 side by side, Manager centred over them at 86400,
			// the assistant's right edge half a gap left of Manager's
			// centre, Manager2 a gap right of Manager though E2 reaches
			// under it. The tree, 403200 by 273600, fits the frame's height
			// at a thirtieth and stands 480 in from either side.
			name: "a manager, an assistant, two employees and a second manager",
			nodes: []hierNode{
				{"Manager", -1, false}, {"E1", 0, false}, {"E2", 0, false}, {"A", 0, true}, {"Manager2", -1, false},
			},
			rootAlg: std, kidsAlg: row,
			w: 14400, h: 9120,
			want: []string{"Manager 3360,0 4800x2400", "E1 480,6720 4800x2400", "E2 6240,6720 4800x2400", "A 480,3360 4800x2400", "Manager2 9120,0 4800x2400"},
		},
		{
			// A column a quarter of a box right of its root's left edge,
			// rows a gap apart: three rows of 9000 and two gaps of 3600,
			// 34200 tall, 22500 wide, scaled to the frame's width at 0.08
			// and centred down.
			name:     "children in a column under hierAlign tL",
			nodes:    []hierNode{{"M", -1, false}, {"E1", 0, false}, {"E2", 0, false}},
			rootAlg:  `<dgm:alg type="hierRoot"><dgm:param type="hierAlign" val="tL"/></dgm:alg>`,
			kidsAlg:  `<dgm:alg type="hierChild"><dgm:param type="chAlign" val="l"/><dgm:param type="linDir" val="fromT"/></dgm:alg>`,
			rootCons: `<dgm:constr type="alignOff" val="0.25"/>`,
			w:        1800, h: 3000,
			want: []string{"M 0,132 1440x720", "E1 360,1140 1440x720", "E2 360,2148 1440x720"},
		},
		{
			// In boxes of u: M's two assistants either side of its line,
			// 0.1u from it, E1 under them, M over E1. The tree, 2.2u by
			// 1.9u, fits the frame at u = 1000.
			name:    "two assistants either side of the line",
			nodes:   []hierNode{{"M", -1, false}, {"A1", 0, true}, {"A2", 0, true}, {"E1", 0, false}},
			rootAlg: std, kidsAlg: row,
			w: 2200, h: 1900,
			want: []string{"M 600,0 1000x500", "A1 0,700 1000x500", "A2 1200,700 1000x500", "E1 600,1400 1000x500"},
		},
		{
			// E1's assistant B stands left of E1's line, a row under E1.
			name: "an assistant of a child",
			nodes: []hierNode{
				{"M", -1, false}, {"A1", 0, true}, {"A2", 0, true}, {"E1", 0, false}, {"B", 3, true},
			},
			rootAlg: std, kidsAlg: row,
			w: 2200, h: 2600,
			want: []string{"M 600,0 1000x500", "A1 0,700 1000x500", "A2 1200,700 1000x500", "E1 600,1400 1000x500", "B 0,2100 1000x500"},
		},
		{
			// orgChart1 offsets a column 0.65 of a box for a root with
			// assistants (an if of axis ch, ptType asst, cnt gte 1), 0.25
			// otherwise: M's children are no assistants.
			name:  "a column offset by whether the root has assistants",
			nodes: []hierNode{{"M", -1, false}, {"E1", 0, false}, {"E2", 0, false}},
			rootAlg: `<dgm:choose><dgm:if axis="ch" ptType="asst" func="cnt" op="gte" val="1"><dgm:alg type="hierRoot"><dgm:param type="hierAlign" val="tL"/></dgm:alg>` +
				`<dgm:constrLst><dgm:constr type="alignOff" val="0.65"/></dgm:constrLst></dgm:if><dgm:else><dgm:alg type="hierRoot"><dgm:param type="hierAlign" val="tL"/></dgm:alg>` +
				`<dgm:constrLst><dgm:constr type="alignOff" val="0.25"/></dgm:constrLst></dgm:else></dgm:choose>`,
			kidsAlg: `<dgm:alg type="hierChild"><dgm:param type="chAlign" val="l"/><dgm:param type="linDir" val="fromT"/></dgm:alg>`,
			w:       1800, h: 3000,
			want: []string{"M 0,132 1440x720", "E1 360,1140 1440x720", "E2 360,2148 1440x720"},
		},
		{
			// A tree that grows across (hierAlign lCtrCh, its children in
			// a column fromT): the levels sp apart to the right, the
			// siblings sibSp apart down, the root centred between its
			// first and last child; 2.2 by 1.2 boxes in all.
			name:    "a tree that grows across",
			nodes:   []hierNode{{"M", -1, false}, {"E1", 0, false}, {"E2", 0, false}},
			rootAlg: `<dgm:alg type="hierRoot"><dgm:param type="hierAlign" val="lCtrCh"/></dgm:alg>`,
			kidsAlg: `<dgm:alg type="hierChild"><dgm:param type="linDir" val="fromT"/></dgm:alg>`,
			w:       2200, h: 1200,
			want: []string{"M 0,350 1000x500", "E1 1200,0 1000x500", "E2 1200,700 1000x500"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := ParseDiagram(mustParse(t, hierData(tc.nodes, false)), mustParse(t, hierLayout(tc.rootAlg, tc.kidsAlg, tc.rootCons)), nil, nil)
			root := d.LayOut(tc.w, tc.h, nil)
			if root == nil {
				t.Fatal("no layout")
			}
			var got []string
			compBoxes(root, -root.X, -root.Y, &got)
			slices.Sort(got)
			want := slices.Sorted(slices.Values(tc.want))
			if strings.Join(got, "; ") != strings.Join(want, "; ") {
				t.Errorf("boxes\n got %v\nwant %v", got, want)
			}
		})
	}
}

// lines lists the straight lines of the connectors under s, in the
// diagram's coordinates, as "x0,y0-x1,y1".
func lines(s *DiagramShape, x, y int64, out *[]string) {
	x, y = x+s.X, y+s.Y
	if s.Preset() == "line" {
		*out = append(*out, fmt.Sprintf("%d,%d-%d,%d", x, y, x+s.W, y+s.H))
	}
	for _, k := range s.Kids {
		lines(k, x, y, out)
	}
}

// TestDiagramHierarchyConnectors states orgChart1's connectors: to a child
// of a row, down from the root's bottom centre to bendDist above the
// child, across and down to its top centre; to an assistant, down to its
// middle and across to its near side; to a child of a column, alike from
// the centre of the root's kid that holds no text.
func TestDiagramHierarchyConnectors(t *testing.T) {
	nodes := []hierNode{{"Manager", -1, false}, {"E1", 0, false}, {"E2", 0, false}, {"A", 0, true}, {"Manager2", -1, false}}
	bend := `<dgm:constr type="bendDist" for="des" ptType="parTrans" refType="sp" fact="0.5"/>`
	d := ParseDiagram(mustParse(t, hierData(nodes, true)), mustParse(t, hierLayout(`<dgm:alg type="hierRoot"/>`, `<dgm:alg type="hierChild"/>`, bend)), nil, nil)
	root := d.LayOut(14400, 9120, nil)
	if root == nil {
		t.Fatal("no layout")
	}
	var got []string
	lines(root, -root.X, -root.Y, &got)
	slices.Sort(got)
	// Manager's bottom centre is 5760,2400; the children's tops 6720,
	// bendDist 480 above them; the assistant's middle 4560, its right
	// side 5280.
	want := []string{
		"2880,6240-2880,6720", "2880,6240-5760,6240", "5280,4560-5760,4560", "5760,2400-5760,4560",
		"5760,2400-5760,6240", "5760,2400-5760,6240", "5760,6240-8640,6240", "8640,6240-8640,6720",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("lines\n got %v\nwant %v", got, want)
	}
}

// TestDiagramHierarchyOfHostileDefinitions lays out hierarchies whose
// numbers and shapes no chart offers: each ends within its step and depth
// caps with the root at the diagram's box, a sibling row past the steps
// left to the equal shares.
func TestDiagramHierarchyOfHostileDefinitions(t *testing.T) {
	chain := make([]hierNode, 200)
	for i := range chain {
		chain[i] = hierNode{"n", i - 1, false}
	}
	wide := make([]hierNode, 3000)
	wide[0] = hierNode{"M", -1, false}
	for i := 1; i < len(wide); i++ {
		wide[i] = hierNode{"e", 0, false}
	}
	std, row := `<dgm:alg type="hierRoot"/>`, `<dgm:alg type="hierChild"/>`
	cases := []struct {
		name   string
		nodes  []hierNode
		layout string
	}{
		{"an offset of 1e300 boxes", []hierNode{{"M", -1, false}, {"E", 0, false}}, hierLayout(`<dgm:alg type="hierRoot"><dgm:param type="hierAlign" val="tL"/></dgm:alg>`,
			`<dgm:alg type="hierChild"><dgm:param type="linDir" val="fromT"/></dgm:alg>`, `<dgm:constr type="alignOff" val="1e300"/>`)},
		{"a chain deeper than a diagram nests", chain, hierLayout(std, row, ``)},
		{"siblings past the steps", wide, hierLayout(std, row, ``)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			start := time.Now()
			root := ParseDiagram(mustParse(t, hierData(tc.nodes, false)), mustParse(t, tc.layout), nil, nil).LayOut(1_000_000, 500_000, nil)
			if root == nil || root.W != 1_000_000 || root.H != 500_000 {
				t.Fatal("the root is not the diagram's box")
			}
			if d := time.Since(start); d > 20*time.Second {
				t.Errorf("took %v", d)
			}
		})
	}
}

// texts lists the texts of the comp shapes under s, sorted.
func texts(s *DiagramShape) []string {
	var boxes []string
	compBoxes(s, 0, 0, &boxes)
	var out []string
	for _, b := range boxes {
		out = append(out, strings.Fields(b)[0])
	}
	slices.Sort(out)
	return out
}

// TestDiagramLayoutNodeOutsideItsForEach lays out layout nodes that no
// for-each over their parent repeats (a root's comp, kids and assts) for
// points whose position among the points of their name is not the
// position of their parent among its own: each takes its parent's child
// of its name, and every point shows its text.
func TestDiagramLayoutNodeOutsideItsForEach(t *testing.T) {
	nodes := []hierNode{{"Manager", -1, false}, {"E1", 0, false}, {"A", 0, true}, {"Manager2", -1, false}}
	root := ParseDiagram(mustParse(t, hierData(nodes, false)), mustParse(t, hierLayout(`<dgm:alg type="hierRoot"/>`, `<dgm:alg type="hierChild"/>`, ``)), nil, nil).LayOut(14400, 9120, nil)
	if got, want := strings.Join(texts(root), " "), "A E1 Manager Manager2"; got != want {
		t.Errorf("texts %q, want %q", got, want)
	}
}

// TestDiagramForEachOfAssistants lays out a for-each of ptType asst
// (21.4.2.8): every assistant of the point, not the first alone.
func TestDiagramForEachOfAssistants(t *testing.T) {
	nodes := []hierNode{{"M", -1, false}, {"A1", 0, true}, {"A2", 0, true}, {"E1", 0, false}}
	root := ParseDiagram(mustParse(t, hierData(nodes, false)), mustParse(t, hierLayout(`<dgm:alg type="hierRoot"/>`, `<dgm:alg type="hierChild"/>`, ``)), nil, nil).LayOut(14400, 9120, nil)
	if got, want := strings.Join(texts(root), " "), "A1 A2 E1 M"; got != want {
		t.Errorf("texts %q, want %q", got, want)
	}
}

// TestDiagramIfCountsItsPointType evaluates an if of func cnt (21.4.2.11)
// over the children of its point type alone: a node whose one child is no
// assistant has no assistant.
func TestDiagramIfCountsItsPointType(t *testing.T) {
	data := strings.Replace(diagramData("a", "b"), `</dgm:ptLst>`, `<dgm:pt modelId="k0"><dgm:prSet/><dgm:spPr/></dgm:pt></dgm:ptLst>`, 1)
	data = strings.Replace(data, `</dgm:cxnLst>`, `<dgm:cxn modelId="kx" srcId="n0" destId="k0" srcOrd="0"/></dgm:cxnLst>`, 1)
	shape := `<dgm:choose><dgm:if axis="ch" ptType="asst" func="cnt" op="gte" val="1"><dgm:shape type="ellipse"/></dgm:if><dgm:else><dgm:shape type="rect"/></dgm:else></dgm:choose>`
	layout := diagramLayout(`<dgm:alg type="lin"/><dgm:shape/><dgm:forEach name="each" axis="ch" ptType="node"><dgm:layoutNode name="node"><dgm:alg type="tx"/>` + shape + `</dgm:layoutNode></dgm:forEach>`)
	root := ParseDiagram(mustParse(t, data), mustParse(t, layout), nil, nil).LayOut(2000, 1000, nil)
	for _, k := range root.Kids {
		if k.Name == "node" && k.Preset() != "rect" {
			t.Errorf("node %s, want rect: no child is an assistant", k.Preset())
		}
	}
}
