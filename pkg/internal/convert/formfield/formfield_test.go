package formfield

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/layout"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// mark is a path of the element e at (x, y).
func mark(e *typeset.StructElem, x, y float64) *typeset.Path {
	return &typeset.Path{
		Ops:    []typeset.PathOp{{Kind: typeset.MoveTo, P: [3]typeset.Point{{X: x, Y: y}}}, {Kind: typeset.LineTo, P: [3]typeset.Point{{X: x + 4, Y: y + 4}}}},
		Stroke: &typeset.Stroke{Color: typeset.Black, Width: 1}, Parent: e,
	}
}

func TestAttach(t *testing.T) {
	t.Parallel()
	a, b, c := &typeset.StructElem{Role: "Form"}, &typeset.StructElem{Role: "Form"}, &typeset.StructElem{Role: "Form"}
	p := &typeset.StructElem{Role: "P", Kids: []*typeset.StructElem{a, b, c}}
	root := &typeset.StructElem{Role: "Document", Kids: []*typeset.StructElem{p}}
	border := &typeset.Path{Ops: mark(nil, 10, 10).Ops, Stroke: &typeset.Stroke{Color: typeset.Black, Width: 1}}
	page1 := &typeset.Page{Width: 200, Height: 200, Content: []typeset.Fragment{
		border,
		&typeset.Group{Transform: typeset.Matrix{A: 1, D: 1, E: 10, F: 10}, Content: []typeset.Fragment{mark(a, 2, 2)}},
		mark(b, 100, 100),
	}}
	page2 := &typeset.Page{Width: 200, Height: 200, Content: []typeset.Fragment{mark(b, 10, 10), mark(c, 50, 50)}}
	box := typeset.Rect{X: 10, Y: 10, Width: 20, Height: 20}
	res := &layout.Result{
		Pages:     []*typeset.Page{page1, page2},
		Structure: root,
		Anchors: map[string]typeset.Dest{
			NodeID(0): {Page: 1, X: 10, Y: 10},
			NodeID(1): {Page: 1, X: 100, Y: 100},
		},
		Extents: map[string]typeset.Rect{NodeID(0): box, NodeID(1): {X: 100, Y: 100, Width: 10, Height: 10}},
	}
	controls := []*Control{
		{ID: NodeID(0), Box: true, Field: typeset.Field{Kind: typeset.FieldCheckBox, Name: "a", Checked: true}},
		{ID: NodeID(1), Box: true, Field: typeset.Field{Kind: typeset.FieldText, Name: "spans pages"}},
		{ID: NodeID(2), Box: true, Field: typeset.Field{Kind: typeset.FieldText, Name: "never laid out"}},
	}
	if missed, err := Attach(res, controls); err != nil || missed != 2 {
		t.Errorf("Attach missed %d controls (%v), want 2", missed, err)
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
	if g, ok := f.Appearance[0].(*typeset.Group); !ok || g.Transform.E != 10 || len(g.Content) != 1 {
		t.Errorf("appearance = %+v", f.Appearance)
	}
	if len(page1.Content) != 2 || page1.Content[0] != border {
		t.Errorf("page 1 content = %+v", page1.Content)
	}
	if len(page2.Fields) != 0 || len(page2.Content) != 2 {
		t.Errorf("page 2 changed: %d fields, %d fragments", len(page2.Fields), len(page2.Content))
	}
}

func TestPrinted(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		f    typeset.Field
		want typeset.PrintField
	}{
		{typeset.Field{Kind: typeset.FieldText, Name: "n", Description: "Your name"}, typeset.PrintField{Role: typeset.PrintText, Desc: "Your name"}},
		{typeset.Field{Kind: typeset.FieldCombo, Name: "city"}, typeset.PrintField{Role: typeset.PrintText, Desc: "city"}},
		{typeset.Field{Kind: typeset.FieldList}, typeset.PrintField{Role: typeset.PrintList}},
		{typeset.Field{Kind: typeset.FieldCheckBox, Name: "a", Checked: true}, typeset.PrintField{Role: typeset.PrintCheckBox, Checked: typeset.CheckOn, Desc: "a"}},
		{typeset.Field{Kind: typeset.FieldRadio, Name: "c"}, typeset.PrintField{Role: typeset.PrintRadio, Checked: typeset.CheckOff, Desc: "c"}},
	} {
		if got := Printed(&tc.f); got != tc.want {
			t.Errorf("Printed(%+v) = %+v, want %+v", tc.f, got, tc.want)
		}
	}
}

func TestClip(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("é", typeset.MaxFieldText)
	got := Clip(long)
	if len(got) > typeset.MaxFieldText || !utf8.ValidString(got) || len(got) < typeset.MaxFieldText-1 {
		t.Errorf("Clip kept %d bytes, valid %v", len(got), utf8.ValidString(got))
	}
	if got := Clip("a\xffb"); got != "a�b" {
		t.Errorf("Clip(invalid) = %q", got)
	}
}
