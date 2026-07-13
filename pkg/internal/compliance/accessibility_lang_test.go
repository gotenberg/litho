package compliance

import (
	"context"
	"testing"
)

// TestCheckLangRequired_PresentEmptyLangIsDeterminable covers PDF/UA-1 §7.2: a
// present but empty /Lang on a structure element is an invalid Language-Tag (t29),
// yet it still makes the language determinable, so t34 (undeterminable language)
// must not also fire. veraPDF fires only t29 here.
func TestCheckLangRequired_PresentEmptyLangIsDeterminable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	doc, cat := tableDoc(t, 1,
		"<< /Type /Catalog /StructTreeRoot 2 0 R >>",
		"<< /Type /StructTreeRoot /K [3 0 R] >>",
		"<< /Type /StructElem /S /Document /P 2 0 R /K [4 0 R] >>",
		"<< /Type /StructElem /S /P /P 3 0 R /Lang () >>",
	)
	got := checkLangRequired(ctx, doc, cat, "PDF/UA-1")

	if !hasRule(got, Rule_14289_1_7_2_t29) {
		t.Error("empty /Lang on a structure element: want §7.2 t29 (invalid tag)")
	}
	if hasRule(got, Rule_14289_1_7_2_t34) {
		t.Error("present but empty /Lang: want no §7.2 t34 (language is determinable)")
	}
}

// TestCheckLangRequired_NoLangAnywhereFiresT34 is the control: with no /Lang in
// the catalog or anywhere in the structure tree, the language is undeterminable
// and §7.2 t34 fires.
func TestCheckLangRequired_NoLangAnywhereFiresT34(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	doc, cat := tableDoc(t, 1,
		"<< /Type /Catalog /StructTreeRoot 2 0 R >>",
		"<< /Type /StructTreeRoot /K [3 0 R] >>",
		"<< /Type /StructElem /S /Document /P 2 0 R /K [4 0 R] >>",
		"<< /Type /StructElem /S /P /P 3 0 R >>",
	)
	got := checkLangRequired(ctx, doc, cat, "PDF/UA-1")

	if !hasRule(got, Rule_14289_1_7_2_t34) {
		t.Error("no /Lang anywhere: want §7.2 t34 (undeterminable language)")
	}
}
