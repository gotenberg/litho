package formfield

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/layout"
)

// mark is a path of the element e at (x, y).
func mark(e *compose.StructElem, x, y float64) *compose.Path {
	return &compose.Path{Ops: []compose.PathOp{{Kind: compose.MoveTo, P: [3]compose.Point{{X: x, Y: y}}}, {Kind: compose.LineTo, P: [3]compose.Point{{X: x + 4, Y: y + 4}}}},
		Stroke: &compose.Stroke{Color: compose.Black, Width: 1}, Parent: e}
}

func TestAttach(t *testing.T) {
	t.Parallel()
	a, b, c := &compose.StructElem{Role: "Form"}, &compose.StructElem{Role: "Form"}, &compose.StructElem{Role: "Form"}
	p := &compose.StructElem{Role: "P", Kids: []*compose.StructElem{a, b, c}}
	root := &compose.StructElem{Role: "Document", Kids: []*compose.StructElem{p}}
	border := &compose.Path{Ops: mark(nil, 10, 10).Ops, Stroke: &compose.Stroke{Color: compose.Black, Width: 1}}
	page1 := &compose.Page{Width: 200, Height: 200, Content: []compose.Fragment{
		border,
		&compose.Group{Transform: compose.Matrix{A: 1, D: 1, E: 10, F: 10}, Content: []compose.Fragment{mark(a, 2, 2)}},
		mark(b, 100, 100),
	}}
	page2 := &compose.Page{Width: 200, Height: 200, Content: []compose.Fragment{mark(b, 10, 10), mark(c, 50, 50)}}
	box := compose.Rect{X: 10, Y: 10, Width: 20, Height: 20}
	res := &layout.Result{
		Pages:     []*compose.Page{page1, page2},
		Structure: root,
		Anchors: map[string]compose.Dest{
			NodeID(0): {Page: 1, X: 10, Y: 10},
			NodeID(1): {Page: 1, X: 100, Y: 100},
		},
		Extents: map[string]compose.Rect{NodeID(0): box, NodeID(1): {X: 100, Y: 100, Width: 10, Height: 10}},
	}
	controls := []*Control{
		{ID: NodeID(0), Box: true, Field: compose.Field{Kind: compose.FieldCheckBox, Name: "a", Checked: true}},
		{ID: NodeID(1), Box: true, Field: compose.Field{Kind: compose.FieldText, Name: "spans pages"}},
		{ID: NodeID(2), Box: true, Field: compose.Field{Kind: compose.FieldText, Name: "never laid out"}},
	}
	if missed := Attach(res, controls); missed != 2 {
		t.Errorf("Attach missed %d controls, want 2", missed)
	}
	if len(page1.Fields) != 1 {
		t.Fatalf("page 1 has %d fields, want 1", len(page1.Fields))
	}
	f := page1.Fields[0]
	if f.Name != "a" || f.Rect != box || f.Parent != a || len(f.Appearance) != 1 || f.OffAppearance != nil {
		t.Errorf("field = %+v", f)
	}
	// The mark moved into the field inside a copy of its group; the
	// border and the other control's content stay.
	if g, ok := f.Appearance[0].(*compose.Group); !ok || g.Transform.E != 10 || len(g.Content) != 1 {
		t.Errorf("appearance = %+v", f.Appearance)
	}
	if len(page1.Content) != 2 || page1.Content[0] != border {
		t.Errorf("page 1 content = %+v", page1.Content)
	}
	if len(page2.Fields) != 0 || len(page2.Content) != 2 {
		t.Errorf("page 2 changed: %d fields, %d fragments", len(page2.Fields), len(page2.Content))
	}
}
