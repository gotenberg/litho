package pdf

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/core/writer"
	"github.com/gotenberg/litho/pkg/internal/remediation"
	"github.com/gotenberg/litho/pkg/models"
)

// TestWriterRemediationFailsWithoutATempFile pins what a remediation does when
// it cannot write its output to a temp file: it fails with
// [ErrRemediationUnverified] and leaves the Writer as it was. It returned the
// report the engine drew up before any output existed, as a success: the
// residual was never revalidated and the regression gates never ran, yet
// nothing told the caller.
func TestWriterRemediationFailsWithoutATempFile(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		staged bool
	}{
		{name: "nothing staged"},
		// A staged edit sends the integrity baseline through a temp file too,
		// and a baseline that could not be measured switched the regression
		// gates off.
		{name: "an edit staged before", staged: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			data := threePagePDF()
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()

			w := NewWriter(r, WithTempDir(filepath.Join(t.TempDir(), "missing")))
			defer w.Close()
			if tc.staged {
				if err := w.RotatePages(90); err != nil {
					t.Fatalf("RotatePages: %v", err)
				}
			}

			report, err := w.RemediatePDFA(ctx, models.PDFA2b)
			if !errors.Is(err, ErrRemediationUnverified) {
				t.Fatalf("RemediatePDFA returned %v with %v, want ErrRemediationUnverified", report, err)
			}
			if !errors.Is(err, os.ErrNotExist) {
				t.Errorf("the error %v does not carry the file-system failure", err)
			}
			if len(w.rewriteOverrides) != 0 || w.forceRewrite || w.trailerDirty {
				t.Error("the failed remediation left repairs queued on the Writer")
			}
		})
	}
}

// TestPipelineRemediationFailsWithoutATempFile pins the same through a
// pipeline, whose remediation stages write their temp files where
// [WithPipelineTempDir] says.
func TestPipelineRemediationFailsWithoutATempFile(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer
	_, err := NewPipeline().
		Open(openThreePages(t)).
		Apply(func(ctx context.Context, w *Writer) error {
			_, err := w.RemediatePDFA(ctx, models.PDFA2b)
			return err
		}).
		WriteTo(context.Background(), &out, WithPipelineTempDir(filepath.Join(t.TempDir(), "missing")))
	if !errors.Is(err, ErrRemediationUnverified) {
		t.Fatalf("the pipeline wrote %d bytes and returned %v, want ErrRemediationUnverified", out.Len(), err)
	}
}

// TestWriterRemediationLaterRoundFailsWithoutATempFile pins a temp file that
// fails in a later round: the remediation fails rather than end early on the
// earlier round's output, since the failure says nothing about the document.
func TestWriterRemediationLaterRoundFailsWithoutATempFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := threePagePDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	dir := filepath.Join(t.TempDir(), "tmp")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	e := &roundsEngine{steps: []roundsStep{{keys: []string{"PageLayout"}}, {keys: []string{"PageMode"}}}}
	run := func(ctx context.Context, alloc *writer.ObjAllocator, doc *document.Document, disabled []string) (*remediation.Report, error) {
		// The second round runs on the first round's output, which the
		// engine reads through an open file, and then needs a temp file of
		// its own: the directory is gone by then, as a disk that fills up
		// mid-remediation.
		if len(e.disabled) == 1 {
			if err := os.RemoveAll(dir); err != nil {
				return nil, err
			}
		}
		return e.run(ctx, alloc, doc, disabled)
	}

	w := NewWriter(r, WithTempDir(dir))
	defer w.Close()
	if _, err := w.remediate(ctx, DefaultRegressionTolerance, e.revalidate, run); !errors.Is(err, ErrRemediationUnverified) {
		t.Fatalf("remediate returned %v, want ErrRemediationUnverified", err)
	}
	if len(e.disabled) != 2 {
		t.Fatalf("the engine ran %d rounds, want the failure in the second", len(e.disabled))
	}
	if len(w.rewriteOverrides) != 0 || w.preSerializedPath != "" {
		t.Error("the failed remediation left its first round queued on the Writer")
	}
}

// TestWriterTempDirPlacesTheRemediationOutput pins [WithTempDir] on a
// standalone Writer: the output a remediation writes to revalidate goes to the
// directory it names and not to [os.TempDir].
func TestWriterTempDirPlacesTheRemediationOutput(t *testing.T) {
	sysTmp := isolateTempDir(t)
	dir := t.TempDir()

	ctx := context.Background()
	data := threePagePDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	w := NewWriter(r, WithTempDir(dir))
	defer w.Close()
	if _, err := w.RemediatePDFA(ctx, models.PDFA2b); err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}
	if got, gotSys := lithoTempFiles(t, dir), lithoTempFiles(t, sysTmp); len(got) != 1 || len(gotSys) != 0 {
		t.Errorf("after the remediation the named dir holds %q and os.TempDir %q, want the output in the named dir only", got, gotSys)
	}
	if err := w.WriteTo(ctx, &bytes.Buffer{}); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if got := lithoTempFiles(t, dir); len(got) != 0 {
		t.Errorf("WriteTo left %q behind", got)
	}
}
