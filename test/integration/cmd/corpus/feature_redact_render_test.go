package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gotenberg/litho/pkg/pdf"
)

// redactKindPDF returns a 200 by 200 page drawing body, with Helvetica as
// /F1, a 2 by 2 picture as /Im0 and an axial shading as /Sh0. The centred
// quarter of the page, the p1-quarter region, is [50 50 150 150].
func redactKindPDF(body string) []byte {
	stream := func(dict, data string) string {
		return fmt.Sprintf("<< %s /Length %d >>\nstream\n%s\nendstream", dict, len(data), data)
	}
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] /Contents 4 0 R" +
			" /Resources << /Font << /F1 5 0 R >> /XObject << /Im0 6 0 R >> /Shading << /Sh0 7 0 R >> >> >>",
		stream("", body),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
		stream("/Type /XObject /Subtype /Image /Width 2 /Height 2 /ColorSpace /DeviceRGB /BitsPerComponent 8",
			"\xff\x00\x00\x00\xff\x00\x00\x00\xff\xff\xff\xff"),
		"<< /ShadingType 2 /ColorSpace /DeviceRGB /Coords [80 0 120 0]" +
			" /Function << /FunctionType 2 /Domain [0 1] /C0 [1 0 0] /C1 [0 0 1] /N 1 >> >>",
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

// redactKinds holds one page per kind of content redaction removes, the
// content inside the p1-quarter region and nothing outside it.
//
//nolint:gochecknoglobals // static test table
var redactKinds = []struct {
	name string
	body string
	// refuses names the refusal the real redaction answers the quarter
	// with, nil when it clears it.
	refuses error
}{
	{"text", "BT /F1 24 Tf 80 95 Td (AB) Tj ET", nil},
	{"path", "0 0 1 rg 80 80 40 40 re f", nil},
	{"image", "q 40 0 0 40 80 80 cm /Im0 Do Q", nil},
	{"inline image", "q 40 0 0 40 80 80 cm BI /W 2 /H 2 /CS /RGB /BPC 8 ID \xff\x00\x00\x00\xff\x00\x00\x00\xff\xff\xff\xff EI Q", nil},
	{"shading", "q 80 80 40 40 re W n /Sh0 sh Q", pdf.ErrRedactionRefused},
}

// runRedactRender prepares the feature over one PDF and returns the outcome
// of the scenario named name.
func runRedactRender(t *testing.T, f *redactRenderFeature, data []byte, name string) (Verdict, redactRenderOutcome) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "kind.pdf")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := f.LoadGoldens(dir); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := f.Prepare(ctx, []Fixture{{ID: "kind", PDFPath: path}}); err != nil {
		t.Fatal(err)
	}
	out, err := f.Run(ctx, path, "kind")
	if err != nil {
		t.Fatal(err)
	}
	var decoded redactRenderOutput
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatal(err)
	}
	for _, sc := range decoded.Scenarios {
		if sc.Name == name {
			return f.Compare("kind", out), sc
		}
	}
	t.Fatalf("no %s scenario in %s", name, out)
	return Verdict{}, redactRenderOutcome{}
}

// newTestRedactRender returns the feature with poppler reported unavailable,
// so a verdict stands on litho's half: the planted leaks must fail it alone.
func newTestRedactRender() *redactRenderFeature {
	f := newRedactRenderFeature()
	f.workers = 1
	f.pin = func() string { return "" }
	f.poppler = func(context.Context, string, []popplerJob) (string, error) {
		return "", fmt.Errorf("%w: test", errPopplerUnavailable)
	}
	return f
}

// TestRedactRenderCatchesPlantedLeaks plants a redaction that removes nothing
// and reports success, for each kind of content, and requires the inside
// check to fail; the real redaction of the same page passes, or refuses for
// the shading it cannot cut.
func TestRedactRenderCatchesPlantedLeaks(t *testing.T) {
	t.Parallel()
	for _, kind := range redactKinds {
		data := redactKindPDF(kind.body)

		broken := newTestRedactRender()
		broken.redact = func(context.Context, *pdf.Writer, pdf.RedactRegion) (*pdf.RedactReport, error) { return nil, nil }
		v, sc := runRedactRender(t, broken, data, "p1-quarter")
		if sc.Litho == nil || sc.Litho.Inside == 0 || v.Metrics[metricInsideLeaked] == 0 {
			t.Errorf("%s: a redaction that left the content passed the inside check: %+v", kind.name, sc.Litho)
		}

		_, sc = runRedactRender(t, newTestRedactRender(), data, "p1-quarter")
		switch {
		case kind.refuses != nil:
			if sc.Refused == "" {
				t.Errorf("%s: the redaction did not refuse: %+v", kind.name, sc)
			}
		case sc.Litho == nil || !sc.Litho.passed():
			t.Errorf("%s: the real redaction failed a check: %+v (%s)", kind.name, sc.Litho, sc.Cause)
		}
	}
}

// TestRedactRenderCatchesChangesOutside plants a redaction that clears the
// whole page when asked for its centred quarter, and requires the outside
// check to fail.
func TestRedactRenderCatchesChangesOutside(t *testing.T) {
	t.Parallel()
	f := newTestRedactRender()
	f.redact = func(ctx context.Context, w *pdf.Writer, region pdf.RedactRegion) (*pdf.RedactReport, error) {
		region.Rect = [4]float64{-1000, -1000, 1000, 1000}
		return w.Redact(ctx, []pdf.RedactRegion{region})
	}
	data := redactKindPDF("0 0 1 rg 80 80 40 40 re f 1 0 0 rg 10 10 20 20 re f")
	v, sc := runRedactRender(t, f, data, "p1-quarter")
	if sc.Litho == nil || sc.Litho.Outside == 0 || v.Metrics[metricOutsideChanged] == 0 {
		t.Errorf("a redaction that cleared outside the region passed the outside check: %+v", sc.Litho)
	}
	if v.Match {
		t.Error("the PDF matched")
	}
}

// TestRedactRenderPopplerUnavailableDoesNotPass requires a scenario whose
// poppler half did not run to count, and the PDF not to match.
func TestRedactRenderPopplerUnavailableDoesNotPass(t *testing.T) {
	t.Parallel()
	v, sc := runRedactRender(t, newTestRedactRender(), redactKindPDF("0 0 1 rg 80 80 40 40 re f"), "p1-quarter")
	if sc.PopplerSkipped == "" {
		t.Fatalf("poppler half not reported skipped: %+v", sc)
	}
	if v.Match || v.Metrics[metricPopplerUnavailable] == 0 {
		t.Errorf("verdict %+v, want a mismatch counting poppler_unavailable", v)
	}
}

// TestRedactRenderIsDeterministic runs the feature twice on one PDF and
// requires the same output.
func TestRedactRenderIsDeterministic(t *testing.T) {
	t.Parallel()
	data := redactKindPDF("BT /F1 24 Tf 80 95 Td (AB) Tj ET q 40 0 0 40 20 20 cm /Im0 Do Q")
	run := func() []byte {
		f := newTestRedactRender()
		dir := t.TempDir()
		path := filepath.Join(dir, "kind.pdf")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := f.Prepare(context.Background(), []Fixture{{ID: "kind", PDFPath: path}}); err != nil {
			t.Fatal(err)
		}
		out, err := f.Run(context.Background(), path, "kind")
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if a, b := run(), run(); !bytes.Equal(a, b) {
		t.Errorf("two runs differ:\n%s\n%s", a, b)
	}
}
