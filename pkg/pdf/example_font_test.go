package pdf_test

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// exampleFontPath is the font program [ExampleWithFontFiles] reads through
// os.DirFS: a synthetic TrueType program named LiberationSans, drawing "A" and
// "B" as rectangles at Helvetica's widths, so it carries no third-party glyph
// data.
var exampleFontPath = filepath.Join("testdata", "fonts", "example_sans.ttf")

// updateExampleFont rewrites exampleFontPath from exampleFont:
//
//	go test ./pkg/pdf -run TestExampleFontIsCurrent -update-example-font
var updateExampleFont = flag.Bool("update-example-font", false, "rewrite "+exampleFontPath)

// exampleFont builds the program exampleFontPath holds.
func exampleFont() []byte {
	return fontbuild.TrueType{
		PostScriptName: "LiberationSans",
		Family:         "Liberation Sans",
		Subfamily:      "Regular",
		NotdefAdvance:  500,
		Glyphs: []fontbuild.Glyph{
			{Name: "A", Unicode: 'A', Advance: 667},
			{Name: "B", Unicode: 'B', Advance: 667},
		},
	}.Build()
}

// TestExampleFontIsCurrent pins that the committed example font is the one
// exampleFont builds, so the examples read a program whose origin the
// repository shows.
func TestExampleFontIsCurrent(t *testing.T) {
	want := exampleFont()
	if *updateExampleFont {
		if err := os.MkdirAll(filepath.Dir(exampleFontPath), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(exampleFontPath, want, 0o644); err != nil { //nolint:gosec // G306: a committed test fixture, meant to be world-readable.
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(exampleFontPath)
	if err != nil {
		t.Fatalf("reading the example font (regenerate with -update-example-font): %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s differs from exampleFont (%d bytes, want %d): regenerate with -update-example-font", exampleFontPath, len(got), len(want))
	}
}
