package ooxml

import (
	"context"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/shape"
)

// spPrOf parses a shape properties element holding body.
func spPrOf(t testing.TB, body string) *Element {
	t.Helper()
	root, err := Parse(context.Background(), []byte(`<a:spPr xmlns:a="`+NSA+`">`+body+`</a:spPr>`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !root.Is(NSA, "spPr") {
		root = root.Child(NSA, "spPr")
	}
	return root
}

func TestParseGeometry_Preset(t *testing.T) {
	tests := []struct {
		body   string
		preset string
		adjust []shape.Adjust
	}{
		{`<a:prstGeom prst="roundRect"><a:avLst><a:gd name="adj" fmla="val 25000"/></a:avLst></a:prstGeom>`, "roundRect", []shape.Adjust{{Name: "adj", Value: 25000}}},
		{`<a:prstGeom prst="star5"><a:avLst/></a:prstGeom>`, "star5", nil},
		{`<a:prstGeom prst="rightArrow"><a:avLst><a:gd name="adj1" fmla="*/ w 1 2"/><a:gd name="adj2" fmla="val -5"/></a:avLst></a:prstGeom>`, "rightArrow", []shape.Adjust{{Name: "adj2", Value: -5}}},
	}
	for _, tt := range tests {
		g := ParseGeometry(spPrOf(t, tt.body))
		if g == nil || g.Preset != tt.preset || g.Custom != nil || len(g.Adjust) != len(tt.adjust) {
			t.Fatalf("%s: %+v", tt.body, g)
		}
		for i := range tt.adjust {
			if g.Adjust[i] != tt.adjust[i] {
				t.Errorf("%s: adjust %+v", tt.body, g.Adjust)
			}
		}
	}
	if g := ParseGeometry(spPrOf(t, `<a:noFill/>`)); g != nil {
		t.Errorf("no geometry: %+v", g)
	}
}

func TestParseGeometry_Custom(t *testing.T) {
	body := `<a:custGeom><a:avLst><a:gd name="adj" fmla="val 50000"/></a:avLst>` +
		`<a:gdLst><a:gd name="x1" fmla="*/ w adj 100000"/><a:gd name="y1" fmla="+- b 0 later"/><a:gd name="later" fmla="val 0"/></a:gdLst>` +
		`<a:ahLst/><a:cxnLst/><a:rect l="l" t="t" r="x1" b="b"/>` +
		`<a:pathLst><a:path w="0" h="0" fill="darken" stroke="0"><a:moveTo><a:pt x="0" y="0"/></a:moveTo><a:lnTo><a:pt x="x1" y="y1"/></a:lnTo>` +
		`<a:arcTo wR="x1" hR="y1" stAng="0" swAng="cd4"/><a:quadBezTo><a:pt x="0" y="0"/><a:pt x="w" y="h"/></a:quadBezTo>` +
		`<a:cubicBezTo><a:pt x="0" y="0"/><a:pt x="0" y="0"/><a:pt x="0" y="h"/></a:cubicBezTo><a:close/></a:path>` +
		`<a:path w="10" h="10" fill="none"><a:moveTo><a:pt x="5" y="5"/></a:moveTo><a:lnTo><a:pt x="10" y="10"/></a:lnTo></a:path></a:pathLst></a:custGeom>`
	g := ParseGeometry(spPrOf(t, body))
	if g == nil || g.Custom == nil {
		t.Fatalf("%+v", g)
	}
	if err := shape.Check(g); err != nil {
		t.Fatal(err)
	}
	c := g.Custom
	if len(c.Adjust) != 1 || len(c.Guides) != 3 || len(c.Paths) != 2 || len(c.Paths[0].Cmds) != 6 {
		t.Fatalf("%+v", c)
	}
	if p := c.Paths[0]; p.Fill != shape.FillDarken || p.Stroke {
		t.Errorf("path 0: %+v", p)
	}
	r := compose.Rect{X: 0, Y: 0, Width: 200, Height: 100}
	paths := shape.Paths(g, r)
	if got := paths[0].Ops[1].P[0]; math.Abs(got.X-100) > 1e-9 || math.Abs(got.Y-100) > 1e-9 {
		t.Errorf("line to %v, want (100, 100)", got)
	}
	if got := paths[1].Ops[1].P[0]; got != (compose.Point{X: 200, Y: 100}) {
		t.Errorf("path space end %v", got)
	}
	if tr := shape.TextRect(g, r); tr.Width != 100 || tr.Height != 100 {
		t.Errorf("text rectangle %v", tr)
	}
	// Overriding the adjustment moves x1.
	g.Adjust = []shape.Adjust{{Name: "adj", Value: 25000}}
	if got := shape.Paths(g, r)[0].Ops[1].P[0]; math.Abs(got.X-50) > 1e-9 {
		t.Errorf("adjusted line to %v", got)
	}
}

func TestParseGeometry_Unresolved(t *testing.T) {
	g := ParseGeometry(spPrOf(t, `<a:custGeom><a:gdLst><a:gd name="x" fmla="+- nowhere 0 0"/><a:gd name="y" fmla="bogus 1"/></a:gdLst><a:pathLst><a:path><a:moveTo><a:pt x="x" y="0"/></a:moveTo></a:path></a:pathLst></a:custGeom>`))
	if err := shape.Check(g); err == nil {
		t.Error("an unknown name passed")
	}
}
