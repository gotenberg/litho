package compose_test

import (
	"bytes"
	"context"
	"image"
	"image/jpeg"
	"os"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/limits"
)

// storeDoc returns a page placing a picture with alpha, a gray one and a
// JPEG, each built under ctx.
func storeDoc(t *testing.T, ctx context.Context) *compose.Document {
	t.Helper()
	rgba := image.NewNRGBA(image.Rect(0, 0, 9, 7))
	for i := range rgba.Pix {
		rgba.Pix[i] = uint8(i * 7) //nolint:gosec // test values wrap on purpose
	}
	gray := image.NewGray(image.Rect(0, 0, 5, 5))
	for i := range gray.Pix {
		gray.Pix[i] = uint8(i * 11) //nolint:gosec // test values wrap on purpose
	}
	pic := image.NewRGBA(image.Rect(0, 0, 6, 4))
	for i := range pic.Pix {
		pic.Pix[i] = uint8(i * 13) //nolint:gosec // test values wrap on purpose
	}
	var jb bytes.Buffer
	if err := jpeg.Encode(&jb, pic, nil); err != nil {
		t.Fatal(err)
	}
	var sources []*compose.ImageSource
	for _, img := range []image.Image{rgba, gray} {
		b := img.Bounds()
		if err := compose.ChargeImage(ctx, b.Dx(), b.Dy(), img.ColorModel()); err != nil {
			t.Fatal(err)
		}
		src, err := compose.EncodeImage(ctx, img)
		if err != nil {
			t.Fatal(err)
		}
		sources = append(sources, src)
	}
	src, err := compose.JPEGImage(ctx, jb.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	sources = append(sources, src)
	page := &compose.Page{Width: 200, Height: 200}
	for i, s := range sources {
		page.Content = append(page.Content, &compose.Image{Rect: compose.Rect{X: float64(10 + 60*i), Y: 10, Width: 50, Height: 50}, Source: s})
	}
	return &compose.Document{Pages: []*compose.Page{page}}
}

// TestPictureStoreKeepsTheBytes locks that a picture kept in the store's
// file writes the same file as one kept in memory, decodes to the same
// pixels, costs the memory budget nothing, and that Close removes the
// file.
func TestPictureStoreKeepsTheBytes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	var outs [2][]byte
	var pixels [2][]image.Image
	for i, hold := range []int64{-1, 1} {
		ctx, mem, end := limits.WithBudget(context.Background(), limits.Limits{})
		ctx, store := compose.WithPictureStore(ctx, dir, hold)
		doc := storeDoc(t, ctx)
		if hold > 0 {
			if mem.Held() != 0 {
				t.Errorf("pictures in the store's file hold %d bytes of the budget", mem.Held())
			}
			if entries, _ := os.ReadDir(dir); len(entries) != 1 { //nolint:errcheck // an unreadable dir fails the check
				t.Errorf("%d store files, want 1", len(entries))
			}
		} else if mem.Held() == 0 {
			t.Error("pictures in memory hold nothing of the budget")
		}
		for _, f := range doc.Pages[0].Content {
			src := f.(*compose.Image).Source
			if hold > 0 && (src.Samples != nil || src.JPEG != nil || src.Alpha != nil) {
				t.Error("a stored picture keeps its bytes in memory")
			}
			img, err := src.Decode(ctx)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			compose.ReleaseImage(ctx, img)
			pixels[i] = append(pixels[i], img)
		}
		if !doc.Pages[0].Content[2].(*compose.Image).Source.IsJPEG() {
			t.Error("the JPEG source does not report itself as one")
		}
		var buf bytes.Buffer
		if err := compose.Write(ctx, doc, &buf, compose.Options{TempDir: dir}); err != nil {
			t.Fatalf("Write with hold %d: %v", hold, err)
		}
		outs[i] = buf.Bytes()
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		end()
		emptyDir(t, dir)
	}
	if !bytes.Equal(outs[0], outs[1]) {
		t.Errorf("the store changes the output: %d bytes against %d", len(outs[1]), len(outs[0]))
	}
	for k := range pixels[0] {
		a, b := pixels[0][k], pixels[1][k]
		if a.Bounds() != b.Bounds() {
			t.Fatalf("picture %d: bounds %v against %v", k, b.Bounds(), a.Bounds())
		}
		for y := a.Bounds().Min.Y; y < a.Bounds().Max.Y; y++ {
			for x := a.Bounds().Min.X; x < a.Bounds().Max.X; x++ {
				if a.At(x, y) != b.At(x, y) {
					t.Fatalf("picture %d differs at (%d, %d)", k, x, y)
				}
			}
		}
	}
}

// TestDecodeChargesByColourModel pins what Decode charges a JPEG source:
// one byte a pixel for a gray file, three for a Y'CbCr one, and that
// ReleaseImage gives it back exactly; the JPEG file itself is charged
// while the document holds it.
func TestDecodeChargesByColourModel(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		img  image.Image
		cost int64
	}{
		{"gray", image.NewGray(image.Rect(0, 0, 10, 10)), 1},
		{"ycbcr", image.NewRGBA(image.Rect(0, 0, 10, 10)), 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var jb bytes.Buffer
			if err := jpeg.Encode(&jb, tc.img, nil); err != nil {
				t.Fatal(err)
			}
			ctx, mem, end := limits.WithBudget(context.Background(), limits.Limits{})
			defer end()
			src, err := compose.JPEGImage(ctx, jb.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			if mem.Held() != int64(jb.Len()) {
				t.Errorf("the JPEG file holds %d bytes of the budget, want %d", mem.Held(), jb.Len())
			}
			img, err := src.Decode(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if got := mem.Held() - int64(jb.Len()); got != 100*tc.cost {
				t.Errorf("Decode charged %d, want %d", got, 100*tc.cost)
			}
			compose.ReleaseImage(ctx, img)
			if mem.Held() != int64(jb.Len()) {
				t.Errorf("after ReleaseImage the budget holds %d, want %d", mem.Held(), jb.Len())
			}
		})
	}
}
