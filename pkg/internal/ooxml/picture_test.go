package ooxml

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/css"
)

// pngBytes returns a w by h PNG.
func pngBytes(t testing.TB, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDecodePicture(t *testing.T) {
	t.Parallel()
	pic, err := DecodePicture(context.Background(), pngBytes(t, 96, 48), nil)
	if err != nil {
		t.Fatal(err)
	}
	if pic.Width != 72 || pic.Height != 36 || pic.Source == nil {
		t.Errorf("picture = %v x %v", pic.Width, pic.Height)
	}
	emf := make([]byte, 64)
	emf[0] = 1
	copy(emf[40:], " EMF")
	if _, err := DecodePicture(context.Background(), emf, nil); !errors.Is(err, ErrUnsupportedPicture) {
		t.Errorf("EMF = %v", err)
	}
	svgPic, err := DecodePicture(context.Background(), []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="40" height="20"><rect width="40" height="20"/></svg>`), nil)
	if err != nil || svgPic.Vector == nil || svgPic.Width != 30 {
		t.Errorf("SVG = %+v, %v", svgPic, err)
	}
}

func TestCropped(t *testing.T) {
	t.Parallel()
	pic, err := DecodePicture(context.Background(), pngBytes(t, 100, 100), nil)
	if err != nil {
		t.Fatal(err)
	}
	var crop [4]float64
	crop[css.Left], crop[css.Right] = 0.25, 0.25
	c := Cropped(pic, crop)
	if c.Width != pic.Width/2 || c.Height != pic.Height || c.Vector == nil {
		t.Fatalf("cropped = %v x %v", c.Width, c.Height)
	}
	frags := c.Vector(context.Background(), compose.Rect{X: 10, Y: 0, Width: 50, Height: 100})
	g, ok := frags[0].(*compose.Group)
	if !ok || g.Clip == nil {
		t.Fatalf("not a clipped group: %#v", frags)
	}
	img := g.Content[0].(*compose.Image)
	if img.Rect != (compose.Rect{X: -15, Y: 0, Width: 100, Height: 100}) {
		t.Errorf("image rect = %+v", img.Rect)
	}
}
