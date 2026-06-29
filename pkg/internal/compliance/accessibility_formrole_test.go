package compliance

import (
	"context"
	"testing"
)

// TestCheckPDFUA1FormRole exercises ISO 14289-1 §7.18.4 t2: a Form structure
// element that omits a PrintField Role attribute must have exactly one child, an
// object reference identifying the widget annotation.
func TestCheckPDFUA1FormRole(t *testing.T) {
	ctx := context.Background()
	build := func(profile, form string, extra ...string) []Violation {
		objs := append([]string{
			"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
			"<< /Type /StructTreeRoot /K 5 0 R >>",
			form,
		}, extra...)
		doc, cat := objsDocPDF(t, objs...)
		return checkPDFUA1FormRole(ctx, doc, cat, profile)
	}

	// A Form omitting a Role attribute with two children fails §7.18.4 t2.
	twoChildren := build("PDF/UA-1",
		"<< /Type /StructElem /S /Form /K [6 0 R 7 0 R] >>",
		"<< /Type /OBJR /Obj 8 0 R >>",
		"<< /Type /OBJR /Obj 9 0 R >>",
		"<< /Type /Annot /Subtype /Widget >>",
		"<< /Type /Annot /Subtype /Widget >>",
	)
	if !hasRule(twoChildren, Rule_14289_1_7_18_4_t02) {
		t.Errorf("two-child Form without Role: want §7.18.4 t2, got %v", twoChildren)
	}

	// A Form omitting a Role attribute whose only child is a widget object
	// reference is conformant.
	oneWidget := build("PDF/UA-1",
		"<< /Type /StructElem /S /Form /K [6 0 R] >>",
		"<< /Type /OBJR /Obj 7 0 R >>",
		"<< /Type /Annot /Subtype /Widget >>",
	)
	if hasRule(oneWidget, Rule_14289_1_7_18_4_t02) {
		t.Errorf("single-widget Form: want none, got %v", oneWidget)
	}

	// A Form carrying a PrintField Role attribute is conformant regardless of
	// child count.
	withRole := build("PDF/UA-1",
		"<< /Type /StructElem /S /Form /A 6 0 R /K [7 0 R 8 0 R] >>",
		"<< /O /PrintField /Role /rb >>",
		"<< /Type /OBJR /Obj 9 0 R >>",
		"<< /Type /OBJR /Obj 10 0 R >>",
		"<< /Type /Annot /Subtype /Widget >>",
		"<< /Type /Annot /Subtype /Widget >>",
	)
	if hasRule(withRole, Rule_14289_1_7_18_4_t02) {
		t.Errorf("Form with PrintField Role: want none, got %v", withRole)
	}

	// The check is scoped to PDF/UA-1; under PDF/UA-2 it does not run.
	ua2 := build("PDF/UA-2",
		"<< /Type /StructElem /S /Form /K [6 0 R 7 0 R] >>",
		"<< /Type /OBJR /Obj 8 0 R >>",
		"<< /Type /OBJR /Obj 9 0 R >>",
		"<< /Type /Annot /Subtype /Widget >>",
		"<< /Type /Annot /Subtype /Widget >>",
	)
	if len(ua2) != 0 {
		t.Errorf("PDF/UA-2: want no violations from UA-1 check, got %v", ua2)
	}
}
