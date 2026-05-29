package pdf

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/models"
)

func TestToViolation_SeverityAndFields(t *testing.T) {
	warn := toViolation(compliance.Violation{Rule: compliance.Rule_cii_sch_Warning, Context: "invoice"})
	if warn.Severity != SeverityWarning {
		t.Errorf("warning rule -> severity %v, want SeverityWarning", warn.Severity)
	}
	if warn.Clause != "CII_SCH" {
		t.Errorf("clause = %q, want CII_SCH", warn.Clause)
	}

	fatal := toViolation(compliance.Violation{
		Rule: compliance.Rule{Specification: "ISO_19005_1", Clause: "6.1.2", TestNumber: 3, Description: "x"},
	})
	if fatal.Severity != SeverityFatal {
		t.Errorf("PDF/A rule -> severity %v, want SeverityFatal", fatal.Severity)
	}

	// An error that is not a compliance violation maps to a fatal
	// Violation carrying its message.
	plain := toViolation(errors.New("boom"))
	if plain.Severity != SeverityFatal || plain.Description != "boom" {
		t.Errorf("plain error -> %+v, want {fatal, boom}", plain)
	}

	// Error() text must match the internal violation exactly so structured
	// and flat forms compare equal.
	cv := compliance.Violation{
		Rule:    compliance.Rule{Clause: "6.1.2", TestNumber: 3, Description: "must embed font"},
		Context: "page 1",
	}
	if got, want := toViolation(cv).Error(), cv.Error(); got != want {
		t.Errorf("Error() mismatch:\n got  %q\n want %q", got, want)
	}
}

func TestValidatePDFAReport_StructuredMatchesFlat(t *testing.T) {
	data := writerTestPDF()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	flat, err := r.ValidatePDFA(ctx, models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFA: %v", err)
	}
	report, err := r.ValidatePDFAReport(ctx, models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFAReport: %v", err)
	}

	if len(report) != len(flat) {
		t.Fatalf("report has %d violations, flat has %d", len(report), len(flat))
	}
	if len(report) == 0 {
		t.Skip("minimal PDF unexpectedly passed PDF/A-2b; nothing to assert")
	}
	for i := range report {
		// PDF/A conformance is binary: every violation is fatal.
		if report[i].Severity != SeverityFatal {
			t.Errorf("violation %d severity = %v, want SeverityFatal", i, report[i].Severity)
		}
		// Same order, same text as the flat form.
		if report[i].Error() != flat[i].Error() {
			t.Errorf("violation %d text mismatch:\n structured %q\n flat       %q", i, report[i].Error(), flat[i].Error())
		}
	}
}
