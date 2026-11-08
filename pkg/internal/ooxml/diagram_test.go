package ooxml

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

const dgmNS = `xmlns:dgm="http://schemas.openxmlformats.org/drawingml/2006/diagram" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"`

// diagramData returns a data model of a list of nodes with texts under
// the document point, with the presentation points a layout of the
// layout node names root, node and sibTrans lays out.
func diagramData(texts ...string) string {
	var pts, cxns strings.Builder
	pts.WriteString(`<dgm:pt modelId="0" type="doc"/>`)
	pts.WriteString(`<dgm:pt modelId="p0" type="pres"><dgm:prSet presAssocID="0" presName="root"/><dgm:spPr/></dgm:pt>`)
	cxns.WriteString(`<dgm:cxn modelId="c0" type="presOf" srcId="0" destId="p0"/>`)
	for i, t := range texts {
		fmt.Fprintf(&pts, `<dgm:pt modelId="n%d"><dgm:prSet/><dgm:spPr/><dgm:t><a:bodyPr/><a:p><a:r><a:t>%s</a:t></a:r></a:p></dgm:t></dgm:pt>`, i, t)
		fmt.Fprintf(&pts, `<dgm:pt modelId="t%d" type="parTrans" cxnId="x%d"/><dgm:pt modelId="s%d" type="sibTrans" cxnId="x%d"><dgm:spPr/></dgm:pt>`, i, i, i, i)
		fmt.Fprintf(&pts, `<dgm:pt modelId="pn%d" type="pres"><dgm:prSet presAssocID="n%d" presName="node" presStyleLbl="node1" presStyleIdx="%d"/><dgm:spPr/></dgm:pt>`, i, i, i)
		fmt.Fprintf(&pts, `<dgm:pt modelId="ps%d" type="pres"><dgm:prSet presAssocID="s%d" presName="sibTrans"/><dgm:spPr/></dgm:pt>`, i, i)
		fmt.Fprintf(&cxns, `<dgm:cxn modelId="x%d" srcId="0" destId="n%d" srcOrd="%d" destOrd="0" parTransId="t%d" sibTransId="s%d"/>`, i, i, i, i, i)
		fmt.Fprintf(&cxns, `<dgm:cxn modelId="o%d" type="presOf" srcId="n%d" destId="pn%d"/><dgm:cxn modelId="q%d" type="presOf" srcId="s%d" destId="ps%d"/>`, i, i, i, i, i, i)
		fmt.Fprintf(&cxns, `<dgm:cxn modelId="r%d" type="presParOf" srcId="p0" destId="pn%d" srcOrd="%d"/><dgm:cxn modelId="u%d" type="presParOf" srcId="p0" destId="ps%d"/>`, i, i, 2*i, i, i)
	}
	return `<dgm:dataModel ` + dgmNS + `><dgm:ptLst>` + pts.String() + `</dgm:ptLst><dgm:cxnLst>` + cxns.String() + `</dgm:cxnLst>` +
		`<dgm:bg><a:solidFill><a:srgbClr val="FF0000"/></a:solidFill></dgm:bg></dgm:dataModel>`
}

// diagramLayout returns a layout definition whose root layout node holds
// body.
func diagramLayout(body string) string {
	return `<dgm:layoutDef ` + dgmNS + `><dgm:layoutNode name="root">` + body + `</dgm:layoutNode></dgm:layoutDef>`
}

// nodeList is the for-each of the node layout node, a text in a shape of
// type typ.
func nodeList(typ, extra string) string {
	return `<dgm:forEach name="each" axis="ch" ptType="node"><dgm:layoutNode name="node"><dgm:alg type="tx"/><dgm:shape type="` + typ + `"/>` + extra + `</dgm:layoutNode></dgm:forEach>`
}

func mustParse(t testing.TB, s string) *Element {
	t.Helper()
	e, err := Parse(context.Background(), []byte(s), nil)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

// boxes lists the root's kids as "name preset x,y wxh".
func boxes(root *DiagramShape) []string {
	var out []string
	for _, k := range root.Kids {
		out = append(out, fmt.Sprintf("%s %s %d,%d %dx%d", k.Name, k.Preset(), k.X, k.Y, k.W, k.H))
	}
	return out
}

func TestDiagramLayOut(t *testing.T) {
	cases := []struct {
		name   string
		texts  []string
		layout string
		w, h   int64
		want   []string
	}{
		{
			name:   "lin from the left shares the width",
			texts:  []string{"a", "b", "c"},
			layout: `<dgm:alg type="lin"/><dgm:shape/>` + nodeList("rect", ""),
			w:      900, h: 300,
			want: []string{"background rect 0,0 900x300", "node rect 0,0 300x300", "node rect 300,0 300x300", "node rect 600,0 300x300"},
		},
		{
			name:   "lin from the top with constrained heights centres them",
			texts:  []string{"a", "b"},
			layout: `<dgm:alg type="lin"><dgm:param type="linDir" val="fromT"/></dgm:alg><dgm:shape/><dgm:constrLst><dgm:constr type="h" for="ch" forName="node" refType="h" fact="0.25"/><dgm:constr type="w" for="ch" forName="node" refType="w" fact="0.5"/></dgm:constrLst>` + nodeList("roundRect", ""),
			w:      400, h: 800,
			want: []string{"background rect 0,0 400x800", "node roundRect 100,0 200x200", "node roundRect 100,200 200x200"},
		},
		{
			name:   "cycle puts the nodes a quarter of the box on a circle",
			texts:  []string{"a", "b", "c", "d"},
			layout: `<dgm:alg type="cycle"/><dgm:shape/>` + nodeList("ellipse", ""),
			w:      1200, h: 1200,
			want: []string{"background rect 0,0 1200x1200", "node ellipse 450,0 300x300", "node ellipse 900,450 300x300", "node ellipse 450,900 300x300", "node ellipse 0,450 300x300"},
		},
		{
			name:  "choose takes the branch the node count decides",
			texts: []string{"a", "b", "c"},
			layout: `<dgm:choose><dgm:if func="cnt" op="gt" val="2"><dgm:alg type="lin"><dgm:param type="linDir" val="fromT"/></dgm:alg></dgm:if><dgm:else><dgm:alg type="lin"/></dgm:else></dgm:choose><dgm:shape/>` +
				nodeList("rect", ""),
			w: 300, h: 900,
			want: []string{"background rect 0,0 300x900", "node rect 0,0 300x300", "node rect 0,300 300x300", "node rect 0,600 300x300"},
		},
		{
			name:  "composite places named kids by constraints in millimetres and factors",
			texts: []string{"a"},
			layout: `<dgm:alg type="composite"/><dgm:shape/><dgm:constrLst>` +
				`<dgm:constr type="l" for="ch" forName="node" val="1"/><dgm:constr type="w" for="ch" forName="node" refType="w" fact="0.5"/>` +
				`<dgm:constr type="h" for="ch" forName="node" refType="h" fact="0.5"/></dgm:constrLst>` + nodeList("rect", ""),
			w: 400000, h: 200000,
			want: []string{"background rect 0,0 400000x200000", "node rect 36000,50000 200000x100000"},
		},
		{
			name:   "a for-each counts no more than cnt",
			texts:  []string{"a", "b", "c"},
			layout: `<dgm:alg type="lin"/><dgm:shape/><dgm:forEach axis="ch" ptType="node" cnt="2"><dgm:layoutNode name="node"><dgm:shape type="rect"/></dgm:layoutNode></dgm:forEach>`,
			w:      600, h: 300,
			want: []string{"background rect 0,0 600x300", "node rect 0,0 300x300", "node rect 300,0 300x300"},
		},
		{
			name:  "z-order offsets lift a shape over the next ones, the last transition hidden",
			texts: []string{"a", "b"},
			layout: `<dgm:alg type="lin"/><dgm:shape/><dgm:forEach axis="ch" ptType="node"><dgm:layoutNode name="node"><dgm:shape type="rect" zOrderOff="1"/></dgm:layoutNode>` +
				`<dgm:forEach axis="followSib" ptType="sibTrans" cnt="1"><dgm:layoutNode name="sibTrans"><dgm:shape type="ellipse"/></dgm:layoutNode></dgm:forEach></dgm:forEach>`,
			w: 600, h: 300,
			want: []string{"background rect 0,0 600x300", "sibTrans ellipse 200,0 200x300", "node rect 0,0 200x300", "node rect 400,0 200x300"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := ParseDiagram(mustParse(t, diagramData(c.texts...)), mustParse(t, diagramLayout(c.layout)), nil, nil)
			got := boxes(d.LayOut(c.w, c.h))
			if strings.Join(got, "; ") != strings.Join(c.want, "; ") {
				t.Errorf("boxes\n got %q\nwant %q", got, c.want)
			}
		})
	}
}

func TestDiagramText(t *testing.T) {
	cases := []struct {
		name, alg string
		check     func(t *testing.T, s *DiagramShape)
	}{
		{"centred and autofit by default", `<dgm:alg type="tx"/>`, func(t *testing.T, s *DiagramShape) {
			if s.Anchor != "ctr" || !s.Autofit || len(s.Paragraphs) != 1 || s.Paragraphs[0].Align != "ctr" || s.Paragraphs[0].Bullet {
				t.Errorf("anchor %q autofit %v paragraphs %+v", s.Anchor, s.Autofit, s.Paragraphs)
			}
		}},
		{"top anchor and left alignment from the parameters", `<dgm:alg type="tx"><dgm:param type="txAnchorVert" val="t"/><dgm:param type="parTxLTRAlign" val="l"/></dgm:alg>`, func(t *testing.T, s *DiagramShape) {
			if s.Anchor != "t" || s.Paragraphs[0].Align != "l" {
				t.Errorf("anchor %q align %q", s.Anchor, s.Paragraphs[0].Align)
			}
		}},
		{"bullets from the first level", `<dgm:alg type="tx"><dgm:param type="stBulletLvl" val="1"/></dgm:alg>`, func(t *testing.T, s *DiagramShape) {
			p := s.Paragraphs[0]
			if !p.Bullet || p.MarL != 285750 || p.Indent != -285750 || p.Align != "" {
				t.Errorf("paragraph %+v", p)
			}
		}},
		{"sp drops the text", `<dgm:alg type="sp"/>`, func(t *testing.T, s *DiagramShape) {
			if s.Paragraphs != nil {
				t.Errorf("paragraphs %+v", s.Paragraphs)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			layout := `<dgm:alg type="lin"/><dgm:shape/><dgm:forEach axis="ch" ptType="node"><dgm:layoutNode name="node">` + c.alg + `<dgm:shape type="rect"/></dgm:layoutNode></dgm:forEach>`
			d := ParseDiagram(mustParse(t, diagramData("a")), mustParse(t, diagramLayout(layout)), nil, nil)
			root := d.LayOut(600, 300)
			if len(root.Kids) != 2 {
				t.Fatalf("kids %q", boxes(root))
			}
			c.check(t, root.Kids[1])
		})
	}
}

func TestDiagramStyleAndColors(t *testing.T) {
	style := `<dgm:styleDef ` + dgmNS + `><dgm:styleLbl name="node1"><dgm:style><a:lnRef idx="2"><a:scrgbClr r="0" g="0" b="0"/></a:lnRef><a:fillRef idx="1"><a:scrgbClr r="0" g="0" b="0"/></a:fillRef><a:effectRef idx="0"/><a:fontRef idx="minor"><a:schemeClr val="lt1"/></a:fontRef></dgm:style></dgm:styleLbl></dgm:styleDef>`
	colors := `<dgm:colorsDef ` + dgmNS + `><dgm:styleLbl name="node1"><dgm:fillClrLst meth="repeat"><a:schemeClr val="accent1"/><a:schemeClr val="accent2"/></dgm:fillClrLst><dgm:txFillClrLst><a:schemeClr val="dk1"/></dgm:txFillClrLst></dgm:styleLbl></dgm:colorsDef>`
	layout := `<dgm:alg type="lin"/><dgm:shape/>` + nodeList("rect", "")
	d := ParseDiagram(mustParse(t, diagramData("a", "b", "c")), mustParse(t, diagramLayout(layout)), mustParse(t, style), mustParse(t, colors))
	root := d.LayOut(900, 300)
	want := []string{"accent1", "accent2", "accent1"}
	for i, k := range root.Kids[1:] {
		if k.Style.Fill == nil || attrOr(k.Style.Fill, "idx") != "1" || attrOr(k.Style.Line, "idx") != "2" {
			t.Fatalf("kid %d style %+v", i, k.Style)
		}
		if got := attrOr(k.StyleColor.Fill, "val"); got != want[i] {
			t.Errorf("kid %d fill colour %q, want %q", i, got, want[i])
		}
		if got := attrOr(k.StyleColor.Font, "val"); got != "dk1" {
			t.Errorf("kid %d text colour %q", i, got)
		}
		if k.StyleColor.Line != nil {
			t.Errorf("kid %d line colour %v, want the style's own", i, k.StyleColor.Line)
		}
	}
}
