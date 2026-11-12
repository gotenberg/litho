package typeset_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// FuzzJPEGImage feeds arbitrary bytes to JPEGImage: it fails with a
// sentinel or returns a source that keeps the bytes as they are.
func FuzzJPEGImage(f *testing.F) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewGray(image.Rect(0, 0, 3, 2)), nil); err != nil {
		f.Fatal(err)
	}
	f.Add(buf.Bytes())
	f.Add(buf.Bytes()[:20])
	f.Fuzz(func(t *testing.T, data []byte) {
		src, err := typeset.JPEGImage(context.Background(), data)
		if err != nil {
			if !errors.Is(err, typeset.ErrInvalidImage) && !errors.Is(err, typeset.ErrLimitExceeded) {
				t.Fatalf("unwrapped error %v", err)
			}
			return
		}
		if src.Width <= 0 || src.Height <= 0 || !bytes.Equal(src.JPEG, data) {
			t.Fatalf("source %d by %d", src.Width, src.Height)
		}
	})
}
