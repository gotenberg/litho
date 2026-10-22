package raster

import (
	"image"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/render/geom"
	"github.com/gotenberg/litho/pkg/internal/render/surface"
)

// joined returns the subpaths of every path as one path.
func joined(paths ...geom.Path) geom.Path {
	var out geom.Path
	for _, p := range paths {
		out.Ops = append(out.Ops, p.Ops...)
		out.Pts = append(out.Pts, p.Pts...)
	}
	return out
}

// zigzag returns a closed path whose edges put far more vertices in each row
// than [maxRowVertexCuts] keeps.
func zigzag(x0, y0, w, h float64, teeth int) geom.Path {
	pts := []geom.Point{pt(x0, y0+h)}
	for i := range teeth {
		x := x0 + w*float64(i)/float64(teeth)
		pts = append(pts, pt(x, y0+float64(i%3)*0.3), pt(x+w/float64(2*teeth), y0+h*0.7+float64(i%5)*0.1))
	}
	pts = append(pts, pt(x0+w, y0+h))
	return polygon(pts...)
}

func TestShapeCoverageInMatchesFill(t *testing.T) {
	const w, h = 97, 61
	clip, err := surface.NewMask(w, h, w*h)
	if err != nil {
		t.Fatal(err)
	}
	for i := range clip.A {
		clip.A[i] = float32(i%7) / 6
	}
	shapes := map[string]struct {
		path geom.Path
		rule FillRule
	}{
		"star even-odd":  {star(48, 30, 28), EvenOdd},
		"star non-zero":  {star(48, 30, 28), NonZero},
		"circle":         {circle(30.3, 20.7, 17.2), NonZero},
		"page minus box": {joined(rect(-1, -1, w+1, h+1), rect(20.5, 10.25, 60.75, 40.5)), EvenOdd},
		"off the raster": {circle(90, 55, 30), NonZero},
		"dense rows":     {zigzag(3.3, 5.1, 90, 40, 200), EvenOdd},
	}
	boxes := []image.Rectangle{
		image.Rect(0, 0, w, h),
		image.Rect(10, 5, 40, 30),
		image.Rect(47, 0, 48, h),
		image.Rect(60, 33, 120, 90),
		image.Rect(0, 59, w, 61),
	}
	for name, s := range shapes {
		shape, err := NewShape(s.path, s.rule, w, h)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, withClip := range []bool{false, true} {
			var m *surface.Mask
			if withClip {
				m = clip
			}
			full := mustFill(t, s.path, s.rule, w, h, m)
			for _, box := range boxes {
				in := box.Intersect(image.Rect(0, 0, w, h))
				var pre []float32
				if withClip {
					pre = make([]float32, in.Dx()*in.Dy())
					for y := in.Min.Y; y < in.Max.Y; y++ {
						for x := in.Min.X; x < in.Max.X; x++ {
							pre[(y-in.Min.Y)*in.Dx()+x-in.Min.X] = clip.A[y*w+x]
						}
					}
				}
				got := shape.CoverageIn(in, pre)
				for y := in.Min.Y; y < in.Max.Y; y++ {
					for x := in.Min.X; x < in.Max.X; x++ {
						want := full.At(x, y)
						v := got[(y-in.Min.Y)*in.Dx()+x-in.Min.X]
						if math.Float32bits(v) != math.Float32bits(want) {
							t.Fatalf("%s, clip %v, box %v: pixel (%d, %d) is %v, Fill gives %v", name, withClip, box, x, y, v, want)
						}
					}
				}
			}
			if b := shape.Bounds(); b != image.Rect(full.X, full.Y, full.X+full.Width, full.Y+full.Height) {
				t.Errorf("%s: bounds %v, Fill covers %v", name, b, image.Rect(full.X, full.Y, full.X+full.Width, full.Y+full.Height))
			}
		}
	}
}
