package compliance

import (
	"context"
	"testing"
)

func ua2AnnotViolations(t *testing.T, annotObj string, extra ...string) []Violation {
	t.Helper()
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
		annotObj,
	}
	objs = append(objs, extra...)
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
			"<< /Type /Catalog /Pages 2 0 R >>",
			"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Annots [4 0 R] >>",
			annotObj,
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
