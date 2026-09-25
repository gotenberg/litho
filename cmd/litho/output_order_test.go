package main

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/pdf"
)

// outputOrderRuns is how many times each test renders the same output. Go
// randomizes map iteration per range statement, so text rendered by ranging a
// map of three or more entries comes out in a different order within a few
// runs.
const outputOrderRuns = 16

// TestProfileNamesAreStable pins that the profile lists the usage and error
// messages print come out in the same order on every call, each profile family
// sorted (CONTRIBUTING "Deterministic output"). They were built by ranging the
// profile tables, so `litho remediate` printed its profiles shuffled.
func TestProfileNamesAreStable(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		names func() []string
	}{
		{"validate", validateProfileNames},
		{"remediate", remediateProfileNames},
	} {
		first := tc.names()
		pdfa := slices.DeleteFunc(slices.Clone(first), func(n string) bool { return !strings.HasPrefix(n, "PDF/A-") })
		if len(pdfa) < 2 || !slices.IsSorted(pdfa) {
			t.Errorf("%s: PDF/A profiles %v, want at least two, sorted", tc.name, pdfa)
		}
		for run := range outputOrderRuns {
			if got := tc.names(); !slices.Equal(got, first) {
				t.Fatalf("%s: run %d listed %v, first run listed %v", tc.name, run, got, first)
			}
		}
	}
}

// TestRedactReportPagesAscend pins that the text report names the pages whose
// fonts were fully removed in ascending page order on every run. It ranged
// the report's map, so the lines came out shuffled.
func TestRedactReportPagesAscend(t *testing.T) {
	t.Parallel()

	report := &pdf.RedactReport{FontsFullyRemovedOnPage: map[int][]int{
		4: {40}, 1: {10}, 3: {30}, 2: {20}, 5: {50},
	}}
	want := []string{"Page 1 ", "Page 2 ", "Page 3 ", "Page 4 ", "Page 5 "}
	for run := range outputOrderRuns {
		var stdout, stderr bytes.Buffer
		if code := printRedactReport(&stdout, &stderr, "text", "in.pdf", report); code != exitOK {
			t.Fatalf("run %d: exit %d, stderr %q", run, code, stderr.String())
		}
		var got []string
		for line := range strings.Lines(stdout.String()) {
			if strings.HasPrefix(line, "Page ") {
				got = append(got, line[:len("Page 1 ")])
			}
		}
		if !slices.Equal(got, want) {
			t.Fatalf("run %d: page lines %q, want %q", run, got, want)
		}
	}
}
