package pdf

import (
	"bytes"
	"context"
	"errors"
	"math"
	"testing"
)

func TestBookmarkGenerationOptionsRefusesOutOfRange(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		opts BookmarkGenerationOptions
	}{
		{"negative MaxDepth", BookmarkGenerationOptions{MaxDepth: -1}},
		{"negative MaxTitleLength", BookmarkGenerationOptions{MaxTitleLength: -1}},
		{"negative MinFontSizeRatio", BookmarkGenerationOptions{MinFontSizeRatio: -1.2}},
		{"NaN MinFontSizeRatio", BookmarkGenerationOptions{MinFontSizeRatio: math.NaN()}},
		{"infinite MinFontSizeRatio", BookmarkGenerationOptions{MinFontSizeRatio: math.Inf(1)}},
		{"negative HeadingScoreThreshold", BookmarkGenerationOptions{HeadingScoreThreshold: -0.1}},
		{"HeadingScoreThreshold above 1", BookmarkGenerationOptions{HeadingScoreThreshold: 1.5}},
		{"NaN HeadingScoreThreshold", BookmarkGenerationOptions{HeadingScoreThreshold: math.NaN()}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			r := openImagePDF(t)

			if _, err := r.GenerateBookmarks(ctx, tc.opts); !errors.Is(err, ErrInvalidBookmarkOptions) {
				t.Errorf("Reader.GenerateBookmarks: got %v, want ErrInvalidBookmarkOptions", err)
			}

			w := NewWriter(r)
			w.GenerateBookmarks(tc.opts, BookmarkReplace)
			var out bytes.Buffer
			if err := w.WriteTo(ctx, &out); !errors.Is(err, ErrInvalidBookmarkOptions) {
				t.Errorf("Writer.WriteTo: got %v, want ErrInvalidBookmarkOptions", err)
			}

			bm := WithBookmarks(BookmarkOptions{Handling: BookmarkReconstruct, Generation: tc.opts})
			out.Reset()
			if err := Split(ctx, r, []int{1}, &out, bm); !errors.Is(err, ErrInvalidBookmarkOptions) {
				t.Errorf("Split: got %v, want ErrInvalidBookmarkOptions", err)
			}
			if out.Len() != 0 {
				t.Errorf("Split wrote %d bytes", out.Len())
			}
			if err := Merge(ctx, []*Reader{r, r}, &out, bm); !errors.Is(err, ErrInvalidBookmarkOptions) {
				t.Errorf("Merge: got %v, want ErrInvalidBookmarkOptions", err)
			}
		})
	}
}

func TestBookmarkGenerationOptionsAcceptsZeroAndBounds(t *testing.T) {
	t.Parallel()

	for _, opts := range []BookmarkGenerationOptions{
		{},
		{MaxDepth: 6, MaxTitleLength: 80, MinFontSizeRatio: 1.3, HeadingScoreThreshold: 1},
	} {
		if err := opts.validate(); err != nil {
			t.Errorf("validate(%+v): %v", opts, err)
		}
	}
}
