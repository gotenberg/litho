package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
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
	for _, want := range []string{"remediation", "remediation-aggressive", "mutate-stamp", "mutate-pages", "mutate-autotag", "mutate-flatten", "mutate-redact", "generate-bookmarks", "split-reconstruct"} {
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
// generations take the fixture slots after every other operation, so adding
// them moved no fixture's assignment, and that every operation runs at least
// twice.
func TestDeterminismFeature_BookmarkOperationsComeLast(t *testing.T) {
	t.Parallel()

	ops := newDeterminismFeature().ops
	if len(ops) < 2 {
		t.Fatalf("%d operations, want at least the two bookmark ones", len(ops))
	}
	if got := ops[len(ops)-2].name + "," + ops[len(ops)-1].name; got != "generate-bookmarks,split-reconstruct" {
		t.Errorf("last operations %s, want generate-bookmarks,split-reconstruct", got)
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
}
