package pdf

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// TestStampText_ExtractionHonoursTheArtifactMarking pins the contract change
// that came with marking stamps as artifacts, in both directions.
//
// A stamp is an /Artifact (ISO 32000-1, 14.8.2.2 names watermarks among them),
// which is what keeps a tagged document conformant with ISO 14289-1, 7.1 test
// 3 after stamping. The visible consequence is that [Reader.ExtractText] omits
// the stamped text by default, exactly as it omits any other artifact, and
// returns it only when [WithArtifacts] asks for it.
//
// Both halves matter. Asserting only that WithArtifacts finds the text would
// still pass if the marking were dropped, which is the change this guards: the
// four stamp tests that had to gain WithArtifacts would go green again on their
// own, and nothing else in the package would notice.
func TestStampText_ExtractionHonoursTheArtifactMarking(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := threePagePDF()

	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()

	w := NewWriter(r)
	if err := w.StampText(TextStamp{
		Text:     "WATERMARK",
		X:        100,
		Y:        400,
		FontSize: 24,
	}, 1); err != nil {
		t.Fatalf("StampText: %v", err)
	}

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}

	r2, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("Open stamped: %v", err)
	}
	defer r2.Close()

	plain, err := r2.ExtractText(ctx, 1)
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	// Precondition: the page's own text still extracts. Without it, a stamp
	// that vanished for some unrelated reason, or a page that never carried
	// text, would satisfy the assertion below for the wrong reason.
	if !strings.Contains(plain, "Page1") {
		t.Fatalf("precondition: the page's own text did not extract: %q", plain)
	}
	if strings.Contains(plain, "WATERMARK") {
		t.Errorf("a default extraction returned the stamp: it is not marked as an artifact: %q", plain)
	}

	withArtifacts, err := r2.ExtractText(ctx, 1, WithArtifacts())
	if err != nil {
		t.Fatalf("ExtractText WithArtifacts: %v", err)
	}
	if !strings.Contains(withArtifacts, "WATERMARK") {
		t.Errorf("WithArtifacts did not return the stamp text: %q", withArtifacts)
	}
}
