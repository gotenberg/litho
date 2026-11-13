package effect

import (
	"context"
	"errors"
	"testing"
)

// TestEncode checks that a surface encodes with straight colours and an
// alpha soft mask over its grid, that a transparent one encodes to
// nothing and that a coverage surface is refused.
func TestEncode(t *testing.T) {
	ctx := context.Background()
	s, err := New(ctx, RGBA, 2, 1, Grid{X: 10, Y: 20, Scale: 2})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Release()
	if img, err := Encode(ctx, s); img != nil || err != nil {
		t.Errorf("a transparent surface encodes to %v, %v, want nothing", img, err)
	}
	// Red at half alpha, premultiplied: 128 of 255.
	copy(s.Pix, []byte{128, 0, 0, 128, 0, 0, 0, 0})
	img, err := Encode(ctx, s)
	if err != nil || img == nil {
		t.Fatalf("Encode = %v, %v", img, err)
	}
	if r := img.Rect; r.X != 10 || r.Y != 20 || r.Width != 1 || r.Height != 0.5 {
		t.Errorf("placed over %+v, want the grid's 1 by 0.5 points at (10, 20)", r)
	}
	if img.Parent != nil || img.Source.Alpha == nil {
		t.Errorf("picture %+v, want no parent and a soft mask", img)
	}
	pix, err := img.Source.Decode(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if r, _, _, _ := pix.At(0, 0).RGBA(); r>>8 != 128 {
		t.Errorf("the first pixel's red decodes to %d premultiplied, want 128", r>>8)
	}
	c, err := New(ctx, Coverage, 1, 1, Grid{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Release()
	if _, err := Encode(ctx, c); !errors.Is(err, ErrFormat) {
		t.Errorf("Encode of a coverage surface = %v, want ErrFormat", err)
	}
}
