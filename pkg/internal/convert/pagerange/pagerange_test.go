package pagerange

import (
	"errors"
	"math"
	"reflect"
	"strings"
	"testing"
)

func TestParse_ChromiumGrammar(t *testing.T) {
	t.Parallel()
	open := uint64(math.MaxUint32)
	tests := []struct {
		spec string
		want []Range
	}{
		{"", nil},
		{" , ,", nil},
		{"3", []Range{{3, 3}}},
		{"1-5, 8, 11-13", []Range{{1, 5}, {8, 8}, {11, 13}}},
		{" 5 - 6 ", []Range{{5, 6}}},
		{"1--3", []Range{{1, 3}}},
		{"+2-+4", []Range{{2, 4}}},
		{"-", []Range{{1, open}}},
		{"-4", []Range{{1, 4}}},
		{"11-", []Range{{11, open}}},
		{"3,1", []Range{{3, 3}, {1, 1}}},
		{"4294967295", []Range{{open, open}}},
		{"\t2\n", []Range{{2, 2}}},
	}
	for _, tt := range tests {
		got, err := Parse(tt.spec)
		if err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Parse(%q) = %v, %v; want %v", tt.spec, got, err, tt.want)
		}
	}
}

func TestParse_RefusesWhatChromiumRefuses(t *testing.T) {
	t.Parallel()
	for _, spec := range []string{
		"a", "1a", "1.5", "-1-2", "1-2-3", "1-a", "0", "0-3", "3-2", "- 3", "3 -", "4294967296",
		"1,,x", "++1", "1e3", "١", "1 2",
	} {
		if _, err := Parse(spec); !errors.Is(err, ErrInvalid) {
			t.Errorf("Parse(%q): %v, want ErrInvalid", spec, err)
		}
	}
}

func TestParse_CapsTheRanges(t *testing.T) {
	t.Parallel()
	atCap := strings.Repeat("1,", MaxRanges)
	if r, err := Parse(atCap); err != nil || len(r) != MaxRanges {
		t.Errorf("%d ranges: %d, %v; want them all", MaxRanges, len(r), err)
	}
	if _, err := Parse(atCap + "1"); !errors.Is(err, ErrInvalid) {
		t.Errorf("%d ranges: %v, want ErrInvalid", MaxRanges+1, err)
	}
}

func TestSelect_SortsMergesAndClips(t *testing.T) {
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
		r, err := Parse(tt.spec)
		if err != nil {
			t.Fatal(err)
		}
		got, err := Select(r, tt.n)
		if err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Select(%q, %d) = %v, %v; want %v", tt.spec, tt.n, got, err, tt.want)
		}
	}
	r, _ := Parse("5-9, 20")
	if _, err := Select(r, 4); !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "4 pages") {
		t.Errorf("a selection past the last page: %v, want ErrInvalid", err)
	}
}

func FuzzParse(f *testing.F) {
	for _, s := range []string{"", "1-5, 8, 11-13", "-", "-3", "4-", "1--3", "+2", "4294967295", "3-2", "a,1"} {
		f.Add(s, 5)
	}
	f.Fuzz(func(t *testing.T, spec string, n int) {
		n = max(min(n, 1000), 0)
		ranges, err := Parse(spec)
		if err != nil {
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("error %v does not wrap ErrInvalid", err)
			}
			return
		}
		if len(ranges) > MaxRanges {
			t.Fatalf("%d ranges past the cap", len(ranges))
		}
		for _, r := range ranges {
			if r.From < 1 || r.From > r.To || r.To > math.MaxUint32 {
				t.Fatalf("range %v out of bounds", r)
			}
		}
		pages, err := Select(ranges, n)
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
