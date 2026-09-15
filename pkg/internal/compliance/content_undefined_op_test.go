package compliance

import (
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// undefinedOpViolations runs the resource walk over ops and keeps only the
// ISO 19005-2, 6.2.2 t1 findings.
func undefinedOpViolations(t *testing.T, ops []content.Operation, report bool) []Violation {
	t.Helper()
	got := checkContentStreamResourcesOps(testCtx(), nopDoc(), ops, nil, nil, nil, 2, 1, report)
	return slices.DeleteFunc(got, func(v Violation) bool {
		return v.Rule != Rule_19005_2_6_2_2_t01
	})
}

// TestUndefinedOperatorCountedPerOccurrence pins the granularity of ISO 19005-2,
// 6.2.2 t1. veraPDF keys the rule on the Op_Undefined object with the test
// `false`, so it instantiates one per operator OCCURRENCE: a repeated undefined
// operator answers once per use, not once per name.
func TestUndefinedOperatorCountedPerOccurrence(t *testing.T) {
	t.Parallel()

	ops := []content.Operation{
		{Operator: "lithoOp"},
		{Operator: "q"},
		{Operator: "lithoOp"},
		{Operator: "otherOp"},
		{Operator: "Q"},
	}
	if got := len(undefinedOpViolations(t, ops, true)); got != 3 {
		t.Errorf("got %d violations, want 3 (two lithoOp uses and one otherOp)", got)
	}
}

// TestUndefinedOperatorSuppressedForPageStreams covers who states the rule.
// checkPageContent already walks every page content stream and states 6.2.2 t1
// there, so the resource walk must stay silent for those or the violation is
// stated twice. Streams that walk does not reach, Form XObjects and Type3
// CharProcs, pass true and are the only ones this path reports.
func TestUndefinedOperatorSuppressedForPageStreams(t *testing.T) {
	t.Parallel()

	ops := []content.Operation{{Operator: "lithoOp"}}
	if got := undefinedOpViolations(t, ops, false); len(got) != 0 {
		t.Errorf("got %d violations, want none: the page walk states this rule", len(got))
	}
}
