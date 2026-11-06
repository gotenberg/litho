package text

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func FuzzLayout(f *testing.F) {
	f.Add("AB AB\nABC D", 30.0, false)
	f.Add("\x00\x01\x7f\u0085\r\r\n\t\t\ufeff\u200d\xed\xa0\x80\xff A\u0301\u2067אב\u2069", 7.5, true)
	f.Add(strings.Repeat("ABCD", 500), 0.01, true)
	// The index reads its files again on first use of a face, so their
	// directory lives as long as the fuzz target.
	fuzzFonts := newFuzzFonts(f, f.TempDir())
	f.Fuzz(func(t *testing.T, text string, width float64, preserve bool) {
		if len(text) > 1<<14 {
			return
		}
		ws := Collapse
		if preserve {
			ws = Preserve
		}
		runs := []Run{{Text: text, Style: style()}, {Text: text, Style: Style{Size: 13.7, LetterSpacing: -1, WordSpacing: 2, Italic: true}}}
		lines, _, err := fuzzFonts.Layout(context.Background(), runs, width, Options{Whitespace: ws})
		if err != nil {
			return
		}
		for _, l := range lines {
			for _, v := range []float64{l.Width, l.Ascent, l.Descent, l.LineGap} {
				if math.IsNaN(v) || math.IsInf(v, 0) {
					t.Fatalf("line metric %v", v)
				}
			}
			for _, r := range l.Runs {
				for _, g := range r.Glyphs {
					if math.IsNaN(g.X) || math.IsInf(g.X, 0) || math.IsNaN(g.Y) || math.IsInf(g.Y, 0) || math.IsNaN(r.Origin.X) {
						t.Fatalf("glyph at %v, %v", g.X, g.Y)
					}
				}
			}
		}
	})
}

// newFuzzFonts indexes the test families in dir once for the fuzz target.
func newFuzzFonts(t testing.TB, dir string) *Fonts {
	t.Helper()
	files := append(sansFamily(), fontFile{name: "digits.ttf", data: testDigits()}, fontFile{name: "hebrew.ttf", data: testHebrew()})
	for _, file := range files {
		if err := os.WriteFile(filepath.Join(dir, file.name), file.data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f, err := NewFonts(context.Background(), nil, []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	return f
}
