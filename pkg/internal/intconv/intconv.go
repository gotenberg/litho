package intconv

import "math"

// Clamp converts v to int, saturating at [math.MaxInt] or [math.MinInt] when v
// lies outside int's range.
//
// A plain int(v) wraps instead, and only on a 32-bit platform, where int stops
// at 2^31 - 1: a value read from a file, such as 2^32 + 5, then turns into 5
// and passes a bounds check that rejects it on a 64-bit platform. Saturating
// keeps it past every bound, so the checks that follow decide alike on both.
// ISO 32000-1, Annex C, Table C.1 holds a conforming PDF integer to the 32-bit
// range, so Clamp changes the value of non-conforming input only.
func Clamp[T ~int64 | ~uint64 | ~uint32 | ~uint](v T) int {
	if v < 0 {
		if int64(v) < math.MinInt {
			return math.MinInt
		}
		return int(v)
	}
	if uint64(v) > math.MaxInt {
		return math.MaxInt
	}
	return int(v)
}
