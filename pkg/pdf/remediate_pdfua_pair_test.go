package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/remediation"
	"github.com/gotenberg/litho/pkg/models"
)

// pairTextPDF is an untagged one-page document drawing "AB" in Helvetica,
// unembedded, whose program testdata/fonts supplies.
func pairTextPDF() []byte {
	const content = "BT /F1 24 Tf 72 700 Td (AB) Tj ET\n"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R" +
			" /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		"<< /Title (Pair) /Author (litho) >>",
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n%\xE2\xE3\xCF\xD3\n")
	offs := make([]int, len(objs)+1)
	for i, o := range objs {
		offs[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f\r\n", len(objs)+1)
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n\r\n", offs[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R /Info 6 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return buf.Bytes()
}

// pairFonts supplies the program of pairTextPDF's font.
var pairFonts = WithFontFiles(os.DirFS("testdata/fonts"))

// pdfuaSpecification reports whether spec, a [Violation.Specification],
// names a standard PDF/UA judges a file against: ISO 14289, and ISO 32005,
// whose structure rules PDF/UA-2 applies.
func pdfuaSpecification(spec string) bool {
	return strings.HasPrefix(spec, "ISO_14289") || spec == "ISO_32005"
}

// TestRemediatePDFAWithPDFUAConformsToBoth pins that a remediation to a PDF/A
// and a PDF/UA profile together writes a file that conforms to both, for
// every pair ISO 19005 and ISO 14289 allow, and that claims both: pdfaid:part
// for PDF/A and pdfuaid:part for PDF/UA (ISO 14289-1 and ISO 14289-2, 5), the
// latter declared in an extension schema under PDF/A-1 to PDF/A-3 (ISO
// 19005-1, 6.7.9; ISO 19005-2 and ISO 19005-3, 6.6.2.3.1). A level-b PDF/A
// pairs as well as a level-a one.
func TestRemediatePDFAWithPDFUAConformsToBoth(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		pdfa  models.PDFAProfile
		pdfua models.PDFUAProfile
	}{
		{models.PDFA1a, models.PDFUA1},
		{models.PDFA1b, models.PDFUA1},
		{models.PDFA2a, models.PDFUA1},
		{models.PDFA2b, models.PDFUA1},
		{models.PDFA2u, models.PDFUA1},
		{models.PDFA3a, models.PDFUA1},
		{models.PDFA3b, models.PDFUA1},
		{models.PDFA3u, models.PDFUA1},
		{models.PDFA4, models.PDFUA2},
		{models.PDFA4e, models.PDFUA2},
	} {
		t.Run(string(tc.pdfa)+" and "+string(tc.pdfua), func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			data := pairTextPDF()
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()
			w := NewWriter(r)
			defer w.Close()
			report, err := w.RemediatePDFA(ctx, tc.pdfa, WithPDFUA(tc.pdfua), pairFonts)
			if err != nil {
				t.Fatalf("RemediatePDFA: %v", err)
			}
			for _, v := range report.ResidualViolations() {
				t.Errorf("residual %s %s", v.Specification, v.Error())
			}
			var out bytes.Buffer
			if err := w.WriteTo(ctx, &out); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}

			got, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
			if err != nil {
				t.Fatalf("reopening the output: %v", err)
			}
			defer got.Close()
			pdfa, err := got.ValidatePDFAReport(ctx, tc.pdfa)
			if err != nil {
				t.Fatalf("ValidatePDFAReport: %v", err)
			}
			pdfua, err := got.ValidatePDFUAReport(ctx, tc.pdfua)
			if err != nil {
				t.Fatalf("ValidatePDFUAReport: %v", err)
			}
			for _, v := range slices.Concat(pdfa, pdfua) {
				t.Errorf("the output fails %s %s", v.Specification, v.Error())
			}
			if part, err := got.DeclaredPDFAPart(ctx); err != nil || part != int(string(tc.pdfa)[len("PDF/A-")]-'0') {
				t.Errorf("the output claims PDF/A part %d (%v), want %s", part, err, tc.pdfa)
			}
			if want := fmt.Sprintf("<pdfuaid:part>%c</pdfuaid:part>", tc.pdfua[len(tc.pdfua)-1]); !bytes.Contains(out.Bytes(), []byte(want)) {
				t.Errorf("the output does not claim %s with %s", tc.pdfua, want)
			}
		})
	}
}

// TestRemediatePDFAWithPDFUAListsTheResidualOfBoth pins that the residual of a
// paired remediation is that of both profiles, each violation naming its
// standard: a font no program is supplied for stays unembedded, which ISO
// 19005-2, 6.2.11.4.1 and ISO 14289-1, 7.21.4.1 both forbid.
func TestRemediatePDFAWithPDFUAListsTheResidualOfBoth(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := pairTextPDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()
	report, err := w.RemediatePDFA(ctx, models.PDFA2b, WithPDFUA(models.PDFUA1))
	if err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}
	specs := map[string]bool{}
	for _, v := range report.ResidualViolations() {
		specs[v.Specification] = true
	}
	if !specs["ISO_19005_2"] || !specs["ISO_14289_1"] || len(specs) != 2 {
		t.Errorf("the residual names %v, want ISO_19005_2 and ISO_14289_1", slices.Sorted(maps.Keys(specs)))
	}
	if len(report.Residual) != len(report.ResidualViolations()) {
		t.Errorf("Residual holds %d errors and ResidualViolations %d", len(report.Residual), len(report.ResidualViolations()))
	}
}

// TestRemediateWithPDFUARefusesIncompatiblePairs pins that a PDF/A profile
// paired with a PDF/UA profile that requires another PDF version is refused
// with [ErrIncompatibleProfiles], a client error, before the document is
// read: PDF/UA-1 requires a version from 1.0 to 1.7 (ISO 14289-1, 6.1), PDF/A-4
// a 2.x one (ISO 19005-4, 6.1.2) and PDF/UA-2 2.0 (ISO 14289-2, 6.1). The
// Writer stays open to edits and writes no temp file.
func TestRemediateWithPDFUARefusesIncompatiblePairs(t *testing.T) {
	for _, tc := range []struct {
		name      string
		data      func() []byte
		remediate func(ctx context.Context, w *Writer) (*RemediationReport, error)
		want      string
	}{
		{
			name: "PDF/A-1b and PDF/UA-2",
			data: signablePDF,
			remediate: func(ctx context.Context, w *Writer) (*RemediationReport, error) {
				return w.RemediatePDFA(ctx, models.PDFA1b, WithPDFUA(models.PDFUA2))
			},
			want: "PDF/A-1b requires PDF/UA-1, not PDF/UA-2",
		},
		{
			name: "PDF/A-2b and PDF/UA-2",
			data: signablePDF,
			remediate: func(ctx context.Context, w *Writer) (*RemediationReport, error) {
				return w.RemediatePDFA(ctx, models.PDFA2b, WithPDFUA(models.PDFUA2))
			},
			want: "PDF/A-2b requires PDF/UA-1, not PDF/UA-2",
		},
		{
			name: "PDF/A-3u and PDF/UA-2",
			data: signablePDF,
			remediate: func(ctx context.Context, w *Writer) (*RemediationReport, error) {
				return w.RemediatePDFA(ctx, models.PDFA3u, WithPDFUA(models.PDFUA2))
			},
			want: "PDF/A-3u requires PDF/UA-1, not PDF/UA-2",
		},
		{
			name: "PDF/A-4 and PDF/UA-1",
			data: signablePDF,
			remediate: func(ctx context.Context, w *Writer) (*RemediationReport, error) {
				return w.RemediatePDFA(ctx, models.PDFA4, WithPDFUA(models.PDFUA1))
			},
			want: "PDF/A-4 requires PDF/UA-2, not PDF/UA-1",
		},
		{
			name: "PDF/A-4f and PDF/UA-1",
			data: signablePDF,
			remediate: func(ctx context.Context, w *Writer) (*RemediationReport, error) {
				return w.RemediatePDFA(ctx, models.PDFA4f, WithPDFUA(models.PDFUA1))
			},
			want: "PDF/A-4f requires PDF/UA-2, not PDF/UA-1",
		},
		{
			name: "Factur-X and PDF/UA-2",
			data: signablePDF,
			remediate: func(ctx context.Context, w *Writer) (*RemediationReport, error) {
				return w.RemediateFacturX(ctx, FacturXConfig{InvoiceXML: []byte(facturXSampleCII), Level: models.FacturXMinimum}, WithPDFUA(models.PDFUA2))
			},
			want: "PDF/A-3b requires PDF/UA-1, not PDF/UA-2",
		},
		{
			name: "a Factur-X repair and PDF/UA-2",
			data: defectiveInvoicePDF,
			remediate: func(ctx context.Context, w *Writer) (*RemediationReport, error) {
				return w.RepairFacturX(ctx, WithPDFUA(models.PDFUA2))
			},
			want: "PDF/A-3b requires PDF/UA-1, not PDF/UA-2",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := isolateTempDir(t)
			ctx := context.Background()
			data := tc.data()
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()
			w := NewWriter(r)
			defer w.Close()

			_, err = tc.remediate(ctx, w)
			if !errors.Is(err, ErrIncompatibleProfiles) {
				t.Fatalf("the remediation returned %v, want ErrIncompatibleProfiles", err)
			}
			if !IsInvalidInput(err) {
				t.Errorf("IsInvalidInput(%v) = false, want true", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("the refusal %q does not say %q", err, tc.want)
			}
			if got := lithoTempFiles(t, dir); len(got) != 0 {
				t.Errorf("the refusal left %q behind", got)
			}
			if err := w.RotatePages(90, 1); err != nil {
				t.Fatalf("RotatePages after the refusal: %v", err)
			}
			var out bytes.Buffer
			if err := w.WriteTo(ctx, &out); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
		})
	}
}

// TestRemediateFacturXWithPDFUAConformsToBoth pins that a Factur-X
// remediation paired with PDF/UA-1 embeds the invoice and writes a file that
// conforms to PDF/A-3 and to PDF/UA-1, the PDF/UA claim declared beside the
// Factur-X one in the extension schema block (ISO 19005-3, 6.6.2.3.1).
func TestRemediateFacturXWithPDFUAConformsToBoth(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := pairTextPDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()
	report, err := w.RemediateFacturX(ctx, FacturXConfig{InvoiceXML: compliantInvoiceXML(t), Level: models.FacturXEN16931}, WithPDFUA(models.PDFUA1), pairFonts)
	if err != nil {
		t.Fatalf("RemediateFacturX: %v", err)
	}
	for _, v := range report.ResidualViolations() {
		t.Errorf("residual %s %s", v.Specification, v.Error())
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	got, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	defer got.Close()
	pdfa, err := got.ValidatePDFAReport(ctx, models.PDFA3b)
	if err != nil {
		t.Fatalf("ValidatePDFAReport: %v", err)
	}
	pdfua, err := got.ValidatePDFUAReport(ctx, models.PDFUA1)
	if err != nil {
		t.Fatalf("ValidatePDFUAReport: %v", err)
	}
	for _, v := range slices.Concat(pdfa, pdfua) {
		t.Errorf("the output fails %s %s", v.Specification, v.Error())
	}
	files, err := got.EmbeddedFiles(ctx)
	if err != nil {
		t.Fatalf("EmbeddedFiles: %v", err)
	}
	if !slices.ContainsFunc(files, func(f models.EmbeddedFile) bool { return f.Name == "factur-x.xml" }) {
		t.Errorf("the output embeds %v, want factur-x.xml", files)
	}
}

// TestRepairFacturXWithPDFUA pins that a Factur-X repair paired with PDF/UA-1
// repairs the invoice container and leaves no PDF/UA-1 violation, and that a
// document without an invoice is refused with [ErrNoInvoiceAttachment] before
// the PDF/UA engine runs, as the repair alone refuses it: the Writer stays
// open to edits and writes no temp file.
func TestRepairFacturXWithPDFUA(t *testing.T) {
	t.Run("an invoice", func(t *testing.T) {
		ctx := context.Background()
		data := defectiveInvoicePDF()
		r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer r.Close()
		w := NewWriter(r)
		defer w.Close()
		report, err := w.RepairFacturX(ctx, WithPDFUA(models.PDFUA1))
		if err != nil {
			t.Fatalf("RepairFacturX: %v", err)
		}
		if !slices.Contains(report.Fixed, "added invoice filespec to catalog /AF") {
			t.Errorf("the repair did not add the invoice to the catalog /AF: %q", report.Fixed)
		}
		for _, v := range report.ResidualViolations() {
			if pdfuaSpecification(v.Specification) {
				t.Errorf("residual %s %s", v.Specification, v.Error())
			}
		}
	})

	t.Run("no invoice", func(t *testing.T) {
		dir := isolateTempDir(t)
		ctx := context.Background()
		data := minimalPDF()
		r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer r.Close()
		w := NewWriter(r)
		defer w.Close()
		if _, err := w.RepairFacturX(ctx, WithPDFUA(models.PDFUA1)); !errors.Is(err, ErrNoInvoiceAttachment) {
			t.Fatalf("err = %v, want ErrNoInvoiceAttachment", err)
		}
		if got := lithoTempFiles(t, dir); len(got) != 0 {
			t.Errorf("the refusal left %q behind", got)
		}
		if err := w.RotatePages(90, 1); err != nil {
			t.Fatalf("RotatePages after the refusal: %v", err)
		}
	})
}

// keyEngine is a remediation engine that sets, each round it runs, the first
// of keys the catalog lacks, and validates to one violation per key the
// catalog lacks. It records its name in log each round it runs.
type keyEngine struct {
	name string
	keys []string
	log  *[]string
}

// run is the [remediationRun] of the engine.
func (e keyEngine) run(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ []string) (*remediation.Report, error) {
	*e.log = append(*e.log, e.name)
	root, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
	if !ok {
		return nil, errors.New("no catalog reference")
	}
	catalog, err := document.ResolveDict(ctx, doc, root)
	if err != nil {
		return nil, err
	}
	for _, k := range e.keys {
		if _, ok := catalog[ast.PdfName(k)]; ok {
			continue
		}
		catalog = maps.Clone(catalog)
		catalog[ast.PdfName(k)] = ast.PdfName("Set")
		alloc.AllocAt(root.ObjectNum, catalog)
		return &remediation.Report{Fixed: []string{"set /" + k}}, nil
	}
	return &remediation.Report{}, nil
}

// validate returns one violation per key the catalog of r lacks.
func (e keyEngine) validate(ctx context.Context, r *Reader) ([]error, error) {
	root, _ := r.doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
	catalog, err := document.ResolveDict(ctx, r.doc, root)
	if err != nil {
		return nil, err
	}
	var out []error
	for _, k := range e.keys {
		if _, ok := catalog[ast.PdfName(k)]; !ok {
			out = append(out, fmt.Errorf("%s: %w", k, errMissingCatalogKey))
		}
	}
	return out, nil
}

// TestPairedRemediationTakesTurns pins the order of the rounds of a paired
// remediation: the PDF/UA engine first, then the other one while the other
// profile leaves violations, and again the one whose profile still leaves
// some, for up to four rounds, so each engine gets a pass on what the other
// repaired. Every round is judged against both profiles.
func TestPairedRemediationTakesTurns(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		ua, a    []string
		want     []string
		residual int
	}{
		{name: "each repairs twice", ua: []string{"LithoUA1", "LithoUA2"}, a: []string{"LithoA1", "LithoA2"}, want: []string{"UA", "A", "UA", "A"}},
		{name: "the PDF/UA profile conforms after one", ua: []string{"LithoUA1"}, a: []string{"LithoA1", "LithoA2"}, want: []string{"UA", "A", "A"}},
		{name: "the PDF/UA profile conforms from the start", a: []string{"LithoA1"}, want: []string{"UA", "A"}},
		{name: "four rounds at most", ua: []string{"LithoUA1", "LithoUA2", "LithoUA3"}, a: []string{"LithoA1", "LithoA2"}, want: []string{"UA", "A", "UA", "A"}, residual: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			data := threePagePDF()
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()

			var log []string
			ua := keyEngine{name: "UA", keys: tc.ua, log: &log}
			a := keyEngine{name: "A", keys: tc.a, log: &log}
			pair := &pairedRemediation{pdfua: models.PDFUA1, validateA: a.validate, validateUA: ua.validate, runA: a.run, runUA: ua.run}
			w := NewWriter(r)
			defer w.Close()
			report, err := w.remediate(ctx, pair.target(engineTarget), DefaultRegressionTolerance, pair.revalidate, pair.run)
			if err != nil {
				t.Fatalf("remediate: %v", err)
			}
			if !slices.Equal(log, tc.want) {
				t.Errorf("the rounds ran %q, want %q", log, tc.want)
			}
			if len(report.Residual) != tc.residual {
				t.Errorf("Residual = %v, want %d", report.Residual, tc.residual)
			}
		})
	}
}

// TestRemediatePDFAWithPDFUARewritesWhatThePDFUARoundLeft pins that the first
// PDF/A round of a paired remediation keeps the full rewrite it asks for, its
// whole repair of a stream /Length the source got wrong (ISO 19005-2,
// 6.1.7.1). It runs on the PDF/UA round's incremental output, which carries
// the source's bytes, and a later round's rewrite counted as no repair, so the
// round was dropped and the output kept the defect.
func TestRemediatePDFAWithPDFUARewritesWhatThePDFUARoundLeft(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data, err := os.ReadFile("../../test/integration/corpus/litho/pdfs/extguards_2b.pdf")
	if err != nil {
		t.Fatalf("reading the fixture: %v", err)
	}
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	before, err := r.ValidatePDFAReport(ctx, models.PDFA2b)
	if err != nil {
		t.Fatalf("ValidatePDFAReport: %v", err)
	}
	if !slices.ContainsFunc(before, func(v Violation) bool { return v.Clause == "6.1.7.1" }) {
		t.Fatal("precondition: the fixture does not fail ISO 19005-2, 6.1.7.1")
	}
	w := NewWriter(r)
	defer w.Close()
	report, err := w.RemediatePDFA(ctx, models.PDFA2b, WithPDFUA(models.PDFUA1))
	if err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}
	for _, v := range report.ResidualViolations() {
		if v.Clause == "6.1.7.1" {
			t.Errorf("residual %s", v.Error())
		}
	}
}

// TestRepaired pins when a later remediation round repairs something: a fix
// other than the full-rewrite note, or the note itself on an output not
// rewritten yet.
func TestRepaired(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		fixes                   []string
		needsRewrite, rewritten bool
		want                    bool
	}{
		{nil, false, false, false},
		{[]string{remediation.FullRewriteNote}, true, true, false},
		{[]string{remediation.FullRewriteNote}, true, false, true},
		{[]string{"set /Lang"}, false, true, true},
		{[]string{"set /Lang", remediation.FullRewriteNote}, true, true, true},
	} {
		if got := repaired(tc.fixes, tc.needsRewrite, tc.rewritten); got != tc.want {
			t.Errorf("repaired(%q, %v, %v) = %v, want %v", tc.fixes, tc.needsRewrite, tc.rewritten, got, tc.want)
		}
	}
}

// TestRemediatePDFA1WithPDFUAKeepsFlattening pins that a paired remediation
// at PDF/A-1b keeps the transparency flattening of its first PDF/A round, a
// later round, which the integrity probe measures as it measures a first one:
// text shown at constant alpha 0, which flattening draws in rendering mode 3
// and the default extraction leaves out, counts as kept. The round was
// measured against the source's visible text, refused, and the output kept
// its transparency, which ISO 19005-1, 6.4 forbids.
func TestRemediatePDFA1WithPDFUAKeepsFlattening(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	in := colorFixturePDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R " +
			"/Resources << /Font << /F1 5 0 R >> /ExtGState << /Zero 6 0 R >> >> >>",
		colorStream("", "q /Zero gs BT /F1 12 Tf 10 100 Td (Searchable text nobody sees) Tj ET Q "+
			"BT /F1 12 Tf 10 50 Td (Shown) Tj ET"),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
		"<< /Type /ExtGState /ca 0 >>",
	})
	w := NewWriter(openColorPDF(t, in))
	t.Cleanup(func() { _ = w.Close() })
	report, err := w.RemediatePDFA(ctx, models.PDFA1b, WithPDFUA(models.PDFUA1))
	if err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}
	if !slicesContainPrefix(report.Fixed, "flattened transparency on 1 page(s)") || len(report.RolledBack) != 0 {
		t.Errorf("flattening not kept: Fixed = %q, RolledBack = %q", report.Fixed, report.RolledBack)
	}
	for _, v := range report.ResidualViolations() {
		if v.Clause == "6.4" {
			t.Errorf("transparency left: %v", v)
		}
	}
	var buf bytes.Buffer
	if err := w.WriteTo(ctx, &buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if text, err := openColorPDF(t, buf.Bytes()).ExtractText(ctx, 1, WithInvisibleText()); err != nil || !strings.Contains(text, "Searchable text nobody sees") {
		t.Errorf("text lost: %q, %v", text, err)
	}
}

// TestRemediatePDFA1WithPDFUAFlatteningFallsBackOnALoss pins that a later
// round whose output the integrity probe refuses with its transparency
// flattened, for a loss flattening does not account for, runs again with the
// flattening disabled, as a first round does ([Writer.remediateFlattening]),
// and lists the flattening as rolled back. The PDF/A engine here drops page
// 1's content after a run that flattened it, in the second round of a pair
// whose PDF/UA profile the document already meets.
func TestRemediatePDFA1WithPDFUAFlatteningFallsBackOnALoss(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := NewWriter(openColorPDF(t, transparentPDF()))
	t.Cleanup(func() { _ = w.Close() })
	pair := &pairedRemediation{
		pdfua:      models.PDFUA1,
		validateA:  func(ctx context.Context, r *Reader) ([]error, error) { return r.ValidatePDFA(ctx, models.PDFA1b) },
		validateUA: func(context.Context, *Reader) ([]error, error) { return nil, nil },
		runUA: func(context.Context, *writer.ObjAllocator, *document.Document, []string) (*remediation.Report, error) {
			return &remediation.Report{}, nil
		},
		runA: func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, disabled []string) (*remediation.Report, error) {
			rep, err := remediation.RemediatePDFA(ctx, doc, alloc, models.PDFA1b, remediation.Options{PreserveContent: true}.WithDisabled(disabled))
			if err != nil || len(rep.Flattened) == 0 {
				return rep, err
			}
			catalog, err := document.ResolveCatalog(ctx, doc)
			if err != nil {
				t.Fatalf("ResolveCatalog: %v", err)
			}
			pages, err := document.ResolvePages(ctx, doc, catalog)
			if err != nil || len(pages) == 0 {
				t.Fatalf("ResolvePages: %v", err)
			}
			page := maps.Clone(pages[0].Dict)
			if queued, ok := alloc.Pending()[pages[0].Ref.ObjectNum].(ast.PdfDict); ok {
				page = maps.Clone(queued)
			}
			page["Contents"] = alloc.Alloc(ast.NewStream(ast.PdfDict{"Length": ast.PdfInt(0)}, nil))
			alloc.AllocAt(pages[0].Ref.ObjectNum, page)
			// The loss sits outside the content tier, as the flattening
			// does, so no rollback reaches it.
			rep.ContentSnapshot = nil
			return rep, nil
		},
	}
	report, err := w.remediateFlattening(ctx, pair.target(remediationTarget{method: "RemediatePDFA"}), DefaultRegressionTolerance, pair.revalidate, pair.run)
	if err != nil {
		t.Fatalf("remediateFlattening: %v", err)
	}
	if !slicesContainPrefix(report.RolledBack, "flattened transparency on 1 page(s)") || slicesContainPrefix(report.Fixed, "flattened transparency") {
		t.Errorf("flattening not abandoned: Fixed = %q, RolledBack = %q", report.Fixed, report.RolledBack)
	}
	for _, v := range report.ResidualViolations() {
		if v.Clause == "6.4" {
			t.Errorf("transparency left: %v", v)
		}
	}
	var buf bytes.Buffer
	if err := w.WriteTo(ctx, &buf); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if text, err := openColorPDF(t, buf.Bytes()).ExtractText(ctx, 1); err != nil || !strings.Contains(text, "Above everything") {
		t.Errorf("text lost: %q, %v", text, err)
	}
}
