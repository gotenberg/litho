package typeset_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"testing"

	"github.com/gotenberg/litho/internal/imagetest"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// rowsOf hands out the rows of samples, each n bytes.
func rowsOf(samples []byte, n int) func(int, func([]byte) error) error {
	return func(y int, put func([]byte) error) error { return put(samples[y*n : (y+1)*n]) }
}

// TestEncodeLayout_WritesTheImageDictionaryOfTheLayout checks the image
// dictionary each layout writes (ISO 32000-1, 8.9.5.1, Table 89): its
// depth, its colour space, its soft mask's depth, its profile, written
// once for two images.
func TestEncodeLayout_WritesTheImageDictionaryOfTheLayout(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	icc := imagetest.ICCProfile("RGB ", 2)
	indexed, err := typeset.EncodeLayout(ctx, 9, 2, typeset.ImageLayout{BitsPerComponent: 2, Components: 1, Palette: []byte{255, 0, 0, 0, 255, 0, 0, 0, 255}, ICC: icc},
		rowsOf(make([]byte, 6), 3), nil)
	if err != nil {
		t.Fatal(err)
	}
	deep, err := typeset.EncodeLayout(ctx, 2, 2, typeset.ImageLayout{BitsPerComponent: 16, Components: 3, ICC: icc, AlphaBits: 16},
		rowsOf(make([]byte, 24), 12), rowsOf(make([]byte, 8), 4))
	if err != nil {
		t.Fatal(err)
	}
	doc := &typeset.Document{Pages: []*typeset.Page{{Width: 100, Height: 100, Content: []typeset.Fragment{
		&typeset.Image{Rect: typeset.Rect{Width: 50, Height: 50}, Source: indexed},
		&typeset.Image{Rect: typeset.Rect{X: 50, Width: 50, Height: 50}, Source: deep},
	}}}}
	out := write(t, doc)
	for _, want := range []string{"/Indexed", "/ICCBased", "/BitsPerComponent 2", "/BitsPerComponent 16", "/SMask"} {
		if !bytes.Contains(out, []byte(want)) {
			t.Errorf("the PDF lacks %s", want)
		}
	}
	if n := bytes.Count(out, []byte("/N 3")); n != 1 {
		t.Errorf("%d profile streams, want one for both images", n)
	}
}

// TestEncodeLayout_RefusesALayoutNoDictionaryDescribes checks the layouts
// refused: a depth Table 89 does not list, a palette over colours, a row
// of the wrong length, a JPEG 2000 layout over samples.
func TestEncodeLayout_RefusesALayoutNoDictionaryDescribes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		l    typeset.ImageLayout
		row  int
	}{
		{"3 bits", typeset.ImageLayout{BitsPerComponent: 3, Components: 1}, 1},
		{"palette over rgb", typeset.ImageLayout{BitsPerComponent: 8, Components: 3, Palette: []byte{1, 2, 3}}, 6},
		{"short row", typeset.ImageLayout{BitsPerComponent: 8, Components: 3}, 5},
		{"jpx over samples", typeset.ImageLayout{BitsPerComponent: 8, Components: 1, JPX: true}, 2},
	} {
		_, err := typeset.EncodeLayout(ctx, 2, 1, tc.l, rowsOf(make([]byte, tc.row), tc.row), nil)
		if !errors.Is(err, typeset.ErrInvalidImage) {
			t.Errorf("%s: %v", tc.name, err)
		}
	}
}

// TestJPXImage_WritesTheFileAsItIs checks a JP2 file is written by
// JPXDecode without a colour space, the file's own applying (Table 89).
func TestJPXImage_WritesTheFileAsItIs(t *testing.T) {
	t.Parallel()
	data := imagetest.JPX(image.NewGray(image.Rect(0, 0, 5, 3)), imagetest.JPXOptions{})
	src, err := typeset.JPXImage(context.Background(), data, 5, 3, typeset.ImageLayout{Components: 1})
	if err != nil {
		t.Fatal(err)
	}
	out := write(t, &typeset.Document{Pages: []*typeset.Page{{Width: 10, Height: 10, Content: []typeset.Fragment{&typeset.Image{Rect: typeset.Rect{Width: 5, Height: 3}, Source: src}}}}})
	if !bytes.Contains(out, []byte("/JPXDecode")) || !bytes.Contains(out, data) {
		t.Error("the JP2 file is not written as it is")
	}
	if _, err := typeset.JPXImage(context.Background(), data, 6, 3, typeset.ImageLayout{Components: 1}); !errors.Is(err, typeset.ErrInvalidImage) {
		t.Errorf("a size the file does not have: %v", err)
	}
}
