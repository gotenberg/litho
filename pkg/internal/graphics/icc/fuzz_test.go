package icc

import (
	"errors"
	"testing"
)

// FuzzProfile feeds arbitrary data to the profile reader and, when it reads
// and builds a transform, converts colours through it. The contract is
// crash-freedom: every input either converts or returns an error carrying a
// sentinel.
func FuzzProfile(f *testing.F) {
	for _, p := range [][]byte{rgbProfile(), greyProfile(), cmykProfile(), extendedProfile()} {
		f.Add(p)
		f.Add(p[:len(p)/2])
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		sentinel := func(err error) {
			if !errors.Is(err, ErrMalformed) && !errors.Is(err, ErrUnsupported) && !errors.Is(err, ErrLimit) {
				t.Fatalf("error without a sentinel: %v", err)
			}
		}
		p, err := Parse(data)
		if err != nil {
			sentinel(err)
			return
		}
		for _, intent := range []Intent{Perceptual, RelativeColorimetric} {
			tr, err := p.NewTransform(intent)
			if err != nil {
				sentinel(err)
				continue
			}
			in := make([]byte, tr.Components())
			for v := range 4 {
				for i := range in {
					in[i] = byte(v * 85)
				}
				tr.Convert(in)
			}
		}
	})
}
