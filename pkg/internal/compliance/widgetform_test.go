package compliance

import (
	"context"
	"testing"
)

func TestUA2WidgetForm(t *testing.T) {
	ctx := context.Background()
	// Build a UA-2 document where the structure element of type enclType encloses
	// (via OBJR) the widget annotation described by widgetBody.
	build := func(enclType, widgetBody string) []Violation {
		doc, cat := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /AcroForm << /Fields [8 0 R] >> >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [8 0 R] >>",
			"<< /Type /StructTreeRoot /Namespaces [5 0 R] /K 6 0 R >>",
			"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
			"<< /Type /StructElem /S /Document /NS 5 0 R /P 4 0 R /K 7 0 R >>",
			"<< /Type /StructElem /S /"+enclType+" /NS 5 0 R /P 6 0 R /K 9 0 R >>",
			widgetBody,
			"<< /Type /OBJR /Obj 8 0 R >>",
		)
		return checkPDFUA2AnnotStructNesting(ctx, doc, cat, "PDF/UA-2")
	}

	fieldWidget := "<< /Type /Annot /Subtype /Widget /FT /Tx /Rect [0 0 10 10] /StructParent 0 >>"

	// A field widget enclosed by a Form element is conformant.
	if got := build("Form", fieldWidget); hasRule(got, Rule_14289_2_8_10_1_t01) {
		t.Errorf("widget in Form: got %v, want none", got)
	}
	// Enclosed by a non-Form element: §8.10.1 t1.
	if got := build("Div", fieldWidget); !hasRule(got, Rule_14289_2_8_10_1_t01) {
		t.Errorf("widget in Div: got %v, want §8.10.1 t1", got)
	}
	// A widget not associated with a form field (no /FT, no /Parent) is exempt.
	nonField := "<< /Type /Annot /Subtype /Widget /Rect [0 0 10 10] /StructParent 0 >>"
	if got := build("Div", nonField); hasRule(got, Rule_14289_2_8_10_1_t01) {
		t.Errorf("non-field widget: got %v, want none", got)
	}

	// §8.9.2.4.16 t1: a Watermark in the structure tree must be in an Annot element.
	watermark := "<< /Type /Annot /Subtype /Watermark /Rect [0 0 10 10] /StructParent 0 >>"
	if got := build("Annot", watermark); hasRule(got, Rule_14289_2_8_9_2_4_16_t01) {
		t.Errorf("Watermark in Annot: got %v, want none", got)
	}
	if got := build("Div", watermark); !hasRule(got, Rule_14289_2_8_9_2_4_16_t01) {
		t.Errorf("Watermark in Div: got %v, want §8.9.2.4.16 t1", got)
	}
}
