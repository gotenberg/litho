package dct

import (
	"context"
	"errors"
	"os"
	"testing"
)

// FuzzDecode feeds arbitrary data to the decoder. The contract is
// crash-freedom: every input either decodes to one sample per component per
// pixel or returns an error carrying a sentinel.
func FuzzDecode(f *testing.F) {
	for _, name := range []string{"progressive", "restart", "cmyk_adobe"} {
		data, err := os.ReadFile("testdata/" + name + ".jpg")
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
		f.Add(data[:len(data)/2])
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		out, n, w, h, err := Decode(context.Background(), data, -1, 0)
		if err != nil {
			if !errors.Is(err, ErrMalformed) && !errors.Is(err, ErrUnsupported) && !errors.Is(err, ErrLimit) {
				t.Fatalf("error without a sentinel: %v", err)
			}
			return
		}
		if len(out) != n*w*h {
			t.Fatalf("%d samples for %dx%d of %d components", len(out), w, h, n)
		}
	})
}
