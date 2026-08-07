package compliance

import (
	"context"
	"testing"
)

// TestCheckFormFieldTU_KidWidgetTU pins ISO 14289-1, 7.18.1 t3 against the shape
// veraPDF's PDF_UA-1__7.18.1-t03-fail-d fixture uses: a radio group whose FIELD
// carries no /TU, whose kid widgets each carry a /TU of their own.
//
// /TU is a field key (ISO 32000-1, Table 220). A kid widget is a pure annotation,
// so its /TU says nothing about the field, and honouring it passed a file the
// rule exists to catch.
func TestCheckFormFieldTU_KidWidgetTU(t *testing.T) {
	ctx := context.Background()

	// obj 4 is the field; obj 5 and 6 are its kid widgets.
	build := func(field, kid string) []Violation {
		objs := []string{
			"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R] >> >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [5 0 R] >>",
			field,
			kid,
		}
		doc, cat := objsDocPDF(t, objs...)
		return checkFormFieldTU(ctx, doc, cat, "PDF/UA-1")
	}

	// The failing shape: field has no /TU, the kid widget states its own.
	kidOnly := build(
		"<< /FT /Btn /T (buttons) /Kids [5 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /Parent 4 0 R /TU (btn1) /Rect [0 0 10 10] /F 4 >>")
	if !hasRule(kidOnly, Rule_14289_1_7_18_1_t03) {
		t.Errorf("field without /TU, kid widget with its own: want 7.18.1 t3, got %v", kidOnly)
	}

	// The field states /TU: the kid inherits it and the group is conformant.
	fieldTU := build(
		"<< /FT /Btn /T (buttons) /TU (choose one) /Kids [5 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /Parent 4 0 R /Rect [0 0 10 10] /F 4 >>")
	if hasRule(fieldTU, Rule_14289_1_7_18_1_t03) {
		t.Errorf("field with /TU: want none, got %v", fieldTU)
	}

	// A widget MERGED with its field carries the field's keys directly, so its
	// own /TU is the field's /TU and must still satisfy the rule.
	merged := build(
		"<< /FT /Btn /T (unused) >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /TU (your name) /Rect [0 0 10 10] /F 4 >>")
	if hasRule(merged, Rule_14289_1_7_18_1_t03) {
		t.Errorf("merged field widget with its own /TU: want none, got %v", merged)
	}

	// A hidden kid widget is exempt even though its field states no /TU.
	hidden := build(
		"<< /FT /Btn /T (buttons) /Kids [5 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /Parent 4 0 R /Rect [0 0 10 10] /F 2 >>")
	if hasRule(hidden, Rule_14289_1_7_18_1_t03) {
		t.Errorf("hidden kid widget: want none, got %v", hidden)
	}
}
