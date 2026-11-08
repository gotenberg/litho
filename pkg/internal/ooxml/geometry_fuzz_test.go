package ooxml

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/shape"
)

func FuzzParseGeometry(f *testing.F) {
	f.Add([]byte(`<a:custGeom><a:avLst><a:gd name="adj" fmla="val 50000"/></a:avLst><a:gdLst><a:gd name="x1" fmla="*/ w adj 100000"/></a:gdLst><a:rect l="l" t="t" r="x1" b="b"/><a:pathLst><a:path w="10" h="10"><a:moveTo><a:pt x="0" y="0"/></a:moveTo><a:arcTo wR="x1" hR="h" stAng="0" swAng="cd2"/><a:close/></a:path></a:pathLst></a:custGeom>`))
	f.Add([]byte(`<a:prstGeom prst="star5"><a:avLst><a:gd name="adj" fmla="val 1`))
	f.Fuzz(func(t *testing.T, data []byte) {
		root, err := Parse(context.Background(), append(append([]byte(`<a:spPr xmlns:a="`+NSA+`">`), data...), []byte(`</a:spPr>`)...), nil)
		if err != nil {
			return
		}
		g := ParseGeometry(root)
		_ = shape.Check(g)
		r := compose.Rect{X: 1, Y: 2, Width: 100, Height: 50}
		for _, p := range shape.Paths(g, r) {
			if len(p.Ops) > 0 && p.Ops[0].Kind != compose.MoveTo {
				t.Fatal("a path does not start with a move")
			}
		}
		_ = shape.TextRect(g, r)
	})
}
