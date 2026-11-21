package typeset_test

import (
	"bytes"
	"errors"
	"image/color"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// checker returns a path over r filled with a pattern of 10-point cells
// whose top-left quarter is red, the first cell's corner at (x0, y0).
func checker(r typeset.Rect, x0, y0 float64) *typeset.Path {
	return &typeset.Path{Ops: rect(r.X, r.Y, r.Width, r.Height), Fill: &typeset.Fill{Tiling: &typeset.Tiling{
		Content:   []typeset.Fragment{&typeset.Path{Ops: rect(0, 0, 5, 5), Fill: &typeset.Fill{Color: red}}},
		Width:     10,
		Height:    10,
		XStep:     10,
		YStep:     10,
		Transform: typeset.Matrix{A: 1, D: 1, E: x0, F: y0},
	}}}
}

func TestWriteTiling_RepeatsTheCellWhereTheTransformPlacesIt(t *testing.T) {
	t.Parallel()
	page := &typeset.Page{Width: 200, Height: 200, Content: []typeset.Fragment{
		checker(typeset.Rect{X: 20, Y: 20, Width: 60, Height: 40}, 20, 20),
		// A group's transform moves the pattern with the path: the
		// pattern's matrix carries the CTM.
		&typeset.Group{Transform: typeset.Matrix{A: 2, D: 2, E: 100, F: 100}, Content: []typeset.Fragment{
			checker(typeset.Rect{Width: 40, Height: 40}, 0, 0),
		}},
		// So does a transparency group, written as a form XObject.
		&typeset.Group{Transform: typeset.Matrix{A: 1, D: 1, F: 120}, Opacity: 0.999, Transparency: &typeset.TransparencyGroup{}, Content: []typeset.Fragment{
			checker(typeset.Rect{X: 20, Width: 40, Height: 40}, 20, 0),
		}},
	}}
	doc := &typeset.Document{Pages: []*typeset.Page{page}}
	if err := doc.Validate(t.Context()); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	out := write(t, doc)
	img := render(t, open(t, out), 1)
	white := color.NRGBA{R: 255, G: 255, B: 255}
	for _, c := range []struct {
		x, y int
		want color.NRGBA
		what string
	}{
		{22, 22, color.NRGBA{R: 255}, "first cell"},
		{27, 22, white, "first cell's empty quarter"},
		{72, 52, color.NRGBA{R: 255}, "last cell"},
		{77, 57, white, "last cell's empty quarter"},
		{12, 12, white, "outside the path"},
		{85, 22, white, "past the path's right edge"},
		{104, 104, color.NRGBA{R: 255}, "scaled cell"},
		{114, 104, white, "scaled cell's empty quarter"},
		{124, 124, color.NRGBA{R: 255}, "scaled second cell"},
		{22, 122, color.NRGBA{R: 255}, "cell in a form"},
		{27, 127, white, "empty quarter in a form"},
		{52, 152, color.NRGBA{R: 255}, "last cell in a form"},
	} {
		if !near(img, c.x, c.y, c.want, 40) {
			t.Errorf("%s: pixel (%d, %d) = %v, want %v", c.what, c.x, c.y, img.At(c.x, c.y), c.want)
		}
	}
	if n := bytes.Count(out, []byte("/PatternType 1")); n != 3 {
		t.Errorf("%d patterns, want one a fill", n)
	}
	if n := bytes.Count(inflated(t, out), []byte(" re")) + bytes.Count(inflated(t, out), []byte(" l\n")); n == 0 {
		t.Error("no cell content written")
	}
}

func TestWriteTiling_PaintsTheCellOnceForEveryTile(t *testing.T) {
	t.Parallel()
	small := &typeset.Page{Width: 200, Height: 200, Content: []typeset.Fragment{checker(typeset.Rect{Width: 20, Height: 20}, 0, 0)}}
	large := &typeset.Page{Width: 200, Height: 200, Content: []typeset.Fragment{checker(typeset.Rect{Width: 200, Height: 200}, 0, 0)}}
	a := write(t, &typeset.Document{Pages: []*typeset.Page{small}})
	b := write(t, &typeset.Document{Pages: []*typeset.Page{large}})
	// The two files differ by the digits of the path's corners alone.
	if len(b) > len(a)+16 {
		t.Errorf("400 tiles write %d bytes, 4 tiles %d", len(b), len(a))
	}
}

func TestValidate_RefusesABadTiling(t *testing.T) {
	t.Parallel()
	elem := &typeset.StructElem{Role: "Figure"}
	for name, mod := range map[string]func(*typeset.Path){
		"zero step":          func(p *typeset.Path) { p.Fill.Tiling.XStep = 0 },
		"negative height":    func(p *typeset.Path) { p.Fill.Tiling.Height = -1 },
		"singular transform": func(p *typeset.Path) { p.Fill.Tiling.Transform = typeset.Matrix{A: 1} },
		"shading too": func(p *typeset.Path) {
			p.Fill.Shading = &typeset.Shading{To: typeset.Point{X: 1}, Stops: []typeset.ColorStop{{Color: red}, {Offset: 1, Color: blue}}}
		},
		"tagged content": func(p *typeset.Path) {
			p.Fill.Tiling.Content[0].(*typeset.Path).Parent = elem
		},
		"bad content": func(p *typeset.Path) {
			p.Fill.Tiling.Content[0].(*typeset.Path).Fill.Color = typeset.Color{R: 2, A: 1}
		},
	} {
		p := checker(typeset.Rect{Width: 20, Height: 20}, 0, 0)
		mod(p)
		doc := &typeset.Document{Pages: []*typeset.Page{{Width: 100, Height: 100, Content: []typeset.Fragment{p}}}, Structure: &typeset.StructElem{Role: "Document", Kids: []*typeset.StructElem{elem}}}
		err := doc.Validate(t.Context())
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		if !errors.Is(err, typeset.ErrInvalidPath) && !errors.Is(err, typeset.ErrInvalidGroup) && !errors.Is(err, typeset.ErrInvalidColor) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestCountGlyphs_CountsPatternCells(t *testing.T) {
	t.Parallel()
	face := buildFace("F", 1)
	p := checker(typeset.Rect{Width: 20, Height: 20}, 0, 0)
	p.Fill.Tiling.Content = append(p.Fill.Tiling.Content, run(t, face, 4, typeset.Point{Y: 4}, "ab"))
	if n := typeset.CountGlyphs(&typeset.Page{Content: []typeset.Fragment{p}}); n != 2 {
		t.Errorf("CountGlyphs = %d, want the 2 of the cell", n)
	}
}

// TestWriteTiling_CellSaysNoTransparencyState fills a purple square with a
// green picture under the Multiply blend mode, one cell covering the fill.
// ISO 32000-1, 11.6.7 starts a pattern cell's blend mode, soft mask and
// alpha constant at their defaults, so the cell's stream sets none of them:
// a reader that runs a lone cell in the painting state then keeps the
// fill's blend mode, and the picture multiplies over the purple.
func TestWriteTiling_CellSaysNoTransparencyState(t *testing.T) {
	t.Parallel()
	green := typeset.Color{G: 1, A: 1}
	cell := &typeset.Tiling{
		Content: []typeset.Fragment{&typeset.Path{Ops: rect(0, 0, 100, 100), Fill: &typeset.Fill{Color: green}}},
		Width:   100, Height: 100, XStep: 100, YStep: 100,
		Transform: typeset.Matrix{A: 1, D: 1},
	}
	page := &typeset.Page{Width: 100, Height: 100, Content: []typeset.Fragment{
		&typeset.Path{Ops: rect(0, 0, 100, 100), Fill: &typeset.Fill{Color: typeset.Color{R: 0.5, G: 0.2, B: 0.9, A: 1}}},
		&typeset.Group{Transform: typeset.Matrix{A: 1, D: 1}, Blend: typeset.BlendMultiply, Content: []typeset.Fragment{
			&typeset.Path{Ops: rect(0, 0, 100, 100), Fill: &typeset.Fill{Tiling: cell}},
		}},
	}}
	doc := &typeset.Document{Pages: []*typeset.Page{page}}
	if err := doc.Validate(t.Context()); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	out := write(t, doc)
	streams := inflated(t, out)
	if !bytes.Contains(streams, []byte("/Pattern cs")) {
		t.Fatal("no pattern fill written")
	}
	if !bytes.Contains(streams, []byte("0 1 0 rg")) {
		t.Fatal("no cell content written")
	}
	if bytes.Contains(streams, []byte(" gs\n0 1 0 rg")) {
		t.Error("the cell's stream sets a graphics state before its content")
	}
	img := render(t, open(t, out), 1)
	if want := (color.NRGBA{R: 0, G: 51, B: 0}); !near(img, 50, 50, want, 3) {
		t.Errorf("pixel (50, 50) = %v, want the green multiplied over the purple, %v", img.At(50, 50), want)
	}
}
