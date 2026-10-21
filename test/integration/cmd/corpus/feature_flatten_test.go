package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/gotenberg/litho/pkg/pdf"
)

// transparentKindPDF is a one-page PDF drawing body against a graphics state
// /Half of constant alpha 0.5 and the font /F1.
func transparentKindPDF(body string) []byte {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R" +
			" /Resources << /Font << /F1 5 0 R >> /ExtGState << /Half 6 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(body), body),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		"<< /Type /ExtGState /ca 0.5 /CA 0.5 >>",
	}
	var buf bytes.Buffer
	buf.WriteString("%PDF-1.7\n")
	offsets := make([]int, 0, len(objects))
	for i, obj := range objects {
		offsets = append(offsets, buf.Len())
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := buf.Len()
	fmt.Fprintf(&buf, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&buf, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return buf.Bytes()
}

// lithoAsPoppler stands in for poppler with litho's own renderer, so the
// poppler half runs without Docker.
func lithoAsPoppler(ctx context.Context, dir string, jobs []popplerJob) (string, error) {
	for _, j := range jobs {
		r, err := pdf.OpenFile(ctx, filepath.Join(dir, j.PDF))
		if err != nil {
			return "", err
		}
		img, err := r.RenderPage(ctx, j.Page, pdf.WithRenderDPI(renderDPI))
		_ = r.Close()
		if err != nil {
			return "", err
		}
		f, err := os.Create(filepath.Join(dir, j.PNG+".png"))
		if err != nil {
			return "", err
		}
		err = png.Encode(f, img)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return "", err
		}
	}
	return "test", nil
}

// runFlatten runs the feature on data and scores it.
func runFlatten(t *testing.T, f *flattenFeature, data []byte) (Verdict, flattenOutput) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "in.pdf")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := f.LoadGoldens(dir); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := f.Prepare(ctx, []Fixture{{ID: "in", PDFPath: path}}); err != nil {
		t.Fatal(err)
	}
	out, err := f.Run(ctx, path, "in")
	if err != nil {
		t.Fatal(err)
	}
	var decoded flattenOutput
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatal(err)
	}
	return f.Compare("in", out), decoded
}

func newTestFlatten() *flattenFeature {
	f := newFlattenFeature()
	f.workers = 1
	f.pin = func() string { return "" }
	f.poppler = lithoAsPoppler
	return f
}

const flattenBody = "0 0 1 rg 0 0 200 100 re f q /Half gs 1 0 0 rg 40 40 80 80 re f Q BT /F1 18 Tf 0 g 20 160 Td (Kept) Tj ET"

// TestFlattenFeature_Plants pins the cell end to end: the real flattening
// passes, a flattening that changes nothing but claims a page fails on the
// transparency it leaves, one that draws over the page fails both
// renderings, one that drops text fails the text check, and a page with no
// transparency skips.
func TestFlattenFeature_Plants(t *testing.T) {
	t.Parallel()
	data := transparentKindPDF(flattenBody)

	v, out := runFlatten(t, newTestFlatten(), data)
	if !v.Match || v.Metrics[metricFlattenChecked] != 1 || out.Pages[0].Poppler == nil {
		t.Errorf("control: %+v %+v", v, out)
	}

	claim := func(ctx context.Context, w *pdf.Writer) (*pdf.FlattenReport, error) {
		// Rewriting the file changes nothing a viewer sees.
		w.SetMetadata(pdf.Metadata{Title: "planted"})
		return &pdf.FlattenReport{Pages: []pdf.FlattenedPage{{Page: 1}}}, ctx.Err()
	}
	left := newTestFlatten()
	left.flatten = claim
	if v, _ := runFlatten(t, left, data); v.Match || v.Metrics[metricFlattenTransparencyLeft] != 1 {
		t.Errorf("transparency left: %+v", v)
	}

	drawn := newTestFlatten()
	drawn.flatten = func(ctx context.Context, w *pdf.Writer) (*pdf.FlattenReport, error) {
		if _, err := w.FlattenTransparency(ctx); err != nil {
			return nil, err
		}
		if err := w.StampText(pdf.TextStamp{Text: "PLANTED PLANTED", FontSize: 40, Opacity: 1}); err != nil {
			return nil, err
		}
		return &pdf.FlattenReport{Pages: []pdf.FlattenedPage{{Page: 1}}}, nil
	}
	v, _ = runFlatten(t, drawn, data)
	if v.Match || v.Metrics[metricFlattenLithoMismatch] != 1 || v.Metrics[metricFlattenPopplerMismatch] != 1 {
		t.Errorf("drawn over: %+v", v)
	}

	dropped := newTestFlatten()
	dropped.flatten = func(ctx context.Context, w *pdf.Writer) (*pdf.FlattenReport, error) {
		if _, err := w.Redact(ctx, []pdf.RedactRegion{{Page: 1, Rect: [4]float64{15, 150, 100, 185}}}); err != nil {
			return nil, err
		}
		return w.FlattenTransparency(ctx)
	}
	if v, _ := runFlatten(t, dropped, data); v.Match || v.Metrics[metricFlattenTextChanged] != 1 {
		t.Errorf("text dropped: %+v", v)
	}

	if v, _ := runFlatten(t, newTestFlatten(), redactKindPDF("0 0 1 rg 0 0 200 100 re f")); !v.Skipped || v.Metrics[metricFlattenUnchanged] != 1 {
		t.Errorf("opaque page: %+v", v)
	}
}

// TestFlattenFeature_PopplerUnavailableDoesNotPass requires a run whose
// poppler half could not run to fail rather than pass on litho's half.
func TestFlattenFeature_PopplerUnavailableDoesNotPass(t *testing.T) {
	t.Parallel()
	f := newTestFlatten()
	f.poppler = func(context.Context, string, []popplerJob) (string, error) {
		return "", fmt.Errorf("%w: test", errPopplerUnavailable)
	}
	if v, _ := runFlatten(t, f, transparentKindPDF(flattenBody)); v.Match || v.Metrics[metricPopplerUnavailable] == 0 {
		t.Errorf("verdict %+v, want a mismatch counting poppler_unavailable", v)
	}
}
