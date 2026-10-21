package recolor

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/writer"
)

// BenchmarkConvertPixels converts a 512 x 512 RGB photograph-like gradient,
// every pixel a distinct colour so the memo rarely hits, to DeviceCMYK.
func BenchmarkConvertPixels(b *testing.B) {
	const side = 512
	data := make([]byte, 0, side*side*3)
	for y := range side {
		for x := range side {
			data = append(data, byte(x), byte(y), byte(x^y))
		}
	}
	p := newTestPage(side, side, imageStream(side, side, familyRGB, 8, data, nil))
	doc, pg := p.open(b)
	ctx := context.Background()
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		alloc := writer.NewObjAllocator(doc.HighestObjectNumber(ctx))
		cfg := deviceTarget(familyCMYK)
		cfg.Colors = deviceColors{tc: 4}
		im, err := NewImages(doc, alloc, cfg)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := im.ConvertXObject(ctx, imageRef, pg.Resources, pg.Dict); err != nil {
			b.Fatal(err)
		}
	}
}
