package raster

import (
	"image"
	"slices"
	"sync"
	"testing"
)

func TestShapeSampleIn(t *testing.T) {
	t.Parallel()
	const w, h = 12, 8
	s, err := NewShape(rect(2.3, 1.1, 6, 4), NonZero, w, h)
	if err != nil {
		t.Fatal(err)
	}
	got := s.SampleIn(image.Rect(0, 0, w, h))
	// Pixel 2's sample cell, x 2 to 2.25, lies left of the edge at 2.3, and
	// pixel 6's holds the right edge at 6: the columns in are 3 to 6. The
	// top edge at 1.1 crosses row 1's cell and the bottom edge at 4 row 4's:
	// the rows in are 1 to 4.
	for y := range h {
		for x := range w {
			want := float32(0)
			if x >= 3 && x <= 6 && y >= 1 && y <= 4 {
				want = 1
			}
			if v := got[y*w+x]; v != want {
				t.Errorf("pixel (%d, %d) = %v, want %v", x, y, v, want)
			}
		}
	}
}

func TestShapeSampleInFillRule(t *testing.T) {
	t.Parallel()
	const w, h = 12, 12
	ring := joined(rect(0.5, 0.5, 10.5, 10.5), rect(3.5, 3.5, 7.5, 7.5))
	for _, tt := range []struct {
		rule FillRule
		hole float32
	}{{EvenOdd, 0}, {NonZero, 1}} {
		s, err := NewShape(ring, tt.rule, w, h)
		if err != nil {
			t.Fatal(err)
		}
		got := s.SampleIn(image.Rect(0, 0, w, h))
		if v := got[5*w+5]; v != tt.hole {
			t.Errorf("rule %v: hole pixel = %v, want %v", tt.rule, v, tt.hole)
		}
		if v := got[2*w+2]; v != 1 {
			t.Errorf("rule %v: ring pixel = %v, want 1", tt.rule, v)
		}
		if v := got[11*w+11]; v != 0 {
			t.Errorf("rule %v: outside pixel = %v, want 0", tt.rule, v)
		}
	}
}

// TestShapeSampleInConcurrent reads one shape from several goroutines, the
// first call of each racing to sort the segments.
func TestShapeSampleInConcurrent(t *testing.T) {
	t.Parallel()
	const w, h = 64, 64
	s, err := NewShape(circle(30, 30, 20), NonZero, w, h)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make([][]float32, 4)
	for i := range results {
		wg.Go(func() { results[i] = s.SampleIn(image.Rect(0, 0, w, h)) })
	}
	wg.Wait()
	for i := 1; i < len(results); i++ {
		if !slices.Equal(results[i], results[0]) {
			t.Fatalf("goroutine %d read another test", i)
		}
	}
}

// TestShapeSampleInBox checks that a part of the raster reads what the
// whole raster reads there.
func TestShapeSampleInBox(t *testing.T) {
	t.Parallel()
	const w, h = 97, 61
	for name, p := range map[string]FillRule{"star": EvenOdd, "circle": NonZero} {
		path := star(48, 30, 28)
		if name == "circle" {
			path = circle(30.3, 20.7, 17.2)
		}
		s, err := NewShape(path, p, w, h)
		if err != nil {
			t.Fatal(err)
		}
		whole := s.SampleIn(image.Rect(0, 0, w, h))
		for _, box := range []image.Rectangle{image.Rect(10, 5, 40, 30), image.Rect(47, 0, 48, h), image.Rect(60, 33, 120, 90)} {
			part := s.SampleIn(box)
			in := box.Intersect(image.Rect(0, 0, w, h))
			for y := in.Min.Y; y < in.Max.Y; y++ {
				for x := in.Min.X; x < in.Max.X; x++ {
					if a, b := part[(y-in.Min.Y)*in.Dx()+x-in.Min.X], whole[y*w+x]; a != b {
						t.Fatalf("%s: box %v pixel (%d, %d) = %v, whole raster %v", name, box, x, y, a, b)
					}
				}
			}
		}
	}
}
