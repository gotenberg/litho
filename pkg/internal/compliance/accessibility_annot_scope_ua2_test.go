package compliance

import (
	"context"
	"testing"
)

// TestUA2AnnotScopeThroughParentTree pins that the PDF/UA-2 annotation rules
// gated on structParentType and isArtifact read both from the element the
// annotation's /StructParent names in the structure parent tree.
//
// A /StructParent entry alone does not put an annotation in the structure tree:
// veraPDF's GFPDAnnot.getParentDictionary resolves it through the parent tree,
// and with no entry there structParentType is null and the rules pass. Every
// rule but the Popup one also passes when the enclosing element or an ancestor
// is an Artifact (getisArtifact). Measured against veraPDF on PDF 2.0 fixtures
// of each shape.
func TestUA2AnnotScopeThroughParentTree(t *testing.T) {
	t.Parallel()

	// build places one annotation, carrying /StructParent 0, under
	// Document > chain...; parentTree decides whether the parent tree maps key 0
	// to the last element of chain.
	build := func(t *testing.T, annot string, chain []string, parentTree bool) []Violation {
		t.Helper()
		const ns = "/NS 4 0 R"
		pt := ""
		if parentTree {
			pt = " /ParentTree << /Nums [0 " + itoa(8+len(chain)-1) + " 0 R] >>"
		}
		objs := []string{
			"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 5 0 R /AcroForm << /Fields [6 0 R] >> >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [6 0 R] >>",
			"<< /Type /Namespace /NS (http://iso.org/pdf2/ssn) >>",
			"<< /Type /StructTreeRoot /Namespaces [4 0 R] /K 7 0 R" + pt + " >>",
			annot,
			"<< /Type /StructElem /S /Document " + ns + " /P 5 0 R /K 8 0 R >>",
		}
		for i, typ := range chain {
			kids := " /K << /Type /OBJR /Obj 6 0 R >>"
			if i < len(chain)-1 {
				kids = " /K " + itoa(9+i) + " 0 R"
			}
			objs = append(objs, "<< /Type /StructElem /S /"+typ+" "+ns+" /P "+itoa(7+i)+" 0 R"+kids+" >>")
		}
		doc, cat := objsDocPDF(t, objs...)
		ctx := context.Background()
		got := checkPDFUA2AnnotationConstraints(ctx, doc, cat, "PDF/UA-2")
		got = append(got, checkAnnotArtifactFlags(ctx, doc, cat, "PDF/UA-2")...)
		got = append(got, checkPDFUA2FormStructure(ctx, doc, cat, "PDF/UA-2")...)
		return append(got, checkWidgetAA(ctx, doc, cat, "PDF/UA-2")...)
	}

	const (
		printerMark = "<< /Type /Annot /Subtype /PrinterMark /Rect [10 10 50 30] /StructParent 0 >>"
		popup       = "<< /Type /Annot /Subtype /Popup /Rect [10 10 50 30] /StructParent 0 >>"
		zeroWidget  = "<< /Type /Annot /Subtype /Widget /FT /Tx /T (f) /TU (field) /Rect [10 10 10 10] /StructParent 0 >>"
		invisible   = "<< /Type /Annot /Subtype /Square /Rect [10 10 50 30] /Contents (x) /F 1 /StructParent 0 >>"
		noView      = "<< /Type /Annot /Subtype /Square /Rect [10 10 50 30] /Contents (x) /F 32 /StructParent 0 >>"
		// A field widget with neither a Lbl kid nor /Contents (8.10.2.3 t1), and
		// one that also carries /AA (8.10.2.3 t2).
		bareField = "<< /Type /Annot /Subtype /Widget /FT /Tx /T (f) /TU (field) /Rect [10 10 50 30] /StructParent 0 >>"
		aaField   = "<< /Type /Annot /Subtype /Widget /FT /Tx /T (f) /TU (field) /Rect [10 10 50 30] /StructParent 0 " +
			"/AA << /K << /S /JavaScript /JS (1;) >> >> >>"
	)

	tests := []struct {
		name       string
		annot      string
		chain      []string
		parentTree bool
		rule       Rule
		fires      bool
	}{
		{"a PrinterMark in a Figure", printerMark, []string{"Figure"}, true, Rule_14289_2_8_9_2_4_14_t01, true},
		{"a PrinterMark under an Artifact", printerMark, []string{"Artifact", "Figure"}, true, Rule_14289_2_8_9_2_4_14_t01, false},
		{"a PrinterMark the parent tree does not map", printerMark, []string{"Figure"}, false, Rule_14289_2_8_9_2_4_14_t01, false},
		{"a Popup in a P", popup, []string{"P"}, true, Rule_14289_2_8_9_2_4_9_t01, true},
		{"a Popup under an Artifact", popup, []string{"Artifact", "P"}, true, Rule_14289_2_8_9_2_4_9_t01, true},
		{"a Popup the parent tree does not map", popup, []string{"P"}, false, Rule_14289_2_8_9_2_4_9_t01, false},
		{"a zero-size widget in a Form", zeroWidget, []string{"Form"}, true, Rule_14289_2_8_9_2_4_13_t01, true},
		{"a zero-size widget under an Artifact", zeroWidget, []string{"Artifact", "Form"}, true, Rule_14289_2_8_9_2_4_13_t01, false},
		{"an invisible annotation in an Annot", invisible, []string{"Annot"}, true, Rule_14289_2_8_9_2_2_t01, true},
		{"an invisible annotation under an Artifact", invisible, []string{"Artifact", "Annot"}, true, Rule_14289_2_8_9_2_2_t01, false},
		{"an invisible annotation the parent tree does not map", invisible, []string{"Annot"}, false, Rule_14289_2_8_9_2_2_t01, false},
		{"a NoView annotation in an Annot", noView, []string{"Annot"}, true, Rule_14289_2_8_9_2_2_t02, true},
		{"a NoView annotation under an Artifact", noView, []string{"Artifact", "Annot"}, true, Rule_14289_2_8_9_2_2_t02, false},
		{"a NoView annotation the parent tree does not map", noView, []string{"Annot"}, false, Rule_14289_2_8_9_2_2_t02, false},
		{"an unlabelled field widget in a Form", bareField, []string{"Form"}, true, Rule_14289_2_8_10_2_3_t01, true},
		{"an unlabelled field widget under an Artifact", bareField, []string{"Artifact", "Form"}, true, Rule_14289_2_8_10_2_3_t01, false},
		{"an unlabelled field widget the parent tree does not map", bareField, []string{"Form"}, false, Rule_14289_2_8_10_2_3_t01, false},
		{"a field widget with actions in a Form", aaField, []string{"Form"}, true, Rule_14289_2_8_10_2_3_t02, true},
		{"a field widget with actions under an Artifact", aaField, []string{"Artifact", "Form"}, true, Rule_14289_2_8_10_2_3_t02, false},
		{"a field widget with actions the parent tree does not map", aaField, []string{"Form"}, false, Rule_14289_2_8_10_2_3_t02, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := build(t, tt.annot, tt.chain, tt.parentTree)
			if fired := hasRule(got, tt.rule); fired != tt.fires {
				t.Errorf("fired = %v, want %v (%v)", fired, tt.fires, got)
			}
		})
	}
}
