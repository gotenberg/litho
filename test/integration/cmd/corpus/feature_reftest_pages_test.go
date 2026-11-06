package main

import (
	"image"
	"slices"
	"testing"
)

func TestParseReftestPages(t *testing.T) {
	t.Parallel()

	cases := []struct {
		spec string
		want []reftestPageRange
		ok   bool
	}{
		{"", nil, true},
		{"2", []reftestPageRange{{2, 2}}, true},
		{" 1 , 3-5", []reftestPageRange{{1, 1}, {3, 5}}, true},
		{"2-", []reftestPageRange{{2, maxReftestPage}}, true},
		{"-3", []reftestPageRange{{1, 3}}, true},
		{"0", nil, false},
		{"3-1", nil, false},
		{"a", nil, false},
		{"1,,2", nil, false},
		{"1-2-3", nil, false},
		{"99999", nil, false},
	}
	for _, tc := range cases {
		got, err := parseReftestPages(tc.spec)
		if (err == nil) != tc.ok || !slices.Equal(got, tc.want) {
			t.Errorf("parseReftestPages(%q) = %v, %v; want %v, ok %v", tc.spec, got, err, tc.want, tc.ok)
		}
	}
}

func TestSelectReftestPages_KeepsTheNamedPagesInOrder(t *testing.T) {
	t.Parallel()

	pages := make([]image.Image, 4)
	for i := range pages {
		pages[i] = image.NewGray(image.Rect(0, 0, i+1, 1))
	}
	width := func(ps []image.Image) []int {
		var out []int
		for _, p := range ps {
			out = append(out, p.Bounds().Dx())
		}
		return out
	}
	cases := []struct {
		spec string
		want []int
	}{
		{"", []int{1, 2, 3, 4}},
		{"2", []int{2}},
		{"4,1-2", []int{1, 2, 4}},
		{"3-", []int{3, 4}},
		{"2,2", []int{2}},
		{"9", nil},
	}
	for _, tc := range cases {
		ranges, err := parseReftestPages(tc.spec)
		if err != nil {
			t.Fatal(err)
		}
		if got := width(selectReftestPages(pages, ranges)); !slices.Equal(got, tc.want) {
			t.Errorf("%q: pages %v, want %v", tc.spec, got, tc.want)
		}
	}
}
