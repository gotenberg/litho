package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
)

// countingFeature counts its runs and calls onRun on each, so a test can cancel
// the run from inside it.
type countingFeature struct {
	runs  atomic.Int64
	onRun func()
}

func (f *countingFeature) Name() string               { return "counting" }
func (f *countingFeature) GoldenSource() string       { return "test" }
func (f *countingFeature) LoadGoldens(_ string) error { return nil }
func (f *countingFeature) HasGolden(_ string) bool    { return true }
func (f *countingFeature) Compare(id string, _ []byte) Verdict {
	return Verdict{FixtureID: id, Match: true}
}

func (f *countingFeature) Run(_ context.Context, _, _ string) ([]byte, error) {
	f.runs.Add(1)
	if f.onRun != nil {
		f.onRun()
	}
	return nil, nil
}

// stubPDFs writes n empty files, which detectClaimedProfile reads as claiming
// no profile.
func stubPDFs(t *testing.T, n int) []string {
	t.Helper()
	dir := t.TempDir()
	pdfs := make([]string, n)
	for i := range pdfs {
		pdfs[i] = filepath.Join(dir, strconv.Itoa(i)+".pdf")
		if err := os.WriteFile(pdfs[i], []byte{}, 0o600); err != nil {
			t.Fatalf("write %s: %v", pdfs[i], err)
		}
	}
	return pdfs
}

func cancelledContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestRunFeatureAcrossFixturesStopsWhenCancelled(t *testing.T) {
	// One worker, and the first run cancels: every fixture queued behind it
	// must drain without running, which is what makes Ctrl-C prompt.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	feat := &countingFeature{onRun: cancel}
	fixtures := make([]Fixture, 100)
	for i := range fixtures {
		fixtures[i] = Fixture{ID: strconv.Itoa(i), PDFPath: "unused.pdf"}
	}

	runFeatureAcrossFixtures(ctx, feat, fixtures, 1, &FeatureReport{})

	if got := feat.runs.Load(); got != 1 {
		t.Errorf("Run called %d times, want 1: the queue kept running after the cancel", got)
	}
}

func TestBuildScoreboardRefusesAnInterruptedRun(t *testing.T) {
	// A cancelled run scored part of the matrix at best, and baseline would
	// write it as the new floor. Whatever the testdata on this machine, the
	// interrupted run is no result.
	//
	// The corpus directory and versions.env resolve from the repository root,
	// and a feature no corpus runs keeps the run instant with or without the
	// testdata. The live run is the control: it returns ok, so the refusal
	// below comes from the cancel and not from a setup failure.
	t.Chdir(filepath.Join("..", "..", "..", ".."))
	opts := runOptions{corpora: []string{"litho"}, features: []string{"no-such-feature"}, workers: 1}
	if _, ok := buildScoreboard(context.Background(), opts); !ok {
		t.Fatal("buildScoreboard failed on a live context, so this test cannot see the cancel")
	}
	if board, ok := buildScoreboard(cancelledContext(), opts); ok {
		t.Errorf("buildScoreboard returned ok on a cancelled context: %+v", board)
	}
}

func TestLoadFixturesAutoStopsWhenCancelled(t *testing.T) {
	m := &Manifest{ProfileMode: "auto", Profiles: []string{"PDF_A-2b"}}
	got, err := loadFixturesAuto(cancelledContext(), m, stubPDFs(t, 50))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("loadFixturesAuto = %d fixtures, %v; want context.Canceled", len(got), err)
	}
}

func TestScanClaimsParallelStopsWhenCancelled(t *testing.T) {
	// The files left unscanned would read as claiming nothing, so a cut-short
	// scan returns an error rather than a short row set.
	rows, err := scanClaimsParallel(cancelledContext(), stubPDFs(t, 50), nil, 4)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("scanClaimsParallel = %d rows, %v; want context.Canceled", len(rows), err)
	}
}

func TestDispatchScanClaimsWritesNoCSVWhenInterrupted(t *testing.T) {
	// Through dispatch, so the test also pins that the command receives the
	// context main installs for Ctrl-C.
	pdfs := stubPDFs(t, 3)
	out := filepath.Join(t.TempDir(), "claims.csv")

	rc := dispatch(cancelledContext(), "scan-claims", []string{"--pdfs", filepath.Dir(pdfs[0]), "--out", out})
	if rc == 0 {
		t.Errorf("scan-claims exited 0 on an interrupted scan")
	}
	if _, err := os.Stat(out); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("an interrupted scan left %s behind (stat err %v): provisioning would read it as the claims", out, err)
	}
}
