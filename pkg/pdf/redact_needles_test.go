package pdf

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// TestWriter_Redact_IgnoresACopyTooShortToIdentify pins that a removed run
// too short to name anything is not looked for. The page loses a lone "A",
// and the catalog's private data spells an "A" of its own: that is not a copy
// of what was removed, and refusing over it refuses every real document, since
// a single character survives in nearly all of them.
func TestWriter_Redact_IgnoresACopyTooShortToIdentify(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	src := plantedPDF("BT /F1 12 Tf 100 700 Td (A) Tj ET BT /F1 12 Tf 100 600 Td (KEEP) Tj ET", plant{
		catalog: "/PieceInfo 12 0 R",
		objects: "12 0 obj\n<< /SomeApp << /Private (A note) >> >>\nendobj\n",
	})
	r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)
	report, err := w.Redact(ctx, []RedactRegion{coversSecret})
	if err != nil {
		t.Fatalf("Redact: %v", err)
	}
	if len(report.RemovedText) == 0 {
		t.Fatal("the region removed nothing, so the test proves nothing")
	}
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if text := allPagesText(t, out.Bytes()); strings.Contains(text, "A") || !strings.Contains(text, "KEEP") {
		t.Errorf("page text = %q, want KEEP without the removed A", text)
	}
}

// TestWriter_Redact_AShortRedactionHoldsThroughAutoTag pins that a redaction
// whose every run is too short to be looked for still holds when auto-tagging,
// the stage that marks up a page's content stream, runs after it. Nothing
// verifies such a run in the output, so the page edit has to carry it.
func TestWriter_Redact_AShortRedactionHoldsThroughAutoTag(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	src := plantedPDF("BT /F1 12 Tf 100 700 Td (QZ) Tj ET BT /F1 12 Tf 100 600 Td (KEEP) Tj ET", plant{})
	r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)
	if _, err := w.Redact(ctx, []RedactRegion{coversSecret}); err != nil {
		t.Fatalf("Redact: %v", err)
	}
	w.AutoTag()

	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	assertRedacted(t, out.Bytes(), []string{"QZ"}, []string{"KEEP"})
}
