package ooxml

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"
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
			name:  "a negative z-order offset moves a shape under the ones before it",
			texts: []string{"a", "b"},
			layout: `<dgm:alg type="lin"/><dgm:shape/><dgm:forEach axis="ch" ptType="node"><dgm:layoutNode name="node"><dgm:shape type="rect"/></dgm:layoutNode>` +
				`<dgm:forEach axis="followSib" ptType="sibTrans" cnt="1"><dgm:layoutNode name="sibTrans"><dgm:shape type="ellipse" zOrderOff="-1"/></dgm:layoutNode></dgm:forEach></dgm:forEach>`,
			w: 600, h: 300,
			want: []string{"background rect 0,0 600x300", "sibTrans ellipse 200,0 200x300", "node rect 0,0 200x300", "node rect 400,0 200x300"},
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
			got := boxes(d.LayOut(c.w, c.h, nil))
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
		{"a margin past 2^31 EMU from a deep negative start level", `<dgm:alg type="tx"><dgm:param type="stBulletLvl" val="-99999"/></dgm:alg>`, func(t *testing.T, s *DiagramShape) {
			if p := s.Paragraphs[0]; !p.Bullet || p.MarL != 285750*100001 {
				t.Errorf("paragraph %+v, want a left margin of %d", p, int64(285750*100001))
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
			root := d.LayOut(600, 300, nil)
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
	root := d.LayOut(900, 300, nil)
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

// TestDiagramTextRotationOfAHugeStartAngle lays out a cycle whose start
// angle is near the largest integer: the kids' rotation, clamped to 2^53
// sixty-thousandths of a degree, turns their text in constant time, as a
// rotation of 179 degrees does.
func TestDiagramTextRotationOfAHugeStartAngle(t *testing.T) {
	texts := make([]string, 200)
	for i := range texts {
		texts[i] = fmt.Sprint(i)
	}
	layout := `<dgm:alg type="cycle"><dgm:param type="stAng" val="9000000000000000000"/><dgm:param type="rotPath" val="alongPath"/></dgm:alg><dgm:shape/>` +
		nodeList("ellipse", "")
	d := ParseDiagram(mustParse(t, diagramData(texts...)), mustParse(t, diagramLayout(layout)), nil, nil)
	done := make(chan *DiagramShape, 1)
	go func() { done <- d.LayOut(1200, 1200, nil) }()
	select {
	case root := <-done:
		n := 0
		for _, k := range root.Kids {
			if k.Name != "node" {
				continue
			}
			n++
			if k.Rot != 1<<53 || k.TextRot != -180 {
				t.Errorf("node rotation %d, text rotation %d; want %d and -180", k.Rot, k.TextRot, int64(1<<53))
			}
		}
		if n != len(texts) {
			t.Errorf("%d nodes, want %d", n, len(texts))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the layout did not finish in 5 s")
	}
}

// TestDiagramTextUprightInATurnedShape lays out nodes whose layout shape
// is turned a quarter: under autoTxRot upr (21.4.2.1 tx, the default) the
// text turns back against the shape's own rot and reads upright.
func TestDiagramTextUprightInATurnedShape(t *testing.T) {
	for _, tc := range []struct {
		rot  string
		want int
	}{{"90", -90}, {"-90", -270}, {"180", -180}, {"0", 0}} {
		layout := `<dgm:alg type="lin"/><dgm:shape/>` + strings.Replace(nodeList("rect", ""), `<dgm:shape type="rect"/>`, `<dgm:shape type="rect" rot="`+tc.rot+`"/>`, 1)
		d := ParseDiagram(mustParse(t, diagramData("a", "b")), mustParse(t, diagramLayout(layout)), nil, nil)
		root := d.LayOut(1200000, 600000, nil)
		n := 0
		for _, k := range root.Kids {
			if k.Name != "node" {
				continue
			}
			n++
			if k.TextRot != tc.want {
				t.Errorf("shape rot %s: text rotation %d, want %d", tc.rot, k.TextRot, tc.want)
			}
		}
		if n != 2 {
			t.Errorf("shape rot %s: %d nodes, want 2", tc.rot, n)
		}
	}
}

// TestParseDiagramCapsTheAtoms reads a layout definition whose constraint
// list, rule list and choose each hold more entries than maxDiagramAtoms:
// the atoms read stop at the cap.
func TestParseDiagramCapsTheAtoms(t *testing.T) {
	var constr, rules, branches strings.Builder
	for range maxDiagramAtoms + 10 {
		constr.WriteString(`<dgm:constr type="w" forName="node" refType="w" fact="0.5"/>`)
		rules.WriteString(`<dgm:rule forName="node"/>`)
		branches.WriteString(`<dgm:if func="cnt" op="gt" val="2"/>`)
	}
	for name, body := range map[string]string{
		"constraints": `<dgm:constrLst>` + constr.String() + `</dgm:constrLst>`,
		"rules":       `<dgm:ruleLst>` + rules.String() + `</dgm:ruleLst>`,
		"branches":    `<dgm:choose>` + branches.String() + `</dgm:choose>`,
	} {
		d := ParseDiagram(nil, mustParse(t, diagramLayout(body)), nil, nil)
		if n := countAtoms(d.Layout, 0); n > maxDiagramAtoms+1 {
			t.Errorf("%s: %d atoms, want at most %d", name, n, maxDiagramAtoms+1)
		}
	}
}

// countAtoms counts a and the atoms under it.
func countAtoms(a *DiagramAtom, depth int) int {
	if a == nil || depth > maxDiagramDepth+1 {
		return 0
	}
	n := 1
	for _, k := range a.Kids {
		n += countAtoms(k, depth+1)
	}
	return n
}

// TestParseDiagramReadsNoNonFiniteNumber reads a shape rotation, an
// aspect ratio and a constraint factor that parse as NaN or an infinity:
// each takes its default.
func TestParseDiagramReadsNoNonFiniteNumber(t *testing.T) {
	for _, v := range []string{"NaN", "Inf", "-Inf", "1e400"} {
		body := `<dgm:alg type="lin"><dgm:param type="ar" val="` + v + `"/></dgm:alg><dgm:shape type="rect" rot="` + v + `"/>` +
			`<dgm:constrLst><dgm:constr type="w" forName="node" refType="w" fact="` + v + `" val="` + v + `"/></dgm:constrLst>`
		d := ParseDiagram(nil, mustParse(t, diagramLayout(body)), nil, nil)
		for _, a := range d.Layout.Kids {
			if a.Ratio != 0 || a.Rot != 0 || a.Kind == AtomConstraint && (a.Constraint.Factor != 1 || a.Constraint.Value != 0) {
				t.Errorf("%s: atom %v read ratio %v, rotation %v, factor %v, value %v", v, a.Kind, a.Ratio, a.Rot, a.Constraint.Factor, a.Constraint.Value)
			}
		}
	}
}

// TestDiagramTextSpacing states the paragraph spacing of laid out text:
// the text algorithm's lnSpPar, lnSpCh, lnSpAfParP and lnSpAfChP (21.4.7.49
// ST_ParameterId), 90%, 90%, 35% and 15% when it states none, and a
// paragraph's own spacing kept.
func TestDiagramTextSpacing(t *testing.T) {
	for _, c := range []struct {
		name, alg, ppr string
		want           [2]int64
	}{
		{"defaults", `<dgm:alg type="tx"/>`, ``, [2]int64{90000, 35000}},
		{"parameters", `<dgm:alg type="tx"><dgm:param type="lnSpPar" val="100"/><dgm:param type="lnSpAfParP" val="20"/></dgm:alg>`, ``, [2]int64{100000, 20000}},
		{"a negative parameter takes the default", `<dgm:alg type="tx"><dgm:param type="lnSpPar" val="-3"/></dgm:alg>`, ``, [2]int64{90000, 35000}},
		{"the paragraph's own", `<dgm:alg type="tx"/>`, `<a:pPr><a:lnSpc><a:spcPct val="100000"/></a:lnSpc><a:spcAft><a:spcPts val="0"/></a:spcAft></a:pPr>`, [2]int64{0, 0}},
	} {
		t.Run(c.name, func(t *testing.T) {
			data := strings.Replace(diagramData("a"), `<a:p><a:r>`, `<a:p>`+c.ppr+`<a:r>`, 1)
			layout := `<dgm:alg type="lin"/><dgm:shape/><dgm:forEach axis="ch" ptType="node"><dgm:layoutNode name="node">` + c.alg + `<dgm:shape type="rect"/></dgm:layoutNode></dgm:forEach>`
			d := ParseDiagram(mustParse(t, data), mustParse(t, diagramLayout(layout)), nil, nil)
			p := d.LayOut(600, 300, nil).Kids[1].Paragraphs[0]
			if got := [2]int64{p.LineSpacing, p.SpaceAfter}; got != c.want {
				t.Errorf("spacing %v, want %v", got, c.want)
			}
		})
	}
}

// TestDiagramLinearSizesFromTheFont lays out a column whose node height is
// a factor of the primary font size its constraint starts at (21.4.2.8
// constr refType primFontSz), read in millimetres: the nodes are that tall
// while they fit the column, and when they do not, the size falls by the
// share that makes them fit, to a whole point, the nodes' text taking it. An equality of no
// value and an lte bound set no height of their own.
func TestDiagramLinearSizesFromTheFont(t *testing.T) {
	for _, c := range []struct {
		size        string
		h           int64
		wantSize    float64
		description string
	}{
		{"10", 540000, 10, "15 mm a node, two fit the column"},
		{"20", 594000, 11, "30 mm a node do not fit: the size falls to the whole point under the share that fits, 11.76"},
	} {
		layout := `<dgm:alg type="lin"><dgm:param type="linDir" val="fromT"/></dgm:alg><dgm:shape/><dgm:constrLst>` +
			`<dgm:constr type="primFontSz" for="des" forName="node" val="` + c.size + `"/>` +
			`<dgm:constr type="h" for="des" forName="node" refType="primFontSz" refFor="des" refForName="node" fact="1.5"/>` +
			`<dgm:constr type="h" for="des" forName="node" op="equ"/>` +
			`<dgm:constr type="h" for="des" forName="node" refType="h" op="lte" fact="0.1"/></dgm:constrLst>` + nodeList("rect", "")
		d := ParseDiagram(mustParse(t, diagramData("a", "b")), mustParse(t, diagramLayout(layout)), nil, nil)
		root := d.LayOut(1270000, 1270000, nil)
		for _, k := range root.Kids {
			if k.Name != "node" {
				continue
			}
			if k.H < c.h-1 || k.H > c.h || math.Abs(k.FontSize-c.wantSize) > 1e-9 {
				t.Errorf("%s: node %d EMU tall at %v points, want %d at %v", c.description, k.H, k.FontSize, c.h, c.wantSize)
			}
		}
	}
}

// TestDiagramLinearAlignment lays out a row and a column whose definitions
// state where their kids sit (21.4.2.3 alg lin: nodeVertAlign across a
// row, vertAlign along a column), and a column that states nothing, whose
// kids start at its top.
func TestDiagramLinearAlignment(t *testing.T) {
	cons := `<dgm:constrLst><dgm:constr type="h" for="ch" forName="node" refType="h" fact="0.25"/><dgm:constr type="w" for="ch" forName="node" refType="w" fact="0.25"/></dgm:constrLst>`
	for _, c := range []struct {
		params string
		want   []string
	}{
		{`<dgm:param type="nodeVertAlign" val="t"/>`, []string{"node rect 0,0 100x100", "node rect 100,0 100x100"}},
		{`<dgm:param type="nodeVertAlign" val="b"/>`, []string{"node rect 0,300 100x100", "node rect 100,300 100x100"}},
		{`<dgm:param type="linDir" val="fromT"/><dgm:param type="vertAlign" val="mid"/>`, []string{"node rect 150,100 100x100", "node rect 150,200 100x100"}},
		{`<dgm:param type="linDir" val="fromT"/>`, []string{"node rect 150,0 100x100", "node rect 150,100 100x100"}},
	} {
		layout := `<dgm:alg type="lin">` + c.params + `</dgm:alg><dgm:shape/>` + cons + nodeList("rect", "")
		d := ParseDiagram(mustParse(t, diagramData("a", "b")), mustParse(t, diagramLayout(layout)), nil, nil)
		got := boxes(d.LayOut(400, 400, nil))[1:]
		if strings.Join(got, "; ") != strings.Join(c.want, "; ") {
			t.Errorf("%s: boxes %q, want %q", c.params, got, c.want)
		}
	}
}

// TestDiagramCompositeBottomEdge lays a kid of a composite out under
// another by a top that refers to the other's bottom edge (21.4.7.21
// ST_ConstraintType b), the edge the other's top and height make, as
// pList1's text sits under its picture.
func TestDiagramCompositeBottomEdge(t *testing.T) {
	layout := `<dgm:alg type="composite"/><dgm:shape/><dgm:constrLst>` +
		`<dgm:constr type="w" for="ch" forName="pict" refType="w"/><dgm:constr type="h" for="ch" forName="pict" refType="h" fact="0.65"/><dgm:constr type="t" for="ch" forName="pict"/>` +
		`<dgm:constr type="w" for="ch" forName="label" refType="w"/><dgm:constr type="h" for="ch" forName="label" refType="h" fact="0.35"/>` +
		`<dgm:constr type="t" for="ch" forName="label" refType="b" refFor="ch" refForName="pict"/></dgm:constrLst>` +
		`<dgm:layoutNode name="pict"><dgm:shape type="rect"/></dgm:layoutNode><dgm:layoutNode name="label"><dgm:shape type="ellipse"/></dgm:layoutNode>`
	d := ParseDiagram(mustParse(t, withPresNames(diagramData("a"), 1, "pict", "label")), mustParse(t, diagramLayout(layout)), nil, nil)
	got := boxes(d.LayOut(1000, 1000, nil))
	want := []string{"background rect 0,0 1000x1000", "pict rect 0,0 1000x650", "label ellipse 0,650 1000x350"}
	if strings.Join(got, "; ") != strings.Join(want, "; ") {
		t.Errorf("boxes\n got %q\nwant %q", got, want)
	}
}

// TestDiagramCompositeSizesFromTheFont lays out a composite whose kids'
// heights are factors of a font size read in millimetres, as hList1's
// parTx and desTx: they keep those heights while they fit, and when they
// do not, the size falls to the whole point under the share that fits,
// the kids and the text taking it.
func TestDiagramCompositeSizesFromTheFont(t *testing.T) {
	layout := `<dgm:alg type="composite"/><dgm:shape/><dgm:constrLst>` +
		`<dgm:constr type="primFontSz" for="ch" forName="pict" val="40"/>` +
		`<dgm:constr type="h" for="ch" forName="pict" op="equ"/>` +
		`<dgm:constr type="h" for="ch" forName="pict" refType="primFontSz" refFor="ch" refForName="pict" fact="0.8"/>` +
		`<dgm:constr type="h" for="ch" forName="label" refType="primFontSz" refFor="ch" refForName="pict" fact="1.22"/>` +
		`<dgm:constr type="t" for="ch" forName="pict"/><dgm:constr type="t" for="ch" forName="label" refType="h" refFor="ch" refForName="pict"/></dgm:constrLst>` +
		`<dgm:layoutNode name="pict"><dgm:alg type="tx"/><dgm:shape type="rect"/></dgm:layoutNode><dgm:layoutNode name="label"><dgm:shape type="ellipse"/></dgm:layoutNode>`
	for _, c := range []struct {
		h           int64
		want        []string
		wantSize    float64
		description string
	}{
		// 40 points: 32 and 48.8 mm, 2908800 EMU in all.
		{3000000, []string{"pict rect 0,45600 1000x1152000", "label ellipse 0,1197600 1000x1756800"}, 40, "the kids fit"},
		// 2000000 EMU hold 0.688 of them: 27.5 points, 27 in whole points.
		{2000000, []string{"pict rect 0,18280 1000x777600", "label ellipse 0,795880 1000x1185840"}, 27, "the size falls"},
	} {
		d := ParseDiagram(mustParse(t, withPresNames(diagramData("a"), 1, "pict", "label")), mustParse(t, diagramLayout(layout)), nil, nil)
		root := d.LayOut(1000, c.h, nil)
		got := boxes(root)[1:]
		if strings.Join(got, "; ") != strings.Join(c.want, "; ") {
			t.Errorf("%s: boxes %q, want %q", c.description, got, c.want)
		}
		if size := root.Kids[1].FontSize; size != c.wantSize {
			t.Errorf("%s: text at %v points, want %v", c.description, size, c.wantSize)
		}
	}
}

// TestDiagramCompositeOwnBound lays out a composite kid whose own
// constraint keeps its height at most 0.4 of its width (op lte, 21.4.7.10
// ST_BoolOperator): it bounds the height the composite sets, and sets none
// of its own.
func TestDiagramCompositeOwnBound(t *testing.T) {
	for _, c := range []struct {
		fact string
		want string
	}{
		{"0.2", "pict rect 0,400 1000x200"},
		{"0.6", "pict rect 0,300 1000x400"},
	} {
		layout := `<dgm:alg type="composite"/><dgm:shape/><dgm:constrLst>` +
			`<dgm:constr type="w" for="ch" forName="pict" refType="w"/><dgm:constr type="h" for="ch" forName="pict" refType="h" fact="` + c.fact + `"/></dgm:constrLst>` +
			`<dgm:layoutNode name="pict"><dgm:shape type="rect"/><dgm:constrLst><dgm:constr type="h" refType="w" op="lte" fact="0.4"/></dgm:constrLst></dgm:layoutNode>`
		d := ParseDiagram(mustParse(t, withPresNames(diagramData("a"), 1, "pict")), mustParse(t, diagramLayout(layout)), nil, nil)
		if got := boxes(d.LayOut(1000, 1000, nil))[1]; got != c.want {
			t.Errorf("height %s of the box: %q, want %q", c.fact, got, c.want)
		}
	}
}

// TestDiagramCompositeInferredEdges lays out composite kids whose edges
// refer to edges another kid's constraints do not state but make: the
// right edge of a kid placed by its centre and width, the left edge of one
// placed by its right edge and width (21.4.7.21 ST_ConstraintType), as
// vProcess5 places its arrows and its texts.
func TestDiagramCompositeInferredEdges(t *testing.T) {
	layout := `<dgm:alg type="composite"/><dgm:shape/><dgm:constrLst>` +
		`<dgm:constr type="w" for="ch" forName="pict" refType="w" fact="0.5"/><dgm:constr type="h" for="ch" forName="pict" refType="h" fact="0.5"/>` +
		`<dgm:constr type="ctrX" for="ch" forName="pict" refType="w" fact="0.5"/><dgm:constr type="t" for="ch" forName="pict"/>` +
		`<dgm:constr type="w" for="ch" forName="label" refType="w" fact="0.25"/><dgm:constr type="h" for="ch" forName="label" refType="h" fact="0.5"/>` +
		`<dgm:constr type="r" for="ch" forName="label" refType="r" refFor="ch" refForName="pict"/><dgm:constr type="t" for="ch" forName="label" refType="h" fact="0.5"/>` +
		`<dgm:constr type="r" for="ch" forName="mark" refType="l" refFor="ch" refForName="label"/><dgm:constr type="w" for="ch" forName="mark" refType="w" fact="0.1"/>` +
		`<dgm:constr type="h" for="ch" forName="mark" refType="h" fact="0.5"/><dgm:constr type="t" for="ch" forName="mark" refType="h" fact="0.5"/></dgm:constrLst>` +
		`<dgm:layoutNode name="pict"><dgm:shape type="rect"/></dgm:layoutNode><dgm:layoutNode name="label"><dgm:shape type="ellipse"/></dgm:layoutNode><dgm:layoutNode name="mark"><dgm:shape type="rect"/></dgm:layoutNode>`
	d := ParseDiagram(mustParse(t, withPresNames(diagramData("a"), 1, "pict", "label", "mark")), mustParse(t, diagramLayout(layout)), nil, nil)
	got := boxes(d.LayOut(1000, 1000, nil))[1:]
	want := []string{"pict rect 250,0 500x500", "label ellipse 500,500 250x500", "mark rect 400,500 100x500"}
	if strings.Join(got, "; ") != strings.Join(want, "; ") {
		t.Errorf("boxes\n got %q\nwant %q", got, want)
	}
}

// TestDiagramCompositeOffsets moves a composite kid's edges by the offsets
// its constraints set (21.4.7.21 ST_ConstraintType lOff, rOff), as
// vProcess5 keeps a node's text clear of the arrow beside it.
func TestDiagramCompositeOffsets(t *testing.T) {
	layout := `<dgm:alg type="composite"/><dgm:shape/><dgm:constrLst>` +
		`<dgm:constr type="w" for="ch" forName="pict" refType="w" fact="0.5"/><dgm:constr type="h" for="ch" forName="pict" refType="h"/>` +
		`<dgm:constr type="l" for="ch" forName="pict"/><dgm:constr type="t" for="ch" forName="pict"/>` +
		`<dgm:constr type="rOff" for="ch" forName="pict" refType="w" fact="-0.1"/><dgm:constr type="lOff" for="ch" forName="pict" refType="w" fact="0.05"/></dgm:constrLst>` +
		`<dgm:layoutNode name="pict"><dgm:shape type="rect"/></dgm:layoutNode>`
	d := ParseDiagram(mustParse(t, withPresNames(diagramData("a"), 1, "pict")), mustParse(t, diagramLayout(layout)), nil, nil)
	if got, want := boxes(d.LayOut(1000, 1000, nil))[1], "pict rect 50,0 350x1000"; got != want {
		t.Errorf("box %q, want %q", got, want)
	}
}

// TestDiagramCompositeRefersToPlacedBox lays out a composite kid whose
// bottom and centre refer to those of a kid placed at the top left by
// default, which its constraints do not state, as venn2 stands its
// smaller circles on the largest.
func TestDiagramCompositeRefersToPlacedBox(t *testing.T) {
	layout := `<dgm:alg type="composite"/><dgm:shape/><dgm:constrLst>` +
		`<dgm:constr type="w" for="ch" forName="pict" refType="w"/><dgm:constr type="h" for="ch" forName="pict" refType="h"/>` +
		`<dgm:constr type="w" for="ch" forName="label" refType="w" fact="0.5"/><dgm:constr type="h" for="ch" forName="label" refType="h" fact="0.5"/>` +
		`<dgm:constr type="b" for="ch" forName="label" refType="b" refFor="ch" refForName="pict"/>` +
		`<dgm:constr type="ctrX" for="ch" forName="label" refType="ctrX" refFor="ch" refForName="pict"/></dgm:constrLst>` +
		`<dgm:layoutNode name="pict"><dgm:shape type="rect"/></dgm:layoutNode><dgm:layoutNode name="label"><dgm:shape type="ellipse"/></dgm:layoutNode>`
	d := ParseDiagram(mustParse(t, withPresNames(diagramData("a"), 1, "pict", "label")), mustParse(t, diagramLayout(layout)), nil, nil)
	got := boxes(d.LayOut(1000, 1000, nil))[1:]
	want := []string{"pict rect 0,0 1000x1000", "label ellipse 250,500 500x500"}
	if strings.Join(got, "; ") != strings.Join(want, "; ") {
		t.Errorf("boxes\n got %q\nwant %q", got, want)
	}
}

// TestDiagramCompositeCutsBeforePlacing places a composite kid taller than
// the composite by its centre: its height is cut to the composite's first,
// as pyramid1's level of 500 mm is.
func TestDiagramCompositeCutsBeforePlacing(t *testing.T) {
	layout := `<dgm:alg type="composite"/><dgm:shape/><dgm:constrLst>` +
		`<dgm:constr type="w" for="ch" forName="pict" refType="w"/><dgm:constr type="h" for="ch" forName="pict" val="500"/>` +
		`<dgm:constr type="ctrY" for="ch" forName="pict" val="1"/></dgm:constrLst>` +
		`<dgm:layoutNode name="pict"><dgm:shape type="rect"/></dgm:layoutNode>`
	d := ParseDiagram(mustParse(t, withPresNames(diagramData("a"), 1, "pict")), mustParse(t, diagramLayout(layout)), nil, nil)
	if got, want := boxes(d.LayOut(100000, 100000, nil))[1], "pict rect 0,-14000 100000x100000"; got != want {
		t.Errorf("box %q, want %q", got, want)
	}
}

// TestDiagramShapesFollowTheSettledSize lays out a composite whose kids are
// sized from a font size that the text then settles under: the layout runs
// again and the kids take the size the text settled at, as hList1's parTx
// and desTx are as tall as Office's text makes them.
//
// # Corpus evidence
//
//   - probe, pptx: hList1 in a 480 by 400 point frame, room for 69.9
//     points: Office sets the text at 51.19 points, the desTx 176.4 points
//     tall, 1.22 of 51.19 mm, and the parTx at its bound, 0.4 of its width.
//   - libreoffice/pptx: smartart-dir's hList1, its text at 31.01 points in
//     Office's page, with a parTx 70.56 and a desTx 107.28 points tall, 0.8
//     and 1.22 of 31.01 mm.
func TestDiagramShapesFollowTheSettledSize(t *testing.T) {
	layout := `<dgm:alg type="composite"/><dgm:shape/><dgm:constrLst>` +
		`<dgm:constr type="primFontSz" for="ch" forName="pict" val="40"/>` +
		`<dgm:constr type="w" for="ch" forName="pict" refType="w"/><dgm:constr type="w" for="ch" forName="label" refType="w"/>` +
		`<dgm:constr type="h" for="ch" forName="pict" refType="primFontSz" refFor="ch" refForName="pict" fact="0.8"/>` +
		`<dgm:constr type="h" for="ch" forName="label" refType="primFontSz" refFor="ch" refForName="pict" fact="1.22"/>` +
		`<dgm:constr type="t" for="ch" forName="pict"/><dgm:constr type="t" for="ch" forName="label" refType="h" refFor="ch" refForName="pict"/></dgm:constrLst>` +
		`<dgm:layoutNode name="pict"><dgm:alg type="tx"/><dgm:shape type="rect"/>` + noMargins + `<dgm:ruleLst><dgm:rule type="primFontSz" val="5"/></dgm:ruleLst></dgm:layoutNode>` +
		`<dgm:layoutNode name="label"><dgm:shape type="ellipse"/></dgm:layoutNode>`
	d := ParseDiagram(mustParse(t, withPresNames(diagramData("abcdefghijkl"), 1, "pict", "label")), mustParse(t, diagramLayout(layout)), nil, nil)
	// Twelve characters at half an em fit 100 points at 16 points: the kids
	// are then 0.8 and 1.22 of 16 mm tall.
	root := d.LayOut(100*12700, 3000000, charFit)
	pict, label := root.Kids[1], root.Kids[2]
	if pict.FontSize != 16 || pict.H != 460800 || label.H != 702720 {
		t.Errorf("text at %v points, kids %d and %d EMU tall, want 16, 460800 and 702720", pict.FontSize, pict.H, label.H)
	}
}

// TestDiagramShapesKeepTheFirstSizeWhenTextFalls lays out a composite
// whose kids follow the size its first text settles at, where that leaves
// the second text less room than it had: the second text would settle
// lower, so the first layout stands.
func TestDiagramShapesKeepTheFirstSizeWhenTextFalls(t *testing.T) {
	layout := `<dgm:alg type="composite"/><dgm:shape/><dgm:constrLst>` +
		`<dgm:constr type="primFontSz" for="ch" forName="pict" val="40"/><dgm:constr type="primFontSz" for="ch" forName="label" val="20"/>` +
		`<dgm:constr type="w" for="ch" forName="pict" refType="w"/><dgm:constr type="w" for="ch" forName="label" refType="w"/>` +
		`<dgm:constr type="h" for="ch" forName="pict" refType="primFontSz" refFor="ch" refForName="pict" fact="0.8"/>` +
		`<dgm:constr type="h" for="ch" forName="label" refType="primFontSz" refFor="ch" refForName="pict" fact="0.2"/>` +
		`<dgm:constr type="t" for="ch" forName="pict"/><dgm:constr type="t" for="ch" forName="label" refType="h" refFor="ch" refForName="pict"/></dgm:constrLst>` +
		`<dgm:layoutNode name="pict"><dgm:alg type="tx"/><dgm:shape type="rect"/>` + noMargins + `<dgm:ruleLst><dgm:rule type="primFontSz" val="5"/></dgm:ruleLst></dgm:layoutNode>` +
		`<dgm:layoutNode name="label"><dgm:alg type="tx"/><dgm:shape type="rect"/>` + noMargins + `<dgm:ruleLst><dgm:rule type="primFontSz" val="5"/></dgm:ruleLst></dgm:layoutNode>`
	d := ParseDiagram(mustParse(t, withPresNames(diagramData("abcdefghijkl"), 1, "pict", "label")), mustParse(t, diagramLayout(layout)), nil, nil)
	// Both texts settle at 16 points in kids sized at 40. Kids sized at 16
	// leave the label 9.1 points, where its text would fall to 7.
	root := d.LayOut(100*12700, 3000000, charFit)
	pict, label := root.Kids[1], root.Kids[2]
	if pict.H != 1152000 || label.H != 288000 || label.FontSize != 16 {
		t.Errorf("kids %d and %d EMU tall, the label at %v points, want 1152000, 288000 and 16", pict.H, label.H, label.FontSize)
	}
}

// TestDiagramLinearShareOfAShrinkingRow states the share of a row whose
// rule shrinks its nodes (21.4.2.24 rule w, val 0): the nodes share what
// the widths the row states leave, padding1 at 0.08 of the row and
// padding2 at the user-defined variable userP the enclosing composite sets
// (a quarter of its dummy's height, which an lte constraint holds to 0.4
// of its width); a space takes 0.2 of a node's share, and the node the
// user widened (prSet custScaleX 150000) one and a half shares.
func TestDiagramLinearShareOfAShrinkingRow(t *testing.T) {
	var pts, cxns strings.Builder
	pts.WriteString(`<dgm:pt modelId="0" type="doc"/>`)
	pres := func(id, assoc, name, parent, extra string) {
		fmt.Fprintf(&pts, `<dgm:pt modelId="%s" type="pres"><dgm:prSet presAssocID="%s" presName="%s"%s/><dgm:spPr/></dgm:pt>`, id, assoc, name, extra)
		if parent == "" {
			fmt.Fprintf(&cxns, `<dgm:cxn modelId="c%s" type="presOf" srcId="%s" destId="%s"/>`, id, assoc, id)
			return
		}
		fmt.Fprintf(&cxns, `<dgm:cxn modelId="c%s" type="presParOf" srcId="%s" destId="%s"/>`, id, parent, id)
	}
	pres("p0", "0", "root", "", "")
	pres("pd", "0", "dummy", "p0", "")
	pres("pl", "0", "linH", "p0", "")
	pres("p1", "0", "padding1", "pl", "")
	for i, text := range []string{"a", "b", "c"} {
		fmt.Fprintf(&pts, `<dgm:pt modelId="n%d"><dgm:prSet/><dgm:spPr/><dgm:t><a:bodyPr/><a:p><a:r><a:t>%s</a:t></a:r></a:p></dgm:t></dgm:pt>`, i, text)
		fmt.Fprintf(&pts, `<dgm:pt modelId="s%d" type="sibTrans" cxnId="x%d"/>`, i, i)
		fmt.Fprintf(&cxns, `<dgm:cxn modelId="x%d" srcId="0" destId="n%d" srcOrd="%d" sibTransId="s%d"/>`, i, i, i, i)
		extra := ""
		if text == "c" {
			extra = ` custScaleX="150000"`
		}
		pres(fmt.Sprintf("pn%d", i), fmt.Sprintf("n%d", i), "node", "pl", extra)
		fmt.Fprintf(&cxns, `<dgm:cxn modelId="o%d" type="presOf" srcId="n%d" destId="pn%d"/>`, i, i, i)
		pres(fmt.Sprintf("ps%d", i), fmt.Sprintf("s%d", i), "space", "pl", "")
	}
	pres("p2", "0", "padding2", "pl", "")
	data := `<dgm:dataModel ` + dgmNS + `><dgm:ptLst>` + pts.String() + `</dgm:ptLst><dgm:cxnLst>` + cxns.String() + `</dgm:cxnLst></dgm:dataModel>`
	sp := func(name string) string {
		return `<dgm:layoutNode name="` + name + `"><dgm:alg type="sp"/><dgm:shape/></dgm:layoutNode>`
	}
	layout := diagramLayout(`<dgm:alg type="composite"/><dgm:shape/><dgm:constrLst>` +
		`<dgm:constr type="w" for="ch" forName="dummy" refType="w"/><dgm:constr type="h" for="ch" forName="dummy" refType="h"/>` +
		`<dgm:constr type="h" for="ch" forName="dummy" refType="w" refFor="ch" refForName="dummy" op="lte" fact="0.4"/>` +
		`<dgm:constr type="w" for="ch" forName="linH" refType="w"/><dgm:constr type="h" for="ch" forName="linH" refType="h"/>` +
		`<dgm:constr type="userP" for="ch" forName="linH" refType="h" refFor="ch" refForName="dummy" fact="0.25"/></dgm:constrLst>` +
		sp("dummy") + `<dgm:layoutNode name="linH"><dgm:alg type="lin"/><dgm:shape/><dgm:constrLst>` +
		`<dgm:constr type="w" for="ch" forName="node" refType="w"/><dgm:constr type="w" for="ch" forName="space" refType="w" refFor="ch" refForName="node" fact="0.2"/>` +
		`<dgm:constr type="w" for="ch" forName="padding1" refType="w" fact="0.08"/><dgm:constr type="userP"/><dgm:constr type="w" for="ch" forName="padding2" refType="userP"/></dgm:constrLst>` +
		`<dgm:ruleLst><dgm:rule type="w" for="ch" forName="node" val="0"/></dgm:ruleLst>` + sp("padding1") +
		`<dgm:forEach axis="ch" ptType="node"><dgm:layoutNode name="node"><dgm:alg type="tx"/><dgm:shape type="rect"/></dgm:layoutNode>` +
		`<dgm:forEach axis="followSib" ptType="sibTrans" cnt="1">` + sp("space") + `</dgm:forEach></dgm:forEach>` + sp("padding2") + `</dgm:layoutNode>`)
	root := ParseDiagram(mustParse(t, data), mustParse(t, layout), nil, nil).LayOut(10000, 5000, nil)
	if root == nil {
		t.Fatal("no layout")
	}
	var got []string
	var walk func(s *DiagramShape, x int64)
	walk = func(s *DiagramShape, x int64) {
		x += s.X
		if s.Name == "node" {
			got = append(got, fmt.Sprintf("%d+%d", x, s.W))
		}
		for _, k := range s.Kids {
			walk(k, x)
		}
	}
	walk(root, -root.X)
	// The dummy is 4000 tall, userP 1000, padding1 800: 8200 left for 3.9
	// shares of 2102.56.
	if want := []string{"800+2102", "3322+2102", "5844+3153"}; strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("nodes at %v, want %v", got, want)
	}
}

// TestDiagramDirectionOfTheDiagram evaluates the variable dir (21.4.2.33)
// for a point that states none as the nearest point above it states it:
// the direction set on the diagram's root turns every node.
func TestDiagramDirectionOfTheDiagram(t *testing.T) {
	data := strings.Replace(diagramData("a", "b"), `<dgm:prSet presAssocID="0" presName="root"/>`,
		`<dgm:prSet presAssocID="0" presName="root"><dgm:presLayoutVars><dgm:dir val="rev"/></dgm:presLayoutVars></dgm:prSet>`, 1)
	shape := `<dgm:choose><dgm:if func="var" arg="dir" op="equ" val="norm"><dgm:shape type="rect"/></dgm:if><dgm:else><dgm:shape type="ellipse"/></dgm:else></dgm:choose>`
	layout := diagramLayout(`<dgm:alg type="lin"/><dgm:shape/><dgm:forEach name="each" axis="ch" ptType="node"><dgm:layoutNode name="node"><dgm:alg type="tx"/>` + shape + `</dgm:layoutNode></dgm:forEach>`)
	root := ParseDiagram(mustParse(t, data), mustParse(t, layout), nil, nil).LayOut(2000, 1000, nil)
	n := 0
	for _, k := range root.Kids {
		if k.Name == "node" {
			n++
			if k.Preset() != "ellipse" {
				t.Errorf("node %s, want the ellipse of dir rev", k.Preset())
			}
		}
	}
	if n != 2 {
		t.Errorf("%d nodes, want 2", n)
	}
}
