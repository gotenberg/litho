package pdf

import (
	"bytes"
	"context"
	"io"
	"testing"
)

// TestWithCompactionSkippedReportsAnIncrementalUpdate pins what
// [WithCompactionSkipped] reports: true when a write asked for compaction and
// appended an incremental update, which carries none, false when the write
// rewrote the file or asked for no compaction.
func TestWithCompactionSkippedReportsAnIncrementalUpdate(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := threeTextPagesPDF()
	rotate := func(_ context.Context, w *Writer) error { return w.RotatePages(90) }

	for _, tc := range []struct {
		name  string
		write func(r *Reader, dst io.Writer, opts ...WriteOption) error
		opts  []WriteOption
		want  bool
	}{
		{
			name: "Writer, incremental, compaction asked",
			write: func(r *Reader, dst io.Writer, opts ...WriteOption) error {
				w := NewWriter(r)
				defer w.Close()
				if err := w.RotatePages(90); err != nil {
					return err
				}
				return w.WriteTo(ctx, dst, opts...)
			},
			opts: []WriteOption{WithDedup()},
			want: true,
		},
		{
			name: "Writer, incremental, no compaction asked",
			write: func(r *Reader, dst io.Writer, opts ...WriteOption) error {
				w := NewWriter(r)
				defer w.Close()
				if err := w.RotatePages(90); err != nil {
					return err
				}
				return w.WriteTo(ctx, dst, opts...)
			},
		},
		{
			name: "Writer, full rewrite",
			write: func(r *Reader, dst io.Writer, opts ...WriteOption) error {
				w := NewWriter(r)
				defer w.Close()
				return w.WriteTo(ctx, dst, append(opts, WithFullRewrite())...)
			},
			opts: []WriteOption{WithObjectStreams()},
		},
		{
			name: "Pipeline, incremental",
			write: func(r *Reader, dst io.Writer, opts ...WriteOption) error {
				_, err := NewPipeline().Open(r).Apply(rotate).WriteTo(ctx, dst, opts...)
				return err
			},
			opts: []WriteOption{WithImageRecompression()},
			want: true,
		},
		{
			name: "Pipeline, split then rotate",
			write: func(r *Reader, dst io.Writer, opts ...WriteOption) error {
				_, err := NewPipeline().Open(r).Split([]int{1}).Apply(rotate).WriteTo(ctx, dst, opts...)
				return err
			},
			opts: []WriteOption{WithObjectStreams()},
		},
		{
			name: "Split",
			write: func(r *Reader, dst io.Writer, opts ...WriteOption) error {
				return Split(ctx, r, []int{1}, dst, opts...)
			},
			opts: []WriteOption{WithObjectStreams()},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()
			// Seeded with the opposite of the answer, so a write that never
			// stores it fails.
			got := !tc.want
			var out bytes.Buffer
			if err := tc.write(r, &out, append(tc.opts, WithCompactionSkipped(&got))...); err != nil {
				t.Fatalf("write: %v", err)
			}
			if got != tc.want {
				t.Errorf("WithCompactionSkipped stored %v, want %v", got, tc.want)
			}
		})
	}
}
