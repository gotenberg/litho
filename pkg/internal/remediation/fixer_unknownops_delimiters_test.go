package remediation

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// TestRepairContentDataSeparatesOperatorsFromDelimiters pins that an operator
// written against ")", ">", "{", "}" or "%", which validation reads as veraPDF
// does, as one undefined operator, is written back apart from the delimiter:
// the comment and the stray delimiter go, and validation reads the operator.
// An operator whose part before the delimiter is no operator goes as any
// undefined operator does, and the operator a viewer reads after the delimiter
// stays.
//
// # Corpus evidence
//
//   - pdfjs calgray.pdf and calrgb.pdf: write "re" against a comment, which
//     veraPDF 1.30 reports as the undefined operator "re%" 60 and 1360 times,
//     and which their PDF/A-1b and PDF/A-2b remediations left in place.
func TestRepairContentDataSeparatesOperatorsFromDelimiters(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	for _, tc := range []struct {
		name               string
		src                string
		delimited, removed int
		want               []string
	}{
		{"an operator against a comment", "0 0 10 10 re%\nf\n", 1, 0, []string{"re", "f"}},
		{"an operator against a parenthesis", "q 1 0 0 1 0 0 cm Q)\n", 1, 0, []string{"q", "cm", "Q"}},
		{"an operator against a dictionary close", "q Q>> q Q{}\n", 2, 0, []string{"q", "Q", "q", "Q"}},
		{"an undefined operator against a parenthesis", "BT /F1 12 Tf (a) T)Tj ET\n", 0, 1, []string{"BT", "Tf", "Tj", "ET"}},
	} {
		out, tally := repairContentData(ctx, []byte(tc.src), 2)
		if tally.delimited != tc.delimited || tally.removed != tc.removed {
			t.Errorf("%s: separated %d operators from a delimiter and removed %d, want %d and %d",
				tc.name, tally.delimited, tally.removed, tc.delimited, tc.removed)
			continue
		}
		if strings.ContainsAny(string(out), "%{}") {
			t.Errorf("%s: the rewrite %q keeps the delimiter", tc.name, out)
		}
		ops, err := content.ParseStreamData(ctx, out)
		if err != nil {
			t.Fatalf("%s: parsing the rewrite %q: %v", tc.name, out, err)
		}
		if got := operatorNames(ops); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: validation reads the rewrite %q as %v, want %v", tc.name, out, got, tc.want)
		}
	}
}

// operatorNames returns the operator of every operation, in order.
func operatorNames(ops []content.Operation) []string {
	out := make([]string, len(ops))
	for i, op := range ops {
		out[i] = op.Operator
	}
	return out
}
