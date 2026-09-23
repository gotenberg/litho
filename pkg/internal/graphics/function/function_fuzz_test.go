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
func FuzzEvalSampled(f *testing.F) {
	f.Add([]byte{255, 128, 0}, 3, 1, 0.5)
	f.Add([]byte{0}, 1, 1, 0.0)
	f.Add([]byte{1, 2, 3, 4}, 2, 2, 1.0)

	f.Fuzz(func(t *testing.T, samples []byte, size, outputs int, x float64) {
		if size < 1 || size > 64 || outputs < 1 || outputs > 4 {
			return
		}
		if len(samples) < size*outputs || len(samples) > 4096 {
			return
		}
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return
		}

		fn := &Function{
			kind:    0,
			domain:  []float64{0, 1},
			outputs: outputs,
			size:    []int{size},
			encode:  []float64{0, float64(size - 1)},
		}
		for range outputs {
			fn.rng = append(fn.rng, 0, 1)
			fn.decode = append(fn.decode, 0, 1)
		}
		fn.samples = make([]float64, size*outputs)
		for i := range fn.samples {
			fn.samples[i] = float64(samples[i]) / 255
		}

		out, err := fn.Eval([]float64{x})
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
