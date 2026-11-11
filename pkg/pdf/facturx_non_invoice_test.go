package pdf

import (
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestValidate_HybridInvoiceProfilesFailAPDFWithNoInvoice pins that the
// Factur-X and ZUGFeRD 2.1 validations fail a PDF that embeds no invoice,
// with a fatal violation naming the catalog's missing /AF array: a hybrid
// invoice carries its XML as an associated file of the document (ISO 19005-3,
// 6.8).
func TestValidate_HybridInvoiceProfilesFailAPDFWithNoInvoice(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	r := openColorPDF(t, minimalPDF())
	for _, tc := range []struct {
		name     string
		validate func() (FacturXReport, error)
	}{
		{"Factur-X", func() (FacturXReport, error) { return r.ValidateFacturXReport(ctx) }},
		{"ZUGFeRD 2.1", func() (FacturXReport, error) { return r.ValidateZUGFeRDReport(ctx, models.ZUGFeRD21) }},
	} {
		report, err := tc.validate()
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		fatals := report.Fatals()
		if !slices.ContainsFunc(fatals, func(v Violation) bool { return v.Clause == "INVOICE" && v.TestNumber == 1 }) {
			t.Errorf("%s: the fatal violations %v do not name the missing /AF array", tc.name, fatals)
		}
	}
}
