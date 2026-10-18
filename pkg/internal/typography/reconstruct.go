package typography

import (
	"context"
	"math"
	"strings"
	"unicode"

	"github.com/gotenberg/litho/pkg/internal/semantics"
	"github.com/gotenberg/litho/pkg/models"
)

// ReconstructText takes spatial text elements and reassembles them into a string.
// When no article thread beads are provided, it preserves content stream order
// for maximum fidelity to the original document layout. When beads are provided,
// it reads the text of each bead in turn, in thread order
// ([reconstructWithBeads]).
func ReconstructText(ctx context.Context, elements []models.TextElement, beads []semantics.Bead) string {
	return reconstructText(ctx, elements, nil, beads)
}

// reconstructText is [ReconstructText] with the font each element was shown
// in, parallel to elements, so a change of font starts a new run the way it
// does in PDF.js. Without fonts (nil, or not one per element) only a change
// of size does.
//
// # Corpus evidence
//
//   - govdocs thread_000_000839.pdf, thread_200_200150.pdf and 19 more: a
//     math font's parentheses carry glyph names no table maps, so they read
//     as no character, and the word gaps around them vanished with them,
//     "time-domainFDTDprogram". PDF.js shows each such glyph as its code
//     (src/core/fonts.js, _charToGlyph) and spaces around it. Keeping the
//     gaps leaves those pages differing from PDF.js by the codes alone, and
//     makes words match on 1 more file (govdocs thread_200_200676.pdf) and
//     on none fewer.
//   - pdfjs issue13915.pdf, govdocs thread_300_300849.pdf and 4 more: "≠" is
//     a zero-width slash, read as "6", set over "=" after a word gap. Read
//     from the slash, the gap vanished before "=" ("σ1(e) 6= 0"), where
//     PDF.js, which measures it from the glyph before the slash, reads
//     "σ1(e) 6 = 0".
//   - All corpora: measuring the gap after a zero-width glyph from the last
//     glyph that advanced makes words match PDF.js on 1 more file (govdocs
//     thread_000_000159.pdf) and on 18 more pages of files that still differ
//     (pdfjs issue13915.pdf 14, govdocs thread_300_300849.pdf 2,
//     thread_300_300511.pdf 1, pdfjs issue20722.pdf 1), and on none fewer;
//     facturx, pdf20 and pdfcpu do not move.
//   - pdfjs issue20722.pdf: "U ̸= 0" for "U̸= 0", U+0338 set by a Tm a
//     word gap past "U", and "und ⃗s" for "und⃗s", the vector arrow set
//     after the word gap and before the letter it tops. pdfjs issue5994.pdf:
//     Lao vowel signs stacked by Tm read "ັ ິ ູ".
//   - All corpora: reading no word gap before a nonspacing mark and
//     measuring past it from the last glyph that advanced makes words match
//     PDF.js on 3 more files (pdfjs bug1108301.pdf, issue5994.pdf,
//     issue7426.pdf) and on 15 more pages of files that still differ (pdfjs
//     issue20722.pdf 14, issue17689.pdf 1), and on none fewer; facturx,
//     govdocs, pdf20 and pdfcpu do not move.
//   - All corpora: reading the glyphs a run shows after a mark that much
//     earlier, as PDF.js does, makes words match PDF.js on 1 more file (pdfjs
//     complex_ttf_font.pdf) and on 2 more pages of files that still differ
//     (pdfjs issue17689.pdf), and on none fewer; facturx, govdocs, pdf20 and
//     pdfcpu do not move.
func reconstructText(ctx context.Context, elements []models.TextElement, fonts []runFont, beads []semantics.Bead) string {
	if len(elements) == 0 {
		return ""
	}

	// Beads order the text across them, not within one.
	if len(beads) > 0 {
		return reconstructWithBeads(ctx, elements, fonts, beads)
	}

	// Content stream order: group elements into lines, apply bidi reordering
	// per line, then join with newlines and inferred spaces.
	type line struct {
		elems []models.TextElement
	}
	var lines []line
	cur := line{elems: []models.TextElement{elements[0]}}
	if len(fonts) != len(elements) {
		fonts = nil
	}
	items := textItemStarts(elements, fonts)
	runs := newRunTracker(elements, fonts, items)

	for i := 1; i < len(elements); i++ {
		if i%500 == 0 {
			if ctx.Err() != nil {
				return ""
			}
		}
		prev := elements[i-1]
		el := elements[i]
		// Space glyphs carry no position in PDF.js ([runTracker]), on a
		// rotated line as on an upright one: a glyph after one is read
		// against the last glyph that painted, and a space glyph set
		// rotated does not break the line or restart a run.
		if isSpaceGlyph(prev.Text) && !isSpaceGlyph(runs.last.Text) {
			prev = runs.last
		}
		var newLine bool
		switch {
		case isSpaceGlyph(el.Text) && (el.Vertical || isOrthogonalRotation(el.Angle) || isRotated(el.Angle)):
		case quarterTurnLine(runs.last, el) && quarterTurnLine(prev, el):
			// A line turned a quarter round keeps the runs of an upright
			// one, across the page X axis ([acrossLine]), so a subscript
			// shifted by less than the run ratio stays on its line.
			newLine = runs.breaksLine(i, el)
		case el.Vertical || isOrthogonalRotation(el.Angle):
			// Vertical WMode or 90°/270° rotated text: characters progress
			// along Y, so line breaks are detected by X changes.
			// PDF.js measures a vertical font against the size along the
			// column and rotated text against the size across its line.
			size := prev.FontSize
			if !el.Vertical {
				size = runs.height(i-1, prev)
			}
			newLine = leavesLine(math.Abs(prev.X-el.X), size)
			runs.restart(i, el)
		case isRotated(el.Angle):
			// For arbitrarily rotated text, project positions onto the
			// perpendicular axis of the text direction to detect line breaks.
			newLine = leavesLine(perpDist(prev, el), runs.height(i-1, prev))
			runs.restart(i, el)
		default:
			newLine = runs.breaksLine(i, el)
		}
		if newLine {
			lines = append(lines, cur)
			cur = line{elems: []models.TextElement{el}}
		} else {
			cur.elems = append(cur.elems, el)
		}
	}
	lines = append(lines, cur)

	var sb strings.Builder
	// Lines are consecutive runs of elements, so lineStart indexes the first
	// element of the current line in elements, and in fonts.
	lineStart := 0
	// advAt indexes, in elements, the last glyph written that advanced the
	// text position ([advances]). PDF.js keeps that position across its text
	// items and lines, so it outlives the line it is on.
	advAt := -1
	marks := runs.marks
	for lineIdx, ln := range lines {
		if lineIdx > 0 {
			sb.WriteString("\n")
		}
		lineFonts := fonts
		if lineFonts != nil {
			lineFonts = fonts[lineStart : lineStart+len(ln.elems)]
		}
		base := lineStart
		lineStart += len(ln.elems)

		indices := make([]int, len(ln.elems))
		for i := range ln.elems {
			indices[i] = i
		}

		// Compute adaptive space threshold from the gap histogram.
		spaceThreshold := computeSpaceThreshold(ln.elems, indices)
		hasMarks := lineHasNonspacingMark(ln.elems)

		// Bidi reorder: a line that shows right-to-left text in visual
		// order is read back in logical order, with the word gaps read in
		// visual order beside it ([reorderBidiIndices]).
		indices, reordered := reorderBidiIndices(ln.elems, indices, func(left, right int) bool {
			prev, el := ln.elems[left], ln.elems[right]
			return shouldInsertSpace(prev, computeLineGap(prev, el), wordSpaceThreshold(ln.elems, lineFonts, left, right, spaceThreshold), prev.Chunk == el.Chunk && el.Chunk != 0)
		})
		if reordered {
			writeReorderedLine(&sb, ln.elems, indices)
			continue
		}

		// Phantom anchors: unresolved glyphs reach this point with
		// Text == "". They still occupy layout space, so their
		// positions and widths anchor the gap computation between the
		// surrounding resolved runs. Walk the line, tracking the
		// previous emitted element for the space heuristic, and let
		// phantoms push that anchor forward without contributing
		// characters. A word gap on either side of a phantom still
		// reads as a space, as it does around the code PDF.js shows
		// for such a glyph.
		//
		// Chunk segmentation: elements that share a
		// models.TextElement.Chunk came from the same content-stream
		// text-operator chain. Small gaps inside a chunk are glyph
		// advancement or kerning adjustments that viewers do not turn
		// into spaces. Large gaps inside a chunk (e.g. a sizeable TJ
		// numeric adjustment) still cross the gap heuristic so the
		// visible word break survives.
		//
		// Literal space glyphs: a space glyph is held back rather than
		// written, and the gap to the next glyph is measured from the
		// last glyph that painted something, so a literal space and a
		// cursor move form one word break. See [suppressesLiteralSpace]
		// for when the held space is dropped.
		//
		// Zero-width glyphs: a glyph the font gives no advance moves the
		// text position nowhere (ISO 32000-1, 9.4.4), so the gap to the
		// glyph after it is measured from the last glyph that advanced, as
		// PDF.js measures it: its text builder records the position after
		// a glyph only when the glyph advanced (src/core/evaluator.js,
		// buildTextContentItem, prevTransform). A zero-width glyph set over
		// the next one after a word gap leaves that gap before both.
		//
		// Nonspacing marks: PDF.js shows a glyph whose character holds a
		// nonspacing mark (Unicode category Mn) without comparing its
		// position, so no word gap is read before it, and gives it no
		// advance, so the glyph after it is measured from the last glyph
		// that advanced, and every glyph its run shows after it sits the
		// mark's advance earlier ([markAdvances]). A mark combines with the character before it
		// (Unicode, 3.6), so a space before it would set it on that space.
		//
		// prevIdx is the previous element, space glyphs included; visIdx
		// the previous element that is not a space glyph.
		prevIdx, visIdx := -1, -1
		pendingSpace := ""
		for _, idx := range indices {
			el := ln.elems[idx]
			if el.Text == "" {
				// A glyph with no character still paints, so a word gap
				// before it is a word gap in the text: hold a space for
				// the next character rather than let the glyph swallow it.
				if pendingSpace == "" && prevIdx >= 0 {
					if prev := ln.elems[prevIdx]; prev.Text != "" && !isSpaceGlyph(prev.Text) &&
						shouldInsertSpace(prev, computeLineGap(prev, el), wordSpaceThreshold(ln.elems, lineFonts, prevIdx, idx, spaceThreshold), prev.Chunk == el.Chunk && el.Chunk != 0) {
						pendingSpace = " "
					}
				}
				prevIdx, visIdx = idx, idx
				if advances(el) {
					advAt = base + idx
				}
				continue
			}
			if isSpaceGlyph(el.Text) {
				if pendingSpace == "" {
					pendingSpace = heldSpace(el.Text)
				}
				prevIdx = idx
				continue
			}
			if prevIdx == idx-1 && items != nil && items[base+idx] != continuesItem {
				if spacesItem(&runs, items[base+idx], base+idx, el) {
					if pendingSpace == "" {
						pendingSpace = " "
					}
					writeSpace(&sb, pendingSpace)
					pendingSpace = ""
					sb.WriteString(el.Text)
					prevIdx, visIdx = idx, idx
					if advances(el) {
						advAt = base + idx
					}
					continue
				}
			}
			if prevIdx >= 0 && !hasNonspacingMark(el.Text) {
				prev := ln.elems[prevIdx]
				gap := computeLineGap(prev, el)
				sameChunk := prev.Chunk == el.Chunk && el.Chunk != 0
				// The reset is read from the last glyph that painted
				// something: a space glyph has no extent a reader sees,
				// and one left at the far end of the line would read
				// any word gap after the text as a jump back.
				anchor, anchorGap := prev, gap
				if isSpaceGlyph(prev.Text) && visIdx >= 0 && ln.elems[visIdx].Text != "" {
					anchor = ln.elems[visIdx]
					anchorGap = computeLineGap(anchor, el)
				}
				switch {
				case anchor.Text != "" && isRunningHeadReset(anchor, el, anchorGap):
					// A running-head reset (page header at the right
					// of a line, then a deliberate Tm reset to the
					// left margin for the title on the same line)
					// drops the trailing space the previous run baked
					// in. The drop is scoped to horizontal LTR text
					// so RTL flow (Hebrew, Arabic) and orthogonal
					// column layouts (90°-rotated body text) keep
					// their literal spaces.
					pendingSpace = ""
					if b := sb.String(); len(b) > 0 && b[len(b)-1] == ' ' {
						buf := []byte(b)
						sb.Reset()
						sb.Write(buf[:len(buf)-1])
					}
				case pendingSpace != "":
					if visIdx < 0 || hasMarks {
						break
					}
					if vis := ln.elems[visIdx]; vis.Text != "" && suppressesLiteralSpace(vis, el, computeLineGap(vis, el), literalSpaceSize(ln.elems, lineFonts, visIdx, idx), spaceGlyphsBetween(ln.elems, visIdx, idx)) {
						pendingSpace = ""
					}
				case !advances(prev) && advAt >= 0 && advAt != base+prevIdx:
					adv := elements[advAt]
					if shouldInsertSpace(adv, computeLineGap(adv, el)-marks.drift(advAt, base+idx), wordSpaceThreshold(elements, fonts, advAt, base+idx, spaceThreshold), adv.Chunk == el.Chunk && el.Chunk != 0) {
						sb.WriteString(" ")
					}
				case shouldInsertSpace(prev, gap-marks.drift(base+prevIdx, base+idx), wordSpaceThreshold(ln.elems, lineFonts, prevIdx, idx, spaceThreshold), sameChunk):
					sb.WriteString(" ")
				}
			}
			writeSpace(&sb, pendingSpace)
			pendingSpace = ""
			sb.WriteString(el.Text)
			prevIdx, visIdx = idx, idx
			if advances(el) {
				advAt = base + idx
			}
		}
		writeSpace(&sb, pendingSpace)
	}

	return normalizeExtractionUnicode(normalizeSoftHyphens(sb.String()))
}

// writeReorderedLine writes a line [reorderBidiIndices] set in logical
// order. Its word spaces are the space glyphs it shows and the
// [bidiSpace] entries read from its gaps, each run of them written once.
// No other gap is read: two elements the order sets side by side may sit
// apart on the page, or in the reverse order.
func writeReorderedLine(sb *strings.Builder, elems []models.TextElement, order []int) {
	pendingSpace := ""
	for _, idx := range order {
		if idx == bidiSpace {
			if pendingSpace == "" {
				pendingSpace = " "
			}
			continue
		}
		text := elems[idx].Text
		if text == "" {
			continue
		}
		if isSpaceGlyph(text) {
			if pendingSpace == "" {
				pendingSpace = heldSpace(text)
			}
			continue
		}
		writeSpace(sb, pendingSpace)
		pendingSpace = ""
		sb.WriteString(text)
	}
	writeSpace(sb, pendingSpace)
}

// lineBreakTolerance is how far, in user space units, a run may start
// across the baseline from the run before it and still continue its line,
// once the glyph that starts it is within the font size of the glyph
// before it ([leavesLine], [runTracker.endsLine]). It mirrors the
// tolerance of the PDF.js reference generator
// (test/integration/corpus/lib/join-lines.mjs, joinLines), which breaks
// a line between two of PDF.js's text items whose origins differ in Y by
// more than it. ISO 32000-1, 14.8.2.5 leaves line breaks to the consumer,
// so the value is the reference's, not the specification's.
const lineBreakTolerance = 2.0

// backwardLineRatio is how far across the baseline, as a fraction of the
// font size, a glyph that moves back along the line may sit from the glyph
// before it and still be on its line. PDF.js ends the line past it
// (src/core/evaluator.js, compareWithLastPosition, "not the same line"),
// where a glyph that moves forward ends it only past the whole font size.
const backwardLineRatio = 0.5

// leavesLine reports whether a glyph across moves away from the glyph
// before it, which was set at size, leaves that glyph's line: past the
// font size, where PDF.js ends the line (compareWithLastPosition), or past
// [lineBreakTolerance], where the reference generator breaks the lines
// PDF.js leaves open. ISO 32000-1, 14.8.2.5 leaves line breaks to the
// consumer, so both bounds are the reference's.
func leavesLine(across, size float64) bool {
	return across > lineBreakTolerance || across > math.Abs(size)
}

// Run segmentation factors, after PDF.js's text-item builder
// (src/core/evaluator.js, compareWithLastPosition), as fractions of the
// font size: a glyph whose baseline moves more than verticalShiftRatio of
// the size across the line of the glyph that started the run starts a new
// run (VERTICAL_SHIFT_RATIO, against the height PDF.js gives a text item
// when it begins one, ensureTextContentItem), a gap
// below negativeRunGapFactor is a move back that starts one
// (NEGATIVE_SPACE_FACTOR), and a gap above spaceInFlowMaxFactor is a jump
// PDF.js marks with a space run of its own (SPACE_IN_FLOW_MAX_FACTOR).
const (
	verticalShiftRatio   = 0.25
	negativeRunGapFactor = -0.2
	spaceInFlowMaxFactor = 0.6
)

// runTracker follows the runs of glyphs a horizontal line is made of, the
// way PDF.js groups glyphs into text items, so a baseline shift breaks the
// line in proportion to the font size rather than at a fixed distance. A
// line turned a quarter round is read the same way across the page X axis
// ([acrossLine]), as PDF.js swaps the axes of such a line.
//
// A glyph continues the run of the glyph painted before it unless its
// baseline moves by more than [verticalShiftRatio] of the size across the
// line of the glyph that started the run,
// its size changes, its MCID changes, a marked-content operator comes
// between the two ([runFont] marked), or the gap
// between them falls outside [negativeRunGapFactor] and
// [spaceInFlowMaxFactor]. Every glyph first meets the test PDF.js puts each
// glyph to against the one before it ([runTracker.endsLine]), which ends
// the line past the font size across the baseline. A glyph that passes it
// and continues a run does not break the line. One that starts a run breaks
// it when its origin sits more than [lineBreakTolerance] across the
// baseline from the start of the run before it, or, after a jump, from the
// glyph it jumped from, where PDF.js places the space run: the reference
// generator breaks the lines of text items PDF.js leaves open by that
// tolerance. A superscript set in a smaller size therefore stays on its
// line while it sits within the tolerance, a baseline that wanders inside
// one run by less than the font size does not break it at all, and the
// lines of a font smaller than the tolerance still break.
//
// Space glyphs carry no position of their own in PDF.js (they only advance
// the text matrix), so they neither start nor continue a run, and never
// break a line: PDF.js compares only the glyphs that paint something
// (src/core/evaluator.js, buildTextContentItem, which moves past a space
// without calling compareWithLastPosition). A space glyph shown away from
// the text, in an empty table cell or a blank paragraph, stays on the line
// of the glyph before it, and the glyph after it is read against the last
// glyph that painted, so a jump back to the same baseline continues the
// line as it does in PDF.js.
//
// A glyph that begins a text item at the edge of a form XObject, or after
// any Do, is compared across that edge as PDF.js compares it
// ([runTracker.breaksItem]).
//
// A glyph is compared with the last glyph that advanced the text position
// ([runTracker.anchor]): PDF.js records the position after a glyph only when
// the glyph advanced (src/core/evaluator.js, buildTextContentItem,
// prevTransform), so a zero-width glyph between the two moves neither the
// gap nor the shift across the line. A nonspacing mark advances nothing in
// PDF.js and is compared with nothing: it continues the run unless its font,
// size, MCID or marked-content sequence starts a new one, and the reference
// generator then breaks the line past [lineBreakTolerance] as for any run.
//
// # Corpus evidence
//
//   - All corpora: with the text rise on the glyph origins, words match
//     PDF.js on 26 more files (govdocs 18, pdfcpu 3, pdfjs 5) and on none
//     fewer; facturx and pdf20 do not move.
//   - govdocs thread_000_000270.pdf: a space glyph that opened the next
//     run, held on the superscript's line, raised that line's space
//     threshold and dropped the space inside "3 )".
//   - govdocs thread_100_100667.pdf, pdfjs issue20668.pdf: a superscript
//     in a font of its own, or a bullet in another font before the line,
//     breaks the line only when the font change starts the run.
//   - govdocs thread_000_000266.pdf, thread_100_100174.pdf and 13 more: a
//     text matrix that scales a 1-point Tf sets the size on the page, and
//     comparing that size started runs PDF.js does not start.
//   - govdocs thread_400_400561.pdf page 18: lines of a 0.2-unit font set
//     0.22 apart read as one line under the fixed tolerance alone.
//   - pdfjs bug857031.pdf: a horizontally compressed map label moving back
//     1.75 units across a 4.2-unit font stays on its line in PDF.js, which
//     measures the move against the size across the line, not along it.
//   - All corpora: words match PDF.js on 1 more file (govdocs
//     thread_400_400561.pdf) and on none fewer, and on 2 more pages of files
//     that still differ (govdocs thread_000_000282.pdf page 84, pdfjs
//     issue19624.pdf page 1); facturx, pdf20 and pdfcpu do not move.
//   - govdocs thread_200_200088.pdf, thread_200_200404.pdf, thread_400_400385.pdf
//     and 10 more: slides set a quarter turn round broke the line at every
//     subscript shifted across it by more than 2 units, "SO 2" and "C L"
//     where PDF.js reads "SO2" and "CL". Reading those lines by runs makes
//     words match PDF.js on 13 more files (all govdocs) and on none fewer.
//   - govdocs thread_000_000547.pdf page 2: a bullet in a smaller size
//     starts the run of "CH4", and PDF.js measures the subscript's shift
//     against the bullet's size, so it breaks the line there and not on
//     page 3, where the bullet has the size of the text. Measuring the
//     shift against the glyph that started the run makes words match PDF.js
//     on 7 more files (govdocs 6, pdfjs 1) and on none fewer.
//   - govdocs thread_000_000282.pdf: each subscript of "NOX" and "SO2" sits
//     2.4 units below its line in a marked-content sequence of its own, with
//     no MCID, set by the Tf of the text and a smaller text matrix. PDF.js
//     ends its text item at the BDC and breaks the line there; litho kept
//     the subscript in the run and glued it, "NOX burners". Ending the run
//     at every marked-content operator makes words match PDF.js on 1 more
//     file (govdocs thread_100_100516.pdf) and on 26 more pages of files
//     that still differ (govdocs thread_000_000282.pdf 23, pdfjs
//     issue16800.pdf 3), and on none fewer; facturx, pdf20 and pdfcpu do
//     not move.
//   - govdocs thread_400_400936.pdf page 216, pdfjs issue8022.pdf, pdfcpu
//     xdp_2.0.pdf: space glyphs in empty table cells, blank paragraphs or a
//     page header broke lines of their own, and the glyph after them, back
//     on the baseline of the text before them, read a space PDF.js does not
//     ("psia #", "call .", "– – 1" for "psia#", "call.", "– –1").
//   - All corpora: keeping space glyphs on the line makes words match
//     PDF.js on 11 more files (govdocs 8, pdfcpu 2, pdfjs 1) and on none
//     fewer; facturx and pdf20 do not move.
//   - govdocs thread_300_300493.pdf pages 6 and 8: the letters of figure
//     captions set a quarter turn round, "(A)", each have an upright space
//     glyph shown at the top of the page between them. Read against that
//     space, each letter broke its line, "( A )". Reading a rotated line past
//     its space glyphs makes words match PDF.js on that file and on none
//     fewer; facturx, pdf20, pdfcpu and pdfjs do not move.
//   - pdfjs issue5874.pdf: a glyph 5.7 units above the last glyph that
//     advanced, 3.6 above the zero-width glyph between them, ends the run
//     where PDF.js ends its text item, a quarter of the 14.3-point size away,
//     and so starts a new line as it does in PDF.js.
type runTracker struct {
	// start is the glyph that started the current run, and startIdx its
	// index.
	start    models.TextElement
	startIdx int
	last     models.TextElement
	// lastIdx indexes the glyph in last, to look up its font.
	lastIdx int
	// fonts is the font of each element, or nil when unknown.
	fonts []runFont
	// elements are the elements fonts pairs with.
	elements []models.TextElement
	// items is the [textItemStarts] of elements, or nil when the fonts are
	// unknown.
	items []int
	// adv is the last glyph that advanced the text position, and advIdx its
	// index, or -1 before any did.
	adv    models.TextElement
	advIdx int
	// marks indexes the nonspacing marks of elements.
	marks markAdvances
}

// newRunTracker starts tracking runs at the first of elements. fonts is
// the font of each element, or nil when unknown, and items their
// [textItemStarts].
func newRunTracker(elements []models.TextElement, fonts []runFont, items []int) runTracker {
	r := runTracker{start: elements[0], last: elements[0], fonts: fonts, elements: elements, items: items, advIdx: -1, marks: newMarkAdvances(elements)}
	if advances(elements[0]) {
		r.adv, r.advIdx = elements[0], 0
	}
	return r
}

// advance makes el, the element i, the last glyph of its run.
func (r *runTracker) advance(i int, el models.TextElement) {
	r.last = el
	r.lastIdx = i
	if advances(el) {
		r.adv, r.advIdx = el, i
	}
}

// anchor returns the glyph a move is measured from, and its index: the last
// glyph that advanced the text position, or the last glyph when none did.
func (r *runTracker) anchor() (models.TextElement, int) {
	if r.advIdx >= 0 {
		return r.adv, r.advIdx
	}
	return r.last, r.lastIdx
}

// Values of [textItemStarts] that name no element.
const (
	// continuesItem marks a glyph that continues the text item of the glyph
	// before it.
	continuesItem = -2
	// opensStream marks the first glyph a stream shows: the glyph begins a
	// text item and has no glyph to compare with.
	opensStream = -1
)

// textItemStarts returns, for each element, whether PDF.js begins a text
// item at it because the item before it ended at the start of a stream or
// at a Do ([runFont] segment), and what PDF.js compares it with there: the
// index of the last glyph its own stream showed before it, [opensStream]
// when that stream showed none, or [continuesItem] for a glyph inside an
// item. A form's first glyph opens its stream, and the glyph after the
// form is compared with the glyph before it (src/core/evaluator.js,
// getTextContent, OPS.paintXObject, where the form's text is read with a
// builder of its own). Space glyphs carry no position in PDF.js, so they
// neither begin an item nor end one. It returns nil without fonts.
func textItemStarts(elements []models.TextElement, fonts []runFont) []int {
	if fonts == nil {
		return nil
	}
	starts := make([]int, len(elements))
	lastIn := make(map[int]int)
	last := -1
	for i, el := range elements {
		starts[i] = continuesItem
		if isSpaceGlyph(el.Text) {
			continue
		}
		f := fonts[i]
		if last >= 0 && f.segment != fonts[last].segment {
			starts[i] = opensStream
			if p, ok := lastIn[f.stream]; ok {
				starts[i] = p
			}
		}
		lastIn[f.stream] = i
		last = i
	}
	return starts
}

// runFont names the font an element was shown in: the resource name and
// the size Tf selected, and the font the name resolved to. It also carries
// rise, how far up the page the text rise (ISO 32000-1, 9.3.7) lifted the
// element, which its position already includes, and height, the size of
// the font across the line on the page: the Tf size scaled by the text
// space y axis, where the element's FontSize is scaled by the x axis.
//
// stream and segment say where the element sat in the content streams
// PDF.js reads as separate text runs. stream names the invocation of the
// content stream that showed it: the page's own, or one painting of a form
// XObject. PDF.js reads a form's text with a builder of its own, so the
// form's first glyph has no previous glyph to compare with, and the glyph
// after the form compares with the last glyph its own stream showed before
// it (src/core/evaluator.js, getTextContent, OPS.paintXObject). segment
// changes wherever PDF.js ends the text item it was building: at the start
// of each stream and after every Do, a form or an image.
//
// marked changes at every marked-content operator, BMC, BDC and EMC, where
// PDF.js ends the text item it was building as well, whether or not the
// sequence carries an MCID (src/core/evaluator.js, getTextContent,
// OPS.beginMarkedContent, OPS.beginMarkedContentProps and
// OPS.endMarkedContent, each of which calls flushTextContentItem). Unlike
// a Do, it leaves the glyph after it compared with the glyph before it, as
// a change of font does.
type runFont struct {
	name    string
	font    *Font
	size    float64
	rise    float64
	height  float64
	stream  int
	segment int
	marked  int
}

// runRecorder collects the [runFont] of each element extraction appends,
// and numbers the streams and segments it names.
type runRecorder struct {
	fonts []runFont
	ids   int
}

// next returns a stream or segment number no element has been given yet.
func (r *runRecorder) next() int {
	r.ids++
	return r.ids
}

// fontChanged reports whether the glyph at i was shown in another font
// than the previous one. PDF.js starts a new text item when Tf selects
// another size, or another resource name for another font
// (buildTextContentItem), so a name that resolves to the same font
// continues the run. The size compared is the operand of Tf: a text matrix
// that scales the same size up does not start a run. Without fonts the
// size compared is the size on the page.
func (r *runTracker) fontChanged(i int, prev, el models.TextElement) bool {
	if r.fonts == nil {
		return el.FontSize != prev.FontSize
	}
	a, b := r.fonts[r.lastIdx], r.fonts[i]
	return a.size != b.size || (a.name != b.name && a.font != b.font)
}

// gapSize returns the font size, on the page, that the gap from last, the
// glyph lastIdx, to el, the glyph i, is measured against when it tells
// whether el starts a run: a move back past [negativeRunGapFactor] or a
// jump past [spaceInFlowMaxFactor]. PDF.js measures the move in the text
// space of the text item last belongs to and compares it with those factors
// times the Tf size of the item (src/core/evaluator.js,
// compareWithLastPosition, advanceX against negativeSpaceMax and
// addFakeSpaces against spaceInFlowMax). A Tm shown while the item is open
// rescales the item (updateAdvanceScale), so the size is the Tf size of
// last at the scale the text matrix gives el, as for a word space
// ([wordSpaceThreshold]). Across a marked-content boundary, which ends the
// item before the Tm can rescale it, and without fonts, it is the size of
// last on the page.
//
// # Corpus evidence
//
//   - pdfjs issue16800.pdf page 5: "𝑓𝑓" set at 18 points, then its
//     subscript "1" by a 13.14-point Tm 3.24 units back and 3.67 below.
//     Against 18 points the move back was too small to start a run and
//     the shift too small to start one across the line, so "1" read glued
//     to "𝑓𝑓", where PDF.js, which measures the move at 13.14 points, ends
//     its item and breaks the line.
//   - All corpora: words match PDF.js on 1 more file (govdocs
//     thread_100_100831.pdf) and on 1 more page of a file that still
//     differs (pdfjs issue16800.pdf page 5), and on none fewer; facturx,
//     pdf20 and pdfcpu do not move.
func (r *runTracker) gapSize(lastIdx, i int, last, el models.TextElement) float64 {
	if r.fonts == nil || last.MCID != el.MCID || r.markedChanged(lastIdx, i) {
		return math.Abs(last.FontSize)
	}
	prevSize, size := math.Abs(r.fonts[lastIdx].size), math.Abs(r.fonts[i].size)
	if prevSize == 0 || size == 0 {
		return math.Abs(last.FontSize)
	}
	return prevSize * math.Abs(el.FontSize) / size
}

// markedChanged reports whether a marked-content operator came between the
// glyph lastIdx and the glyph i ([runFont] marked). Without fonts only a
// change of MCID tells.
func (r *runTracker) markedChanged(lastIdx, i int) bool {
	return r.fonts != nil && r.fonts[lastIdx].marked != r.fonts[i].marked
}

// restart begins a new run at the element i, for a line the horizontal
// rule did not decide.
func (r *runTracker) restart(i int, el models.TextElement) {
	r.start, r.startIdx = el, i
	r.advance(i, el)
}

// breaksLine reports whether el, the element i, starts a new line, and
// advances the run state past it. A run is compared with the last glyph that
// was not a space, and a space glyph never starts a line ([runTracker]).
func (r *runTracker) breaksLine(i int, el models.TextElement) bool {
	if isSpaceGlyph(el.Text) {
		return false
	}
	if r.items != nil && r.items[i] != continuesItem {
		return r.breaksItem(i, el)
	}
	last, lastIdx := r.last, r.lastIdx
	fontChanged := r.fontChanged(i, last, el)
	if hasNonspacingMark(el.Text) {
		r.advance(i, el)
		if !fontChanged && el.MCID == last.MCID && !r.markedChanged(lastIdx, i) {
			return false
		}
		ref := r.start
		r.start, r.startIdx = el, i
		return acrossLine(ref, el) > lineBreakTolerance
	}
	from, fromIdx := r.anchor()
	r.advance(i, el)
	size := r.gapSize(fromIdx, i, from, el)
	gap := computeLineGap(from, el) - r.marks.drift(fromIdx, i)
	if r.endsLine(fromIdx, i, from, el, gap, size) {
		r.start, r.startIdx = el, i
		return true
	}
	ref := r.start
	switch {
	case gap > spaceInFlowMaxFactor*size:
		ref = from
	case gap < negativeRunGapFactor*size,
		acrossLine(from, el) > verticalShiftRatio*r.height(r.startIdx, r.start),
		fontChanged,
		el.MCID != last.MCID,
		r.markedChanged(lastIdx, i):
	default:
		return false
	}
	r.start, r.startIdx = el, i
	return acrossLine(ref, el) > lineBreakTolerance
}

// breaksItem reports whether el, the element i, starts a new line as the
// first glyph of a text item PDF.js began because the item before it ended
// at the start of a stream or at a Do ([textItemStarts]), and advances the
// run state past it.
//
// PDF.js compares the glyph with the last glyph its own stream showed, the
// glyph before the form it follows, and never with the glyphs of a form:
// the first glyph of a form has nothing to compare with
// (src/core/evaluator.js, getTextContent, OPS.paintXObject and
// compareWithLastPosition). The glyph starts a text item whatever that
// comparison finds, and the reference generator then breaks the line when
// the item sits more than [lineBreakTolerance] across the baseline from the
// item before it, the run the last glyph belongs to
// (test/integration/corpus/lib/join-lines.mjs, joinLines). When the
// comparison ends the line, PDF.js flags it, and the reference breaks it
// only past [sameBaselineRatio] of the size of that last glyph. When it
// finds a gap past [trackingSpaceFactor], PDF.js places a space item at the
// glyph it compared with, and the reference measures from there as well.
// ISO 32000-1, 8.10.1 paints a form as a unit and 14.8.2.5 leaves line
// breaks to the consumer, so the boundary is the reference's, not the
// specification's.
//
// # Corpus evidence
//
//   - govdocs thread_300_300849.pdf pages 20, 27 and 35: a figure label
//     drawn by a form joined the caption line after it, "(b)Jochen", where
//     PDF.js breaks the line.
func (r *runTracker) breaksItem(i int, el models.TextElement) bool {
	before := r.items[i]
	from, fromIdx := r.last, r.lastIdx
	start := r.start
	r.advance(i, el)
	r.start, r.startIdx = el, i
	if before == opensStream {
		return acrossLine(start, el) > lineBreakTolerance
	}
	prev := r.elements[before]
	size := math.Abs(prev.FontSize)
	gap := computeLineGap(prev, el)
	if r.endsLine(before, i, prev, el, gap, size) {
		return acrossLine(start, el) > min(lineBreakTolerance, sameBaselineRatio*r.height(fromIdx, from))
	}
	if gap > trackingSpaceFactor*size {
		return acrossLine(start, prev) > lineBreakTolerance || acrossLine(prev, el) > lineBreakTolerance
	}
	return acrossLine(start, el) > lineBreakTolerance
}

// spacesItem reports whether a space goes before el, the element i, when
// el begins a text item after a stream boundary on the line of the glyph
// before it, and before is its [textItemStarts]. It only adds spaces: where
// it reports false, the word space rule of every other glyph decides
// ([wordSpaceThreshold], against the element before el), so a form never
// glues its text to the page's.
//
// PDF.js compares the glyph after a form with the glyph before the form,
// and writes a space when the gap between the two passes
// [trackingSpaceFactor] of its size (src/core/evaluator.js,
// compareWithLastPosition, addFakeSpaces and pushWhitespace, since the item
// it would join has ended). litho does the same. It departs from PDF.js at
// the first glyph of a form, which PDF.js compares with nothing and so
// writes no space before, whatever the gap: litho measures that gap by its
// word space rule, and keeps the space a reader sees. Readable text over
// agreement with the reference is a user decision (2026-10-17). Vertical
// and rotated text, and a glyph that ends the line of the glyph before the
// form, are left to the word space rule. ISO 32000-1, 14.8.2.5 leaves word
// breaks to the consumer.
//
// # Corpus evidence
//
//   - pdfjs issue14301.pdf, govdocs thread_100_100591.pdf, pdfcpu
//     CenterOfWhy.pdf: text a form XObject draws beside the page's text
//     reads "Prénom: 1/20", "Information Reports" and "M-2004-002 1",
//     where PDF.js glues it, "Prénom:1/20", so the goldens of those files
//     do not match by decision.
func spacesItem(r *runTracker, before, i int, el models.TextElement) bool {
	if before == opensStream || el.Vertical || isRotated(el.Angle) || isOrthogonalRotation(el.Angle) {
		return false
	}
	prev := r.elements[before]
	if prev.Vertical || isRotated(prev.Angle) || isOrthogonalRotation(prev.Angle) {
		return false
	}
	size := math.Abs(prev.FontSize)
	gap := computeLineGap(prev, el)
	return !r.endsLine(before, i, prev, el, gap, size) && gap > trackingSpaceFactor*size
}

// sameBaselineRatio is how far across the baseline, as a fraction of the
// font size, the text after a line PDF.js flagged as ended may sit from
// the text before the flag and still continue its line, after the
// reference generator (test/integration/corpus/lib/join-lines.mjs,
// sameBaselineRatio).
const sameBaselineRatio = 0.1

// endsLine reports whether el, the element i, ends the line of last, the
// glyph lastIdx painted before it at size, the way PDF.js compares every
// glyph with the one before it whatever run each belongs to
// (compareWithLastPosition): a glyph that moves back along the line, gap
// below [negativeRunGapFactor], ends it past [backwardLineRatio] of the
// size across the baseline, and any other glyph past the whole size, once
// the change of text rise between the two is taken out, so a superscript
// that drops back to the baseline stays on its line. The bound follows the
// font size, so the lines of a tiny font break where a fixed tolerance
// glues them.
func (r *runTracker) endsLine(lastIdx, i int, last, el models.TextElement, gap, size float64) bool {
	height := r.height(lastIdx, last)
	if gap < negativeRunGapFactor*size {
		return acrossLine(last, el) > backwardLineRatio*height
	}
	if quarterTurnLine(last, el) {
		// The text rise of a quarter-turned glyph lifts it along the page
		// X axis, which [runFont] rise does not record, and PDF.js takes
		// no rise out of a move across a rotated line: it scales the rise
		// change by the Y axis of the text transform, zero at a quarter
		// turn (compareWithLastPosition).
		return acrossLine(last, el) > height
	}
	dy := el.Y - last.Y
	if r.fonts != nil {
		dy -= r.fonts[i].rise - r.fonts[lastIdx].rise
	}
	return math.Abs(dy) > height
}

// height returns the size across its line of the font el, the element i,
// was shown in ([runFont] height), or its FontSize when the fonts are
// unknown.
func (r *runTracker) height(i int, el models.TextElement) float64 {
	if r.fonts != nil && r.fonts[i].height > 0 {
		return r.fonts[i].height
	}
	return math.Abs(el.FontSize)
}

// isRunningHeadReset reports whether the transition from prev to el is
// the kind of large, horizontal backward jump that producers emit to
// place a running head (right-aligned page number) on the same line as
// the following body text. The check requires:
//
//   - Both elements are horizontal LTR (no rotation, no vertical
//     writing mode) so RTL scripts and column layouts are excluded.
//   - The gap is more negative than [negativeSpaceFactor] × fontSize so
//     normal kerning never qualifies.
//   - The X-position regresses by at least [negativeSpaceJumpMin]
//     user-space units, which weeds out single-glyph backward shifts
//     that come from microtypography or inline ligature corrections.
//
// The combined check captures the running-head pattern (Federal
// Register-style filings) without dropping the literal space in RTL
// paragraphs or in vertically-set columns.
//
// prev is the last glyph that painted something: after a space glyph the
// caller passes the glyph before it, which PDF.js measures every move from
// (src/core/evaluator.js, compareWithLastPosition, against the position
// of the last visible glyph).
//
// # Corpus evidence
//
//   - govdocs thread_400_400907.pdf page 3: a space glyph at x 533, right of
//     "Original Signature on File", read the "Date:" at x 408 after it as a
//     reset and dropped the word space PDF.js reads ("File Date:"). Reading
//     the reset from "File" makes words match PDF.js on that file and on
//     none fewer; facturx, pdf20, pdfcpu and pdfjs do not move.
func isRunningHeadReset(prev, el models.TextElement, gap float64) bool {
	if prev.Vertical || el.Vertical {
		return false
	}
	if isOrthogonalRotation(prev.Angle) || isOrthogonalRotation(el.Angle) {
		return false
	}
	if isRotated(prev.Angle) || isRotated(el.Angle) {
		return false
	}
	if stringHasRTL(prev.Text) || stringHasRTL(el.Text) {
		return false
	}
	fs := math.Abs(el.FontSize)
	if gap >= negativeSpaceFactor*fs {
		return false
	}
	xJump := prev.X - el.X
	return xJump >= negativeSpaceJumpMin
}

// negativeSpaceFactor and negativeSpaceJumpMin together gate the
// running-head trailing-space drop. The factor is a backward-jump
// cutoff measured as a fraction of effective fontSize; the jump
// minimum guards against single-glyph backward shifts (kerning
// adjustments, italic overhang) that would otherwise satisfy the
// factor on small fonts. See [isRunningHeadReset] for usage.
const (
	negativeSpaceFactor  = -0.5
	negativeSpaceJumpMin = 50.0
)

// ideographicSpace is U+3000, the full-width space of CJK typesetting.
// Producers paint it as a fixed-width spacer (paragraph indents, padding
// between characters), so extraction reads it as a word break.
const ideographicSpace = 0x3000

// notASpaceFactor bounds, as a fraction of the font size, the gap at or
// below which a literal space glyph marks no word break: the next glyph
// starts where the previous one ends, or before it. It mirrors PDF.js's
// NOT_A_SPACE_FACTOR (src/core/evaluator.js, buildTextContentItem), the
// reference litho's extraction is measured against. ISO 32000-1, 14.8.2.5
// leaves word breaks to the consumer once the content stream carries
// none it can trust, so the cutoff is a heuristic, not a spec value.
const notASpaceFactor = 0.03

// exactAbutment is the gap, in user-space units, below which the next
// glyph starts exactly where the previous one ends. A producer that sets
// each word by its own Tm at the computed end of the previous glyph, and
// still shows a space between them, signals the break by the space
// alone, so the space stays. PDF.js keeps it on the same condition
// (compareWithLastPosition returns before the NOT_A_SPACE test when the
// two positions are equal), for one space glyph: after two, the last two
// characters it saw are spaces and it writes none (saveLastChar). The
// bound absorbs float rounding only.
const exactAbutment = 1e-6

// isSpaceGlyph reports whether s is made only of whitespace, the characters
// PDF.js reads as a space glyph ([isWhitespaceRune]).
//
// # Corpus evidence
//
//   - govdocs thread_200_200926.pdf: page numbers "-3-" set as "3", a
//     glyph mapped to a line feed, then "-" moved back onto the end of "3"
//     read "-3 -", where PDF.js reads the line feed as a space glyph the
//     move cancels.
//   - pdfcpu TheGoProgrammingLanguageCh1.pdf: headings set with U+00A0
//     between letters, each moved back over the space, read "Cont ents".
//   - All corpora: reading every whitespace character as a space glyph makes
//     words match PDF.js on 6 more files (govdocs 3, pdfjs 2, pdfcpu 1) and
//     on 2 more pages of files that still differ (govdocs
//     thread_000_000150.pdf, thread_100_100896.pdf), and on none fewer;
//     facturx and pdf20 do not move.
func isSpaceGlyph(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !isWhitespaceRune(r) {
			return false
		}
	}
	return true
}

// isWhitespaceRune reports whether r is whitespace as PDF.js reads a glyph's
// character: the JavaScript \s class (ECMA-262, CharacterClassEscape, the
// WhiteSpace and LineTerminator characters), which PDF.js tests the
// character with (src/core/unicode.js, getCharUnicodeCategory). Its text
// builder shows no such glyph: it advances the text position by it and reads
// it as a space, which the next glyph keeps or cancels
// (src/core/evaluator.js, buildTextContentItem). ISO 32000-1, 14.8.2.5 leaves
// word breaks to the consumer, so the set is the reference's.
func isWhitespaceRune(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xA0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, ideographicSpace, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}

// suppressesLiteralSpace reports whether the literal space glyphs shown
// between prev and el, spaces of them, mark no word break. The next glyph
// starts at or before the end of the last painted glyph (gap at most
// [notASpaceFactor] × size, the size [literalSpaceSize] measures the gap
// in), so the space advanced the text matrix and the producer moved it
// back over itself: the two glyphs abut on the page and no reader sees a
// gap between them.
//
// After two space glyphs or more the bound is [trackingSpaceFactor] × size,
// the gap PDF.js reads as tracking: below it PDF.js writes a space only
// when the last two characters it saw are a glyph and one space
// (src/core/evaluator.js, compareWithLastPosition, shouldAddWhitepsace and
// saveLastChar), so the next glyph, moved back over the spaces to within
// tracking of the text before them, is read without one.
//
// The gap is only read for left-to-right text at a positive font size,
// horizontal or rotated by 90° or -90°, between two glyphs set in the same
// direction: [computeLineGap] measures it along the advance of those
// ([gapReadable]). Vertical, otherwise rotated, mirrored and right-to-left
// runs measure their gaps along other axes or in visual order, and keep
// their literal spaces. So does an exact abutment after one space glyph
// ([exactAbutment]).
//
// # Corpus evidence
//
//   - pdfjs SimFang-variant.pdf, XiaoBiaoSong.pdf: every Chinese glyph is
//     shown with a trailing space by a Td that lands on the next glyph,
//     so emitting the spaces read a space after each character.
//   - pdfjs issue15139.pdf: "2", " ", "/", " ", "2" each placed by its own
//     Tm, one em apart, read "2 / 2" for the "2/2" the page shows.
//   - facturx: 26 invoices end "von" and a date with a space the next Tm
//     overprints, where PDF.js reads "von1" and "05.03.2018Nr.".
//   - All corpora: words match PDF.js on 83 more files (facturx 26, govdocs
//     43, pdfcpu 2, pdfjs 12) and on none fewer.
//   - govdocs thread_100_100451.pdf (drop cap), pdfjs prefilled_f1040.pdf
//     (exact abutment), issue269_2.pdf (180° text), issue14415.pdf (negative
//     font size), issue9186.pdf (U+200B around each space): each lost
//     spaces PDF.js keeps until the guard above that names its case.
//   - govdocs thread_000_000012.pdf, thread_000_000750.pdf and 6 more: text
//     rotated a quarter turn read a space PDF.js drops ("$500,000y").
//   - govdocs thread_200_200396.pdf: a tick label "100" and the rotated "y"
//     after it read glued until both glyphs had to share a direction.
//   - pdfjs issue20232.pdf: "и дата", rotated a quarter turn, read "идата"
//     while the widths of its UTF-8 codes were read at the code, not the CID.
//   - All corpora: dropping the space in ±90° text makes words match PDF.js
//     on 8 more files and on none fewer.
//   - govdocs thread_000_000338.pdf page 5: "References", a space glyph
//     ending its TJ, another at the next Td, then the leader dots moved back
//     to 0.08 em after the "s", read "References ...." for PDF.js's
//     "References....".
//   - govdocs thread_000_000208.pdf page 69: "effi", two space glyphs, then
//     "ciency" at the exact end of the ligature, read "effi ciency".
//   - All corpora: the tracking bound after two spaces or more makes words
//     match PDF.js on 5 more files (govdocs 4, pdfjs 1) and on none fewer;
//     facturx, pdf20 and pdfcpu do not move.
func suppressesLiteralSpace(prev, el models.TextElement, gap, size float64, spaces int) bool {
	if prev.Vertical || el.Vertical || !gapReadable(el.Angle) || math.Abs(prev.Angle-el.Angle) >= math.Pi/180 {
		return false
	}
	if stringHasRTL(prev.Text) || stringHasRTL(el.Text) {
		return false
	}
	// A format character (U+200B ZERO WIDTH SPACE and the rest of Unicode
	// category Cf) paints nothing, so it does not anchor a gap.
	if isFormatOnly(prev.Text) || isFormatOnly(el.Text) {
		return false
	}
	// A negative font size runs the glyphs leftward (ISO 32000-1, 9.4.4),
	// so the gap, read rightward, says nothing about whether they abut.
	if prev.FontSize <= 0 || el.FontSize <= 0 {
		return false
	}
	if spaces >= 2 {
		return gap <= trackingSpaceFactor*size
	}
	if math.Abs(gap) < exactAbutment {
		return false
	}
	return gap <= notASpaceFactor*size
}

// spaceGlyphsBetween counts the space glyphs among the elements strictly
// between p and i.
func spaceGlyphsBetween(elems []models.TextElement, p, i int) int {
	n := 0
	for k := p + 1; k < i; k++ {
		if isSpaceGlyph(elems[k].Text) {
			n++
		}
	}
	return n
}

// literalSpaceSize returns the font size, on the page, that the gap between
// the element at p, the last painted glyph before a literal space, and the
// one at i is measured against, the elements of one line and their fonts
// (nil when unknown). PDF.js compares that gap with NOT_A_SPACE_FACTOR
// times the Tf size of the text item p belongs to, in the text space of the
// glyph at i: a Tm that rescales the text rescales the item before the
// glyph is compared (src/core/evaluator.js, updateAdvanceScale and
// compareWithLastPosition). So the size is the Tf size of p at the scale
// the text matrix gives i, as for a word space ([wordSpaceThreshold]).
// Across a marked-content boundary, which ends the item, and without fonts,
// the smaller size of the two stands: it keeps a drop cap from swallowing
// the space after it.
//
// # Corpus evidence
//
//   - govdocs thread_300_300829.pdf: "c", a space, then "j" set by a Tm
//     at 21.3 points after 11.8: the 0.5-point gap passed 0.03 of the
//     smaller size and kept a space PDF.js drops ("cj").
//   - govdocs thread_400_400566.pdf page 4: "5", a space, then a "." set
//     at 14.4 points after 9.75, read "5 ." for "5.".
//   - All corpora: words match PDF.js on 2 more files (both govdocs) and
//     on 2 more pages of files that still differ (govdocs
//     thread_000_000282.pdf, thread_300_300695.pdf), and on none fewer;
//     facturx, pdf20, pdfcpu and pdfjs do not move.
func literalSpaceSize(elems []models.TextElement, fonts []runFont, p, i int) float64 {
	prev, el := elems[p], elems[i]
	if fonts != nil && prev.MCID == el.MCID {
		prevSize, size := math.Abs(fonts[p].size), math.Abs(fonts[i].size)
		if prevSize > 0 && size > 0 {
			return prevSize * el.FontSize / size
		}
	}
	return min(prev.FontSize, el.FontSize)
}

// gapReadable reports whether [computeLineGap] measures a gap along the
// direction text at angle advances: horizontal text, and text rotated by 90°
// or -90°. The gap means that only between two glyphs set in the same
// direction.
func gapReadable(angle float64) bool {
	return math.Abs(angle) < math.Pi/180 || isOrthogonalRotation(angle)
}

// isFormatOnly reports whether s is made only of format characters
// (Unicode category Cf).
func isFormatOnly(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}

// lineHasNonspacingMark reports whether any element of a line carries a
// nonspacing mark (Unicode category Mn). Producers that shape such text
// (Thai vowel and tone marks, Indic matras) place the glyph after a mark
// by its shaped position, while the text matrix advances by the mark's
// /Widths entry (ISO 32000-1, 9.4.4). The two disagree, so a gap read
// from the advance on such a line says nothing about whether glyphs
// abut, and [suppressesLiteralSpace] is not consulted.
//
// # Corpus evidence
//
//   - pdfjs issue13211.pdf: Thai syllables placed by Tm overlap their
//     predecessor by up to 1.4 em in the advance litho computes, though
//     the page shows a word space between them.
func lineHasNonspacingMark(elems []models.TextElement) bool {
	for _, el := range elems {
		for _, r := range el.Text {
			if unicode.Is(unicode.Mn, r) {
				return true
			}
		}
	}
	return false
}

// heldSpace returns the space a held run of whitespace glyphs s is written
// as: a space, a tab or an [ideographicSpace] as itself, and any other
// whitespace, a line feed or U+00A0 among them, as U+0020, as PDF.js writes
// every whitespace glyph (src/core/evaluator.js, buildTextContentItem).
func heldSpace(s string) string {
	for _, r := range s {
		if r != ' ' && r != '\t' && r != ideographicSpace {
			return " "
		}
	}
	return s
}

// writeSpace writes the held space glyph s to sb, unless sb already ends
// in whitespace. Collapsing keeps two literal spaces, or
// a space next to an inferred one, from leaking into the output as two.
func writeSpace(sb *strings.Builder, s string) {
	if s == "" {
		return
	}
	if b := sb.String(); len(b) > 0 && isWhitespaceByte(b[len(b)-1]) {
		return
	}
	sb.WriteString(s)
}

func isWhitespaceByte(b byte) bool {
	return b == ' ' || b == '\t'
}

// stringHasRTL reports whether s contains any right-to-left script
// codepoint. Used by [isRunningHeadReset] to skip the trailing-space
// drop on RTL paragraphs where X naturally decreases as glyphs
// progress.
func stringHasRTL(s string) bool {
	for _, r := range s {
		if isRTL(r) {
			return true
		}
	}
	return false
}

// reconstructWithBeads reads a page whose text the beads of its article
// threads cover ([ReconstructText]), elements paired with fonts as for
// [reconstructText]. An article orders its beads, not the text inside one
// (ISO 32000-1, 12.4.3): the text of each bead is read in content-stream
// order, as a page without beads is, and the beads follow one another in
// thread order, a blank line between two. An element in no bead follows
// them all.
//
// # Corpus evidence
//
//   - govdocs thread_200_200417.pdf page 1: one bead covers the page.
//     Sorting its text into rows 5 units tall put each superscript "1" on
//     the line of the author name before it and glued them, "Xu1,", where
//     PDF.js, which reads no beads, breaks the line at each. Reading the
//     text inside a bead in content order makes words match PDF.js on that
//     file and on none fewer; facturx, pdf20, pdfcpu and pdfjs do not move.
func reconstructWithBeads(ctx context.Context, elements []models.TextElement, fonts []runFont, beads []semantics.Bead) string {
	if len(fonts) != len(elements) {
		fonts = nil
	}
	groups := make([][]int, len(beads)+1)
	for i, el := range elements {
		group := len(beads)
		for b, bead := range beads {
			if beadContains(bead, el) {
				group = b
				break
			}
		}
		groups[group] = append(groups[group], i)
	}
	var sb strings.Builder
	for _, group := range groups {
		if len(group) == 0 {
			continue
		}
		if ctx.Err() != nil {
			return ""
		}
		groupElements := make([]models.TextElement, len(group))
		var groupFonts []runFont
		if fonts != nil {
			groupFonts = make([]runFont, len(group))
		}
		for k, i := range group {
			groupElements[k] = elements[i]
			if fonts != nil {
				groupFonts[k] = fonts[i]
			}
		}
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(reconstructText(ctx, groupElements, groupFonts, nil))
	}
	return sb.String()
}

// beadContains reports whether the origin of el lies in the rectangle of
// bead, whichever corners the rectangle names.
func beadContains(bead semantics.Bead, el models.TextElement) bool {
	minX, maxX := min(bead.Rect[0], bead.Rect[2]), max(bead.Rect[0], bead.Rect[2])
	minY, maxY := min(bead.Rect[1], bead.Rect[3]), max(bead.Rect[1], bead.Rect[3])
	return el.X >= minX && el.X <= maxX && el.Y >= minY && el.Y <= maxY
}

// computeLineGap computes the inter-glyph gap (delta - prevWidth) for a pair,
// with delta measured along the direction the text advances. Vertical
// writing advances down the page (ISO 32000-1, 9.7.4.3, a negative w1), and
// text rotated by 90° advances up it or, rotated by -90°, down it, so the
// sign of the advance's Y component decides which way a gap is positive.
// Text turned upside down, rotated by 180° ([isHalfTurn]), advances leftward
// along the page X axis, so its gap is positive when the next glyph sits left
// of the end of the previous one. PDF.js measures the same advance in the
// text's own direction (src/core/evaluator.js, compareWithLastPosition,
// which swaps and negates the coordinates for each orthogonal rotation and
// negates both for a half turn).
//
// A width of 0 is a glyph the font gives no advance (a /W or /Widths entry
// of 0, ISO 32000-1, 9.6.2.1 and 9.7.4.3), so the gap is measured from its
// own position. Callers measure the glyph after it from the last glyph that
// advanced instead ([runTracker.anchor]), as PDF.js does. No element is ever
// 0 wide for want of metrics: [Font.GlyphWidth] falls back to a non-zero
// width. Only a negative width, which no glyph is drawn with, takes half an
// em.
//
// # Corpus evidence
//
//   - govdocs thread_100_100518.pdf, thread_000_000744.pdf and 7 more: chart
//     labels and table headings set upside down read every gap as negative,
//     so no word space was ever inserted: "TransferCargo", and "Mid-Atlantic"
//     for a heading tracked letter by letter that PDF.js reads
//     "M i d - A t l a n t i c".
//     Reading the gap leftward makes words match PDF.js on 6 more files (all
//     govdocs) and on none fewer; facturx, pdf20, pdfcpu and pdfjs do not
//     move.
//   - pdfjs issue5747.pdf, govdocs thread_000_000262.pdf and 9 more: text
//     rotated by -90° read every gap as negative, so no word space was ever
//     inserted ("dolorsitamet,").
//   - pdfjs issue20930.pdf, bug1905623.pdf: vertical writing read "ABCDE"
//     for glyphs PDF.js reads "A B C D E".
//   - govdocs thread_300_300516.pdf: a /blank glyph states /Widths 0 and a
//     TJ gap of 0.287 em follows it. Half an em taken off that gap lost the
//     space PDF.js reads ("Jeff␣Sherwood").
//   - pdfjs issue7020.pdf: half an em taken off the gap after a /Widths 0
//     glyph lost the space PDF.js reads ("æè").
func computeLineGap(prev, curr models.TextElement) float64 {
	var delta float64
	switch {
	case curr.Vertical:
		delta = prev.Y - curr.Y
	case isOrthogonalRotation(curr.Angle):
		delta = curr.Y - prev.Y
		if math.Sin(curr.Angle) < 0 {
			delta = -delta
		}
	case isRotated(curr.Angle):
		delta = alongDist(prev, curr)
	case isHalfTurn(curr.Angle):
		delta = prev.X - curr.X
	default:
		delta = curr.X - prev.X
	}
	if prev.Width >= 0 {
		return delta - prev.Width
	}
	return delta - 0.5*curr.FontSize
}

// computeSpaceThreshold returns the inter-glyph gap above which a real
// space is inserted. Earlier versions tried to detect a bimodal gap
// distribution per line and pick the threshold adaptively. In
// practice the adaptive rule overshot the natural cutoff and dropped
// real inter-word spaces on lines that mixed kerning with word
// breaks. A fixed 0.102 × fontSize fraction (the canonical tracking
// cutoff for typeset text) is authoritative regardless of the line's
// gap histogram, so the threshold is a constant fraction of the
// line's average font size. Content-stream order reads it only where the
// fonts of the line are unknown ([wordSpaceThreshold]).
func computeSpaceThreshold(elems []models.TextElement, indices []int) float64 {
	if len(indices) == 0 {
		return 1.0
	}
	if len(indices) < 3 {
		return 0.102 * math.Abs(elems[indices[0]].FontSize)
	}
	var totalFontSize float64
	for i := 1; i < len(indices); i++ {
		totalFontSize += elems[indices[i]].FontSize
	}
	avgFontSize := math.Abs(totalFontSize / float64(len(indices)-1))
	return 0.102 * avgFontSize
}

// trackingSpaceFactor is the fraction of the font size at or below which a
// gap between two glyphs is tracking, not a word space. It mirrors PDF.js's
// TRACKING_SPACE_FACTOR (src/core/evaluator.js), the reference litho's
// extraction is measured against; ISO 32000-1, 14.8.2.5 leaves word breaks
// to the consumer, so the value is a heuristic, not a spec value.
const trackingSpaceFactor = 0.102

// wordSpaceThreshold returns the gap above which a word space separates the
// element at i from the one at p, the elements of one line and their fonts
// (nil when unknown). PDF.js measures the gap in the text space of the text
// item the previous glyph belongs to and compares it with
// [trackingSpaceFactor] times the size Tf set for that item
// (compareWithLastPosition, ensureTextContentItem), so the threshold follows
// the glyphs on either side of the gap, not the average size of the line:
//
//   - Within an item the size is the Tf operand, scaled by the text matrix
//     in force when the glyph is shown, since a Tm that changes the scale
//     rescales the item (updateAdvanceScale). A Tf that selects another size
//     or font starts a new item only once the glyph is shown, so the gap
//     before it still takes the previous size, at the new glyph's scale.
//   - A marked-content boundary ends the item before the operators that
//     follow it, so a Tm after it no longer rescales anything: the gap takes
//     the previous glyph's size and scale, its size on the page. That holds
//     at every BMC, BDC and EMC ([runFont] marked), whether or not the
//     sequence carries an MCID (src/core/evaluator.js, getTextContent, which
//     flushes the item there and leaves its metrics for the next glyph's
//     comparison).
//
// Without fonts, lineThreshold, the line's [computeSpaceThreshold], stands.
//
// # Corpus evidence
//
//   - pdfjs issue17232.pdf: "__/__/____" sets each underscore run and slash
//     by its own Tm and marked-content sequence, at 25.6 and 8 points; the
//     slash's size put a space PDF.js does not read after each run.
//   - govdocs thread_300_300083.pdf: small capitals set by a 9-point Tm read
//     "THEM ...." where the leaders' 11-point Tm keeps PDF.js at "THEM....".
//   - All corpora: words match PDF.js on 33 more files (govdocs 28, pdfjs 5)
//     and on none fewer, the line average having spaced or joined glyphs set
//     in a size of their own.
//   - govdocs thread_000_000152.pdf page 38: a 13-point "^" then "Riverine"
//     at 7.4 points, each in a marked-content sequence of its own with no
//     MCID, read "^ Riverine" against the 7.4-point size, where PDF.js
//     measures the gap at 13 points and reads "^Riverine". Taking the size
//     of the glyph before a marked-content operator makes words match PDF.js
//     on that page and on none fewer; no file moves.
func wordSpaceThreshold(elems []models.TextElement, fonts []runFont, p, i int, lineThreshold float64) float64 {
	prev, el := elems[p], elems[i]
	if prev.MCID != el.MCID || (fonts != nil && fonts[p].marked != fonts[i].marked) {
		return trackingSpaceFactor * math.Abs(prev.FontSize)
	}
	if fonts == nil {
		return lineThreshold
	}
	prevSize, size := math.Abs(fonts[p].size), math.Abs(fonts[i].size)
	if prevSize == 0 || size == 0 {
		return lineThreshold
	}
	return trackingSpaceFactor * prevSize * math.Abs(el.FontSize) / size
}

// needsSpace decides whether to insert a space between two consecutive glyphs.
func needsSpace(gap, threshold float64) bool {
	return gap > threshold
}

// chunkKerningLimit returns the maximum gap that still counts as
// intra-chunk kerning for [ReconstructText]. A same-chunk gap at or
// below this limit never produces a space; a larger gap falls back to
// the adaptive space heuristic so big TJ adjustments inside a single
// operator chain still surface as visible breaks. The 0.102 factor is
// the canonical tracking cutoff so tracked text (positive Tc with
// compensating TJ shifts) merges as a single chunk.
func chunkKerningLimit(prev models.TextElement) float64 {
	const kerningFactor = 0.102
	return kerningFactor * math.Abs(prev.FontSize)
}

// shouldInsertSpace combines the adaptive per-line threshold with the
// chunk-aware kerning floor. Inside a chunk, only gaps above the
// kerning floor can produce a space; across chunks the threshold
// alone decides, matching the behaviour before chunk tracking landed.
func shouldInsertSpace(prev models.TextElement, gap, spaceThreshold float64, sameChunk bool) bool {
	if sameChunk && gap <= chunkKerningLimit(prev) {
		return false
	}
	return needsSpace(gap, spaceThreshold)
}

// isOrthogonalRotation returns true when the text angle is close to 90° or
// 270° (±π/2). These are not "rotated" in the arbitrary sense. They are
// orthogonal text that progresses along the Y axis instead of X.
func isOrthogonalRotation(angle float64) bool {
	const threshold = math.Pi / 180 // 1 degree
	a := math.Abs(angle)
	return math.Abs(a-math.Pi/2) < threshold || math.Abs(a-3*math.Pi/2) < threshold
}

// isRotated returns true when the text angle deviates significantly from
// horizontal (0) or vertical (±π/2). A 1° threshold avoids false positives
// from minor floating-point drift in purely horizontal/vertical text.
func isRotated(angle float64) bool {
	const threshold = math.Pi / 180 // 1 degree
	a := math.Abs(angle)
	if a < threshold {
		return false // ~horizontal
	}
	if math.Abs(a-math.Pi/2) < threshold {
		return false // ~vertical (WMode=1 handled separately)
	}
	if math.Abs(a-math.Pi) < threshold {
		return false // ~180° (mirrored horizontal)
	}
	return true
}

// isHalfTurn reports whether the text angle is close to 180°: text turned
// upside down, whose glyphs advance leftward along the page X axis.
func isHalfTurn(angle float64) bool {
	const threshold = math.Pi / 180 // 1 degree
	return math.Abs(math.Abs(angle)-math.Pi) < threshold
}

// quarterTurnLine reports whether a and b are set along the same line
// direction rotated a quarter turn, 90° or -90°, so their lines run along
// the page Y axis.
func quarterTurnLine(a, b models.TextElement) bool {
	return !a.Vertical && !b.Vertical && isOrthogonalRotation(a.Angle) && isOrthogonalRotation(b.Angle) &&
		math.Abs(a.Angle-b.Angle) < math.Pi/180
}

// acrossLine returns how far b sits from a across the line they are set
// on: along the page X axis for two glyphs set a quarter turn round
// ([quarterTurnLine]), along the page Y axis otherwise. PDF.js swaps the
// axes of a quarter-turned line before it compares two glyphs
// (src/core/evaluator.js, compareWithLastPosition), and the reference
// generator measures a rotated line along the normal of its baseline
// (test/integration/corpus/lib/join-lines.mjs, acrossLine), so both read
// the X distance there.
func acrossLine(a, b models.TextElement) float64 {
	if quarterTurnLine(a, b) {
		return math.Abs(b.X - a.X)
	}
	return math.Abs(b.Y - a.Y)
}

// perpDist returns the absolute distance between two elements projected onto
// the perpendicular of the average text direction. This replaces the naive
// |ΔY| check for rotated text.
func perpDist(a, b models.TextElement) float64 {
	angle := (a.Angle + b.Angle) / 2
	sinA := math.Sin(angle)
	cosA := math.Cos(angle)
	// Perpendicular direction: (-sin θ, cos θ)
	pa := -a.X*sinA + a.Y*cosA
	pb := -b.X*sinA + b.Y*cosA
	return math.Abs(pa - pb)
}

// alongDist returns the signed distance between two elements projected onto
// the text direction vector. Positive means b is ahead of a in reading order.
func alongDist(a, b models.TextElement) float64 {
	angle := (a.Angle + b.Angle) / 2
	cosA := math.Cos(angle)
	sinA := math.Sin(angle)
	// Along direction: (cos θ, sin θ)
	da := a.X*cosA + a.Y*sinA
	db := b.X*cosA + b.Y*sinA
	return db - da
}

// advances reports whether el moves the position PDF.js measures the next
// glyph from: a glyph with an advance whose character holds no nonspacing
// mark (src/core/evaluator.js, buildTextContentItem, prevTransform, and
// src/core/unicode.js, getCharUnicodeCategory, isZeroWidthDiacritic).
func advances(el models.TextElement) bool {
	return el.Width != 0 && !hasNonspacingMark(el.Text)
}

// hasNonspacingMark reports whether s holds a nonspacing mark (Unicode
// category Mn), anywhere in it, as PDF.js tests a glyph's character.
func hasNonspacingMark(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Mn, r) {
			return true
		}
	}
	return false
}

// markAdvances records, for a sequence of elements, how far the nonspacing
// marks among them advanced the text position. PDF.js advances past no such
// mark (src/core/evaluator.js, buildTextContentItem, which zeroes the advance
// of a zero-width diacritic), so it reads every glyph after a mark, until a
// text-positioning operator sets the position again, that far before where
// litho reads it. It is built once per sequence so each answer costs no walk
// of the elements between.
type markAdvances struct {
	// sum holds, at k, the widths of the marks among the first k elements.
	sum []float64
	// chunkStart holds, for each element, the index of the first element of
	// the run of consecutive elements that share its chunk, the glyphs shown
	// since a text-positioning operator last set the position.
	chunkStart []int
}

// newMarkAdvances indexes the nonspacing marks of elements.
func newMarkAdvances(elements []models.TextElement) markAdvances {
	m := markAdvances{sum: make([]float64, len(elements)+1), chunkStart: make([]int, len(elements))}
	for k, e := range elements {
		m.sum[k+1] = m.sum[k]
		if hasNonspacingMark(e.Text) {
			m.sum[k+1] += e.Width
		}
		if k > 0 && elements[k-1].Chunk == e.Chunk {
			m.chunkStart[k] = m.chunkStart[k-1]
		} else {
			m.chunkStart[k] = k
		}
	}
	return m
}

// shift returns how far before litho's position PDF.js reads the element k:
// the advance of the marks before it in its run. It returns 0 for an index
// out of range.
func (m markAdvances) shift(k int) float64 {
	if k < 0 || k >= len(m.chunkStart) {
		return 0
	}
	return m.sum[k] - m.sum[m.chunkStart[k]]
}

// drift returns how much farther along the line litho reads the element b
// from the element a than PDF.js does, the amount a gap between the two is
// corrected by.
func (m markAdvances) drift(a, b int) float64 {
	return m.shift(b) - m.shift(a)
}
