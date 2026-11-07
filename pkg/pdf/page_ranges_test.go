package pdf

import (
	"errors"
	"runtime"
	"slices"
	"testing"
)

func TestParsePageRanges(t *testing.T) {
	tests := []struct {
		spec      string
		pageCount int
		want      []int
		wantErr   bool
	}{
		{"1", 5, []int{1}, false},
		{"1-3", 5, []int{1, 2, 3}, false},
		{"1,3,5", 5, []int{1, 3, 5}, false},
		{"1-3,5", 5, []int{1, 2, 3, 5}, false},
		{" 1 - 3 , 5 ", 5, []int{1, 2, 3, 5}, false},
		{"even", 6, []int{2, 4, 6}, false},
		{"EVEN", 6, []int{2, 4, 6}, false},
		{"odd", 6, []int{1, 3, 5}, false},
		{"last", 5, []int{5}, false},
		{"0", 5, nil, true},
		{"6", 5, nil, true},
		{"3-1", 5, nil, true},
		{"", 5, nil, true},
		{"abc", 5, nil, true},
		{"1,,2", 5, nil, true},
		// The result keeps order and duplicates: the page operations treat it
		// as a set, Split honors both.
		{"3,1", 5, []int{3, 1}, false},
		{"1,1", 5, []int{1, 1}, false},
		// Open ranges run to the last page or from the first.
		{"2-", 5, []int{2, 3, 4, 5}, false},
		{"5-", 5, []int{5}, false},
		{"-2", 5, []int{1, 2}, false},
		{" 2 - ", 5, []int{2, 3, 4, 5}, false},
		{" - 2", 5, []int{1, 2}, false},
		// Exclusions remove pages wherever they appear; without an inclusion
		// the selection starts from every page.
		{"!1", 5, []int{2, 3, 4, 5}, false},
		{" !1 ", 5, []int{2, 3, 4, 5}, false},
		{"! 1", 5, []int{2, 3, 4, 5}, false},
		{"!3-", 5, []int{1, 2}, false},
		{"!last", 5, []int{1, 2, 3, 4}, false},
		{"!LAST", 5, []int{1, 2, 3, 4}, false},
		{"!even", 5, []int{1, 3, 5}, false},
		{"!1,!2", 3, []int{3}, false},
		{"3,1,!2", 5, []int{3, 1}, false},
		{"!2,3,1", 5, []int{3, 1}, false},
		{"2-,!4", 5, []int{2, 3, 5}, false},
		{"-2,!1", 5, []int{2}, false},
		{"1-2,!3", 5, []int{1, 2}, false},
		{"1,1,!2", 5, []int{1, 1}, false},
		// Keywords are ordinary parts.
		{"odd,!1", 5, []int{3, 5}, false},
		{"1-3,last", 5, []int{1, 2, 3, 5}, false},
		{"odd,last", 5, []int{1, 3, 5, 5}, false},
		{"even,1", 5, []int{2, 4, 1}, false},
		// Malformed parts.
		{"-", 5, nil, true},
		{" - ", 5, nil, true},
		{"!", 5, nil, true},
		{"!!1", 5, nil, true},
		{"!-", 5, nil, true},
		{"1,!", 5, nil, true},
		{"1-last", 5, nil, true},
		{"last-", 5, nil, true},
		{"n1", 5, nil, true},
		{"odd,,1", 5, nil, true},
		{"1-a", 5, nil, true},
		// Every named page lies in [1, pageCount], excluded or open.
		{"6-", 5, nil, true},
		{"-6", 5, nil, true},
		{"!6", 5, nil, true},
		{"!0", 5, nil, true},
		{"!3-1", 5, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			got, err := ParsePageRanges(tt.spec, tt.pageCount)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParsePageRanges(%q, %d) err=%v, wantErr=%t", tt.spec, tt.pageCount, err, tt.wantErr)
			}
			if err != nil {
				if !errors.Is(err, ErrInvalidPageRange) {
					t.Errorf("ParsePageRanges(%q, %d) err=%v, want it to wrap ErrInvalidPageRange", tt.spec, tt.pageCount, err)
				}
				return
			}
			if !slices.Equal(got, tt.want) {
				t.Errorf("ParsePageRanges(%q, %d) = %v, want %v", tt.spec, tt.pageCount, got, tt.want)
			}
		})
	}
}

// TestParsePageRanges_EmptySelectionIsAnError pins that a specification
// selecting nothing fails: an empty page list reaches the page operations as
// "every page".
func TestParsePageRanges_EmptySelectionIsAnError(t *testing.T) {
	tests := []struct {
		spec      string
		pageCount int
	}{
		{"even", 1},
		{"even", 0},
		{"odd", 0},
		{"last", 0},
		{"!1", 1},
		{"!last", 1},
		{"1-3,!1-3", 5},
		{"!odd,!even", 5},
		{"!1-", 5},
		{"!-5", 5},
		{"2,!2", 5},
		{"!1", 0},
	}
	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			got, err := ParsePageRanges(tt.spec, tt.pageCount)
			if !errors.Is(err, ErrInvalidPageRange) {
				t.Fatalf("ParsePageRanges(%q, %d) = %v, %v, want ErrInvalidPageRange", tt.spec, tt.pageCount, got, err)
			}
		})
	}
}

// TestParsePageRanges_OutOfRangeNamesTheFirstEndpoint pins which page an
// out-of-range rejection names: the first offending endpoint in list order.
func TestParsePageRanges_OutOfRangeNamesTheFirstEndpoint(t *testing.T) {
	tests := []struct {
		spec      string
		pageCount int
		want      string
	}{
		{"0-3", 5, "invalid page range: page 0 out of range [1, 5]"},
		{"1-6", 5, "invalid page range: page 6 out of range [1, 5]"},
		{"0-6", 5, "invalid page range: page 0 out of range [1, 5]"},
		{"1,9,2", 5, "invalid page range: page 9 out of range [1, 5]"},
		{"7-9,1", 5, "invalid page range: page 7 out of range [1, 5]"},
		{"1-3,0", 5, "invalid page range: page 0 out of range [1, 5]"},
		{"3-1", 5, "invalid page range: range 3-1 starts after it ends"},
		{"6-", 5, "invalid page range: page 6 out of range [1, 5]"},
		{"-6", 5, "invalid page range: page 6 out of range [1, 5]"},
		{"0-", 5, "invalid page range: page 0 out of range [1, 5]"},
		{"!6", 5, "invalid page range: page 6 out of range [1, 5]"},
		{"!0-6", 5, "invalid page range: page 0 out of range [1, 5]"},
		{"1,!9,7", 5, "invalid page range: page 9 out of range [1, 5]"},
		{"!1", 1, `invalid page range: "!1" selects no pages in a 1-page document`},
		{"1-3,!1-3", 5, `invalid page range: "1-3,!1-3" selects no pages in a 5-page document`},
	}
	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			_, err := ParsePageRanges(tt.spec, tt.pageCount)
			if err == nil || err.Error() != tt.want {
				t.Errorf("ParsePageRanges(%q, %d) err = %v, want %q", tt.spec, tt.pageCount, err, tt.want)
			}
		})
	}
}

// parsePageRangesAlloc reports the bytes one ParsePageRanges call allocates.
// TotalAlloc is process-wide, so the tests using it must not run in parallel.
func parsePageRangesAlloc(t *testing.T, spec string, pageCount int) (uint64, []int, error) {
	t.Helper()

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	got, err := ParsePageRanges(spec, pageCount)
	runtime.ReadMemStats(&after)

	return after.TotalAlloc - before.TotalAlloc, got, err
}

// TestParsePageRanges_RejectsAHugeRangeWithoutExpandingIt pins that an
// out-of-range range is rejected before it is expanded. Expanding first
// allocates hundreds of MiB for "1-10000000", far above the 1 MiB threshold.
func TestParsePageRanges_RejectsAHugeRangeWithoutExpandingIt(t *testing.T) {
	const maxBytes = 1 << 20

	for _, spec := range []string{"1-10000000", "!1-10000000", "10000000-", "-10000000", "!10000000-"} {
		alloc, got, err := parsePageRangesAlloc(t, spec, 5)
		if err == nil {
			t.Fatalf("ParsePageRanges(%q, 5) = %v, want an out-of-range error", spec, got)
		}
		if alloc > maxBytes {
			t.Errorf("ParsePageRanges(%q, 5) allocated %d bytes, want under %d", spec, alloc, maxBytes)
		}
	}
}

// TestParsePageRanges_LegitimateSpecStaysCheap is the control for the test
// above: a function that fails immediately would pass it too.
func TestParsePageRanges_LegitimateSpecStaysCheap(t *testing.T) {
	const maxBytes = 1 << 20

	alloc, got, err := parsePageRangesAlloc(t, "1-5", 5)
	if err != nil {
		t.Fatalf("ParsePageRanges(\"1-5\", 5) err = %v, want nil", err)
	}
	if len(got) != 5 {
		t.Fatalf("ParsePageRanges(\"1-5\", 5) = %v, want five pages", got)
	}
	if alloc > maxBytes {
		t.Errorf("ParsePageRanges(\"1-5\", 5) allocated %d bytes, want under %d", alloc, maxBytes)
	}
}
