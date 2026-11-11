package pdf

import (
	"context"
	"strings"
	"testing"
)

// TestPipeline_StampPDFOverlaysTheStampAsAnArtifact pins that a StampPDF stage
// draws the stamp page over the target as an artifact (ISO 32000-1, 14.8.2.2):
// ExtractText finds the stamp's heading only with [WithArtifacts], and the
// page count does not change. The stamp and the target are the same page, so
// the heading shows once without the artifacts and twice with them.
func TestPipeline_StampPDFOverlaysTheStampAsAnArtifact(t *testing.T) {
	t.Parallel()

	const heading = "Document Heading"
	ctx := t.Context()
	stamp := openColorPDF(t, untaggedHeadingPDF())
	data := runPipeline(t, NewPipeline().Open(openColorPDF(t, untaggedHeadingPDF())).
		Apply(func(_ context.Context, w *Writer) error {
			return w.StampPDF(PDFStamp{Source: stamp, X: 50, Y: 50})
		}))

	out := openColorPDF(t, data)
	if n, err := out.PageCount(ctx); err != nil || n != 1 {
		t.Errorf("PageCount = %d, %v; want 1: StampPDF overlays, it adds no page", n, err)
	}
	for _, tc := range []struct {
		name string
		opts []ExtractTextOption
		want int
	}{
		{"without the artifacts", nil, 1},
		{"with the artifacts", []ExtractTextOption{WithArtifacts()}, 2},
	} {
		text, err := out.ExtractText(ctx, 1, tc.opts...)
		if err != nil {
			t.Fatalf("%s: ExtractText: %v", tc.name, err)
		}
		if got := strings.Count(text, heading); got != tc.want {
			t.Errorf("%s: the text holds %q %d times, want %d", tc.name, heading, got, tc.want)
		}
	}
}
