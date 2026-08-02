package compliance

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
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

// treelessLangDoc builds a one-page document with no /StructTreeRoot: stream is
// the page content and catalogEntries are extra catalog keys (pass "" for none).
func treelessLangDoc(t *testing.T, catalogEntries, stream string) (*document.Document, ast.PdfDict) {
	t.Helper()
	return tableDoc(t, 1,
		"<< /Type /Catalog /Pages 2 0 R "+catalogEntries+">>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(stream), stream),
	)
}

// TestCheckLangRequired_TreelessDocumentNeedsATextItem covers the subject of ISO
// 14289-1 §7.2 t34 on a document with no /StructTreeRoot. The rule's object is
// SETextItem, which veraPDF builds only where a content stream shows text, so a
// page painting nothing but graphics leaves the rule with nothing to report. One
// Tj brings the subject into existence, and a catalog /Lang then determines its
// language (gContainsCatalogLang == true || Lang != null). The absent structure
// tree is a violation of §7.1 t11, not of this rule.
//
// With no tree, the marked content wrapping a text run is the only other source
// of a language, and it covers that run alone: a /Lang on one sequence leaves a
// bare run elsewhere on the page undetermined, exactly as veraPDF's outward walk
// from each text-showing operator does.
func TestCheckLangRequired_TreelessDocumentNeedsATextItem(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name    string
		catalog string
		stream  string
		want    bool
	}{
		{"graphics only, no catalog /Lang", "", "0 0 1 rg 10 10 20 20 re f\n", false},
		{"text, no catalog /Lang", "", "BT (x) Tj ET\n", true},
		{"text, catalog /Lang", "/Lang (en-US) ", "BT (x) Tj ET\n", false},
		{
			"text under a marked-content /Lang", "",
			"/Span <</Lang (en-US)>> BDC\nBT (x) Tj ET\nEMC\n", false,
		},
		{
			"one covered run, one bare run", "",
			"/Span <</Lang (en-US)>> BDC\nBT (x) Tj ET\nEMC\nBT (y) Tj ET\n", true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			doc, cat := treelessLangDoc(t, tc.catalog, tc.stream)
			got := checkLangRequired(ctx, doc, cat, "PDF/UA-1")

			if hasRule(got, Rule_14289_1_7_2_t34) != tc.want {
				t.Errorf("§7.2 t34 on a tree-less document: want fired=%v, got %v", tc.want, got)
			}
		})
	}
}

// TestCheckLangRequired_TreelessFormXObjectTextIsATextItem extends the same
// subject test through a Do. veraPDF builds an SETextItem for text a Form
// XObject paints just as it does for text in a page stream, so text reachable
// only through the XObject keeps ISO 14289-1 §7.2 t34 firing.
func TestCheckLangRequired_TreelessFormXObjectTextIsATextItem(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	pageStream := "q /X0 Do Q\n"
	formStream := "BT (x) Tj ET\n"
	doc, cat := tableDoc(t, 1,
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R "+
			"/Resources << /XObject << /X0 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(pageStream), pageStream),
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 612 792] /Length %d >>\nstream\n%sendstream",
			len(formStream), formStream),
	)
	got := checkLangRequired(ctx, doc, cat, "PDF/UA-1")

	if !hasRule(got, Rule_14289_1_7_2_t34) {
		t.Errorf("text painted by a Form XObject is still a text item: want §7.2 t34, got %v", got)
	}
}

// outlineLangDoc builds a document whose catalog carries catalogEntries plus an
// /Outlines pointing at two sibling items, the first of them carrying itemLang
// verbatim (pass "" for none).
func outlineLangDoc(t *testing.T, catalogEntries, itemLang string) (*document.Document, ast.PdfDict) {
	t.Helper()
	return tableDoc(t, 1,
		"<< /Type /Catalog /Pages 2 0 R /Outlines 4 0 R "+catalogEntries+">>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /Outlines /First 5 0 R /Last 6 0 R /Count 2 >>",
		"<< /Title (Chapter 1) /Parent 4 0 R /Next 6 0 R "+itemLang+">>",
		"<< /Title (Chapter 2) /Parent 4 0 R /Prev 5 0 R >>",
	)
}

// TestCheckOutlineLang_ItemLangDoesNotExemptEntry pins ISO 14289-1 §7.2 t2 for an
// outline item carrying its own /Lang. ISO 32000-1 Table 153 defines no /Lang on
// an outline item dictionary, so such a key is undefined and cannot make the
// title's language determinable: veraPDF's PDOutline rule tests only
// gContainsCatalogLang == true and its PDOutline model object exposes no
// per-item language at all. Both items must be reported.
func TestCheckOutlineLang_ItemLangDoesNotExemptEntry(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	doc, cat := outlineLangDoc(t, "", "/Lang (en-US) ")
	got := checkOutlineLang(ctx, doc, cat, "PDF/UA-1")

	if n := countRuleLang(got, Rule_14289_1_7_2_t02); n != 2 {
		t.Errorf("undefined /Lang on an outline item grants no exemption: want 2 §7.2 t2, got %d (%v)", n, got)
	}
}

// TestCheckOutlineLang_CatalogLangCoversOutline is the control: the catalog
// /Lang of ISO 32000-1 §14.9.2 is the one source that determines an outline
// title's language, so no item is reported when it is present.
func TestCheckOutlineLang_CatalogLangCoversOutline(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	doc, cat := outlineLangDoc(t, "/Lang (en-US) ", "")
	got := checkOutlineLang(ctx, doc, cat, "PDF/UA-1")

	if hasRule(got, Rule_14289_1_7_2_t02) {
		t.Errorf("catalog /Lang determines every outline title: want no §7.2 t2, got %v", got)
	}
}

// xdefaultXMP builds a catalog XMP packet whose dc:title alternative carries
// the given xml:lang qualifiers. Passing none omits the alternative entirely.
func xdefaultXMP(langs ...string) string {
	alt := ""
	if len(langs) > 0 {
		var items strings.Builder
		for _, l := range langs {
			items.WriteString(`<rdf:li xml:lang="` + l + `">T</rdf:li>`)
		}
		alt = `<rdf:Description rdf:about="" xmlns:dc="http://purl.org/dc/elements/1.1/">` +
			`<dc:title><rdf:Alt>` + items.String() + `</rdf:Alt></dc:title></rdf:Description>`
	}
	return `<x:xmpmeta xmlns:x="adobe:ns:meta/">` +
		`<rdf:RDF xmlns:rdf="http://www.w3.org/1999/02/22-rdf-syntax-ns#">` +
		alt + `</rdf:RDF></x:xmpmeta>`
}

// TestXMPHasXDefaultLangAlt pins the subject of ISO 14289-1 §7.2 t33. veraPDF
// scopes the rule to a language alternative (XMPLangAlt), so a packet holding
// none presents nothing whose language could be undeterminable. Reporting on a
// bare packet flagged every metadata stream without a dc:title.
func TestXMPHasXDefaultLangAlt(t *testing.T) {
	t.Parallel()

	if !xmpHasXDefaultLangAlt([]byte(xdefaultXMP("x-default"))) {
		t.Error("alternative qualified x-default: want detected")
	}
	if !xmpHasXDefaultLangAlt([]byte(xdefaultXMP("x-default", "en-US"))) {
		t.Error("alternative carrying x-default beside a specific tag: want detected")
	}
	if xmpHasXDefaultLangAlt([]byte(xdefaultXMP())) {
		t.Error("packet with no alternative: want none, the rule has no subject")
	}
	if xmpHasXDefaultLangAlt([]byte(xdefaultXMP("en-US"))) {
		t.Error("alternative qualified only en-US: want none, xDefault is false")
	}
}

// TestMcidsShowingText_InlineLangIsPerSequence pins that an inline /Lang covers
// only the text inside the marked-content sequence carrying it, and the
// sequences nested in that one. ISO 14289-1 §7.2 t34 is decided per text item,
// and veraPDF resolves an item's language by walking the enclosing sequences
// outwards, so a tag on one sequence leaves a sibling's text undetermined.
// litho used to take the answer document-wide, which silenced the rule for
// every file carrying a single inline tag anywhere.
func TestMcidsShowingText_InlineLangIsPerSequence(t *testing.T) {
	ctx := context.Background()

	bdc := func(mcid int, lang bool) content.Operation {
		props := ast.PdfDict{"MCID": ast.PdfInt(mcid)}
		if lang {
			props["Lang"] = ast.PdfString("en-US")
		}
		return content.Operation{
			Operator: "BDC",
			Operands: []ast.PdfObject{ast.PdfName("P"), props},
		}
	}
	show := content.Operation{Operator: "Tj", Operands: []ast.PdfObject{ast.PdfString("x")}}
	emc := content.Operation{Operator: "EMC"}

	t.Run("a tag covers its own sequence only", func(t *testing.T) {
		got := mcidsShowingText(ctx, []content.Operation{
			bdc(0, true), show, emc,
			bdc(1, false), show, emc,
		})
		if !got[0] {
			t.Error("MCID 0 carries the /Lang and should be covered")
		}
		if got[1] {
			t.Error("MCID 1 has no /Lang and must not be covered by its sibling's")
		}
	})

	t.Run("a tag covers nested sequences", func(t *testing.T) {
		// The outer sequence carries the tag; the inner one owns the text.
		got := mcidsShowingText(ctx, []content.Operation{
			bdc(0, true), bdc(1, false), show, emc, emc,
		})
		if !got[1] {
			t.Error("MCID 1 is nested inside a tagged sequence and should be covered")
		}
	})

	t.Run("no tag anywhere covers nothing", func(t *testing.T) {
		got := mcidsShowingText(ctx, []content.Operation{bdc(0, false), show, emc})
		if _, owns := got[0]; !owns {
			t.Fatal("MCID 0 owns the text and should be present")
		}
		if got[0] {
			t.Error("MCID 0 has no /Lang and must not be covered")
		}
	})
}
