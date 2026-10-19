package filter

import (
	"bytes"
	"testing"
)

// FuzzEncodePNGPredictor asserts that predicting arbitrary samples never
// panics and that litho's decoder always restores them.
func FuzzEncodePNGPredictor(f *testing.F) {
	f.Add([]byte{1, 2, 3, 4, 5, 6, 7, 8, 9}, uint8(3), uint8(3), uint16(3))
	f.Add([]byte{1, 2, 3, 4, 5}, uint8(3), uint8(3), uint16(3))
	f.Fuzz(func(t *testing.T, data []byte, colors, bpcSel uint8, columns uint16) {
		bpc := []int{1, 2, 4, 8, 16}[int(bpcSel)%5]
		enc, err := EncodePNGPredictor(data, int(colors), bpc, int(columns))
		if err != nil {
			return
		}
		dec, err := applyPNGPredictor(enc, &predictorParams{
			Predictor: 15, Colors: int(colors), BitsPerComponent: bpc, Columns: int(columns),
		})
		if err != nil {
			t.Fatalf("decoding a predicted row set: %v", err)
		}
		if !bytes.Equal(dec, data) {
			t.Fatal("round trip changed the samples")
		}
	})
}
