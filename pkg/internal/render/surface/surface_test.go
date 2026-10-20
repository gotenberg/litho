package surface

import (
	"errors"
	"testing"
)

func TestNewCanvasBoundsItsSize(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ w, h int }{{0, 10}, {10, -1}, {1 << 20, 1 << 20}} {
		if _, err := NewCanvas(tc.w, tc.h, 1<<24); !errors.Is(err, ErrTooLarge) {
			t.Errorf("NewCanvas(%d, %d) err = %v, want ErrTooLarge", tc.w, tc.h, err)
		}
		if _, err := NewMask(tc.w, tc.h, 1<<24); !errors.Is(err, ErrTooLarge) {
			t.Errorf("NewMask(%d, %d) err = %v, want ErrTooLarge", tc.w, tc.h, err)
		}
	}
	if _, err := NewCanvas(100, 100, 10000); err != nil {
		t.Errorf("NewCanvas at exactly the budget: %v", err)
	}
}

func TestBlendOverIsSourceOver(t *testing.T) {
	t.Parallel()
	c, err := NewCanvas(1, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	c.Fill(1, 1, 1, 1)
	c.BlendOver(0, 0, 0, 0, 0, 0.25)
	r, _, _, a := c.At(0, 0)
	if r != 0.75 || a != 1 {
		t.Errorf("white under 25%% black = (%v, a %v), want (0.75, a 1)", r, a)
	}
	c.BlendOver(5, 5, 0, 0, 0, 1)
	img := c.Image()
	if got := img.NRGBAAt(0, 0); got.R != 191 || got.A != 255 {
		t.Errorf("8-bit pixel = %+v, want R 191, A 255", got)
	}
}

func TestMaskIntersect(t *testing.T) {
	t.Parallel()
	a, _ := NewMask(2, 1, 2)
	b, _ := NewMask(2, 1, 2)
	a.Set(0, 0, 1)
	a.Set(1, 0, 0.5)
	b.Set(0, 0, 0.5)
	b.Set(1, 0, 2)
	if err := a.Intersect(b); err != nil {
		t.Fatal(err)
	}
	if a.At(0, 0) != 0.5 || a.At(1, 0) != 0.5 {
		t.Errorf("intersection = %v, want [0.5 0.5]", a.A)
	}
	c, _ := NewMask(1, 1, 1)
	if err := a.Intersect(c); err == nil {
		t.Error("Intersect accepted masks of different sizes")
	}
}
