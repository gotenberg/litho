package pdf

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"
)

// TestWriterDeletesTheOnlySourcePageAfterAnInsertion pins that a deletion
// queued after an insertion may remove every page of the Reader, as long as
// the insertion leaves the document a page.
//
// DeletePages counted the pages it named against the Reader's alone and
// refused to delete a one-page document's only page, although the insertion
// queued before it had given the document a second one: replacing the only
// page of a document took a second Writer. The refusal stays when nothing
// would be left.
func TestWriterDeletesTheOnlySourcePageAfterAnInsertion(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	open := func(t *testing.T, text string) *Reader {
		t.Helper()
		data := textPagePDF(text)
		r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		t.Cleanup(func() {
			if err := r.Close(); err != nil {
				t.Errorf("Reader.Close: %v", err)
			}
		})
		return r
	}

	t.Run("an insertion then the deletion", func(t *testing.T) {
		t.Parallel()

		w := NewWriter(open(t, "Original"))
		defer w.Close()
		if err := w.InsertPages(1, open(t, "Inserted")); err != nil {
			t.Fatalf("InsertPages: %v", err)
		}
		if err := w.DeletePages(1); err != nil {
			t.Fatalf("DeletePages: %v", err)
		}
		if got, want := pageTexts(t, writeAndReopen(t, w)), []string{"Inserted"}; !slices.Equal(got, want) {
			t.Errorf("the output pages show %q, want %q", got, want)
		}
	})

	t.Run("nothing left", func(t *testing.T) {
		t.Parallel()

		w := NewWriter(open(t, "Original"))
		defer w.Close()
		if err := w.DeletePages(1); err != nil {
			t.Fatalf("DeletePages: %v", err)
		}
		err := w.WriteTo(ctx, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "cannot delete all") {
			t.Errorf("WriteTo = %v, want the refusal to empty the document", err)
		}
	})
}
