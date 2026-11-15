package docx

import (
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// TestVMLTextPath checks Word's fit of a text path to its shape: an
// untrimmed one at the shape's height, stretched to its width, its
// baseline at 0.776 of the height; a trimmed one with its ink stretched
// to the box; the shadow a copy at its offset.
func TestVMLTextPath(t *testing.T) {
	t.Parallel()
	shape := func(trim string) string {
		return `<w:p><w:r><w:pict><v:shapetype id="_x0000_t136" coordsize="21600,21600" o:spt="136"><v:textpath on="t" fitshape="t"/></v:shapetype>` +
			`<v:shape id="w" type="#_x0000_t136" style="width:200pt;height:50pt" fillcolor="red" stroked="f"><v:shadow on="t" offset="3pt,3pt"/>` +
			`<v:textpath style="font-family:&quot;Test Sans&quot;" ` + trim + ` string="AB"/></v:shape></w:pict></w:r></w:p>`
	}
	paths := func(trim string) []*typeset.Path {
		t.Helper()
		pics := find(build(t, shape(trim), nil).root, func(n *layout.Node) bool { return n.Kind == layout.Replaced })
		if len(pics) != 1 || pics[0].Alt != "AB" {
			t.Fatalf("%d replaced boxes, want the text path", len(pics))
		}
		var out []*typeset.Path
		for _, f := range pics[0].Picture.Vector(t.Context(), typeset.Rect{Width: 200, Height: 50}) {
			if p, ok := f.(*typeset.Path); ok {
				out = append(out, p)
			}
		}
		if len(out) != 2 {
			t.Fatalf("%d paths, want the shadow and the text", len(out))
		}
		return out
	}
	near := func(a, b float64) bool { return math.Abs(a-b) < 0.01 }
	plain := paths("")
	x0, _, x1, y1, _ := opsBounds(plain[1].Ops)
	if x0 < 0 || x1 > 200 || x1-x0 < 150 || !near(y1, 0.776*50) {
		t.Errorf("untrimmed ink x %.2f..%.2f, bottom %.2f, want within the width, on the baseline %.2f", x0, x1, y1, 0.776*50)
	}
	if sx0, _, _, sy1, _ := opsBounds(plain[0].Ops); !near(sx0, x0+3) || !near(sy1, y1+3) || plain[0].Fill.Color.R != 0.5 {
		t.Errorf("shadow at %.2f, %.2f, colour %v", sx0, sy1, plain[0].Fill.Color)
	}
	if plain[1].Fill.Color != (typeset.Color{R: 1, A: 1}) {
		t.Errorf("text fill %v, want red", plain[1].Fill.Color)
	}
	x0, y0, x1, y1, _ := opsBounds(paths(`trim="t"`)[1].Ops)
	if !near(x0, 0) || !near(y0, 0) || !near(x1, 200) || !near(y1, 50) {
		t.Errorf("trimmed ink %.2f,%.2f..%.2f,%.2f, want the box", x0, y0, x1, y1)
	}
}

// TestVMLTextPath_FollowsItsShapeTypePath checks that a text path of a
// shape type other than plain text is set along the type's path: two
// subpaths an envelope its ink fills, the string not kept as text, as
// Word writes none.
func TestVMLTextPath_FollowsItsShapeTypePath(t *testing.T) {
	t.Parallel()
	// A path that rises from the left to the right edge: the top edge
	// from (0, 10800) to (21600, 0), the bottom one the box's.
	src := `<w:p><w:r><w:pict><v:shapetype id="_x0000_t172" coordsize="21600,21600" o:spt="172" path="m0,10800l21600,0m0,21600l21600,21600e"><v:path textpathok="t"/><v:textpath on="t" fitshape="t"/></v:shapetype>` +
		`<v:shape id="w" type="#_x0000_t172" style="width:200pt;height:50pt" fillcolor="red" stroked="f">` +
		`<v:textpath style="font-family:&quot;Test Sans&quot;" string="AB"/></v:shape></w:pict></w:r></w:p>`
	pics := find(build(t, src, nil).root, func(n *layout.Node) bool { return n.Kind == layout.Replaced })
	if len(pics) != 1 {
		t.Fatalf("%d replaced boxes, want the text path", len(pics))
	}
	var hidden bool
	x0, y0, x1, y1 := math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
	var walk func([]typeset.Fragment)
	walk = func(fs []typeset.Fragment) {
		for _, f := range fs {
			switch v := f.(type) {
			case *typeset.Path:
				_, oy0, _, oy1, _ := opsBounds(v.Ops)
				ox0, _, ox1, _, _ := opsBounds(v.Ops)
				x0, y0, x1, y1 = min(x0, ox0), min(y0, oy0), max(x1, ox1), max(y1, oy1)
			case *typeset.Text:
				hidden = hidden || v.Invisible
			case *typeset.Group:
				walk(v.Content)
			}
		}
	}
	walk(pics[0].Picture.Vector(t.Context(), typeset.Rect{Width: 200, Height: 50}))
	if hidden {
		t.Error("the string is kept as text")
	}
	if math.Abs(x0) > 0.01 || math.Abs(x1-200) > 0.01 || math.Abs(y1-50) > 0.01 || math.Abs(y0) > 0.5 {
		t.Errorf("outlines span (%.2f, %.2f)-(%.2f, %.2f), want the envelope from the top right corner to the bottom", x0, y0, x1, y1)
	}
}
