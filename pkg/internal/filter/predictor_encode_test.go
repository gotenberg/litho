package filter

import (
	"bytes"
	"errors"
	"testing"
)

func TestEncodePNGPredictorRoundTrips(t *testing.T) {
	cases := []struct {
		name                 string
		colors, bpc, columns int
		rows                 int
	}{
		{"gray 8", 1, 8, 7, 5},
		{"rgb 8", 3, 8, 9, 4},
		{"cmyk 8", 4, 8, 3, 3},
		{"rgb 16", 3, 16, 5, 3},
		{"bilevel", 1, 1, 13, 6},
		{"gray 2", 1, 2, 5, 4},
		{"gray 4", 1, 4, 3, 4},
		{"devicen 32", 32, 8, 2, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rowBytes := (tc.columns*tc.colors*tc.bpc + 7) / 8
			data := make([]byte, rowBytes*tc.rows)
			for i := range data {
				data[i] = byte(i*37 + i/rowBytes*11)
			}
			enc, err := EncodePNGPredictor(data, tc.colors, tc.bpc, tc.columns)
			if err != nil {
				t.Fatalf("EncodePNGPredictor: %v", err)
			}
			if len(enc) != len(data)+tc.rows {
				t.Fatalf("encoded %d bytes, want %d", len(enc), len(data)+tc.rows)
			}
			dec, err := applyPNGPredictor(enc, &predictorParams{
				Predictor: 15, Colors: tc.colors, BitsPerComponent: tc.bpc, Columns: tc.columns,
			})
			if err != nil {
				t.Fatalf("applyPNGPredictor: %v", err)
			}
			if !bytes.Equal(dec, data) {
				t.Fatal("round trip changed the samples")
			}
		})
	}
}

func TestEncodePNGPredictorPicksPredictionForGradients(t *testing.T) {
	// A horizontal ramp predicts to a run of equal bytes under Sub, which the
	// heuristic must prefer to the raw row.
	data := make([]byte, 64)
	for i := range data {
		data[i] = byte(i * 3)
	}
	enc, err := EncodePNGPredictor(data, 1, 8, 64)
	if err != nil {
		t.Fatal(err)
	}
	if enc[0] == 0 {
		t.Fatal("chose no prediction for a ramp")
	}
}

func TestEncodePNGPredictorRejectsLayouts(t *testing.T) {
	cases := []struct {
		name                 string
		data                 []byte
		colors, bpc, columns int
	}{
		{"partial row", make([]byte, 10), 1, 8, 3},
		{"empty", nil, 1, 8, 1},
		{"bpc 3", make([]byte, 6), 1, 3, 2},
		{"colors 0", make([]byte, 6), 0, 8, 2},
		{"colors 33", make([]byte, 66), 33, 8, 2},
		{"columns 0", make([]byte, 6), 1, 8, 0},
		{"columns past data", make([]byte, 1), 1, 1, 9},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := EncodePNGPredictor(tc.data, tc.colors, tc.bpc, tc.columns)
			if !errors.Is(err, ErrPredictorLayout) {
				t.Fatalf("err = %v, want ErrPredictorLayout", err)
			}
		})
	}
}
