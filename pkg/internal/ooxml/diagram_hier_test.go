package ooxml

import (
	"fmt"
	"slices"
	"strings"
	"testing"
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
