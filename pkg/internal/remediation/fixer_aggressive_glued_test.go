package remediation

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// aggressiveUndefinedOperator is the finding that sends a page through the
// aggressive content rewrite's removal of undefined operators.
var aggressiveUndefinedOperator = []compliance.Violation{{Rule: compliance.Rule_19005_2_6_2_2_t01}}

// TestFixAggressive_SeparatesGluedOperators pins that the aggressive content
// rewrite writes a run of operators glued together back as the operators a
// viewer reads, as the unknown-ops fixer does, instead of deleting the run as
// one undefined operator. "ETBT" deleted took the ET and BT a viewer draws the
// second line with, and the text after it moved.
//
// # Corpus evidence
//
//   - pdfjs sci-notation.pdf: writes "ETBT" between its two text objects,
//     which the unknown-ops fixer already separates ahead of this rewrite.
func TestFixAggressive_SeparatesGluedOperators(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	src := "BT /F1 24 Tf 50 500 Td (a) Tj ETBT /F1 12 Tf 50 600 Td (b) Tj ET qQ zz"
	doc, alloc := newTestDoc(t, exponentPagePDF(src, "", "", ""))
	fixes, err := fixAggressive(ctx, alloc, doc, Options{}, aggressiveUndefinedOperator, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixAggressive: %v", err)
	}
	report := strings.Join(fixes, "\n")
	for _, want := range []string{
		"removed 1 unknown operators",
		"separated 2 content-stream operator(s)",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("fixes = %q, want %q among them", report, want)
		}
	}

	viewer, err := content.ParseStreamData(ctx, []byte(src), content.SplitGluedOperators())
	if err != nil {
		t.Fatalf("parsing the source: %v", err)
	}
	var want []content.Operation
	for _, op := range viewer {
		if content.IsOperator(op.Operator) {
			want = append(want, op)
		}
	}
	got, err := content.ParseStreamData(ctx, pendingStreamBytes(t, alloc, 4))
	if err != nil {
		t.Fatalf("parsing the rewrite: %v", err)
	}
	if !sameOperations(got, want) {
		t.Errorf("the rewrite reads as\n  %v\nwant what a viewer reads in the source\n  %v", got, want)
	}
}

// TestFixAggressive_RemovesARunThatIsNotOperators pins the other side: a run
// that splits into anything but operators is one undefined operator, and the
// aggressive rewrite removes it whole, as the unknown-ops fixer does.
func TestFixAggressive_RemovesARunThatIsNotOperators(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	for _, src := range []string{"(a) Tj ETXX", "(a) Tj junk"} {
		doc, alloc := newTestDoc(t, exponentPagePDF(src, "", "", ""))
		fixes, err := fixAggressive(ctx, alloc, doc, Options{}, aggressiveUndefinedOperator, "PDF/A-2b")
		if err != nil {
			t.Fatalf("%q: fixAggressive: %v", src, err)
		}
		if report := strings.Join(fixes, "\n"); strings.Contains(report, "separated") {
			t.Errorf("%q: fixes = %q, want no operator separated", src, report)
		}
		got, err := content.ParseStreamData(ctx, pendingStreamBytes(t, alloc, 4))
		if err != nil {
			t.Fatalf("%q: parsing the rewrite: %v", src, err)
		}
		if len(got) != 1 || got[0].Operator != "Tj" {
			t.Errorf("%q: the rewrite reads as %v, want the Tj alone", src, got)
		}
	}
}

// TestFixAggressive_LeavesAnOperatorEndedByADigit pins that "q1" is no
// undefined operator: the compliance reading ends an operator at a digit, as
// veraPDF does, so it reads q then the number 1, and the aggressive rewrite has
// nothing to remove.
func TestFixAggressive_LeavesAnOperatorEndedByADigit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	doc, alloc := newTestDoc(t, exponentPagePDF("(a) Tj q1", "", "", ""))
	fixes, err := fixAggressive(ctx, alloc, doc, Options{}, aggressiveUndefinedOperator, "PDF/A-2b")
	if err != nil {
		t.Fatalf("fixAggressive: %v", err)
	}
	if len(fixes) != 0 {
		t.Errorf("fixes = %q, want none", fixes)
	}
	if _, pending := alloc.Pending()[4]; pending {
		t.Error("the page content was rewritten, want it left as written")
	}
}
