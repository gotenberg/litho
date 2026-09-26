package pdf

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// untaggedHeadingPDF is a single untagged page with a heading and a two-line
// body paragraph, so AutoTag has real text structure to synthesize.
func untaggedHeadingPDF() []byte {
	cs := "BT /F1 24 Tf 72 700 Td (Document Heading) Tj ET\n" +
		"BT /F1 12 Tf 72 678 Td (First body line of the paragraph here.) Tj ET\n" +
		"BT /F1 12 Tf 72 664 Td (Second body line continuing it here.) Tj ET\n"
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] " +
			"/Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(cs), cs),
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objs)+1)
	for i, body := range objs {
		offsets[i+1] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	xrefStart := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n", len(objs)+1)
	buf.WriteString("0000000000 65535 f \r\n")
	for i := 1; i <= len(objs); i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \r\n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xrefStart)
	return buf.Bytes()
}

// TestWriter_AutoTag_ResolvesStructureViolations is the end-to-end proof: an
// untagged page fails the PDF/UA structure and content-marking rules; after
// AutoTag and a write, re-validating reports none of them (residuals like
// /Lang, title, and the XMP identifier remain: AutoTag does not own those).
func TestWriter_AutoTag_ResolvesStructureViolations(t *testing.T) {
	ctx := context.Background()
	data := untaggedHeadingPDF()

	r0, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	before, err := r0.ValidatePDFUAReport(ctx, models.PDFUA1)
	r0.Close()
	if err != nil {
		t.Fatalf("ValidatePDFUAReport(before): %v", err)
	}
	if !hasStructureViolation(before) {
		t.Skipf("baseline has no structure/content-marking violation to resolve; got:\n%s", describeAll(before))
	}

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)
	w.AutoTag()
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	r.Close()

	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("re-open tagged output: %v", err)
	}
	defer r2.Close()
	after, err := r2.ValidatePDFUAReport(ctx, models.PDFUA1)
	if err != nil {
		t.Fatalf("ValidatePDFUAReport(after): %v", err)
	}
	if hasStructureViolation(after) {
		t.Errorf("AutoTag did not resolve the structure/content-marking violations:\n%s", describeStructureViolations(after))
	}
}

// TestWriter_RemediatePDFUA_AutoTagsStructure proves the remediation chain
// synthesizes a real structure tree. An untagged page fails the PDF/UA structure
// and content-marking rules; after RemediatePDFUA with default options and a
// write, re-validating reports none of them, the report credits the synthesized
// tree, and the page content carries auto-tagging's per-element marks (/H1, /P)
// rather than the content-tag fixer's crude whole-page /Document wrapper.
func TestWriter_RemediatePDFUA_AutoTagsStructure(t *testing.T) {
	ctx := context.Background()
	data := untaggedHeadingPDF()

	r0, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	before, err := r0.ValidatePDFUAReport(ctx, models.PDFUA1)
	r0.Close()
	if err != nil {
		t.Fatalf("ValidatePDFUAReport(before): %v", err)
	}
	if !hasStructureViolation(before) {
		t.Skipf("baseline has no structure/content-marking violation to resolve; got:\n%s", describeAll(before))
	}

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)
	report, err := w.RemediatePDFUA(ctx, models.PDFUA1)
	if err != nil {
		t.Fatalf("RemediatePDFUA: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	r.Close()

	// The fix must come from auto-tagging, not the flat whole-page wrapper.
	var autoTagged bool
	for _, fix := range report.Fixed {
		if strings.Contains(fix, "synthesized structure tree") {
			autoTagged = true
			break
		}
	}
	if !autoTagged {
		t.Errorf("report does not credit a synthesized structure tree; fixed=%v", report.Fixed)
	}

	// The written content carries per-element marks and not the content-tag
	// fixer's whole-page /Document wrapper, proving it deferred (no double-wrap).
	outStr := out.String()
	if !strings.Contains(outStr, "/H1 <</MCID") || !strings.Contains(outStr, "/P <</MCID") {
		t.Errorf("per-element auto-tag marks missing from remediated content")
	}
	if strings.Contains(outStr, "/Document <</MCID") {
		t.Errorf("content-tag whole-page wrapper present; auto-tag did not supersede it")
	}

	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("re-open remediated output: %v", err)
	}
	defer r2.Close()
	after, err := r2.ValidatePDFUAReport(ctx, models.PDFUA1)
	if err != nil {
		t.Fatalf("ValidatePDFUAReport(after): %v", err)
	}
	if hasStructureViolation(after) {
		t.Errorf("RemediatePDFUA did not resolve the structure/content-marking violations:\n%s", describeStructureViolations(after))
	}
}

func hasStructureViolation(vs []Violation) bool {
	return slices.ContainsFunc(vs, isStructureViolation)
}

func isStructureViolation(v Violation) bool {
	d := strings.ToLower(v.Description + " " + v.Context)
	return strings.Contains(d, "structtreeroot") ||
		strings.Contains(d, "marked") ||
		strings.Contains(d, "artifact") ||
		strings.Contains(d, "/markinfo") ||
		strings.Contains(d, "tagged")
}

func describeStructureViolations(vs []Violation) string {
	var b strings.Builder
	for _, v := range vs {
		if isStructureViolation(v) {
			fmt.Fprintf(&b, "  clause %s test %d: %s\n", v.Clause, v.TestNumber, v.Description)
		}
	}
	return b.String()
}

func describeAll(vs []Violation) string {
	var b strings.Builder
	for _, v := range vs {
		fmt.Fprintf(&b, "  clause %s test %d: %s\n", v.Clause, v.TestNumber, v.Description)
	}
	return b.String()
}

// tinyRealPDF builds an untagged page whose content paints a rectangle with a
// width of 1e-42, a real closer to zero than ISO 19005-2, 6.1.13 allows. A
// PDF/A-2b remediation clamps it in the page's content stream.
func tinyRealPDF() []byte {
	const content = "BT /F1 12 Tf 50 700 Td (clamped width) Tj ET\n" +
		"0 0 0 rg 50 600 0.000000000000000000000000000000000000000001 20 re f\n"
	return buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R" +
			" /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	})
}

// TestWriterAutoTagAfterOneRemediationRoundKeepsTheRepairs pins that AutoTag
// queued after a remediation that keeps one round tags the remediated document:
// the output validates as AutoTag applied to the written remediated output
// does.
//
// A remediation that keeps one round leaves its repairs in the Writer's
// overrides, and auto-tagging read the page content, the catalog and the
// resources from the Reader's document. After a PDF/A-2b remediation it marked
// up the source content stream and wrote it over the repaired one, so the real
// the remediation had clamped came back (ISO 19005-2, 6.1.13). After a PDF/UA-1
// remediation it found the source catalog untagged and tagged the document a
// second time, replacing the structure tree the remediation had built.
func TestWriterAutoTagAfterOneRemediationRoundKeepsTheRepairs(t *testing.T) {
	t.Parallel()

	validateUA1 := func(t *testing.T, data []byte) []error {
		t.Helper()
		ctx := context.Background()
		r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("reopening the output: %v", err)
		}
		defer r.Close()
		res, err := r.ValidatePDFUA(ctx, models.PDFUA1)
		if err != nil {
			t.Fatalf("ValidatePDFUA: %v", err)
		}
		return res
	}
	for _, tc := range []struct {
		name      string
		data      []byte
		remediate func(ctx context.Context, w *Writer) (*RemediationReport, error)
		validate  func(t *testing.T, data []byte) []error
	}{
		{
			name: "a PDF/A-2b remediation clamping a real in the content",
			data: tinyRealPDF(),
			remediate: func(ctx context.Context, w *Writer) (*RemediationReport, error) {
				return w.RemediatePDFA(ctx, models.PDFA2b)
			},
			validate: validatePDFA2b,
		},
		{
			name: "a PDF/UA-1 remediation tagging a form widget",
			data: placeholderFormPDF(),
			remediate: func(ctx context.Context, w *Writer) (*RemediationReport, error) {
				return w.RemediatePDFUA(ctx, models.PDFUA1)
			},
			validate: validateUA1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			rounds := &keptRounds{}
			r, err := Open(ctx, bytes.NewReader(tc.data), int64(len(tc.data)), WithLogger(slog.New(rounds)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()
			w := NewWriter(r)
			defer w.Close()
			report, err := tc.remediate(ctx, w)
			if err != nil {
				t.Fatalf("remediation: %v", err)
			}
			if rounds.n.Load() != 0 {
				t.Fatalf("the remediation kept %d later rounds, so the Writer edits its output and the test proves nothing", rounds.n.Load())
			}
			if len(report.Fixed) == 0 {
				t.Fatal("the remediation fixed nothing, so the test proves nothing")
			}
			w.AutoTag()
			var got bytes.Buffer
			if err := w.WriteTo(ctx, &got); err != nil {
				t.Fatalf("WriteTo of AutoTag queued after the remediation: %v", err)
			}

			// The remediated document, tagged on its own.
			r2, err := Open(ctx, bytes.NewReader(tc.data), int64(len(tc.data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r2.Close()
			remW := NewWriter(r2)
			defer remW.Close()
			if _, err := tc.remediate(ctx, remW); err != nil {
				t.Fatalf("remediation: %v", err)
			}
			var remediated bytes.Buffer
			if err := remW.WriteTo(ctx, &remediated); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			base, err := Open(ctx, bytes.NewReader(remediated.Bytes()), int64(remediated.Len()))
			if err != nil {
				t.Fatalf("reopening the remediated output: %v", err)
			}
			defer base.Close()
			wantW := NewWriter(base)
			defer wantW.Close()
			wantW.AutoTag()
			var want bytes.Buffer
			if err := wantW.WriteTo(ctx, &want); err != nil {
				t.Fatalf("WriteTo of the tagged remediated output: %v", err)
			}

			if !bytes.Contains(got.Bytes(), []byte("/StructTreeRoot")) {
				t.Error("the output carries no structure tree")
			}
			gotHits, wantHits := ruleHits(tc.validate(t, got.Bytes())), ruleHits(tc.validate(t, want.Bytes()))
			if !slices.Equal(gotHits, wantHits) {
				t.Errorf("AutoTag queued after the remediation validates to %q, want the tagged remediated output's %q", gotHits, wantHits)
			}
		})
	}
}

// TestWriterAutoTagAfterAStampKeepsTheStamp pins that AutoTag queued after a
// stamp leaves the stamp in the output. Auto-tagging read the page content
// from the Reader's document and wrote its marked copy over the page's
// /Contents, dropping the stream the stamp had appended to it.
func TestWriterAutoTagAfterAStampKeepsTheStamp(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := untaggedHeadingPDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()
	if err := w.StampText(TextStamp{Text: "DRAFT", X: 50, Y: 50}); err != nil {
		t.Fatalf("StampText: %v", err)
	}
	w.AutoTag()
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	defer r2.Close()
	text, err := r2.ExtractAllText(ctx, WithArtifacts())
	if err != nil {
		t.Fatalf("ExtractAllText: %v", err)
	}
	for _, want := range []string{"DRAFT", "Document Heading"} {
		if !strings.Contains(text, want) {
			t.Errorf("the output text %q lacks %q", text, want)
		}
	}
}
