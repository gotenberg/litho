package pdf

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

// TestStampPDF_DrawsASourceOpenedWithItsPassword pins that StampPDF takes a
// stamp document opened with [WithPassword]: the overlay is written, the
// target keeps its page count, and the stamp's text is there as an artifact.
// A wrong password is refused when the stamp is opened, before StampPDF.
func TestStampPDF_DrawsASourceOpenedWithItsPassword(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	enc := encryptBytes(t, untaggedHeadingPDF(), "s3cret")
	if _, err := Open(ctx, bytes.NewReader(enc), int64(len(enc)), WithPassword("wrong")); !errors.Is(err, ErrInvalidPassword) {
		t.Fatalf("opening the stamp with a wrong password: %v, want %v", err, ErrInvalidPassword)
	}

	w := NewWriter(openColorPDF(t, threePagePDF()))
	t.Cleanup(func() { _ = w.Close() })
	if err := w.StampPDF(PDFStamp{Source: openColorPDF(t, enc, WithPassword("s3cret")), X: 50, Y: 50}, 1); err != nil {
		t.Fatalf("StampPDF: %v", err)
	}
	out := openColorPDF(t, writeAll(t, w))
	if n, err := out.PageCount(ctx); err != nil || n != 3 {
		t.Errorf("PageCount = %d, %v; want 3", n, err)
	}
	text, err := out.ExtractText(ctx, 1, WithArtifacts())
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if !strings.Contains(text, "Document Heading") {
		t.Errorf("page 1 text %q lacks the encrypted stamp's heading", text)
	}
}
