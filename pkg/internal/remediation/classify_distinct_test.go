package remediation

import (
	"fmt"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
)

// TestClassifyMatchesEachViolationAsAlone checks that matching each distinct
// rule and context once gives every fixer the violations, and their indices,
// that matching every violation on its own gives.
func TestClassifyMatchesEachViolationAsAlone(t *testing.T) {
	contexts := []string{"", "page 1", "page 2", "font F1 (obj 12): glyph widths differ", "content stream of page 3: name length 200"}
	var violations []compliance.Violation
	for _, rules := range [][]compliance.Rule{compliance.PDFA2bRules(), compliance.PDFA1aRules(), compliance.PDFUA1Rules(), compliance.PDFUA2Rules()} {
		for i, r := range rules {
			for k := range 3 {
				violations = append(violations, compliance.Violation{Rule: r, Context: contexts[(i+k)%len(contexts)]})
			}
		}
	}
	got := classify(violations)
	var want []planEntry
	for _, f := range registry {
		var matched []compliance.Violation
		var indices []int
		for i, v := range violations {
			if f.matches(v) {
				matched = append(matched, v)
				indices = append(indices, i)
			}
		}
		if len(matched) > 0 {
			want = append(want, planEntry{f: f, violations: matched, indices: indices})
		}
	}
	if len(got) != len(want) {
		t.Fatalf("classify gave %d plan entries, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].f.name != want[i].f.name {
			t.Errorf("entry %d: fixer %q, want %q", i, got[i].f.name, want[i].f.name)
		}
		if !slices.Equal(got[i].indices, want[i].indices) {
			t.Errorf("entry %d (%s): indices %v, want %v", i, want[i].f.name, got[i].indices, want[i].indices)
		}
		if fmt.Sprint(got[i].violations) != fmt.Sprint(want[i].violations) {
			t.Errorf("entry %d (%s): violations differ", i, want[i].f.name)
		}
	}
	if len(want) < 10 {
		t.Errorf("only %d fixers matched: the violations reach too few fixers to test classify", len(want))
	}
}
