package pdf

import (
	"bytes"
	"context"
	"testing"
)

// TestPipelineCompactsAnIntermediateTheLastStageEdits pins that the compaction
// options of [Pipeline.WriteTo] reach the output when a Split or a Merge is
// followed by a stage that leaves an incremental update, a signature among
// them.
//
// A Split or a Merge that is not the last stage writes an intermediate file,
// uncompacted since the options belong to the output, and the last stage's
// Writer edits that file. Signing it, or rotating a page of it, appended an
// incremental update, and an incremental update carries no compaction, so the
// pipeline returned an uncompacted output with no word said. The intermediate
// is the pipeline's own: no caller holds its bytes, so nothing is lost by
// rewriting it, and a signature goes on top of the compacted rewrite.
func TestPipelineCompactsAnIntermediateTheLastStageEdits(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := threeTextPagesPDF()

	for _, tc := range []struct {
		name   string
		build  func(p *Pipeline, r *Reader) *Pipeline
		signed bool
	}{
		{
			name: "split, then sign",
			build: func(p *Pipeline, _ *Reader) *Pipeline {
				return p.Split([]int{1, 3}).Apply(func(_ context.Context, w *Writer) error {
					w.Sign(newCMSTestSigner(t))
					return nil
				})
			},
			signed: true,
		},
		{
			name: "merge, then sign",
			build: func(p *Pipeline, r *Reader) *Pipeline {
				return p.Merge([]*Reader{r}).Apply(func(_ context.Context, w *Writer) error {
					w.Sign(newCMSTestSigner(t))
					return nil
				})
			},
			signed: true,
		},
		{
			name: "split, then rotate",
			build: func(p *Pipeline, _ *Reader) *Pipeline {
				return p.Split([]int{1, 3}).Apply(func(_ context.Context, w *Writer) error {
					return w.RotatePages(90)
				})
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			write := func(opts ...WriteOption) []byte {
				r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
				if err != nil {
					t.Fatalf("Open: %v", err)
				}
				defer r.Close()
				var buf bytes.Buffer
				if _, err := tc.build(NewPipeline().Open(r), r).WriteTo(ctx, &buf, opts...); err != nil {
					t.Fatalf("Pipeline.WriteTo: %v", err)
				}
				return buf.Bytes()
			}

			plain := write()
			if bytes.Contains(plain, []byte("/ObjStm")) {
				t.Fatal("precondition: the uncompacted pipeline packed objects")
			}
			packed := write(WithObjectStreams(), WithDedup())
			if !bytes.Contains(packed, []byte("/ObjStm")) {
				t.Error("the compaction options did not reach the output")
			}
			if tc.signed {
				if got := signatureCount(t, packed); got != 1 {
					t.Errorf("the compacted output holds %d signatures, want 1", got)
				}
				return
			}
			if again := write(WithObjectStreams(), WithDedup()); !bytes.Equal(again, packed) {
				t.Error("the compacted output differs between two runs")
			}
		})
	}
}

// TestPipelineSignsTheCallersSourceIncrementally is the control that keeps the
// rewrite to the pipeline's own intermediate files. A signature on the
// caller's document goes in an incremental update that keeps the source bytes
// (ISO 32000-1, 7.5.6), compaction or not: rewriting the source would drop
// every signature it already carries.
func TestPipelineSignsTheCallersSourceIncrementally(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := threeTextPagesPDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	var buf bytes.Buffer
	p := NewPipeline().Open(r).Apply(func(_ context.Context, w *Writer) error {
		w.Sign(newCMSTestSigner(t))
		return nil
	})
	if _, err := p.WriteTo(ctx, &buf, WithObjectStreams(), WithDedup()); err != nil {
		t.Fatalf("Pipeline.WriteTo: %v", err)
	}
	if !bytes.HasPrefix(buf.Bytes(), data) {
		t.Error("signing the caller's source rewrote it rather than appending an update")
	}
}
