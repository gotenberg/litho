package compliance

import (
	"context"
	"testing"
)

// TestUA2MarkupAnnotOutsideStructureTree pins the ISO 14289-2 §8.9.2.3 t1
// exemption for a markup annotation that no structure element encloses. The
// clause constrains the enclosing element, so an annotation absent from the
// tree is an artifact and passes; veraPDF encodes the same exemption as
// structParentType == null. Reporting it flagged every out-of-tree markup
// annotation, none of which veraPDF flags.
func TestUA2MarkupAnnotOutsideStructureTree(t *testing.T) {
	ctx := context.Background()

	// Build a UA-2 document whose Square markup annotation is enclosed by a
	// structure element of type enclType. Passing an empty enclType leaves the
	// annotation out of the structure tree entirely.
	build := func(enclType string) []Violation {
		square := "<< /Type /Annot /Subtype /Square /Rect [0 0 10 10] /Contents (a square) /StructParent 0 >>"
		if enclType == "" {
			doc, cat := objsDocPDF(t,
				"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
				"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
				"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [8 0 R] >>",
				"<< /Type /StructTreeRoot /Namespaces [5 0 R] /K 6 0 R >>",
				"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
				"<< /Type /StructElem /S /Document /NS 5 0 R /P 4 0 R /K 7 0 R >>",
				"<< /Type /StructElem /S /P /NS 5 0 R /P 6 0 R >>",
				square,
			)
			return checkPDFUA2AnnotStructNesting(ctx, doc, cat, "PDF/UA-2")
		}
		doc, cat := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [8 0 R] >>",
			"<< /Type /StructTreeRoot /Namespaces [5 0 R] /K 6 0 R >>",
			"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
			"<< /Type /StructElem /S /Document /NS 5 0 R /P 4 0 R /K 7 0 R >>",
			"<< /Type /StructElem /S /"+enclType+" /NS 5 0 R /P 6 0 R /K 9 0 R >>",
			square,
			"<< /Type /OBJR /Obj 8 0 R >>",
		)
		return checkPDFUA2AnnotStructNesting(ctx, doc, cat, "PDF/UA-2")
	}

	if got := build(""); hasRule(got, Rule_14289_2_8_9_2_3_t01) {
		t.Errorf("markup annotation outside the structure tree: got %v, want none", got)
	}
	if got := build("Annot"); hasRule(got, Rule_14289_2_8_9_2_3_t01) {
		t.Errorf("markup annotation in Annot: got %v, want none", got)
	}
	if got := build("Div"); !hasRule(got, Rule_14289_2_8_9_2_3_t01) {
		t.Errorf("markup annotation in Div: got %v, want §8.9.2.3 t1", got)
	}
}
