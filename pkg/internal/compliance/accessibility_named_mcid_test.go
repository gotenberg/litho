package compliance

import (
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/models"
)

// namedListText shows one line of text through font /F1.
const namedListText = "BT /F1 12 Tf 50 700 Td (x) Tj ET"

// namedListDoc is a one-page tagged document whose marked content names its
// property lists. The page (/StructParents 0) draws its content and may draw
// form /Fm (/StructParents 1); the ParentTree maps MCID 0 of both to /P
// element 6, which reaches the root.
type namedListDoc struct {
	pageProps string // entries of the page's /Properties
	page      string // the page's content
	formRes   string // extra entries of the form's /Resources, such as its /Properties
	form      string // the form's content
	lang      bool   // the catalog carries a /Lang
	elemLang  bool   // element 6 carries a /Lang
	font      string // font /F1, a /Helvetica Type 1 when empty
	extra     []string
}

// objects returns the document's indirect objects, object 1 first.
func (d namedListDoc) objects() []string {
	lang, elemLang := "", ""
	if d.lang {
		lang = " /Lang (en-US)"
	}
	if d.elemLang {
		elemLang = " /Lang (en-US)"
	}
	font := d.font
	if font == "" {
		font = "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>"
	}
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /MarkInfo << /Marked true >> /StructTreeRoot 5 0 R" + lang + " >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /StructParents 0" +
			" /Resources << /Font << /F1 9 0 R >> /XObject << /Fm 8 0 R >> /Properties << " + d.pageProps + " >> >> >>",
		streamObj(d.page),
		"<< /Type /StructTreeRoot /K [6 0 R] /ParentTree 7 0 R >>",
		"<< /Type /StructElem /S /P /P 5 0 R /Pg 3 0 R" + elemLang + " /K [0 << /Type /MCR /MCID 0 /Stm 8 0 R >>] >>",
		"<< /Nums [0 [6 0 R] 1 [6 0 R]] >>",
		fmt.Sprintf("<< /Type /XObject /Subtype /Form /BBox [0 0 612 792] /StructParents 1"+
			" /Resources << /Font << /F1 9 0 R >> %s >> /Length %d >>\nstream\n%s\nendstream",
			d.formRes, len(d.form), d.form),
		font,
	}
	return append(objs, d.extra...)
}

// uaCount validates doc at profile and counts the violations of rule.
func uaCount(t *testing.T, doc *document.Document, profile models.PDFUAProfile, rule Rule) int {
	t.Helper()
	viols, err := ValidatePDFUA(context.Background(), doc, profile)
	if err != nil {
		t.Fatalf("ValidatePDFUA: %v", err)
	}
	n := 0
	for _, e := range viols {
		if v, ok := e.(Violation); ok && v.Rule == rule {
			n++
		}
	}
	return n
}

// TestContentMarking_NamedPropertyLists pins that a BDC naming its property
// list from /Properties ties its content to the structure element its /MCID
// maps (ISO 32000-1, 14.6.2), as an inline list does. litho read the /MCID of
// an inline list alone, so every named sequence read as untagged and failed
// ISO 14289-1, 7.1 t3 and ISO 14289-2, 8.2.2 t1.
//
// A form names through its own resources and then through the page's, as
// veraPDF's PDResourcesHandler.getProperties does: an own definition wins even
// when it is no dictionary, and only a missing or null one falls to the
// page's. Each case measured against veraPDF 1.30.2 at PDF/UA-1.
func TestContentMarking_NamedPropertyLists(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	pageText := "/P /MC0 BDC " + namedListText + " EMC"
	formText := "/P /MC1 BDC " + namedListText + " EMC"
	cases := []struct {
		name     string
		doc      namedListDoc
		untagged bool
	}{
		{"the page names a mapped list", namedListDoc{pageProps: "/MC0 << /MCID 0 >>", page: pageText}, false},
		{"the page names a list through a reference", namedListDoc{pageProps: "/MC0 10 0 R", page: pageText, extra: []string{"<< /MCID 0 >>"}}, false},
		{"the page names an unmapped list", namedListDoc{pageProps: "/MC0 << /MCID 5 >>", page: pageText}, true},
		{"the page names a list it does not define", namedListDoc{page: pageText}, true},
		{"a form names its own list", namedListDoc{page: "q /Fm Do Q", formRes: "/Properties << /MC1 << /MCID 0 >> >>", form: formText}, false},
		{"a form names a list only the page defines", namedListDoc{pageProps: "/MC1 << /MCID 0 >>", page: "q /Fm Do Q", form: formText}, false},
		{"a form's own list wins over the page's", namedListDoc{pageProps: "/MC1 << /MCID 0 >>", page: "q /Fm Do Q", formRes: "/Properties << /MC1 << /MCID 5 >> >>", form: formText}, true},
		{"a form's null entry falls to the page's", namedListDoc{pageProps: "/MC1 << /MCID 0 >>", page: "q /Fm Do Q", formRes: "/Properties << /MC1 null >>", form: formText}, false},
		{"a form's entry that is no dictionary stays", namedListDoc{pageProps: "/MC1 << /MCID 0 >>", page: "q /Fm Do Q", formRes: "/Properties << /MC1 5 >>", form: formText}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, catalog := objsDocPDF(t, tc.doc.objects()...)
			if got := hasRule(checkContentMarking(ctx, doc, catalog, "PDF/UA-1"), Rule_14289_1_7_1_t03); got != tc.untagged {
				t.Errorf("PDF/UA-1 7.1 t3 = %v, want %v", got, tc.untagged)
			}
			if got := hasRule(checkContentMarking(ctx, doc, catalog, "PDF/UA-2"), Rule_14289_2_8_2_2_t01); got != tc.untagged {
				t.Errorf("PDF/UA-2 8.2.2 t1 = %v, want %v", got, tc.untagged)
			}
		})
	}
}

// TestLanguage_NamedPropertyLists pins the language rules that read a
// sequence's property list, named from /Properties as surely as written
// inline, in a catalog with no /Lang. Measured against veraPDF 1.30.2 at
// PDF/UA-1:
//   - text under a named list whose /MCID maps an element with no /Lang fails
//     ISO 14289-1, 7.2 t34, which litho missed, and a /Lang in the list covers
//     it;
//   - a form's text whose /MCID comes from a list only the page defines is
//     owned by an element with a /Lang, where litho reported 7.2 t34;
//   - a form's /Span naming a list only the page defines, whose /ActualText
//     has no language, fails 7.2 t30, which litho missed.
func TestLanguage_NamedPropertyLists(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		doc  namedListDoc
		rule Rule
		want int
	}{
		{"a named MCID owned by an element with no language", namedListDoc{pageProps: "/MC0 << /MCID 0 >>", page: "/P /MC0 BDC " + namedListText + " EMC"}, Rule_14289_1_7_2_t34, 1},
		{"a named MCID whose list carries a language", namedListDoc{pageProps: "/MC0 << /MCID 0 /Lang (en-US) >>", page: "/P /MC0 BDC " + namedListText + " EMC"}, Rule_14289_1_7_2_t34, 0},
		{"a form's MCID from a list only the page defines", namedListDoc{pageProps: "/MC1 << /MCID 0 >>", page: "q /Fm Do Q", form: "/P /MC1 BDC " + namedListText + " EMC", elemLang: true}, Rule_14289_1_7_2_t34, 0},
		{"a form's /Span from a list only the page defines", namedListDoc{pageProps: "/MC1 << /ActualText (x) >>", page: "q /Fm Do Q", form: "/Span /MC1 BDC " + namedListText + " EMC", elemLang: true}, Rule_14289_1_7_2_t30, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t, tc.doc.objects()...)
			if got := uaCount(t, doc, models.PDFUA1, tc.rule); got != tc.want {
				t.Errorf("%s t%d = %d, want %d", tc.rule.Clause, tc.rule.TestNumber, got, tc.want)
			}
		})
	}
}

// TestPUA_FormNamesPageList pins that a form's Private Use glyph is covered by
// an /ActualText in a property list only the page defines, which the form
// names through the page's resources. Measured against veraPDF 1.30.2 at
// PDF/UA-2: the list with /ActualText passes ISO 14289-2, 8.4.3 t1, where
// litho reported it, and the same list without one fails it.
func TestPUA_FormNamesPageList(t *testing.T) {
	t.Parallel()

	cmap := "/CIDInit /ProcSet findresource begin 12 dict begin begincmap" +
		" /CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def" +
		" /CMapName /Adobe-Identity-UCS def /CMapType 2 def 1 begincodespacerange <00> <FF> endcodespacerange" +
		" 1 beginbfchar <78> <E000> endbfchar endcmap CMapName currentdict /CMap defineresource pop end end"
	font := "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding /ToUnicode 10 0 R >>"
	for _, tc := range []struct {
		name  string
		props string
		want  int
	}{
		{"an /ActualText in the page's list", "/MC1 << /MCID 0 /ActualText (y) >>", 0},
		{"no /ActualText in the page's list", "/MC1 << /MCID 0 >>", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			doc, _ := objsDocPDF(t, namedListDoc{
				pageProps: tc.props, page: "q /Fm Do Q", form: "/Span /MC1 BDC " + namedListText + " EMC",
				lang: true, font: font, extra: []string{streamObj(cmap)},
			}.objects()...)
			if got := uaCount(t, doc, models.PDFUA2, Rule_14289_2_8_4_3_t01); got != tc.want {
				t.Errorf("8.4.3 t1 = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestMarkedContentProps_Mcid pins what [markedContentProps.mcid] reads: an
// integer /MCID of a BDC's list alone, never a BMC's or a DP's.
func TestMarkedContentProps_Mcid(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	props := &markedContentProps{resources: ast.PdfDict{"Properties": ast.PdfDict{
		"A": ast.PdfDict{"MCID": ast.PdfInt(3)},
		"B": ast.PdfDict{"MCID": ast.PdfReal(3)},
	}}}
	for _, tc := range []struct {
		name string
		op   string
		arg  ast.PdfObject
		want int
	}{
		{"BDC naming A", "BDC", ast.PdfName("A"), 3},
		{"BDC naming B, a real", "BDC", ast.PdfName("B"), -1},
		{"BDC naming nothing defined", "BDC", ast.PdfName("C"), -1},
		{"DP naming A", "DP", ast.PdfName("A"), -1},
	} {
		op := content.Operation{Operator: tc.op, Operands: []ast.PdfObject{ast.PdfName("P"), tc.arg}}
		if got := props.mcid(ctx, op); got != tc.want {
			t.Errorf("%s: mcid = %d, want %d", tc.name, got, tc.want)
		}
	}
}
