package imagetiles

import (
	"image"
	"slices"
	"testing"
)

func TestCellRuns(t *testing.T) {
	t.Parallel()
	// 3 x 3 cells, photo marked P:
	//   P P .
	//   P P .
	//   . P P
	photo := []bool{true, true, false, true, true, false, false, true, true}
	got := CellRuns(photo, 3, 3)
	want := []image.Rectangle{
		image.Rect(0, 0, 2, 2), image.Rect(2, 0, 3, 2),
		image.Rect(0, 2, 1, 3), image.Rect(1, 2, 3, 3),
	}
	if !slices.Equal(got, want) {
		t.Errorf("CellRuns = %v, want %v", got, want)
	}
	covered := make([]int, len(photo))
	for _, r := range got {
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				covered[y*3+x]++
				if photo[y*3+x] != photo[r.Min.Y*3+r.Min.X] {
					t.Errorf("run %v mixes kinds", r)
				}
			}
		}
	}
	for i, n := range covered {
		if n != 1 {
			t.Errorf("cell %d covered %d times", i, n)
		}
	}
}

func TestCropSamples(t *testing.T) {
	t.Parallel()
	px := image.Rect(10, 20, 14, 23)
	samples := make([]byte, 0, 2*4*3)
	for y := range 3 {
		for x := range 4 {
			samples = append(samples, byte(10*y+x), 0)
		}
	}
	got := CropSamples(samples, px, 2, image.Rect(11, 21, 13, 23))
	want := []byte{11, 0, 12, 0, 21, 0, 22, 0}
	if !slices.Equal(got, want) {
		t.Errorf("CropSamples = %v, want %v", got, want)
	}
}

func TestPadOutside(t *testing.T) {
	t.Parallel()
	// A 4 x 3 composite of one component, its pixel at (x, y) holding
	// 10y + x, whose region's box reaches the pixels (1, 1) to (2, 1).
	px := image.Rect(0, 0, 4, 3)
	var samples []byte
	for y := range 3 {
		for x := range 4 {
			samples = append(samples, byte(10*y+x))
		}
	}
	got := PadOutside(samples, px, image.Rect(1, 1, 3, 2), 1, px)
	want := []byte{11, 11, 12, 12, 11, 11, 12, 12, 11, 11, 12, 12}
	if !slices.Equal(got, want) {
		t.Errorf("PadOutside = %v, want %v", got, want)
	}
	if got := PadOutside(samples, px, px, 1, image.Rect(1, 1, 3, 2)); !slices.Equal(got, []byte{11, 12}) {
		t.Errorf("PadOutside inside the box = %v, want the samples", got)
	}
}
