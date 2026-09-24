package typography

import (
	"bytes"
	"context"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
)

// extractBody runs the extractor over body using [minimalFontPDF]'s resources.
func extractBody(t *testing.T, body string) []elementView {
	t.Helper()
	ctx := context.Background()

	data := minimalFontPDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, err := document.ResolveCatalog(ctx, doc)
	if err != nil {
		t.Fatalf("ResolveCatalog: %v", err)
	}
	pages, err := document.ResolvePages(ctx, doc, catalog)
	if err != nil || len(pages) == 0 {
		t.Fatalf("ResolvePages: %v (%d pages)", err, len(pages))
	}

	ops, err := content.ParseStreamData(ctx, []byte(body))
	if err != nil {
		t.Fatalf("ParseStreamData: %v", err)
	}

	machine := graphics.NewMachine([4]float64{0, 0, 612, 792})
	elements, err := ExtractElements(ctx, doc, machine, pages[0], ops)
	if err != nil {
		t.Fatalf("ExtractElements: %v", err)
	}

	out := make([]elementView, 0, len(elements))
	for _, el := range elements {
		out = append(out, elementView{Text: el.Text, X: el.X, Y: el.Y, Hidden: el.Hidden})
	}
	return out
}

type elementView struct {
	Text   string
	X, Y   float64
	Hidden bool
}

// hiddenFor reports the Hidden flag of the first element whose text matches.
func hiddenFor(t *testing.T, views []elementView, want string) bool {
	t.Helper()
	for _, v := range views {
		if v.Text == want {
			return v.Hidden
		}
	}
	t.Fatalf("no element with text %q in %v", want, views)
	return false
}

func TestHidden_ClipExcludesTextOutsideIt(t *testing.T) {
	t.Parallel()

	// The clip covers x 0-200, y 0-200. "A" is drawn inside it and "B" far
	// outside, in the same clipped graphics state.
	body := "q 0 0 200 200 re W n " +
		"BT /F1 12 Tf 50 50 Td (A) Tj ET " +
		"BT /F1 12 Tf 400 700 Td (B) Tj ET " +
		"Q"

	views := extractBody(t, body)
	if hiddenFor(t, views, "A") {
		t.Error("glyph inside the clip reported Hidden")
	}
	if !hiddenFor(t, views, "B") {
		t.Error("glyph outside the clip did not report Hidden")
	}
}

func TestHidden_NoClipLeavesEverythingVisible(t *testing.T) {
	t.Parallel()

	// Same two positions, no W at all. The machine still seeds its clipping
	// path with the page box, so without the ClipApplied gate this would
	// report Hidden for anything near the edges and duplicate IsClipped.
	body := "BT /F1 12 Tf 50 50 Td (A) Tj ET " +
		"BT /F1 12 Tf 400 700 Td (B) Tj ET"

	views := extractBody(t, body)
	if hiddenFor(t, views, "A") || hiddenFor(t, views, "B") {
		t.Errorf("text reported Hidden with no clip in force: %v", views)
	}
}

func TestHidden_ClipIsRestoredByQ(t *testing.T) {
	t.Parallel()

	// The clip lives inside q/Q. Text drawn after the Q is outside no clip at
	// all, so it must not inherit the one that has been restored away.
	body := "q 0 0 200 200 re W n BT /F1 12 Tf 50 50 Td (A) Tj ET Q " +
		"BT /F1 12 Tf 400 700 Td (B) Tj ET"

	views := extractBody(t, body)
	if hiddenFor(t, views, "B") {
		t.Error("text after Q inherited a clip the restore should have dropped")
	}
}
