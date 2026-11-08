package pptx

import (
	"bytes"
	"context"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/convert/slides"
)

// paritySources returns the directory Gotenberg's decks of the pptx
// corpus sit under: LITHO_PPTX_SOURCES, else the corpus's sources.
func paritySources() string {
	if dir := os.Getenv("LITHO_PPTX_SOURCES"); dir != "" {
		return dir
	}
	return filepath.Join("..", "..", "..", "..", "test", "integration", "corpus", "gotenberg", "pptx", "sources")
}

// findSource returns the path of the file name under dir, "" when absent.
func findSource(dir, name string) string {
	found := ""
	_ = filepath.WalkDir(dir, func(path string, e fs.DirEntry, err error) error { //nolint:errcheck // a missing corpus skips the test
		if err == nil && !e.IsDir() && e.Name() == name {
			found = path
			return fs.SkipAll
		}
		return nil
	})
	return found
}

// TestParity_GotenbergDecks reads Gotenberg's decks and checks the slides,
// titles, hidden slide, notes and the placeholder-resolved boxes of a few
// shapes against what LibreOffice prints.
//
// # Corpus evidence
//
//   - pptx: deck prints 7 slides of 959.98 by 540 points without its
//     hidden seventh, "Agenda" centred in a 648-point title box at 36,
//     21.63, its bullets at 43.2 (the body box's 36 plus its 7.2 inset),
//     the table's first cell text at 79.2 (72 plus 7.2); pitch-lo prints
//     4 slides of 793.75 by 446.5 points, PITCH-1 to PITCH-4 in boxes at
//     56.69, 28.35.
func TestParity_GotenbergDecks(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		file  string
		check func(t *testing.T, doc *slides.Document)
	}{
		{"deck.pptx", func(t *testing.T, doc *slides.Document) {
			if len(doc.Slides) != 8 || math.Abs(doc.Width-959.98) > 0.01 || doc.Height != 540 {
				t.Fatalf("%d slides of %v x %v", len(doc.Slides), doc.Width, doc.Height)
			}
			var titles []string
			for i, s := range doc.Slides {
				titles = append(titles, s.Title)
				if s.Hidden != (i == 6) {
					t.Errorf("slide %d hidden %v", i+1, s.Hidden)
				}
			}
			want := []string{"Q4 Product Review", "Agenda", "Two columns", "Table", "Bar chart", "Image", "Hidden slide", "Thank you"}
			if !slices.Equal(titles, want) {
				t.Errorf("titles %q", titles)
			}
			for i, marker := range map[int]string{1: "NOTES-MARKER-2", 7: "NOTES-MARKER-8"} {
				if n := doc.Slides[i].Notes; len(n) == 0 || len(n[0].Runs) == 0 || n[0].Runs[0].Text != marker {
					t.Errorf("slide %d notes %+v", i+1, n)
				}
			}
			boxes := map[string]compose.Rect{
				"Title 1":               {X: 36, Y: 21.6249, Width: 648, Height: 90},
				"Content Placeholder 2": {X: 36, Y: 126, Width: 648, Height: 356.3750},
			}
			for name, want := range boxes {
				if sh := shapeNamed(doc.Slides[1].Shapes, name); sh == nil || !near(sh.Box, want) {
					t.Errorf("%s: %+v, want %+v", name, sh, want)
				}
			}
			if tb := shapeNamed(doc.Slides[3].Shapes, "Table 2"); tb == nil || tb.Table == nil || tb.Box != (compose.Rect{X: 72, Y: 144, Width: 720, Height: 144}) {
				t.Errorf("table %+v", tb)
			}
			if ch := shapeNamed(doc.Slides[4].Shapes, "Chart 2"); ch == nil || ch.Chart == nil {
				t.Errorf("chart %+v", ch)
			}
			if p := shapeNamed(doc.Slides[5].Shapes, "Picture 2"); p == nil || p.Picture == nil || p.Box != (compose.Rect{X: 288, Y: 180, Width: 360, Height: 120}) {
				t.Errorf("picture %+v", p)
			}
		}},
		{"pitch-lo.pptx", func(t *testing.T, doc *slides.Document) {
			if len(doc.Slides) != 4 {
				t.Fatalf("%d slides", len(doc.Slides))
			}
			for i, s := range doc.Slides {
				if len(s.Shapes) != 2 || s.Shapes[0].Text == nil {
					t.Fatalf("slide %d shapes %+v", i+1, s.Shapes)
				}
				if got := s.Shapes[0].Text.Paragraphs[0].Runs[0].Text; got != "PITCH-"+string(rune('1'+i)) {
					t.Errorf("slide %d text %q", i+1, got)
				}
				if !near(s.Shapes[0].Box, compose.Rect{X: 56.6929, Y: 28.3465, Width: 680.3150, Height: 85.0394}) {
					t.Errorf("slide %d box %+v", i+1, s.Shapes[0].Box)
				}
			}
		}},
	} {
		t.Run(tc.file, func(t *testing.T) {
			t.Parallel()
			path := findSource(paritySources(), tc.file)
			if path == "" {
				t.Skipf("%s not found under %s", tc.file, paritySources())
			}
			data, err := os.ReadFile(path) //nolint:gosec // a fixture of the corpus
			if err != nil {
				t.Fatal(err)
			}
			doc, _, err := load(context.Background(), bytes.NewReader(data), testFonts(t), office.Options{})
			if err != nil {
				t.Fatal(err)
			}
			tc.check(t, doc)
		})
	}
}
