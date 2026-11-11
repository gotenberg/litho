package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/pdf"
)

// targetsSource is a two-page HTML source: a heading, a paragraph and a
// picture large enough that a 10-point shift moves more than the render
// rule's 1% of the page, then a paragraph on its own page. The picture has
// no alternate text, which PDF/UA requires (ISO 14289-1, 7.3).
const targetsSource = `<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Targets</title></head>
<body><h1>First page</h1><p>Some text on the first page.</p>
<img src="picture.png" style="display:block;width:450pt;height:400pt">
<p style="break-before:page">Second page text.</p></body></html>`

// targetsTestTargets are the targets the tests convert to: a version, a
// PDF/A profile the source meets and a PDF/UA profile it does not.
//
//nolint:gochecknoglobals // static test table
var targetsTestTargets = targetsByID("v1.4", "a2b", "ua1")

// targetsByID returns the targets of [outputTargets] with the given IDs, in
// the table's order, so a target added to the table moves no test.
func targetsByID(ids ...string) []outputTarget {
	var out []outputTarget
	for _, t := range outputTargets {
		if slices.Contains(ids, t.ID) {
			out = append(out, t)
		}
	}
	return out
}

// newTargetsSourceDir writes targetsSource and its picture into a fresh
// directory.
func newTargetsSourceDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(targetsSource), 0o600); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 90, 80))
	for y := range 80 {
		for x := range 90 {
			img.Set(x, y, color.RGBA{R: 40, G: 60, B: 90, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "picture.png"), buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// newTestTargets returns a targets feature over targetsTestTargets with the
// fixture fonts, its oracles off. It skips the test when the fonts are not
// fetched.
func newTestTargets(t *testing.T) *targetsFeature {
	t.Helper()
	fonts, err := fixtureFontDirs(fixtureFontsRoot(), diffFontsTarget)
	if err != nil {
		t.Skip(err)
	}
	f := newTargetsFeature()
	f.targets = targetsTestTargets
	f.fontDirs = fonts
	f.SetRunOptions(2, 0)
	return f
}

// runTargets prepares, runs and compares the one fixture of dir.
func runTargets(t *testing.T, f *targetsFeature, dir string) (Verdict, targetsOutput) {
	t.Helper()
	ctx := context.Background()
	if err := f.Prepare(ctx, []Fixture{{ID: "doc", PDFPath: dir}}); err != nil {
		t.Fatal(err)
	}
	data, err := f.Run(ctx, dir, "doc")
	if err != nil {
		t.Fatal(err)
	}
	var out targetsOutput
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return f.Compare("doc", data), out
}

// result returns the target's result in out.
func result(t *testing.T, out targetsOutput, id string) targetResult {
	t.Helper()
	for _, r := range out.Targets {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("no result for target %s in %+v", id, out)
	return targetResult{}
}

// hasReason reports whether one of r's reasons contains part.
func hasReason(r targetResult, part string) bool {
	return slices.ContainsFunc(r.Reasons, func(s string) bool { return strings.Contains(s, part) })
}

// plant wraps the real converter so the conversion to target id goes
// through edit and report, a defect planted in that target alone.
func plant(id string, edit func([]byte) []byte, report func(*pdf.ConvertReport)) targetConverter {
	return func(ctx context.Context, src []byte, s htmlSettings, t outputTarget) ([]byte, *pdf.ConvertReport, error) {
		if t.ID == id && edit != nil {
			src = edit(src)
		}
		data, rep, err := convertTarget(ctx, src, s, t)
		if err == nil && t.ID == id && report != nil {
			report(rep)
		}
		return data, rep, err
	}
}

// TestTargets_PassesFaithfulTargets checks the unplanted conversion: the
// version and the PDF/A target pass every check, and the PDF/UA target
// fails on the picture without alternate text, its report agreeing with
// the validator.
func TestTargets_PassesFaithfulTargets(t *testing.T) {
	f := newTestTargets(t)
	v, out := runTargets(t, f, newTargetsSourceDir(t))
	if out.Error != "" {
		t.Fatalf("base: %s", out.Error)
	}
	for _, id := range []string{"v1.4", "a2b"} {
		if r := result(t, out, id); !r.Pass {
			t.Errorf("%s: %v, want a pass", id, r.Reasons)
		}
		if v.Scores[id] != 1 || v.Metrics[metricTargetPassPrefix+id] != 1 {
			t.Errorf("%s: score %v, metric %d, want 1", id, v.Scores[id], v.Metrics[metricTargetPassPrefix+id])
		}
	}
	ua := result(t, out, "ua1")
	if ua.Pass || !hasReason(ua, "litho's validator finds PDF/UA-1 violations") || ua.ReportDisagrees {
		t.Errorf("ua1: %+v, want a failure on the validator with the report agreeing", ua)
	}
	if v.Match || v.Metrics[metricReportDisagrees] != 0 || v.Metrics[metricConvertError] != 0 {
		t.Errorf("verdict %+v, want unmatched on ua1 alone", v)
	}
}

// TestTargets_CatchesPlantedDefects plants three defects, each in one
// target, and checks the cell fails that target for its cause: a page's text
// dropped, a picture shifted by 10 points, and a report that claims a clean
// file while a violation stands.
func TestTargets_CatchesPlantedDefects(t *testing.T) {
	for name, tc := range map[string]struct {
		target   string
		convert  targetConverter
		reason   string
		disagree bool
	}{
		"text dropped": {
			target: "a2b",
			convert: plant("a2b", func(src []byte) []byte {
				return bytes.Replace(src, []byte("Second page text."), []byte("&#160;"), 1)
			}, nil),
			reason: "page 2's text differs",
		},
		"picture shifted": {
			target: "v1.4",
			convert: plant("v1.4", func(src []byte) []byte {
				return bytes.Replace(src, []byte("display:block;"), []byte("display:block;margin-left:10pt;"), 1)
			}, nil),
			reason: "page 1 draws apart from the base",
		},
		"report claims a clean file": {
			target: "ua1",
			convert: plant("ua1", nil, func(rep *pdf.ConvertReport) {
				rep.Conformance.Residual = nil
			}),
			reason:   "the conversion report says the file conforms",
			disagree: true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newTestTargets(t)
			f.convert = tc.convert
			v, out := runTargets(t, f, newTargetsSourceDir(t))
			r := result(t, out, tc.target)
			if r.Pass || !hasReason(r, tc.reason) {
				t.Errorf("%s: %+v, want a failure: %s", tc.target, r, tc.reason)
			}
			if v.Scores[tc.target] != 0 {
				t.Errorf("%s: score %v, want 0", tc.target, v.Scores[tc.target])
			}
			if got := v.Metrics[metricReportDisagrees]; got != boolToInt(tc.disagree) {
				t.Errorf("report_disagrees %d, want %d", got, boolToInt(tc.disagree))
			}
			for _, other := range []string{"v1.4", "a2b"} {
				if other != tc.target && !result(t, out, other).Pass {
					t.Errorf("%s fails though nothing was planted there: %v", other, result(t, out, other).Reasons)
				}
			}
		})
	}
}

// compliantVeraPDF stands in for veraPDF, finding every file of dir
// compliant.
func compliantVeraPDF(_ context.Context, dir, _ string) (map[string]veraVerdict, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	out := map[string]veraVerdict{}
	for _, e := range entries {
		out[strings.TrimSuffix(e.Name(), ".pdf")] = veraVerdict{}
	}
	return out, nil
}

// strictPdfcpu stands in for pdfcpu, accepting every file.
func strictPdfcpu(_ context.Context, _ string, files []string) (map[string]pdfcpuVerdict, string, error) {
	out := map[string]pdfcpuVerdict{}
	for _, f := range files {
		out[f] = pdfcpuVerdict{ok: true}
	}
	return out, "", nil
}

// TestTargets_Oracles checks the oracles' half on a sampled fixture: oracles
// that agree leave the version and PDF/A targets passing; a veraPDF that
// finds the PDF/UA file compliant disagrees with litho's validator; a
// poppler that draws a blank page disagrees with litho's drawing; and an
// oracle that does not run fails every target it would judge.
func TestTargets_Oracles(t *testing.T) {
	blank := func(_ context.Context, dir string, jobs []popplerJob) (string, error) {
		img := image.NewRGBA(image.Rect(0, 0, 595, 842))
		for i := range img.Pix {
			img.Pix[i] = 0xff
		}
		for _, j := range jobs {
			if err := writeImage(filepath.Join(dir, j.PNG+".png"), img); err != nil {
				return "", err
			}
		}
		return "", nil
	}
	unavailable := errors.New("not here")
	for name, tc := range map[string]struct {
		setup func(f *targetsFeature)
		check func(t *testing.T, v Verdict, out targetsOutput)
	}{
		"agreeing": {
			setup: func(*targetsFeature) {},
			check: func(t *testing.T, v Verdict, out targetsOutput) {
				for _, id := range []string{"v1.4", "a2b"} {
					if r := result(t, out, id); !r.Pass || !r.OracleChecked {
						t.Errorf("%s: %+v, want a pass the oracles checked", id, r)
					}
				}
				if ua := result(t, out, "ua1"); !ua.VeraPDFDisagrees || !hasReason(ua, "veraPDF ua1 finds no violation") {
					t.Errorf("ua1: %+v, want veraPDF to disagree", ua)
				}
				if v.Metrics[metricVeraPDFDisagrees] != 1 || v.Metrics[metricOracleTargets] != 3 || v.Metrics[metricRendererDisagrees] != 0 {
					t.Errorf("metrics %v", v.Metrics)
				}
			},
		},
		"poppler draws apart": {
			setup: func(f *targetsFeature) { f.poppler = blank },
			check: func(t *testing.T, v Verdict, out targetsOutput) {
				if r := result(t, out, "a2b"); r.Pass || !r.RendererDisagrees {
					t.Errorf("a2b: %+v, want poppler to disagree", r)
				}
				if v.Metrics[metricRendererDisagrees] != 3 {
					t.Errorf("renderer_disagrees %d, want 3", v.Metrics[metricRendererDisagrees])
				}
			},
		},
		"oracles missing": {
			setup: func(f *targetsFeature) {
				f.veraFound = func() error { return unavailable }
				f.poppler = func(context.Context, string, []popplerJob) (string, error) { return "", unavailable }
				f.pdfcpu = func(context.Context, string, []string) (map[string]pdfcpuVerdict, string, error) {
					return nil, "", unavailable
				}
			},
			check: func(t *testing.T, v Verdict, out targetsOutput) {
				for _, r := range out.Targets {
					if r.Pass {
						t.Errorf("%s passes with no oracle", r.ID)
					}
				}
				if v.Metrics[metricVeraPDFUnavailable] != 2 || v.Metrics[metricPopplerUnavailable] != 3 || v.Metrics[metricPdfcpuUnavailable] != 1 {
					t.Errorf("metrics %v, want veraPDF unavailable on 2 targets, poppler on 3, pdfcpu on 1", v.Metrics)
				}
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newTestTargets(t)
			f.SetRunOptions(2, 1)
			f.vera, f.veraFound = compliantVeraPDF, func() error { return nil }
			f.poppler, f.popplerPin = lithoAsPoppler, func() string { return "" }
			f.pdfcpu, f.pdfcpuPin = strictPdfcpu, func() string { return "" }
			tc.setup(f)
			v, out := runTargets(t, f, newTargetsSourceDir(t))
			if !out.Sampled {
				t.Fatal("the fixture is not sampled at one in one")
			}
			tc.check(t, v, out)
		})
	}
}

func TestOracleSampled(t *testing.T) {
	if oracleSampled("anything", 0) {
		t.Error("a sample of 0 takes a fixture")
	}
	if !oracleSampled("anything", 1) {
		t.Error("a sample of 1 leaves a fixture out")
	}
	n := 0
	for i := range 4096 {
		if oracleSampled(strings.Repeat("x", i%7)+string(rune('a'+i%26))+string(rune(i)), 8) {
			n++
		}
	}
	if n < 400 || n > 640 {
		t.Errorf("%d of 4096 sampled at one in eight", n)
	}
	if oracleSampled("a", 8) != oracleSampled("a", 8) {
		t.Error("the sample is not stable")
	}
}

func TestReadPdfcpuVerdict(t *testing.T) {
	for _, tc := range []struct {
		rc, answer string
		ok         bool
		message    string
	}{
		{"0", "validating(mode=strict) a.pdf ...\nvalidation ok\n", true, ""},
		{"1", "installing user font:\nvalidating(mode=strict) a.pdf ...\nvalidation error (obj#:27) (try --mode=relaxed): dict=pagesDict entry=Tabs: unsupported in version 1.4\n", false,
			"validation error (obj#:27) (try --mode=relaxed): dict=pagesDict entry=Tabs: unsupported in version 1.4"},
		{"1", "mode must be one of: r(elaxed), s(trict)", false, "mode must be one of: r(elaxed), s(trict)"},
		{"124", "", false, "exit status 124"},
	} {
		got := readPdfcpuVerdict(tc.rc, []byte(tc.answer))
		if got.ok != tc.ok || got.message != tc.message {
			t.Errorf("readPdfcpuVerdict(%s, %q) = %+v", tc.rc, tc.answer, got)
		}
	}
}

// TestTargets_CompareCountsEveryTarget checks that a base that does not
// convert scores every target 0 and counts one conversion error.
func TestTargets_CompareCountsEveryTarget(t *testing.T) {
	f := newTargetsFeature()
	v := f.Compare("doc", []byte(`{"error":"base conversion: refused"}`))
	if v.Match || v.Skipped || v.Metrics[metricConvertError] != 1 {
		t.Errorf("verdict %+v", v)
	}
	if len(v.Scores) != len(outputTargets) {
		t.Errorf("%d scores, want one per target (%d)", len(v.Scores), len(outputTargets))
	}
	for _, target := range outputTargets {
		if s, ok := v.Scores[target.ID]; !ok || s != 0 {
			t.Errorf("%s: score %v, %v", target.ID, s, ok)
		}
	}
}
