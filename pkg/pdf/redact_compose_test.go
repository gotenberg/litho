package pdf

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// allPagesText extracts the text of every page of out, hidden text included,
// so a run a later stage re-drew under a clip or in white still counts.
func allPagesText(t *testing.T, out []byte) string {
	t.Helper()
	ctx := context.Background()

	r, err := Open(ctx, bytes.NewReader(out), int64(len(out)))
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	pages, err := r.PageCount(ctx)
	if err != nil {
		t.Fatalf("PageCount: %v", err)
	}
	var b strings.Builder
	for page := 1; page <= pages; page++ {
		text, err := r.ExtractText(ctx, page, WithHiddenText())
		if err != nil {
			t.Fatalf("ExtractText page %d: %v", page, err)
		}
		b.WriteString(text)
		b.WriteByte('\n')
	}
	return b.String()
}

// assertRedacted fails when any of gone is still in out, as bytes or as
// extractable text, or when any of kept is missing from the text.
func assertRedacted(t *testing.T, out []byte, gone, kept []string) {
	t.Helper()
	text := allPagesText(t, out)
	for _, word := range gone {
		if bytes.Contains(out, []byte(word)) {
			t.Errorf("%q survives in the output bytes", word)
		}
		if strings.Contains(text, word) {
			t.Errorf("%q survives in the extracted text %q", word, text)
		}
	}
	for _, word := range kept {
		if !strings.Contains(text, word) {
			t.Errorf("%q, outside every region, is missing from %q", word, text)
		}
	}
}

// TestWriter_Redact_SurvivesEveryWriterStage pins that a redaction holds
// whatever else the same Writer does, before or after it.
//
// Each of these sequences wrote the redacted text back while every call
// reported success. Redact and remediation each started from an empty
// allocator and replaced the other's overrides, a remediation file written
// before Redact was streamed as is, and auto-tagging, queued or run as a
// remediation fixer, rebuilds the page from the source content stream. The
// assertions are on the output: what the file holds is the only thing a
// redaction promises.
func TestWriter_Redact_SurvivesEveryWriterStage(t *testing.T) {
	t.Parallel()

	redactSecret := func(t *testing.T, w *Writer) {
		t.Helper()
		if _, err := w.Redact(context.Background(), []RedactRegion{coversSecret}); err != nil {
			t.Fatalf("Redact: %v", err)
		}
	}
	remediateA := func(profile models.PDFAProfile) func(*testing.T, *Writer) {
		return func(t *testing.T, w *Writer) {
			t.Helper()
			if _, err := w.RemediatePDFA(context.Background(), profile); err != nil {
				t.Fatalf("RemediatePDFA(%s): %v", profile, err)
			}
		}
	}
	autoTag := func(_ *testing.T, w *Writer) { w.AutoTag() }
	remediateUA := func(t *testing.T, w *Writer) {
		t.Helper()
		if _, err := w.RemediatePDFUA(context.Background(), models.PDFUA1); err != nil {
			t.Fatalf("RemediatePDFUA: %v", err)
		}
	}

	for _, tc := range []struct {
		name  string
		steps []func(*testing.T, *Writer)
	}{
		{"AutoTag then Redact", []func(*testing.T, *Writer){autoTag, redactSecret}},
		{"Redact then AutoTag", []func(*testing.T, *Writer){redactSecret, autoTag}},
		{"Redact then RemediatePDFUA", []func(*testing.T, *Writer){redactSecret, remediateUA}},
		{"Redact then RemediatePDFA 2b", []func(*testing.T, *Writer){redactSecret, remediateA(models.PDFA2b)}},
		{"Redact then RemediatePDFA 2a", []func(*testing.T, *Writer){redactSecret, remediateA(models.PDFA2a)}},
		{"RemediatePDFA 2b then Redact", []func(*testing.T, *Writer){remediateA(models.PDFA2b), redactSecret}},
		{"RemediatePDFUA then Redact", []func(*testing.T, *Writer){remediateUA, redactSecret}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			src := redactableTextPDF("")
			r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			w := NewWriter(r)
			for _, step := range tc.steps {
				step(t, w)
			}
			var out bytes.Buffer
			if err := w.WriteTo(ctx, &out); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			assertRedacted(t, out.Bytes(), []string{"SECRET"}, []string{"KEEP"})
		})
	}
}

// TestWriter_Redact_SecondCallKeepsTheFirst pins that two Redact calls on one
// Writer both hold, on different pages and on the same page.
//
// Each call started from an empty allocator and replaced the overrides, so
// only the last call reached the output. On one page the second call also
// re-read the source content stream, restoring the text the first removed.
func TestWriter_Redact_SecondCallKeepsTheFirst(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		src    []byte
		first  RedactRegion
		second RedactRegion
		gone   []string
	}{
		{
			name:   "different pages",
			src:    twoPageRedactablePDF(),
			first:  RedactRegion{Page: 1, Rect: [4]float64{90, 690, 300, 730}},
			second: RedactRegion{Page: 2, Rect: [4]float64{90, 690, 300, 730}},
			gone:   []string{"ALPHA", "BETA"},
		},
		{
			name:   "same page",
			src:    redactableTextPDF(""),
			first:  coversSecret,
			second: RedactRegion{Page: 1, Rect: [4]float64{90, 590, 300, 630}},
			gone:   []string{"SECRET", "KEEP"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			r, err := Open(ctx, bytes.NewReader(tc.src), int64(len(tc.src)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			w := NewWriter(r)
			if _, err := w.Redact(ctx, []RedactRegion{tc.first}); err != nil {
				t.Fatalf("first Redact: %v", err)
			}
			if _, err := w.Redact(ctx, []RedactRegion{tc.second}); err != nil {
				t.Fatalf("second Redact: %v", err)
			}
			var out bytes.Buffer
			if err := w.WriteTo(ctx, &out); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			assertRedacted(t, out.Bytes(), tc.gone, nil)
		})
	}
}

// TestWriter_WriteTo_RefusesARedactionALaterStageUndid pins the fail-closed
// end of the write-time check. A later stage that puts removed text where no
// redaction pass reaches makes WriteTo refuse and write nothing, rather than
// report success over a file that still holds it.
func TestWriter_WriteTo_RefusesARedactionALaterStageUndid(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	src := redactableTextPDF("")
	r, err := Open(ctx, bytes.NewReader(src), int64(len(src)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	w := NewWriter(r)
	if _, err := w.Redact(ctx, []RedactRegion{coversSecret}); err != nil {
		t.Fatalf("Redact: %v", err)
	}
	// /Info is the home SanitizeDocument deliberately leaves to ClearMetadata,
	// which runs inside Redact and not after it.
	w.SetMetadata(Metadata{Title: "SECRET"})

	var out bytes.Buffer
	err = w.WriteTo(ctx, &out)
	if !errors.Is(err, ErrRedactionIncomplete) {
		t.Fatalf("WriteTo = %v, want ErrRedactionIncomplete", err)
	}
	if out.Len() != 0 {
		t.Fatalf("WriteTo refused but wrote %d bytes", out.Len())
	}
}
