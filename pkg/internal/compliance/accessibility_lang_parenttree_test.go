package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestTextLangThroughParentTree pins ISO 14289-1, 7.2 t34 on text in an
// MCID-marked sequence of a tagged document with no catalog /Lang. veraPDF finds
// the text's owning element only through the ParentTree entry the page's
// /StructParents selects (ISO 32000-1, 14.7.4.4), so with no ParentTree, or an
// MCID it does not map, the element listing the MCID in /K lends the text no
// /Lang and its language is undetermined. Every case was measured against
// veraPDF.
func TestTextLangThroughParentTree(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const txt = "/P << /MCID 1 >> BDC BT /F1 12 Tf 10 10 Td (Hi) Tj ET EMC"

	tests := []struct {
		name       string
		parentTree string // extra StructTreeRoot entries
		pageSP     string // extra page entries
		pLang      string // extra entries on the owning P
		want       int
	}{
		{"no ParentTree", "", "", "", 1},
		{"no ParentTree, the listing element has /Lang", "", "", " /Lang (en)", 1},
		{"a ParentTree mapping the MCID to an element with /Lang", " /ParentTree << /Nums [0 [null 8 0 R]] >>", " /StructParents 0", " /Lang (en)", 0},
		{"a ParentTree mapping the MCID to an element without /Lang", " /ParentTree << /Nums [0 [null 8 0 R]] >>", " /StructParents 0", "", 1},
		{"a ParentTree that does not map the MCID", " /ParentTree << /Nums [0 [8 0 R]] >>", " /StructParents 0", " /Lang (en)", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 6 0 R /MarkInfo << /Marked true >> >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >>"+tt.pageSP+" >>",
				fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(txt), txt),
				"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
				"<< /Type /StructTreeRoot /K 7 0 R"+tt.parentTree+" >>",
				"<< /Type /StructElem /S /Document /P 6 0 R /K [8 0 R] >>",
				"<< /Type /StructElem /S /P /P 7 0 R /Pg 3 0 R /K 1"+tt.pLang+" >>",
			)
			viols, err := ValidatePDFUA(ctx, doc, models.PDFUA1)
			if err != nil {
				t.Fatalf("ValidatePDFUA: %v", err)
			}
			if got := countRuleErrs(t, viols, Rule_14289_1_7_2_t34); got != tt.want {
				t.Errorf("7.2 t34 fired %d times, want %d", got, tt.want)
			}
		})
	}
}

// TestTextLangUnderTypelessElement pins ISO 14289-1, 7.2 t34 on text whose
// owning element sits under an element with no structure type. veraPDF climbs
// through that element for a /Lang, so text with none on the way is
// undetermined and text under a wrapper with /Lang is not. Every case was
// measured against veraPDF.
func TestTextLangUnderTypelessElement(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const txt = "/P << /MCID 0 >> BDC BT /F1 12 Tf 10 10 Td (Hi) Tj ET EMC"

	tests := []struct {
		name      string
		wrapLang  string
		ownerLang string
		want      int
	}{
		{"no /Lang on the way", "", "", 1},
		{"a /Lang on the typeless wrapper", " /Lang (en)", "", 0},
		{"a /Lang on the owning element", "", " /Lang (en)", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 6 0 R /MarkInfo << /Marked true >> >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> /StructParents 0 >>",
				fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(txt), txt),
				"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
				"<< /Type /StructTreeRoot /K 7 0 R /ParentTree << /Nums [0 [9 0 R]] >> >>",
				"<< /Type /StructElem /S /Document /P 6 0 R /K [8 0 R] >>",
				"<< /Type /StructElem /P 7 0 R /K [9 0 R]"+tt.wrapLang+" >>",
				"<< /Type /StructElem /S /P /P 8 0 R /Pg 3 0 R /K 0"+tt.ownerLang+" >>",
			)
			viols, err := ValidatePDFUA(ctx, doc, models.PDFUA1)
			if err != nil {
				t.Fatalf("ValidatePDFUA: %v", err)
			}
			if got := countRuleErrs(t, viols, Rule_14289_1_7_2_t34); got != tt.want {
				t.Errorf("7.2 t34 fired %d times, want %d", got, tt.want)
			}
		})
	}
}
