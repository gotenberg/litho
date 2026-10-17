package typography

import (
	"context"
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/gotenberg/litho/pkg/internal/semantics"
	"github.com/gotenberg/litho/pkg/models"
)

// ReconstructText takes spatial text elements and reassembles them into a string.
// When no article thread beads are provided, it preserves content stream order
// for maximum fidelity to the original document layout. When beads are provided,
// it sorts elements by bead index to respect explicit multi-column reading order.
func ReconstructText(ctx context.Context, elements []models.TextElement, beads []semantics.Bead) string {
	return reconstructText(ctx, elements, nil, beads)
}

// reconstructText is [ReconstructText] with the font each element was shown
// in, parallel to elements, so a change of font starts a new run the way it
// does in PDF.js. Without fonts (nil, or not one per element) only a change
// of size does.
func reconstructText(ctx context.Context, elements []models.TextElement, fonts []runFont, beads []semantics.Bead) string {
	if len(elements) == 0 {
		return ""
	}

	// When beads are present, use bead-aware spatial sorting.
	if len(beads) > 0 {
		return reconstructWithBeads(ctx, elements, beads)
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
	runs := runTracker{startY: elements[0].Y, last: elements[0], fonts: fonts}

	for i := 1; i < len(elements); i++ {
		if i%500 == 0 {
			if ctx.Err() != nil {
				return ""
			}
		}
		prev := elements[i-1]
		el := elements[i]
		var newLine bool
		switch {
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
			newLine = runs.breaksLine(i, prev, el)
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
	for lineIdx, ln := range lines {
		if lineIdx > 0 {
			sb.WriteString("\n")
		}
		lineFonts := fonts
		if lineFonts != nil {
			lineFonts = fonts[lineStart : lineStart+len(ln.elems)]
		}
		lineStart += len(ln.elems)

		// Bidi reorder: reverse RTL runs that are in visual order (X
		// decreasing) to restore logical reading order.
		texts := make([]string, len(ln.elems))
		xPos := make([]float64, len(ln.elems))
		indices := make([]int, len(ln.elems))
		for i, el := range ln.elems {
			texts[i] = el.Text
			xPos[i] = el.X
			indices[i] = i
		}
		indices = reorderBidiIndices(texts, indices, xPos)

		// Compute adaptive space threshold from the gap histogram.
		spaceThreshold := computeSpaceThreshold(ln.elems, indices)
		hasMarks := lineHasNonspacingMark(ln.elems)

		// Phantom anchors: unresolved glyphs reach this point with
		// Text == "". They still occupy layout space, so their
		// positions and widths anchor the gap computation between the
		// surrounding resolved runs. Walk the line, tracking the
		// previous emitted element for the space heuristic, and let
		// phantoms push that anchor forward without contributing
		// characters or triggering space insertion.
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
		// prevIdx is the previous element, space glyphs included; visIdx
		// the previous element that is not a space glyph.
		prevIdx, visIdx := -1, -1
		pendingSpace := ""
		for _, idx := range indices {
			el := ln.elems[idx]
			if el.Text == "" {
				prevIdx, visIdx = idx, idx
				continue
			}
			if isSpaceGlyph(el.Text) {
				if pendingSpace == "" {
					pendingSpace = el.Text
				}
				prevIdx = idx
				continue
			}
			if prevIdx >= 0 {
				prev := ln.elems[prevIdx]
				gap := computeLineGap(prev, el)
				sameChunk := prev.Chunk == el.Chunk && el.Chunk != 0
				switch {
				case prev.Text != "" && isRunningHeadReset(prev, el, gap):
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
					if vis := ln.elems[visIdx]; vis.Text != "" && suppressesLiteralSpace(vis, el, computeLineGap(vis, el)) {
						pendingSpace = ""
					}
				case prev.Text != "" && shouldInsertSpace(prev, gap, wordSpaceThreshold(ln.elems, lineFonts, prevIdx, idx, spaceThreshold), sameChunk):
					sb.WriteString(" ")
				}
			}
			writeSpace(&sb, pendingSpace)
			pendingSpace = ""
			sb.WriteString(el.Text)
			prevIdx, visIdx = idx, idx
		}
		writeSpace(&sb, pendingSpace)
	}

	return normalizeExtractionUnicode(normalizeSoftHyphens(sb.String()))
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
// the previous glyph's size starts a new run (VERTICAL_SHIFT_RATIO), a gap
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
// line in proportion to the font size rather than at a fixed distance.
//
// A glyph continues the run of the glyph painted before it unless its
// baseline moves by more than [verticalShiftRatio] of that glyph's size,
// its size changes, its marked-content sequence changes, or the gap
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
// the text matrix), so they neither start nor continue a run. One breaks the
// line only when it sits more than the tolerance across the baseline from
// the element before it, which keeps a space with the text it was shown
// beside.
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
type runTracker struct {
	startY float64
	last   models.TextElement
	// lastIdx indexes the glyph in last, to look up its font.
	lastIdx int
	// fonts is the font of each element, or nil when unknown.
	fonts []runFont
}

// runFont names the font an element was shown in: the resource name and
// the size Tf selected, and the font the name resolved to. It also carries
// rise, how far up the page the text rise (ISO 32000-1, 9.3.7) lifted the
// element, which its position already includes, and height, the size of
// the font across the line on the page: the Tf size scaled by the text
// space y axis, where the element's FontSize is scaled by the x axis.
type runFont struct {
	name   string
	font   *Font
	size   float64
	rise   float64
	height float64
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

// restart begins a new run at the element i, for a line the horizontal
// rule did not decide.
func (r *runTracker) restart(i int, el models.TextElement) {
	r.startY = el.Y
	r.last = el
	r.lastIdx = i
}

// breaksLine reports whether el, the element i, starts a new line after
// prev, the element before it, and advances the run state past it. A run
// is compared with the last glyph that was not a space, which prev need not
// be.
func (r *runTracker) breaksLine(i int, prev, el models.TextElement) bool {
	if isSpaceGlyph(el.Text) {
		return math.Abs(el.Y-prev.Y) > lineBreakTolerance
	}
	last, lastIdx := r.last, r.lastIdx
	fontChanged := r.fontChanged(i, last, el)
	r.last = el
	r.lastIdx = i
	size := math.Abs(last.FontSize)
	gap := computeLineGap(last, el)
	if r.endsLine(lastIdx, i, last, el, gap, size) {
		r.startY = el.Y
		return true
	}
	ref := r.startY
	switch {
	case gap > spaceInFlowMaxFactor*size:
		ref = last.Y
	case gap < negativeRunGapFactor*size,
		math.Abs(el.Y-last.Y) > verticalShiftRatio*size,
		fontChanged,
		el.MCID != last.MCID:
	default:
		return false
	}
	r.startY = el.Y
	return math.Abs(el.Y-ref) > lineBreakTolerance
}

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
	dy := el.Y - last.Y
	height := r.height(lastIdx, last)
	if gap < negativeRunGapFactor*size {
		return math.Abs(dy) > backwardLineRatio*height
	}
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
// two positions are equal). The bound absorbs float rounding only.
const exactAbutment = 1e-6

// isSpaceGlyph reports whether s is made only of space glyphs: U+0020,
// a tab, or [ideographicSpace].
func isSpaceGlyph(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r != ' ' && r != '\t' && r != ideographicSpace {
			return false
		}
	}
	return true
}

// suppressesLiteralSpace reports whether a literal space glyph shown
// between prev and el marks no word break. The next glyph starts at or
// before the end of the last painted glyph (gap at most
// [notASpaceFactor] × the smaller font size), so the space advanced the
// text matrix and the producer moved it back over itself: the two glyphs
// abut on the page and no reader sees a gap between them. The smaller
// size keeps a drop cap from swallowing the space after it.
//
// The gap is only read for left-to-right text at a positive font size,
// horizontal or rotated by 90° or -90°, between two glyphs set in the same
// direction: [computeLineGap] measures it along the advance of those
// ([gapReadable]). Vertical, otherwise rotated, mirrored and right-to-left
// runs measure their gaps along other axes or in visual order, and keep
// their literal spaces. So does an exact abutment ([exactAbutment]).
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
func suppressesLiteralSpace(prev, el models.TextElement, gap float64) bool {
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
	if math.Abs(gap) < exactAbutment {
		return false
	}
	return gap <= notASpaceFactor*min(prev.FontSize, el.FontSize)
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

// reconstructWithBeads handles the bead-aware spatial sorting path for PDFs
// that have article threads defining explicit reading order.
func reconstructWithBeads(ctx context.Context, elements []models.TextElement, beads []semantics.Bead) string {
	type elementWithBead struct {
		models.TextElement
		BeadIndex int
	}

	// Detect whether the page contains vertical text.
	verticalCount := 0
	for _, el := range elements {
		if el.Vertical {
			verticalCount++
		}
	}
	isVerticalPage := verticalCount > len(elements)/2

	ewb := make([]elementWithBead, len(elements))
	for i, el := range elements {
		assignedBead := -1
		for bIdx, bead := range beads {
			minX := bead.Rect[0]
			maxX := bead.Rect[2]
			if minX > maxX {
				minX, maxX = maxX, minX
			}
			minY := bead.Rect[1]
			maxY := bead.Rect[3]
			if minY > maxY {
				minY, maxY = maxY, minY
			}

			if el.X >= minX && el.X <= maxX && el.Y >= minY && el.Y <= maxY {
				assignedBead = bIdx
				break
			}
		}
		ewb[i] = elementWithBead{TextElement: el, BeadIndex: assignedBead}
	}

	if isVerticalPage {
		sort.SliceStable(ewb, func(i, j int) bool {
			if ewb[i].BeadIndex != ewb[j].BeadIndex {
				if ewb[i].BeadIndex == -1 {
					return false
				}
				if ewb[j].BeadIndex == -1 {
					return true
				}
				return ewb[i].BeadIndex < ewb[j].BeadIndex
			}
			if ewb[i].X > ewb[j].X+2.0 {
				return true
			} else if ewb[i].X < ewb[j].X-2.0 {
				return false
			}
			return ewb[i].Y > ewb[j].Y
		})
	} else {
		sort.SliceStable(ewb, func(i, j int) bool {
			if ewb[i].BeadIndex != ewb[j].BeadIndex {
				if ewb[i].BeadIndex == -1 {
					return false
				}
				if ewb[j].BeadIndex == -1 {
					return true
				}
				return ewb[i].BeadIndex < ewb[j].BeadIndex
			}
			// Y-tolerance sized for sub/superscripts. Typical drops
			// are 20-30% of font size (e.g., 7.98 vs 9.96 baseline ≈
			// 2.5 pt). A 5 pt tolerance keeps the subscript glyphs
			// sorted in document order with the baseline run instead
			// of forming a separate "row" that the gap heuristic then
			// reads as a word break ("0 -Tl" → "0 -T l"). Real line
			// breaks are 10+ pt (line height ≈ 1.2 × font size for
			// 10+ pt fonts), well above this threshold.
			const sameRowDY = 5.0
			if ewb[i].Y > ewb[j].Y+sameRowDY {
				return true
			} else if ewb[i].Y < ewb[j].Y-sameRowDY {
				return false
			}
			return ewb[i].X < ewb[j].X
		})
	}

	// Group sorted elements into lines.
	type lineGroup struct {
		elements []elementWithBead
	}
	var lines []lineGroup
	currentLine := lineGroup{elements: []elementWithBead{ewb[0]}}

	for i := 1; i < len(ewb); i++ {
		if i%500 == 0 {
			if ctx.Err() != nil {
				return ""
			}
		}
		prev := ewb[i-1]
		curr := ewb[i]

		isNewLine := false
		switch {
		case prev.BeadIndex != curr.BeadIndex:
			isNewLine = true
		case isVerticalPage:
			dx := prev.X - curr.X
			if dx > 0.5*curr.FontSize || dx < -0.5*curr.FontSize {
				isNewLine = true
			}
		default:
			dy := prev.Y - curr.Y
			if dy > 0.5*curr.FontSize || dy < -0.5*curr.FontSize {
				isNewLine = true
			}
		}

		if isNewLine {
			lines = append(lines, currentLine)
			currentLine = lineGroup{elements: []elementWithBead{curr}}
		} else {
			currentLine.elements = append(currentLine.elements, curr)
		}
	}
	lines = append(lines, currentLine)

	var sb strings.Builder
	for lineIdx, line := range lines {
		texts := make([]string, len(line.elements))
		xPos := make([]float64, len(line.elements))
		indices := make([]int, len(line.elements))
		for i, el := range line.elements {
			texts[i] = el.Text
			xPos[i] = el.X
			indices[i] = i
		}
		indices = reorderBidiIndices(texts, indices, xPos)

		if lineIdx > 0 {
			prevLine := lines[lineIdx-1]
			lastPrev := prevLine.elements[len(prevLine.elements)-1]
			firstCurr := line.elements[0]
			if lastPrev.BeadIndex != firstCurr.BeadIndex {
				sb.WriteString("\n\n")
			} else {
				sb.WriteString("\n")
			}
		}

		// Extract TextElements for threshold computation.
		lineElems := make([]models.TextElement, len(line.elements))
		for j, e := range line.elements {
			lineElems[j] = e.TextElement
		}
		spaceThreshold := computeSpaceThreshold(lineElems, indices)

		for i, idx := range indices {
			if i > 0 {
				prev := line.elements[indices[i-1]].TextElement
				curr := line.elements[idx].TextElement
				sameChunk := prev.Chunk == curr.Chunk && curr.Chunk != 0
				gap := computeLineGap(prev, curr)
				if shouldInsertSpace(prev, gap, spaceThreshold, sameChunk) {
					sb.WriteString(" ")
				}
			}
			sb.WriteString(line.elements[idx].Text)
		}
	}

	return normalizeExtractionUnicode(normalizeSoftHyphens(sb.String()))
}

// computeLineGap computes the inter-glyph gap (delta - prevWidth) for a pair,
// with delta measured along the direction the text advances. Vertical
// writing advances down the page (ISO 32000-1, 9.7.4.3, a negative w1), and
// text rotated by 90° advances up it or, rotated by -90°, down it, so the
// sign of the advance's Y component decides which way a gap is positive.
// PDF.js measures the same advance in the text's own direction
// (src/core/evaluator.js, compareWithLastPosition, which swaps and negates
// the coordinates for each orthogonal rotation).
//
// # Corpus evidence
//
//   - pdfjs issue5747.pdf, govdocs thread_000_000262.pdf and 9 more: text
//     rotated by -90° read every gap as negative, so no word space was ever
//     inserted ("dolorsitamet,").
//   - pdfjs issue20930.pdf, bug1905623.pdf: vertical writing read "ABCDE"
//     for glyphs PDF.js reads "A B C D E".
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
	default:
		delta = curr.X - prev.X
	}
	if prev.Width > 0 {
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
//     the previous glyph's size and scale, its size on the page.
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
func wordSpaceThreshold(elems []models.TextElement, fonts []runFont, p, i int, lineThreshold float64) float64 {
	prev, el := elems[p], elems[i]
	if prev.MCID != el.MCID {
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
