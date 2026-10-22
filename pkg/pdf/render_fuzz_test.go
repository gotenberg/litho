package pdf

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

// FuzzRenderPage opens the fuzzed bytes and draws their first page under the
// options mode selects, in either colour model and any page boundary, at a
// small resolution and pixel budget. It must never reach the panic boundary:
// a recovered panic surfaces as [ErrInternalPanic].
func FuzzRenderPage(f *testing.F) {
	for _, seed := range [][]byte{cmykRenderPDF(), boxedPDF(90), annotatedPDF()} {
		f.Add(seed, uint8(0))
		f.Add(seed[:len(seed)/2], uint8(5))
	}
	f.Fuzz(func(t *testing.T, data []byte, mode uint8) {
		ctx := context.Background()
		r, err := Open(ctx, bytes.NewReader(data), int64(len(data)), WithLimits(Limits{MaxRenderPixels: 1 << 16, MaxStreamBytes: 1 << 20, MaxImagePixels: 1 << 16}))
		if err != nil {
			return
		}
		defer r.Close()
		opts := []RenderOption{
			WithRenderDPI(float64(9 + mode%4*9)),
			WithRenderBox(RenderBox(mode / 4 % 5)),
			WithRenderTarget(RenderTarget(mode / 32 % 2)),
		}
		if mode >= 64 {
			opts = append(opts, WithRenderColorModel(RenderCMYK))
		}
		if _, err := r.RenderPage(ctx, 1, opts...); errors.Is(err, ErrInternalPanic) {
			t.Fatalf("RenderPage panicked: %v", err)
		}
	})
}
