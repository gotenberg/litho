package odt

import "testing"

// TestFormatNumber checks the number formats against what LibreOffice
// prints for them (the pinned image: "01." for the first item of a list
// in "01, 02, 03, ...", the Old Hungarian signs of one and five).
func TestFormatNumber(t *testing.T) {
	cases := []struct {
		n      int
		format string
		want   string
	}{
		{3, "1", "3"},
		{3, "", ""},
		{28, "a", "ab"},
		{28, "A", "AB"},
		{14, "i", "xiv"},
		{14, "I", "XIV"},
		{1, "01, 02, 03, ...", "01"},
		{12, "01, 02, 03, ...", "12"},
		{123, "01, 02, 03, ...", "123"},
		{7, "001, 002, 003, ...", "007"},
		{7, "0001, 0002, 0003, ...", "0007"},
		{7, "00001, 00002, 00003, ...", "00007"},
		{1, "𐳺, 𐳺𐳺, 𐳺𐳺𐳺, ...", "𐳺"},
		{4, "𐳺, 𐳺𐳺, 𐳺𐳺𐳺, ...", "𐳺𐳺𐳺𐳺"},
		{5, "𐳺, 𐳺𐳺, 𐳺𐳺𐳺, ...", "𐳻"},
		{1166, "𐳺, 𐳺𐳺, 𐳺𐳺𐳺, ...", "𐳿𐳾𐳽𐳼𐳻𐳺"},
		{10000, "𐳺, 𐳺𐳺, 𐳺𐳺𐳺, ...", "10000"},
		{0, "𐳺, 𐳺𐳺, 𐳺𐳺𐳺, ...", "0"},
	}
	for _, tc := range cases {
		if got := formatNumber(tc.n, tc.format); got != tc.want {
			t.Errorf("formatNumber(%d, %q) = %q, want %q", tc.n, tc.format, got, tc.want)
		}
	}
}
