package typography

import (
	"bytes"
	"context"
	"math"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/internal/graphics"
	"github.com/gotenberg/litho/pkg/internal/graphics/content"
	"github.com/gotenberg/litho/pkg/models"
)

// minimalFontPDF is a single-page PDF that declares a Helvetica Type1
// font and a content stream drawing "Hello". Used to drive the full
// extractTextRecursive path (Tf, Tj, font resolution, text-matrix
// update, element emission) without spinning up pkg/pdf.
func minimalFontPDF() []byte {
	return []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
			"5 0 obj\n<< /Font << /F1 4 0 R >> >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
			"6 0 obj\n<< /Length 44 >>\nstream\nBT /F1 12 Tf 100 700 Td (Hello) Tj ET\nendstream\nendobj\n" +
			"xref\n0 7\n" +
			"0000000000 65535 f \r\n" +
			"0000000009 00000 n \r\n" +
			"0000000058 00000 n \r\n" +
			"0000000177 00000 n \r\n" +
			"0000000115 00000 n \r\n" +
			"0000000165 00000 n \r\n" +
			"0000000256 00000 n \r\n" +
			"trailer\n<< /Size 7 /Root 1 0 R >>\n" +
			"startxref\n326\n%%EOF\n")
}

// TestExtractElements_MinimalPDF runs ExtractElements against a crafted
// PDF with a known text string. The assertion is behavioral: the
// returned elements contain "Hello" somewhere in their Text fields,
// and the element count is non-zero. Exact counts, positions, and
// widths are not asserted. Those vary with the implementation.
func TestExtractElements_MinimalPDF(t *testing.T) {
	t.Parallel()
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
	if err != nil {
		t.Fatalf("ResolvePages: %v", err)
	}
	if len(pages) == 0 {
		t.Fatal("ResolvePages: no pages")
	}
	page := pages[0]

	ops, err := content.ParsePageContent(ctx, doc, page.Dict)
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}

	machine := graphics.NewMachine([4]float64{0, 0, 612, 792})
	elements, err := ExtractElements(ctx, doc, machine, page, ops)
	if err != nil {
		t.Fatalf("ExtractElements: %v", err)
	}
	if len(elements) == 0 {
		t.Fatal("ExtractElements: got 0 elements, want >0")
	}

	var joined strings.Builder
	for _, e := range elements {
		joined.WriteString(e.Text)
	}
	if !strings.Contains(joined.String(), "Hello") {
		t.Errorf("extracted text: got %q, want to contain %q", joined.String(), "Hello")
	}
}

// TestExtractElements_MalformedTfNoOperands pins the operand guard: a Tf
// operator with no operands, which a malformed content stream can carry, must
// not panic. Before the guard, extractTextRecursive read op.Operands[0] and
// crashed with "index out of range [0] with length 0". Reproduces pdfjs
// bug1130815.pdf, which reached this path through ExtractTables.
func TestExtractElements_MalformedTfNoOperands(t *testing.T) {
	t.Parallel()
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
		t.Fatalf("ResolvePages: %v (pages=%d)", err, len(pages))
	}

	ops := []content.Operation{{Operator: "Tf"}} // no operands
	machine := graphics.NewMachine([4]float64{0, 0, 612, 792})
	if _, err := ExtractElements(ctx, doc, machine, pages[0], ops); err != nil {
		t.Fatalf("ExtractElements on Tf with no operands: %v", err)
	}
}

// TestExtractText_MinimalPDF runs the higher-level ExtractText which
// reconstructs a string from the element list. Asserts the text is
// present.
func TestExtractText_MinimalPDF(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	data := minimalFontPDF()
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, _ := document.ResolveCatalog(ctx, doc)
	pages, _ := document.ResolvePages(ctx, doc, catalog)
	page := pages[0]
	ops, _ := content.ParsePageContent(ctx, doc, page.Dict)

	machine := graphics.NewMachine([4]float64{0, 0, 612, 792})
	got, err := ExtractText(ctx, doc, machine, page, ops, ExtractionConfig{})
	if err != nil {
		t.Fatalf("ExtractText: %v", err)
	}
	if !strings.Contains(got, "Hello") {
		t.Errorf("ExtractText: got %q, want to contain %q", got, "Hello")
	}
}

// TestExtractElements_ActualTextEmitsRealGlyph guards the BDC /ActualText
// path. A producer that wraps a glyph in a /Span <</ActualText...>> BDC
// (commonly small-cap renderings of capital glyphs) used to suppress the
// inner Tj and emit a synthetic placeholder element with FontSize=Tfs and
// a hardcoded 0.5 advance, leaving downstream gap accounting blind to
// where the substituted glyph really sat. For text extraction the
// substitution is dropped and the rendered glyph is reported: this test
// pins that contract, so the original glyph reaches the element list
// with its real advance and effective font size.
func TestExtractElements_ActualTextEmitsRealGlyph(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// Single-page PDF with content stream:
	//   BT /F1 1 Tf
	//   8 0 0 8 100 500 Tm           (effective FontSize 8)
	//   /Span <</ActualText (v)>> BDC
	//   (V) Tj
	//   EMC
	//   ET
	const stream = "BT /F1 1 Tf 8 0 0 8 100 500 Tm /Span <</ActualText (v)>> BDC (V) Tj EMC ET"
	data := []byte(
		"%PDF-1.4\n" +
			"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
			"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n" +
			"4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n" +
			"5 0 obj\n<< /Font << /F1 4 0 R >> >>\nendobj\n" +
			"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources 5 0 R /Contents 6 0 R >>\nendobj\n" +
			"6 0 obj\n<< /Length " + ftoa(len(stream)) + " >>\nstream\n" + stream + "\nendstream\nendobj\n" +
			"xref\n0 7\n0000000000 65535 f \n0000000000 00000 n \n0000000000 00000 n \n0000000000 00000 n \n0000000000 00000 n \n0000000000 00000 n \n0000000000 00000 n \n" +
			"trailer\n<< /Size 7 /Root 1 0 R >>\nstartxref\n0\n%%EOF\n")
	doc, err := document.NewDocument(ctx, bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatalf("NewDocument: %v", err)
	}
	catalog, _ := document.ResolveCatalog(ctx, doc)
	pages, _ := document.ResolvePages(ctx, doc, catalog)
	if len(pages) == 0 {
		t.Fatal("no pages")
	}
	ops, _ := content.ParsePageContent(ctx, doc, pages[0].Dict)
	machine := graphics.NewMachine([4]float64{0, 0, 612, 792})
	elems, err := ExtractElements(ctx, doc, machine, pages[0], ops)
	if err != nil {
		t.Fatalf("ExtractElements: %v", err)
	}
	if len(elems) != 1 {
		t.Fatalf("got %d elements, want 1; elems=%+v", len(elems), elems)
	}
	got := elems[0]
	if got.Text != "V" {
		t.Errorf("Text = %q, want %q (rendered glyph, not ActualText)", got.Text, "V")
	}
	if math.Abs(got.FontSize-8.0) > 1e-6 {
		t.Errorf("FontSize = %v, want 8 (effective Tm scale × Tfs)", got.FontSize)
	}
	if got.Width <= 0 {
		t.Errorf("Width = %v, want >0 (real glyph advance, not zero/placeholder)", got.Width)
	}
}

// ftoa is a tiny helper for the inline xref-less fixture above.
func ftoa(n int) string {
	return strings.TrimSpace(string([]byte{byte('0' + n/100), byte('0' + (n/10)%10), byte('0' + n%10)}))
}

// TestExtractElements_ContextCancellation honors a cancelled context.
// The per-op cancellation check in extractTextRecursive fires every
// 100 operators. For a 1-op content stream the outer ctx.Err() check
// at function entry is what catches this.
func TestExtractElements_ContextCancellation(t *testing.T) {
	t.Parallel()
	data := minimalFontPDF()
	doc, err := document.NewDocument(context.Background(), bytes.NewReader(data), int64(len(data)), nil)
	if err != nil {
		t.Fatal(err)
	}
	catalog, _ := document.ResolveCatalog(context.Background(), doc)
	pages, _ := document.ResolvePages(context.Background(), doc, catalog)
	page := pages[0]
	ops, _ := content.ParsePageContent(context.Background(), doc, page.Dict)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	machine := graphics.NewMachine([4]float64{0, 0, 612, 792})
	// May return an error (ctx.Canceled) or partial results, either
	// is acceptable. The contract is "no panic, no block".
	_, _ = ExtractElements(ctx, doc, machine, page, ops) //nolint:errcheck // cancelation contract exercised; result irrelevant.
}

func TestCoalesceElements_MergesSameRun(t *testing.T) {
	t.Parallel()
	in := []models.TextElement{
		{Text: "H", X: 0, Y: 0, Width: 7, FontSize: 12, SpaceWidth: 4},
		{Text: "e", X: 7, Y: 0, Width: 6, FontSize: 12, SpaceWidth: 4},
		{Text: "l", X: 13, Y: 0, Width: 3, FontSize: 12, SpaceWidth: 4},
		{Text: "l", X: 16, Y: 0, Width: 3, FontSize: 12, SpaceWidth: 4},
		{Text: "o", X: 19, Y: 0, Width: 6, FontSize: 12, SpaceWidth: 4},
	}
	got := CoalesceElements(context.Background(), in)
	if len(got) != 1 {
		t.Fatalf("CoalesceElements: got %d runs, want 1", len(got))
	}
	if got[0].Text != "Hello" {
		t.Errorf("text: got %q, want %q", got[0].Text, "Hello")
	}
	if got[0].Width != 25 {
		t.Errorf("width: got %v, want 25", got[0].Width)
	}
}

func TestCoalesceElements_SplitsOnStyleChange(t *testing.T) {
	t.Parallel()
	in := []models.TextElement{
		{Text: "H", X: 0, Y: 0, Width: 7, FontSize: 12},
		{Text: "e", X: 7, Y: 0, Width: 6, FontSize: 14}, // different size
	}
	got := CoalesceElements(context.Background(), in)
	if len(got) != 2 {
		t.Fatalf("font-size change: got %d runs, want 2", len(got))
	}
}

func TestCoalesceElements_SplitsOnGap(t *testing.T) {
	t.Parallel()
	in := []models.TextElement{
		{Text: "A", X: 0, Y: 0, Width: 7, FontSize: 12, SpaceWidth: 4},
		{Text: "B", X: 50, Y: 0, Width: 7, FontSize: 12, SpaceWidth: 4}, // big jump
	}
	got := CoalesceElements(context.Background(), in)
	if len(got) != 2 {
		t.Fatalf("big gap: got %d runs, want 2", len(got))
	}
}

func TestCoalesceElements_DropsWhitespaceRuns(t *testing.T) {
	t.Parallel()
	in := []models.TextElement{
		{Text: "Hello", X: 0, Y: 0, Width: 25, FontSize: 12},
		{Text: "   ", X: 0, Y: 20, Width: 9, FontSize: 12},
		{Text: "World", X: 0, Y: 40, Width: 30, FontSize: 12},
	}
	got := CoalesceElements(context.Background(), in)
	if len(got) != 2 {
		t.Fatalf("whitespace drop: got %d runs %+v, want 2", len(got), got)
	}
	if got[0].Text != "Hello" || got[1].Text != "World" {
		t.Errorf("unexpected texts: %q, %q", got[0].Text, got[1].Text)
	}
}

func TestCoalesceElements_RotatedText(t *testing.T) {
	t.Parallel()
	// 90° rotated text advances along Y instead of X.
	angle := math.Pi / 2
	in := []models.TextElement{
		{Text: "A", X: 0, Y: 0, Width: 8, FontSize: 12, Angle: angle, SpaceWidth: 4},
		{Text: "B", X: 0, Y: 8, Width: 8, FontSize: 12, Angle: angle, SpaceWidth: 4},
	}
	got := CoalesceElements(context.Background(), in)
	if len(got) != 1 {
		t.Fatalf("rotated: got %d runs %+v, want 1", len(got), got)
	}
	if got[0].Text != "AB" {
		t.Errorf("text: got %q, want %q", got[0].Text, "AB")
	}
}

func TestCoalesceElements_SplitsOnMCIDChange(t *testing.T) {
	t.Parallel()
	in := []models.TextElement{
		{Text: "A", X: 0, Y: 0, Width: 8, FontSize: 12, MCID: 1},
		{Text: "B", X: 8, Y: 0, Width: 8, FontSize: 12, MCID: 2},
	}
	got := CoalesceElements(context.Background(), in)
	if len(got) != 2 {
		t.Fatalf("MCID change: got %d runs, want 2", len(got))
	}
}
