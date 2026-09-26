package pdf

import (
	"bytes"
	"context"
	"errors"
	"maps"
	"os"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/remediation"
	"github.com/gotenberg/litho/pkg/models"
)

// isolateTempDir points [os.TempDir] at a fresh directory for the rest of the
// test and returns it, so the test can count the temp files litho leaves.
// The test must not be parallel.
func isolateTempDir(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(key, dir)
	}
	if got := os.TempDir(); got != dir {
		t.Skipf("os.TempDir() = %q ignores the environment on this platform", got)
	}
	return dir
}

// lithoTempFiles returns the names of the litho temp files in dir.
func lithoTempFiles(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "litho-") {
			names = append(names, e.Name())
		}
	}
	return names
}

// remediatedWriter opens the three-page fixture and remediates it for
// PDF/A-2b, which always has something to repair there.
func remediatedWriter(t *testing.T) *Writer {
	t.Helper()

	ctx := context.Background()
	data := threePagePDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := r.Close(); closeErr != nil {
			t.Logf("closing reader: %v", closeErr)
		}
	})
	w := NewWriter(r)
	report, err := w.RemediatePDFA(ctx, models.PDFA2b)
	if err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}
	if len(report.Fixed) == 0 {
		t.Fatal("the remediation fixed nothing, so it proves nothing about its temp file")
	}
	return w
}

// TestWriterCloseRemovesRemediationOutput pins the release path of a Writer
// that is remediated and never written. The remediation leaves its output in
// a temp file for WriteTo to stream, and nothing removed it when WriteTo never
// ran, so every such Writer leaked a litho-remediate-*.pdf into os.TempDir.
func TestWriterCloseRemovesRemediationOutput(t *testing.T) {
	dir := isolateTempDir(t)
	w := remediatedWriter(t)

	if got := lithoTempFiles(t, dir); len(got) != 1 {
		t.Fatalf("after remediation the temp dir holds %q, want the one output", got)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := lithoTempFiles(t, dir); len(got) != 0 {
		t.Errorf("Close left %q behind", got)
	}
	if err := w.Close(); err != nil {
		t.Errorf("a second Close returned %v, want nil", err)
	}

	ctx := context.Background()
	if err := w.WriteTo(ctx, &bytes.Buffer{}); !errors.Is(err, ErrWriterClosed) {
		t.Errorf("WriteTo after Close returned %v, want ErrWriterClosed", err)
	}
	if _, err := w.RemediatePDFA(ctx, models.PDFA2b); !errors.Is(err, ErrWriterClosed) {
		t.Errorf("RemediatePDFA after Close returned %v, want ErrWriterClosed", err)
	}
	if _, err := w.Redact(ctx, []RedactRegion{{Page: 1, Rect: [4]float64{0, 0, 10, 10}}}); !errors.Is(err, ErrWriterClosed) {
		t.Errorf("Redact after Close returned %v, want ErrWriterClosed", err)
	}
	if got := lithoTempFiles(t, dir); len(got) != 0 {
		t.Errorf("a refused call after Close left %q behind", got)
	}
}

// TestWriterWriteToRemovesRemediationOutput pins the common flow: WriteTo
// streams the remediation output and removes it, and a Close deferred after
// it has nothing left to do.
func TestWriterWriteToRemovesRemediationOutput(t *testing.T) {
	dir := isolateTempDir(t)
	w := remediatedWriter(t)

	var out bytes.Buffer
	if err := w.WriteTo(context.Background(), &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if out.Len() == 0 {
		t.Fatal("WriteTo wrote nothing")
	}
	if got := lithoTempFiles(t, dir); len(got) != 0 {
		t.Errorf("WriteTo left %q behind", got)
	}
	if err := w.Close(); err != nil {
		t.Errorf("Close after WriteTo returned %v, want nil", err)
	}
}

// TestWriterStaleRemediationOutputIsRemoved pins the stale path: an edit
// queued after the remediation makes WriteTo serialize afresh, and the file
// it no longer streams goes all the same.
func TestWriterStaleRemediationOutputIsRemoved(t *testing.T) {
	dir := isolateTempDir(t)
	w := remediatedWriter(t)

	if err := w.RotatePages(90); err != nil {
		t.Fatalf("RotatePages: %v", err)
	}
	if err := w.WriteTo(context.Background(), &bytes.Buffer{}); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if got := lithoTempFiles(t, dir); len(got) != 0 {
		t.Errorf("WriteTo left %q behind", got)
	}
}

// emptyingEngine is a remediation run that sets a catalog key, a structural
// repair, and then empties every page's content stream, a content-tier repair
// the integrity probe refuses for the text it drops. With snapshot false it
// reports no content snapshot, so the writer cannot roll the repair back and
// the remediation fails. cancel, when set, runs after the repairs, as a caller
// giving up midway.
type emptyingEngine struct {
	snapshot bool
	cancel   func()
}

// run is the [remediationRun] of the engine.
func (e emptyingEngine) run(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, _ []string) (*remediation.Report, error) {
	root, ok := doc.Index.Trailer["Root"].(ast.PdfIndirectRef)
	if !ok {
		return nil, errors.New("no catalog reference")
	}
	catalog, err := document.ResolveDict(ctx, doc, root)
	if err != nil {
		return nil, err
	}
	next := maps.Clone(catalog)
	next["PageLayout"] = ast.PdfName("SinglePage")
	alloc.AllocAt(root.ObjectNum, next)
	rep := &remediation.Report{Fixed: []string{"set /PageLayout"}}

	if e.snapshot {
		snap := alloc.Snapshot()
		rep.ContentSnapshot = &snap
	}
	cat, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		return nil, err
	}
	pages, err := document.ResolvePages(ctx, doc, cat)
	if err != nil {
		return nil, err
	}
	for _, p := range pages {
		if ref, ok := p.Dict["Contents"].(ast.PdfIndirectRef); ok {
			alloc.AllocAt(ref.ObjectNum, ast.NewStream(ast.PdfDict{}, nil))
		}
	}
	rep.Fixed = append(rep.Fixed, "emptied the page content")
	if e.snapshot {
		rep.ContentFixed = []string{"emptied the page content"}
		rep.ContentFixers = []string{"empty-content"}
	}
	if e.cancel != nil {
		e.cancel()
	}
	return rep, nil
}

// cancelOnCall returns a revalidation that finds nothing and runs cancel on
// its nth call, as a caller giving up while that output is judged. n = 0
// never cancels.
func cancelOnCall(n int, cancel func()) func(context.Context, *Reader) ([]error, error) {
	calls := 0
	return func(context.Context, *Reader) ([]error, error) {
		calls++
		if calls == n {
			cancel()
		}
		return nil, nil
	}
}

// TestWriterFailedRemediationLeavesNoTempFile pins the error paths: a
// remediation the integrity gates refuse, and one whose caller cancels it,
// remove every temp file they wrote, since the caller has no output to write
// and may not think to Close, and put the Writer back as it was. A cancelled
// remediation reports the cancellation: it used to report the output it could
// not finish judging as a broken PDF.
func TestWriterFailedRemediationLeavesNoTempFile(t *testing.T) {
	for _, tc := range []struct {
		name       string
		engine     emptyingEngine
		cancelRun  bool
		cancelCall int
		wantErr    error
	}{
		{name: "a regression that cannot be rolled back", wantErr: ErrRemediationBrokePDF},
		{name: "a cancellation before the output is judged", cancelRun: true, wantErr: context.Canceled},
		{name: "a cancellation while the output is judged", engine: emptyingEngine{snapshot: true}, cancelCall: 1, wantErr: context.Canceled},
		{name: "a cancellation while the rollback is judged", engine: emptyingEngine{snapshot: true}, cancelCall: 2, wantErr: context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := isolateTempDir(t)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			data := threePagePDF()
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()

			w := NewWriter(r)
			e := tc.engine
			if tc.cancelRun {
				e.cancel = cancel
			}
			_, err = w.remediate(ctx, DefaultRegressionTolerance, cancelOnCall(tc.cancelCall, cancel), e.run)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("remediate returned %v, want %v", err, tc.wantErr)
			}
			if got := lithoTempFiles(t, dir); len(got) != 0 {
				t.Errorf("the failed remediation left %q behind", got)
			}
			if len(w.rewriteOverrides) != 0 {
				t.Errorf("the failed remediation left %d overrides queued", len(w.rewriteOverrides))
			}
		})
	}
}

// TestWriterRemediationRoundCancellationLeavesNoTempFile pins the rounds: a
// cancellation inside a later round ended the loop as a round that fixed
// nothing does, and the remediation returned the earlier round's output as a
// success the caller had given up on.
func TestWriterRemediationRoundCancellationLeavesNoTempFile(t *testing.T) {
	dir := isolateTempDir(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	data := threePagePDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	e := &roundsEngine{steps: []roundsStep{{keys: []string{"PageLayout"}}, {keys: []string{"PageMode"}}}}
	run := func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, disabled []string) (*remediation.Report, error) {
		if len(e.disabled) == 1 {
			cancel()
		}
		return e.run(ctx, alloc, doc, disabled)
	}
	w := NewWriter(r)
	if _, err := w.remediate(ctx, DefaultRegressionTolerance, e.revalidate, run); !errors.Is(err, context.Canceled) {
		t.Fatalf("remediate returned %v, want context.Canceled", err)
	}
	if len(e.disabled) != 2 {
		t.Fatalf("the engine ran %d rounds, want the cancellation inside the second", len(e.disabled))
	}
	if got := lithoTempFiles(t, dir); len(got) != 0 {
		t.Errorf("the cancelled remediation left %q behind", got)
	}
}

// TestWriterSecondRemediationSupersedesTheFirst pins a Writer remediated
// twice. The first run's output stayed on the Writer for WriteTo to stream,
// and WriteTo judged it fresh by the queue length alone, which a remediation
// does not change: when the first run repaired nothing, the second run's
// probe and the final WriteTo both streamed the first run's file, and the
// second run's repairs never reached the output. The first file also leaked.
func TestWriterSecondRemediationSupersedesTheFirst(t *testing.T) {
	dir := isolateTempDir(t)
	ctx := context.Background()

	data := threePagePDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	w := NewWriter(r)
	defer w.Close()
	first := &roundsEngine{}
	if _, err := w.remediate(ctx, DefaultRegressionTolerance, first.revalidate, first.run); err != nil {
		t.Fatalf("first remediate: %v", err)
	}
	second := &roundsEngine{steps: []roundsStep{{keys: []string{"PageLayout"}}}}
	report, err := w.remediate(ctx, DefaultRegressionTolerance, second.revalidate, second.run)
	if err != nil {
		t.Fatalf("second remediate: %v", err)
	}
	if len(report.Residual) != 0 {
		t.Errorf("the second remediation judged an output without its repair: %v", report.Residual)
	}
	if got := lithoTempFiles(t, dir); len(got) != 1 {
		t.Errorf("after two remediations the temp dir holds %q, want the second output only", got)
	}

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	defer r2.Close()
	if res, err := second.revalidate(ctx, r2); err != nil || len(res) != 0 {
		t.Errorf("the output misses the second remediation's repair: %v %v", res, err)
	}
}
