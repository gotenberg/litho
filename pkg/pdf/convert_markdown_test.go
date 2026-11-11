package pdf

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestConvert_MarkdownLaysOutTheRenderedHTML(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := "# Notes\n\nA paragraph with a [link](https://example.com/).\n\n| a | b |\n|---|---|\n| one | two |\n"
	var out bytes.Buffer
	rep, err := FromMarkdown(strings.NewReader(src), WithConvertFontDirs(convertFontDir(t))).WriteTo(ctx, &out)
	if err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	if rep.Pages != 1 || len(rep.Warnings) != 0 {
		t.Errorf("report %+v", rep)
	}
	data := out.Bytes()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer r.Close()
	got, err := r.ExtractText(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	flat := strings.Join(strings.Fields(got), " ")
	for _, w := range []string{"Notes", "A paragraph with a link.", "one", "two"} {
		if !strings.Contains(flat, w) {
			t.Errorf("text %q lacks %q", flat, w)
		}
	}
	meta, err := r.Metadata(ctx)
	if err != nil || meta["Title"] != "Notes" {
		t.Errorf("metadata %v, %v", meta, err)
	}
	if _, err := FromMarkdown(strings.NewReader(strings.Repeat("x", 64)),
		WithConvertFontDirs(convertFontDir(t)), WithConvertLimits(Limits{MaxConvertInputBytes: 16})).WriteTo(ctx, &bytes.Buffer{}); !IsInvalidInput(err) {
		t.Errorf("past the cap: %v", err)
	}
}
