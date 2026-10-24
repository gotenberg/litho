package typography

import (
	"context"
	"math"
	"strings"

	"github.com/gotenberg/litho/pkg/models"
)

// overdrawMaxOffset bounds, in ems of the glyph's font size, how far a glyph
// may lie from an earlier glyph of the same character and still be the same
// glyph drawn again: a drop shadow, an emboss or a fake bold drawn two to
// five times. Not spec-mandated. Producers offset such a copy by a few
// hundredths of an em for a fake bold and by about a tenth for a shadow,
// while two glyphs a reader tells apart, a character doubled in a word, the
// cells of a column, a word repeated on a line, lie at least an advance or a
// line apart.
const overdrawMaxOffset = 0.25

// overdrawMaxAdvanceShare bounds the same offset along the baseline, as a
// share of the glyph's advance, between two glyphs of one piece of text
// ([overdrawPieces]), so that a narrow character set twice in a row never
// matches its neighbour: the two l of "ll" lie under a quarter of an em
// apart, but a whole advance. Not spec-mandated: a glyph drawn again at once
// for an effect lies over itself, under half its advance from it. A copy of
// a line drawn after the line, a shadow, is another piece and is not bound
// by it: no advance separates it from its original.
const overdrawMaxAdvanceShare = 0.5

// overdrawSizeSlack is the relative difference of font size two glyphs may
// show and still be one glyph drawn twice. Not spec-mandated: it admits the
// rounding of a producer's matrices.
const overdrawSizeSlack = 0.01

// overdrawAngleSlack is the largest difference, in radians, between the
// baselines of two glyphs that are one glyph drawn twice. Not
// spec-mandated: it admits the rounding of a producer's matrices.
const overdrawAngleSlack = 0.01

// maxOverdrawCell bounds the glyphs [dropOverdrawn] indexes in one cell.
// Anti-DoS guard, not spec-mandated: a crafted page can set thousands of one
// character in one cell that match none of one another, a different angle
// each, and comparing each with all costs their square. A real page sets a
// character in one cell of a quarter em a handful of times, once per copy, so
// 64 holds every copy of it, and a glyph past the bound is compared but not
// indexed.
const maxOverdrawCell = 64

// overdrawCell keys the index of glyphs [dropOverdrawn] reads: the glyph's
// text, its font size on a logarithmic grid of [overdrawSizeSlack], and the
// cell of its origin on a grid of [overdrawMaxOffset] ems.
type overdrawCell struct {
	text string
	size int64
	x, y int64
}

// dropOverdrawn returns elements, and fonts parallel to them when not nil,
// without the glyphs that draw an earlier glyph again over itself: the same
// character at the same size, along the same direction, under
// [overdrawMaxOffset] ems away on each axis ([overdraws]), the earlier glyph
// or one of its copies, with its neighbours copied alike ([copiedInContext]).
//
// A copy drawn so is one piece of text shown several times for an effect, a
// drop shadow, an emboss, a fake bold, and the page shows it once. The text
// of a page is what it shows, so it is read once. The first copy stays: the
// copy drawn last shows on top (ISO 32000-1, 8.2) but spells the same
// characters, and the first keeps the text where the content stream first
// draws it, so the reading order of the page does not move; a page header
// drawn again at the end of its stream stays read at the top. Glyphs are
// matched one by one rather than run by run, since the copies of one line are
// often cut into show-text chains at different places. PDF.js's
// getTextContent keeps every copy (src/core/evaluator.js holds no such
// filter); this reads the words a sighted reader reads.
//
// # Corpus evidence
//
//   - All corpora: the default reading changes on 628 pages of 141 files,
//     90419 characters read twice or more no longer repeated and 9135 moved,
//     and every word read before survives but on 4 pages, labels of a plot
//     drawn over one another sharing their "Q=" (govdocs
//     thread_400_400141.pdf) and three words the merge joins otherwise.
//   - govdocs thread_200_200433.pdf and thread_200_200835.pdf: PowerPoint
//     slides draw each line in a dark shadow and again in white a tenth of an
//     em away, read "OutlineOutline" and "PurposePurpose" once the white copy
//     is judged visible.
//   - govdocs thread_400_400574.pdf, thread_200_200044.pdf and
//     thread_300_300592.pdf: headings drawn five times a fraction of a point
//     apart for a fake bold, read "NOTICENOTICENOTICENOTICENOTICE" and
//     "Eight-Eight-Eight-Eight-Eight-".
//   - pdfjs issue20513.pdf: an outlined logo drawn four times, its kerning
//     pairs cut into different show-text chains in each copy.
//   - govdocs thread_100_100853.pdf and thread_300_300033.pdf: text set over
//     other text shares a letter, or two in a row, with it here and there;
//     matching glyphs alone read "sec ion" for "section" and "Am dment" for
//     "Amendment", which the neighbours on both sides tell apart.
//   - pdfjs bug900822.pdf: each glyph drawn four times in a row, read
//     "TTTTeeeesssstttt".
//   - govdocs thread_300_300250.pdf: a footer drawn again 0.24 em away with
//     its spacing drifting, matched in part, read "This lth rd luation".
//   - govdocs thread_100_100499.pdf: "Following" set with a space drawn
//     between its two l, read "Fol owing" when glyphs of one piece were not
//     held an advance apart.
//   - govdocs thread_000_000208.pdf: "Modified for the Web" drawn at the top
//     of each page and again, at the same place, at the end of its stream;
//     keeping the last copy read the header at the end of the page.
func dropOverdrawn(ctx context.Context, elements []models.TextElement, fonts []runFont) ([]models.TextElement, []runFont, error) {
	if len(elements) < 2 {
		return elements, fonts, nil
	}
	piece := overdrawPieceOf(elements)
	// original[j] is the glyph j draws again, -1 when it draws none.
	original := make([]int, len(elements))
	for j := range original {
		original[j] = -1
	}
	seen := make(map[overdrawCell][]int)
	found := false
	for j := range elements {
		if j%1000 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
		}
		key, ok := overdrawKey(elements[j])
		if !ok {
			continue
		}
	search:
		for dx := int64(-1); dx <= 1; dx++ {
			for dy := int64(-1); dy <= 1; dy++ {
				c := key
				c.x, c.y = c.x+dx, c.y+dy
				for _, i := range seen[c] {
					if overdraws(elements, piece, i, j) && copiedInContext(elements, piece, i, j) {
						original[j] = i
						found = true
						break search
					}
				}
			}
		}
		if len(seen[key]) < maxOverdrawCell {
			seen[key] = append(seen[key], j)
		}
	}
	if !found {
		return elements, fonts, nil
	}
	lies := func(k int, from map[int]bool) bool {
		key, ok := overdrawKey(elements[k])
		if !ok {
			return false
		}
		for dx := int64(-1); dx <= 1; dx++ {
			for dy := int64(-1); dy <= 1; dy++ {
				c := key
				c.x, c.y = c.x+dx, c.y+dy
				for _, i := range seen[c] {
					if i < k && from[piece[i]] && overdraws(elements, piece, i, k) {
						return true
					}
				}
			}
		}
		return false
	}
	dropped := overdrawPieces(elements, piece, original, lies)
	outEl := make([]models.TextElement, 0, len(elements))
	var outFonts []runFont
	if fonts != nil {
		outFonts = make([]runFont, 0, len(fonts))
	}
	for i := range elements {
		if dropped[i] {
			continue
		}
		outEl = append(outEl, elements[i])
		if fonts != nil {
			outFonts = append(outFonts, fonts[i])
		}
	}
	return outEl, outFonts, nil
}

// overdrawAcross is how far apart across their baseline, in ems, two glyphs
// drawn one after the other may lie and still be one line of one piece of
// text ([overdrawPieces]). Not spec-mandated: half an em keeps a superscript
// on its line and the next line, an em or more away, off it.
const overdrawAcross = 0.5

// overdrawPieces decides, from the copy each glyph draws (original, -1 for
// none), which glyphs [dropOverdrawn] drops, piece by piece: a piece is the
// glyphs drawn one after the other along one line within [overdrawReach] ems
// of each other. A glyph copying a glyph of its own piece, a fake bold drawn
// glyph by glyph, goes, and so does one copying a glyph of another piece. A
// copy whose spacing drifts from the original's matches part of its glyphs
// in context alone, and keeping the rest would mix the two copies into one
// line ("This lth rd luation"): in a piece holding [overdrawChain] glyphs
// or more copying glyphs of other pieces, every other glyph lying on the same
// character of one of those pieces, by [overdraws] alone, goes too, as does
// the white space between two glyphs that go. lies reports such a character
// for glyph k among the pieces from holds.
func overdrawPieces(elements []models.TextElement, piece, original []int, lies func(k int, from map[int]bool) bool) []bool {
	dropped := make([]bool, len(elements))
	blank := func(k int) bool { return strings.TrimSpace(elements[k].Text) == "" }
	for start := 0; start < len(elements); {
		end := start + 1
		for end < len(elements) && piece[end] == piece[start] {
			end++
		}
		// from holds the other pieces this one copies glyphs of.
		from := make(map[int]bool)
		copies := 0
		for k := start; k < end; k++ {
			if original[k] >= 0 {
				dropped[k] = true
				if o := piece[original[k]]; o != piece[k] {
					from[o] = true
					copies++
				}
			}
		}
		if copies >= overdrawChain {
			for k := start; k < end; k++ {
				if !dropped[k] && !blank(k) && lies(k, from) {
					dropped[k] = true
				}
			}
			for k := start; k < end; k++ {
				if dropped[k] || !blank(k) {
					continue
				}
				before, after := k-1, k+1
				for before >= start && blank(before) {
					before--
				}
				for after < end && blank(after) {
					after++
				}
				if (before < start || dropped[before]) && (after >= end || dropped[after]) {
					dropped[k] = true
				}
			}
		}
		start = end
	}
	return dropped
}

// overdrawPieceOf numbers the piece of text each glyph belongs to (see
// [overdrawPieces]).
func overdrawPieceOf(elements []models.TextElement) []int {
	piece := make([]int, len(elements))
	for k := 1; k < len(elements); k++ {
		piece[k] = piece[k-1]
		if !overdrawSameLine(elements[k-1], elements[k]) {
			piece[k]++
		}
	}
	return piece
}

// overdrawSameLine reports b, drawn right after a, on a's line and within
// [overdrawReach] ems of it along it.
func overdrawSameLine(a, b models.TextElement) bool {
	em := math.Max(math.Abs(a.FontSize), math.Abs(b.FontSize))
	if a.Vertical != b.Vertical || math.Abs(a.Angle-b.Angle) > overdrawAngleSlack || !(em > 0) {
		return false
	}
	dx, dy := b.X-a.X, b.Y-a.Y
	along := dx*math.Cos(a.Angle) + dy*math.Sin(a.Angle)
	across := -dx*math.Sin(a.Angle) + dy*math.Cos(a.Angle)
	if a.Vertical {
		along, across = across, along
	}
	return math.Abs(along) <= overdrawReach*em && math.Abs(across) <= overdrawAcross*em
}

// overdrawKey returns the index cell of el, false for an element that names
// no character or has no size.
func overdrawKey(el models.TextElement) (overdrawCell, bool) {
	size := math.Abs(el.FontSize)
	if el.Text == "" || !(size > 0) || math.IsInf(size, 0) || math.IsNaN(el.X) || math.IsNaN(el.Y) {
		return overdrawCell{}, false
	}
	cell := overdrawMaxOffset * size
	x, y := math.Floor(el.X/cell), math.Floor(el.Y/cell)
	if math.Abs(x) > math.MaxInt32 || math.Abs(y) > math.MaxInt32 {
		return overdrawCell{}, false
	}
	return overdrawCell{
		text: el.Text,
		size: int64(math.Round(math.Log(size) / overdrawSizeSlack)),
		x:    int64(x),
		y:    int64(y),
	}, true
}

// overdrawShiftSlack is how far apart, in ems, the shifts of two glyphs of
// one copy may lie: a copy moves its glyphs as a whole, so a glyph and its
// neighbour move alike, to the rounding of the producer's numbers. Not
// spec-mandated.
const overdrawShiftSlack = 0.02

// overdrawReach is how far apart, in ems, two glyphs drawn one after the
// other may lie and still be one piece of text, the glyphs of a word or of
// two words side by side. Not spec-mandated.
const overdrawReach = 2

// overdrawChain is the number of glyphs drawn in a row, each a copy of the
// glyph drawn at the same rank of an earlier row by the same shift, that
// proves the row a copy of the earlier one rather than another text sharing
// a few letters with it. Not spec-mandated: two different texts set over one
// another share a letter often and two in a row now and then ("en" of
// "Amendment" over "Reference"), three in a row at the same shift seldom.
const overdrawChain = 3

// copiedInContext reports glyph j a copy of glyph i in its context, not the
// same character of another text that happens to lie on it. It holds when
// every element drawn between i and j shows j's character, a glyph drawn
// again at once, as a fake bold drawn glyph by glyph. Otherwise the glyphs
// drawn before and after i and j are paired rank by rank while each pair is a
// copy by the same shift, and it holds when [overdrawChain] pairs or more
// chain so, or when the chain spans every glyph of the piece of text j
// belongs to, a short word or a lone bullet drawn twice.
func copiedInContext(elements []models.TextElement, piece []int, i, j int) bool {
	run := true
	for k := i + 1; k < j; k++ {
		if elements[k].Text != elements[j].Text {
			run = false
			break
		}
	}
	if run {
		return true
	}
	em := math.Abs(elements[j].FontSize)
	dx, dy := elements[j].X-elements[i].X, elements[j].Y-elements[i].Y
	pair := func(a, b int) bool {
		if a < 0 || b >= len(elements) || a >= b {
			return false
		}
		ea, eb := elements[a], elements[b]
		return overdraws(elements, piece, a, b) &&
			math.Abs(eb.X-ea.X-dx) <= overdrawShiftSlack*em && math.Abs(eb.Y-ea.Y-dy) <= overdrawShiftSlack*em
	}
	back, fwd := 0, 0
	for back < overdrawChain && i-back-1 >= 0 && pair(i-back-1, j-back-1) {
		back++
	}
	for fwd < overdrawChain && pair(i+fwd+1, j+fwd+1) {
		fwd++
	}
	chain := 1 + back + fwd
	if chain >= overdrawChain {
		return true
	}
	// The piece of text j belongs to, past the chain: the glyphs drawn next
	// to it within reach, the earlier copy's glyphs left out.
	start, end := j-back, j+fwd
	near := func(a, b int) bool {
		reach := overdrawReach * math.Max(math.Abs(elements[a].FontSize), math.Abs(elements[b].FontSize))
		return math.Abs(elements[b].X-elements[a].X) <= reach && math.Abs(elements[b].Y-elements[a].Y) <= reach
	}
	if start-1 > i+fwd && near(start-1, start) {
		return false
	}
	if end+1 < len(elements) && near(end, end+1) {
		return false
	}
	return true
}

// overdraws reports glyph j drawing glyph i, drawn earlier, again over
// itself (see [dropOverdrawn]), piece giving the piece of text of each glyph.
func overdraws(elements []models.TextElement, piece []int, i, j int) bool {
	earlier, later := elements[i], elements[j]
	if earlier.Text != later.Text || earlier.Vertical != later.Vertical || math.Abs(earlier.Angle-later.Angle) > overdrawAngleSlack {
		return false
	}
	a, b := math.Abs(earlier.FontSize), math.Abs(later.FontSize)
	if math.Abs(a-b) > overdrawSizeSlack*math.Max(a, b) {
		return false
	}
	em := b
	dx, dy := later.X-earlier.X, later.Y-earlier.Y
	if math.Abs(dx) >= overdrawMaxOffset*em || math.Abs(dy) >= overdrawMaxOffset*em {
		return false
	}
	// Along the baseline, measured in the glyph's own direction.
	along := math.Abs(dx*math.Cos(later.Angle) + dy*math.Sin(later.Angle))
	if later.Vertical {
		along = math.Abs(-dx*math.Sin(later.Angle) + dy*math.Cos(later.Angle))
	}
	// A glyph that advances nothing, of a font whose widths are missing, is
	// set where the next one is, so a character doubled in a word lies on
	// itself: such glyphs are never told copies.
	w := math.Min(math.Abs(earlier.Width), math.Abs(later.Width))
	return w > 0 && (piece[i] != piece[j] || along < overdrawMaxAdvanceShare*w)
}
