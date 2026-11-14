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

// TestAtoi pins that Atoi reads a value past 2^31 on every platform and
// saturates it at math.MaxInt, and that Atoi32 refuses it with the bound it
// passes: the results are the same where int is 32 bits and where it is 64.
func TestAtoi(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		in       string
		atoi     int
		atoiErr  bool
		atoi32   int
		atoi32Ok bool
	}{
		{"0", 0, false, 0, true},
		{"+42", 42, false, 42, true},
		{"-2147483648", math.MinInt32, false, math.MinInt32, true},
		{"2147483647", math.MaxInt32, false, math.MaxInt32, true},
		{"2147483648", min(2147483648, math.MaxInt), false, math.MaxInt32, false},
		{"-3000000000", max(-3000000000, math.MinInt), false, math.MinInt32, false},
		{"9223372036854775807", math.MaxInt, false, math.MaxInt32, false},
		{"9223372036854775808", math.MaxInt, true, math.MaxInt32, false},
		{"1.5", 0, true, 0, false},
		{" 7", 0, true, 0, false},
		{"", 0, true, 0, false},
	} {
		got, err := Atoi(tc.in)
		if got != tc.atoi || (err != nil) != tc.atoiErr {
			t.Errorf("Atoi(%q) = %d, %v; want %d, error %t", tc.in, got, err, tc.atoi, tc.atoiErr)
		}
		got32, err := Atoi32(tc.in)
		if got32 != tc.atoi32 || (err == nil) != tc.atoi32Ok {
			t.Errorf("Atoi32(%q) = %d, %v; want %d, ok %t", tc.in, got32, err, tc.atoi32, tc.atoi32Ok)
		}
	}
}
