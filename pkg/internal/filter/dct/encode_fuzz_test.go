package dct

import (
	"context"
	"errors"
	"testing"
)

// fuzzEncodeMaxPixels keeps a fuzzed image small enough that every run
// encodes and decodes in milliseconds; the encoder's work is linear in the
// pixels, so larger images find nothing smaller ones do not.
const fuzzEncodeMaxPixels = 1 << 14

// FuzzEncode feeds arbitrary dimensions, component counts, options and
// samples to the encoder. The contract is crash-freedom: every input is
// either refused with [ErrInvalidInput] or written as a JPEG that [Decode]
// reads back at the dimensions and component count it was given.
func FuzzEncode(f *testing.F) {
	valid := testPhoto(13, 11, 3)
	f.Add(13, 11, 3, 85, uint8(1), true, 2, valid)
	f.Add(13, 11, 3, 85, uint8(0), false, 0, valid[:len(valid)/2])
	f.Add(5, 7, 4, 1, uint8(0), true, 1, testPhoto(5, 7, 4))
	f.Add(1, 1, 1, 100, uint8(0), false, 0, []byte{0x80})
	f.Fuzz(func(t *testing.T, width, height, comps, quality int, sub uint8, optimize bool, restart int, samples []byte) {
		if width > 0 && height > 0 && width*height > fuzzEncodeMaxPixels {
			t.Skip()
		}
		opts := EncodeOptions{Quality: quality, Subsampling: Subsampling(sub), OptimizeHuffman: optimize, RestartInterval: restart}
		data, err := Encode(context.Background(), samples, width, height, comps, opts)
		if err != nil {
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("error without the sentinel: %v", err)
			}
			return
		}
		out, n, w, h, err := Decode(context.Background(), data, -1, 0, DefaultMaxSamples)
		if err != nil {
			t.Fatalf("the encoder's output does not decode: %v", err)
		}
		if n != comps || w != width || h != height || len(out) != len(samples) {
			t.Fatalf("decoded %dx%d of %d components, %d samples, from %dx%d of %d", w, h, n, len(out), width, height, comps)
		}
	})
}
