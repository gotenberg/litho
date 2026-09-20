package main

import (
	"runtime"
	"testing"
)

// parsePagesAlloc reports the bytes one parsePages call allocates.
//
// TotalAlloc is process-wide and monotonic, so it counts every allocation the
// call makes even when the result is discarded, which a len() assertion cannot
// see: the defect this guards produced a correct error and a huge slice at the
// same time. These tests therefore must not run with t.Parallel.
func parsePagesAlloc(t *testing.T, spec string, pageCount int) (uint64, []int, error) {
	t.Helper()

	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	got, err := parsePages(spec, pageCount)
	runtime.ReadMemStats(&after)

	return after.TotalAlloc - before.TotalAlloc, got, err
}

// TestParsePages_RejectsAHugeRangeWithoutExpandingIt pins that an out-of-range
// range costs nothing to reject.
//
// parsePages used to expand every part and validate afterwards, so the endpoint
// alone decided the allocation: "1-10000000" against a five-page document
// measured 469 MiB and 56 ms before returning "page 6 out of range [1, 5]".
// strconv.Atoi accepts up to 2^63-1, so "1-2000000000" asks for roughly 94 GiB.
// litho's page specs reach it from a CLI flag and from the HTTP surface that
// wraps it, which is what makes an unbounded, always-discarded allocation a
// denial of service rather than a waste.
//
// The threshold is 1 MiB against a measured 469 MiB, a 469x margin, so this
// fails decisively on the old code instead of drifting near a boundary.
func TestParsePages_RejectsAHugeRangeWithoutExpandingIt(t *testing.T) {
	const (
		spec     = "1-10000000"
		maxBytes = 1 << 20
	)

	alloc, got, err := parsePagesAlloc(t, spec, 5)
	if err == nil {
		t.Fatalf("parsePages(%q, 5) = %v, want an out-of-range error", spec, got)
	}
	if alloc > maxBytes {
		t.Errorf("parsePages(%q, 5) allocated %d bytes (%.1f MiB), want under %d: "+
			"the range is being expanded before its bounds are checked",
			spec, alloc, float64(alloc)/(1<<20), maxBytes)
	}
}

// TestParsePages_LegitimateSpecStaysCheap is the control for the test above.
//
// Without it, the allocation assertion would also pass if parsePages stopped
// working entirely, since a function that returns an error immediately
// allocates nothing either.
func TestParsePages_LegitimateSpecStaysCheap(t *testing.T) {
	const maxBytes = 1 << 20

	alloc, got, err := parsePagesAlloc(t, "1-5", 5)
	if err != nil {
		t.Fatalf("parsePages(\"1-5\", 5) err = %v, want nil", err)
	}
	if len(got) != 5 {
		t.Fatalf("parsePages(\"1-5\", 5) = %v, want five pages", got)
	}
	if alloc > maxBytes {
		t.Errorf("parsePages(\"1-5\", 5) allocated %d bytes, want under %d", alloc, maxBytes)
	}
}

// TestParsePages_OutOfRangeMessagesAreUnchanged pins the exact text of every
// out-of-range rejection.
//
// Moving the bounds check ahead of the expansion is only safe if it names the
// same page the post-expansion scan did. That scan reported the first offender
// in expansion order, which is always an endpoint, so checking parts in list
// order with endpoints first reproduces it. Each case below was run against the
// previous implementation and returned exactly this string.
func TestParsePages_OutOfRangeMessagesAreUnchanged(t *testing.T) {
	tests := []struct {
		spec      string
		pageCount int
		want      string
	}{
		{"0-3", 5, "page 0 out of range [1, 5]"},
		{"1-6", 5, "page 6 out of range [1, 5]"},
		{"0-6", 5, "page 0 out of range [1, 5]"},
		{"1,9,2", 5, "page 9 out of range [1, 5]"},
		{"7-9,1", 5, "page 7 out of range [1, 5]"},
		{"1-3,0", 5, "page 0 out of range [1, 5]"},
		{"0", 5, "page 0 out of range [1, 5]"},
		{"6", 5, "page 6 out of range [1, 5]"},
		{"3-1", 5, "invalid range 3-1: start > end"},
	}

	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			_, err := parsePages(tt.spec, tt.pageCount)
			if err == nil {
				t.Fatalf("parsePages(%q, %d) returned no error, want %q", tt.spec, tt.pageCount, tt.want)
			}
			if err.Error() != tt.want {
				t.Errorf("parsePages(%q, %d) err = %q, want %q", tt.spec, tt.pageCount, err, tt.want)
			}
		})
	}
}
