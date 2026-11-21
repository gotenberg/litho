package pdf

import (
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"
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
		// The result keeps the order of the specification and lists each
		// page once, where the specification first names it.
		{"3,1", 5, []int{3, 1}, false},
		{"1,1", 5, []int{1}, false},
		{"3,1-4", 5, []int{3, 1, 2, 4}, false},
		{"2-4,1-", 5, []int{2, 3, 4, 1, 5}, false},
		{"4-,odd", 5, []int{4, 5, 1, 3}, false},
		{"even,odd,last", 5, []int{2, 4, 1, 3, 5}, false},
		{"1-,1-,1-", 5, []int{1, 2, 3, 4, 5}, false},
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
		{"1,1,!2", 5, []int{1}, false},
		{"1-,!2-4,1-", 5, []int{1, 5}, false},
		{"!odd,!odd,1-", 5, []int{2, 4}, false},
		// Keywords are ordinary parts.
		{"odd,!1", 5, []int{3, 5}, false},
		{"1-3,last", 5, []int{1, 2, 3, 5}, false},
		{"odd,last", 5, []int{1, 3, 5}, false},
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

// TestParsePageRanges_RejectsABadLaterPartWithoutExpanding pins that every
// part is validated before any is expanded: the parts ahead of an invalid one
// cost nothing.
func TestParsePageRanges_RejectsABadLaterPartWithoutExpanding(t *testing.T) {
	// Expanding the parts ahead allocates hundreds of MiB, validating them
	// one record each.
	const maxBytes = 4 << 20

	for _, last := range []string{"0-9223372036854775807", "9223372036854775808", "abc", "5001", "!0"} {
		spec := strings.Repeat("1-,", 10_000) + last
		alloc, got, err := parsePageRangesAlloc(t, spec, 5_000)
		if !errors.Is(err, ErrInvalidPageRange) {
			t.Fatalf("ParsePageRanges(10000 x \"1-\" + %q, 5000) = %d pages, %v, want ErrInvalidPageRange", last, len(got), err)
		}
		if alloc > maxBytes {
			t.Errorf("ParsePageRanges(10000 x \"1-\" + %q, 5000) allocated %d bytes, want under %d", last, alloc, maxBytes)
		}
	}
}

// TestParsePageRanges_RepeatedPartsStayBounded pins that the result holds
// each page once and that its cost does not grow with the number of parts
// times the page count.
func TestParsePageRanges_RepeatedPartsStayBounded(t *testing.T) {
	const (
		pageCount = 5_000
		// Expanding every part allocates hundreds of MiB.
		maxBytes = 4 << 20
	)

	var shifted strings.Builder
	for i := 1; i <= pageCount; i++ {
		fmt.Fprintf(&shifted, "%d-,", i)
	}
	specs := map[string]string{
		"10000 x 1-":   strings.TrimSuffix(strings.Repeat("1-,", 10_000), ","),
		"10000 x odd":  strings.TrimSuffix(strings.Repeat("odd,even,", 5_000), ","),
		"10000 x !1-2": strings.Repeat("!1-2,", 10_000) + "1-",
		"1- to 5000-":  strings.TrimSuffix(shifted.String(), ","),
	}
	for name, spec := range specs {
		t.Run(name, func(t *testing.T) {
			alloc, got, err := parsePageRangesAlloc(t, spec, pageCount)
			if err != nil {
				t.Fatalf("ParsePageRanges(%s, %d) err = %v, want nil", name, pageCount, err)
			}
			if len(got) > pageCount {
				t.Fatalf("ParsePageRanges(%s, %d) = %d pages, want at most %d", name, pageCount, len(got), pageCount)
			}
			seen := make(map[int]bool, len(got))
			for _, p := range got {
				if seen[p] {
					t.Fatalf("ParsePageRanges(%s, %d) lists page %d twice", name, pageCount, p)
				}
				seen[p] = true
			}
			if alloc > maxBytes {
				t.Errorf("ParsePageRanges(%s, %d) allocated %d bytes, want under %d", name, pageCount, alloc, maxBytes)
			}
		})
	}
}

// TestValidatePageRanges_AgreesWithParsePageRanges pins that a valid
// specification passes and that a malformed or out-of-range one fails with
// the error ParsePageRanges returns, while a specification that only selects
// no page passes.
func TestValidatePageRanges_AgreesWithParsePageRanges(t *testing.T) {
	valid := []struct {
		spec      string
		pageCount int
	}{
		{"1", 5}, {"1-3,5", 5}, {" 1 - 3 , 5 ", 5}, {"EVEN", 6}, {"odd,!1", 5},
		{"last", 5}, {"3-", 5}, {"-3", 5}, {"1,1", 5}, {"!2,3,1", 5},
		// Valid parts selecting no page.
		{"1-3,!1-3", 5}, {"even", 1}, {"!1", 1},
	}
	for _, tt := range valid {
		err := ValidatePageRanges(tt.spec, tt.pageCount)
		if err != nil {
			t.Errorf("ValidatePageRanges(%q, %d) = %v, want nil", tt.spec, tt.pageCount, err)
		}
	}

	invalid := []struct {
		spec      string
		pageCount int
	}{
		{"", 5}, {" ", 5}, {"0", 5}, {"6", 5}, {"3-1", 5}, {"1,,2", 5}, {"!", 5},
		{"-", 5}, {"1-last", 5}, {"abc", 5}, {"!0", 5}, {"last", 0},
		{"0-9223372036854775807", 5}, {"9223372036854775808", 5},
		{strings.Repeat("1-,", 10_000) + "5001", 5_000},
	}
	for _, tt := range invalid {
		err := ValidatePageRanges(tt.spec, tt.pageCount)
		if !errors.Is(err, ErrInvalidPageRange) {
			t.Errorf("ValidatePageRanges(%.40q, %d) = %v, want ErrInvalidPageRange", tt.spec, tt.pageCount, err)
			continue
		}
		_, parseErr := ParsePageRanges(tt.spec, tt.pageCount)
		if parseErr == nil || parseErr.Error() != err.Error() {
			t.Errorf("ValidatePageRanges(%.40q, %d) = %v, want the ParsePageRanges error %v", tt.spec, tt.pageCount, err, parseErr)
		}
	}
}

// TestValidatePageRanges_ExpandsNothing pins that validating a long
// specification costs no page set: its allocations do not grow with
// pageCount.
func TestValidatePageRanges_ExpandsNothing(t *testing.T) {
	spec := strings.TrimSuffix(strings.Repeat("1-,!2-,", 10_000), ",")

	allocs := testing.AllocsPerRun(5, func() {
		err := ValidatePageRanges(spec, 1<<30)
		if err != nil {
			t.Fatalf("ValidatePageRanges = %v, want nil", err)
		}
	})
	if allocs > 0 {
		t.Errorf("ValidatePageRanges allocated %v times per run, want 0", allocs)
	}
}
