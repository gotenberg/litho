package pdf

import (
	"bytes"
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
)

// composeTo runs Compose and returns the output.
func composeTo(t *testing.T, width, height float64, placements []Placement, opts ...WriteOption) []byte {
	t.Helper()

	var out bytes.Buffer
	if err := Compose(context.Background(), width, height, placements, &out, opts...); err != nil {
		t.Fatalf("Compose: %v", err)
	}
	return out.Bytes()
}

// glyphOf returns the first text element on page pageNum of r that starts
// word.
func glyphOf(t *testing.T, r *Reader, pageNum int, word string) (x, y, size float64) {
	t.Helper()

	elements, err := r.ExtractTextElements(context.Background(), pageNum)
	if err != nil {
		t.Fatalf("ExtractTextElements: %v", err)
	}
	for _, e := range elements {
		if strings.HasPrefix(e.Text, word[:1]) {
			return e.X, e.Y, e.FontSize
		}
	}
	t.Fatalf("no glyph of %q on page %d among %d elements", word, pageNum, len(elements))
	return 0, 0, 0
}

// TestCompose_ShippingLabel pins the use the feature exists for: two one-page
// documents placed one above the other on a 6 by 5 inch label, 432 by 360
// points. The output is one page of that size whose text holds both
// sources', A's above B's, found without WithArtifacts.
func TestCompose_ShippingLabel(t *testing.T) {
	t.Parallel()

	a := openBytes(t, viewedSourcePDF("/MediaBox [0 0 432 144]", "Alpha", 10, 10))
	defer a.Close()
	b := openBytes(t, viewedSourcePDF("/MediaBox [0 0 432 216]", "Bravo", 10, 10))
	defer b.Close()

	out := openBytes(t, composeTo(t, 432, 360, []Placement{
		{Source: a, X: 0, Y: 216},
		{Source: b, X: 0, Y: 0},
	}))
	defer out.Close()

	if n, err := out.PageCount(context.Background()); err != nil || n != 1 {
		t.Fatalf("PageCount = %d, %v, want 1", n, err)
	}
	page, err := out.Page(context.Background(), 1)
	if err != nil {
		t.Fatalf("Page: %v", err)
	}
	if page.MediaBox != [4]float64{0, 0, 432, 360} {
		t.Errorf("MediaBox = %v, want [0 0 432 360]", page.MediaBox)
	}
	text := strings.Join(pageTexts(t, out), " ")
	if !strings.Contains(text, "Alpha") || !strings.Contains(text, "Bravo") {
		t.Errorf("the page's text is %q, want both sources' words", text)
	}
	_, ay, _ := glyphOf(t, out, 1, "Alpha")
	_, by, _ := glyphOf(t, out, 1, "Bravo")
	if math.Abs(ay-226) > 0.01 || math.Abs(by-10) > 0.01 {
		t.Errorf("A's text sits at y %g and B's at %g, want 226 and 10", ay, by)
	}
}

// TestCompose_BlankPages pins that the output has as many pages as the
// highest TargetPage names, and that a page no placement names is blank.
func TestCompose_BlankPages(t *testing.T) {
	t.Parallel()

	src := openBytes(t, textPagePDF("Hello"))
	defer src.Close()

	data := composeTo(t, 612, 792, []Placement{
		{Source: src, TargetPage: 1},
		{Source: src, TargetPage: 3},
	})
	out := openBytes(t, data)
	defer out.Close()

	texts := pageTexts(t, out)
	if len(texts) != 3 {
		t.Fatalf("the output has %d pages, want 3", len(texts))
	}
	if texts[0] != "Hello" || texts[1] != "" || texts[2] != "Hello" {
		t.Errorf("page texts = %q, want Hello, nothing, Hello", texts)
	}
	tree := readOutputTree(t, data)
	if _, ok := tree.pages[1].Dict["Contents"]; ok {
		t.Errorf("the blank page states /Contents %v, want none", tree.pages[1].Dict["Contents"])
	}
}

// TestCompose_RotatedOffsetSourceStaysInItsSlot pins that a source page
// whose box starts away from the origin and whose /Rotate turns it a quarter
// lands, turned, inside the slot whose bottom-left corner is (X, Y): 500 by
// 300 points from (50, 100).
func TestCompose_RotatedOffsetSourceStaysInItsSlot(t *testing.T) {
	t.Parallel()

	// (40, 50) shows at (y - lly, urx - x) = (20, 280) on the turned page.
	src := openBytes(t, viewedSourcePDF("/MediaBox [20 30 320 530] /Rotate 90", "Turned", 40, 50))
	defer src.Close()

	out := openBytes(t, composeTo(t, 612, 792, []Placement{{Source: src, X: 50, Y: 100}}))
	defer out.Close()

	x, y, _ := glyphOf(t, out, 1, "Turned")
	if math.Abs(x-70) > 0.01 || math.Abs(y-380) > 0.01 {
		t.Errorf("the text starts at (%g, %g), want (70, 380), inside the slot [50 100 550 400]", x, y)
	}
}

// TestCompose_ScaleHalvesTheDrawing pins that Scale 0.5 halves the distances
// from (X, Y) and the size of the text.
func TestCompose_ScaleHalvesTheDrawing(t *testing.T) {
	t.Parallel()

	src := openBytes(t, viewedSourcePDF("/MediaBox [0 0 200 200]", "Half", 100, 100))
	defer src.Close()

	out := openBytes(t, composeTo(t, 612, 792, []Placement{{Source: src, X: 10, Y: 10, Scale: 0.5}}))
	defer out.Close()

	x, y, size := glyphOf(t, out, 1, "Half")
	if math.Abs(x-60) > 0.01 || math.Abs(y-60) > 0.01 || math.Abs(size-6) > 0.01 {
		t.Errorf("the text starts at (%g, %g) at %g points, want (60, 60) at 6", x, y, size)
	}
}

// TestCompose_RefusesWhatItCannotDraw pins each refusal, the sentinel it
// wraps, and that every one is the caller's input.
func TestCompose_RefusesWhatItCannotDraw(t *testing.T) {
	t.Parallel()

	src := openBytes(t, textPagePDF("Hello"))
	defer src.Close()

	for _, tc := range []struct {
		name          string
		width, height float64
		placements    []Placement
		want          error
		says          string
	}{
		{"a width under 3 points", 2, 360, []Placement{{Source: src}}, ErrInvalidPageSize, "2 by 360"},
		{"a height past 14,400 points", 432, 20000, []Placement{{Source: src}}, ErrInvalidPageSize, "432 by 20000"},
		{"no placements", 432, 360, nil, ErrInvalidPlacement, "no placements"},
		{"a nil source", 432, 360, []Placement{{Source: src}, {}}, ErrInvalidPlacement, "placement 1: no source"},
		{"a page outside the source", 432, 360, []Placement{{Source: src, Page: 9}}, ErrInvalidPlacement, "placement 0: page 9"},
		{"a negative target page", 432, 360, []Placement{{Source: src, TargetPage: -1}}, ErrInvalidPlacement, "target page -1"},
		{"a target page past the cap", 432, 360, []Placement{{Source: src, TargetPage: maxComposedPages + 1}}, ErrInvalidPlacement, "target page"},
		{"a negative scale", 432, 360, []Placement{{Source: src, Scale: -1}}, ErrInvalidPlacement, "scale -1"},
		{"a NaN position", 432, 360, []Placement{{Source: src, X: math.NaN()}}, ErrInvalidPlacement, "not finite"},
		{"an infinite rotation", 432, 360, []Placement{{Source: src, Rotation: math.Inf(1)}}, ErrInvalidPlacement, "rotation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer
			err := Compose(context.Background(), tc.width, tc.height, tc.placements, &out)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Compose = %v, want %v", err, tc.want)
			}
			if !IsInvalidInput(err) {
				t.Errorf("IsInvalidInput(%v) = false, want true", err)
			}
			if !strings.Contains(err.Error(), tc.says) {
				t.Errorf("the error %q does not say %q", err, tc.says)
			}
			if out.Len() != 0 {
				t.Errorf("a refused composition wrote %d bytes", out.Len())
			}
		})
	}
}

// TestCompose_IsDeterministic pins that identical inputs write identical
// bytes.
func TestCompose_IsDeterministic(t *testing.T) {
	t.Parallel()

	a := openBytes(t, textPagePDF("Alpha"))
	defer a.Close()
	b := openBytes(t, viewedSourcePDF("/MediaBox [20 30 320 530] /CropBox [40 50 300 500] /Rotate 270", "Bravo", 60, 70))
	defer b.Close()
	placements := []Placement{
		{Source: a, Scale: 0.5},
		{Source: b, X: 300, Y: 50, Rotation: 15, TargetPage: 2},
		{Source: a, X: 20, Y: 400, Scale: 0.25, TargetPage: 2},
	}

	first := composeTo(t, 612, 792, placements, WithXRefStream())
	second := composeTo(t, 612, 792, placements, WithXRefStream())
	if !bytes.Equal(first, second) {
		t.Errorf("two runs wrote different bytes: %d and %d", len(first), len(second))
	}
}

// TestCompose_SharesAFormForAPagePlacedTwice pins that a source page placed
// twice is drawn from one form XObject.
func TestCompose_SharesAFormForAPagePlacedTwice(t *testing.T) {
	t.Parallel()

	src := openBytes(t, textPagePDF("Twice"))
	defer src.Close()

	data := composeTo(t, 612, 792, []Placement{{Source: src}, {Source: src, TargetPage: 2}})
	tree := readOutputTree(t, data)
	formOf := func(page *document.Page) ast.PdfIndirectRef {
		res, _ := document.ResolveDict(tree.ctx, tree.doc, page.Dict["Resources"])
		xobjects, _ := document.ResolveDict(tree.ctx, tree.doc, res["XObject"])
		for _, ref := range xobjects {
			if r, ok := ref.(ast.PdfIndirectRef); ok {
				return r
			}
		}
		t.Fatal("the page names no XObject")
		return ast.PdfIndirectRef{}
	}
	if a, b := formOf(tree.pages[0]), formOf(tree.pages[1]); a != b {
		t.Errorf("the two pages draw forms %v and %v, want one", a, b)
	}
}

// TestPipeline_ComposeOpensThePipeline pins that Compose seeds a pipeline in
// place of Open, that a later Apply edits the composed document, and that a
// Compose after Open is refused.
func TestPipeline_ComposeOpensThePipeline(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	src := openBytes(t, textPagePDF("Placed"))
	defer src.Close()

	var out bytes.Buffer
	_, err := NewPipeline().
		Compose(432, 360, []Placement{{Source: src, Y: -400}}).
		Apply(func(_ context.Context, w *Writer) error {
			return w.StampText(TextStamp{Text: "Stamped", X: 10, Y: 10})
		}).
		WriteTo(ctx, &out)
	if err != nil {
		t.Fatalf("Pipeline.WriteTo: %v", err)
	}
	r := openBytes(t, out.Bytes())
	defer r.Close()
	text, err := r.ExtractText(ctx, 1, WithArtifacts())
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if !strings.Contains(text, "Placed") || !strings.Contains(text, "Stamped") {
		t.Errorf("the output's text is %q, want the placed and the stamped words", text)
	}

	_, err = NewPipeline().Open(src).Compose(432, 360, []Placement{{Source: src}}).WriteTo(ctx, &bytes.Buffer{})
	if !errors.Is(err, ErrPipelineNoSource) || !strings.Contains(err.Error(), "first stage") {
		t.Errorf("Open then Compose returned %v, want ErrPipelineNoSource saying compose must be first", err)
	}
}
