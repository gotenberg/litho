package compliance

import (
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestEmptyStreamLength pins ISO 19005-2, 6.1.7.1 t1 on a stream with no data:
// an endstream that follows only the EOL ending the stream keyword has no EOL
// of its own before it, and veraPDF measures the stream at -1 bytes, so it fails
// whatever its /Length, while a second EOL makes it a conforming empty stream.
// Every case was measured against veraPDF.
//
// # Corpus evidence
//
//   - pdfjs issue4461.pdf: writes such an empty stream.
func TestEmptyStreamLength(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		stream string
		fires  bool
	}{
		{"LF then endstream", "<< /Length 0 >>\nstream\nendstream", true},
		{"CRLF then endstream", "<< /Length 0 >>\nstream\r\nendstream", true},
		{"LF, LF, then endstream", "<< /Length 0 >>\nstream\n\nendstream", false},
		{"CRLF, CRLF, then endstream", "<< /Length 0 >>\nstream\r\n\r\nendstream", false},
		{"one byte, LF, then endstream", "<< /Length 1 >>\nstream\nX\nendstream", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := cyclePDF(
				"<< /Type /Catalog /Pages 2 0 R /Extra 4 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Resources << >> >>",
				tc.stream,
			)
			if got := firesRuleProfile(t, data, models.PDFA2b, "6.1.7.1", 1); got != tc.fires {
				t.Errorf("6.1.7.1 t1 violation = %v, want %v", got, tc.fires)
			}
		})
	}
}
