package compliance

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// validateUA runs the full PDF/UA pipeline on a fixture and returns the typed
// violations.
func validateUA(t *testing.T, profile models.PDFUAProfile, objs ...string) []Violation {
	t.Helper()
	doc, _ := objsDocPDF(t, objs...)
	errs, err := ValidatePDFUA(context.Background(), doc, profile)
	if err != nil {
		t.Fatalf("ValidatePDFUA(%s): %v", profile, err)
	}
	violations := make([]Violation, 0, len(errs))
	for _, e := range errs {
		v, ok := e.(Violation)
		if !ok {
			t.Fatalf("ValidatePDFUA(%s) returned a non-Violation error: %v", profile, e)
		}
		violations = append(violations, v)
	}
	return violations
}

// assertNoUA1Rules sweeps every returned violation and fails on any rule from
// the ISO 14289-1 specification. PDF/UA-2 conformance is governed by
// ISO 14289-2, a distinct standard, so a PDF/UA-2 run must never emit an
// ISO 14289-1 rule.
func assertNoUA1Rules(t *testing.T, violations []Violation) {
	t.Helper()
	for _, v := range violations {
		if v.Rule.Specification == "ISO_14289_1" {
			t.Errorf("PDF/UA-2 run leaked UA-1 rule §%s t%d: %s [%s]",
				v.Rule.Clause, v.Rule.TestNumber, v.Rule.Description, v.Context)
		}
	}
}

// tableDefectObjs is a tagged document with a UA-1-only structure defect: a
// TD directly under a TBody (no TR). PDF/UA-1 §7.2 rejects this containment.
// PDF/UA-2 (ISO 14289-2) does not carry the fixed UA-1 tag-containment rules,
// so the defect emits no ISO 14289-1 rule under PDF/UA-2.
func tableDefectObjs() []string {
	return []string{
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /MarkInfo << /Marked true >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Document /P 4 0 R /K [6 0 R] >>",
		"<< /Type /StructElem /S /Table /P 5 0 R /K [7 0 R] >>",
		"<< /Type /StructElem /S /TBody /P 6 0 R /K [8 0 R] >>",
		"<< /Type /StructElem /S /TD /P 7 0 R >>",
	}
}

// headingSkipObjs is a tagged document with a UA-1-only heading defect: H1
// followed by H3 skips a level. PDF/UA-1 §7.4.2 rejects the skip. PDF/UA-2
// (ISO 14289-2) does not impose the strict numbered-heading sequence, so the
// skip emits no ISO 14289-1 rule under PDF/UA-2.
func headingSkipObjs() []string {
	return []string{
		"<< /Type /Catalog /Pages 2 0 R /StructTreeRoot 4 0 R /MarkInfo << /Marked true >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
		"<< /Type /StructTreeRoot /K 5 0 R >>",
		"<< /Type /StructElem /S /Document /P 4 0 R /K [6 0 R 7 0 R] >>",
		"<< /Type /StructElem /S /H1 /P 5 0 R >>",
		"<< /Type /StructElem /S /H3 /P 5 0 R >>",
	}
}

// TestUA1RulesNotEmittedUnderUA2_TableContainment pins the profile gate: a
// TD-under-TBody defect must trip ISO 14289-1 §7.2 under PDF/UA-1 and emit
// nothing from the ISO 14289-1 specification under PDF/UA-2.
func TestUA1RulesNotEmittedUnderUA2_TableContainment(t *testing.T) {
	ua1 := validateUA(t, models.PDFUA1, tableDefectObjs()...)
	if !hasRule(ua1, Rule_14289_1_7_2_t09) {
		t.Errorf("PDF/UA-1: TD under TBody must trip §7.2 t9, got %v", ua1)
	}

	ua2 := validateUA(t, models.PDFUA2, tableDefectObjs()...)
	assertNoUA1Rules(t, ua2)
}

// TestUA1RulesNotEmittedUnderUA2_HeadingSkip pins the profile gate for the
// heading-hierarchy rule: an H1-to-H3 skip must trip ISO 14289-1 §7.4.2 under
// PDF/UA-1 and emit nothing from the ISO 14289-1 specification under PDF/UA-2.
func TestUA1RulesNotEmittedUnderUA2_HeadingSkip(t *testing.T) {
	ua1 := validateUA(t, models.PDFUA1, headingSkipObjs()...)
	if !hasRule(ua1, Rule_14289_1_7_4_2_t01) {
		t.Errorf("PDF/UA-1: H1 to H3 skip must trip §7.4.2 t1, got %v", ua1)
	}

	ua2 := validateUA(t, models.PDFUA2, headingSkipObjs()...)
	assertNoUA1Rules(t, ua2)
}
