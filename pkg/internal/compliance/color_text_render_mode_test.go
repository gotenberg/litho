package compliance

import (
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestColorScope_TextRenderModeColour pins ISO 19005-2, 6.2.4.3 t4 on text
// shown with no colour space selected for what the rendering mode paints. ISO
// 32000-1, Table 106 fills in modes 0, 2, 4 and 6 and strokes in modes 1, 2, 5
// and 6, so text in a stroking mode with only a stroke space selected uses no
// DeviceGray, and text that also strokes with only a fill space selected does.
// Every case was measured against veraPDF.
//
// # Corpus evidence
//
//   - pdfjs issue20513.pdf: strokes its text after only K.
func TestColorScope_TextRenderModeColour(t *testing.T) {
	t.Parallel()

	doc := func(pre string, mode int) []byte {
		content := fmt.Sprintf("%s BT /F1 12 Tf %d Tr 10 50 Td (A) Tj ET", pre, mode)
		return cyclePDF(
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << /Font << /F1 5 0 R >> >> /Contents 4 0 R >>",
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
			"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		)
	}

	for _, tc := range []struct {
		name  string
		pre   string
		mode  int
		fires bool
	}{
		{"stroked text after a stroke space", "0 0 0 1 K", 1, false},
		{"stroked text with nothing selected", "", 1, true},
		{"filled text after a stroke space", "0 0 0 1 K", 0, true},
		{"filled and stroked text after a fill space", "0 0 0 1 k", 2, true},
		{"stroked and clipped text after a stroke space", "0 0 0 1 K", 5, false},
		{"clipping text with nothing selected", "", 7, false},
		{"filled and clipped text after a stroke space", "0 0 0 1 K", 4, true},
		{"filled, stroked and clipped text after a fill space", "0 0 0 1 k", 6, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := firesRuleProfile(t, doc(tc.pre, tc.mode), models.PDFA2b, "6.2.4.3", 4); got != tc.fires {
				t.Errorf("6.2.4.3 t4 violation = %v, want %v", got, tc.fires)
			}
		})
	}
}
