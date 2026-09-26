package compliance

import (
	"context"
	"strconv"
	"testing"
)

// TestTextStringPUA pins the positions ISO 14289-2, 8.6 t1 reaches: every
// string veraPDF reads as a CosTextString, which is an outline item's /Title
// and every CosLang. An annotation's own /Lang and a structure element's
// /ActualText are no CosTextString, so a PUA code point there answers nothing
// here. Each case was measured on veraPDF 1.30.2 with a crafted file.
func TestTextStringPUA(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	const pua = "<FEFFE000>"
	build := func(catalogLang, elemExtra, annotElemExtra, annotExtra, bdcExtra string) []Violation {
		t.Helper()
		if catalogLang == "" {
			catalogLang = "(en-US)"
		}
		content := "/P <</MCID 0 " + bdcExtra + ">> BDC\nEMC\n"
		doc, catalog := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /Lang "+catalogLang+" >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 9 0 R /Annots [8 0 R] /StructParents 0 >>",
			"<< /Type /StructTreeRoot /K 5 0 R /ParentTree << /Nums [0 [6 0 R] 1 7 0 R] >> >>",
			"<< /Type /StructElem /S /Document /P 4 0 R /K [6 0 R] >>",
			"<< /Type /StructElem /S /P /P 5 0 R /Pg 3 0 R /K 0 "+elemExtra+" >>",
			// The element the annotation names by /StructParent, which the
			// tree's /K never reaches.
			"<< /Type /StructElem /S /Annot /P 5 0 R /K << /Type /OBJR /Obj 8 0 R >> "+annotElemExtra+" >>",
			"<< /Type /Annot /Subtype /Text /Rect [0 0 10 10] /Contents (a note) /StructParent 1 "+annotExtra+" >>",
			"<< /Length "+strconv.Itoa(len(content))+" >>\nstream\n"+content+"endstream",
		)
		return checkTextStringPUA(ctx, buildValidationCtx(ctx, doc, catalog, "PDF/UA-2"))
	}

	for _, tc := range []struct {
		name                                 string
		catalog, elem, annotElem, annot, bdc string
		want                                 bool
	}{
		{name: "no PUA anywhere"},
		{name: "catalog /Lang", catalog: pua, want: true},
		{name: "structure element /Lang", elem: "/Lang " + pua, want: true},
		{name: "annotation's enclosing element /Lang", annotElem: "/Lang " + pua, want: true},
		{name: "marked-content /Lang", bdc: "/Lang " + pua, want: true},
		{name: "annotation's own /Lang", annot: "/Lang " + pua},
		{name: "structure element /ActualText", elem: "/ActualText " + pua},
		{name: "a noncharacter of plane 15", elem: "/Lang <FEFFDBBFDFFE>"},
		{name: "plane 16 private use", elem: "/Lang <FEFFDBFFDFFD>", want: true},
	} {
		got := hasRule(build(tc.catalog, tc.elem, tc.annotElem, tc.annot, tc.bdc), Rule_14289_2_8_6_t01)
		if got != tc.want {
			t.Errorf("%s: 8.6 t1 reported = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestTextStringPUA_OutlineTitle pins that an outline item's /Title, the
// CosTextString veraPDF builds outside every CosLang, still reaches ISO
// 14289-2, 8.6 t1, and that its conforming sibling does not.
func TestTextStringPUA_OutlineTitle(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, tc := range []struct {
		title string
		want  bool
	}{
		{"<FEFF0061E000>", true},
		{"(a plain heading)", false},
	} {
		doc, catalog := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R /Outlines 3 0 R >>",
			"<< /Type /Pages /Kids [] /Count 0 >>",
			"<< /Type /Outlines /First 4 0 R /Last 4 0 R /Count 1 >>",
			"<< /Title "+tc.title+" /Parent 3 0 R >>",
		)
		got := hasRule(checkTextStringPUA(ctx, buildValidationCtx(ctx, doc, catalog, "PDF/UA-2")), Rule_14289_2_8_6_t01)
		if got != tc.want {
			t.Errorf("outline /Title %s: 8.6 t1 reported = %v, want %v", tc.title, got, tc.want)
		}
	}
}
