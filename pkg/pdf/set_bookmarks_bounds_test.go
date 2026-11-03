package pdf

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/models"
)

// TestSetBookmarksRefusesADeadBookmark pins that SetBookmarks refuses, as
// invalid input and without writing, a bookmark at any depth that names no
// page of the document or has no title. Such a bookmark used to be written
// with a null destination and read back on page 0.
func TestSetBookmarksRefusesADeadBookmark(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		bookmarks []models.Bookmark
		want      error
		says      []string
	}{
		{"page past the end", []models.Bookmark{{Title: "X", PageNumber: 4}}, ErrInvalidPageNumber, []string{`bookmark 1 "X"`, "page 4", "3 pages"}},
		{"page 0", []models.Bookmark{{Title: "A", PageNumber: 1}, {Title: "Zero", PageNumber: 0}}, ErrInvalidPageNumber, []string{`bookmark 2 "Zero"`, "page 0", "3 pages"}},
		{"negative page", []models.Bookmark{{Title: "Minus", PageNumber: -1}}, ErrInvalidPageNumber, []string{"page -1"}},
		{"nested child past the end", []models.Bookmark{{Title: "A", PageNumber: 1, Children: []models.Bookmark{
			{Title: "A.1", PageNumber: 2},
			{Title: "A.2", PageNumber: 3, Children: []models.Bookmark{{Title: "Deep", PageNumber: 9}}},
		}}}, ErrInvalidPageNumber, []string{`bookmark 1.2.1 "Deep"`, "page 9", "3 pages"}},
		{"empty title", []models.Bookmark{{Title: "", PageNumber: 1}}, ErrEmptyText, []string{"bookmark 1 has an empty title"}},
		{"nested empty title", []models.Bookmark{{Title: "A", PageNumber: 1, Children: []models.Bookmark{{PageNumber: 2}}}}, ErrEmptyText, []string{"bookmark 1.1 has an empty title"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			data := threePagePDF()
			r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			defer r.Close()
			w := NewWriter(r)
			defer w.Close()
			w.SetBookmarks(tc.bookmarks)

			var out bytes.Buffer
			err = w.WriteTo(ctx, &out)
			if !errors.Is(err, tc.want) {
				t.Fatalf("WriteTo returned %v, want %v", err, tc.want)
			}
			if !IsInvalidInput(err) {
				t.Errorf("IsInvalidInput(%v) = false, want true", err)
			}
			for _, s := range tc.says {
				if !strings.Contains(err.Error(), s) {
					t.Errorf("the refusal %q does not say %s", err, s)
				}
			}
			if out.Len() != 0 {
				t.Errorf("the refused write wrote %d bytes", out.Len())
			}
		})
	}
}

// TestSetBookmarksKeepsBookmarksOnEveryPage is the control for the test
// above: a nested outline naming the first and the last page is written and
// reads back as given.
func TestSetBookmarksKeepsBookmarksOnEveryPage(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	data := threePagePDF()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	w := NewWriter(r)
	defer w.Close()
	want := []models.Bookmark{
		{Title: "First", PageNumber: 1, Children: []models.Bookmark{{Title: "Last", PageNumber: 3, Children: []models.Bookmark{}}}},
	}
	w.SetBookmarks(want)
	var out bytes.Buffer
	if err := w.WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	got, err := Open(ctx, bytes.NewReader(out.Bytes()), int64(out.Len()))
	if err != nil {
		t.Fatalf("reopening the output: %v", err)
	}
	defer got.Close()
	bookmarks, err := got.Bookmarks(ctx)
	if err != nil {
		t.Fatalf("Bookmarks: %v", err)
	}
	if len(bookmarks) != 1 || bookmarks[0].Title != "First" || bookmarks[0].PageNumber != 1 ||
		len(bookmarks[0].Children) != 1 || bookmarks[0].Children[0].Title != "Last" || bookmarks[0].Children[0].PageNumber != 3 {
		t.Errorf("the output's bookmarks are %+v, want %+v", bookmarks, want)
	}
}
