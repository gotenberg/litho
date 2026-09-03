package pdf

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/remediation"
)

// TestCountRepairsIgnoresTheFullRewriteNote pins that the fix-to-fixpoint loop
// does not read the full-rewrite note as progress.
//
// The note repairs nothing: it records that a formatting violation is handled
// by the rewrite the orchestrator performs. A round is kept when it "fixed
// something", so a file whose formatting violation cannot actually be repaired
// re-emitted the note every round, ran the loop to its bound, and re-serialised
// itself twice for nothing.
func TestCountRepairsIgnoresTheFullRewriteNote(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		fixes []string
		want  int
	}{
		{"only the note", []string{remediation.FullRewriteNote}, 0},
		{"the note repeated", []string{remediation.FullRewriteNote, remediation.FullRewriteNote}, 0},
		{"nothing at all", nil, 0},
		{"a real repair", []string{"set catalog /Lang to \"en\""}, 1},
		{"a repair beside the note", []string{remediation.FullRewriteNote, "removed /NeedsRendering from catalog"}, 1},
	} {
		if got := countRepairs(tc.fixes); got != tc.want {
			t.Errorf("%s: countRepairs = %d, want %d", tc.name, got, tc.want)
		}
	}
}

// TestDedupeFixesKeepsTheFirstOfEach pins that a repeated description is
// reported once, in the order it first appeared.
//
// Each round of the loop re-emits the same sentence, so a file reported
// "Fixed: 3" with one note listed three times.
func TestDedupeFixesKeepsTheFirstOfEach(t *testing.T) {
	t.Parallel()

	got := dedupeFixes([]string{"a", "b", "a", "c", "b"})
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("dedupeFixes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("dedupeFixes[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestDedupeFixesLeavesDistinctDescriptions is the control: a report whose
// fixes are all different must come through untouched.
func TestDedupeFixesLeavesDistinctDescriptions(t *testing.T) {
	t.Parallel()

	in := []string{"one", "two", "three"}
	got := dedupeFixes(in)
	if len(got) != len(in) {
		t.Fatalf("dedupeFixes = %v, want %v", got, in)
	}
	for i := range in {
		if got[i] != in[i] {
			t.Errorf("dedupeFixes[%d] = %q, want %q", i, got[i], in[i])
		}
	}
}
