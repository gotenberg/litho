package compliance

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// firstLangElemObj is the object number of the first structure element
// [langFixture] appends after its fixed preamble objects.
const firstLangElemObj = 8

// langFixture builds a one-page tagged document with no catalog /Lang: stream is
// the page content, elems are structure elements under a /Document, and the
// ParentTree maps marked-content identifier i to elems[i], the forward link ISO
// 32000-1 §14.7.4.4 defines and veraPDF follows to reach a text item's owner.
func langFixture(t *testing.T, stream string, elems ...string) (*document.Document, ast.PdfDict) {
	t.Helper()

	var refs strings.Builder
	for i := range elems {
		fmt.Fprintf(&refs, "%d 0 R ", firstLangElemObj+i)
	}
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 6 0 R /StructParents 0 >>",
		"<< /Type /StructTreeRoot /K [5 0 R] /ParentTree 7 0 R >>",
		"<< /Type /StructElem /S /Document /P 4 0 R /K [" + refs.String() + "] >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(stream), stream),
		"<< /Nums [0 [" + refs.String() + "]] >>",
	}
	return tableDoc(t, 1, append(objs, elems...)...)
}

// countRuleLang returns how many violations carry rule r.
func countRuleLang(vs []Violation, r Rule) int {
	n := 0
	for _, v := range vs {
		if v.Rule == r {
			n++
		}
	}
	return n
}

// TestCheckLangRequired_SiblingLangDoesNotCoverOtherElements covers ISO 14289-1
// §7.2 t34. Two sibling paragraphs each show text through their own
// marked-content sequence and the catalog declares no /Lang, but only the first
// carries /Lang. veraPDF resolves each SETextItem's language from the /Lang of
// the element owning its marked content or of that element's /P ancestors
// (gContainsCatalogLang == true || Lang != null), so it reports the second
// paragraph. A /Lang on an unrelated sibling must not silence the rule.
func TestCheckLangRequired_SiblingLangDoesNotCoverOtherElements(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	doc, cat := langFixture(t,
		"/P <</MCID 0>> BDC\nBT (first) Tj ET\nEMC\n/P <</MCID 1>> BDC\nBT (second) Tj ET\nEMC\n",
		"<< /Type /StructElem /S /P /P 5 0 R /Pg 3 0 R /Lang (en-US) /K 0 >>",
		"<< /Type /StructElem /S /P /P 5 0 R /Pg 3 0 R /K 1 >>",
	)
	got := checkLangRequired(ctx, doc, cat, "PDF/UA-1")

	if n := countRuleLang(got, Rule_14289_1_7_2_t34); n != 1 {
		t.Errorf("sibling /Lang covers only its own element: want exactly 1 §7.2 t34, got %d (%v)", n, got)
	}
}

// TestCheckLangRequired_AncestorLangCoversDescendant is the counterpart: a
// /Lang on the enclosing /Document determines the language of the paragraph
// below it, because veraPDF walks the /P chain to the first element carrying a
// /Lang key (GFOp_BDC.getStructParentLang).
func TestCheckLangRequired_AncestorLangCoversDescendant(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	doc, cat := tableDoc(t, 1,
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 6 0 R /StructParents 0 >>",
		"<< /Type /StructTreeRoot /K [5 0 R] /ParentTree 7 0 R >>",
		"<< /Type /StructElem /S /Document /P 4 0 R /Lang (en-US) /K [8 0 R] >>",
		"<< /Length 40 >>\nstream\n/P <</MCID 0>> BDC\nBT (x) Tj ET\nEMC\nendstream",
		"<< /Nums [0 [8 0 R]] >>",
		"<< /Type /StructElem /S /P /P 5 0 R /Pg 3 0 R /K 0 >>",
	)
	got := checkLangRequired(ctx, doc, cat, "PDF/UA-1")

	if hasRule(got, Rule_14289_1_7_2_t34) {
		t.Errorf("ancestor /Lang determines the language: want no §7.2 t34, got %v", got)
	}
}

// TestCheckLangRequired_ImageOnlyElementIsNotATextItem covers the object ISO
// 14289-1 §7.2 t34 names, SETextItem. A /Figure whose marked content draws an
// image XObject shows no text, so veraPDF builds an SEImageXObjectItem and
// reports nothing, even with no /Lang in reach (verapdf-corpus 7.2-t22-fail-a).
func TestCheckLangRequired_ImageOnlyElementIsNotATextItem(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	doc, cat := langFixture(t,
		"/P <</MCID 0>> BDC\nBT (text) Tj ET\nEMC\n/Figure <</MCID 1>> BDC\n/Im0 Do\nEMC\n",
		"<< /Type /StructElem /S /P /P 5 0 R /Pg 3 0 R /Lang (en-US) /K 0 >>",
		"<< /Type /StructElem /S /Figure /P 5 0 R /Pg 3 0 R /K 1 >>",
	)
	got := checkLangRequired(ctx, doc, cat, "PDF/UA-1")

	if hasRule(got, Rule_14289_1_7_2_t34) {
		t.Errorf("image-only element shows no text: want no §7.2 t34, got %v", got)
	}
}

// TestCheckLangRequired_PresentEmptyLangIsDeterminable covers PDF/UA-1 §7.2: a
// present but empty /Lang on a structure element is an invalid Language-Tag (t29),
// yet it still makes the language determinable, so t34 (undeterminable language)
// must not also fire. veraPDF fires only t29 here.
func TestCheckLangRequired_PresentEmptyLangIsDeterminable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	doc, cat := langFixture(t,
		"/P <</MCID 0>> BDC\nBT (x) Tj ET\nEMC\n",
		"<< /Type /StructElem /S /P /P 5 0 R /Pg 3 0 R /Lang () /K 0 >>",
	)
	got := checkLangRequired(ctx, doc, cat, "PDF/UA-1")

	if !hasRule(got, Rule_14289_1_7_2_t29) {
		t.Error("empty /Lang on a structure element: want §7.2 t29 (invalid tag)")
	}
	if hasRule(got, Rule_14289_1_7_2_t34) {
		t.Error("present but empty /Lang: want no §7.2 t34 (language is determinable)")
	}
}

// TestCheckLangRequired_EmptyAncestorLangCoversDescendant extends the previous
// case down the tree: veraPDF stops its /P walk at the first element carrying a
// /Lang key and treats the language as determined even when that value is empty,
// so a descendant of an empty-/Lang element is covered too.
func TestCheckLangRequired_EmptyAncestorLangCoversDescendant(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	doc, cat := tableDoc(t, 1,
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 6 0 R /StructParents 0 >>",
		"<< /Type /StructTreeRoot /K [5 0 R] /ParentTree 7 0 R >>",
		"<< /Type /StructElem /S /Document /P 4 0 R /Lang () /K [8 0 R] >>",
		"<< /Length 40 >>\nstream\n/P <</MCID 0>> BDC\nBT (x) Tj ET\nEMC\nendstream",
		"<< /Nums [0 [8 0 R]] >>",
		"<< /Type /StructElem /S /P /P 5 0 R /Pg 3 0 R /K 0 >>",
	)
	got := checkLangRequired(ctx, doc, cat, "PDF/UA-1")

	if hasRule(got, Rule_14289_1_7_2_t34) {
		t.Errorf("empty ancestor /Lang still determines the language: want no §7.2 t34, got %v", got)
	}
}

// TestCheckLangRequired_NoLangAnywhereFiresT34 is the control: with no /Lang in
// the catalog or anywhere in the structure tree, the language is undeterminable
// and §7.2 t34 fires.
func TestCheckLangRequired_NoLangAnywhereFiresT34(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	doc, cat := langFixture(t,
		"/P <</MCID 0>> BDC\nBT (x) Tj ET\nEMC\n",
		"<< /Type /StructElem /S /P /P 5 0 R /Pg 3 0 R /K 0 >>",
	)
	got := checkLangRequired(ctx, doc, cat, "PDF/UA-1")

	if !hasRule(got, Rule_14289_1_7_2_t34) {
		t.Error("no /Lang anywhere: want §7.2 t34 (undeterminable language)")
	}
}

// TestCheckLangRequired_InlineMarkedContentLangSuppressesT34 pins the guard that
// keeps litho off veraPDF's answer for files whose only /Lang sits on a BDC
// property list. veraPDF reads that tag when it resolves an SETextItem's
// language; litho does not map a structure element back to the BDC wrapping its
// marked content, so it reports nothing rather than risk a false positive.
func TestCheckLangRequired_InlineMarkedContentLangSuppressesT34(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	doc, cat := langFixture(t,
		"/P <</MCID 0 /Lang (en-US)>> BDC\nBT (x) Tj ET\nEMC\n",
		"<< /Type /StructElem /S /P /P 5 0 R /Pg 3 0 R /K 0 >>",
	)
	got := checkLangRequired(ctx, doc, cat, "PDF/UA-1")

	if hasRule(got, Rule_14289_1_7_2_t34) {
		t.Errorf("inline marked-content /Lang: want no §7.2 t34, got %v", got)
	}
}
