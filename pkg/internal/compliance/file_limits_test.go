package compliance

import "testing"

func TestDecodedNameLength(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		want int
	}{
		{"Simple", 6},
		{"", 0},
		{"A#20B", 3},           // One hex escape -> 3 decoded bytes
		{"#48#65#6C#6C#6F", 5}, // "Hello" all escaped
		{"NoEscape", 8},
		{"#GG", 3},       // Invalid hex, treated as 3 literal bytes
		{"Trail#2", 7},   // Incomplete escape at end, treated as literals
		{"#41#42#43", 3}, // "ABC"
		{"a#20b#20c", 5}, // "a b c"
		{"#", 1},         // Lone # is literal
		{"##", 2},        // Two lone #'s
		{"#41", 1},       // Single valid escape
		{"#41X#42", 3},   // Two escapes with literal between
		{"a#zz", 4},      // Invalid hex chars after #
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := decodedNameLength(tc.name); got != tc.want {
				t.Errorf("decodedNameLength(%q) = %d, want %d", tc.name, got, tc.want)
			}
		})
	}
}

func TestIsHexDigit(t *testing.T) {
	t.Parallel()
	tests := []struct {
		c    byte
		want bool
	}{
		{'0', true},
		{'9', true},
		{'5', true},
		{'a', true},
		{'f', true},
		{'c', true},
		{'A', true},
		{'F', true},
		{'C', true},
		{'g', false},
		{'G', false},
		{'z', false},
		{'Z', false},
		{' ', false},
		{'/', false},
		{':', false},
		{0, false},
		{0xFF, false},
	}
	for _, tc := range tests {
		t.Run(string(tc.c), func(t *testing.T) {
			t.Parallel()
			if got := isHexDigit(tc.c); got != tc.want {
				t.Errorf("isHexDigit(%q) = %v, want %v", tc.c, got, tc.want)
			}
		})
	}
}
