package pdf

import (
	"context"
	"errors"
	"fmt"

	"github.com/gotenberg/litho/pkg/internal/compliance"
	"github.com/gotenberg/litho/pkg/models"
)

// checkInvoiceLevel refuses an invoice XML [Writer.RemediateFacturX] would
// embed at a level the validator then rejects it at: a payload that is no
// Cross-Industry Invoice, or one whose specification identifier (BT-24, which
// EN 16931-1 makes mandatory) declares another level than level. Factur-X
// 1.0.07, 4.3 fixes the identifier of each level, and the profile Schematron
// of the level rejects any other one (FX-SCH-A-000026).
//
// An XRechnung invoice passes at BASIC, EN 16931 and EXTENDED: XRechnung is a
// CIUS of EN 16931, and the validator applies its rules to such an invoice as
// advisories, so it conforms at those levels. At MINIMUM and BASIC WL, whose
// invoices carry less than EN 16931 requires, it is refused.
func checkInvoiceLevel(invoiceXML []byte, level models.FacturXLevel) error {
	declared, guideline, err := compliance.InvoiceLevel(invoiceXML)
	if err != nil {
		return err
	}
	if declared == level {
		return nil
	}
	if declared == models.FacturXXRechnung && (level == models.FacturXBasic || level == models.FacturXEN16931 || level == models.FacturXExtended) {
		return nil
	}
	if declared == "" {
		return fmt.Errorf("%w: level %q given, but the invoice XML declares no Factur-X level (guideline ID %q in ExchangedDocumentContext/GuidelineSpecifiedDocumentContextParameter/ID)",
			ErrFacturXLevelMismatch, level, guideline)
	}
	return fmt.Errorf("%w: level %q given, but the invoice XML declares %q (guideline ID %q)",
		ErrFacturXLevelMismatch, level, declared, guideline)
}

// facturXFailures returns the violations of a [Reader.ValidateFacturX] result
// that fail conformance: the hard violations of [FacturXReport.Fatals]. A
// warning, a soft Schematron finding, does not by itself make an invoice
// non-conformant, and an advisory (a Schematron rule litho could not
// evaluate) is no verdict at all, so a remediation's residual counts neither,
// as a validation's compliance verdict does not.
func facturXFailures(errs []error) []error {
	var out []error
	for _, e := range partitionFacturXErrors(errs).Violations {
		if toViolation(e).Severity == SeverityFatal {
			out = append(out, e)
		}
	}
	return out
}

// facturXResidual returns the residual of a Factur-X invoice r reads, embedded
// at the PDF/A-3 profile: the failures [Reader.ValidateFacturX] reports.
// ValidateFacturX judges the archival base at PDF/A-3b, which every PDF/A-3
// level includes. A PDF/A-3a or PDF/A-3u output answers to its own level too
// (ISO 19005-3, 6.1), so its residual is that level's violations plus the
// invoice's own, in place of the PDF/A-3b ones.
func facturXResidual(ctx context.Context, r *Reader, profile models.PDFAProfile) ([]error, error) {
	errs, err := r.ValidateFacturX(ctx)
	if err != nil {
		return nil, err
	}
	failures := facturXFailures(errs)
	if profile == "" || profile == models.PDFA3b {
		return failures, nil
	}
	out, err := r.ValidatePDFA(ctx, profile)
	if err != nil {
		return nil, err
	}
	for _, e := range failures {
		cv, ok := errors.AsType[compliance.Violation](e)
		if !ok || cv.Rule.Specification != compliance.Rule_invoice_PDFA3Base.Specification || cv.Rule == compliance.Rule_invoice_PDFA3Base {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}
