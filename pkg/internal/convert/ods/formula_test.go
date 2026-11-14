package ods

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/convert/office"
	"github.com/gotenberg/litho/pkg/internal/text"
	"github.com/gotenberg/litho/pkg/internal/typeset"
)

// TestDrawing_ReadsAFormula locks a formula object in a cell: a drawing
// of the frame's size, marked a formula, that draws the formula's glyphs.
func TestDrawing_ReadsAFormula(t *testing.T) {
	t.Parallel()
	dir := filepath.Join("..", "..", "..", "..", "test", "integration", "testdata", "fonts", "gotenberg", "truetype", "liberation")
	if _, err := os.Stat(dir); err != nil {
		t.Skip("fixture fonts not fetched (make corpus-fetch CORPUS=fonts)")
	}
	fonts, err := text.NewFonts(context.Background(), nil, []string{dir})
	if err != nil {
		t.Fatal(err)
	}
	body := table("S", `<table:table-row><table:table-cell><draw:frame svg:x="0.1cm" svg:y="0.1cm" svg:width="2cm" svg:height="1cm"><draw:object>`+
		`<math xmlns="http://www.w3.org/1998/Math/MathML" display="block"><mrow><mi>x</mi><mo>+</mo><mn>1</mn></mrow></math>`+
		`</draw:object></draw:frame></table:table-cell></table:table-row>`)
	doc, _, err := Read(context.Background(), flat{body: body}.bytes(), fonts, office.Options{})
	if err != nil {
		t.Fatal(err)
	}
	sh := doc.Sheets[0]
	if len(sh.Drawings) != 1 || !sh.Drawings[0].Formula || sh.Drawings[0].Picture == nil || sh.Drawings[0].Alt != "x + 1" {
		t.Fatalf("drawings %+v, want one formula", sh.Drawings)
	}
	glyphs := 0
	for _, f := range sh.Drawings[0].Picture.Vector(t.Context(), typeset.Rect{Width: 56.7, Height: 28.35}) {
		if g, ok := f.(*typeset.Group); ok {
			for _, k := range g.Content {
				if inner, ok := k.(*typeset.Group); ok {
					glyphs += len(inner.Content)
				}
			}
		}
	}
	if glyphs != 3 {
		t.Errorf("the formula draws %d runs, want 3", glyphs)
	}
}
