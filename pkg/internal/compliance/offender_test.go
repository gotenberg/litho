package compliance

import "testing"

// TestLowestOffenderPicksTheSmallest covers the contract the font rules rely on:
// whichever order candidates arrive in, the same one comes back out.
func TestLowestOffenderPicksTheSmallest(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		offer []uint16
		want  uint16
	}{
		{"ascending", []uint16{3, 7, 9}, 3},
		{"descending", []uint16{9, 7, 3}, 3},
		{"smallest in the middle", []uint16{9, 3, 7}, 3},
		{"single candidate", []uint16{42}, 42},
		{"duplicates", []uint16{5, 5, 5}, 5},
		{"zero is a legitimate offender", []uint16{4, 0, 2}, 0},
	} {
		var l lowestOffender[uint16]
		for _, v := range tc.offer {
			l.offer(v)
		}
		got, found := l.result()
		if !found {
			t.Errorf("%s: nothing offered back", tc.name)
			continue
		}
		if got != tc.want {
			t.Errorf("%s: got %d, want %d", tc.name, got, tc.want)
		}
	}
}

// TestLowestOffenderEmpty asserts an untouched tracker reports nothing. The
// callers return a violation only when found is true, so a zero value wrongly
// reported as present would invent a failure against a conforming font.
func TestLowestOffenderEmpty(t *testing.T) {
	t.Parallel()

	var l lowestOffender[uint16]
	if got, found := l.result(); found {
		t.Errorf("empty tracker reported offender %d", got)
	}
}

// TestLowestOffenderStrings covers the glyph-name sites, which order
// lexicographically rather than numerically.
func TestLowestOffenderStrings(t *testing.T) {
	t.Parallel()

	var l lowestOffender[string]
	for _, name := range []string{"zero", "alpha", "mu"} {
		l.offer(name)
	}
	got, found := l.result()
	if !found {
		t.Fatal("nothing offered back")
	}
	if got != "alpha" {
		t.Errorf("got %q, want %q", got, "alpha")
	}
}
