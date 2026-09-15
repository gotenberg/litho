package compliance

import (
	"context"
	"testing"
)

// TestCheckWidgetLabel_ByStandardType pins how ISO 14289-2 §8.10.2.3 t1 finds
// the Lbl that labels a field widget: a kid of the widget's enclosing element
// whose standard type is Lbl and that has content of its own.
//
// veraPDF types the kid through the /RoleMap first and ignores an empty Lbl
// (GFPDWidgetAnnot.getcontainsLbl). On one-widget fixtures it passes a
// MyLabel mapped to Lbl, and reports an empty Lbl and a Lbl mapped to Span.
func TestCheckWidgetLabel_ByStandardType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		roleMap string
		label   string
		fires   bool
	}{
		{"a Lbl with content", "", "<< /Type /StructElem /S /Lbl /K 0 >>", false},
		{"an empty Lbl", "", "<< /Type /StructElem /S /Lbl >>", true},
		{"a custom label mapped to Lbl", "/RoleMap << /MyLabel /Lbl >>", "<< /Type /StructElem /S /MyLabel /K 0 >>", false},
		{"a Lbl the role map remaps", "/RoleMap << /Lbl /Span >>", "<< /Type /StructElem /S /Lbl /K 0 >>", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, cat := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [8 0 R] >>",
				"<< /Type /StructTreeRoot /K 5 0 R /ParentTree 6 0 R "+tt.roleMap+" >>",
				"<< /Type /StructElem /S /Form /K [7 0 R 9 0 R] >>",
				"<< /Nums [0 5 0 R] >>",
				"<< /Type /OBJR /Obj 8 0 R >>",
				"<< /Type /Annot /Subtype /Widget /FT /Tx /T (f) /StructParent 0 >>",
				tt.label,
			)
			got := checkPDFUA2FormStructure(context.Background(), doc, cat, "PDF/UA-2")
			if fired := hasRule(got, Rule_14289_2_8_10_2_3_t01); fired != tt.fires {
				t.Errorf("8.10.2.3 t1 fired = %v, want %v (%v)", fired, tt.fires, got)
			}
		})
	}
}

// TestCheckFormWidgetCount_ByStandardType pins that ISO 14289-2 §8.10.1 t2 (at
// most one widget per Form) applies to an element whose standard type is Form.
// veraPDF builds its SEForm object from the standard type, which the /RoleMap
// decides first (GFSEFactory.createTypedStructElem).
func TestCheckFormWidgetCount_ByStandardType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		roleMap string
		s       string
		fires   bool
	}{
		{"a Form with two widgets", "", "/Form", true},
		{"a custom type mapped to Form", "/RoleMap << /MyForm /Form >>", "/MyForm", true},
		{"a Form the role map remaps", "/RoleMap << /Form /Div >>", "/Form", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, cat := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [8 0 R 10 0 R] >>",
				"<< /Type /StructTreeRoot /K 5 0 R /ParentTree 6 0 R "+tt.roleMap+" >>",
				"<< /Type /StructElem /S "+tt.s+" /K [7 0 R 9 0 R] >>",
				"<< /Nums [0 5 0 R 1 5 0 R] >>",
				"<< /Type /OBJR /Obj 8 0 R >>",
				"<< /Type /Annot /Subtype /Widget /FT /Tx /T (a) /StructParent 0 /Contents (a) >>",
				"<< /Type /OBJR /Obj 10 0 R >>",
				"<< /Type /Annot /Subtype /Widget /FT /Tx /T (b) /StructParent 1 /Contents (b) >>",
			)
			got := checkPDFUA2FormStructure(context.Background(), doc, cat, "PDF/UA-2")
			if fired := hasRule(got, Rule_14289_2_8_10_1_t02); fired != tt.fires {
				t.Errorf("8.10.1 t2 fired = %v, want %v (%v)", fired, tt.fires, got)
			}
		})
	}
}
