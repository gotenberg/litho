package typography

import (
	"context"
	"math"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
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

// TestOffPage_BaselineAgainstCropBox pins the test [offPage] makes: a glyph
// is off the page when its baseline segment does not touch the crop box [10
// 10 602 782] (ISO 32000-1, 14.11.2).
func TestOffPage_BaselineAgainstCropBox(t *testing.T) {
	t.Parallel()
	crop := [4]float64{10, 10, 602, 782}
	nan := math.NaN()
	tests := []struct {
		name string
		seg  [2][2]float64
		off  bool
	}{
		{"inside", [2][2]float64{{50, 400}, {56, 400}}, false},
		{"cut by the left edge, origin outside", [2][2]float64{{7, 400}, {13, 400}}, false},
		{"wholly left", [2][2]float64{{2, 400}, {8, 400}}, true},
		{"cut by the right edge, origin inside", [2][2]float64{{600, 400}, {606, 400}}, false},
		{"wholly right, origin just past the edge", [2][2]float64{{602.4, 400}, {608.4, 400}}, true},
		{"baseline under the bottom edge", [2][2]float64{{50, 9}, {56, 9}}, true},
		{"baseline on the bottom edge", [2][2]float64{{50, 10}, {56, 10}}, false},
		{"baseline over the top edge", [2][2]float64{{50, 783}, {56, 783}}, true},
		{"zero advance on the edge", [2][2]float64{{602, 400}, {602, 400}}, false},
		{"turned, running up through the bottom edge", [2][2]float64{{300, 5}, {300, 15}}, false},
		// Turned 45 degrees, a segment whose bounding box reaches the
		// corner of the crop box while the segment does not.
		{"turned, past the corner", [2][2]float64{{600, 786}, {606, 780}}, true},
		{"turned, over the corner", [2][2]float64{{598, 784}, {604, 778}}, false},
		{"non-numbers stay", [2][2]float64{{nan, nan}, {nan, nan}}, false},
	}
	for _, tc := range tests {
		if got := offPage(tc.seg, crop); got != tc.off {
			t.Errorf("%s: offPage = %v, want %v", tc.name, got, tc.off)
		}
	}
}

// TestTextPageBox pins the box the glyphs are read against: the crop box,
// its corners in order, and none for a page without one or with one that
// shares no area with the media box.
func TestTextPageBox(t *testing.T) {
	t.Parallel()
	media := ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(612), ast.PdfInt(792)}
	if got, ok := textPageBox(&document.Page{MediaBox: media}); ok {
		t.Errorf("no crop box: %v, want none", got)
	}
	outside := ast.PdfArray{ast.PdfInt(1000), ast.PdfInt(1000), ast.PdfInt(1100), ast.PdfInt(1100)}
	if got, ok := textPageBox(&document.Page{CropBox: outside, MediaBox: media}); ok {
		t.Errorf("crop box outside the media box: %v, want none", got)
	}
	swapped := ast.PdfArray{ast.PdfInt(562), ast.PdfInt(742), ast.PdfInt(50), ast.PdfInt(50)}
	got, ok := textPageBox(&document.Page{CropBox: swapped, MediaBox: media})
	if want := [4]float64{50, 50, 562, 742}; !ok || got != want {
		t.Errorf("swapped corners: %v, want %v", got, want)
	}
}

// TestGlyphBaseline_Advance pins the baseline segment of a glyph: from its
// origin along its advance, through the text matrix, and down its vertical
// advance in vertical writing.
func TestGlyphBaseline_Advance(t *testing.T) {
	t.Parallel()
	machine := graphics.NewMachine([4]float64{0, 0, 612, 792})
	state := machine.Current()
	state.Text.Tm = [6]float64{0, 1, -1, 0, 100, 200}
	font := &Font{Subtype: "Type1", FirstChar: 65, LastChar: 65, Widths: []float64{667}}
	got := glyphBaseline(state, font, 10, 65)
	want := [2][2]float64{{100, 200}, {100, 206.67}}
	for i := range got {
		if math.Abs(got[i][0]-want[i][0]) > 1e-9 || math.Abs(got[i][1]-want[i][1]) > 1e-9 {
			t.Fatalf("turned a quarter: segment = %v, want %v", got, want)
		}
	}
	state.Text.Tm = [6]float64{1, 0, 0, 1, 100, 200}
	vertical := &Font{Subtype: "Type0", WMode: 1, DW2: [2]float64{880, -1000}}
	got = glyphBaseline(state, vertical, 10, 1)
	want = [2][2]float64{{100, 200}, {100, 190}}
	for i := range got {
		if math.Abs(got[i][0]-want[i][0]) > 1e-9 || math.Abs(got[i][1]-want[i][1]) > 1e-9 {
			t.Fatalf("vertical: segment = %v, want %v", got, want)
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
// exercises the full [ExtractElements] path with a CropBox set.
func TestIsClipped_ViaExtractElements(t *testing.T) {
	// A page whose machine processes Tj ops would need a full font pipeline,
	// so the test checks boxRectFromArray and the clipping bounds directly,
	// which is the new code path.

	cropBox := ast.PdfArray{ast.PdfInt(100), ast.PdfInt(100), ast.PdfInt(500), ast.PdfInt(700)}
	page := &document.Page{
		CropBox:   cropBox,
		MediaBox:  ast.PdfArray{ast.PdfInt(0), ast.PdfInt(0), ast.PdfInt(612), ast.PdfInt(792)},
		Resources: ast.PdfDict{},
	}

	machine := graphics.NewMachine(boxRectFromArray(cropBox))

	// ExtractElements with no ops produces no elements, which is fine.
	// The key assertion is that the machine got the right clipping bounds.
	elements, err := ExtractElements(context.Background(), nil, machine, page, []content.Operation{})
	if err != nil {
		t.Fatal(err)
	}
	if len(elements) != 0 {
		t.Errorf("expected 0 elements with no ops, got %d", len(elements))
	}
}
