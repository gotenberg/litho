package typeset

import (
	"context"
	"errors"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/typography/fontbuild"
)

// cancelled returns a context already cancelled.
func cancelled(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	return ctx
}

// TestValidate_StopsOnCancel checks that a cancelled context stops
// Validate's walks over the pages, the structure tree and the outline,
// each within its first checkpoint.
func TestValidate_StopsOnCancel(t *testing.T) {
	t.Parallel()
	ctx := cancelled(t)
	page := &Page{Width: 100, Height: 100}
	for range 3 * validateCheck {
		page.Content = append(page.Content, &Path{Ops: []PathOp{{Kind: MoveTo}, {Kind: LineTo, P: [3]Point{{X: 1, Y: 1}}}}, Stroke: &Stroke{Color: Black, Width: 1}})
	}
	if err := (&Document{Pages: []*Page{page}}).Validate(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("Validate on a cancelled context = %v, want context.Canceled", err)
	}
	v := validator{pages: 1}
	if err := v.page(ctx, page, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("page on a cancelled context = %v, want context.Canceled", err)
	}
	root := &StructElem{Role: "Document"}
	for range 2 * validateCheck {
		root.Kids = append(root.Kids, &StructElem{Role: "P"})
	}
	if _, err := structElems(ctx, root); !errors.Is(err, context.Canceled) {
		t.Errorf("structElems on a cancelled context = %v, want context.Canceled", err)
	}
	entries := 0
	if err := v.outline(ctx, []Outline{{Title: "a", Dest: Dest{Page: 1}}}, 1, &entries, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("outline on a cancelled context = %v, want context.Canceled", err)
	}
}

// TestWriter_StopsOnCancel checks that a cancelled context stops the
// outline writer and the final renumbering, and that a font whose load it
// stops fails with the context's error, not as an invalid font.
func TestWriter_StopsOnCancel(t *testing.T) {
	t.Parallel()
	ctx := cancelled(t)
	w := newDocWriter(t.Context(), Options{}, Version17, false)
	w.heights = []float64{100}
	if _, err := w.outlines(ctx, []Outline{{Title: "a", Dest: Dest{Page: 1}}}); !errors.Is(err, context.Canceled) {
		t.Errorf("outlines on a cancelled context = %v, want context.Canceled", err)
	}
	for range 2 * finalCheck {
		w.alloc.Alloc(ast.PdfInt(1))
	}
	if _, _, err := w.final(ctx, ast.PdfDict{}); !errors.Is(err, context.Canceled) {
		t.Errorf("final on a cancelled context = %v, want context.Canceled", err)
	}
	program := fontbuild.TrueType{PostScriptName: "T", Family: "T", Subfamily: "Regular", Glyphs: []fontbuild.Glyph{{Name: "A", Unicode: 'A', Advance: 500}}, NotdefAdvance: 500}.Build()
	if _, err := w.font(ctx, &Face{Program: program, Key: "T"}); !errors.Is(err, context.Canceled) || errors.Is(err, ErrInvalidFont) {
		t.Errorf("font on a cancelled context = %v, want context.Canceled alone", err)
	}
}
