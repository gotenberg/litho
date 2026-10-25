package mutations

import (
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// TestGlyphOutlines_UnembeddedFontKeepsItsAdvanceBox pins that a font the
// document does not embed, with no substitute to draw it, bounds no outline:
// each viewer draws it with a program of its own (ISO 32000-1, 9.6.2.2), so
// its glyphs keep their advance boxes. A name no resource holds, empty code
// bytes and a nil bounder bound nothing either.
func TestGlyphOutlines_UnembeddedFontKeepsItsAdvanceBox(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	doc, page := redactFixture(t)
	resources, err := document.ResolveDict(ctx, doc, page.Dict["Resources"])
	if err != nil {
		t.Fatalf("resources: %v", err)
	}
	o := NewGlyphOutlines(doc, nil)
	for _, tc := range []struct {
		name string
		font string
		code []byte
	}{
		{"unembedded Helvetica", "F1", []byte("S")},
		{"no such font", "F9", []byte("S")},
		{"no code", "F1", nil},
	} {
		if box, ok := o.Bounds(ctx, resources, tc.font, tc.code); ok {
			t.Errorf("%s: bounds %v, want none", tc.name, box)
		}
	}
	var none *GlyphOutlines
	if _, ok := none.Bounds(ctx, ast.PdfDict{}, "F1", []byte("S")); ok {
		t.Error("a nil bounder bounded a glyph")
	}
}
