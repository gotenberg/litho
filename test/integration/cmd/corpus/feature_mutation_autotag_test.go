package main

import (
	"bytes"
	"compress/zlib"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/pdf"
)

// autoTagTestPDF returns an untagged PDF with one page per content stream,
// each drawing in Helvetica as F1. With deflate, every stream is
// Flate-compressed.
func autoTagTestPDF(t *testing.T, deflate bool, contents ...string) []byte {
	t.Helper()
	kids := make([]string, len(contents))
	for i := range contents {
		kids[i] = fmt.Sprintf("%d 0 R", 4+i)
	}
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(contents)),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
	}
	for i := range contents {
		objects = append(objects, fmt.Sprintf(
			"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>",
			4+len(contents)+i))
	}
	for _, c := range contents {
		body, filter := []byte(c), ""
		if deflate {
			var z bytes.Buffer
			zw := zlib.NewWriter(&z)
			if _, err := zw.Write(body); err != nil {
				t.Fatalf("deflating: %v", err)
			}
			if err := zw.Close(); err != nil {
				t.Fatalf("deflating: %v", err)
			}
			body, filter = z.Bytes(), " /Filter /FlateDecode"
		}
		objects = append(objects, fmt.Sprintf("<< /Length %d%s >>\nstream\n%s\nendstream", len(body), filter, body))
	}

	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offsets := make([]int, len(objects))
	for i, body := range objects {
		offsets[i] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, body)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return buf.Bytes()
}

// markedSpan is page content whose text sits in a marked-content sequence
// that is neither structure nor an artifact, which AutoTag leaves alone.
const markedSpan = "/Span <</Lang (en)>> BDC BT /F1 12 Tf 72 700 Td (Marked text) Tj ET EMC"

// TestMutationAutoTag_ADocumentedSkipIsNotABrokenPromise runs mutate-autotag
// on untagged inputs with text whose content is already marked, plain and
// Flate-compressed. AutoTag skips them by design and says so, and the check
// finds the marked content itself, so neither counts.
func TestMutationAutoTag_ADocumentedSkipIsNotABrokenPromise(t *testing.T) {
	t.Parallel()
	for _, deflate := range []bool{false, true} {
		t.Run(fmt.Sprintf("deflate=%t", deflate), func(t *testing.T) {
			t.Parallel()
			data := autoTagTestPDF(t, deflate, "BT /F1 12 Tf 72 700 Td (Unmarked text) Tj ET", markedSpan)
			f := newMutationFeature(specNamed(t, "autotag"))
			for _, v := range runMutationFixtures(t, f, data, "PDF_UA-1", "PDF_A-2b") {
				if v.Skipped || !v.Match || v.Metrics[metricPromiseBroken] != 0 {
					t.Errorf("%s: skipped=%t match=%t promise_broken=%d reason %q, want a match",
						v.FixtureID, v.Skipped, v.Match, v.Metrics[metricPromiseBroken], v.Reason)
				}
			}
		})
	}
}

// TestMutationAutoTag_AnUnprovenReportIsABrokenPromise plants AutoTag reports
// the input contradicts, on a plain untagged document with text and no marked
// content. A check that took the report's word would pass each of them, so a
// defect that made AutoTag skip, or claim a tree it did not build, would
// leave the metric at zero.
func TestMutationAutoTag_AnUnprovenReportIsABrokenPromise(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		report pdf.AutoTagReport
		// skipEdit leaves AutoTag unqueued, so the output has no tree.
		skipEdit bool
		want     string
	}{
		{name: "never applied", report: pdf.AutoTagReport{}, want: "without applying"},
		{name: "tagged without a tree", report: pdf.AutoTagReport{Applied: true}, skipEdit: true, want: "no structure tree"},
		{name: "already tagged", report: pdf.AutoTagReport{Applied: true, Skipped: pdf.ErrAutoTagAlreadyTagged}, want: "structure tree nor /MarkInfo"},
		{name: "marked content", report: pdf.AutoTagReport{Applied: true, Skipped: fmt.Errorf("page 1: %w", pdf.ErrAutoTagMarkedContent), Page: 1}, want: "no marked-content operator"},
		{name: "unreadable content", report: pdf.AutoTagReport{Applied: true, Skipped: pdf.ErrAutoTagUnreadableContent, Page: 1}, want: "page 1 reads"},
		{name: "nothing to tag", report: pdf.AutoTagReport{Applied: true, Skipped: pdf.ErrAutoTagNothingToTag}, want: "has text"},
		{name: "an unknown reason", report: pdf.AutoTagReport{Applied: true, Skipped: errors.New("felt like it")}, want: "felt like it"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec := specNamed(t, "autotag")
			apply := spec.apply
			spec.apply = func(ctx context.Context, f *mutationFeature, in *mutationInput, w *pdf.Writer) (mutationPlan, error) {
				plan := identityPlan(in)
				if !tc.skipEdit {
					var err error
					if plan, err = apply(ctx, f, in, w); err != nil {
						return plan, err
					}
				}
				report := tc.report
				plan.autoTag = &report
				return plan, nil
			}
			v := runMutationFixtures(t, newMutationFeature(spec), mutationTestPDF(t), "PDF_UA-1")[0]
			if v.Match || v.Metrics[metricPromiseBroken] != 1 {
				t.Fatalf("match=%t promise_broken=%d reason %q, want a broken promise", v.Match, v.Metrics[metricPromiseBroken], v.Reason)
			}
			if !strings.Contains(v.Reason, tc.want) {
				t.Errorf("reason %q does not say %q", v.Reason, tc.want)
			}
		})
	}
}

func TestScanMarkedContent(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		data []byte
		want bool
	}{
		{name: "plain BDC", data: autoTagTestPDF(t, false, markedSpan), want: true},
		{name: "deflated BDC", data: autoTagTestPDF(t, true, markedSpan), want: true},
		{name: "deflated BMC", data: autoTagTestPDF(t, true, "/Artifact BMC 0 0 m 10 10 l S EMC"), want: true},
		{name: "no marked content", data: autoTagTestPDF(t, true, "BT /F1 12 Tf 72 700 Td (Plain text) Tj ET"), want: false},
		{name: "operator letters inside a name", data: autoTagTestPDF(t, false, "/XBDC gs 0 0 m 10 10 l S"), want: false},
	} {
		if got := scanMarkedContent(tc.data); got != tc.want {
			t.Errorf("%s: scanMarkedContent = %t, want %t", tc.name, got, tc.want)
		}
	}
}
