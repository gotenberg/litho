package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"image"
	"image/color"
	"image/draw"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/pdf"
)

// lithoCorpusPDFs is the tracked litho corpus, which the determinism tests
// read from the repository.
const lithoCorpusPDFs = "../../corpus/litho/pdfs"

// writeDeterminismPDF writes the plain two-page mutation document as a litho
// fixture named name and returns its path.
func writeDeterminismPDF(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, mutationTestPDF(t), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// runDeterminism runs f on fixtureID of path and returns its verdict.
func runDeterminism(t *testing.T, f *determinismFeature, path, fixtureID string) Verdict {
	t.Helper()
	out, err := f.Run(context.Background(), path, fixtureID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	return f.Compare(fixtureID, out)
}

// TestDeterminismFeature_CountsARunThatDiffers pins the cell's contract: an
// operation that writes the same bytes twice matches with a zero metric, and
// one that writes different bytes counts one nondeterministic cell and names
// the operation.
func TestDeterminismFeature_CountsARunThatDiffers(t *testing.T) {
	t.Parallel()

	path := writeDeterminismPDF(t, "plain_2b.pdf")
	f := newDeterminismFeature()
	f.ops = []determinismOp{
		{name: "steady", run: func(_ context.Context, data []byte, _ string) ([]byte, error) { return data, nil }},
		{name: "random", run: func(_ context.Context, _ []byte, _ string) ([]byte, error) { return []byte(rand.Text()), nil }},
	}
	fixtures := []Fixture{
		{ID: "PDF_A-1b/plain_2b", PDFPath: path},
		{ID: "PDF_A-2b/plain_2b", PDFPath: path},
		{ID: "PDF_UA-1/plain_2b", PDFPath: path},
	}
	if err := f.Prepare(context.Background(), fixtures); err != nil {
		t.Fatal(err)
	}

	steady := runDeterminism(t, f, path, fixtures[0].ID)
	if !steady.Match || steady.Metrics[metricNondeterministic] != 0 {
		t.Errorf("steady operation: %+v, want a match with a zero metric", steady)
	}
	random := runDeterminism(t, f, path, fixtures[1].ID)
	if random.Match || random.Metrics[metricNondeterministic] != 1 || !strings.HasPrefix(random.Reason, "random:") {
		t.Errorf("random operation: %+v, want one nondeterministic cell naming the operation", random)
	}
	if spare := runDeterminism(t, f, path, fixtures[2].ID); !spare.Skipped {
		t.Errorf("fixture beyond the operations: %+v, want skipped", spare)
	}
}

// TestDeterminismFeature_RealOperationsAgree runs every operation of the cell
// on a plain document and expects each to write the same bytes twice.
func TestDeterminismFeature_RealOperationsAgree(t *testing.T) {
	t.Parallel()

	path := writeDeterminismPDF(t, "plain_2b.pdf")
	f := newDeterminismFeature()
	var fixtures []Fixture
	for i := range f.ops {
		fixtures = append(fixtures, Fixture{ID: "P" + string(rune('a'+i)) + "/plain_2b", PDFPath: path})
	}
	if err := f.Prepare(context.Background(), fixtures); err != nil {
		t.Fatal(err)
	}
	for i, fx := range fixtures {
		v := runDeterminism(t, f, path, fx.ID)
		if !v.Match || v.Skipped {
			t.Errorf("%s: %+v, want a match", f.ops[i].name, v)
		}
	}
}

// TestDeterminismFeature_LeavesOutTheRandomMutations pins the operation list:
// both remediation modes and every mutation but the two whose cryptography
// draws random bytes.
func TestDeterminismFeature_LeavesOutTheRandomMutations(t *testing.T) {
	t.Parallel()

	var names []string
	for _, op := range newDeterminismFeature().ops {
		names = append(names, op.name)
	}
	got := strings.Join(names, ",")
	for _, want := range []string{"remediation", "remediation-aggressive", "mutate-stamp", "mutate-pages", "mutate-autotag", "mutate-flatten", "mutate-redact", "generate-bookmarks", "split-reconstruct", "flatten-transparency", "render"} {
		if !strings.Contains(","+got+",", ","+want+",") {
			t.Errorf("operations %s lack %s", got, want)
		}
	}
	for _, unwanted := range []string{"mutate-sign", "mutate-encrypt"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("operations %s hold %s, whose output is random by design", got, unwanted)
		}
	}
}

// TestDeterminismFeature_BookmarkOperationsComeLast pins that the bookmark
// generations take the fixture slots after every operation before them, and
// transparency flattening the slot after theirs, then the render pass, so
// adding each moved no fixture's assignment, and that every operation runs at
// least twice.
func TestDeterminismFeature_BookmarkOperationsComeLast(t *testing.T) {
	t.Parallel()

	ops := newDeterminismFeature().ops
	if len(ops) < 4 {
		t.Fatalf("%d operations, want at least the two bookmark ones, flattening and rendering", len(ops))
	}
	var last []string
	for _, op := range ops[len(ops)-4:] {
		last = append(last, op.name)
	}
	if got := strings.Join(last, ","); got != "generate-bookmarks,split-reconstruct,flatten-transparency,render" {
		t.Errorf("last operations %s, want generate-bookmarks,split-reconstruct,flatten-transparency,render", got)
	}
	for _, op := range ops {
		if op.runs < 2 {
			t.Errorf("%s runs %d times, want at least 2", op.name, op.runs)
		}
	}
}

// TestDeterminismFeature_RunsEveryTimeItSays pins that a cell runs its
// operation op.runs times and fails on a run past the second that differs.
func TestDeterminismFeature_RunsEveryTimeItSays(t *testing.T) {
	t.Parallel()

	path := writeDeterminismPDF(t, "plain_2b.pdf")
	f := newDeterminismFeature()
	calls := 0
	f.ops = []determinismOp{{name: "third-differs", runs: 3, run: func(_ context.Context, data []byte, _ string) ([]byte, error) {
		calls++
		if calls == 3 {
			return []byte("other"), nil
		}
		return data, nil
	}}}
	v := runDeterminism(t, f, path, "PDF_A-2b/plain_2b")
	if calls != 3 {
		t.Errorf("ran %d times, want 3", calls)
	}
	if v.Match || v.Metrics[metricNondeterministic] != 1 {
		t.Errorf("verdict %+v, want one nondeterministic cell", v)
	}
}

// TestDeterminismFeature_SizeTieFixtureReachesGeneration pins that
// sizetie_2b.pdf, the fixture whose two font sizes tie, gives both bookmark
// generations a heading to find, so a tie broken at random changes their
// output and the cell sees it.
func TestDeterminismFeature_SizeTieFixtureReachesGeneration(t *testing.T) {
	t.Parallel()

	data, err := os.ReadFile(filepath.Join(lithoCorpusPDFs, "sizetie_2b.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	for name, run := range map[string]func(context.Context, []byte) ([]byte, error){
		"generate-bookmarks": generateBookmarksForDeterminism,
		"split-reconstruct":  splitReconstructForDeterminism,
	} {
		out, err := run(context.Background(), data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		r, err := pdf.Open(context.Background(), bytes.NewReader(out), int64(len(out)))
		if err != nil {
			t.Fatalf("%s: reopening: %v", name, err)
		}
		bms, err := r.Bookmarks(context.Background())
		r.Close()
		if err != nil {
			t.Fatalf("%s: reading the outline: %v", name, err)
		}
		if len(bms) != 1 || bms[0].Title != "AABB" {
			t.Errorf("%s: outline %+v, want the one heading AABB", name, bms)
		}
	}
}

// TestLithoHomeProfile_CoversEveryFixture pins that every litho fixture maps
// to a home profile, so none skips the cell unnoticed.
func TestLithoHomeProfile_CoversEveryFixture(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(lithoCorpusPDFs)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		base, ok := strings.CutSuffix(e.Name(), ".pdf")
		if !ok {
			continue
		}
		n++
		home, ok := lithoHomeProfile(base)
		if !ok {
			t.Errorf("%s: no home profile", e.Name())
			continue
		}
		if _, isA := dirToPDFAProfile[home]; !isA {
			if _, isUA := dirToPDFUAProfile[home]; !isUA {
				t.Errorf("%s: home profile %s has no remediation", e.Name(), home)
			}
		}
	}
	if n == 0 {
		t.Fatal("found no litho fixture")
	}
}

// TestDeterminismMetricIsACostMetric pins that a rise in nondeterministic
// cells is a regression.
func TestDeterminismMetricIsACostMetric(t *testing.T) {
	t.Parallel()

	if !costMetrics[metricNondeterministic] {
		t.Error("nondeterministic is not a cost metric")
	}
	raw, err := json.Marshal(determinismOutput{Op: "x", Same: true})
	if err != nil {
		t.Fatal(err)
	}
	if v := newDeterminismFeature().Compare("id", raw); len(v.Metrics) != 1 {
		t.Errorf("a matching cell emits %v, want the metric at zero", v.Metrics)
	}
	if !costMetrics[metricRenderPagesNondeterministic] {
		t.Error("render_pages_nondeterministic is not a cost metric")
	}
	if costMetrics[metricRenderPagesChecked] || uncomparedMetrics[metricRenderPagesChecked] {
		t.Error("render_pages_checked, the render pass's coverage, is not gated as good-when-higher")
	}
}

// renderOnlyDeterminism returns a determinism feature loaded the way a
// manifest mapping it to goldens/render loads it, so it runs the render pass
// alone, with render as its page renderer.
func renderOnlyDeterminism(t *testing.T, render pageRenderer) *determinismFeature {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "goldens", determinismRenderOnlyDir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	f := newDeterminismFeature()
	f.render = render
	if err := f.LoadGoldens(dir); err != nil {
		t.Fatal(err)
	}
	return f
}

// TestDeterminismFeature_RenderPassCatchesANondeterministicRender plants a
// renderer that draws page 1 one pixel apart in the second run, and expects
// the render cell to count it, page and all, where the real renderer matches.
func TestDeterminismFeature_RenderPassCatchesANondeterministicRender(t *testing.T) {
	t.Parallel()

	path := writeDeterminismPDF(t, "plain.pdf")

	steady := runDeterminism(t, renderOnlyDeterminism(t, lithoRenderPage), path, "plain")
	if !steady.Match || steady.Metrics[metricNondeterministic] != 0 ||
		steady.Metrics[metricRenderPagesChecked] != 2 || steady.Metrics[metricRenderPagesNondeterministic] != 0 {
		t.Errorf("real renderer: %+v, want a match over 2 pages with zero metrics", steady)
	}

	calls := 0
	planted := func(ctx context.Context, r *pdf.Reader, page int, dpi float64, opts ...pdf.RenderOption) (image.Image, error) {
		img, err := lithoRenderPage(ctx, r, page, dpi, opts...)
		if err != nil {
			return nil, err
		}
		calls++
		// The first run draws pages 1 and 2, the second run's page 1 is call 3.
		if calls != 3 {
			return img, nil
		}
		out := image.NewRGBA(img.Bounds())
		draw.Draw(out, out.Bounds(), img, img.Bounds().Min, draw.Src)
		b := out.Bounds()
		c := out.RGBAAt(b.Min.X, b.Min.Y)
		out.SetRGBA(b.Min.X, b.Min.Y, color.RGBA{R: ^c.R, G: c.G, B: c.B, A: 0xff})
		return out, nil
	}
	v := runDeterminism(t, renderOnlyDeterminism(t, planted), path, "plain")
	if calls != 2*determinismRenderRuns {
		t.Errorf("drew %d pages, want 2 pages in each of %d runs", calls, determinismRenderRuns)
	}
	if v.Match || v.Metrics[metricNondeterministic] != 1 ||
		v.Metrics[metricRenderPagesChecked] != 2 || v.Metrics[metricRenderPagesNondeterministic] != 1 {
		t.Errorf("planted renderer: %+v, want one nondeterministic cell, 1 of 2 pages", v)
	}
	if !strings.HasPrefix(v.Reason, determinismRenderOp+":") {
		t.Errorf("reason %q does not name the render pass", v.Reason)
	}
}

// TestDeterminismFeature_RenderPassSkipsAPDFThatDoesNotOpen pins that a file
// litho cannot open, the same way twice, skips rather than scoring a match
// over no page.
func TestDeterminismFeature_RenderPassSkipsAPDFThatDoesNotOpen(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "broken.pdf")
	if err := os.WriteFile(path, []byte("not a pdf"), 0o600); err != nil {
		t.Fatal(err)
	}
	if v := runDeterminism(t, renderOnlyDeterminism(t, lithoRenderPage), path, "broken"); !v.Skipped {
		t.Errorf("unopenable file: %+v, want skipped", v)
	}
}

// TestDeterminismFeature_GoldenDirectoryPicksTheOperations pins that a
// goldens/render directory keeps the render pass alone and that any other
// directory keeps every operation, both drawing with the fonts of the
// corpus's goldens/render/fonts.
func TestDeterminismFeature_GoldenDirectoryPicksTheOperations(t *testing.T) {
	t.Parallel()

	root := filepath.Join(t.TempDir(), "goldens")
	fonts := filepath.Join(root, determinismRenderOnlyDir, renderFontsDir)
	if err := os.MkdirAll(fonts, 0o750); err != nil {
		t.Fatal(err)
	}

	full := newDeterminismFeature()
	n := len(full.ops)
	if err := full.LoadGoldens(filepath.Join(root, "compliance")); err != nil {
		t.Fatal(err)
	}
	if len(full.ops) != n || full.fontsDir != fonts {
		t.Errorf("goldens/compliance: %d operations and fonts %q, want %d and %q", len(full.ops), full.fontsDir, n, fonts)
	}

	only := newDeterminismFeature()
	if err := only.LoadGoldens(filepath.Join(root, determinismRenderOnlyDir)); err != nil {
		t.Fatal(err)
	}
	if len(only.ops) != 1 || only.ops[0].name != determinismRenderOp || only.fontsDir != fonts {
		t.Errorf("goldens/render: operations %+v and fonts %q, want the render pass alone and %q", only.ops, only.fontsDir, fonts)
	}
}
