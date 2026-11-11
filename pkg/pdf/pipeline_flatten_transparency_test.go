package pdf

import (
	"bytes"
	"context"
	"testing"
)

// flattenAt72 is a Pipeline.Apply stage that flattens at 72 dpi.
func flattenAt72(ctx context.Context, w *Writer) error {
	_, err := w.FlattenTransparency(ctx, WithFlattenDPI(72))
	return err
}

// TestPipeline_FlattenTransparencyWritesWhatTheWriterWrites pins that a
// pipeline stage running FlattenTransparency writes the bytes a Writer
// running it alone writes, on a page with transparency and on one with
// nothing to flatten, as an incremental update and as a full rewrite.
func TestPipeline_FlattenTransparencyWritesWhatTheWriterWrites(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"something to flatten", translucentCyanPDF()},
		{"nothing to flatten", untaggedHeadingPDF()},
	} {
		for _, opts := range [][]WriteOption{nil, {WithFullRewrite()}} {
			w := NewWriter(openColorPDF(t, tc.data))
			t.Cleanup(func() { _ = w.Close() })
			if err := flattenAt72(t.Context(), w); err != nil {
				t.Fatalf("%s: FlattenTransparency: %v", tc.name, err)
			}
			want := writeAll(t, w, opts...)
			got := runPipeline(t, NewPipeline().Open(openColorPDF(t, tc.data)).Apply(flattenAt72), opts...)
			if !bytes.Equal(got, want) {
				t.Errorf("%s, %d write options: the pipeline wrote %d bytes, the Writer %d, and they differ",
					tc.name, len(opts), len(got), len(want))
			}
		}
	}
}

// TestPipeline_FlattenTransparencyAfterAnEditKeepsTheDefaultProfile pins that
// a FlattenTransparency stage after another stage composites through the CMYK
// profile the caller opened the source with: the edit before it does not
// reopen the document under the built-in profile.
func TestPipeline_FlattenTransparencyAfterAnEditKeepsTheDefaultProfile(t *testing.T) {
	t.Parallel()

	setTitle := func(_ context.Context, w *Writer) error {
		w.SetMetadata(Metadata{Title: "T"})
		return nil
	}
	for _, tc := range []struct {
		name      string
		opts      []OpenOption
		wantWhite bool
	}{
		{"the built-in profile", nil, false},
		{"the caller's profile", []OpenOption{WithDefaultCMYKProfile(whiteProfile(t))}, true},
	} {
		r := openColorPDF(t, translucentCyanPDF(), tc.opts...)
		out := runPipeline(t, NewPipeline().Open(r).Apply(setTitle).Apply(flattenAt72))
		if got := renderCentre(t, out); (got == white) != tc.wantWhite {
			t.Errorf("%s: the flattened centre is %v, want white %t", tc.name, got, tc.wantWhite)
		}
	}
}
