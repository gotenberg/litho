package mutations

import (
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
)

// TestFlattenDrawsAChoiceFieldWithoutAppearance pins that a choice field with
// a value and no /AP is drawn from its value and leaves with the form: a list
// box's selections, which /V lists in an array (ISO 32000-1, Table 231), and
// the display text /Opt pairs with an export value. A multiple-selection list
// box was kept as a field, and the flattened document still had a form.
func TestFlattenDrawsAChoiceFieldWithoutAppearance(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, widget, want string
	}{
		{"multiple selection", "<< /Type /Annot /Subtype /Widget /FT /Ch /Ff 2097152 /T (tags) /Opt [(a) (b) (c)] /V [(a) (c)] /Rect [10 10 130 60] >>", "(a, c) Tj"},
		{"export and display pairs", "<< /Type /Annot /Subtype /Widget /FT /Ch /Ff 2097152 /T (tags) /Opt [[(a) (Alpha)] [(b) (Bravo)] [(c) (Charlie)]] /V [(a) (c)] /Rect [10 10 130 60] >>", "(Alpha, Charlie) Tj"},
		{"combo box", "<< /Type /Annot /Subtype /Widget /FT /Ch /Ff 131072 /T (country) /Opt [[(FR) (France)] [(DE) (Germany)]] /V (FR) /Rect [10 10 130 30] >>", "(France) Tj"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, doc, alloc := flattenDoc(t, tc.widget)
			if err := FlattenForm(ctx, alloc, doc); err != nil {
				t.Fatalf("FlattenForm: %v", err)
			}
			page, drawn := flattenedPage(t, alloc)
			if annots, _ := page["Annots"].(ast.PdfArray); len(annots) != 0 {
				t.Errorf("the widget survived the flatten: %v", annots)
			}
			if len(drawn) != 1 {
				t.Fatalf("the flatten drew %d appearances, want 1", len(drawn))
			}
			if !strings.Contains(drawn[0], tc.want) {
				t.Errorf("the generated appearance does not draw %s: %q", tc.want, drawn[0])
			}
		})
	}
}
