package intconv

import (
	"math"
	"testing"
)

func TestClamp(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		got  int
		want int
	}{
		{"zero", Clamp(int64(0)), 0},
		{"negative int64", Clamp(int64(-7)), -7},
		{"int64 at MaxInt", Clamp(int64(math.MaxInt)), math.MaxInt},
		{"int64 at MinInt", Clamp(int64(math.MinInt)), math.MinInt},
		{"int64 max", Clamp(int64(math.MaxInt64)), math.MaxInt},
		{"int64 min", Clamp(int64(math.MinInt64)), math.MinInt},
		{"int64 past 2^32", Clamp(int64(1<<32 + 5)), min(1<<32+5, math.MaxInt)},
		{"uint32 max", Clamp(uint32(math.MaxUint32)), min(math.MaxUint32, math.MaxInt)},
		{"uint64 max", Clamp(uint64(math.MaxUint64)), math.MaxInt},
		{"uint at MaxInt", Clamp(uint(math.MaxInt)), math.MaxInt},
		{"uint max", Clamp(uint(math.MaxUint)), math.MaxInt},
	} {
		if tc.got != tc.want {
			t.Errorf("%s: Clamp = %d, want %d", tc.name, tc.got, tc.want)
		}
	}
}
