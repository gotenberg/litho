package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestPDFUA2DescScope pins the veraPDF scoping of ISO 14289-2 §8.14.1 t1
// learned live: the rule covers file specifications in the EmbeddedFiles name
// tree only, and PRESENCE of /Desc suffices (an empty /Desc () passes; vera
// passes the empty-Desc filespec of PDF_A-3b__veraPDF_test_suite_6-8-t01-fail-a
// remediated to PDF/UA-2 while failing the Desc-less
// PDF_UA-2__8.14.1-t01-fail-a).
func TestPDFUA2DescScope(t *testing.T) {
	ctx := context.Background()

	build := func(filespec string) (*document.Document, ast.PdfDict) {
		return objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R /Names << /EmbeddedFiles 5 0 R >> >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
			filespec,
			"<< /Names [(a.txt) 4 0 R] >>",
			"<< /Length 0 >>",
		)
	}

	t.Run("missing Desc in tree fails", func(t *testing.T) {
		doc, cat := build("<< /Type /Filespec /F (a.txt) /UF (a.txt) /EF << /F 6 0 R >> >>")
		got := checkPDFUAFileSpecFUF(ctx, doc, cat, "PDF/UA-2")
		if !hasRule(got, Rule_14289_2_8_14_1_t01) {
			t.Errorf("Desc-less tree filespec: got %v, want 8.14.1 t1", got)
		}
	})

	t.Run("empty Desc passes", func(t *testing.T) {
		doc, cat := build("<< /Type /Filespec /F (a.txt) /UF (a.txt) /Desc () /EF << /F 6 0 R >> >>")
		got := checkPDFUAFileSpecFUF(ctx, doc, cat, "PDF/UA-2")
		if hasRule(got, Rule_14289_2_8_14_1_t01) {
			t.Errorf("empty /Desc must satisfy presence-only check, got %v", got)
		}
	})

	t.Run("Desc-less filespec outside tree passes", func(t *testing.T) {
		doc, cat := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R /AF [4 0 R] >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
			"<< /Type /Filespec /F (a.txt) /UF (a.txt) /EF << /F 5 0 R >> >>",
			"<< /Length 0 >>",
		)
		got := checkPDFUAFileSpecFUF(ctx, doc, cat, "PDF/UA-2")
		if hasRule(got, Rule_14289_2_8_14_1_t01) {
			t.Errorf("AF-only filespec is out of scope for 8.14.1, got %v", got)
		}
	})
}
