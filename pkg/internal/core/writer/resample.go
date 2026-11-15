package writer

import (
	"context"
	"errors"
	"fmt"
)

// errResampleInput reports dimensions [resampleArea] cannot take: a target
// larger than its source or of no pixels, or samples that do not fill the
// source. The pass checks them before it calls it.
var errResampleInput = errors.New("resampling: invalid dimensions")

// resampleArea averages samples, rows of w x h pixels of c components at 8
// bits each, down to w2 x h2 pixels. Each target pixel is the mean of the
// source area it covers, every source pixel weighted by the exact share of
// it the target pixel covers, so a target pixel that covers a source pixel
// in part takes that part of it. This is the average of Ghostscript's
// /Average downsampling generalised to a factor that is not whole, and it
// works on each component of the image's own colour space as it stands
// (ISO 32000-1, 8.9.5: the samples are values in that space, and a mean of
// them is one).
//
// The arithmetic is integer: in units of 1/w2 of a source pixel across and
// 1/h2 down, a target pixel covers w by h units and a source pixel w2 by h2,
// so every weight is a whole number, the weights of one target pixel sum to
// w x h, and the mean is rounded half up. The result is the same on every
// machine. Rows are read and written in order, one source row and one row
// of sums at a time beside the output.
//
// w x h must be at most 2^31 pixels, which the image budget of the pass
// keeps it under, so that a sum of 255 x w x h fits 64 bits.
func resampleArea(ctx context.Context, samples []byte, w, h, c, w2, h2 int) ([]byte, error) {
	if w < 1 || h < 1 || c < 1 || w2 < 1 || h2 < 1 || w2 > w || h2 > h ||
		int64(w)*int64(h) > 1<<31 || int64(len(samples)) < int64(w)*int64(h)*int64(c) {
		return nil, fmt.Errorf("%w: %dx%d of %d components to %dx%d from %d bytes", errResampleInput, w, h, c, w2, h2, len(samples))
	}
	rowBytes := w * c // fits int: at most len(samples), checked above
	out := make([]byte, 0, w2*h2*c)
	hsum := make([]uint64, w2*c)
	acc := make([]uint64, w2*c)
	total := uint64(w) * uint64(h) //nolint:gosec // Both positive, checked above.
	half := total / 2

	// Down the image, in units of 1/h2 of a source row: target row y covers
	// [y*h, (y+1)*h), source row i covers [i*h2, (i+1)*h2).
	y := int64(0)
	for i := range h {
		if i%64 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, fmt.Errorf("resampling: %w", err)
			}
		}
		resampleRow(samples[i*rowBytes:(i+1)*rowBytes], w, c, w2, hsum)
		lo, hi := int64(i)*int64(h2), int64(i+1)*int64(h2)
		for lo < hi {
			bound := (y + 1) * int64(h)
			seg := min(hi, bound) - lo
			weight := uint64(seg) //nolint:gosec // Positive: lo < hi and lo < bound.
			for k, v := range hsum {
				acc[k] += v * weight
			}
			lo += seg
			if lo == bound {
				for _, v := range acc {
					out = append(out, byte((v+half)/total)) //nolint:gosec // A weighted mean of bytes, at most 255.
				}
				clear(acc)
				y++
			}
		}
	}
	return out, nil
}

// resampleRow writes into sums the weighted sums of one source row of w
// pixels of c components across w2 target pixels: in units of 1/w2 of a
// source pixel, target pixel x covers [x*w, (x+1)*w) and source pixel j
// covers [j*w2, (j+1)*w2), and each sum takes each source sample times the
// units of it the target covers, w units in all.
func resampleRow(row []byte, w, c, w2 int, sums []uint64) {
	clear(sums)
	x := int64(0)
	for j := range w {
		lo, hi := int64(j)*int64(w2), int64(j+1)*int64(w2)
		px := row[j*c : (j+1)*c]
		for lo < hi {
			bound := (x + 1) * int64(w)
			seg := min(hi, bound) - lo
			weight := uint64(seg) //nolint:gosec // Positive: lo < hi and lo < bound.
			base := x * int64(c)
			for k, v := range px {
				sums[base+int64(k)] += uint64(v) * weight
			}
			lo += seg
			if lo == bound {
				x++
			}
		}
	}
}
