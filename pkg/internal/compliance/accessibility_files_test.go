package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestPDFUA2DescScope pins the scope of ISO 14289-2 §8.14.1: the /Desc
// requirement applies to file specifications reachable through the
// EmbeddedFiles name tree, not to those reachable only via /AF. PRESENCE of
// /Desc satisfies the clause, so an empty /Desc () passes while a Desc-less
// in-tree filespec fails; §8.14.1 mandates the entry but does not constrain
// its string value.
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
