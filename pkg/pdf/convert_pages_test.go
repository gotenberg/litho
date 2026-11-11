package pdf

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

// TestConvert_PageRanges checks that the page range specification of
// [ParsePageRanges] selects a converted document's pages after layout, in
// the order it lists them, and that a specification the document has no
// pages for fails as an invalid option.
func TestConvert_PageRanges(t *testing.T) {
	t.Parallel()
	src := "one\fnew\ftwo\fnew\fthree"
	for _, tc := range []struct {
		spec  string
		pages int
	}{
		{"", 5}, {"2", 1}, {"3-5", 3}, {"5,1", 2}, {"1,1", 2}, {"odd", 3}, {"last", 1},
	} {
		var out bytes.Buffer
		rep, err := FromText(strings.NewReader(src), WithPageRanges(tc.spec), WithConvertFontDirs(convertFontDir(t))).WriteTo(context.Background(), &out)
		if err != nil {
			t.Fatalf("pages %q: %v", tc.spec, err)
		}
		if rep.Pages != tc.pages {
			t.Errorf("pages %q wrote %d pages, want %d", tc.spec, rep.Pages, tc.pages)
		}
	}
	for _, spec := range []string{"x", "3-2", "0", "9", "even,x"} {
		var out bytes.Buffer
		_, err := FromText(strings.NewReader(src), WithPageRanges(spec), WithConvertFontDirs(convertFontDir(t))).WriteTo(context.Background(), &out)
		if !errors.Is(err, ErrInvalidConvertOptions) || !errors.Is(err, ErrInvalidPageRange) {
			t.Errorf("pages %q: %v, want ErrInvalidConvertOptions wrapping ErrInvalidPageRange", spec, err)
		}
		if out.Len() != 0 {
			t.Errorf("pages %q: %d bytes written on error", spec, out.Len())
		}
	}
	// A single HTML page has no range to select.
	var out bytes.Buffer
	rep, err := FromHTML(strings.NewReader(`<p>a</p><p style="break-before:page">b</p>`),
		WithSinglePage(true), WithPageRanges("2"), WithConvertFontDirs(convertFontDir(t))).WriteTo(context.Background(), &out)
	if !errors.Is(err, ErrInvalidConvertOptions) || rep != nil || out.Len() != 0 {
		t.Errorf("single page with a selection: %+v, %v, %d bytes; want ErrInvalidConvertOptions and nothing written", rep, err, out.Len())
	}
}
