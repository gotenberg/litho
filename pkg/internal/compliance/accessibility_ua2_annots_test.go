package compliance

import (
	"context"
	"testing"
)

// ua2AnnotViolations runs the PDF/UA-2 annotation constraints on a one-page
// document holding annotObj. Its structure parent tree maps key 0 to a P
// element, so an annotation that carries /StructParent 0 sits in the structure
// tree the way veraPDF reads it.
func ua2AnnotViolations(t *testing.T, annotObj string, extra ...string) []Violation {
	t.Helper()
	root := 5 + len(extra)
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot " + itoa(root) + " 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		annotObj,
	}
	objs = append(objs, extra...)
	objs = append(objs,
		"<< /Type /StructTreeRoot /K "+itoa(root+1)+" 0 R /ParentTree << /Nums [0 "+itoa(root+1)+" 0 R] >> >>",
		"<< /Type /StructElem /S /P /P "+itoa(root)+" 0 R /K << /Type /OBJR /Obj 4 0 R >> >>",
	)
	doc, cat := objsDocPDF(t, objs...)
	return checkPDFUA2AnnotationConstraints(context.Background(), doc, cat, "PDF/UA-2")
}

func TestOCConfigAS_UA2(t *testing.T) {
	ctx := context.Background()
	doc, cat := objsDocPDF(t,
		"<< /Type /Catalog /Pages 2 0 R /OCProperties << /D << /Name (D) /AS [] >> >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	)
	if got := checkOCConfigNames(ctx, doc, cat, "PDF/UA-2"); !hasRule(got, Rule_14289_2_8_7_t02) {
		t.Errorf("UA-2 /AS in OC config: got %v, want includes §8.7 t2", got)
	}
	if got := checkOCConfigNames(ctx, doc, cat, "PDF/UA-1"); !hasRule(got, Rule_14289_1_7_10_t02) {
		t.Errorf("UA-1 /AS in OC config: got %v, want includes §7.10 t2", got)
	}
}

func TestUA2ForbiddenAnnots(t *testing.T) {
	// TrapNet is forbidden outright.
	if got := ua2AnnotViolations(t, "<< /Type /Annot /Subtype /TrapNet >>"); !hasRule(got, Rule_14289_2_8_9_2_4_15_t01) {
		t.Errorf("TrapNet: got %v, want includes §8.9.2.4.15 t1", got)
	}
}

func TestUA2ArtifactRequiredAnnots(t *testing.T) {
	// Popup and PrinterMark must be artifacts: in the structure tree they fail.
	if got := ua2AnnotViolations(t, "<< /Type /Annot /Subtype /Popup /StructParent 0 >>"); !hasRule(got, Rule_14289_2_8_9_2_4_9_t01) {
		t.Errorf("Popup in structure: got %v, want includes §8.9.2.4.9 t1", got)
	}
	if got := ua2AnnotViolations(t, "<< /Type /Annot /Subtype /Popup >>"); hasRule(got, Rule_14289_2_8_9_2_4_9_t01) {
		t.Errorf("Popup as artifact: got %v, want none", got)
	}
	if got := ua2AnnotViolations(t, "<< /Type /Annot /Subtype /PrinterMark /StructParent 0 >>"); !hasRule(got, Rule_14289_2_8_9_2_4_14_t01) {
		t.Errorf("PrinterMark in structure: got %v, want includes §8.9.2.4.14 t1", got)
	}
	if got := ua2AnnotViolations(t, "<< /Type /Annot /Subtype /PrinterMark >>"); hasRule(got, Rule_14289_2_8_9_2_4_14_t01) {
		t.Errorf("PrinterMark as artifact: got %v, want none", got)
	}
}

func TestUA2AnnotContentsRequired(t *testing.T) {
	cases := []struct {
		subtype string
		rule    Rule
	}{
		{"Ink", Rule_14289_2_8_9_2_4_8_t01},
		{"3D", Rule_14289_2_8_9_2_4_19_t01},
		{"RichMedia", Rule_14289_2_8_9_2_4_19_t02},
	}
	for _, c := range cases {
		if got := ua2AnnotViolations(t, "<< /Type /Annot /Subtype /"+c.subtype+" >>"); !hasRule(got, c.rule) {
			t.Errorf("%s without /Contents: got %v, want includes %v", c.subtype, got, c.rule)
		}
		if got := ua2AnnotViolations(t, "<< /Type /Annot /Subtype /"+c.subtype+" /Contents (desc) >>"); hasRule(got, c.rule) {
			t.Errorf("%s with /Contents: got %v, want none", c.subtype, got)
		}
	}
}

func TestUA2StampNameOrContents(t *testing.T) {
	if got := ua2AnnotViolations(t, "<< /Type /Annot /Subtype /Stamp >>"); !hasRule(got, Rule_14289_2_8_9_2_4_7_t01) {
		t.Errorf("Stamp without /Name or /Contents: got %v, want includes §8.9.2.4.7 t1", got)
	}
	if got := ua2AnnotViolations(t, "<< /Type /Annot /Subtype /Stamp /Name /Approved >>"); hasRule(got, Rule_14289_2_8_9_2_4_7_t01) {
		t.Errorf("Stamp with /Name: got %v, want none", got)
	}
	if got := ua2AnnotViolations(t, "<< /Type /Annot /Subtype /Stamp /Contents (a stamp) >>"); hasRule(got, Rule_14289_2_8_9_2_4_7_t01) {
		t.Errorf("Stamp with /Contents: got %v, want none", got)
	}
}

func TestUA2ZeroSizeWidgetArtifact(t *testing.T) {
	// Zero-size widget in the structure tree must be an artifact.
	if got := ua2AnnotViolations(t, "<< /Type /Annot /Subtype /Widget /Rect [10 10 10 10] /StructParent 0 >>"); !hasRule(got, Rule_14289_2_8_9_2_4_13_t01) {
		t.Errorf("zero-size Widget in structure: got %v, want includes §8.9.2.4.13 t1", got)
	}
	// Zero-size widget as artifact (no /StructParent): conformant.
	if got := ua2AnnotViolations(t, "<< /Type /Annot /Subtype /Widget /Rect [10 10 10 10] >>"); hasRule(got, Rule_14289_2_8_9_2_4_13_t01) {
		t.Errorf("zero-size Widget artifact: got %v, want none", got)
	}
	// Non-zero widget in structure: conformant.
	if got := ua2AnnotViolations(t, "<< /Type /Annot /Subtype /Widget /Rect [10 10 40 30] /StructParent 0 >>"); hasRule(got, Rule_14289_2_8_9_2_4_13_t01) {
		t.Errorf("sized Widget in structure: got %v, want none", got)
	}
}

func TestUA2ScreenContents(t *testing.T) {
	if got := ua2AnnotViolations(t, "<< /Type /Annot /Subtype /Screen >>"); !hasRule(got, Rule_14289_2_8_9_2_4_12_t01) {
		t.Errorf("Screen without /Contents: got %v, want includes §8.9.2.4.12 t1", got)
	}
	if got := ua2AnnotViolations(t, "<< /Type /Annot /Subtype /Screen /Contents (clip) >>"); hasRule(got, Rule_14289_2_8_9_2_4_12_t01) {
		t.Errorf("Screen with /Contents: got %v, want none", got)
	}
}

func TestUA2FileAttachmentAFRelationship(t *testing.T) {
	annot := "<< /Type /Annot /Subtype /FileAttachment /FS 5 0 R >>"
	if got := ua2AnnotViolations(t, annot, "<< /Type /Filespec /F (a.txt) >>"); !hasRule(got, Rule_14289_2_8_9_2_4_10_t01) {
		t.Errorf("FileAttachment filespec without /AFRelationship: got %v, want includes §8.9.2.4.10 t1", got)
	}
	if got := ua2AnnotViolations(t, annot, "<< /Type /Filespec /F (a.txt) /AFRelationship /Data >>"); hasRule(got, Rule_14289_2_8_9_2_4_10_t01) {
		t.Errorf("FileAttachment filespec with /AFRelationship: got %v, want none", got)
	}
}

func TestUA2AnnotArtifactFlags(t *testing.T) {
	flagViolations := func(annotObj string) []Violation {
		doc, cat := objsDocPDF(t,
			"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 5 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
			annotObj,
			"<< /Type /StructTreeRoot /K 6 0 R /ParentTree << /Nums [0 6 0 R] >> >>",
			"<< /Type /StructElem /S /Annot /P 5 0 R /K << /Type /OBJR /Obj 4 0 R >> >>",
		)
		return checkAnnotArtifactFlags(context.Background(), doc, cat, "PDF/UA-2")
	}

	// NoView set, ToggleNoView clear, in structure tree: violates t2.
	if got := flagViolations("<< /Type /Annot /Subtype /Widget /F 32 /StructParent 0 >>"); !hasRule(got, Rule_14289_2_8_9_2_2_t02) {
		t.Errorf("NoView without ToggleNoView in structure: got %v, want includes §8.9.2.2 t2", got)
	}
	// NoView set with ToggleNoView also set (32|256): conformant.
	if got := flagViolations("<< /Type /Annot /Subtype /Widget /F 288 /StructParent 0 >>"); hasRule(got, Rule_14289_2_8_9_2_2_t02) {
		t.Errorf("NoView with ToggleNoView: got %v, want no t2", got)
	}
	// Invisible flag in structure tree still triggers t1 (regression guard).
	if got := flagViolations("<< /Type /Annot /Subtype /Widget /F 1 /StructParent 0 >>"); !hasRule(got, Rule_14289_2_8_9_2_2_t01) {
		t.Errorf("Invisible in structure: got %v, want includes §8.9.2.2 t1", got)
	}
	// NoView annotation that is an artifact (no /StructParent): conformant.
	if got := flagViolations("<< /Type /Annot /Subtype /Widget /F 32 >>"); len(got) != 0 {
		t.Errorf("NoView artifact (no StructParent): got %v, want none", got)
	}
}
