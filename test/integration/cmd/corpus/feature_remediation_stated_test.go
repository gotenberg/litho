package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
	"github.com/gotenberg/litho/pkg/pdf"
)

// TestRemediateStating checks what the remediation cells state of a PDF, as
// a caller would: to PDF/UA the language always, written only where the
// document states none, and the title only when the report finds the
// document untitled; to PDF/A neither. A document that states both conforms
// on its own and is not marked stated; one that states neither conforms with
// what the cell stated and is marked so.
func TestRemediateStating(t *testing.T) {
	fonts, err := fixtureFontDirs(fixtureFontsRoot(), diffFontsTarget)
	if err != nil {
		t.Skip(err)
	}
	ctx := context.Background()
	convert := func(head string) []byte {
		var buf bytes.Buffer
		c := pdf.FromHTML(strings.NewReader(`<!doctype html>`+head+`<p>A line of text.</p>`), pdf.WithConvertFontDirs(fonts...))
		if _, err := c.WriteTo(ctx, &buf); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	ua := func(ctx context.Context, w *pdf.Writer, stated []pdf.RemediateOption) (*models.RemediationReport, error) {
		return w.RemediatePDFUA(ctx, models.PDFUA1, stated...)
	}
	pdfa := func(ctx context.Context, w *pdf.Writer, stated []pdf.RemediateOption) (*models.RemediationReport, error) {
		if len(stated) > 0 {
			t.Errorf("PDF/A remediation stated %d options, want none", len(stated))
		}
		return w.RemediatePDFA(ctx, models.PDFA2b, stated...)
	}
	validateUA := func(r *pdf.Reader) ([]models.Violation, error) { return r.ValidatePDFUAReport(ctx, models.PDFUA1) }
	validateA := func(r *pdf.Reader) ([]models.Violation, error) { return r.ValidatePDFAReport(ctx, models.PDFA2b) }
	for name, tc := range map[string]struct {
		head       string
		accessible bool
		remediate  statedRemediation
		validate   func(*pdf.Reader) ([]models.Violation, error)
		stated     bool
	}{
		"source states both":   {head: `<html lang="en"><title>Notes</title>`, accessible: true, remediate: ua, validate: validateUA},
		"source states none":   {accessible: true, remediate: ua, validate: validateUA, stated: true},
		"PDF/A states nothing": {remediate: pdfa, validate: validateA},
	} {
		t.Run(name, func(t *testing.T) {
			out, stated, err := remediateStating(ctx, convert(tc.head), tc.accessible, "doc", tc.remediate)
			if err != nil {
				t.Fatal(err)
			}
			if stated != tc.stated {
				t.Errorf("stated = %v, want %v", stated, tc.stated)
			}
			r, err := pdf.Open(ctx, bytes.NewReader(out), int64(len(out)))
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			if violations, err := tc.validate(r); err != nil || len(violations) > 0 {
				t.Errorf("the output fails: %v, %v", violations, err)
			}
		})
	}
}
