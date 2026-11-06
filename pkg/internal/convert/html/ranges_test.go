package html

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestParsePageRanges_ChromiumGrammar(t *testing.T) {
	t.Parallel()
	open := uint64(math.MaxUint32)
	tests := []struct {
		spec string
		want []pageRange
	}{
		{"", nil},
		{" , ,", nil},
		{"3", []pageRange{{3, 3}}},
		{"1-5, 8, 11-13", []pageRange{{1, 5}, {8, 8}, {11, 13}}},
		{" 5 - 6 ", []pageRange{{5, 6}}},
		{"1--3", []pageRange{{1, 3}}},
		{"+2-+4", []pageRange{{2, 4}}},
		{"-", []pageRange{{1, open}}},
		{"-4", []pageRange{{1, 4}}},
		{"11-", []pageRange{{11, open}}},
		{"3,1", []pageRange{{3, 3}, {1, 1}}},
		{"4294967295", []pageRange{{open, open}}},
		{"\t2\n", []pageRange{{2, 2}}},
	}
	for _, tt := range tests {
		got, err := parsePageRanges(tt.spec)
		if err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("parsePageRanges(%q) = %v, %v; want %v", tt.spec, got, err, tt.want)
		}
	}
}

func TestParsePageRanges_RefusesWhatChromiumRefuses(t *testing.T) {
	t.Parallel()
	for _, spec := range []string{
		"a", "1a", "1.5", "-1-2", "1-2-3", "1-a", "0", "0-3", "3-2", "- 3", "3 -", "4294967296",
		"1,,x", "++1", "1e3", "١", "1 2",
	} {
		if _, err := parsePageRanges(spec); !errors.Is(err, ErrInvalidOptions) {
			t.Errorf("parsePageRanges(%q): %v, want ErrInvalidOptions", spec, err)
		}
	}
}

func TestSelectPages_SortsMergesAndClips(t *testing.T) {
	t.Parallel()
	tests := []struct {
		spec string
		n    int
		want []int
	}{
		{"", 3, []int{0, 1, 2}},
		{"2-3", 12, []int{1, 2}},
		{"11-, 3,1-2,  5 - 6", 12, []int{0, 1, 2, 4, 5, 10, 11}},
		{"3,1,3,2-3", 5, []int{0, 1, 2}},
		{"2-20", 4, []int{1, 2, 3}},
		{"9, 2", 4, []int{1}},
		{"-", 2, []int{0, 1}},
	}
	for _, tt := range tests {
		r, err := parsePageRanges(tt.spec)
		if err != nil {
			t.Fatal(err)
		}
		got, err := selectPages(r, tt.n)
		if err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("selectPages(%q, %d) = %v, %v; want %v", tt.spec, tt.n, got, err, tt.want)
		}
	}
	r, _ := parsePageRanges("5-9, 20")
	if _, err := selectPages(r, 4); !errors.Is(err, ErrInvalidOptions) || !strings.Contains(err.Error(), "4 pages") {
		t.Errorf("a selection past the last page: %v, want ErrInvalidOptions", err)
	}
}

func FuzzParsePageRanges(f *testing.F) {
	for _, s := range []string{"", "1-5, 8, 11-13", "-", "-3", "4-", "1--3", "+2", "4294967295", "3-2", "a,1"} {
		f.Add(s, 5)
	}
	f.Fuzz(func(t *testing.T, spec string, n int) {
		n = max(min(n, 1000), 0)
		ranges, err := parsePageRanges(spec)
		if err != nil {
			if !errors.Is(err, ErrInvalidOptions) {
				t.Fatalf("error %v does not wrap ErrInvalidOptions", err)
			}
			return
		}
		for _, r := range ranges {
			if r.from < 1 || r.from > r.to || r.to > math.MaxUint32 {
				t.Fatalf("range %v out of bounds", r)
			}
		}
		pages, err := selectPages(ranges, n)
		if err != nil {
			return
		}
		for i, p := range pages {
			if p < 0 || p >= n || i > 0 && p <= pages[i-1] {
				t.Fatalf("pages %v not ascending within [0, %d)", pages, n)
			}
		}
	})
}
