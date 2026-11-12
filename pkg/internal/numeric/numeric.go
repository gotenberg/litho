package numeric

import "math"

// Finite reports whether v is a number: neither NaN nor an infinity.
func Finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// Positive reports whether v is greater than zero. It is false for NaN, so
// a caller that refuses what is not positive refuses NaN with it. It is
// true for the positive infinity: see [PositiveFinite].
func Positive(v float64) bool { return v > 0 }

// PositiveFinite reports whether v is greater than zero and finite: a
// length, a size or a scale a layout can use.
func PositiveFinite(v float64) bool { return v > 0 && !math.IsInf(v, 1) }

// NonNegative reports whether v is zero or greater. It is false for NaN.
func NonNegative(v float64) bool { return v >= 0 }
