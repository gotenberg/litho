package remediation

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// TestRepairContentDataSeparatesGluedOperators pins that a run of operators
// written with no white space between them comes apart rather than going as one
// undefined operator: the rewrite reads, as validation reads it, the operators
// a viewer reads in the source, so the text after "ETBT" stays where the viewer
// draws it.
//
// # Corpus evidence
//
//   - pdfjs sci-notation.pdf: writes "ETBT" between its two text objects.
//     Removing the run moved the second line off the page for text
//     extraction, and the PDF/A-2b remediation rolled its content repairs
//     back.
func TestRepairContentDataSeparatesGluedOperators(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	for _, tc := range []struct {
		name      string
		src       string
		separated int
		removed   int
	}{
		{"ET run into BT", "BT /F1 24 Tf 50 500 Td (a) Tj ETBT /F1 12 Tf 50 600 Td (b) Tj ET", 1, 0},
		{"three operators and the operands of the first", "/F1 12 TfT*T* (a) Tj", 2, 0},
		{"a glued run beside an undefined operator", "qQ zz", 1, 1},
	} {
		out, tally := repairContentData(ctx, []byte(tc.src), 2)
		if tally.separated != tc.separated || tally.removed != tc.removed {
			t.Errorf("%s: separated %d and removed %d operators, want %d and %d",
				tc.name, tally.separated, tally.removed, tc.separated, tc.removed)
			continue
		}
		viewer, err := content.ParseStreamData(ctx, []byte(tc.src), content.SplitGluedOperators())
		if err != nil {
			t.Fatalf("%s: parsing the source: %v", tc.name, err)
		}
		var want []content.Operation
		for _, op := range viewer {
			if content.IsOperator(op.Operator) {
				want = append(want, op)
			}
		}
		got, err := content.ParseStreamData(ctx, out)
		if err != nil {
			t.Fatalf("%s: parsing the rewrite %q: %v", tc.name, out, err)
		}
		if !sameOperations(got, want) {
			t.Errorf("%s: the rewrite %q reads as\n  %v\nwant what a viewer reads in the source\n  %v", tc.name, out, got, want)
		}
	}
}

// TestRepairContentDataRemovesARunThatIsNotOperators pins the other side: a run
// that splits into anything but operators, such as an operand or an undefined
// operator, is one undefined operator and goes whole, as it went before runs
// were split.
func TestRepairContentDataRemovesARunThatIsNotOperators(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	for _, src := range []string{"(a) Tj ETXX", "(a) Tj junk", "(a) Tj q1"} {
		out, tally := repairContentData(ctx, []byte(src), 2)
		if tally.separated != 0 || tally.removed != 1 {
			t.Errorf("%q: separated %d and removed %d operators, want 0 and 1", src, tally.separated, tally.removed)
			continue
		}
		got, err := content.ParseStreamData(ctx, out)
		if err != nil {
			t.Fatalf("parsing the rewrite %q: %v", out, err)
		}
		if len(got) != 1 || got[0].Operator != "Tj" {
			t.Errorf("%q: the rewrite %q reads as %v, want the Tj alone", src, out, got)
		}
	}
}
