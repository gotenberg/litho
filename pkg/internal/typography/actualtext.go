package typography

import (
	"slices"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"

	"github.com/gotenberg/litho/pkg/internal/core/ast"
	"github.com/gotenberg/litho/pkg/internal/core/document"
	"github.com/gotenberg/litho/pkg/models"
)

// Text extraction returns the glyphs as drawn and leaves /ActualText out
// in general: a small-caps "V" drawn for an ActualText "v" stays "V". One
// case is honoured. When the glyphs of a marked-content sequence hold
// every character of its ActualText, compared in compatibility decomposed
// form (UAX #15, NFKD), and do not spell it as it is, the sequence is a
// cluster whose glyphs are drawn otherwise than its text is written: a
// Devanagari vowel sign drawn before the consonant it follows, a
// reordered Indic or Khmer cluster, Thai SARA AM drawn as NIKHAHIT and
// SARA AA, a dotted circle a shaper draws under a mark (ISO 32000-1,
// 14.9.4 makes the ActualText the content's text in logical order). The
// first of its glyphs that advances then carries the ActualText and the
// others none, so the extracted text reads as written. Elements keep their positions, one per
// glyph: what selects glyphs by area still finds every one.

// maxActualTextRunes bounds the ActualText a sequence is compared with and
// the text of its glyphs, each decomposed. Anti-DoS guard, not
// spec-mandated: a reordered cluster spans a few characters, and a longer
// text is no cluster.
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
// sequence, its ActualText text when their glyphs hold its characters and
// do not spell it (see above): text on the carrier, nothing on the others,
// and the carrier widened over the span the advancing glyphs run through
// when they run along one horizontal baseline. The carrier is the first
// glyph that advances, and the first glyph when none does.
//
// The replacement text stands for the sequence as a whole (ISO 32000-1,
// 14.9.4), so it is read where the sequence sits on its line: a glyph that
// advances does, where a mark drawn before its base, raised or lowered off
// the baseline with no advance of its own, does not. A sequence whose
// glyphs spell the text but begin with such a mark gives it to the carrier
// too, so the mark does not carry a character off the line. The glyphs
// left with no character and no advance take no place on the line
// ([takesNoPlace]).
//
// PDF.js reads no ActualText, so it has no rule here. Given to the first
// glyph, a mark off the baseline after a marked-content operator, the text
// starts a line of its own, and the word reads in three pieces.
//
// # Corpus evidence
//
//   - litho/html ar-shaping: "أحرارًا", "عقلاً", "وضميرًا", "كَتَبَ", each
//     tanween or fatha drawn before its letter inside an ActualText span,
//     read "أحرا", "رً" and "ا" on three lines, and "كَ تَ بَ", with the text
//     on the mark.
//   - All corpora: the pages whose text moves, 11 of pdfjs, 2 of
//     govdocs/pdf and 1 of pdfcpu, move in their white space alone, a glyph
//     with no character and no advance ending no line: empty lines go, and
//     one space moves across a line end (pdfjs issue3115r.pdf page 71). No
//     verdict moves.
func reorderActualText(elems []models.TextElement, text string) {
	if len(elems) < 2 {
		return
	}
	want := []rune(norm.NFKD.String(text))
	if len(want) == 0 || len(want) > maxActualTextRunes {
		return
	}
	var drawn strings.Builder
	for _, e := range elems {
		drawn.WriteString(e.Text)
		if drawn.Len() > maxActualTextRunes*utf8.UTFMax {
			return
		}
	}
	carrier := 0
	for i, e := range elems {
		if e.Width != 0 {
			carrier = i
			break
		}
	}
	if drawn.String() == text && carrier == 0 {
		return
	}
	got := []rune(norm.NFKD.String(drawn.String()))
	if len(got) < len(want) || len(got) > maxActualTextRunes {
		return
	}
	slices.Sort(got)
	slices.Sort(want)
	// Every character of the ActualText is drawn: the sorted glyph texts
	// hold the sorted ActualText as a subsequence.
	k := 0
	for _, r := range got {
		if k < len(want) && r == want[k] {
			k++
		}
	}
	if k < len(want) {
		return
	}
	c := elems[carrier]
	x0, x1 := c.X, c.X+c.Width
	flat := !c.Vertical && c.Angle == 0
	for i, e := range elems {
		if i == carrier || e.Width == 0 {
			continue
		}
		flat = flat && !e.Vertical && e.Angle == 0 && e.Y == c.Y
		x0, x1 = min(x0, e.X), max(x1, e.X+e.Width)
	}
	for i := range elems {
		elems[i].Text = ""
	}
	elems[carrier].Text = text
	if flat {
		elems[carrier].X, elems[carrier].Width = x0, x1-x0
	}
}
