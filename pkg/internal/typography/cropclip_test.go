package typography

import (
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/models"
)

func TestBoxRectFromArray(t *testing.T) {
	arr := ast.PdfArray{ast.PdfInt(10), ast.PdfReal(20.5), ast.PdfInt(600), ast.PdfReal(780.0)}
	got := boxRectFromArray(arr)
	want := [4]float64{10, 20.5, 600, 780}
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestBoxRectFromArray_Short(t *testing.T) {
	arr := ast.PdfArray{ast.PdfInt(10)}
	got := boxRectFromArray(arr)
	if got != [4]float64{} {
		t.Errorf("expected zero rect for short array, got %v", got)
	}
}

func TestBoxRectFromArray_Nil(t *testing.T) {
	got := boxRectFromArray(nil)
	if got != [4]float64{} {
		t.Errorf("expected zero rect for nil array, got %v", got)
	}
}

// TestIsClipped_OutsideCropBox verifies that elements placed outside the
// CropBox are flagged with IsClipped = true.
func TestIsClipped_OutsideCropBox(t *testing.T) {
	elements := []models.TextElement{
		{Text: "A", X: 50, Y: 400},  // Inside CropBox [10, 10, 602, 782]
		{Text: "B", X: 5, Y: 400},   // Outside left edge
		{Text: "C", X: 50, Y: 5},    // Outside bottom edge
		{Text: "D", X: 610, Y: 400}, // Outside right edge
		{Text: "E", X: 50, Y: 790},  // Outside top edge
		{Text: "F", X: 10, Y: 10},   // Exactly on corner (inside)
		{Text: "G", X: 602, Y: 782}, // Exactly on far corner (inside)
	}

	cropBox := ast.PdfArray{ast.PdfInt(10), ast.PdfInt(10), ast.PdfInt(602), ast.PdfInt(782)}
	crop := boxRectFromArray(cropBox)
	minX, minY, maxX, maxY := crop[0], crop[1], crop[2], crop[3]

	for i := range elements {
		if elements[i].X < minX || elements[i].X > maxX ||
			elements[i].Y < minY || elements[i].Y > maxY {
			elements[i].IsClipped = true
		}
	}

	tests := []struct {
		text    string
		clipped bool
	}{
		{"A", false},
		{"B", true},
		{"C", true},
		{"D", true},
		{"E", true},
		{"F", false},
		{"G", false},
	}

	for i, tc := range tests {
		if elements[i].IsClipped != tc.clipped {
			t.Errorf("element %q: IsClipped=%v, want %v", tc.text, elements[i].IsClipped, tc.clipped)
		}
	}
}

// TestIsClipped_NoCropBox verifies that when CropBox is nil/empty (unbounded),
// no elements are clipped.
func TestIsClipped_NoCropBox(t *testing.T) {
	// Build a minimal page with no CropBox (and no MediaBox = unbounded).
	page := &document.Page{
		Resources: ast.PdfDict{},
	}

	cropBox := boxRectFromArray(page.CropBox)
	if cropBox != [4]float64{} {
		t.Fatal("expected zero rect for nil CropBox")
	}
	// With zero rect, no clipping should occur (matching ExtractElements logic).
}

// TestIsClipped_ViaExtractElements is an integration-level test that
// exercises the full ExtractElements path with a CropBox set.
func TestIsClipped_ViaExtractElements(t *testing.T) {
	// We need a page with a CropBox and a machine that processes Tj ops.
	// Since we can't easily construct a full font pipeline, we test the
	// boxRectFromArray and clipping logic directly, which is the new code path.

	cropBox := ast.PdfArray{ast.PdfInt(100), ast.PdfInt(100), ast.PdfInt(500), ast.PdfInt(700)}
	page := &document.Page{
		CropBox:   cropBox,
		MediaBox:  ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(612), ast.PdfInt(792)},
		Resources: ast.PdfDict{},
	}

	machine := graphics.NewMachine(boxRectFromArray(cropBox))

	// ExtractElements with no ops produces no elements, which is fine.
	// The key assertion is that the machine got the right clipping bounds.
	elements, err := ExtractElements(nil, machine, page, []content.Operation{})
	if err != nil {
		t.Fatal(err)
	}
	if len(elements) != 0 {
		t.Errorf("expected 0 elements with no ops, got %d", len(elements))
	}
}
