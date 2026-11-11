package pdf

import (
	"bytes"
	"context"
	"testing"
)

// TestPipeline_OpenAloneWritesWhatTheWriterWrites pins that a pipeline of
// Open alone, written in full with every compaction pass, writes the bytes a
// Writer with no edit writes with the same options.
func TestPipeline_OpenAloneWritesWhatTheWriterWrites(t *testing.T) {
	t.Parallel()

	data, _ := gradientImagePDF()
	opts := []WriteOption{WithFullRewrite(), WithObjectStreams(), WithDedup(), WithImageRecompression(), WithFontSubsetting()}
	w := NewWriter(openColorPDF(t, data))
	t.Cleanup(func() { _ = w.Close() })
	want := writeAll(t, w, opts...)
	got := runPipeline(t, NewPipeline().Open(openColorPDF(t, data)), opts...)
	if !bytes.Equal(got, want) {
		t.Errorf("the pipeline wrote %d bytes, the Writer %d, and they differ", len(got), len(want))
	}
	if len(want) >= len(data) {
		t.Errorf("the rewrite is %d bytes, the source %d: the uncompressed image was not recompressed", len(want), len(data))
	}
}

// TestPipeline_FullRewriteAfterARotationPacksTheOutput pins that a rotation
// stage, which alone appends an incremental update with no object stream,
// is packed when the pipeline is written in full with object streams.
func TestPipeline_FullRewriteAfterARotationPacksTheOutput(t *testing.T) {
	t.Parallel()

	rotate := func(_ context.Context, w *Writer) error { return w.RotatePages(90) }
	build := func() *Pipeline { return NewPipeline().Open(openColorPDF(t, threeTextPagesPDF())).Apply(rotate) }
	if bytes.Contains(runPipeline(t, build(), WithObjectStreams()), []byte(objStmMarker)) {
		t.Fatal("precondition: the incremental update carries an object stream")
	}
	if !bytes.Contains(runPipeline(t, build(), WithFullRewrite(), WithObjectStreams()), []byte(objStmMarker)) {
		t.Error("the full rewrite after the rotation carries no object stream")
	}
}
