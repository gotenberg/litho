package heuristics

import "testing"

func TestMarginEdge(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		minY, maxY float64
		want       Edge
		wantOffset float64
	}{
		{name: "running head", minY: 760, maxY: 760, want: EdgeTop, wantOffset: 32},
		{name: "page number", minY: 30, maxY: 30, want: EdgeBottom, wantOffset: 30},
		{name: "body", minY: 400, maxY: 400, want: EdgeNone},
		{name: "block reaching out of the band", minY: 600, maxY: 760, want: EdgeNone},
		{name: "outside the box", minY: 800, maxY: 800, want: EdgeNone},
		{name: "below the box", minY: -5, maxY: 10, want: EdgeNone},
	} {
		got, off := MarginEdge(tc.minY, tc.maxY, 0, 792)
		if got != tc.want || (got != EdgeNone && off != tc.wantOffset) {
			t.Errorf("%s: got (%v, %v), want (%v, %v)", tc.name, got, off, tc.want, tc.wantOffset)
		}
	}
}

func TestRunningLines(t *testing.T) {
	t.Parallel()

	opts := RunningOptions{MinPages: 3, PositionTolerance: 4, FoldDigits: true}
	lines := []Line{
		{Page: 0, Text: "Page 1", Edge: EdgeBottom, Offset: 30},
		{Page: 1, Text: "Page 2", Edge: EdgeBottom, Offset: 31},
		{Page: 2, Text: "Page 3", Edge: EdgeBottom, Offset: 30},
		// The same text in the other band is another group.
		{Page: 3, Text: "Page 4", Edge: EdgeTop, Offset: 30},
		// A repeat far from the cluster stays content.
		{Page: 3, Text: "Page 9", Edge: EdgeBottom, Offset: 60},
		// A text on too few pages stays content.
		{Page: 0, Text: "Draft", Edge: EdgeTop, Offset: 20},
		{Page: 1, Text: "Draft", Edge: EdgeTop, Offset: 20},
		// A line in no band is never furniture with a tolerance set.
		{Page: 0, Text: "Body", Edge: EdgeNone},
		{Page: 1, Text: "Body", Edge: EdgeNone},
		{Page: 2, Text: "Body", Edge: EdgeNone},
	}
	want := []bool{true, true, true, false, false, false, false, false, false, false}
	got := RunningLines(lines, opts)
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d (%q): got %v, want %v", i, lines[i].Text, got[i], want[i])
		}
	}

	// Two repeats on one page are one page.
	same := []Line{{Page: 0, Text: "x"}, {Page: 0, Text: "x"}, {Page: 0, Text: "x"}}
	for i, r := range RunningLines(same, RunningOptions{MinPages: 2}) {
		if r {
			t.Errorf("line %d repeats on one page only, read as running", i)
		}
	}

	// Without a tolerance, position is not compared and case folds.
	anywhere := []Line{{Page: 0, Text: "Title"}, {Page: 1, Text: "TITLE "}}
	for i, r := range RunningLines(anywhere, RunningOptions{MinPages: 2, FoldCase: true}) {
		if !r {
			t.Errorf("line %d: a case-folded repeat on two pages not read as running", i)
		}
	}
}

func TestIsPageNumber(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		text string
		n    int
		want bool
	}{
		{"7", 7, true},
		{" - 7 - ", 7, true},
		{"Page 7", 7, true},
		{"page 7 of 12", 7, true},
		{"7 / 12", 7, true},
		{"Page7of12", 7, true},
		{"8", 7, false},
		{"07", 7, false},
		{"+7", 7, false},
		{"7 apples", 7, false},
		{"Section 7", 7, false},
		{"", 1, false},
		{"0", 0, false},
	} {
		if got := IsPageNumber(tc.text, tc.n); got != tc.want {
			t.Errorf("IsPageNumber(%q, %d) = %v, want %v", tc.text, tc.n, got, tc.want)
		}
	}
}
