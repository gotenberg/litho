package pdf

import (
	"bytes"
	"context"
	"slices"
	"testing"
)

// TestWriterPageNumbersAfterRemediation pins which document the page numbers of
// an edit queued after a remediation name, the rule the remediation methods
// document on [Writer].
//
// A remediation that keeps a later round leaves the Writer editing its output,
// so a later edit names the remediated document's pages. One that keeps a
// single round leaves the Writer editing its source, so a later edit names the
// source's pages. The two differ only when pages were deleted or inserted
// before the remediation: here page 1 of three is deleted, and page 2 names
// the source's second page after one round, the first of the output, and the
// output's second page after three.
//
// A deletion queued after the remediation names the same pages either way,
// since both documents then still hold the source's pages in order.
//
// The engine of [roundsEngine] fixes the round count: one step is one round.
func TestWriterPageNumbersAfterRemediation(t *testing.T) {
	t.Parallel()

	oneRound := []roundsStep{{keys: []string{"PageLayout"}}}
	threeRounds := []roundsStep{
		{keys: []string{"PageLayout"}}, {keys: []string{"PageMode"}}, {keys: []string{"Lang"}},
	}
	for _, tc := range []struct {
		name          string
		steps         []roundsStep
		deleteFirst   bool
		wantRotations []int
	}{
		{name: "a deletion, then one round", steps: oneRound, deleteFirst: true, wantRotations: []int{90, 0}},
		{name: "a deletion, then three rounds", steps: threeRounds, deleteFirst: true, wantRotations: []int{0, 90}},
		{name: "one round, then a deletion", steps: oneRound, wantRotations: []int{90, 0}},
		{name: "three rounds, then a deletion", steps: threeRounds, wantRotations: []int{90, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			data := validXrefPDF(t)
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()

			e := &roundsEngine{steps: tc.steps}
			w := NewWriter(r)
			defer w.Close()
			if tc.deleteFirst {
				if err := w.DeletePages(1); err != nil {
					t.Fatalf("DeletePages: %v", err)
				}
			}
			if _, err := w.remediate(ctx, DefaultRegressionTolerance, e.revalidate, e.run); err != nil {
				t.Fatalf("remediate: %v", err)
			}
			if len(e.disabled) != len(tc.steps) {
				t.Fatalf("the engine ran %d rounds, want %d", len(e.disabled), len(tc.steps))
			}
			if !tc.deleteFirst {
				if err := w.DeletePages(1); err != nil {
					t.Fatalf("DeletePages: %v", err)
				}
			}
			if err := w.RotatePages(90, 2); err != nil {
				t.Fatalf("RotatePages: %v", err)
			}

			var out bytes.Buffer
			if err := w.WriteTo(ctx, &out); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			if got := rotationsOf(t, out.Bytes()); !slices.Equal(got, tc.wantRotations) {
				t.Errorf("the output pages have rotations %v, want %v", got, tc.wantRotations)
			}
		})
	}
}
