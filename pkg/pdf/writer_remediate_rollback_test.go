package pdf

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// pages12PDF builds a 12-page document whose pages show a line of Helvetica
// text each, unembedded and WinAnsi-encoded. Page 12 also shows the control
// byte 0x01, which WinAnsiEncoding leaves undefined, so it references the
// .notdef glyph (ISO 19005-2, 6.2.11.8 t1, ISO 14289-1, 7.21.8 t1). The font's
// /ToUnicode maps that byte to "X", so text extraction reads it.
//
// An aggressive remediation's text-control fixer strips the byte, which costs
// the extracted text the one character: a strict regression tolerance refuses
// that output and rolls the content tier back, the default one keeps it.
func pages12PDF() []byte {
	const toUnicode = "/CIDInit /ProcSet findresource begin 12 dict begin begincmap " +
		"/CMapName /Pages12 def 1 begincodespacerange <00> <FF> endcodespacerange " +
		"1 beginbfchar <01> <0058> endbfchar endcmap " +
		"CMapName currentdict /CMap defineresource pop end end"
	const pages = 12
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"", // The page tree, once the page numbers are known.
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding /ToUnicode 4 0 R >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(toUnicode), toUnicode),
	}
	var kids []string
	for i := range pages {
		body := fmt.Sprintf("BT /F1 12 Tf 72 700 Td (Page %d of the twelve-page fixture) Tj ET\n", i+1)
		if i == pages-1 {
			body += "BT /F1 12 Tf 72 600 Td (\\001) Tj ET\n"
		}
		page := len(objs) + 1
		kids = append(kids, fmt.Sprintf("%d 0 R", page))
		objs = append(objs,
			fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents %d 0 R /Resources << /Font << /F1 3 0 R >> >> >>", page+1),
			fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body))
	}
	objs[1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), pages)
	return buildPDF(objs)
}

// strictTextTolerance refuses any drop of the extracted text.
var strictTextTolerance = RegressionTolerance{MaxTextDropRatio: 0, MaxImageDropRatio: 1}

// remediatedOutput is what one remediation of a source wrote, with the report
// it returned.
type remediatedOutput struct {
	report *RemediationReport
	bytes  []byte
}

// remediateToBytes opens data, remediates it with remediate, and writes the
// output.
func remediateToBytes(t *testing.T, data []byte, remediate func(context.Context, *Writer) (*RemediationReport, error)) remediatedOutput {
	t.Helper()
	ctx := context.Background()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	w := NewWriter(r)
	report, err := remediate(ctx, w)
	if err != nil {
		t.Fatalf("remediating: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	return remediatedOutput{report: report, bytes: out.Bytes()}
}

// sortedErrorStrings returns the messages of errs, sorted.
func sortedErrorStrings(errs []error) []string {
	out := errorStrings(errs)
	slices.Sort(out)
	return out
}

// TestWriterRemediateRollbackReportsTheOutput drives a real content-tier
// rollback end to end and pins that the report describes the bytes written.
// An aggressive remediation of [pages12PDF] under a strict tolerance strips
// the control byte, loses a character of text, and rolls the content tier
// back. Its output must then be the output of the same remediation without
// the aggressive fixes, which never strip it: the same Fixed, the same bytes
// but for the /ID, whose derivation hashes the content mode, and a Residual
// that re-validating the written bytes reproduces, the .notdef reference
// included. RolledBack must name the strip, and only it.
func TestWriterRemediateRollbackReportsTheOutput(t *testing.T) {
	t.Parallel()

	data := pages12PDF()
	type profileRun struct {
		remediate func(context.Context, *Writer, ...RemediateOption) (*RemediationReport, error)
		validate  func(context.Context, *Reader) ([]error, error)
	}
	pdfa := func(p models.PDFAProfile) profileRun {
		return profileRun{
			remediate: func(ctx context.Context, w *Writer, opts ...RemediateOption) (*RemediationReport, error) {
				return w.RemediatePDFA(ctx, p, opts...)
			},
			validate: func(ctx context.Context, r *Reader) ([]error, error) { return r.ValidatePDFA(ctx, p) },
		}
	}
	pdfua := func(p models.PDFUAProfile) profileRun {
		return profileRun{
			remediate: func(ctx context.Context, w *Writer, opts ...RemediateOption) (*RemediationReport, error) {
				return w.RemediatePDFUA(ctx, p, opts...)
			},
			validate: func(ctx context.Context, r *Reader) ([]error, error) { return r.ValidatePDFUA(ctx, p) },
		}
	}
	for _, tc := range []struct {
		name string
		run  profileRun
		// notdef is the clause the profile states the .notdef rule in.
		notdef string
		// derivedIDs is set when the remediation mints a trailer /ID, which
		// hashes the content mode (see WithAggressiveFixes).
		derivedIDs bool
	}{
		{name: "PDF/A-2b", run: pdfa(models.PDFA2b), notdef: "6.2.11.8", derivedIDs: true},
		{name: "PDF/A-4", run: pdfa(models.PDFA4), notdef: "6.2.10.9", derivedIDs: true},
		{name: "PDF/UA-1", run: pdfua(models.PDFUA1), notdef: "7.21.8"},
		{name: "PDF/UA-2", run: pdfua(models.PDFUA2), notdef: "8.4.5.9"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			with := func(opts ...RemediateOption) func(context.Context, *Writer) (*RemediationReport, error) {
				return func(ctx context.Context, w *Writer) (*RemediationReport, error) {
					return tc.run.remediate(ctx, w, opts...)
				}
			}
			kept := remediateToBytes(t, data, with(WithAggressiveFixes()))
			rolled := remediateToBytes(t, data, with(WithAggressiveFixes(), WithRegressionTolerance(strictTextTolerance)))
			plain := remediateToBytes(t, data, with())

			var strip string
			for _, d := range kept.report.Fixed {
				if strings.Contains(d, "control-byte") {
					strip = d
				}
			}
			if strip == "" {
				t.Fatalf("the default tolerance kept no control-byte strip: Fixed = %q, RolledBack = %q", kept.report.Fixed, kept.report.RolledBack)
			}
			if !slices.Equal(rolled.report.RolledBack, []string{strip}) {
				t.Fatalf("RolledBack = %q, want only %q", rolled.report.RolledBack, strip)
			}
			if slices.Contains(rolled.report.Fixed, strip) {
				t.Errorf("Fixed still names the rolled-back %q", strip)
			}
			if !slices.Equal(rolled.report.Fixed, plain.report.Fixed) {
				t.Errorf("the rolled-back run reports Fixed %q, want the %q of a run that never stripped", rolled.report.Fixed, plain.report.Fixed)
			}
			gotBytes, wantBytes := rolled.bytes, plain.bytes
			if tc.derivedIDs {
				gotBytes, wantBytes = stripRandomID(gotBytes), stripRandomID(wantBytes)
			}
			if !bytes.Equal(gotBytes, wantBytes) {
				t.Errorf("the rolled-back output (%d bytes) differs from the output of a run that never stripped (%d bytes)", len(rolled.bytes), len(plain.bytes))
			}

			ctx := context.Background()
			r, err := Open(ctx, bytes.NewReader(rolled.bytes), int64(len(rolled.bytes)))
			if err != nil {
				t.Fatalf("reopening the rolled-back output: %v", err)
			}
			defer r.Close()
			text, err := r.ExtractText(ctx, 12)
			if err != nil {
				t.Fatalf("ExtractText: %v", err)
			}
			if !strings.Contains(text, "X") {
				t.Errorf("page 12 of the rolled-back output reads %q, want the control byte's X kept", text)
			}
			found, err := tc.run.validate(ctx, r)
			if err != nil {
				t.Fatalf("validating the rolled-back output: %v", err)
			}
			if got, want := sortedErrorStrings(rolled.report.Residual), sortedErrorStrings(found); !slices.Equal(got, want) {
				t.Errorf("Residual = %q, want the %q re-validating the written bytes finds", got, want)
			}
			if !slices.ContainsFunc(rolled.report.ResidualViolations(), func(v Violation) bool { return v.Clause == tc.notdef }) {
				t.Errorf("Residual = %q, want the .notdef reference (%s) the rolled-back strip had claimed", rolled.report.Residual, tc.notdef)
			}
		})
	}
}
