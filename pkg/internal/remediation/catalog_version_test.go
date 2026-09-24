package remediation

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/models"
)

// PDF/UA-2 and WTPDF remediation writes PDF 2.0 structure namespaces
// (/Namespaces, /NS) but usually stays INCREMENTAL, and an incremental update
// cannot re-emit the header: it is byte zero of the bytes being preserved. So
// those outputs declared whatever the source did, 1.4 included, while carrying
// PDF 2.0 constructs.
//
// The catalog /Version (ISO 32000-1, 7.5.2) is the one carrier left, and
// litho's reader honors it. What follows pins that it is written for the
// profiles whose requirement it can legitimately carry, and for no others.

// remediateAt runs one remediation over a fresh document declaring the given
// header version, and returns the catalog the run left in the allocator.
func remediateAt(t *testing.T, version string, run func(context.Context, *document.Document, *writer.ObjAllocator) (*Report, error)) (ast.PdfDict, *Report) {
	t.Helper()
	ctx := context.Background()
	data := versionTablePDF(version)

	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	if doc.Version != version {
		t.Fatalf("fixture declares %q, want %q; the raise would not be observable",
			doc.Version, version)
	}

	alloc := writer.NewObjAllocator(3) // the xref carries objects 1..3
	report, err := run(ctx, doc, alloc)
	if err != nil {
		t.Fatalf("remediate: %v", err)
	}
	if len(report.Fixed) == 0 && len(report.Residual) == 0 {
		t.Fatal("the fixture produced no violations, so remediate returned early: " +
			"the catalog step was never reached, for a reason unrelated to what is under test")
	}

	catalog, ok := alloc.Pending()[1].(ast.PdfDict)
	if !ok {
		// Nothing rewrote the catalog at all, which is a legitimate outcome
		// for the control cases below.
		return nil, report
	}
	return catalog, report
}

func remediateUA(profile models.PDFUAProfile) func(context.Context, *document.Document, *writer.ObjAllocator) (*Report, error) {
	return func(ctx context.Context, d *document.Document, a *writer.ObjAllocator) (*Report, error) {
		return RemediatePDFUA(ctx, d, a, profile, Options{})
	}
}

// TestDeclareCatalogVersion_RaisesAnIncrementalUA2Output is the regression.
func TestDeclareCatalogVersion_RaisesAnIncrementalUA2Output(t *testing.T) {
	catalog, report := remediateAt(t, "1.4", remediateUA(models.PDFUA2))

	if catalog == nil {
		t.Fatal("no catalog was written, so the version could not have been declared")
	}
	if got := catalog["Version"]; got != ast.PdfName("2.0") {
		t.Errorf("catalog /Version = %v, want /2.0: the output declares 1.4 while "+
			"carrying PDF 2.0 structure namespaces", got)
	}

	// The write is reported, not silent. A mutation nothing records is how the
	// original defect survived four green tools.
	var announced bool
	for _, f := range report.Fixed {
		if bytes.Contains([]byte(f), []byte("catalog")) && bytes.Contains([]byte(f), []byte("2.0")) {
			announced = true
		}
	}
	if !announced {
		t.Errorf("the catalog version raise is absent from Report.Fixed: %v", report.Fixed)
	}
}

// TestDeclareCatalogVersion_LeavesPDFUA1Alone is the control that separates
// this from a blanket raise. ISO 14289-1 §6.1 wants %PDF-1.n with n in 0..7,
// so 1.4 already conforms and the profile's 1.7 is a CEILING for a 2.0 source,
// not a floor for a 1.4 one.
func TestDeclareCatalogVersion_LeavesPDFUA1Alone(t *testing.T) {
	catalog, _ := remediateAt(t, "1.4", remediateUA(models.PDFUA1))

	if catalog == nil {
		t.Fatal("remediation left the catalog untouched, so this control asserts nothing " +
			"and would pass whether or not a version was declared")
	}
	if got, ok := catalog["Version"]; ok {
		t.Errorf("catalog /Version = %v on a PDF/UA-1 output; 1.4 already conforms "+
			"and 1.7 is a ceiling, not a floor", got)
	}
}

// TestDeclareCatalogVersion_LeavesPDFA4Alone pins the deliberate exclusion.
// ISO 19005-4 §6.1.2 governs the header, and §6.1.12 is checked with
// fallbackToHeader false, so an ABSENT catalog entry conforms. Remediation
// re-emits the header at 2.0 instead, so a catalog entry would add a key litho
// leaves absent by design.
//
// # Corpus evidence
//
//   - verapdf PDF_A-4__veraPDF_test_suite_6-1-2-t01-fail-a.pdf: remediation
//     re-emits the header at 2.0 and writes no catalog entry.
func TestDeclareCatalogVersion_LeavesPDFA4Alone(t *testing.T) {
	catalog, _ := remediateAt(t, "1.4", func(ctx context.Context, d *document.Document, a *writer.ObjAllocator) (*Report, error) {
		return RemediatePDFA(ctx, d, a, models.PDFA4, Options{})
	})

	if catalog == nil {
		t.Fatal("remediation left the catalog untouched, so this control asserts nothing " +
			"and would pass whether or not a version was declared")
	}
	if got, ok := catalog["Version"]; ok {
		t.Errorf("catalog /Version = %v on a PDF/A-4 output; an absent entry conforms "+
			"and the header carries the requirement", got)
	}
}

// TestDeclareCatalogVersion_AddsNothingRedundant: a document already declaring
// 2.0 says everything PDF/UA-2 requires, so the key would be noise.
func TestDeclareCatalogVersion_AddsNothingRedundant(t *testing.T) {
	catalog, _ := remediateAt(t, "2.0", remediateUA(models.PDFUA2))

	if catalog == nil {
		t.Fatal("remediation left the catalog untouched, so this control asserts nothing " +
			"and would pass whether or not a version was declared")
	}
	if got, ok := catalog["Version"]; ok {
		t.Errorf("added catalog /Version %v to a document whose header already declares 2.0", got)
	}
}

func TestCatalogCarriesVersion(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		profile string
		want    bool
	}{
		// ISO 14289-2's rule accepts the catalog entry or the header, and
		// these profiles frequently stay incremental.
		{"PDF/UA-2", true},
		{"WTPDF-1.0-Reuse", true},
		{"WTPDF-1.0-Accessibility", true},
		// The header carries the requirement for these, and for PDF/A-4 an
		// absent catalog entry positively conforms.
		{"PDF/A-4", false},
		{"PDF/A-1b", false},
		{"PDF/A-2b", false},
		{"PDF/UA-1", false},
		{"", false},
	} {
		t.Run(tc.profile, func(t *testing.T) {
			t.Parallel()
			if got := catalogCarriesVersion(tc.profile); got != tc.want {
				t.Errorf("catalogCarriesVersion(%q) = %v, want %v", tc.profile, got, tc.want)
			}
		})
	}
}
