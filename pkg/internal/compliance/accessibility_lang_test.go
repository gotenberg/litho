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
// reports nothing, even with no /Lang in reach.
//
// # Corpus evidence
//
//   - verapdf 7.2-t22-fail-a: a /Figure whose marked content draws an image
//     XObject carries no /Lang in reach, and veraPDF reports no t34.
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

// TestCheckLangRequired_InlineMarkedContentLangSuppressesT34 pins that a /Lang
// on the BDC property list wrapping an element's text determines that text's
// language. veraPDF reads that tag when it resolves an SETextItem's language,
// and litho resolves it through the marked-content sequence that owns the
// element's text ([structElemsShowingText]), so it reports nothing.
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

// TestCheckOutlineLang_EveryReachableItem pins that §7.2 t2 judges every
// dictionary reached through /First and /Next, titled or not. veraPDF models
// each one as a PDOutline (OutlinesHelper asks only isDictionaryBased), so a
// page object in that place is reported too. Every case was measured against
// veraPDF.
//
// # Corpus evidence
//
//   - pdfjs issue10880.pdf: points /Outlines /First at a page object, which
//     veraPDF reports.
func TestCheckOutlineLang_EveryReachableItem(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	tests := []struct {
		name     string
		outlines string
		items    []string
		want     int
	}{
		{
			"an item without /Title", "<< /Type /Outlines /First 5 0 R /Count 1 >>",
			[]string{"<< /Parent 4 0 R >>"},
			1,
		},
		{
			"a page in the item's place", "<< /Type /Outlines /First 3 0 R /Count 1 >>",
			nil, 1,
		},
		{
			"an untitled item before a titled one", "<< /Type /Outlines /First 5 0 R /Last 6 0 R /Count 2 >>",
			[]string{"<< /Parent 4 0 R /Next 6 0 R >>", "<< /Title (Chapter 2) /Parent 4 0 R /Prev 5 0 R >>"},
			2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			objs := append([]string{
				"<< /Type /Catalog /Pages 2 0 R /Outlines 4 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
				tt.outlines,
			}, tt.items...)
			doc, cat := tableDoc(t, 1, objs...)
			got := checkOutlineLang(ctx, doc, cat, "PDF/UA-1")
			if n := countRuleLang(got, Rule_14289_1_7_2_t02); n != tt.want {
				t.Errorf("want %d §7.2 t2, got %d (%v)", tt.want, n, got)
			}
		})
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

// TestXMPLangAltOnlyXDefault pins the subject of ISO 14289-1 §7.2 t33. veraPDF
// scopes the rule to a language alternative (XMPLangAlt): a top-level property
// whose items all carry xml:lang, failing when its single item is qualified
// x-default. A packet holding none presents nothing whose language could be
// undeterminable. Every case was measured against veraPDF.
func TestXMPLangAltOnlyXDefault(t *testing.T) {
	t.Parallel()

	packet := func(rdfNS, props string) string {
		return `<x:xmpmeta xmlns:x="adobe:ns:meta/"><rdf:RDF xmlns:rdf="` + rdfNS + `">` +
			`<rdf:Description rdf:about="" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:ex="http://example.com/ns/">` +
			props + `</rdf:Description></rdf:RDF></x:xmpmeta>`
	}
	const rdfNS = "http://www.w3.org/1999/02/22-rdf-syntax-ns#"
	title := `<dc:title><rdf:Alt><rdf:li xml:lang="x-default">T</rdf:li></rdf:Alt></dc:title>`

	tests := []struct {
		name string
		xmp  string
		want bool
	}{
		{"alternative qualified x-default", xdefaultXMP("x-default"), true},
		{"x-default beside a specific tag", xdefaultXMP("x-default", "en-US"), false},
		{"no alternative", xdefaultXMP(), false},
		{"alternative qualified en-US", xdefaultXMP("en-US"), false},
		{"another property's specific tag does not rescue it", packet(rdfNS, title+
			`<dc:description><rdf:Alt><rdf:li xml:lang="en-US">D</rdf:li></rdf:Alt></dc:description>`), true},
		{"a Bag with one x-default item", packet(rdfNS,
			`<dc:subject><rdf:Bag><rdf:li xml:lang="x-default">S</rdf:li></rdf:Bag></dc:subject>`), true},
		{"two x-default items", packet(rdfNS,
			`<dc:title><rdf:Alt><rdf:li xml:lang="x-default">A</rdf:li><rdf:li xml:lang="x-default">B</rdf:li></rdf:Alt></dc:title>`), false},
		{"an item without xml:lang", packet(rdfNS,
			`<dc:subject><rdf:Bag><rdf:li xml:lang="x-default">A</rdf:li><rdf:li>B</rdf:li></rdf:Bag></dc:subject>`), false},
		{"x-default on a simple property", packet(rdfNS, `<dc:format xml:lang="x-default">application/pdf</dc:format>`), false},
		{"x-default inside a structure field", packet(rdfNS,
			`<ex:Outer rdf:parseType="Resource"><ex:Inner><rdf:Alt><rdf:li xml:lang="x-default">I</rdf:li></rdf:Alt></ex:Inner></ex:Outer>`), false},
		{"rdf prefix bound to another URI", packet("http://example.com/rdf#", title), false},
		{"a control character inside the item", packet(rdfNS,
			`<dc:description><rdf:Alt><rdf:li xml:lang="x-default">&#x5;4.1.1</rdf:li></rdf:Alt></dc:description>`), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := xmpLangAltOnlyXDefault([]byte(tt.xmp)); got != tt.want {
				t.Errorf("xmpLangAltOnlyXDefault = %v, want %v", got, tt.want)
			}
		})
	}
}

// mcidsShowingText reduces [mcidTextItems] to whether an enclosing /Lang
// covers each identifier's text.
func mcidsShowingText(ctx context.Context, ops []content.Operation, props *markedContentProps) map[int]bool {
	found := make(map[int]bool)
	for mcid, text := range mcidTextItems(ctx, ops, props) {
		found[mcid] = text.covered
	}
	return found
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

	inline := &markedContentProps{}

	t.Run("a tag covers its own sequence only", func(t *testing.T) {
		got := mcidsShowingText(ctx, []content.Operation{
			bdc(0, true), show, emc,
			bdc(1, false), show, emc,
		}, inline)
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
		}, inline)
		if !got[1] {
			t.Error("MCID 1 is nested inside a tagged sequence and should be covered")
		}
	})

	t.Run("no tag anywhere covers nothing", func(t *testing.T) {
		got := mcidsShowingText(ctx, []content.Operation{bdc(0, false), show, emc}, inline)
		if _, owns := got[0]; !owns {
			t.Fatal("MCID 0 owns the text and should be present")
		}
		if got[0] {
			t.Error("MCID 0 has no /Lang and must not be covered")
		}
	})

	// A property list named from the page's /Properties carries its /MCID and
	// /Lang as an inline one does (ISO 32000-1, 14.6.2). Measured against
	// veraPDF 1.30.2: text under such a list is owned by the element its MCID
	// maps, and a /Lang in it determines the text's language.
	t.Run("a named property list owns and covers like an inline one", func(t *testing.T) {
		named := &markedContentProps{resources: ast.PdfDict{"Properties": ast.PdfDict{
			"MC0": ast.PdfDict{"MCID": ast.PdfInt(0)},
			"MC1": ast.PdfDict{"MCID": ast.PdfInt(1), "Lang": ast.PdfString("en-US")},
		}}}
		bdcNamed := func(name string) content.Operation {
			return content.Operation{Operator: "BDC", Operands: []ast.PdfObject{ast.PdfName("P"), ast.PdfName(name)}}
		}
		got := mcidsShowingText(ctx, []content.Operation{
			bdcNamed("MC0"), show, emc,
			bdcNamed("MC1"), show, emc,
			bdcNamed("MC9"), show, emc,
		}, named)
		if covered, owns := got[0]; !owns || covered {
			t.Errorf("MCID 0 = (%v, %v), want owned and not covered", covered, owns)
		}
		if !got[1] {
			t.Error("MCID 1 carries a /Lang in its named list and should be covered")
		}
		if len(got) != 2 {
			t.Errorf("got %v, want MCIDs 0 and 1 alone: MC9 names no list", got)
		}
	})
}
