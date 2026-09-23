package function

import (
	"math"
	"testing"
)

// FuzzBitsAt feeds arbitrary sample streams to the packed-sample reader.
//
// The stream comes straight from the file and its length is never guaranteed to
// match what /Size, /BitsPerSample and /Range imply, so an index past the end is
// reachable with any input. The reader must never panic and must never return a
// value wider than its declared depth.
func FuzzBitsAt(f *testing.F) {
	for _, seed := range []struct {
		data []byte
		i    int
		bps  int
	}{
		{[]byte{0xFF, 0x00}, 0, 8},
		{[]byte{0xB2, 0x4F}, 3, 4},
		{[]byte{}, 0, 8},
		{[]byte{0x01}, 7, 1},
		{[]byte{0xAA, 0xBB, 0xCC, 0xDD}, 0, 32},
	} {
		f.Add(seed.data, seed.i, seed.bps)
	}

	f.Fuzz(func(t *testing.T, data []byte, i, bps int) {
		// Mirror the caller's validation: New accepts only these depths and
		// never asks for a negative index.
		switch bps {
		case 1, 2, 4, 8, 12, 16, 24, 32:
		default:
			return
		}
		if i < 0 || i > 1<<20 || len(data) > 1<<16 {
			return
		}

		got := bitsAt(data, i, bps)
		if bps < 64 && got > uint64(1)<<uint(bps)-1 {
			t.Fatalf("bitsAt returned %#x, wider than %d bits", got, bps)
		}
	})
}

// FuzzEvalSampled drives the interpolator with arbitrary sample tables.
//
// Every number in a sampled function is file-controlled: the table, the encode
// and decode ranges and the size. The interpolator must never panic, never
// index outside the table, and never return NaN, which would convert to a
// garbage byte rather than fail when a caller builds a colour from it.
//
// Both a one-input and a two-input table are driven, because the multilinear
// path visits 2^m corners and compounds a stride per input: a one-input fuzz
// exercises neither. A DeviceN tint transform in the corpora takes seven.
func FuzzEvalSampled(f *testing.F) {
	f.Add([]byte{255, 128, 0}, 3, 1, 0.5, 0.0, false)
	f.Add([]byte{0}, 1, 1, 0.0, 0.0, false)
	f.Add([]byte{1, 2, 3, 4}, 2, 2, 1.0, 0.0, false)
	f.Add([]byte{0, 60, 120, 180, 240, 255}, 3, 1, 0.5, 0.5, true)
	f.Add([]byte{0, 1, 2, 3, 4, 5, 6, 7}, 2, 1, 0.25, 0.75, true)

	f.Fuzz(func(t *testing.T, samples []byte, size, outputs int, x0, x1 float64, twoInputs bool) {
		if size < 1 || size > 64 || outputs < 1 || outputs > 4 {
			return
		}
		if math.IsNaN(x0) || math.IsInf(x0, 0) || math.IsNaN(x1) || math.IsInf(x1, 0) {
			return
		}

		fn := &Function{kind: 0, outputs: outputs}
		var cells int
		if twoInputs {
			// A second input of the same size: the table is size*size cells.
			fn.domain = []float64{0, 1, 0, 1}
			fn.size = []int{size, size}
			fn.encode = []float64{0, float64(size - 1), 0, float64(size - 1)}
			cells = size * size
		} else {
			fn.domain = []float64{0, 1}
			fn.size = []int{size}
			fn.encode = []float64{0, float64(size - 1)}
			cells = size
		}
		if len(samples) < cells*outputs || len(samples) > 4096 {
			return
		}
		for range outputs {
			fn.rng = append(fn.rng, 0, 1)
			fn.decode = append(fn.decode, 0, 1)
		}
		fn.samples = make([]float64, cells*outputs)
		for i := range fn.samples {
			fn.samples[i] = float64(samples[i]) / 255
		}

		inputs := []float64{x0}
		if twoInputs {
			inputs = append(inputs, x1)
		}

		out, err := fn.Eval(inputs)
		if err != nil {
			t.Fatalf("Eval returned %v for a well-formed table", err)
		}
		if len(out) != outputs {
			t.Fatalf("Eval returned %d values, want %d", len(out), outputs)
		}
		for j, v := range out {
			if math.IsNaN(v) {
				t.Fatalf("output %d is NaN", j)
			}
			if v < 0 || v > 1 {
				t.Fatalf("output %d = %v, outside the declared /Range [0 1]", j, v)
			}
		}
	})
}
