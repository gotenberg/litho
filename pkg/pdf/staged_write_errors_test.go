package pdf

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestRedactReportsTheStagedWriteFailure pins that the pixel check of Redact,
// when the document it compares against cannot be written, reports why rather
// than its own failure. It reported a RedactionPixelError for page 0, which
// matches ErrRedactionPixelCheck and ErrRedactionRefused, so a write that
// failed on litho's side was classified as the caller's redaction.
func TestRedactReportsTheStagedWriteFailure(t *testing.T) {
	t.Parallel()

	w := NewWriter(openBytes(t, editTestPDF()))
	defer w.Close()
	if err := badStampPDF(t, w); err != nil {
		t.Fatalf("StampPDF: %v", err)
	}
	_, err := w.Redact(context.Background(), []RedactRegion{{Page: 1, Rect: [4]float64{0, 0, 100, 100}}})

	if _, ok := errors.AsType[*RedactionPixelError](err); ok {
		t.Errorf("Redact = %v, want no RedactionPixelError: the check never ran", err)
	}
	if errors.Is(err, ErrRedactionPixelCheck) || errors.Is(err, ErrRedactionRefused) {
		t.Errorf("Redact = %v, matches a redaction refusal", err)
	}
	if editErr, ok := errors.AsType[*EditError](err); !ok || editErr.Method != "StampPDF" {
		t.Errorf("Redact = %v, want the stamp's EditError", err)
	}
}

// TestStagedWriteNamesTheOperation pins the message of a failure to write the
// document an operation reads: it names that operation. Every one of them said
// "to remediate", a transparency flattening and a raster redaction included.
func TestStagedWriteNamesTheOperation(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		method string
		run    func(ctx context.Context, w *Writer) error
	}{
		{"FlattenTransparency", func(ctx context.Context, w *Writer) error {
			_, err := w.FlattenTransparency(ctx)
			return err
		}},
		{"Redact", func(ctx context.Context, w *Writer) error {
			_, err := w.Redact(ctx, []RedactRegion{{Page: 1, Rect: [4]float64{0, 0, 100, 100}}},
				WithRedactionRasterFallback(), WithoutRedactionPixelCheck())
			return err
		}},
	} {
		t.Run(tc.method, func(t *testing.T) {
			t.Parallel()

			w := NewWriter(openBytes(t, editTestPDF()))
			defer w.Close()
			if err := badStampPDF(t, w); err != nil {
				t.Fatalf("StampPDF: %v", err)
			}
			err := tc.run(context.Background(), w)
			if !errors.Is(err, ErrInvalidPageNumber) {
				t.Fatalf("%s = %v, want the stamp's ErrInvalidPageNumber", tc.method, err)
			}
			// The message is what is pinned here, not which error it is.
			if msg := err.Error(); !strings.Contains(msg, "Writer."+tc.method) || strings.Contains(msg, "remediate") {
				t.Errorf("%s = %q, want a message naming Writer.%s and no remediation", tc.method, msg, tc.method)
			}
		})
	}
}
