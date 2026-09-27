package pdf

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// openTestWriter opens data and returns a Writer over it, both closed when the
// test ends.
func openTestWriter(t *testing.T, data []byte) *Writer {
	t.Helper()
	r, err := Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	w := NewWriter(r)
	t.Cleanup(func() { w.Close() })
	return w
}

// TestWriterAutoTagReportIsFilledWhenTheEditIsApplied pins the report's
// lifecycle: AutoTag queues the edit like every other, so the report says
// nothing until WriteTo applies it, and then that the document was tagged.
func TestWriterAutoTagReportIsFilledWhenTheEditIsApplied(t *testing.T) {
	t.Parallel()

	w := openTestWriter(t, untaggedHeadingPDF())
	report := w.AutoTag()
	if report == nil {
		t.Fatal("AutoTag returned no report")
	}
	if report.Applied || report.Skipped != nil {
		t.Fatalf("before WriteTo the report reads applied=%t skipped=%v, want neither", report.Applied, report.Skipped)
	}
	var out bytes.Buffer
	if err := w.WriteTo(context.Background(), &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if !report.Applied || report.Skipped != nil || report.Page != 0 {
		t.Errorf("after WriteTo the report reads applied=%t skipped=%v page=%d, want a tagged document", report.Applied, report.Skipped, report.Page)
	}
	if !bytes.Contains(out.Bytes(), []byte("/StructTreeRoot")) {
		t.Error("the report says tagged, but the output carries no structure tree")
	}
}

// TestWriterAutoTagReportNamesWhyItSkipped pins that each documented reason to
// leave a document alone reaches the caller as its sentinel, and that the
// output is then written all the same: a skip is not a failure.
func TestWriterAutoTagReportNamesWhyItSkipped(t *testing.T) {
	t.Parallel()

	emptyPage := buildPDF([]string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] >>",
	})
	for _, tc := range []struct {
		name string
		data []byte
		// before queues edits ahead of the AutoTag under test.
		before   func(t *testing.T, w *Writer)
		want     error
		wantPage int
	}{
		{
			name: "after a stamp, whose content is an artifact",
			data: untaggedHeadingPDF(),
			before: func(t *testing.T, w *Writer) {
				t.Helper()
				if err := w.StampText(TextStamp{Text: "DRAFT", X: 50, Y: 50}); err != nil {
					t.Fatalf("StampText: %v", err)
				}
			},
			want:     ErrAutoTagMarkedContent,
			wantPage: 1,
		},
		{
			name:   "after another AutoTag",
			data:   untaggedHeadingPDF(),
			before: func(_ *testing.T, w *Writer) { w.AutoTag() },
			want:   ErrAutoTagAlreadyTagged,
		},
		{
			name: "on a page that draws nothing",
			data: emptyPage,
			want: ErrAutoTagNothingToTag,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := openTestWriter(t, tc.data)
			if tc.before != nil {
				tc.before(t, w)
			}
			report := w.AutoTag()
			if err := w.WriteTo(context.Background(), io.Discard); err != nil {
				t.Fatalf("WriteTo: %v", err)
			}
			if !report.Applied {
				t.Fatal("WriteTo returned without applying AutoTag")
			}
			if !errors.Is(report.Skipped, tc.want) {
				t.Errorf("Skipped = %v, want %v", report.Skipped, tc.want)
			}
			if report.Page != tc.wantPage {
				t.Errorf("Page = %d, want %d", report.Page, tc.wantPage)
			}
		})
	}
}

// TestWriterAutoTagReportIsFilledByTheCallThatAppliesIt pins that a
// remediation or a redaction queued after AutoTag, which apply the edits
// queued before them, fill the report when they return, before any WriteTo.
func TestWriterAutoTagReportIsFilledByTheCallThatAppliesIt(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		after func(ctx context.Context, w *Writer) error
	}{
		{name: "a remediation", after: func(ctx context.Context, w *Writer) error {
			_, err := w.RemediatePDFUA(ctx, models.PDFUA1)
			return err
		}},
		{name: "a redaction", after: func(ctx context.Context, w *Writer) error {
			_, err := w.Redact(ctx, []RedactRegion{{Page: 1, Rect: [4]float64{60, 650, 560, 690}}})
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := openTestWriter(t, untaggedHeadingPDF())
			report := w.AutoTag()
			if err := tc.after(context.Background(), w); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if !report.Applied || report.Skipped != nil {
				t.Errorf("after %s the report reads applied=%t skipped=%v, want a tagged document", tc.name, report.Applied, report.Skipped)
			}
		})
	}
}

// TestWriterAutoTagAfterARemediationReportsNothing pins that a refused
// AutoTag still returns a report, one WriteTo never fills: the refusal itself
// comes back from WriteTo.
func TestWriterAutoTagAfterARemediationReportsNothing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	w := openTestWriter(t, untaggedHeadingPDF())
	if _, err := w.RemediatePDFA(ctx, models.PDFA2b); err != nil {
		t.Fatalf("RemediatePDFA: %v", err)
	}
	report := w.AutoTag()
	if report == nil {
		t.Fatal("a refused AutoTag returned no report")
	}
	if err := w.WriteTo(ctx, io.Discard); !errors.Is(err, ErrEditAfterRemediation) {
		t.Fatalf("WriteTo = %v, want ErrEditAfterRemediation", err)
	}
	if report.Applied || report.Skipped != nil {
		t.Errorf("the refused AutoTag reports applied=%t skipped=%v, want neither", report.Applied, report.Skipped)
	}
}
