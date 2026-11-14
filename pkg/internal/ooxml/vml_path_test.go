package ooxml

import (
	"slices"
	"testing"
)

// vmlPair parses a shape and its shape type for the VML tests.
func vmlPair(t *testing.T, shapeXML, typeXML string) (*Element, *Element) {
	t.Helper()
	root, err := Parse(t.Context(), []byte(`<r xmlns:v="urn:schemas-microsoft-com:vml">`+shapeXML+typeXML+`</r>`), nil)
	if err != nil {
		t.Fatal(err)
	}
	var typ *Element
	if typeXML != "" {
		typ = root.Kids()[1]
	}
	return root.Kids()[0], typ
}

// TestVMLPath_InheritsFromItsType locks ECMA-376 Part 4, 14.1.2.20: a
// shape takes the path, formulas, coordinate space and adjustments of the
// shape type it names, its own adj values overriding position by
// position.
func TestVMLPath_InheritsFromItsType(t *testing.T) {
	t.Parallel()
	sp, typ := vmlPair(t, `<v:shape adj=",7"/>`,
		`<v:shapetype coordsize="21600,21600" adj="1,2,3" path="m@0,l,21600r21600,xe">`+
			`<v:formulas><v:f eqn="val #0"/><v:f eqn="sum @0 0 #1"/></v:formulas>`+
			`<v:path textboxrect="0,10800,21600,21600" limo="5,6"/></v:shapetype>`)
	src, ok := VMLPath(sp, typ)
	if !ok {
		t.Fatal("no path")
	}
	if src.Path != "m@0,l,21600r21600,xe" || len(src.Formulas) != 2 || src.SizeX != 21600 || src.SizeY != 21600 {
		t.Errorf("source %+v", src)
	}
	if !slices.Equal(src.Adjust, []float64{1, 7, 3}) {
		t.Errorf("adj %v, want [1 7 3]", src.Adjust)
	}
	if src.TextBoxRect != "0,10800,21600,21600" || src.LimoX != 5 || src.LimoY != 6 {
		t.Errorf("v:path %q %g %g", src.TextBoxRect, src.LimoX, src.LimoY)
	}
}

// TestVMLPath_OwnPathAndRefusals locks a shape's own path, in its path
// attribute or its v:path v, the default coordinate space of 1000 by
// 1000, and the shapes VMLPath refuses: no path, or a text path whose
// path warps text (14.1.2.22).
func TestVMLPath_OwnPathAndRefusals(t *testing.T) {
	t.Parallel()
	sp, _ := vmlPair(t, `<v:shape><v:path v="m,l10,10e"/></v:shape>`, "")
	if src, ok := VMLPath(sp, nil); !ok || src.Path != "m,l10,10e" || src.SizeX != 1000 || src.OriginX != 0 {
		t.Errorf("own v:path: %+v %v", src, ok)
	}
	sp, _ = vmlPair(t, `<v:shape/>`, "")
	if _, ok := VMLPath(sp, nil); ok {
		t.Error("a shape of no path has one")
	}
	sp, typ := vmlPair(t, `<v:shape><v:textpath on="t" string="A"/></v:shape>`, `<v:shapetype path="m0@0c7200@2,14400@2,21600@0e"/>`)
	if _, ok := VMLPath(sp, typ); ok {
		t.Error("a text path's warp is drawn as an outline")
	}
}
