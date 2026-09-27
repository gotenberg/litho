package pdf

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// hiddenAnnotsPDF builds a one-page form: a hidden field "secret" merged with
// its widget, a displayed field "name", a hidden note with its pop-up, and a
// Square that shows in print alone (NoView with Print).
func hiddenAnnotsPDF() []byte {
	ap := streamObj("/Type /XObject /Subtype /Form /BBox [0 0 100 20] /Resources << >>", []byte("0 0 100 20 re S"))
	return limitsPDF(
		"<< /Type /Catalog /Pages 2 0 R /AcroForm << /Fields [4 0 R 5 0 R] /DA (/Helv 0 Tf 0 g) >> >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 9 0 R /Resources << >> /Annots [4 0 R 5 0 R 6 0 R 7 0 R 8 0 R] >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (secret) /V (s3cr3t) /Rect [10 10 110 30] /F 6 /AP << /N 10 0 R >> >>",
		"<< /Type /Annot /Subtype /Widget /FT /Tx /T (name) /V (Ada) /Rect [10 40 110 60] /F 4 /AP << /N 10 0 R >> >>",
		"<< /Type /Annot /Subtype /Text /Rect [200 200 220 220] /F 6 /Contents (note) /Popup 7 0 R /AP << /N 10 0 R >> >>",
		"<< /Type /Annot /Subtype /Popup /Rect [220 200 320 260] /Parent 6 0 R >>",
		"<< /Type /Annot /Subtype /Square /Rect [300 300 350 350] /F 36 /AP << /N 10 0 R >> >>",
		streamObj("", []byte("BT /F1 12 Tf ET")),
		ap,
	)
}

// remediateHiddenAnnots opens data, runs remediate on a Writer over it, and
// returns the report and the reopened output.
func remediateHiddenAnnots(t *testing.T, data []byte, remediate func(context.Context, *Writer) (*RemediationReport, error)) (*RemediationReport, *Reader) {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	w := NewWriter(r)
	t.Cleanup(func() { w.Close() })
	report, err := remediate(ctx, w)
	if err != nil {
		t.Fatalf("remediating: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	t.Cleanup(func() { r2.Close() })
	return report, r2
}

// annotSubtypes lists the subtypes of the output's page 1 annotations.
func annotSubtypes(t *testing.T, r *Reader) []string {
	t.Helper()
	annots, err := r.ExtractAnnotations(context.Background(), 1)
	if err != nil {
		t.Fatalf("ExtractAnnotations: %v", err)
	}
	var subtypes []string
	for _, a := range annots {
		subtypes = append(subtypes, strings.TrimPrefix(a.Subtype, "/"))
	}
	return subtypes
}

// hiddenFieldNames lists the output's form field names.
func hiddenFieldNames(t *testing.T, r *Reader) []string {
	t.Helper()
	fields, err := r.FormFields(context.Background())
	if err != nil {
		t.Fatalf("FormFields: %v", err)
	}
	var names []string
	for _, f := range fields {
		names = append(names, f.Name)
	}
	slices.Sort(names)
	return names
}

// TestRemediatePDFARemovesAnnotationsThatNeverShow runs the whole PDF/A-2b
// remediation, every fixer after annot-flags included, and checks the written
// file: the hidden field and the hidden note with its pop-up are gone, the
// displayed field stays, and the Square that shows in print alone keeps its
// place with flags that show it on screen too. No annotation flag violation is
// left (ISO 19005-2, 6.3.2).
func TestRemediatePDFARemovesAnnotationsThatNeverShow(t *testing.T) {
	t.Parallel()

	report, out := remediateHiddenAnnots(t, hiddenAnnotsPDF(), func(ctx context.Context, w *Writer) (*RemediationReport, error) {
		return w.RemediatePDFA(ctx, models.PDFA2b)
	})

	for _, want := range []string{
		"removed Hidden Widget annotation from page 1",
		"removed Hidden Text annotation from page 1",
		"removed Popup annotation of a removed annotation from page 1",
		`removed form field "secret" with its widget`,
	} {
		if !slices.Contains(report.Fixed, want) {
			t.Errorf("report.Fixed = %q, want %q", report.Fixed, want)
		}
	}

	if got, want := annotSubtypes(t, out), []string{"Widget", "Square"}; !slices.Equal(got, want) {
		t.Errorf("page 1 annotations = %v, want %v", got, want)
	}
	if got, want := hiddenFieldNames(t, out), []string{"name"}; !slices.Equal(got, want) {
		t.Errorf("form fields = %v, want %v", got, want)
	}

	ctx := context.Background()
	violations, err := out.ValidatePDFAReport(ctx, models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFAReport: %v", err)
	}
	for _, v := range violations {
		if v.Clause == "6.3.2" {
			t.Errorf("the output keeps %s %s t%d: %s", v.Specification, v.Clause, v.TestNumber, v.Context)
		}
	}

	catalog, err := document.ResolveCatalog(ctx, out.doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, out.doc, catalog)
	if err != nil || len(pages) != 1 {
		t.Fatalf("ResolvePages: %v (%d pages)", err, len(pages))
	}
	annots, err := document.ResolveArray(ctx, out.doc, pages[0].Dict["Annots"])
	if err != nil {
		t.Fatalf("resolving /Annots: %v", err)
	}
	for _, entry := range annots {
		annot, err := document.ResolveDict(ctx, out.doc, entry)
		if err != nil {
			t.Fatalf("resolving an annotation: %v", err)
		}
		if subtype, _ := annot["Subtype"].(ast.PdfName); subtype == "Square" && annot["F"] != ast.PdfInt(4) {
			t.Errorf("the Square that prints carries /F %v, want 4: shown on screen and in print", annot["F"])
		}
	}
}

// TestRemediatePDFUAKeepsHiddenAnnotations pins the PDF/UA path. ISO 14289-1
// does not forbid the Hidden and NoView flags, so a PDF/UA-1 remediation
// leaves every annotation and field where the author put it.
func TestRemediatePDFUAKeepsHiddenAnnotations(t *testing.T) {
	t.Parallel()

	report, out := remediateHiddenAnnots(t, hiddenAnnotsPDF(), func(ctx context.Context, w *Writer) (*RemediationReport, error) {
		return w.RemediatePDFUA(ctx, models.PDFUA1)
	})

	for _, fix := range report.Fixed {
		if strings.HasPrefix(fix, "removed Hidden") || strings.HasPrefix(fix, "removed NoView") {
			t.Errorf("a PDF/UA-1 remediation reported %q", fix)
		}
	}
	if got, want := annotSubtypes(t, out), []string{"Widget", "Widget", "Text", "Popup", "Square"}; !slices.Equal(got, want) {
		t.Errorf("page 1 annotations = %v, want %v", got, want)
	}
	if got, want := hiddenFieldNames(t, out), []string{"name", "secret"}; !slices.Equal(got, want) {
		t.Errorf("form fields = %v, want %v", got, want)
	}
}
