package pdf

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/remediation"
)

// errEngineFailed is the error the failing engines of
// [TestWriterRemediationTempFilesAreReleased] return.
var errEngineFailed = errors.New("the engine failed")

// remediateAtBoundary runs [Writer.remediate] behind the panic boundary the
// public remediation methods install, so a panic inside it comes back as
// [ErrInternalPanic] as it does to a caller.
func remediateAtBoundary(ctx context.Context, w *Writer, revalidate func(context.Context, *Reader) ([]error, error), run remediationRun) (rep *RemediationReport, err error) {
	defer recoverToError(nil, "remediate", &err)
	return w.remediate(ctx, engineTarget, DefaultRegressionTolerance, revalidate, run)
}

// panicOnCall returns revalidate, which then panics on its nth call, as a
// validation that meets a construct it does not expect while an output is
// judged.
func panicOnCall(n int, revalidate func(context.Context, *Reader) ([]error, error)) func(context.Context, *Reader) ([]error, error) {
	calls := 0
	return func(ctx context.Context, r *Reader) ([]error, error) {
		calls++
		if calls == n {
			panic("revalidation panicked")
		}
		return revalidate(ctx, r)
	}
}

// TestWriterRemediationTempFilesAreReleased counts the litho temp files a
// remediation leaves in a private os.TempDir on every way out of it: once it
// returns, and once the Writer is closed. A run that fails, is cancelled or
// panics owes the caller no output, so it leaves no file even before Close; a
// run that succeeds leaves its output alone, until WriteTo or Close removes
// it.
//
// A panic unwound through a remediation past every temp file it held, the
// output being judged, the rounds' outputs and the edited document being
// written alike, and the panic boundary of the public method turned it into
// an error: each such run left a litho-remediate-*.pdf in os.TempDir for good.
func TestWriterRemediationTempFilesAreReleased(t *testing.T) {
	twoRounds := func() *roundsEngine {
		return &roundsEngine{steps: []roundsStep{{keys: []string{"PageLayout"}}, {keys: []string{"PageMode"}}}}
	}
	for _, tc := range []struct {
		name string
		// setup returns the engine and the revalidation the run uses, and
		// may queue an edit on w or cancel the run through cancel.
		setup func(w *Writer, cancel func()) (remediationRun, func(context.Context, *Reader) ([]error, error))
		// wantErr is what the run returns, nil for a success.
		wantErr error
		// wantKept is the number of files a successful run leaves for
		// WriteTo.
		wantKept int
	}{
		{
			name: "a success",
			setup: func(*Writer, func()) (remediationRun, func(context.Context, *Reader) ([]error, error)) {
				e := twoRounds()
				return e.run, e.revalidate
			},
			wantKept: 1,
		},
		{
			name: "a success after an edit",
			setup: func(w *Writer, _ func()) (remediationRun, func(context.Context, *Reader) ([]error, error)) {
				if err := w.RotatePages(90); err != nil {
					t.Fatalf("RotatePages: %v", err)
				}
				e := &roundsEngine{steps: []roundsStep{{keys: []string{"PageLayout"}}}}
				return e.run, e.revalidate
			},
			// The output, and the edited document the Writer now edits,
			// which Close removes.
			wantKept: 2,
		},
		{
			name: "a success in a later round after an edit",
			setup: func(w *Writer, _ func()) (remediationRun, func(context.Context, *Reader) ([]error, error)) {
				if err := w.RotatePages(90); err != nil {
					t.Fatalf("RotatePages: %v", err)
				}
				e := twoRounds()
				return e.run, e.revalidate
			},
			// The Writer edits the later round's output, which replaces the
			// edited document.
			wantKept: 1,
		},
		{
			name: "a success after a rollback",
			setup: func(*Writer, func()) (remediationRun, func(context.Context, *Reader) ([]error, error)) {
				return emptyingEngine{snapshot: true}.run, cancelOnCall(0, nil)
			},
			wantKept: 1,
		},
		{
			name: "a fixer error in the first round",
			setup: func(*Writer, func()) (remediationRun, func(context.Context, *Reader) ([]error, error)) {
				return func(context.Context, *writer.ObjAllocator, *document.Document, []string) (*remediation.Report, error) {
					return nil, errEngineFailed
				}, cancelOnCall(0, nil)
			},
			wantErr: errEngineFailed,
		},
		{
			name: "a fixer error in a later round",
			setup: func(*Writer, func()) (remediationRun, func(context.Context, *Reader) ([]error, error)) {
				e := twoRounds()
				return func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, disabled []string) (*remediation.Report, error) {
					if len(e.disabled) == 1 {
						return nil, errEngineFailed
					}
					return e.run(ctx, alloc, doc, disabled)
				}, e.revalidate
			},
			// A round that fails is a round that fixed nothing: the first
			// round's output stands.
			wantKept: 1,
		},
		{
			name: "a regression that cannot be rolled back",
			setup: func(*Writer, func()) (remediationRun, func(context.Context, *Reader) ([]error, error)) {
				return emptyingEngine{}.run, cancelOnCall(0, nil)
			},
			wantErr: ErrRemediationBrokePDF,
		},
		{
			name: "a ctx cancelled before the run",
			setup: func(_ *Writer, cancel func()) (remediationRun, func(context.Context, *Reader) ([]error, error)) {
				cancel()
				e := twoRounds()
				return e.run, e.revalidate
			},
			wantErr: context.Canceled,
		},
		{
			name: "a ctx cancelled in a later round",
			setup: func(_ *Writer, cancel func()) (remediationRun, func(context.Context, *Reader) ([]error, error)) {
				e := twoRounds()
				return func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, disabled []string) (*remediation.Report, error) {
					if len(e.disabled) == 1 {
						cancel()
					}
					return e.run(ctx, alloc, doc, disabled)
				}, e.revalidate
			},
			wantErr: context.Canceled,
		},
		{
			name: "a panic while the first output is judged",
			setup: func(*Writer, func()) (remediationRun, func(context.Context, *Reader) ([]error, error)) {
				e := twoRounds()
				return e.run, panicOnCall(1, e.revalidate)
			},
			wantErr: ErrInternalPanic,
		},
		{
			name: "a panic while the rollback is judged",
			setup: func(*Writer, func()) (remediationRun, func(context.Context, *Reader) ([]error, error)) {
				return emptyingEngine{snapshot: true}.run, panicOnCall(2, cancelOnCall(0, nil))
			},
			wantErr: ErrInternalPanic,
		},
		{
			name: "a panic in the engine of a later round",
			setup: func(*Writer, func()) (remediationRun, func(context.Context, *Reader) ([]error, error)) {
				e := twoRounds()
				return func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, disabled []string) (*remediation.Report, error) {
					if len(e.disabled) == 1 {
						panic("the engine panicked")
					}
					return e.run(ctx, alloc, doc, disabled)
				}, e.revalidate
			},
			wantErr: ErrInternalPanic,
		},
		{
			name: "a panic while a later round's output is judged",
			setup: func(*Writer, func()) (remediationRun, func(context.Context, *Reader) ([]error, error)) {
				e := twoRounds()
				return e.run, panicOnCall(2, e.revalidate)
			},
			wantErr: ErrInternalPanic,
		},
		{
			name: "a panic while the edited document is written",
			setup: func(w *Writer, _ func()) (remediationRun, func(context.Context, *Reader) ([]error, error)) {
				w.mutations = append(w.mutations, mutation{apply: func(context.Context, *writer.ObjAllocator, *document.Document) error {
					panic("the edit panicked")
				}})
				e := twoRounds()
				return e.run, e.revalidate
			},
			wantErr: ErrInternalPanic,
		},
		{
			name: "a panic after an edit",
			setup: func(w *Writer, _ func()) (remediationRun, func(context.Context, *Reader) ([]error, error)) {
				if err := w.RotatePages(90); err != nil {
					t.Fatalf("RotatePages: %v", err)
				}
				e := twoRounds()
				return e.run, panicOnCall(1, e.revalidate)
			},
			wantErr: ErrInternalPanic,
			// The edited document, which the Writer edits after the panic
			// as before the run, until Close.
			wantKept: 1,
		},
	} {
		for _, release := range []string{"Close", "WriteTo"} {
			t.Run(tc.name+", then "+release, func(t *testing.T) {
				dir := isolateTempDir(t)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()

				data := threePagePDF()
				r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
				if err != nil {
					t.Fatalf("Open: %v", err)
				}
				defer r.Close()

				w := NewWriter(r)
				run, revalidate := tc.setup(w, cancel)
				_, err = remediateAtBoundary(ctx, w, revalidate, run)
				if tc.wantErr == nil && err != nil {
					t.Fatalf("remediate: %v", err)
				}
				if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
					t.Fatalf("remediate returned %v, want %v", err, tc.wantErr)
				}
				if got := lithoTempFiles(t, dir); len(got) != tc.wantKept {
					t.Errorf("the remediation left %q, want %d files", got, tc.wantKept)
				}

				if release == "WriteTo" && tc.wantErr == nil {
					if err := w.WriteTo(context.Background(), &bytes.Buffer{}); err != nil {
						t.Fatalf("WriteTo: %v", err)
					}
				}
				if err := w.Close(); err != nil {
					t.Fatalf("Close: %v", err)
				}
				if got := lithoTempFiles(t, dir); len(got) != 0 {
					t.Errorf("%s left %q behind", release, got)
				}
			})
		}
	}
}
