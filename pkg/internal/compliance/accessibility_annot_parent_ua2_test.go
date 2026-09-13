package compliance

import (
	"context"
	"testing"
)

// TestUA2AnnotParentThroughParentTree pins that PDF/UA-2 finds an annotation's
// enclosing structure element through its /StructParent entry, and exempts one
// whose enclosing element sits under an Artifact.
//
// ISO 14289-2 gates its annotation rules on veraPDF's structParentType,
// structParentStandardType and isArtifact, all read from the element the
// annotation's /StructParent names in the structure parent tree (ISO 32000-2,
// 14.7.5.4). An annotation that only an OBJR references has no such element and
// is out of scope; one the parent tree places in a P is in scope even with no
// OBJR; and one whose element or any ancestor is an Artifact is exempt.
// Measured against veraPDF on PDF 2.0 fixtures of each shape.
func TestUA2AnnotParentThroughParentTree(t *testing.T) {
	t.Parallel()

	// build places a Link or Square annotation under Document > chain...; the
	// last element of chain encloses it through an OBJR (objr), through the
	// parent tree (structParent), or both.
	build := func(t *testing.T, subtype string, chain []string, structParent, objr bool) []Violation {
		t.Helper()
		const ns = "/NS 4 0 R"
		objs := []string{
			"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 5 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [6 0 R] >>",
			"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
		}
		parentTree := ""
		if structParent {
			parentTree = " /ParentTree << /Nums [0 " + itoa(8+len(chain)-1) + " 0 R] >>"
		}
		objs = append(objs, "<< /Type /StructTreeRoot /Namespaces [4 0 R] /K 7 0 R"+parentTree+" >>")
		annot := "<< /Type /Annot /Subtype /" + subtype + " /Rect [10 10 50 30] /Contents (x) /F 4"
		if structParent {
			annot += " /StructParent 0"
		}
		objs = append(objs, annot+" >>")
		objs = append(objs, "<< /Type /StructElem /S /Document "+ns+" /P 5 0 R /K 8 0 R >>")
		for i, typ := range chain {
			num, parent := 8+i, 7+i
			kids := ""
			switch {
			case i < len(chain)-1:
				kids = " /K " + itoa(num+1) + " 0 R"
			case objr:
				kids = " /K << /Type /OBJR /Obj 6 0 R >>"
			}
			objs = append(objs, "<< /Type /StructElem /S /"+typ+" "+ns+" /P "+itoa(parent)+" 0 R"+kids+" >>")
		}
		doc, cat := objsDocPDF(t, objs...)
		return checkPDFUA2AnnotStructNesting(context.Background(), doc, cat, "PDF/UA-2")
	}

	tests := []struct {
		name         string
		subtype      string
		chain        []string
		structParent bool
		objr         bool
		rule         Rule
		fires        bool
	}{
		{"a Link in a P through both links", "Link", []string{"P"}, true, true, Rule_14289_2_8_2_5_20_t01, true},
		{"a Link in a P that only an OBJR references", "Link", []string{"P"}, false, true, Rule_14289_2_8_2_5_20_t01, false},
		{"a Link the parent tree alone places in a P", "Link", []string{"P"}, true, false, Rule_14289_2_8_2_5_20_t01, true},
		{"a Link in a Span under an Artifact", "Link", []string{"Artifact", "Span"}, true, true, Rule_14289_2_8_2_5_20_t01, false},
		{"a Square in a P through both links", "Square", []string{"P"}, true, true, Rule_14289_2_8_9_2_3_t01, true},
		{"a Square in a P that only an OBJR references", "Square", []string{"P"}, false, true, Rule_14289_2_8_9_2_3_t01, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := build(t, tt.subtype, tt.chain, tt.structParent, tt.objr)
			if fired := hasRule(got, tt.rule); fired != tt.fires {
				t.Errorf("fired = %v, want %v (%v)", fired, tt.fires, got)
			}
		})
	}
}
