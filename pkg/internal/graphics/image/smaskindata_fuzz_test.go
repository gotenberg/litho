package image

import "testing"

// FuzzUnpremultiply asserts that undoing premultiplication never panics,
// whatever lengths the samples and the opacity have against the pixel count.
func FuzzUnpremultiply(f *testing.F) {
	f.Add([]byte{50, 100, 7, 9, 200, 255}, []byte{100, 0, 255}, 3, 2)
	f.Add([]byte{50, 100, 7}, []byte{100}, 4, 3)
	f.Fuzz(func(_ *testing.T, samples, alpha []byte, pixels, comps int) {
		Unpremultiply(samples, alpha, pixels, comps)
	})
}
