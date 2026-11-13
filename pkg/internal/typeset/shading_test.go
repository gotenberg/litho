package typeset_test

import (
	"bytes"
	"image/color"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

var (
	red  = typeset.Color{R: 1, A: 1}
	blue = typeset.Color{B: 1, A: 1}
)

func TestWriteShading_PaintsAxialAndRadial(t *testing.T) {
	t.Parallel()
	axial := &typeset.Shading{
		From: typeset.Point{X: 10}, To: typeset.Point{X: 90},
		Stops: []typeset.ColorStop{{Color: red}, {Offset: 0.5, Color: red}, {Offset: 0.5, Color: blue}, {Offset: 1, Color: blue}},
	}
	radial := &typeset.Shading{
		Kind: typeset.Radial, From: typeset.Point{X: 150, Y: 50}, To: typeset.Point{X: 150, Y: 50}, ToRadius: 40,
		Stops: []typeset.ColorStop{{Color: red}, {Offset: 0.5, Color: red}, {Offset: 0.5, Color: blue}, {Offset: 1, Color: blue}},
	}
	// An ellipse: the circle of radius 40 squashed to half its height
	// around y = 150.
	ellipse := &typeset.Shading{
		Kind: typeset.Radial, From: typeset.Point{X: 150, Y: 150}, To: typeset.Point{X: 150, Y: 150}, ToRadius: 40,
		Stops:     []typeset.ColorStop{{Color: red}, {Offset: 0.5, Color: red}, {Offset: 0.5, Color: blue}, {Offset: 1, Color: blue}},
		Transform: typeset.Matrix{A: 1, D: 0.5, F: 75},
	}
	page := &typeset.Page{Width: 200, Height: 200, Content: []typeset.Fragment{
		&typeset.Path{Ops: rect(0, 0, 100, 100), Fill: &typeset.Fill{Shading: axial}},
		&typeset.Path{Ops: rect(100, 0, 100, 100), Fill: &typeset.Fill{Shading: radial}},
		&typeset.Path{Ops: rect(100, 100, 100, 100), Fill: &typeset.Fill{Shading: ellipse}},
		&typeset.Path{Ops: rect(0, 100, 100, 100), Fill: &typeset.Fill{Shading: axial}, Stroke: &typeset.Stroke{Color: typeset.Black, Width: 4}},
	}}
	doc := &typeset.Document{Pages: []*typeset.Page{page}}
	if err := doc.Validate(t.Context()); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	out := write(t, doc)
	img := render(t, open(t, out), 1)
	for _, c := range []struct {
		x, y int
		want color.NRGBA
		what string
	}{
		{2, 50, color.NRGBA{R: 255}, "extended before the axis"},
		{40, 50, color.NRGBA{R: 255}, "first half of the axis"},
		{60, 50, color.NRGBA{B: 255}, "second half of the axis"},
		{98, 50, color.NRGBA{B: 255}, "extended after the axis"},
		{150, 50, color.NRGBA{R: 255}, "radial centre"},
		{150, 85, color.NRGBA{B: 255}, "radial outer ring"},
		{198, 98, color.NRGBA{B: 255}, "radial extended past the end circle"},
		{150, 136, color.NRGBA{B: 255}, "ellipse squashed vertically"},
		{180, 150, color.NRGBA{B: 255}, "ellipse keeps its width"},
		{168, 150, color.NRGBA{R: 255}, "ellipse inner half"},
		{40, 150, color.NRGBA{R: 255}, "stroked shading fill"},
		{1, 150, color.NRGBA{}, "stroke over the shading"},
	} {
		if !near(img, c.x, c.y, c.want, 40) {
			t.Errorf("%s pixel (%d, %d) = %v, want %v", c.what, c.x, c.y, img.At(c.x, c.y), c.want)
		}
	}
	// One shading shared by two paths is one object; a hard edge is a
	// stitching bound, not a zero-width segment.
	if n := bytes.Count(out, []byte("/ShadingType 2")); n != 1 {
		t.Errorf("%d axial shading objects, want 1", n)
	}
	if !bytes.Contains(out, []byte("/Bounds [0.5]")) {
		t.Errorf("no single bound at 0.5 in\n%s", out)
	}
}

func TestWriteShading_TranslucentStopsMask(t *testing.T) {
	t.Parallel()
	sh := &typeset.Shading{
		From: typeset.Point{X: 0}, To: typeset.Point{X: 100},
		Stops: []typeset.ColorStop{{Color: typeset.Color{R: 1, A: 0}}, {Offset: 1, Color: red}},
	}
	page := &typeset.Page{Width: 100, Height: 50, Content: []typeset.Fragment{
		&typeset.Path{Ops: rect(0, 0, 100, 50), Fill: &typeset.Fill{Color: blue}},
		&typeset.Group{Opacity: 0.5, Content: []typeset.Fragment{
			&typeset.Path{Ops: rect(0, 0, 100, 50), Fill: &typeset.Fill{Shading: sh}},
		}},
	}}
	out := write(t, &typeset.Document{Pages: []*typeset.Page{page}})
	img := render(t, open(t, out), 1)
	for _, c := range []struct {
		x, y int
		want color.NRGBA
		what string
	}{
		{2, 25, color.NRGBA{B: 255}, "transparent end shows the backdrop"},
		{50, 25, color.NRGBA{R: 64, B: 191}, "quarter red: half alpha under half opacity"},
		{98, 25, color.NRGBA{R: 127, B: 128}, "opaque end at half opacity"},
	} {
		if !near(img, c.x, c.y, c.want, 24) {
			t.Errorf("%s pixel (%d, %d) = %v, want %v", c.what, c.x, c.y, img.At(c.x, c.y), c.want)
		}
	}
	if !bytes.Contains(out, []byte("/Luminosity")) {
		t.Error("no luminosity soft mask written")
	}
}

func TestWriteShading_ManyStops(t *testing.T) {
	t.Parallel()
	stops := make([]typeset.ColorStop, 0, 1024)
	for i := range 1024 {
		c := red
		if i%4 >= 2 {
			c = blue
		}
		stops = append(stops, typeset.ColorStop{Offset: float64(i) / 1023, Color: c})
	}
	sh := &typeset.Shading{To: typeset.Point{X: 100}, Stops: stops}
	page := &typeset.Page{Width: 100, Height: 10, Content: []typeset.Fragment{
		&typeset.Path{Ops: rect(0, 0, 100, 10), Fill: &typeset.Fill{Shading: sh}},
	}}
	render(t, open(t, write(t, &typeset.Document{Pages: []*typeset.Page{page}})), 1)
}

// TestWriteShading_PaintsConic locks the conic shading: a type 1
// shading whose calculator function takes the angle clockwise from the
// ray toward To, here up from the centre of a square, a translucent stop
// painting through the same function in grey.
func TestWriteShading_PaintsConic(t *testing.T) {
	t.Parallel()
	green := typeset.Color{G: 1, A: 1}
	conic := &typeset.Shading{
		Kind: typeset.Conic, From: typeset.Point{X: 50, Y: 50}, To: typeset.Point{X: 50, Y: 40},
		Stops: []typeset.ColorStop{{Color: red}, {Offset: 0.25, Color: red}, {Offset: 0.25, Color: blue}, {Offset: 0.5, Color: blue}, {Offset: 0.5, Color: green}, {Offset: 1, Color: typeset.Color{G: 1}}},
	}
	page := &typeset.Page{Width: 100, Height: 100, Content: []typeset.Fragment{
		&typeset.Path{Ops: rect(0, 0, 100, 100), Fill: &typeset.Fill{Color: typeset.Black}},
		&typeset.Path{Ops: rect(0, 0, 100, 100), Fill: &typeset.Fill{Shading: conic}},
	}}
	doc := &typeset.Document{Pages: []*typeset.Page{page}}
	if err := doc.Validate(t.Context()); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	out := write(t, doc)
	img := render(t, open(t, out), 1)
	for _, c := range []struct {
		x, y int
		want color.NRGBA
		what string
	}{
		{60, 10, color.NRGBA{R: 255}, "first quarter, clockwise from up"},
		{90, 60, color.NRGBA{B: 255}, "second quarter"},
		{40, 90, color.NRGBA{G: 255}, "third quarter, green and opaque"},
		{10, 15, color.NRGBA{G: 69, A: 255}, "at 311 degrees, green at 27% over black"},
	} {
		if !near(img, c.x, c.y, c.want, 48) {
			t.Errorf("%s pixel (%d, %d) = %v, want %v", c.what, c.x, c.y, img.At(c.x, c.y), c.want)
		}
	}
	if !bytes.Contains(out, []byte("/ShadingType 1")) || !bytes.Contains(out, []byte("/FunctionType 4")) || !bytes.Contains(out, []byte("{exch atan 360 div")) {
		t.Errorf("no function-based shading with a calculator function in\n%s", out)
	}
	if !bytes.Contains(out, []byte("/Luminosity")) {
		t.Error("no luminosity soft mask for the translucent stop")
	}
}
