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

// referencePNGPredict is the prediction [EncodePNGPredictor] promises,
// written plainly: every filter of ISO/IEC 15948, 9.2 through one switch,
// the least sum of absolute signed bytes winning, ties to the lowest type.
func referencePNGPredict(data []byte, bpp, rowBytes int) []byte {
	var out []byte
	prev := make([]byte, rowBytes)
	for r := range len(data) / rowBytes {
		cur := data[r*rowBytes : (r+1)*rowBytes]
		var best []byte
		bestScore := -1
		for ft := range 5 {
			row := []byte{byte(ft)}
			score := 0
			for i := range cur {
				var left, upLeft byte
				if i >= bpp {
					left, upLeft = cur[i-bpp], prev[i-bpp]
				}
				up := prev[i]
				var d byte
				switch ft {
				case 0:
					d = cur[i]
				case 1:
					d = cur[i] - left
				case 2:
					d = cur[i] - up
				case 3:
					d = cur[i] - byte((int(left)+int(up))/2)
				default:
					d = cur[i] - paethPredictor(left, up, upLeft)
				}
				row = append(row, d)
				score += abs(int(int8(d)))
			}
			if bestScore < 0 || score < bestScore {
				best, bestScore = row, score
			}
		}
		out = append(out, best...)
		prev = cur
	}
	return out
}

func TestEncodePNGPredictorMatchesReference(t *testing.T) {
	cases := []struct {
		colors, bpc, columns, rows int
	}{
		{1, 8, 31, 9}, {3, 8, 17, 12}, {4, 8, 13, 10}, {3, 16, 7, 6}, {1, 1, 29, 8}, {1, 4, 5, 7}, {32, 8, 2, 3},
	}
	for _, tc := range cases {
		rowBytes := (tc.columns*tc.colors*tc.bpc + 7) / 8
		bpp := max((tc.colors*tc.bpc+7)/8, 1)
		data := make([]byte, rowBytes*tc.rows)
		seed := uint32(tc.colors*131 + tc.bpc*17 + tc.columns) //nolint:gosec // Small test constants.
		for i := range data {
			// A ramp with noise, so more than one filter type wins a row.
			seed = seed*1664525 + 1013904223
			data[i] = byte(i/3+(i/rowBytes)*5) + byte(seed>>29) //nolint:gosec // Wrapping is the point.
		}
		got, err := EncodePNGPredictor(data, tc.colors, tc.bpc, tc.columns)
		if err != nil {
			t.Fatal(err)
		}
		if want := referencePNGPredict(data, bpp, rowBytes); !bytes.Equal(got, want) {
			t.Fatalf("%+v: the prediction differs from the reference", tc)
		}
	}
}

// failingWriter accepts limit bytes and then fails.
type failingWriter struct {
	limit, n int
}

var errWriterFull = errors.New("writer full")

func (w *failingWriter) Write(p []byte) (int, error) {
	if w.n+len(p) > w.limit {
		return 0, errWriterFull
	}
	w.n += len(p)
	return len(p), nil
}

func TestWritePNGPredictorStopsOnWriterError(t *testing.T) {
	data := make([]byte, 10*100)
	w := &failingWriter{limit: 250}
	if err := WritePNGPredictor(w, data, 1, 8, 100); !errors.Is(err, errWriterFull) {
		t.Fatalf("err = %v, want the writer's", err)
	}
	if w.n != 2*101 {
		t.Fatalf("wrote %d bytes before stopping, want the 2 rows that fit", w.n)
	}
	var buf bytes.Buffer
	if err := WritePNGPredictor(&buf, data, 1, 8, 100); err != nil {
		t.Fatal(err)
	}
	want, err := EncodePNGPredictor(data, 1, 8, 100)
	if err != nil || !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("WritePNGPredictor wrote other bytes than EncodePNGPredictor returns (err %v)", err)
	}
	if err := WritePNGPredictor(&buf, data[:7], 1, 8, 100); !errors.Is(err, ErrPredictorLayout) {
		t.Fatalf("err = %v, want ErrPredictorLayout", err)
	}
}
