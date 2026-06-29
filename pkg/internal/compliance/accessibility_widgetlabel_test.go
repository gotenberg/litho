package compliance

import (
	"context"
	"testing"
)

// TestCheckWidgetLabel exercises ISO 14289-2 §8.10.2.3 t1: a field widget that
// participates in the structure tree and supplies neither a Lbl structure
// element nor a /Contents entry fails, including when it is hidden (veraPDF's
// test has no Hidden exemption).
func TestCheckWidgetLabel(t *testing.T) {
	ctx := context.Background()
	// formChildren and widget vary; obj 5 is the Form structure element, obj 6
	// the ParentTree, obj 7 the OBJR to the widget (obj 8). Extra structure
	// elements (a Lbl) start at obj 9.
	build := func(formK, widget string, extra ...string) []Violation {
		objs := append([]string{
			"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [8 0 R] >>",
			"<< /Type /StructTreeRoot /K 5 0 R /ParentTree 6 0 R >>",
			"<< /Type /StructElem /S /Form /K " + formK + " >>",
			"<< /Nums [0 5 0 R] >>",
			"<< /Type /OBJR /Obj 8 0 R >>",
			widget,
		}, extra...)
		doc, cat := objsDocPDF(t, objs...)
		return checkPDFUA2FormStructure(ctx, doc, cat, "PDF/UA-2")
	}

	// A hidden (/F 2) field widget with no Lbl and no /Contents fails: there is
	// no Hidden exemption.
	hidden := build("[7 0 R]",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /StructParent 0 /F 2 >>")
	if !hasRule(hidden, Rule_14289_2_8_10_2_3_t01) {
		t.Errorf("hidden field widget, no label/Contents: want §8.10.2.3 t1, got %v", hidden)
	}

	// A field widget whose /Contents supplies a description is conformant.
	withContents := build("[7 0 R]",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /StructParent 0 /Contents (label) >>")
	if hasRule(withContents, Rule_14289_2_8_10_2_3_t01) {
		t.Errorf("field widget with /Contents: want none, got %v", withContents)
	}

	// A field widget whose enclosing element carries a Lbl is conformant.
	withLbl := build("[7 0 R 9 0 R]",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /StructParent 0 >>",
		"<< /Type /StructElem /S /Lbl >>")
	if hasRule(withLbl, Rule_14289_2_8_10_2_3_t01) {
		t.Errorf("field widget with Lbl: want none, got %v", withLbl)
	}

	// A non-field widget (no /FT, /Parent, /T) is out of scope.
	nonField := build("[7 0 R]",
		"<< /Type /Annot /Subtype /Widget /StructParent 0 >>")
	if hasRule(nonField, Rule_14289_2_8_10_2_3_t01) {
		t.Errorf("non-field widget: want none, got %v", nonField)
	}
}
