package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"strings"
	"testing"
	"testing/fstest"
)

// TestConvert_MemoryBudget pins [Limits.MaxConvertMemoryBytes] through the
// public API: a conversion the lowered budget cannot hold fails with
// [ErrConvertInputTooLarge], names what it was holding, and writes
// nothing; the same source converts under the default budget.
func TestConvert_MemoryBudget(t *testing.T) {
	t.Parallel()
	dir := convertFontDir(t)
	text := strings.Repeat("words and more words on a line of text\n", 4000)
	var cells strings.Builder
	for i := range 5000 {
		fmt.Fprintf(&cells, "%d,w%d,%d,w%d\n", i, i, i*7, i*3)
	}
	var pic bytes.Buffer
	if err := png.Encode(&pic, image.NewNRGBA(image.Rect(0, 0, 1000, 1000))); err != nil {
		t.Fatal(err)
	}
	pictures := fstest.MapFS{"a.png": {Data: pic.Bytes()}}
	for name, tc := range map[string]struct {
		format Format
		src    string
		opts   []ConvertOption
		holds  string
	}{
		"text glyph runs": {format: FormatText, src: text, holds: "glyph runs"},
		"html styles":     {format: FormatHTML, src: strings.Repeat("<p>a paragraph of words</p>", 2000), holds: "computed styles"},
		"csv cells":       {format: FormatCSV, src: cells.String(), holds: "cells"},
		"html picture": {
			format: FormatHTML, src: `<img src="a.png">`, holds: "decoded pictures",
			opts: []ConvertOption{WithResources(pictures)},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			opts := append([]ConvertOption{WithConvertFontDirs(dir)}, tc.opts...)
			var out bytes.Buffer
			low := append(opts, WithConvertLimits(Limits{MaxConvertMemoryBytes: 2 << 20}))
			rep, err := Convert(context.Background(), strings.NewReader(tc.src), tc.format, &out, low...)
			if !errors.Is(err, ErrConvertInputTooLarge) || rep != nil || !strings.Contains(err.Error(), tc.holds) {
				t.Fatalf("under 2 MiB: %v, %+v, want ErrConvertInputTooLarge naming %s", err, rep, tc.holds)
			}
			if out.Len() != 0 {
				t.Errorf("wrote %d bytes, want none", out.Len())
			}
			if _, err := Convert(context.Background(), strings.NewReader(tc.src), tc.format, &out, opts...); err != nil {
				t.Errorf("under the default budget: %v", err)
			}
		})
	}
}
