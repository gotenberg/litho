package compliance

import (
	"context"
	"fmt"
	"testing"
)

// TestSpanLang_FormXObject pins that ISO 14289-1 §7.2 t30 reaches /Span
// marked content inside a Form XObject.
//
// The rule's subject is every SEMarkedContent veraPDF builds, and it builds them
// for the content of a Form XObject a page draws, not only for the page's own
// stream. A /Lang on the sequence the Do sits in covers the form's content,
// since the form paints inside it (ISO 32000-1 §14.6).
//
// # Corpus evidence
//
//   - pdfjs issue19971.pdf: draws a form whose /Span sequences carry
//     /ActualText with no language anywhere; veraPDF reports each one and
//     litho, scanning page content alone, reported none.
func TestSpanLang_FormXObject(t *testing.T) {
	t.Parallel()

	const form = "/Span <</ActualText (x)>> BDC EMC"
	tests := []struct {
		name  string
		page  string
		fires bool
	}{
		{"a form drawn outside marked content", "q /Fm0 Do Q", true},
		{"a form drawn inside a sequence with /Lang", "/Span <</Lang (en-US)>> BDC /Fm0 Do EMC", false},
		{"a /Span on the page itself", "/Span <</ActualText (x)>> BDC EMC", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, cat := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /XObject << /Fm0 5 0 R >> >> /Contents 4 0 R >>",
				fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(tt.page), tt.page),
				fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 200 200] /Length %d >>\nstream\n%s\nendstream", len(form), form),
			)
			got := checkSpanMarkedContentLang(context.Background(), doc, cat, "PDF/UA-1")
			if fired := hasRule(got, Rule_14289_1_7_2_t30); fired != tt.fires {
				t.Errorf("7.2 t30 fired = %v, want %v (%v)", fired, tt.fires, got)
			}
		})
	}
}
