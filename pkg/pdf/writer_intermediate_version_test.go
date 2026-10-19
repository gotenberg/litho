package pdf

import (
	"bytes"
	"context"
	"testing"
)

// TestWithVersionReachesTheOutputOfAnIntermediate pins that [WithVersion]
// reaches the output when the Writer edits a file litho wrote for it, a
// [Pipeline] intermediate or the output of a remediation that kept a later
// round, as the compaction options do.
//
// WithVersion sets the header of a full rewrite, and the Writer over such a
// file appended an incremental update to it, which keeps the header the file
// was written with: the requested version was dropped with only a Debug line.
// No caller holds the bytes of that file, so rewriting it loses nothing.
func TestWithVersionReachesTheOutputOfAnIntermediate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := threeTextPagesPDF()
	if bytes.HasPrefix(data, []byte("%PDF-2.0")) {
		t.Fatalf("precondition: the fixture already declares %q", pdfHeader(data))
	}

	for _, tc := range []struct {
		name  string
		write func(t *testing.T, r *Reader, dst *bytes.Buffer, opts ...WriteOption)
	}{
		{
			name: "pipeline split, then rotate",
			write: func(t *testing.T, r *Reader, dst *bytes.Buffer, opts ...WriteOption) {
				t.Helper()
				p := NewPipeline().Open(r).Split([]int{1, 3}).Apply(func(_ context.Context, w *Writer) error {
					return w.RotatePages(90)
				})
				if _, err := p.WriteTo(ctx, dst, opts...); err != nil {
					t.Fatalf("Pipeline.WriteTo: %v", err)
				}
			},
		},
		{
			name: "pipeline merge, then nothing",
			write: func(t *testing.T, r *Reader, dst *bytes.Buffer, opts ...WriteOption) {
				t.Helper()
				p := NewPipeline().Open(r).Merge([]*Reader{r}).Apply(func(context.Context, *Writer) error {
					return nil
				})
				if _, err := p.WriteTo(ctx, dst, opts...); err != nil {
					t.Fatalf("Pipeline.WriteTo: %v", err)
				}
			},
		},
		{
			name: "remediation that kept a later round",
			write: func(t *testing.T, r *Reader, dst *bytes.Buffer, opts ...WriteOption) {
				t.Helper()
				steps := []roundsStep{{keys: []string{"PageLayout"}}, {keys: []string{"PageMode"}}, {keys: []string{"Lang"}}}
				e := &roundsEngine{steps: steps}
				w := NewWriter(r)
				defer w.Close()
				if _, err := w.remediate(ctx, engineTarget, DefaultRegressionTolerance, e.revalidate, e.run); err != nil {
					t.Fatalf("remediate: %v", err)
				}
				if w.basePath == "" {
					t.Fatal("precondition: the remediation kept no later round")
				}
				if err := w.WriteTo(ctx, dst, opts...); err != nil {
					t.Fatalf("WriteTo: %v", err)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			run := func(opts ...WriteOption) []byte {
				r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
				if err != nil {
					t.Fatalf("Open: %v", err)
				}
				defer r.Close()
				var out bytes.Buffer
				tc.write(t, r, &out, opts...)
				return out.Bytes()
			}

			if plain := run(); bytes.HasPrefix(plain, []byte("%PDF-2.0")) {
				t.Fatalf("precondition: the output declares %q without WithVersion", pdfHeader(plain))
			}
			out := run(WithVersion("2.0"))
			if !bytes.HasPrefix(out, []byte("%PDF-2.0")) {
				t.Errorf("the output declares %q, want %%PDF-2.0", pdfHeader(out))
			}
			got, err := Open(ctx, bytes.NewReader(out), int64(len(out)))
			if err != nil {
				t.Fatalf("reopening the output: %v", err)
			}
			defer got.Close()
			if v := got.Version(); v != "2.0" {
				t.Errorf("the output reads as version %q, want 2.0", v)
			}
		})
	}
}

// TestWithVersionKeepsTheCallersSourceIncremental is the control: on the
// caller's own file WithVersion still applies to a full rewrite only, and an
// edit that leaves an incremental update keeps the source's bytes.
func TestWithVersionKeepsTheCallersSourceIncremental(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := threeTextPagesPDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()
	if err := w.RotatePages(90); err != nil {
		t.Fatalf("RotatePages: %v", err)
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out, WithVersion("2.0")); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if !bytes.HasPrefix(out.Bytes(), data) {
		t.Error("WithVersion rewrote the caller's source rather than appending an update")
	}
}
