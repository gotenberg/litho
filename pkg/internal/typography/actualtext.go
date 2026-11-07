package typography

import (
	"slices"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// Text extraction returns the glyphs as drawn and leaves /ActualText out
// in general: a small-caps "V" drawn for an ActualText "v" stays "V". One
// case is honoured. When the glyphs of a marked-content sequence spell
// exactly the characters of its ActualText in another order, the sequence
// is a cluster whose glyphs are drawn out of logical order: a Devanagari
// vowel sign drawn before the consonant it follows, a reordered Indic or
// Khmer cluster (ISO 32000-1, 14.9.4 makes the ActualText the content's
// text in logical order). Its first element then carries the ActualText
// and the others none, so the extracted text reads as written. Elements
// keep their positions, one per glyph: what selects glyphs by area still
// finds every one.

// maxActualTextRunes bounds the ActualText a sequence is compared with.
// Anti-DoS guard, not spec-mandated: a reordered cluster spans a few
// characters, and a longer text is no cluster.
const maxActualTextRunes = 64

// actualSpan is an open marked-content sequence carrying an ActualText:
// the index of its first element, its text, and the marked-content depth
// it opened at.
type actualSpan struct {
	start int
	text  string
	depth int
}

// actualTextOf returns the ActualText a BDC property value carries, raw
// bytes as the machine records them, decoded as a text string (ISO
// 32000-1, 7.9.2.2).
func actualTextOf(raw string) string {
	return document.DecodePdfString(ast.PdfString(raw))
}

// reorderActualText gives elems, the elements of one marked-content
// sequence, its ActualText text when their glyphs spell its characters in
// another order: text on the first, nothing on the others, and the first
// widened over the span the glyphs advance through when they run along
// one horizontal baseline.
func reorderActualText(elems []models.TextElement, text string) {
	if len(elems) < 2 {
		return
	}
	want := []rune(text)
	if len(want) == 0 || len(want) > maxActualTextRunes {
		return
	}
	var got []rune
	for _, e := range elems {
		got = append(got, []rune(e.Text)...)
		if len(got) > len(want) {
			return
		}
	}
	if string(got) == text || len(got) != len(want) {
		return
	}
	slices.Sort(got)
	sorted := slices.Clone(want)
	slices.Sort(sorted)
	if !slices.Equal(got, sorted) {
		return
	}
	x0, x1 := elems[0].X, elems[0].X+elems[0].Width
	flat := !elems[0].Vertical && elems[0].Angle == 0
	for _, e := range elems[1:] {
		flat = flat && !e.Vertical && e.Angle == 0 && e.Y == elems[0].Y
		x0, x1 = min(x0, e.X), max(x1, e.X+e.Width)
	}
	elems[0].Text = text
	if flat {
		elems[0].X, elems[0].Width = x0, x1-x0
	}
	for i := range elems[1:] {
		elems[i+1].Text = ""
	}
}
