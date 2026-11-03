package pdf

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// compliantInvoiceXML returns a CII invoice declaring EN 16931 that conforms
// at that level, with one Schematron warning (PEPPOL-EN16931-R008, an empty
// element), which does not fail conformance.
func compliantInvoiceXML(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/facturx/en16931.xml")
	if err != nil {
		t.Fatalf("reading the invoice fixture: %v", err)
	}
	return data
}

// remediateFacturXBytes runs [Writer.RemediateFacturX] on data, or
// [Writer.RepairFacturX] when cfg is nil, and returns the report and the
// output.
func remediateFacturXBytes(t *testing.T, data []byte, cfg *FacturXConfig) (*RemediationReport, []byte) {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()
	var report *RemediationReport
	if cfg == nil {
		report, err = w.RepairFacturX(ctx)
	} else {
		report, err = w.RemediateFacturX(ctx, *cfg)
	}
	if err != nil {
		t.Fatalf("remediation: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return report, out.Bytes()
}

// facturXFatals returns the failures [Reader.ValidateFacturXReport] reports
// on data, the violations that make a validation non-compliant.
func facturXFatals(t *testing.T, data []byte) []Violation {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	defer r.Close()
	report, err := r.ValidateFacturXReport(ctx)
	if err != nil {
		t.Fatalf("ValidateFacturXReport: %v", err)
	}
	return report.Fatals()
}

// TestRemediateFacturXRefusesAnInvoiceItWouldNotConform pins that
// RemediateFacturX refuses, as invalid input and before touching the Writer,
// an invoice XML the validator would then reject at the level asked for: one
// declaring another level (Factur-X 1.0.07, 4.3), and a payload that is no
// Cross-Industry Invoice. It used to embed them and report an empty residual.
func TestRemediateFacturXRefusesAnInvoiceItWouldNotConform(t *testing.T) {
	t.Parallel()

	en16931 := compliantInvoiceXML(t)
	for _, tc := range []struct {
		name  string
		xml   []byte
		level models.FacturXLevel
		want  error
		says  []string
	}{
		{"EN 16931 XML at MINIMUM", en16931, models.FacturXMinimum, ErrFacturXLevelMismatch, []string{`"MINIMUM"`, `"EN 16931"`, "urn:cen.eu:en16931:2017"}},
		{"EN 16931 XML at BASIC WL", en16931, models.FacturXBasicWL, ErrFacturXLevelMismatch, []string{`"BASIC WL"`, `"EN 16931"`}},
		{"EN 16931 XML at BASIC", en16931, models.FacturXBasic, ErrFacturXLevelMismatch, []string{`"BASIC"`, `"EN 16931"`}},
		{"EN 16931 XML at EXTENDED", en16931, models.FacturXExtended, ErrFacturXLevelMismatch, []string{`"EXTENDED"`, `"EN 16931"`}},
		{"EN 16931 XML at XRECHNUNG", en16931, models.FacturXXRechnung, ErrFacturXLevelMismatch, []string{`"XRECHNUNG"`, `"EN 16931"`}},
		{"MINIMUM XML at EN 16931", []byte(facturXSampleCII), models.FacturXEN16931, ErrFacturXLevelMismatch, []string{`"EN 16931"`, `"MINIMUM"`}},
		{"no guideline ID", bytes.Replace(en16931, []byte("urn:cen.eu:en16931:2017"), nil, 1), models.FacturXEN16931, ErrFacturXLevelMismatch, []string{"declares no Factur-X level"}},
		{"a CSV", []byte("id,amount\n1,10.00\n"), models.FacturXBasic, ErrInvoiceNotCII, []string{"root element is not CrossIndustryInvoice"}},
		{"truncated XML", en16931[:len(en16931)/2], models.FacturXEN16931, ErrInvoiceNotCII, []string{"not well-formed"}},
		{"another XML root", []byte("<invoice/>"), models.FacturXBasic, ErrInvoiceNotCII, []string{"root element is not CrossIndustryInvoice"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			data := writerTestPDF()
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()
			w := NewWriter(r)
			defer w.Close()

			_, err = w.RemediateFacturX(ctx, FacturXConfig{InvoiceXML: tc.xml, Level: tc.level})
			if !errors.Is(err, tc.want) {
				t.Fatalf("RemediateFacturX returned %v, want %v", err, tc.want)
			}
			if !IsInvalidInput(err) {
				t.Errorf("IsInvalidInput(%v) = false, want true", err)
			}
			for _, s := range tc.says {
				if !strings.Contains(err.Error(), s) {
					t.Errorf("the refusal %q does not say %s", err, s)
				}
			}
			// The refusal leaves the Writer open to edits.
			if err := w.RotatePages(90, 1); err != nil {
				t.Fatalf("RotatePages after the refusal: %v", err)
			}
		})
	}
}

// TestFacturXResidualMatchesValidation pins that the residual of a Factur-X
// remediation, on the embed path and on the repair path alike, is exactly
// the failures a validation of its output reports: a warning counts on
// neither side, and an invoice the validator rejects is not reported as
// conforming. A compliant invoice reports an empty residual when embedded,
// when embedded again into the output, and when that output is repaired.
func TestFacturXResidualMatchesValidation(t *testing.T) {
	t.Parallel()

	compliant := compliantInvoiceXML(t)
	// An element the CII:100 XSD does not define, at a level the XML
	// declares: the remediation embeds it, and the validator rejects it.
	broken := bytes.Replace(compliant, []byte("<ram:CountryID>FR</ram:CountryID>"), []byte("<ram:CountryID>FR</ram:CountryID><ram:Bogus/>"), 1)

	for _, tc := range []struct {
		name         string
		xml          []byte
		wantResidual bool
	}{
		{"compliant invoice", compliant, false},
		{"invoice failing the XSD", broken, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg := &FacturXConfig{InvoiceXML: tc.xml, Level: models.FacturXEN16931}
			embedded, out := remediateFacturXBytes(t, writerTestPDF(), cfg)
			again, outAgain := remediateFacturXBytes(t, out, cfg)
			repaired, outRepaired := remediateFacturXBytes(t, out, nil)

			for _, run := range []struct {
				name   string
				report *RemediationReport
				output []byte
			}{
				{"embedded", embedded, out},
				{"embedded again", again, outAgain},
				{"repaired", repaired, outRepaired},
			} {
				fatals := facturXFatals(t, run.output)
				if len(run.report.Residual) != len(fatals) {
					t.Errorf("%s: residual %d, but validating the output finds %d failures: residual %v, failures %v",
						run.name, len(run.report.Residual), len(fatals), run.report.Residual, fatals)
				}
				if got := len(run.report.Residual) > 0; got != tc.wantResidual {
					t.Errorf("%s: residual %v, want a residual: %v", run.name, run.report.Residual, tc.wantResidual)
				}
				for _, v := range run.report.ResidualViolations() {
					if v.Severity != SeverityFatal {
						t.Errorf("%s: residual holds a %s: %v", run.name, v.Severity, v)
					}
				}
				files := embeddedFileNames(t, run.output)
				if len(files) != 1 || files[0] != "factur-x.xml" {
					t.Errorf("%s: the output embeds %q, want the invoice once", run.name, files)
				}
			}
		})
	}
}

// embeddedFileNames returns the names of the files data embeds.
func embeddedFileNames(t *testing.T, data []byte) []string {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	defer r.Close()
	files, err := r.EmbeddedFiles(ctx)
	if err != nil {
		t.Fatalf("EmbeddedFiles: %v", err)
	}
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = f.Name
	}
	return names
}
