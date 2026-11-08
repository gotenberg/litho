package ooxml

import (
	"context"
	"testing"
)

func FuzzDiagram(f *testing.F) {
	f.Add([]byte(diagramData("a", "b", "c")), []byte(diagramLayout(`<dgm:alg type="lin"/><dgm:shape/>`+nodeList("rect", ""))))
	f.Add([]byte(diagramData("a", "b")), []byte(diagramLayout(`<dgm:alg type="snake"><dgm:param type="grDir" val="bR"/><dgm:param type="contDir" val="revDir"/></dgm:alg><dgm:shape/>`+nodeList("ellipse", `<dgm:forEach ref="each"/>`))))
	f.Add([]byte(diagramData("a")), []byte(diagramLayout(`<dgm:choose><dgm:if func="maxDepth" op="gte" val="1"><dgm:alg type="hierRoot"/></dgm:if></dgm:choose><dgm:shape/>`+nodeList("conn", `<dgm:alg type="conn"/>`))))
	f.Fuzz(func(t *testing.T, data, layout []byte) {
		dm, err := Parse(context.Background(), data, nil)
		if err != nil {
			return
		}
		ld, err := Parse(context.Background(), layout, nil)
		if err != nil {
			return
		}
		root := ParseDiagram(dm, ld, nil, nil).LayOut(1_000_000, 500_000)
		if root == nil || root.W != 1_000_000 {
			t.Fatal("the root is not the diagram's box")
		}
	})
}
