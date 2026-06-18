package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
)

func TestTOCIRefRequired(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	check := func(profile, tociExtra string) []Violation {
		doc, cat := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
			"<< /Type /StructTreeRoot /K 5 0 R >>",
			"<< /Type /StructElem /S /TOC /P 4 0 R /K [6 0 R] >>",
			"<< /Type /StructElem /S /TOCI /P 5 0 R "+tociExtra+" >>",
		)
		root, err := document.ResolveDict(ctx, doc, cat["StructTreeRoot"])
		if err != nil {
			t.Fatalf("resolve StructTreeRoot: %v", err)
		}
		var results []Violation
		walkStructureTree(ctx, doc, root, profile, &results)
		return results
	}

	t.Run("UA-2 TOCI without /Ref fires 8.2.5.8-t1", func(t *testing.T) {
		t.Parallel()
		if got := check("PDF/UA-2", ""); !hasRule(got, Rule_14289_2_8_2_5_8_t01) {
			t.Errorf("TOCI without /Ref: got %v, want 8.2.5.8-t1", got)
		}
	})

	t.Run("UA-2 TOCI with /Ref is clean", func(t *testing.T) {
		t.Parallel()
		if got := check("PDF/UA-2", "/Ref [7 0 R]"); hasRule(got, Rule_14289_2_8_2_5_8_t01) {
			t.Errorf("TOCI with /Ref must not fire 8.2.5.8-t1, got %v", got)
		}
	})

	t.Run("UA-1 never fires 8.2.5.8 (UA-2-only rule)", func(t *testing.T) {
		t.Parallel()
		if got := check("PDF/UA-1", ""); hasRule(got, Rule_14289_2_8_2_5_8_t01) {
			t.Errorf("UA-1 must not fire the UA-2 8.2.5.8 rule, got %v", got)
		}
	})
}
