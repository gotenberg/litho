package pdf

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// emptyCorner is a region in the bottom-right corner of a US Letter page, away
// from the text [signablePDF] and [laterRoundPDF] draw at y=700, the rectangle
// [laterRoundPDF] fills from (20, 20) to (120, 120), and its annotation.
var emptyCorner = []RedactRegion{{Page: 1, Rect: [4]float64{540, 0, 590, 50}}}

// TestRedactSelectingNothingAfterStagedEdits pins that a redaction selecting
// nothing leaves the Writer as it was when other edits are already staged.
//
// A redaction that removes nothing returns early, so it does not clear the
// metadata or force a rewrite. It told that apart by an empty pending map, and
// the edits staged before it are pending too: after a remediation that kept
// one round, it went on to clear the metadata, and the output lost the XMP
// packet ISO 19005-2, 6.6.2.1 requires. A remediation is now the last edit,
// so a redaction after one is refused, even one that would select nothing,
// and the remediated output is never written without it.
func TestRedactSelectingNothingAfterStagedEdits(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	t.Run("after a remediation", func(t *testing.T) {
		t.Parallel()

		data := signablePDF()
		r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		defer r.Close()
		w := NewWriter(r)
		defer w.Close()
		report, err := w.RemediatePDFA(ctx, models.PDFA2b)
		if err != nil {
			t.Fatalf("RemediatePDFA: %v", err)
		}
		if len(report.Fixed) == 0 {
			t.Fatal("the remediation fixed nothing, so the test proves nothing")
		}
		if _, err := w.Redact(ctx, emptyCorner); !errors.Is(err, ErrEditAfterRemediation) {
			t.Fatalf("Redact after a remediation returned %v, want ErrEditAfterRemediation", err)
		}

		var out bytes.Buffer
		if err := w.WriteTo(ctx, &out); !errors.Is(err, ErrEditAfterRemediation) {
			t.Fatalf("WriteTo returned %v, want the recorded ErrEditAfterRemediation", err)
		}
		if out.Len() != 0 {
			t.Errorf("WriteTo wrote %d bytes after the refusal", out.Len())
		}
	})

	t.Run("after a rotation", func(t *testing.T) {
		t.Parallel()

		data := laterRoundPDF(true)
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
		if _, err := w.Redact(ctx, emptyCorner); err != nil {
			t.Fatalf("Redact: %v", err)
		}

		var out bytes.Buffer
		if err := w.WriteTo(ctx, &out); err != nil {
			t.Fatalf("WriteTo: %v", err)
		}
		got, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
		if err != nil {
			t.Fatalf("reopening the output: %v", err)
		}
		defer got.Close()
		meta, err := got.Metadata(ctx)
		if err != nil {
			t.Fatalf("Metadata: %v", err)
		}
		if meta["Title"] != "Fixture" || meta["Author"] != "litho" {
			t.Errorf("the output carries Title %q and Author %q, want the source's %q and %q",
				meta["Title"], meta["Author"], "Fixture", "litho")
		}
		if rot := rotationsOf(t, out.Bytes()); !slices.Equal(rot, []int{90}) {
			t.Errorf("the output pages have rotations %v, want [90]", rot)
		}
	})
}
