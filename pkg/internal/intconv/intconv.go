package intconv

import (
	"math"
	"strconv"
)

// MaxCountStart bounds the magnitude of the number a count read from a
// document starts from, and of an adjustment to it: a list's start value,
// a first page number, a page number adjustment. Anti-overflow guard, not
// spec-mandated: ODF 1.3 types these as unbounded integers and ECMA-376 as
// 32-bit ones, and a start past the bound saturates at it. At 2^29 a count
// it starts, moved by an adjustment as large and by one for each of the
// 2,000,000 elements a part holds at most, stays within 32 bits, so it adds
// alike where int is 32 or 64 bits.
const MaxCountStart = 1 << 29

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

// Atoi parses s as a base-10 integer of 64 bits, the syntax of
// [strconv.Atoi], and narrows it with [Clamp]. Its error is the one
// [strconv.ParseInt] returns for that syntax and width.
//
// [strconv.Atoi] parses at the width of int, so a value from 2^31 up reads
// where int is 64 bits and fails where it is 32. Atoi reads it on both and
// saturates it at [math.MaxInt]: a caller that compares the result with
// bounds that fit 32 bits before any arithmetic decides alike on every
// platform. A caller that adds, multiplies, shows or stores the value
// parses with [strconv.ParseInt] and computes in int64, or reads a 32-bit
// value with [Atoi32].
func Atoi(s string) (int, error) {
	v, err := strconv.ParseInt(s, 10, 64)
	return Clamp(v), err
}

// Atoi32 parses s as a base-10 integer of 32 bits, the syntax of
// [strconv.Atoi]. A value outside the 32-bit range returns the bound it
// passes and an error wrapping [strconv.ErrRange], on every platform.
func Atoi32(s string) (int, error) {
	v, err := strconv.ParseInt(s, 10, 32)
	return int(v), err // fits int: parsed at 32 bits
}
