package pdf

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// errStopPipeline is the failure the stage under test returns.
var errStopPipeline = errors.New("stop the pipeline")

// openThreePages opens the three-page fixture for a pipeline test.
func openThreePages(t *testing.T) *Reader {
	t.Helper()

	data := threePagePDF()
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() {
		if closeErr := r.Close(); closeErr != nil {
			t.Logf("closing reader: %v", closeErr)
		}
	})
	return r
}

// TestPipelineRemovesUnwrittenRemediationOutput pins a pipeline that fails
// after a remediation stage. The remediation left its output on the stage
// Writer for the terminal write, the failure skipped that write, and nothing
// else removed the file. It also pins where the file goes: in the pipeline's
// temp directory, which [WithPipelineTempDir] names for the pipeline's own
// files and which a remediation stage ignored for its.
func TestPipelineRemovesUnwrittenRemediationOutput(t *testing.T) {
	sysTmp := isolateTempDir(t)
	pipeTmp := t.TempDir()

	var duringStage, duringStageSys []string
	_, err := NewPipeline().
		Open(openThreePages(t)).
		Apply(func(ctx context.Context, w *Writer) error {
			_, err := w.RemediatePDFA(ctx, models.PDFA2b)
			return err
		}).
		Apply(func(context.Context, *Writer) error {
			duringStage = lithoTempFiles(t, pipeTmp)
			duringStageSys = lithoTempFiles(t, sysTmp)
			return errStopPipeline
		}).
		WriteTo(context.Background(), &bytes.Buffer{}, WithPipelineTempDir(pipeTmp))
	if !errors.Is(err, errStopPipeline) {
		t.Fatalf("WriteTo returned %v, want the stage failure", err)
	}
	if len(duringStage) != 1 || len(duringStageSys) != 0 {
		t.Errorf("after the remediation stage the pipeline dir held %q and os.TempDir %q, want the output in the pipeline dir only",
			duringStage, duringStageSys)
	}
	if got := lithoTempFiles(t, pipeTmp); len(got) != 0 {
		t.Errorf("the failed pipeline left %q in its temp dir", got)
	}
	if got := lithoTempFiles(t, sysTmp); len(got) != 0 {
		t.Errorf("the failed pipeline left %q in os.TempDir", got)
	}
}

// TestPipelineRemovesReplacedIntermediates pins that an intermediate file goes
// once the next one replaces it as the current document, rather than when the
// pipeline ends: a long pipeline held a copy of the document per restructuring
// stage on disk at once.
func TestPipelineRemovesReplacedIntermediates(t *testing.T) {
	pipeTmp := t.TempDir()

	var duringStage []string
	var out bytes.Buffer
	_, err := NewPipeline().
		Open(openThreePages(t)).
		Apply(func(_ context.Context, w *Writer) error { return w.RotatePages(90) }).
		Split([]int{1, 2}).
		Apply(func(context.Context, *Writer) error {
			duringStage = lithoTempFiles(t, pipeTmp)
			return nil
		}).
		Split([]int{1}).
		WriteTo(context.Background(), &out, WithPipelineTempDir(pipeTmp))
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if len(duringStage) != 1 {
		t.Errorf("after the first split the pipeline dir held %q, want only the file the current document reads", duringStage)
	}
	if got := lithoTempFiles(t, pipeTmp); len(got) != 0 {
		t.Errorf("the pipeline left %q behind", got)
	}

	r, err := Open(context.Background(), bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	defer r.Close()
	if n, err := r.PageCount(context.Background()); err != nil || n != 1 {
		t.Errorf("the output has %d pages (%v), want 1", n, err)
	}
}
