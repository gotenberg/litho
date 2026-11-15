package dct

import (
	"context"
	"fmt"
)

// Quality returns the quality, 1 to 100, whose luminance table [Encode]
// writes nearest the table that quantizes the first component of data: the
// quality whose table of T.81, Annex K.1 scaled as [EncodeOptions.Quality]
// scales it sums closest to that table, the higher of two as close. The
// first component is grey, Y or C, the one an eye reads detail in, and an
// encoder that follows the Independent JPEG Group's scaling writes the
// quality it was given.
//
// Quality reads the marker segments up to the first scan alone, as [Check]
// does, and fails as Check fails on them, or with [ErrMalformed] when the
// table the first component names is not defined before the scan or holds a
// zero quantizer (T.81, B.2.4.1 makes each quantizer at least 1).
func Quality(ctx context.Context, data []byte) (int, error) {
	d := &decoder{data: data, maxSamples: DefaultMaxSamples, checkOnly: true}
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 0, fmt.Errorf("%w: no start-of-image marker", ErrMalformed)
	}
	if err := d.decodeSegments(ctx); err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if !d.sawScan || len(d.comps) == 0 {
		return 0, fmt.Errorf("%w: no scan", ErrMalformed)
	}
	table := &d.quant[d.comps[0].tq]
	var sum int
	for _, q := range table {
		if q == 0 {
			return 0, fmt.Errorf("%w: quantization table %d undefined or holding 0", ErrMalformed, d.comps[0].tq)
		}
		sum += int(q)
	}
	best, bestDist := 0, -1
	for q := 1; q <= 100; q++ {
		scaled := scaledQuant(&lumaQuant, q)
		var s int
		for _, v := range scaled {
			s += v
		}
		dist := s - sum
		if dist < 0 {
			dist = -dist
		}
		if bestDist < 0 || dist <= bestDist {
			best, bestDist = q, dist
		}
	}
	return best, nil
}
