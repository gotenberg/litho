package plaintext_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/compose"
	"github.com/gotenberg/litho/pkg/internal/convert/plaintext"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
	"github.com/gotenberg/litho/pkg/pdf"
)

// glyphAdvance is the advance of every glyph of the test face, upem 1000:
// at 12 points a character is 6 points wide.
const glyphAdvance = 500

// testSerif builds a "Test Serif" face covering printable ASCII, U+0020 to
// U+007E, every glyph glyphAdvance wide.
func testSerif() []byte {
	glyphs := []fontbuild.Glyph{{Name: "space", Unicode: ' ', Advance: glyphAdvance, Empty: true}}
	for r := rune('!'); r <= '~'; r++ {
		glyphs = append(glyphs, fontbuild.Glyph{Name: fmt.Sprintf("uni%04X", r), Unicode: r, Advance: glyphAdvance})
	}
	return fontbuild.TrueType{
		PostScriptName: "TestSerif-Regular",
		Family:         "Test Serif",
		Subfamily:      "Regular",
		NotdefAdvance:  glyphAdvance,
		Glyphs:         glyphs,
	}.Build()
}

// writeFontDir writes the test face into dir.
func writeFontDir(t testing.TB, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "test_serif.ttf"), testSerif(), 0o600); err != nil {
		t.Fatal(err)
	}
}

// testFonts indexes the test face from a fresh directory.
func testFonts(t testing.TB) *text.Fonts {
	t.Helper()
	dir := t.TempDir()
	writeFontDir(t, dir)
	f, err := text.NewFonts(context.Background(), nil, []string{dir})
	if err != nil {
		t.Fatalf("NewFonts: %v", err)
	}
	if f.Len() != 1 {
		t.Fatalf("indexed %d faces, want 1", f.Len())
	}
	return f
}

// convert lays src out, failing the test on error.
func convert(t *testing.T, f *text.Fonts, src string, opts plaintext.Options) *compose.Document {
	t.Helper()
	doc, err := plaintext.Convert(context.Background(), strings.NewReader(src), f, opts)
	if err != nil {
		t.Fatalf("Convert(%q): %v", src, err)
	}
	return doc
}

// write writes doc as PDF bytes.
func write(t testing.TB, doc *compose.Document) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := compose.Write(context.Background(), doc, &b, compose.Options{}); err != nil {
		t.Fatalf("compose.Write: %v", err)
	}
	return b.Bytes()
}

// open opens PDF bytes.
func open(t testing.TB, data []byte) *pdf.Reader {
	t.Helper()
	r, err := pdf.Open(context.Background(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("pdf.Open: %v", err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

// pageLines returns the text of each line of a page, in order: the runs that
// share a baseline joined.
func pageLines(p *compose.Page) []string {
	var lines []string
	last := -1.0
	for _, f := range p.Content {
		run, ok := f.(*compose.Text)
		if !ok {
			continue
		}
		var b strings.Builder
		for _, g := range run.Glyphs {
			b.WriteString(g.Text)
		}
		if run.Origin.Y != last || len(lines) == 0 {
			lines = append(lines, b.String())
			last = run.Origin.Y
			continue
		}
		lines[len(lines)-1] += b.String()
	}
	return lines
}

// paragraphs returns the P elements of doc's structure.
func paragraphs(t *testing.T, doc *compose.Document) []*compose.StructElem {
	t.Helper()
	if doc.Structure == nil || doc.Structure.Role != "Document" {
		t.Fatalf("structure root %+v, want a Document element", doc.Structure)
	}
	for _, k := range doc.Structure.Kids {
		if k.Role != "P" || len(k.Kids) != 0 {
			t.Fatalf("structure kid %+v, want a P without kids", k)
		}
	}
	return doc.Structure.Kids
}
