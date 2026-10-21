package recolor

import (
	"bytes"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/filter"
)

// fullEncoding is what [encodeSamples] promises, computed in full: both
// encodings compressed whole, the smaller kept, a tie to the plain one.
func fullEncoding(t *testing.T, samples []byte, width, comps int) (plain, best []byte) {
	t.Helper()
	plain, _, err := flateCompress(samples, -1)
	if err != nil {
		t.Fatal(err)
	}
	predicted, err := filter.EncodePNGPredictor(samples, comps, 8, width)
	if err != nil {
		t.Fatal(err)
	}
	packed, _, err := flateCompress(predicted, -1)
	if err != nil {
		t.Fatal(err)
	}
	if len(packed) < len(plain) {
		return plain, packed
	}
	return plain, plain
}

// TestEncodeSamples_MatchesFullEncoding checks that encodeSamples, which
// predicts the samples as the compressor takes them and stops the second
// encoding at the first one's size, keeps the bytes encoding both in full
// keeps, in either order, around every limit where the choice turns: on a
// photograph the prediction wins, on flat art the plain data.
func TestEncodeSamples_MatchesFullEncoding(t *testing.T) {
	const width, height, comps = 301, 257, 4
	photo := make([]byte, width*height*comps)
	flat := make([]byte, width*height*comps)
	seed := uint32(7)
	for i := range photo {
		// Gradients with a little noise, past several of the compressor's
		// blocks.
		seed = seed*1664525 + 1013904223
		px := i / comps
		photo[i] = byte(px%width+px/width*(i%comps+1)) + byte(seed>>30) //nolint:gosec // Wrapping is the point.
		// A two-colour dither, whose differences take more symbols than its
		// samples.
		if i%comps == 0 && seed>>31 == 1 {
			copy(flat[i:i+comps], []byte{255, 255, 255, 255})
		}
	}
	for name, samples := range map[string][]byte{"photo": photo, "flat": flat} {
		plain, full := fullEncoding(t, samples, width, comps)
		if name == "photo" && bytes.Equal(full, plain) || name == "flat" && !bytes.Equal(full, plain) {
			t.Fatalf("%s: the expected encoding does not win: the test checks less than it says", name)
		}
		limits := []int{-1, 0}
		for _, n := range []int{len(plain), len(full)} {
			limits = append(limits, n-1, n, n+1)
		}
		for _, limit := range limits {
			want, wantFits := full, true
			if limit >= 0 && len(full) > limit {
				want, wantFits = nil, false
			}
			for _, predictFirst := range []bool{false, true} {
				got, parms, fits, err := encodeSamples(samples, width, comps, 8, limit, predictFirst)
				if err != nil {
					t.Fatal(err)
				}
				if fits != wantFits || !bytes.Equal(got, want) {
					t.Fatalf("%s, limit %d, predict first %v: fits %v (%d bytes), want %v (%d bytes)", name, limit, predictFirst, fits, len(got), wantFits, len(want))
				}
				if fits && (parms != nil) == bytes.Equal(got, plain) {
					t.Fatalf("%s, limit %d, predict first %v: /DecodeParms %v for the %d bytes kept", name, limit, predictFirst, parms, len(got))
				}
			}
		}
	}
}
