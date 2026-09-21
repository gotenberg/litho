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

// TestCheckOrphanMCIDs_DeepParentTreeDoesNotFabricate pins that a ParentTree
// nested past maxEFNameTreeDepth does not report content the tree does cover.
//
// numberTreeLookup returned nil both for a key that is absent and for a walk it
// had stopped, parentTreeMCIDs turned that nil into an empty covered set, and
// every MCID on the page then read as unreachable. Measured on this fixture: a
// leaf at depth 64 reported nothing and a leaf at depth 65 reported
// ISO 14289-1 §7.1 t3, on a file whose ParentTree names the element at every
// nesting level.
func TestCheckOrphanMCIDs_DeepParentTreeDoesNotFabricate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Either side of the bound, and far past it.
	chains := []int{maxEFNameTreeDepth, maxEFNameTreeDepth + 2, maxEFNameTreeDepth * 4}
	for _, chain := range chains {
		doc, catalog := parentTreeDepthDoc(t, ctx, chain, 0, "en-US")
		if got := checkOrphanMCIDs(ctx, doc, catalog, "PDF/UA-1"); len(got) > 0 {
			t.Errorf("ParentTree nested %d levels: violations = %d, want 0, the tree names the element (%v)",
				chain, len(got), got)
		}
	}
}

// TestCheckOrphanMCIDs_AbsentEntryStillFires is the control that keeps the
// abstention above from becoming blanket suppression. The page declares
// /StructParents 0 and the ParentTree maps only key 7, so the entry is
// genuinely absent and the walk completes: the content really is unreachable
// and ISO 14289-1 §7.1 t3 must still fire.
func TestCheckOrphanMCIDs_AbsentEntryStillFires(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	doc, catalog := parentTreeDepthDoc(t, ctx, 3, 7, "en-US")
	got := checkOrphanMCIDs(ctx, doc, catalog, "PDF/UA-1")
	if len(got) != 1 {
		t.Fatalf("violations = %d, want 1: key 0 is absent from a fully read ParentTree (%v)", len(got), got)
	}
	if msg := got[0].Context; msg != "page 1: marked content MCID 0 is not reachable through the ParentTree" {
		t.Fatalf("message = %q, want the orphan-MCID report", msg)
	}
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
