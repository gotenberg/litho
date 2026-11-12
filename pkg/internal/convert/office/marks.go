package office

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/gotenberg/litho/pkg/internal/css"
)

// This file holds the drawing of the marked view of tracked changes
// ([ChangesMarked]) that the DOCX and ODT converters share. Word's
// marked view is not measured: no reference prints a document's changes
// shown. Both converters draw LibreOffice's, which prints a DOCX
// document's changes as an OpenDocument one's. The layout engine draws
// the change bar of a line that holds changed content (layout.Node
// ChangeBar).

// MarkKind is how the marked view draws the text of a change.
type MarkKind uint8

const (
	// MarkInsert draws an insertion: underlined, in its author's colour.
	MarkInsert MarkKind = iota + 1
	// MarkDelete draws a deletion: struck through, in its author's
	// colour.
	MarkDelete
	// MarkAttribute draws a change of the text's attributes: bold, in its
	// author's colour.
	MarkAttribute
	// MarkMoveTo draws the destination of moved text: double underlined,
	// in [MovedColor].
	MarkMoveTo
	// MarkMoveFrom draws the origin of moved text: double struck through,
	// in [MovedColor].
	MarkMoveFrom
)

// authorColors are the colours of the authors of a marked view, 0xRRGGBB,
// the first author of the document in the first, cycling past the ninth.
//
// # Corpus evidence
//
//   - probe, odt: LibreOffice 26.8 (the pinned gotenberg/gotenberg:9.0.0
//     image, soffice --convert-to pdf, changes shown) draws the changes of
//     eleven authors in these nine colours, in the order the authors
//     first appear in the body, the tenth and eleventh in the first two
//     again; an insertion underlined and a deletion struck through, line
//     and text in the colour.
//   - probe, odt: LibreOffice 26.8 (gotenberg/gotenberg:9.4.1) draws the
//     same probe in the same nine colours.
//   - litho/docx tracked-changes: Microsoft's reference prints the
//     accepted text, without marks.
//
//nolint:gochecknoglobals // static palette
var authorColors = [...]uint32{0xC69200, 0x0646A2, 0x579D1C, 0x692B9D, 0xC5000B, 0x008080, 0x8C8400, 0x35556B, 0xD17600}

// The fixed colours of the marked view, 0xRRGGBB.
//
// # Corpus evidence
//
//   - probe, docx: LibreOffice 26.8 (gotenberg/gotenberg:9.4.1) draws the
//     w:moveFrom and w:moveTo runs of litho/docx tracked-changes in 008000,
//     double struck through and double underlined, whatever their author's
//     colour.
//   - probe, odt: LibreOffice 26.8 (gotenberg/gotenberg:9.4.1) draws a table row
//     whose style sets loext:text-changes-only false and whose every cell an
//     insertion spans on an E1F2FA ground, one a deletion spans on FCE6F4, over
//     the background of its cells, from the table's left border edge to its
//     right and over the row's height; a table inside an insertion or a deletion
//     so for every row. A row whose style leaves the attribute out keeps its own
//     ground.
//   - probe, docx: LibreOffice 26.8 (gotenberg/gotenberg:9.4.1) draws a row
//     whose w:trPr holds a w:ins on E1F2FA and one holding a w:del on FCE6F4.
const (
	MovedColor      uint32 = 0x008000
	RowInsertedFill uint32 = 0xE1F2FA
	RowDeletedFill  uint32 = 0xFCE6F4
)

// AuthorColor returns the colour, 0xRRGGBB, of the author a marked view
// numbers n from 0 in the order it meets them.
func AuthorColor(n int) uint32 {
	return authorColors[max(n, 0)%len(authorColors)]
}

// MarkColor returns the colour, 0xRRGGBB, the marked view draws a change
// of kind by the author numbered n in.
func MarkColor(kind MarkKind, n int) uint32 {
	if kind == MarkMoveTo || kind == MarkMoveFrom {
		return MovedColor
	}
	return AuthorColor(n)
}

// RGB returns the opaque css.Color of c, 0xRRGGBB.
func RGB(c uint32) css.Color {
	return css.Color{R: float64(c>>16&0xFF) / 255, G: float64(c>>8&0xFF) / 255, B: float64(c&0xFF) / 255, A: 1}
}

// Hex returns c, 0xRRGGBB, as six upper-case hexadecimal digits.
func Hex(c uint32) string {
	return fmt.Sprintf("%06X", c&0xFFFFFF)
}

// minMoveRunes is the fewest characters, white space trimmed, the text of
// a deletion and an insertion share for the marked view to draw them as a
// move ([MoveKey]).
const minMoveRunes = 6

// MoveKey returns the key under which the marked view pairs a deletion of
// text by author with an insertion of the same key: a deletion and an
// insertion of equal keys are drawn as the origin and the destination of
// moved text ([MarkMoveFrom], [MarkMoveTo]). ok is false for text too
// short to pair: fewer than [minMoveRunes] characters once trimmed, or no
// inner space.
//
// # Corpus evidence
//
//   - probe, odt: LibreOffice 26.8 (gotenberg/gotenberg:9.4.1) draws as moved a
//     deletion and an insertion of the same author whose texts are equal once
//     trimmed ("abc de", "  Padded words  "), whatever their dates; not one word
//     of thirteen letters, not "ab cd", not two authors, not two texts differing
//     in case.
func MoveKey(author, text string) (string, bool) {
	t := strings.TrimSpace(text)
	if utf8.RuneCountInString(t) < minMoveRunes || !strings.ContainsAny(t, " \t") {
		return "", false
	}
	return author + "\x00" + t, true
}
