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
	return redactImagePDF(body, "/Width 2 /Height 2", "\xff\x00\x00\x00\xff\x00\x00\x00\xff\xff\xff\xff")
}

// redactImagePDF returns the page of [redactKindPDF] with /Im0 an 8-bit
// DeviceRGB image of the entries and samples given.
func redactImagePDF(body, entries, samples string) []byte {
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
		stream("/Type /XObject /Subtype /Image "+entries+" /ColorSpace /DeviceRGB /BitsPerComponent 8", samples),
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
		broken.redact = func(context.Context, *pdf.Writer, pdf.RedactRegion, ...pdf.RedactOption) (*pdf.RedactReport, error) {
			return nil, nil
		}
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
	f.redact = func(ctx context.Context, w *pdf.Writer, region pdf.RedactRegion, _ ...pdf.RedactOption) (*pdf.RedactReport, error) {
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

// redactShadingBody is the shading kind of [redactKinds], which the default
// mode refuses.
const redactShadingBody = "q 80 80 40 40 re W n /Sh0 sh Q"

// TestRedactRenderChecksTheRasterFallback requires a page the default mode
// refuses, a shading the region cuts, to run again with the raster fallback
// and pass litho's checks, and a page the default mode clears to run no
// fallback scenario.
func TestRedactRenderChecksTheRasterFallback(t *testing.T) {
	t.Parallel()
	v, sc := runRedactRender(t, newTestRedactRender(), redactKindPDF(redactShadingBody), "p1-quarter"+redactFallbackSuffix)
	if !sc.Fallback || sc.RasterisedUnits == 0 || sc.Refused != "" || !sc.Litho.passed() {
		t.Errorf("fallback scenario %+v (%s), want the shading rasterised and litho's checks passed", sc, sc.Cause)
	}
	if v.Metrics[metricFallbackScenarios] == 0 || v.Metrics[metricRasterisedUnits] == 0 || v.Metrics[metricRedactRefused] == 0 {
		t.Errorf("metrics %v, want fallback scenarios after refusals", v.Metrics)
	}

	f := newTestRedactRender()
	dir := t.TempDir()
	path := filepath.Join(dir, "kind.pdf")
	if err := os.WriteFile(path, redactKindPDF("BT /F1 24 Tf 80 95 Td (AB) Tj ET"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := f.Prepare(context.Background(), []Fixture{{ID: "kind", PDFPath: path}}); err != nil {
		t.Fatal(err)
	}
	out, err := f.Run(context.Background(), path, "kind")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(out, []byte(redactFallbackSuffix)) {
		t.Errorf("a page the default mode clears ran a fallback scenario: %s", out)
	}
}

// TestRedactRenderCatchesAFallbackLeak plants a fallback that cuts only a
// sliver of the region out of the shading it rasterises, and requires the
// inside check to fail on the fallback scenario.
func TestRedactRenderCatchesAFallbackLeak(t *testing.T) {
	t.Parallel()
	f := newTestRedactRender()
	f.redact = func(ctx context.Context, w *pdf.Writer, region pdf.RedactRegion, opts ...pdf.RedactOption) (*pdf.RedactReport, error) {
		if len(opts) > 0 {
			region.Rect = [4]float64{81, 81, 82, 82}
		}
		return w.Redact(ctx, []pdf.RedactRegion{region}, opts...)
	}
	v, sc := runRedactRender(t, f, redactKindPDF(redactShadingBody), "p1-quarter"+redactFallbackSuffix)
	if sc.Litho == nil || sc.Litho.Inside == 0 || v.Metrics[metricFallbackInsideLeaked] == 0 || v.Match {
		t.Errorf("a fallback that left the shading inside the region passed: %+v, metrics %v", sc.Litho, v.Metrics)
	}
}

// TestRedactRenderCatchesAFadedFallbackImage requires litho's half of a
// fallback scenario's outside check to fail on an image that paints the
// source faded, as one drawn under a constant alpha of 0.8 does, and to keep
// passing one whose samples resample an edge of the source. The render
// tolerance poppler's half keeps passes the faded image.
func TestRedactRenderCatchesAFadedFallbackImage(t *testing.T) {
	t.Parallel()
	const w, h = 6, 4
	fill := func(px [3]uint8) []uint8 {
		out := make([]uint8, 0, 3*w*h)
		for range w * h {
			out = append(out, px[:]...)
		}
		return out
	}
	sets := &redactPixelSets{w: w, h: h, region: [4]float64{100, 100, 101, 101}}
	paints := make([]bool, w*h)
	for i := range paints {
		paints[i] = true
	}
	navy := [3]uint8{40, 40, 200}
	faded := [3]uint8{83, 83, 211} // navy at alpha 0.8 over white
	before := fill(navy)
	out := sets.countOutside(before, fill(faded), w, nil, paints, redactRenderFallbackTolerance)
	if out.changed != w*h || out.strict != w*h {
		t.Errorf("faded image under litho's tolerance: %d changed, %d strict, want %d of each", out.changed, out.strict, w*h)
	}
	if out := sets.countOutside(before, fill(faded), w, nil, paints, renderChannelTolerance); out.changed != 0 {
		t.Errorf("faded image under the render tolerance: %d changed, want 0", out.changed)
	}

	// The left half navy, the right half white; the image's resampling
	// blends the column at the edge.
	edge := fill([3]uint8{255, 255, 255})
	resampled := fill([3]uint8{255, 255, 255})
	for y := range h {
		for x := range w / 2 {
			copy(edge[3*(y*w+x):], navy[:])
			copy(resampled[3*(y*w+x):], navy[:])
		}
		copy(resampled[3*(y*w+w/2-1):], []uint8{150, 150, 228})
	}
	if out := sets.countOutside(edge, resampled, w, nil, paints, redactRenderFallbackTolerance); out.changed != 0 || out.resampled == 0 {
		t.Errorf("resampled edge under litho's tolerance: %d changed, %d resampled, want 0 and some", out.changed, out.resampled)
	}

	// A point sample on a stroke thinner than the pixel: darker than any
	// pixel around it, within the render tolerance. At an edge it stands.
	for y := range h {
		copy(resampled[3*(y*w+w/2-1):], []uint8{20, 20, 180})
	}
	if out := sets.countOutside(edge, resampled, w, nil, paints, redactRenderFallbackTolerance); out.changed != 0 {
		t.Errorf("point-sampled edge under litho's tolerance: %d changed, want 0", out.changed)
	}
}

// TestRedactRenderInterpolationSpill redacts the centred quarter over an
// interpolated 4 by 4 image whose upper right samples it covers, and requires
// the kept samples the interpolation blends into the blank to count as
// interpolation_spill, not as a leak. Two planted leaks over the same image
// still fail the inside check: a redaction that removes nothing, and one that
// leaves unblanked the samples whose centres lie just inside the region.
func TestRedactRenderInterpolationSpill(t *testing.T) {
	t.Parallel()
	var samples []byte
	for y := range 4 {
		for x := range 4 {
			samples = append(samples, byte(120+40*x), byte(120+40*y), 220)
		}
	}
	const body = "q 100 0 0 100 0 0 cm /Im0 Do Q"
	smooth := redactImagePDF(body, "/Width 4 /Height 4 /Interpolate true", string(samples))

	v, sc := runRedactRender(t, newTestRedactRender(), smooth, "p1-quarter")
	if sc.Litho == nil || !sc.Litho.passed() || sc.Litho.InterpolationSpill == 0 || v.Metrics[metricInterpolationSpill] == 0 {
		t.Errorf("interpolated image: %+v, metrics %v, want a pass with interpolation spill", sc.Litho, v.Metrics)
	}

	_, sc = runRedactRender(t, newTestRedactRender(), redactImagePDF(body, "/Width 4 /Height 4", string(samples)), "p1-quarter")
	if sc.Litho == nil || !sc.Litho.passed() || sc.Litho.InterpolationSpill != 0 {
		t.Errorf("image drawn without interpolation: %+v, want a pass without interpolation spill", sc.Litho)
	}

	for name, redact := range map[string]func(context.Context, *pdf.Writer, pdf.RedactRegion, ...pdf.RedactOption) (*pdf.RedactReport, error){
		"removes nothing": func(context.Context, *pdf.Writer, pdf.RedactRegion, ...pdf.RedactOption) (*pdf.RedactReport, error) {
			return nil, nil
		},
		// The samples centred at 62.5 lie in the region [50 50 150 150] and
		// out of [70 70 150 150].
		"keeps samples inside": func(ctx context.Context, w *pdf.Writer, region pdf.RedactRegion, opts ...pdf.RedactOption) (*pdf.RedactReport, error) {
			region.Rect = [4]float64{70, 70, 150, 150}
			return w.Redact(ctx, []pdf.RedactRegion{region}, append(opts, pdf.WithoutRedactionPixelCheck())...)
		},
	} {
		f := newTestRedactRender()
		f.redact = redact
		v, sc := runRedactRender(t, f, smooth, "p1-quarter")
		if sc.Litho == nil || sc.Litho.Inside == 0 || v.Metrics[metricInsideLeaked] == 0 || v.Match {
			t.Errorf("%s: a leak over an interpolated image passed: %+v", name, sc.Litho)
		}
	}
}

// TestRedactRenderSpillSpreadStops requires poppler's spill to grow from the
// pixels litho shows blanking changed into the pixels poppler draws darker
// next to them, and to stop [redactRenderSpillSpread] pixels out, so a page
// drawn darker all over still fails.
func TestRedactRenderSpillSpreadStops(t *testing.T) {
	t.Parallel()
	const w = 3 * redactRenderSpillSpread
	sets := &redactPixelSets{w: w, h: 1, spill: make([]bool, w)}
	sets.spill[0] = true
	before, after := make([]uint8, 3*w), make([]uint8, 3*w)
	for i := range before {
		before[i], after[i] = 200, 100
	}
	spread := sets.spreadSpill(before, after)
	for x, v := range spread {
		if want := x <= redactRenderSpillSpread; v != want {
			t.Errorf("pixel %d: spill %v, want %v", x, v, want)
		}
	}
	// A pixel drawn lighter after is no blank's.
	after[3*1] = 250
	if sets.spreadSpill(before, after)[1] {
		t.Error("a lighter pixel joined the spill")
	}
}
