package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// parentTreeDepthDoc builds a one-page tagged document whose ParentTree is a
// number tree nested chain levels deep, with its /Nums leaf mapping numsKey to
// the structure element. The page declares /StructParents 0, so a numsKey other
// than 0 makes the entry genuinely absent rather than merely unread.
//
// elemLang carries the structure element's /Lang entry, or is empty for an
// element that supplies no language. The catalog never declares /Lang, so the
// element is the only thing that can determine it.
func parentTreeDepthDoc(t *testing.T, ctx context.Context, chain, numsKey int, elemLang string) (*document.Document, ast.PdfDict) {
	t.Helper()

	const content = "/Span << /MCID 0 >> BDC\nBT /F1 12 Tf 20 700 Td (hello) Tj ET\nEMC\n"
	lang := ""
	if elemLang != "" {
		lang = " /Lang (" + elemLang + ")"
	}
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 6 0 R /MarkInfo << /Marked true >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R" +
			" /Resources << /Font << /F1 5 0 R >> >> /StructParents 0 >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Type /StructTreeRoot /K [7 0 R] /ParentTree 8 0 R >>",
		"<< /Type /StructElem /S /Span /P 6 0 R /Pg 3 0 R" + lang + " /K 0 >>",
	}
	for i := range chain {
		if i == chain-1 {
			objs = append(objs, fmt.Sprintf("<< /Nums [%d [7 0 R]] >>", numsKey))
			continue
		}
		objs = append(objs, fmt.Sprintf("<< /Kids [%d 0 R] >>", 8+i+1))
	}

	doc, _ := objsDocPDF(t, objs...)
	catalog, err := document.ResolveDict(ctx, doc, ast.PdfIndirectRef{ObjectNum: 1})
	if err != nil {
		t.Fatalf("resolve catalog: %v", err)
	}
	return doc, catalog
}

// TestStructureTreeLangCoverage_DeepParentTreeDoesNotFabricate pins the second
// rule the same truncated read manufactured. markedContentHasMCID answered "no
// owner" for content whose owner sat past the bound, so the scan counted the
// text as belonging to no structure element and reported ISO 14289-1 §7.2 t34,
// "text outside the structure tree", for a file that tags and localises it.
func TestStructureTreeLangCoverage_DeepParentTreeDoesNotFabricate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	chains := []int{maxEFNameTreeDepth, maxEFNameTreeDepth + 2, maxEFNameTreeDepth * 4}
	for _, chain := range chains {
		doc, catalog := parentTreeDepthDoc(t, ctx, chain, 0, "en-US")
		got := checkStructureTreeLangCoverage(ctx, doc, catalog,
			langRequiredRule("PDF/UA-1"), langValidRule("PDF/UA-1"))
		if len(got) > 0 {
			t.Errorf("ParentTree nested %d levels: violations = %d, want 0, the element carries /Lang (%v)",
				chain, len(got), got)
		}
	}
}

// TestStructureTreeLangCoverage_UndeterminedTextStillFires is the control for
// that abstention. With the tree read whole and no /Lang anywhere, the text
// genuinely has no determinable language and §7.2 t34 must still fire, so
// counting truncated content as owned cannot silence the rule outright.
func TestStructureTreeLangCoverage_UndeterminedTextStillFires(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	doc, catalog := parentTreeDepthDoc(t, ctx, 3, 0, "")
	got := checkStructureTreeLangCoverage(ctx, doc, catalog,
		langRequiredRule("PDF/UA-1"), langValidRule("PDF/UA-1"))
	if len(got) == 0 {
		t.Fatal("violations = 0, want at least 1: no /Lang covers the text and the tree was read whole")
	}
}
