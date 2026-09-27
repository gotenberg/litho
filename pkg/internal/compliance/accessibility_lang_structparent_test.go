package compliance

import (
	"context"
	"strconv"
	"testing"
)

// TestStructParentLangValidated pins that ISO 14289-1, 7.2 t29, ISO 14289-2,
// 8.4.4 t2, ISO 19005-1, 6.8.4 t1 and ISO 19005-2 and -3, 6.7.4 t1 judge the
// /Lang of the structure element an annotation or a form field names by its
// /StructParent, as veraPDF reads it (GFPDAnnot.getLang,
// GFPDFormField.getLang), whether or not the structure tree reaches that
// element.
//
// Each case was measured on veraPDF 1.30.2 at PDF/UA-1 and PDF/UA-2, with and
// without a catalog /Lang, and at PDF/A-1a and PDF/A-2a, on a file holding one
// annotation per case: the counts below are veraPDF's. An element both the tree
// and an annotation reach is judged once for each, and a widget that is also a
// field of the /AcroForm once as an annotation and once as a field. The PDF/A
// rules admit an empty tag, which the PDF/UA rules do not.
func TestStructParentLangValidated(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	build := func(catalogLang, annotExtra, elem string, inTree, field bool, profile string) []Violation {
		t.Helper()
		acroForm, subtype := "", "/Text"
		if field {
			acroForm = "/AcroForm << /Fields [8 0 R] >> "
			subtype = "/Widget /FT /Tx /T (f)"
		}
		// The annotation names element 7 unless it names the tree's own /P.
		named := "7 0 R"
		if inTree {
			named = "6 0 R"
		}
		content := "/P <</MCID 0>> BDC\nEMC\n"
		doc, catalog := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R "+acroForm+catalogLang+" >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 9 0 R /Annots [8 0 R] /StructParents 0 >>",
			"<< /Type /StructTreeRoot /K 5 0 R /ParentTree << /Nums [0 [6 0 R] 1 "+named+"] >> >>",
			"<< /Type /StructElem /S /Document /P 4 0 R /K [6 0 R] >>",
			"<< /Type /StructElem /S /P /P 5 0 R /Pg 3 0 R /K 0 /Lang (in_tree) >>",
			// The tree's /K never reaches this element.
			elem,
			"<< /Type /Annot /Subtype "+subtype+" /Rect [0 0 10 10] /Contents (a note) /StructParent 1 "+annotExtra+" >>",
			"<< /Length "+strconv.Itoa(len(content))+" >>\nstream\n"+content+"endstream",
		)
		vctx := buildValidationCtx(ctx, doc, catalog, profile)
		if IsPDFUA(profile) {
			return checkAccessibilityGroup(ctx, vctx)
		}
		return checkStructureGroup(ctx, vctx)
	}

	const orphan = "<< /Type /StructElem /S /Annot /P 5 0 R /K << /Type /OBJR /Obj 8 0 R >> "
	for _, tc := range []struct {
		name       string
		annotExtra string
		elem       string
		inTree     bool
		field      bool
		// want counts the tree's own malformed /Lang (in_tree) too.
		want int
		// emptyTag marks the case the PDF/A rules pass.
		emptyTag bool
	}{
		{name: "element outside the tree", elem: orphan + "/Lang (orphan_x) >>", want: 2},
		{name: "element the tree reaches too", elem: orphan + ">>", inTree: true, want: 2},
		{name: "entry that is no structure element", elem: "<< /Lang (no_type) >>", want: 2},
		{name: "empty tag", elem: orphan + "/Lang () >>", want: 2, emptyTag: true},
		{name: "well-formed tag", elem: orphan + "/Lang (de-DE) >>", want: 1},
		{name: "widget that is a form field", elem: orphan + "/Lang (field_x) >>", field: true, want: 3},
		{name: "annotation's own /Lang", annotExtra: "/Lang (own_x)", elem: orphan + ">>", want: 1},
		{name: "a name, not a string", elem: orphan + "/Lang /name >>", want: 1},
	} {
		for _, profile := range []string{"PDF/UA-1", "PDF/UA-2", "PDF/A-1a", "PDF/A-2a", "PDF/A-3a"} {
			rule, want := langValidRule(profile), tc.want
			if !IsPDFUA(profile) {
				rule = pdfaLangValidRule(PDFAPart(profile))
				if tc.emptyTag {
					want--
				}
			}
			for _, catalogLang := range []string{"/Lang (en-US)", ""} {
				got := countRule(build(catalogLang, tc.annotExtra, tc.elem, tc.inTree, tc.field, profile), rule)
				if got != want {
					t.Errorf("%s, %s, catalog %q: %s fired %d time(s), want %d",
						tc.name, profile, catalogLang, rule.Clause, got, want)
				}
			}
		}
	}
}
