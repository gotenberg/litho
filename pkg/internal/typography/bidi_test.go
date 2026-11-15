package typography

import (
	"context"
	"strings"
	"testing"

	"github.com/gotenberg/litho/pkg/internal/semantics"
	"github.com/gotenberg/litho/pkg/models"
)

func TestReorderBidiIndices_LTROnly(t *testing.T) {
	input := []string{"H", "e", "l", "l", "o"}
	xPos := []float64{10, 16, 22, 28, 34}
	indices := make([]int, len(input))
	for i := range indices {
		indices[i] = i
	}
	reordered, _ := reorderBidiIndices(bidiElems(input, xPos), indices, nil)
	got := joinByIndices(input, reordered)
	want := "Hello"
	if got != want {
		t.Errorf("LTR only: got %q, want %q", got, want)
	}
}

func TestReorderBidiIndices_RTLVisualOrder(t *testing.T) {
	// Visual order: leftmost glyph first in stream (X increasing for RTL
	// = last logical character first). Should reverse.
	input := []string{"ם", "ו", "ל", "ש"}
	xPos := []float64{10, 16, 22, 28} // X increasing = visual order for RTL
	indices := make([]int, len(input))
	for i := range indices {
		indices[i] = i
	}
	reordered, _ := reorderBidiIndices(bidiElems(input, xPos), indices, nil)
	got := joinByIndices(input, reordered)
	want := "שלום"
	if got != want {
		t.Errorf("RTL visual order: got %q, want %q", got, want)
	}
}

func TestReorderBidiIndices_RTLLogicalOrder(t *testing.T) {
	// Logical order: first logical character first (X decreasing for RTL
	// = rightmost character first in reading order). Should NOT reverse.
	input := []string{"ש", "ל", "ו", "ם"}
	xPos := []float64{28, 22, 16, 10} // X decreasing = logical order for RTL
	indices := make([]int, len(input))
	for i := range indices {
		indices[i] = i
	}
	reordered, _ := reorderBidiIndices(bidiElems(input, xPos), indices, nil)
	got := joinByIndices(input, reordered)
	want := "שלום" // Already correct, not reversed
	if got != want {
		t.Errorf("RTL logical order: got %q, want %q", got, want)
	}
}

func TestReorderBidiIndices_Mixed(t *testing.T) {
	// Visual order RTL embedded in LTR: Hebrew glyphs have increasing X.
	input := []string{"H", "e", "l", "l", "o", " ", "ם", "ו", "ל", "ש", " ", "W", "o", "r", "l", "d"}
	xPos := []float64{10, 16, 22, 28, 34, 40, 52, 58, 64, 70, 76, 82, 88, 94, 100, 106}
	indices := make([]int, len(input))
	for i := range indices {
		indices[i] = i
	}
	reordered, _ := reorderBidiIndices(bidiElems(input, xPos), indices, nil)
	got := joinByIndices(input, reordered)
	want := "Hello שלום World"
	if got != want {
		t.Errorf("Mixed: got %q, want %q", got, want)
	}
}

func TestReorderBidiIndices_ArabicVisualOrder(t *testing.T) {
	// Visual order: leftmost glyph first (X increasing).
	input := []string{"ا", "ب", "ح", "ر", "م"}
	xPos := []float64{10, 16, 22, 28, 34} // X increasing = visual order for RTL
	indices := make([]int, len(input))
	for i := range indices {
		indices[i] = i
	}
	reordered, _ := reorderBidiIndices(bidiElems(input, xPos), indices, nil)
	got := joinByIndices(input, reordered)
	want := "مرحبا"
	if got != want {
		t.Errorf("Arabic visual: got %q, want %q", got, want)
	}
}

// reorderVisual reorders texts laid out left to right at unit spacing, in
// stream order, and joins them in the order reorderBidiIndices returns.
func reorderVisual(texts []string, xPos []float64) string {
	indices := make([]int, len(texts))
	for i := range indices {
		indices[i] = i
	}
	if xPos == nil {
		xPos = make([]float64, len(texts))
		for i := range xPos {
			xPos[i] = float64(10 + 6*i)
		}
	}
	order, _ := reorderBidiIndices(bidiElems(texts, xPos), indices, nil)
	return joinByIndices(texts, order)
}

// TestReorderBidiIndices_NumberInsideRTL pins that a European number drawn
// inside a right-to-left line keeps its digit order: UAX #9 rule I2 raises
// it one level above the line, and rule L2 reverses it back.
func TestReorderBidiIndices_NumberInsideRTL(t *testing.T) {
	// Visual "11:דבוע", logically "עובד:11".
	got := reorderVisual([]string{"1", "1", ":", "ד", "ב", "ו", "ע", " ", "0", "1", "0", "0", "1", " ", ":", " ", "ה", "ק", "ל", "ח", "מ"}, nil)
	want := "מחלקה : 01001 עובד:11"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestReorderBidiIndices_MarkBeforeNeutral pins that a non-spacing mark drawn
// between a right-to-left letter and a full stop stays on its letter: UAX #9
// rule W1 gives it the type of the character before it in the run.
func TestReorderBidiIndices_MarkBeforeNeutral(t *testing.T) {
	// Visual ".ًارذع", logically "عذراً.".
	got := reorderVisual([]string{".", "ً", "ا", "ر", "ذ", "ع"}, []float64{10, 15.56, 15.58, 20.6, 27.08, 35.02})
	want := "عذراً."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestReorderBidiIndices_RunsPlacedRightToLeft pins a line whose runs are
// placed from right to left while each run draws its glyphs from left to
// right: the glyphs read in the order of their positions, not of the
// stream.
func TestReorderBidiIndices_RunsPlacedRightToLeft(t *testing.T) {
	// Visually "קוחל )ב(15ףיעס", drawn as the runs "ףיעס" at 100, "15" at
	// 80, ")ב(" at 60 and "קוחל " at 24, in that stream order.
	texts := []string{"ף", "י", "ע", "ס", "1", "5", ")", "ב", "(", "ק", "ו", "ח", "ל", " "}
	xPos := []float64{100, 106, 112, 118, 80, 86, 60, 66, 72, 24, 30, 36, 42, 48}
	got := reorderVisual(texts, xPos)
	want := "סעיף15(ב) לחוק"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestReorderBidiIndices_MirroredBracketsSetTheBase pins that a line whose
// bracket pair is drawn mirrored around left-to-right text reads as a
// right-to-left paragraph, though most of its characters run left to
// right: the pair's closing glyph, on the left, maps to ")", and UAX #9
// rule N0 resolves a pair around left-to-right text to the right-to-left
// direction only in a right-to-left paragraph ([mirroredAroundLTR]). A pair
// drawn as it reads, or a mirrored pair around right-to-left text alone,
// leaves the base to the share of right-to-left characters.
func TestReorderBidiIndices_MirroredBracketsSetTheBase(t *testing.T) {
	for _, tc := range []struct {
		name  string
		texts []string
		want  string
	}{
		{
			// Visual ")ZWJ, 16.0( فرحلا", logically "الحرف (ZWJ, 16.0)".
			name:  "mirrored pair around Latin",
			texts: []string{")", "Z", "W", "J", ",", " ", "1", "6", ".", "0", "(", " ", "ف", "ر", "ح", "ل", "ا"},
			want:  "الحرف (ZWJ, 16.0)",
		},
		{
			name:  "pair as it reads around Latin",
			texts: []string{"(", "Z", "W", "J", ",", " ", "1", "6", ".", "0", ")", " ", "ف", "ر", "ح", "ل", "ا"},
			want:  "(ZWJ, 16.0) الحرف",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := reorderVisual(tc.texts, nil); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestReorderBidiIndices_NumberAtTheLogicalEnd pins that a European number
// at the left end of a right-to-left line, its logical end, is read after
// the Arabic letter to its right: UAX #9 rule W2 makes it an Arabic number,
// which leaves the percent sign after it a neutral of the line
// ([logicalEndContext]). Read after sor, it stayed a European number that
// took the sign into its run.
func TestReorderBidiIndices_NumberAtTheLogicalEnd(t *testing.T) {
	// Visual ".%15 ةبسنب", logically "بنسبة 15%.".
	got := reorderVisual([]string{".", "%", "1", "5", " ", "ة", "ب", "س", "ن", "ب"}, nil)
	want := "بنسبة 15%."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	// After a Hebrew letter the number stays European and takes the sign:
	// "אחוזים 15%." is drawn ". 1 5 %", so ". % 1 5" reads "אחוזים %15.".
	got = reorderVisual([]string{".", "%", "1", "5", " ", "ם", "י", "ז", "ו", "ח", "א"}, nil)
	want = "אחוזים %15."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestReorderBidiIndices_Empty(t *testing.T) {
	got, _ := reorderBidiIndices(nil, nil, nil)
	if len(got) != 0 {
		t.Errorf("empty: got %v, want nil/empty", got)
	}
}

func TestNormalizeSoftHyphens(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "soft hyphen at line break keeps the break",
			input: "docu\u00AD\nment",
			want:  "docu\nment",
		},
		{
			name:  "unicode hyphen preserved at line break",
			input: "docu\u2010\nment",
			want:  "docu\u2010\nment",
		},
		{
			name:  "hyphen-minus preserved at line break",
			input: "docu-\nment",
			want:  "docu-\nment",
		},
		{
			name:  "mid-line soft hyphens are dropped",
			input: "soft\u00ADhyphen",
			want:  "softhyphen",
		},
		{
			name:  "no hyphens unchanged",
			input: "hello\nworld",
			want:  "hello\nworld",
		},
		{
			name:  "multiple soft hyphens at line breaks",
			input: "un\u00AD\nbreak\u00AD\nable",
			want:  "un\nbreak\nable",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeSoftHyphens(tc.input)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReconstructText_BidiLine(t *testing.T) {
	// Visual order: Hebrew glyphs with X increasing (leftmost first in stream).
	elements := []models.TextElement{
		{Text: "ם", X: 10, Y: 100, Width: 6, FontSize: 12},
		{Text: "ו", X: 16, Y: 100, Width: 6, FontSize: 12},
		{Text: "ל", X: 22, Y: 100, Width: 6, FontSize: 12},
		{Text: "ש", X: 28, Y: 100, Width: 6, FontSize: 12},
	}

	got := ReconstructText(context.Background(), elements, nil)
	want := "שלום"
	if got != want {
		t.Errorf("ReconstructText bidi: got %q, want %q", got, want)
	}
}

// rowElements lays texts out from left to right on one baseline, each 6
// units wide, adding gap after the text at each index in gaps.
func rowElements(texts []string, gaps map[int]float64) []models.TextElement {
	elements := make([]models.TextElement, len(texts))
	x := 10.0
	for i, t := range texts {
		elements[i] = models.TextElement{Text: t, X: x, Y: 100, Width: 6, FontSize: 12}
		x += 6 + gaps[i]
	}
	return elements
}

// TestReconstructText_BidiWordGap pins that a word gap a right-to-left line
// in visual order shows without a space glyph reads as a space: it is read
// between the glyphs that border it on the page, not between the glyphs
// logical order sets side by side.
func TestReconstructText_BidiWordGap(t *testing.T) {
	// Visually "םלוע םולש", the gap after the fourth glyph.
	elements := rowElements([]string{"ם", "ל", "ו", "ע", "ם", "ו", "ל", "ש"}, map[int]float64{3: 6})
	got := ReconstructText(context.Background(), elements, nil)
	want := "שלום עולם"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestReconstructText_BidiRunBoundary pins that a right-to-left word set
// against left-to-right text reads no space the page does not show.
func TestReconstructText_BidiRunBoundary(t *testing.T) {
	// Visually "Hello worldםולש", the Hebrew word touching the "d".
	elements := rowElements([]string{"H", "e", "l", "l", "o", " ", "w", "o", "r", "l", "d", "ם", "ו", "ל", "ש"}, nil)
	got := ReconstructText(context.Background(), elements, nil)
	want := "Hello worldשלום"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestReconstructText_SoftHyphenAcrossLines pins that a soft hyphen ending a
// line is dropped and the line break kept, as PDF.js reads it.
func TestReconstructText_SoftHyphenAcrossLines(t *testing.T) {
	elements := []models.TextElement{
		{Text: "d", X: 10, Y: 100, Width: 6, FontSize: 12},
		{Text: "o", X: 16, Y: 100, Width: 6, FontSize: 12},
		{Text: "c", X: 22, Y: 100, Width: 6, FontSize: 12},
		{Text: "u", X: 28, Y: 100, Width: 6, FontSize: 12},
		{Text: "\u00AD", X: 34, Y: 100, Width: 6, FontSize: 12},
		{Text: "m", X: 10, Y: 80, Width: 6, FontSize: 12},
		{Text: "e", X: 16, Y: 80, Width: 6, FontSize: 12},
		{Text: "n", X: 22, Y: 80, Width: 6, FontSize: 12},
		{Text: "t", X: 28, Y: 80, Width: 6, FontSize: 12},
	}

	got := ReconstructText(context.Background(), elements, nil)
	want := "docu\nment"
	if got != want {
		t.Errorf("ReconstructText soft hyphen: got %q, want %q", got, want)
	}
}

// Keeps the semantics import in use: these tests pass no beads.
var _ = []semantics.Bead{}

// bidiElems builds one element per text at the given X positions, each
// narrower than the step between them.
func bidiElems(texts []string, xPos []float64) []models.TextElement {
	elems := make([]models.TextElement, len(texts))
	for i, t := range texts {
		elems[i] = models.TextElement{Text: t, X: xPos[i], Width: 5, FontSize: 12}
	}
	return elems
}

func joinByIndices(texts []string, indices []int) string {
	var sb strings.Builder
	for _, idx := range indices {
		sb.WriteString(texts[idx])
	}
	return sb.String()
}

// TestReconstructText_BidiCellsApart pins that the runs of a right-to-left
// line a producer places from right to left, each drawn from left to
// right, the cells of a table row, read apart where the page sets them
// apart ([visualOrder]), and that a number in a cell of its own keeps its
// currency sign: the cells after the first gap are read from their own sor
// ([logicalEndContext]).
func TestReconstructText_BidiCellsApart(t *testing.T) {
	cell := func(x float64, texts ...string) []models.TextElement {
		out := make([]models.TextElement, len(texts))
		for i, s := range texts {
			out[i] = models.TextElement{Text: s, X: x + float64(6*i), Y: 100, Width: 6, FontSize: 12}
		}
		return out
	}
	var elements []models.TextElement
	// The cells "התקנה", "כמות", "12" and "$150.00", right to left, each
	// drawn left to right in visual order.
	elements = append(elements, cell(400, "ה", "נ", "ק", "ת", "ה")...)
	elements = append(elements, cell(300, "ת", "ו", "מ", "כ")...)
	elements = append(elements, cell(200, "1", "2")...)
	elements = append(elements, cell(100, "$", "1", "5", "0", ".", "0", "0")...)
	got := ReconstructText(context.Background(), elements, nil)
	want := "התקנה כמות 12 $150.00"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// TestReconstructText_SpaceOpeningTheNextLine pins that a space glyph set on
// the baseline of the next line, at its start in the stream, reads with
// that line and not with the right-to-left line before it, which it would
// open with a space the page does not show there ([spacesOpeningLine]).
func TestReconstructText_SpaceOpeningTheNextLine(t *testing.T) {
	el := func(text string, x, y float64) models.TextElement {
		return models.TextElement{Text: text, X: x, Y: y, Width: 6, FontSize: 12}
	}
	// "فيما" then "بعد", each drawn left to right in visual order, the
	// second line opened by a space glyph under the first word.
	elements := []models.TextElement{
		el("ا", 100, 500), el("م", 106, 500), el("ي", 112, 500), el("ف", 118, 500),
		el(" ", 115, 480),
		el("د", 121, 480), el("ع", 127, 480), el("ب", 133, 480),
	}
	got := ReconstructText(context.Background(), elements, nil)
	want := "فيما\nبعد "
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
