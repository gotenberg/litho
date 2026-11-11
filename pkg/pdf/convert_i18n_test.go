package pdf

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
)

// TestConvert_I18nTextExtractsInSourceOrder converts Gotenberg's i18n test
// page (testdata/gotenberg-i18n) with its pinned fixture fonts and extracts the text back: each
// paragraph reads in its source order, the Devanagari vowel sign drawn
// before its consonant included. The word spaces of a line whose glyphs
// carry nonspacing marks follow PDF.js and may differ, so that line is
// compared without its spaces.
func TestConvert_I18nTextExtractsInSourceOrder(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "test", "integration")
	fonts := filepath.Join(root, "testdata", "fonts")
	src, err := os.ReadFile(filepath.Join("testdata", "gotenberg-i18n", "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(fonts, "noto-sans-devanagari-2.007")); err != nil {
		t.Skip("the fixture fonts are not fetched: make corpus-fetch CORPUS=fonts")
	}
	ctx := context.Background()
	var out bytes.Buffer
	if _, err := FromHTML(bytes.NewReader(src),
		WithConvertFontDirs(filepath.Join(fonts, "liberation-2.1.5"), filepath.Join(fonts, "dejavu-2.37"),
			filepath.Join(fonts, "noto-cjk-sans-2.004"), filepath.Join(fonts, "noto-sans-thai-2.002"),
			filepath.Join(fonts, "noto-sans-devanagari-2.007"))).WriteTo(ctx, &out); err != nil {
		t.Fatalf("WriteTo: %v", err)
	}
	data := out.Bytes()
	r, err := Open(ctx, bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	text, err := r.ExtractText(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(text), "\n")
	want := []string{"Multilingual"}
	for line := range strings.SplitSeq(string(src), "\n") {
		if found := strings.Contains(line, "<p"); found {
			body := line[strings.Index(line, ">")+1:]
			want = append(want, strings.TrimSuffix(body, "</p>"))
		}
	}
	if len(got) != len(want) {
		t.Fatalf("lines %q, want %q", got, want)
	}
	squeeze := func(s string) string {
		return strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return -1
			}
			return r
		}, s)
	}
	for i := range want {
		g, w := got[i], want[i]
		if strings.HasPrefix(w, "HI:") {
			g, w = squeeze(g), squeeze(w)
		}
		if g != w {
			t.Errorf("line %d = %q, want %q", i, g, w)
		}
	}
}
