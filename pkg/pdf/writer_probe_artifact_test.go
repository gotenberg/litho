package pdf

import (
	"bytes"
	"context"
	"fmt"
	"testing"
)

// onePageTextPDF is a Letter page drawing content in Helvetica.
func onePageTextPDF(content string) []byte {
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R " +
			"/Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	})
}

// TestProbeIntegrityCountsArtifactText pins that the remediation probe counts
// text marked as an artifact: it is still drawn, so a fix that marks it loses
// nothing.
func TestProbeIntegrityCountsArtifactText(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := onePageTextPDF("/Artifact <</Type /Pagination /Subtype /Footer>> BDC\n" +
		"BT /F1 9 Tf 300 30 Td (Page 1) Tj ET\nEMC\n")
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	snap, err := probeIntegrity(ctx, r)
	if err != nil {
		t.Fatalf("probeIntegrity: %v", err)
	}
	if snap.textLen == 0 {
		t.Error("probe counted no text on a page whose only text is a pagination artifact")
	}
}
