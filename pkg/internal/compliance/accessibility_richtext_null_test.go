package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestMarkupRichTextNullEntries pins ISO 14289-2, 8.9.2.3 t2 on null entries as
// veraPDF reads them: containsRC is a knownKey on /RC and Contents a
// getStringKey value, so an /RC or /Contents written as null is absent
// (ISO 32000-1, 7.3.7) and the rule does not apply. Every case was measured
// against veraPDF.
func TestMarkupRichTextNullEntries(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const rc = `(<body xmlns="http://www.w3.org/1999/xhtml"><p>x</p></body>)`

	tests := []struct {
		name string
		body string
		want int
	}{
		{"an /RC written as null beside a /Contents", "/Subtype /Square /RC null /Contents (x)", 0},
		{"a /Contents written as null beside an /RC", "/Subtype /Square /RC " + rc + " /Contents null", 0},
		{"an /RC whose text equals /Contents", "/Subtype /Square /RC " + rc + " /Contents (x)", 0},
		{"an /RC whose text differs from /Contents", "/Subtype /Square /RC " + rc + " /Contents (y)", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			viols, err := ValidatePDFUA(ctx, annotPresenceDoc(t, tt.body, ""), models.PDFUA2)
			if err != nil {
				t.Fatalf("ValidatePDFUA: %v", err)
			}
			if got := countRuleErrs(t, viols, Rule_14289_2_8_9_2_3_t02); got != tt.want {
				t.Errorf("8.9.2.3 t2 fired %d times, want %d", got, tt.want)
			}
		})
	}
}
