package compliance

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// TestUAAttributeLangDetermined pins ISO 14289-1 §7.2 t21/t22/t23: text carried
// by an /ActualText, /Alt or /E attribute needs a determinable natural language.
// The document has no catalog /Lang; a structure element carrying the attribute
// has no /Lang and no ancestor /Lang, so its language is undeterminable even
// though a SIBLING element carries /Lang. veraPDF reports the source-specific
// test per element; litho previously suppressed all coverage once any element
// had /Lang (verapdf-corpus 7.2-t21/t22/t23-fail-a).
func TestUAAttributeLangDetermined(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		attr string
		test int
	}{
		{"ActualText", "/ActualText (x)", 21},
		{"Alt", "/Alt (x)", 22},
		{"E", "/E (x)", 23},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			data := cyclePDF(
				"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 5 0 R /MarkInfo << /Marked true >> >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>",
				"<< /Length 0 >>\nstream\n\nendstream",
				"<< /Type /StructTreeRoot /K [6 0 R] >>",
				"<< /Type /StructElem /S /Document /P 5 0 R /K [7 0 R 8 0 R] >>",
				// The attribute-bearing element has no /Lang and no ancestor /Lang.
				"<< /Type /StructElem /S /H1 /P 6 0 R "+c.attr+" >>",
				// A sibling carries /Lang; it must not make the sibling determinable.
				"<< /Type /StructElem /S /P /P 6 0 R /Lang (en-US) >>",
			)
			doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
			if err != nil {
				t.Fatalf("NewDocument: %v", err)
			}
			viols, err := ValidatePDFUA(context.Background(), doc, models.PDFUA1)
			if err != nil {
				t.Fatalf("ValidatePDFUA: %v", err)
			}
			fired := false
			for _, e := range viols {
				if v, ok := e.(Violation); ok && v.Rule.Clause == "7.2" && v.Rule.TestNumber == c.test {
					fired = true
				}
			}
			if !fired {
				t.Errorf("%s attribute with no determinable language must fire §7.2 t%d", c.name, c.test)
			}
		})
	}
}
