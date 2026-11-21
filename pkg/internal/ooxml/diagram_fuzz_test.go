package ooxml

import (
	"context"
	"testing"
)

func FuzzDiagram(f *testing.F) {
	f.Add([]byte(diagramData("a", "b", "c")), []byte(diagramLayout(`<dgm:alg type="lin"/><dgm:shape/>`+nodeList("rect", ""))))
	f.Add([]byte(diagramData("a", "b")), []byte(diagramLayout(`<dgm:alg type="snake"><dgm:param type="grDir" val="bR"/><dgm:param type="contDir" val="revDir"/></dgm:alg><dgm:shape/>`+nodeList("ellipse", `<dgm:forEach ref="each"/>`))))
	f.Add([]byte(diagramData("a")), []byte(diagramLayout(`<dgm:choose><dgm:if func="maxDepth" op="gte" val="1"><dgm:alg type="hierRoot"/></dgm:if></dgm:choose><dgm:shape/>`+nodeList("conn", `<dgm:alg type="conn"/>`))))
	// The text sizing of hostile definitions: bounds in a cycle, rules of
	// NaN and of a negative floor, ten thousand tied nodes.
	sized := func(root, node string) []byte {
		return []byte(diagramLayout(`<dgm:alg type="lin"/><dgm:shape/>` + root + `<dgm:forEach name="each" axis="ch" ptType="node">` +
			`<dgm:layoutNode name="node"><dgm:alg type="tx"/><dgm:shape type="rect"/>` + node + `</dgm:layoutNode>` +
			`<dgm:layoutNode name="sib"><dgm:alg type="tx"/><dgm:shape type="rect"/></dgm:layoutNode></dgm:forEach>`))
	}
	f.Add([]byte(withPresNames(diagramData("abc", "abcdefghijkl"), 2, "sib")), sized(`<dgm:constrLst>`+
		`<dgm:constr type="primFontSz" for="ch" forName="node" refType="primFontSz" refFor="ch" refForName="sib" op="lte"/>`+
		`<dgm:constr type="primFontSz" for="ch" forName="sib" refType="primFontSz" refFor="ch" refForName="node" op="lte"/></dgm:constrLst>`, ``))
	f.Add([]byte(diagramData("abcdefghijkl")), sized(`<dgm:constrLst><dgm:constr type="primFontSz" for="ch" forName="node" val="65"/></dgm:constrLst>`,
		`<dgm:ruleLst><dgm:rule type="primFontSz" val="NaN"/><dgm:rule type="primFontSz" val="-5"/></dgm:ruleLst>`))
	many := make([]string, 10_000)
	for i := range many {
		many[i] = "word"
	}
	f.Add([]byte(diagramData(many...)), sized(`<dgm:constrLst><dgm:constr type="primFontSz" for="ch" forName="node" op="equ" val="65"/></dgm:constrLst>`,
		`<dgm:ruleLst><dgm:rule type="primFontSz" val="5"/></dgm:ruleLst>`))
	// The snake's numeric parameters and lengths: a fixed break past any
	// count and below one, a balanced grid of ten thousand nodes, a spacing
	// of 1e300 millimetres, a node of no height.
	f.Add([]byte(diagramData("a", "b", "c")), []byte(diagramLayout(blockList(`<dgm:param type="bkpt" val="fixed"/><dgm:param type="bkPtFixedVal" val="99999999999"/>`))))
	f.Add([]byte(diagramData("a", "b", "c")), []byte(diagramLayout(blockList(`<dgm:param type="bkpt" val="fixed"/><dgm:param type="bkPtFixedVal" val="-3"/><dgm:param type="flowDir" val="col"/>`))))
	f.Add([]byte(diagramData(many...)), []byte(diagramLayout(blockList(`<dgm:param type="bkpt" val="bal"/>`))))
	f.Add([]byte(diagramData("a", "b")), []byte(diagramLayout(`<dgm:alg type="snake"/><dgm:shape/><dgm:constrLst><dgm:constr type="sp" val="1e300"/>`+
		`<dgm:constr type="h" for="ch" forName="node" val="0"/></dgm:constrLst>`+nodeList("rect", ""))))
	f.Fuzz(func(t *testing.T, data, layout []byte) {
		dm, err := Parse(context.Background(), data, nil)
		if err != nil {
			return
		}
		ld, err := Parse(context.Background(), layout, nil)
		if err != nil {
			return
		}
		calls := 0
		root := ParseDiagram(dm, ld, nil, nil).LayOut(1_000_000, 500_000, func(s *DiagramShape) bool {
			calls++
			return charFit(s)
		})
		if root == nil || root.W != 1_000_000 {
			t.Fatal("the root is not the diagram's box")
		}
		if calls > maxDiagramFits {
			t.Fatalf("%d measurements, past %d", calls, maxDiagramFits)
		}
	})
}
