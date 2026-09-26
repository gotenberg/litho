package remediation

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/models"
)

// A remediation that stays INCREMENTAL cannot re-emit the header: it is byte
// zero of the bytes being preserved. So its outputs declared whatever the
// source did, 1.4 included, while a rewrite by the same remediation declared
// the version its profile builds on, and PDF/UA-2 outputs carried PDF 2.0
// structure namespaces (/Namespaces, /NS) under a 1.4 header.
//
// The catalog /Version (ISO 32000-1, 7.7.2, Table 28) is the one carrier left,
// and litho's reader honors it. What follows pins that it raises the version
// an incremental output declares, and that it is written nowhere the header
// already says it.

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

// TestDeclareCatalogVersion_RaisesAnIncrementalUA1Output pins that a PDF/UA-1
// output declares 1.7 whichever way it is written. ISO 14289-1 builds on ISO
// 32000-1, and a rewrite writes %PDF-1.7; the incremental output of a 1.4
// source declared 1.4, although ISO 14289-1, 6.1 accepts any %PDF-1.n with n up
// to 7 and so no clause stood in the way of the raise.
func TestDeclareCatalogVersion_RaisesAnIncrementalUA1Output(t *testing.T) {
	catalog, _ := remediateAt(t, "1.4", remediateUA(models.PDFUA1))

	if catalog == nil {
		t.Fatal("no catalog was written, so the version could not have been declared")
	}
	if got := catalog["Version"]; got != ast.PdfName("1.7") {
		t.Errorf("catalog /Version = %v, want /1.7: a rewrite of the same document "+
			"declares 1.7 in its header", got)
	}
}

// TestDeclareCatalogVersion_LeavesAMatchingHeaderAlone is the control that
// separates the raise from a blanket write: a PDF/UA-1 source already
// declaring 1.7 in its header says everything the profile asks.
func TestDeclareCatalogVersion_LeavesAMatchingHeaderAlone(t *testing.T) {
	catalog, _ := remediateAt(t, "1.7", remediateUA(models.PDFUA1))

	if catalog == nil {
		t.Fatal("remediation left the catalog untouched, so this control asserts nothing " +
			"and would pass whether or not a version was declared")
	}
	if got, ok := catalog["Version"]; ok {
		t.Errorf("added catalog /Version %v to a document whose header already declares 1.7", got)
	}
}

// TestDeclareCatalogVersion_LeavesPDFA4Alone pins the deliberate exclusion.
// ISO 19005-4 §6.1.2 governs the header, and §6.1.12 constrains the catalog
// entry only when present (see compliance.checkVersionPart4), so an ABSENT
// entry conforms. Remediation re-emits the header at 2.0 instead, so a catalog
// entry would add a key litho leaves absent by design.
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

// catalogVersionPDF builds a one-page document whose header declares header
// and whose catalog carries catalogExtra, with a cross-reference table
// computed from the objects it writes.
func catalogVersionPDF(header, catalogExtra string) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R " + catalogExtra + " >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-" + header + "\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// TestDeclareOutputVersion pins the catalog /Version declareOutputVersion
// leaves, over the header it finds, the entry the catalog already carries and
// the write path. want is the version it writes, "-" an entry it removes, and
// "" a catalog it leaves as it is.
func TestDeclareOutputVersion(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, header, catalog, required string
		rewrite                         bool
		want                            string
	}{
		{"earlier header raised", "1.4", "", "1.7", false, "1.7"},
		{"earlier header, earlier entry raised", "1.3", "/Version /1.4", "1.7", false, "1.7"},
		// Table 28 reads a later entry, so it declares more than the output.
		{"earlier header, later entry lowered", "1.3", "/Version /1.5", "1.4", false, "1.4"},
		{"matching header", "1.7", "", "1.7", false, ""},
		{"matching header, matching entry", "1.7", "/Version /1.7", "1.7", false, ""},
		{"matching header, contradicting entry", "1.7", "/Version /2.0", "1.7", false, "-"},
		// The entry cannot lower a later header, so the output declares the
		// header's version and an entry naming another goes.
		{"later header", "1.7", "", "1.4", false, ""},
		{"later header, later entry", "1.7", "/Version /2.0", "1.4", false, "-"},
		// A rewrite writes the required version in the header.
		{"rewrite, no entry", "1.7", "", "2.0", true, ""},
		{"rewrite, contradicting entry", "2.0", "/Version /1.7", "2.0", true, "-"},
		{"rewrite, entry no version number", "1.7", "/Version /Nonsense", "2.0", true, "-"},
		{"no required version", "1.4", "/Version /1.7", "", false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			data := catalogVersionPDF(tc.header, tc.catalog)
			doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
			if err != nil {
				t.Fatalf("NewDocument: %v", err)
			}
			if doc.HeaderVersion != tc.header {
				t.Fatalf("HeaderVersion = %q, want %q", doc.HeaderVersion, tc.header)
			}
			alloc := writer.NewObjAllocator(3)

			fix := declareOutputVersion(ctx, alloc, doc, tc.required, tc.rewrite)
			catalog, written := alloc.Pending()[1].(ast.PdfDict)
			if written != (tc.want != "") || (fix != "") != written {
				t.Fatalf("catalog written = %v with fix %q, want written %v", written, fix, tc.want != "")
			}
			switch version, present := catalog["Version"]; {
			case !written:
			case tc.want == "-" && present:
				t.Errorf("catalog /Version = %v, want it removed", version)
			case tc.want != "-" && version != ast.PdfName(tc.want):
				t.Errorf("catalog /Version = %v, want /%s", version, tc.want)
			}
		})
	}
}
