package compliance

import (
	"context"
	"testing"
)

// TestAnnotParentThroughParentTree pins that PDF/UA-1 finds an annotation's
// enclosing structure element through its /StructParent entry, not through the
// OBJR that references it.
//
// ISO 32000-1, 14.7.4.4 gives an annotation in the structure tree a
// /StructParent key naming its entry in the structure parent tree, and veraPDF's
// GFPDAnnot.getParentDictionary reads that entry and nothing else: an
// annotation without /StructParent has no parent there, so ISO 14289-1 7.18.5
// t1 fails for a Link and 7.18.8 t1 passes for a PrinterMark however the tree
// references them. The parent's standard type goes through the role map before
// the standard-name test, so a /Link remapped to /Span no longer encloses a
// Link annotation.
func TestAnnotParentThroughParentTree(t *testing.T) {
	t.Parallel()

	build := func(t *testing.T, subtype, enclType, structParent, roleMap string) []Violation {
		t.Helper()
		doc, cat := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [5 0 R] >>",
			"<< /Type /StructTreeRoot /K 6 0 R /ParentTree 8 0 R "+roleMap+" >>",
			"<< /Type /Annot /Subtype /"+subtype+" /Rect [10 10 50 30] /Contents (x) "+structParent+" >>",
			"<< /Type /StructElem /S /Document /P 4 0 R /K 7 0 R >>",
			"<< /Type /StructElem /S /"+enclType+" /P 6 0 R /K 9 0 R >>",
			"<< /Nums [0 7 0 R] >>",
			"<< /Type /OBJR /Obj 5 0 R >>",
		)
		return checkAnnotationNesting(context.Background(), doc, cat, "PDF/UA-1")
	}

	tests := []struct {
		name         string
		subtype      string
		enclType     string
		structParent string
		roleMap      string
		rule         Rule
		fires        bool
	}{
		{"a Link reached through its StructParent", "Link", "Link", "/StructParent 0", "", Rule_14289_1_7_18_5_t01, false},
		{"a Link referenced only by an OBJR", "Link", "Link", "", "", Rule_14289_1_7_18_5_t01, true},
		{"a Link whose Link element the role map remaps", "Link", "Link", "/StructParent 0", "/RoleMap << /Link /Span >>", Rule_14289_1_7_18_5_t01, true},
		{"a Link element reached through a custom type", "Link", "MyLink", "/StructParent 0", "/RoleMap << /MyLink /Link >>", Rule_14289_1_7_18_5_t01, false},
		{"a PrinterMark with a StructParent", "PrinterMark", "Figure", "/StructParent 0", "", Rule_14289_1_7_18_8_t01, true},
		{"a PrinterMark referenced only by an OBJR", "PrinterMark", "Figure", "", "", Rule_14289_1_7_18_8_t01, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := build(t, tt.subtype, tt.enclType, tt.structParent, tt.roleMap)
			if fired := hasRule(got, tt.rule); fired != tt.fires {
				t.Errorf("fired = %v, want %v (%v)", fired, tt.fires, got)
			}
		})
	}
}
