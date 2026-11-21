package ooxml

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// charFit is a measurement for the tests: a text fits when each of its
// words, at half an em a character, fits the box's width less its insets,
// and its lines, one a paragraph at 1.2 em, fit its height.
func charFit(s *DiagramShape) bool {
	w := float64(s.W-s.Insets[0]-s.Insets[2]) / 12700
	h := float64(s.H-s.Insets[1]-s.Insets[3]) / 12700
	size := s.FontSize
	lines := 0
	for _, p := range s.Paragraphs {
		lines++
		for _, t := range p.Source.childrenOf(NSA, "r") {
			for _, word := range strings.Fields(t.Child(NSA, "t").Text) {
				if float64(len(word))*size*0.5 > w {
					return false
				}
			}
		}
	}
	return float64(lines)*size*1.2 <= h
}

// sizedNodes lays the texts out in a row of node shapes 3 inches wide
// and 2 inches tall each, under a root holding rootBody and nodes holding
// nodeBody and margin constraints of no value, and returns the node
// shapes.
func sizedNodes(t *testing.T, texts []string, rootBody, nodeBody string) []*DiagramShape {
	t.Helper()
	layout := `<dgm:alg type="lin"/><dgm:shape/>` + rootBody +
		`<dgm:forEach name="each" axis="ch" ptType="node"><dgm:layoutNode name="node"><dgm:alg type="tx"/><dgm:shape type="rect"/>` + nodeBody + noMargins + `</dgm:layoutNode></dgm:forEach>`
	d := ParseDiagram(mustParse(t, diagramData(texts...)), mustParse(t, diagramLayout(layout)), nil, nil)
	root := d.LayOut(int64(len(texts))*3*914400, 2*914400, charFit)
	var out []*DiagramShape
	for _, k := range root.Kids {
		if k.Name == "node" {
			out = append(out, k)
		}
	}
	if len(out) != len(texts) {
		t.Fatalf("%d nodes, want %d", len(out), len(texts))
	}
	return out
}

// noMargins sets the four margins of a layout node to no value.
const noMargins = `<dgm:constrLst><dgm:constr type="lMarg"/><dgm:constr type="tMarg"/><dgm:constr type="rMarg"/><dgm:constr type="bMarg"/></dgm:constrLst>`

func sizesOf(nodes []*DiagramShape) []float64 {
	var out []float64
	for _, n := range nodes {
		out = append(out, n.FontSize)
	}
	return out
}

// TestDiagramTextSize states the size search: a start from a constraint
// value, a shrink down to a rule's floor, whole points, and the shapes one
// constraint names tied at the size the widest needs (ECMA-376 Part 1,
// 21.4.2.8 constr, 21.4.2.24 rule).
func TestDiagramTextSize(t *testing.T) {
	rule := `<dgm:ruleLst><dgm:rule type="primFontSz" val="5"/></dgm:ruleLst>`
	tied := `<dgm:constrLst><dgm:constr type="primFontSz" for="ch" forName="node" op="equ" val="65"/></dgm:constrLst>`
	own := `<dgm:constrLst><dgm:constr type="primFontSz" val="65"/></dgm:constrLst>`
	// A node is 216 points wide: "a" fits at 65 points, "abcdefghijkl"
	// (12 characters at half an em) at 36 points.
	cases := []struct {
		name           string
		root, node     string
		texts          []string
		want           []float64
		wantAutofitOff bool
	}{
		{"a constraint value no shape outgrows is kept", tied, rule, []string{"a", "b"}, []float64{65, 65}, true},
		{"one constraint ties its nodes at the size the widest needs", tied, rule, []string{"a", "abcdefghijkl"}, []float64{36, 36}, true},
		{"an op none constraint over the nodes ties them too", strings.Replace(tied, ` op="equ"`, ``, 1), rule, []string{"a", "abcdefghijkl"}, []float64{36, 36}, true},
		{"a node's own constraint sizes it alone", ``, own + rule, []string{"a", "abcdefghijkl"}, []float64{65, 36}, true},
		{"without a rule the start is kept", tied, ``, []string{"abcdefghijkl"}, []float64{65}, true},
		{"the floor of the rule stops the shrink", tied, `<dgm:ruleLst><dgm:rule type="primFontSz" val="40"/></dgm:ruleLst>`, []string{"abcdefghijkl"}, []float64{40}, true},
		{"a rule after an unbounded growth is never reached", tied, `<dgm:ruleLst><dgm:rule type="h" val="INF"/><dgm:rule type="primFontSz" val="5"/></dgm:ruleLst>`, []string{"abcdefghijkl"}, []float64{65}, true},
		{"a rule of the enclosing node naming the node shrinks it", tied + `<dgm:ruleLst><dgm:rule type="primFontSz" for="ch" forName="node" val="5"/></dgm:ruleLst>`, ``, []string{"abcdefghijkl"}, []float64{36}, true},
		{"a size between whole points falls to the point under it", strings.Replace(tied, `val="65"`, `val="36.5"`, 1), rule, []string{"abcdefghijkl"}, []float64{36}, true},
		{"no constraint value leaves the text to its autofit", ``, rule, []string{"a"}, []float64{0}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			nodes := sizedNodes(t, c.texts, c.root, c.node)
			if got := sizesOf(nodes); fmt.Sprint(got) != fmt.Sprint(c.want) {
				t.Errorf("sizes %v, want %v", got, c.want)
			}
			for _, n := range nodes {
				if n.Autofit == c.wantAutofitOff {
					t.Errorf("autofit %v, want %v", n.Autofit, !c.wantAutofitOff)
				}
			}
		})
	}
}

// TestDiagramTextSizeInsets states the insets of sized text: a margin
// constraint with refType primFontSz is that factor of the size, a side no
// constraint sets 0.56 of it, a margin constraint of a value that many
// millimetres.
func TestDiagramTextSizeInsets(t *testing.T) {
	tied := `<dgm:constrLst><dgm:constr type="primFontSz" for="ch" forName="node" op="equ" val="20"/></dgm:constrLst>`
	node := `<dgm:constrLst><dgm:constr type="lMarg" refType="primFontSz" fact="0.3"/><dgm:constr type="tMarg" val="1"/></dgm:constrLst>`
	layout := `<dgm:alg type="lin"/><dgm:shape/>` + tied +
		`<dgm:forEach name="each" axis="ch" ptType="node"><dgm:layoutNode name="node"><dgm:alg type="tx"/><dgm:shape type="rect"/>` + node + `</dgm:layoutNode></dgm:forEach>`
	d := ParseDiagram(mustParse(t, diagramData("a")), mustParse(t, diagramLayout(layout)), nil, nil)
	n := d.LayOut(3*914400, 2*914400, charFit).Kids[1]
	want := [4]int64{76200, 36000, 142240, 142240}
	if n.Insets != want || n.Inset != [4]bool{true, true, true, true} {
		t.Errorf("insets %v %v, want %v", n.Insets, n.Inset, want)
	}
}

// TestDiagramTextSizeSecondary states a secondary size that follows the
// primary one it refers to, and a size bounded by a factor of another's.
func TestDiagramTextSizeSecondary(t *testing.T) {
	data := withPresNames(diagramData("abcdefghijkl", "b"), 2, "half", "sib")
	layout := `<dgm:alg type="lin"/><dgm:shape/><dgm:constrLst>` +
		`<dgm:constr type="primFontSz" for="ch" forName="node" op="equ" val="65"/>` +
		`<dgm:constr type="secFontSz" for="ch" forName="sib" refType="primFontSz" refFor="ch" refForName="node" op="equ"/>` +
		`<dgm:constr type="primFontSz" for="ch" forName="half" refType="primFontSz" refFor="ch" refForName="node" op="lte" fact="0.5"/>` +
		`</dgm:constrLst><dgm:ruleLst><dgm:rule type="primFontSz" for="ch" forName="node" val="5"/></dgm:ruleLst>` +
		`<dgm:forEach name="each" axis="ch" ptType="node">` +
		`<dgm:layoutNode name="half"><dgm:alg type="tx"/><dgm:shape type="rect"/><dgm:presOf axis="self"/></dgm:layoutNode>` +
		`<dgm:layoutNode name="node"><dgm:alg type="tx"/><dgm:shape type="rect"/></dgm:layoutNode>` +
		`<dgm:layoutNode name="sib"><dgm:alg type="tx"/><dgm:shape type="rect"/><dgm:constrLst><dgm:constr type="secFontSz" val="65"/><dgm:constr type="primFontSz" refType="secFontSz"/></dgm:constrLst></dgm:layoutNode>` +
		`</dgm:forEach>`
	d := ParseDiagram(mustParse(t, data), mustParse(t, diagramLayout(layout)), nil, nil)
	root := d.LayOut(2*3*914400, 914400, charFit)
	sizes := map[string][]float64{}
	var walk func(s *DiagramShape)
	walk = func(s *DiagramShape) {
		for _, k := range s.Kids {
			if k.text {
				sizes[k.Name] = append(sizes[k.Name], k.FontSize)
			}
			walk(k)
		}
	}
	walk(root)
	if fmt.Sprint(sizes["node"]) != fmt.Sprint(sizes["sib"]) || len(sizes["node"]) == 0 {
		t.Errorf("node sizes %v, sib sizes %v: the secondary size does not follow the primary", sizes["node"], sizes["sib"])
	}
	if len(sizes["half"]) == 0 {
		t.Fatal("no half shapes")
	}
	for i, v := range sizes["half"] {
		if i < len(sizes["node"]) && v > sizes["node"][i]/2 {
			t.Errorf("half size %v past half the node's %v", v, sizes["node"][i])
		}
	}
}

// withPresNames adds to a data model of n nodes made by diagramData a
// presentation point of each name for each node, under the root's.
func withPresNames(data string, n int, names ...string) string {
	var pts, cxns strings.Builder
	for i := range n {
		for j, name := range names {
			fmt.Fprintf(&pts, `<dgm:pt modelId="x%d_%d" type="pres"><dgm:prSet presAssocID="n%d" presName="%s"/><dgm:spPr/></dgm:pt>`, i, j, i, name)
			fmt.Fprintf(&cxns, `<dgm:cxn modelId="xo%d_%d" type="presOf" srcId="n%d" destId="x%d_%d"/><dgm:cxn modelId="xr%d_%d" type="presParOf" srcId="p0" destId="x%d_%d"/>`, i, j, i, i, j, i, j, i, j)
		}
	}
	data = strings.Replace(data, `</dgm:ptLst>`, pts.String()+`</dgm:ptLst>`, 1)
	return strings.Replace(data, `</dgm:cxnLst>`, cxns.String()+`</dgm:cxnLst>`, 1)
}

// TestDiagramTextSizeKeepsTextFormattedByHand lays out a node whose text
// was formatted by hand (prSet custT): it keeps its run's size and its
// insets are the factor of that size.
func TestDiagramTextSizeKeepsTextFormattedByHand(t *testing.T) {
	data := strings.Replace(diagramData("abcdefghijkl"), `<dgm:pt modelId="n0"><dgm:prSet/>`, `<dgm:pt modelId="n0"><dgm:prSet custT="1"/>`, 1)
	data = strings.Replace(data, `<a:r><a:t>abcdefghijkl`, `<a:r><a:rPr sz="700"/><a:t>abcdefghijkl`, 1)
	layout := `<dgm:alg type="lin"/><dgm:shape/><dgm:constrLst><dgm:constr type="primFontSz" for="ch" forName="node" op="equ" val="65"/></dgm:constrLst>` +
		`<dgm:forEach name="each" axis="ch" ptType="node"><dgm:layoutNode name="node"><dgm:alg type="tx"/><dgm:shape type="rect"/>` +
		`<dgm:constrLst><dgm:constr type="lMarg" refType="primFontSz" fact="0.05"/></dgm:constrLst><dgm:ruleLst><dgm:rule type="primFontSz" val="5"/></dgm:ruleLst></dgm:layoutNode></dgm:forEach>`
	d := ParseDiagram(mustParse(t, data), mustParse(t, diagramLayout(layout)), nil, nil)
	calls := 0
	root := d.LayOut(3*914400, 914400, func(s *DiagramShape) bool { calls++; return charFit(s) })
	n := root.Kids[len(root.Kids)-1]
	if n.FontSize != 0 || n.Autofit || n.Insets[0] != 4445 || calls != 0 {
		t.Errorf("size %v autofit %v left inset %d, %d measurements; want 0, false, 4445, 0", n.FontSize, n.Autofit, n.Insets[0], calls)
	}
}

// TestDiagramTextSizeOfHostileDefinitions sizes layout definitions an
// attacker writes: bounds that refer to each other in a cycle, rules of
// NaN and of a negative floor, a start past any font size, and ten
// thousand nodes. Each ends, the same on every run, within the caps.
func TestDiagramTextSizeOfHostileDefinitions(t *testing.T) {
	cycle := `<dgm:constrLst><dgm:constr type="primFontSz" for="ch" forName="node" refType="primFontSz" refFor="ch" refForName="sib" op="lte"/>` +
		`<dgm:constr type="primFontSz" for="ch" forName="sib" refType="primFontSz" refFor="ch" refForName="node" op="lte"/></dgm:constrLst>`
	t.Run("bounds in a cycle", func(t *testing.T) {
		layout := `<dgm:alg type="lin"/><dgm:shape/>` + cycle + `<dgm:forEach name="each" axis="ch" ptType="node">` +
			`<dgm:layoutNode name="node"><dgm:alg type="tx"/><dgm:shape type="rect"/></dgm:layoutNode>` +
			`<dgm:layoutNode name="sib"><dgm:alg type="tx"/><dgm:shape type="rect"/></dgm:layoutNode></dgm:forEach>`
		d := ParseDiagram(mustParse(t, diagramData("abc", "abcdefghijkl")), mustParse(t, diagramLayout(layout)), nil, nil)
		first := fmt.Sprint(sizesOf(d.LayOut(6*914400, 914400, charFit).Kids))
		for range 3 {
			if got := fmt.Sprint(sizesOf(d.LayOut(6*914400, 914400, charFit).Kids)); got != first {
				t.Fatalf("sizes %s, then %s", first, got)
			}
		}
	})
	for _, floor := range []string{"NaN", "-5", "0", "INF"} {
		t.Run("a rule of "+floor, func(t *testing.T) {
			nodes := sizedNodes(t, []string{"abcdefghijkl"}, `<dgm:constrLst><dgm:constr type="primFontSz" for="ch" forName="node" val="65"/></dgm:constrLst>`,
				`<dgm:ruleLst><dgm:rule type="primFontSz" val="`+floor+`"/></dgm:ruleLst>`)
			if nodes[0].FontSize != 65 {
				t.Errorf("size %v, want the start, 65", nodes[0].FontSize)
			}
		})
	}
	t.Run("a start past any font size", func(t *testing.T) {
		nodes := sizedNodes(t, []string{"a"}, `<dgm:constrLst><dgm:constr type="primFontSz" for="ch" forName="node" val="1e300"/></dgm:constrLst>`, ``)
		if nodes[0].FontSize != maxDiagramFontSize {
			t.Errorf("size %v, want %d", nodes[0].FontSize, maxDiagramFontSize)
		}
	})
	t.Run("ten thousand nodes", func(t *testing.T) {
		texts := make([]string, 10_000)
		for i := range texts {
			texts[i] = fmt.Sprint("word", i)
		}
		layout := `<dgm:alg type="lin"/><dgm:shape/><dgm:constrLst><dgm:constr type="primFontSz" for="ch" forName="node" op="equ" val="65"/></dgm:constrLst>` +
			`<dgm:forEach name="each" axis="ch" ptType="node"><dgm:layoutNode name="node"><dgm:alg type="tx"/><dgm:shape type="rect"/>` +
			`<dgm:ruleLst><dgm:rule type="primFontSz" val="5"/></dgm:ruleLst></dgm:layoutNode></dgm:forEach>`
		d := ParseDiagram(mustParse(t, diagramData(texts...)), mustParse(t, diagramLayout(layout)), nil, nil)
		calls := 0
		done := make(chan *DiagramShape, 1)
		go func() {
			done <- d.LayOut(30_000_000, 914400, func(s *DiagramShape) bool { calls++; return charFit(s) })
		}()
		select {
		case <-done:
			if calls > maxDiagramFits {
				t.Errorf("%d measurements, want at most %d", calls, maxDiagramFits)
			}
		case <-time.After(20 * time.Second):
			t.Fatal("the sizing did not finish in 20 s")
		}
	})
}

// TestDiagramTextSizeLeavesUnresolvedBoxes lays out a node whose enclosing
// layout sizes a shape from a user-defined variable (21.4.7.21
// ST_ConstraintType userA to userZ), which the layout engine does not
// resolve: the node's box is not the one PowerPoint computes, so its text
// keeps the size and autofit the text algorithm gives it, and its insets.
func TestDiagramTextSizeLeavesUnresolvedBoxes(t *testing.T) {
	root := `<dgm:constrLst><dgm:constr type="primFontSz" for="ch" forName="node" op="equ" val="65"/>` +
		`<dgm:constr type="w" for="ch" forName="node" refType="userA"/></dgm:constrLst>`
	nodes := sizedNodes(t, []string{"abcdefghijkl"}, root, `<dgm:ruleLst><dgm:rule type="primFontSz" val="5"/></dgm:ruleLst>`)
	if n := nodes[0]; !n.Autofit || n.Inset != [4]bool{} {
		t.Errorf("size %v autofit %v insets %v %v: the sizing changed a box it cannot resolve", n.FontSize, n.Autofit, n.Insets, n.Inset)
	}
}

// TestDiagramTextSizeMarginOfAMargin states a margin that refers to
// another margin of its node (bMarg refType tMarg): it is that margin,
// the factor of the size the other one is.
func TestDiagramTextSizeMarginOfAMargin(t *testing.T) {
	tied := `<dgm:constrLst><dgm:constr type="primFontSz" for="ch" forName="node" op="equ" val="20"/></dgm:constrLst>`
	node := `<dgm:constrLst><dgm:constr type="tMarg" refType="primFontSz" fact="0.5"/><dgm:constr type="bMarg" refType="tMarg"/>` +
		`<dgm:constr type="lMarg"/><dgm:constr type="rMarg"/></dgm:constrLst>`
	layout := `<dgm:alg type="lin"/><dgm:shape/>` + tied +
		`<dgm:forEach name="each" axis="ch" ptType="node"><dgm:layoutNode name="node"><dgm:alg type="tx"/><dgm:shape type="rect"/>` + node + `</dgm:layoutNode></dgm:forEach>`
	d := ParseDiagram(mustParse(t, diagramData("a")), mustParse(t, diagramLayout(layout)), nil, nil)
	n := d.LayOut(3*914400, 2*914400, charFit).Kids[1]
	if want := [4]int64{0, 127000, 0, 127000}; n.Insets != want {
		t.Errorf("insets %v, want %v", n.Insets, want)
	}
}
