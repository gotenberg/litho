package compliance

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// TestUANoMetadataFiresOnlyPresence pins that a PDF/UA-1 file with no catalog
// /Metadata reports only the missing-metadata requirement (§7.1 t8), not the
// dc:title (§7.1 t9) or the §5 PDF/UA identifier rule. veraPDF scopes the latter
// two to the XMP package object, so with no package they do not fire. Firing
// them was a false positive.
//
// # Corpus evidence
//
//   - verapdf 7.1-t08-fail-a: with no XMP package, veraPDF's dc:title and
//     identifier rules do not fire on it.
func TestUANoMetadataFiresOnlyPresence(t *testing.T) {
	t.Parallel()
	data := cyclePDF(
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 5 0 R /MarkInfo << /Marked true >> /Lang (en-US) >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>",
		"<< /Length 0 >>\nstream\n\nendstream",
		"<< /Type /StructTreeRoot /K [6 0 R] >>",
		"<< /Type /StructElem /S /Document /P 5 0 R >>",
	)
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	viols, err := ValidatePDFUA(context.Background(), doc, models.PDFUA1)
	if err != nil {
		t.Fatalf("ValidatePDFUA: %v", err)
	}
	fired := map[string]bool{}
	for _, e := range viols {
		if v, ok := e.(Violation); ok {
			fired[v.Rule.Clause] = true
			if v.Rule.Clause == "5" || (v.Rule.Clause == "7.1" && v.Rule.TestNumber == 9) {
				t.Errorf("no-metadata UA file must not fire %s t%d", v.Rule.Clause, v.Rule.TestNumber)
			}
		}
	}
	if !fired["7.1"] {
		t.Error("no-metadata UA file must fire the §7.1 metadata-presence rule")
	}
}
