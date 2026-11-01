package pdf

import (
	"context"
	"errors"
	"io"
	"testing"
)

// TestStampInputErrorsAreClassified pins the sentinels of a stamp the caller
// got wrong, each returned when the stamp is queued, before anything is read
// from the document: text the standard fonts cannot encode, and image data
// that does not read as its type. They used to surface at write time as plain
// errors, which a service answered as a server error.
func TestStampInputErrorsAreClassified(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		queue func(w *Writer) error
		want  error
	}{
		{"Japanese text in a standard font", func(w *Writer) error {
			return w.StampText(TextStamp{Text: "日本語"})
		}, ErrTextNotEncodable},
		{"a JPEG that is not one", func(w *Writer) error {
			return w.StampImage(ImageStamp{Data: []byte("not a jpeg"), MimeType: "image/jpeg"})
		}, ErrInvalidImage},
		{"a PNG that is not one", func(w *Writer) error {
			return w.StampImage(ImageStamp{Data: []byte("not an image"), MimeType: "image/png"})
		}, ErrInvalidImage},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			w := NewWriter(openBytes(t, editTestPDF()))
			defer w.Close()
			err := tc.queue(w)
			if !errors.Is(err, tc.want) || !IsInvalidInput(err) {
				t.Fatalf("queueing = %v, want %v, classified as invalid input", err, tc.want)
			}
			if len(w.mutations) != 0 {
				t.Errorf("a refused stamp queued %d edits", len(w.mutations))
			}
		})
	}
}

// TestStampTextInAFontThatCoversIt is the control: text a standard font cannot
// encode is accepted with a TTF, which the stamp embeds.
func TestStampTextInAFontThatCoversIt(t *testing.T) {
	t.Parallel()

	font, err := NewTTFFont(minimalTTF(t))
	if err != nil {
		t.Fatalf("NewTTFFont: %v", err)
	}
	w := NewWriter(openBytes(t, editTestPDF()))
	defer w.Close()
	if err := w.StampText(TextStamp{Text: "日本語", Font: font}); err != nil {
		t.Errorf("StampText with a TTF = %v, want nil", err)
	}
}

// TestStampImageDamagedPixelsFailTheWrite pins the case the queue-time check
// cannot see: a PNG whose header reads and whose pixel data does not decode
// fails the write with the same sentinel, in the stamp's EditError.
func TestStampImageDamagedPixelsFailTheWrite(t *testing.T) {
	t.Parallel()

	data := buildPNG(t, false)
	// The signature and the IHDR chunk, 33 bytes, are all the header check
	// reads; the image data is cut short.
	truncated := data[:40]

	w := NewWriter(openBytes(t, editTestPDF()))
	defer w.Close()
	if err := w.StampImage(ImageStamp{Data: truncated, MimeType: "image/png"}); err != nil {
		t.Fatalf("StampImage refused a PNG whose header reads: %v", err)
	}
	err := w.WriteTo(context.Background(), io.Discard)
	if !errors.Is(err, ErrInvalidImage) || !IsInvalidInput(err) {
		t.Fatalf("WriteTo = %v, want ErrInvalidImage, classified as invalid input", err)
	}
	if editErr, ok := errors.AsType[*EditError](err); !ok || editErr.Method != "StampImage" {
		t.Errorf("WriteTo = %v, want the EditError of StampImage", err)
	}
}

// TestDeletingEveryPageIsClassified pins the deletion that leaves no page: the
// write fails with ErrNoPageLeft, which a service answers as the caller's
// mistake.
func TestDeletingEveryPageIsClassified(t *testing.T) {
	t.Parallel()

	w := NewWriter(openBytes(t, editTestPDF()))
	defer w.Close()
	if err := w.DeletePages(1); err != nil {
		t.Fatalf("DeletePages: %v", err)
	}
	err := w.WriteTo(context.Background(), io.Discard)
	if !errors.Is(err, ErrNoPageLeft) || !IsInvalidInput(err) {
		t.Fatalf("WriteTo = %v, want ErrNoPageLeft, classified as invalid input", err)
	}
}
