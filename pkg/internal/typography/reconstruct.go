package typography

import (
	"context"
	"math"
	"sort"
	"strings"

	"github.com/gotenberg/litho/pkg/internal/semantics"
	"github.com/gotenberg/litho/pkg/models"
)

// ReconstructText takes spatial text elements and reassembles them into a string.
// When no article thread beads are provided, it preserves content stream order
// for maximum fidelity to the original document layout. When beads are provided,
// it sorts elements by bead index to respect explicit multi-column reading order.
func ReconstructText(ctx context.Context, elements []models.TextElement, beads []semantics.Bead) string {
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

	for i := 1; i < len(elements); i++ {
		if i%500 == 0 {
			if ctx.Err() != nil {
				return ""
			}
		}
		prev := elements[i-1]
		el := elements[i]
		newLine := false
		switch {
		case el.Vertical || isOrthogonalRotation(el.Angle):
			// Vertical WMode or 90°/270° rotated text: characters progress
			// along Y, so line breaks are detected by X changes.
			if math.Abs(prev.X-el.X) > 2.0 {
				newLine = true
			}
		case isRotated(el.Angle):
			// For arbitrarily rotated text, project positions onto the
			// perpendicular axis of the text direction to detect line breaks.
			newLine = perpDist(prev, el) > 2.0
		default:
			if math.Abs(prev.Y-el.Y) > 2.0 {
				newLine = true
			}
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
	for lineIdx, ln := range lines {
		if lineIdx > 0 {
			sb.WriteString("\n")
		}

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

		// Phantom anchors: unresolved glyphs reach this point with
		// Text == "". They still occupy layout space, so their
		// positions and widths anchor the gap computation between the
		// surrounding resolved runs. Walk the line, tracking the
		// previous emitted element for the space heuristic, and let
		// phantoms push that anchor forward without contributing
		// characters or triggering space insertion.
		//
		// Chunk segmentation: elements that share a
		// [models.TextElement.Chunk] came from the same content-stream
		// text-operator chain. Small gaps inside a chunk are glyph
		// advancement or kerning adjustments that viewers do not turn
		// into spaces. Large gaps inside a chunk (e.g. a sizeable TJ
		// numeric adjustment) still cross the gap heuristic so the
		// visible word break survives.
		prevIdx := -1
		for _, idx := range indices {
			el := ln.elems[idx]
			if el.Text == "" {
				prevIdx = idx
				continue
			}
			if prevIdx >= 0 {
				prev := ln.elems[prevIdx]
				gap := computeLineGap(prev, el)
				sameChunk := prev.Chunk == el.Chunk && el.Chunk != 0
				if prev.Text != "" && isRunningHeadReset(prev, el, gap) {
					// A running-head reset (page header at the right
					// of a line, then a deliberate Tm reset to the
					// left margin for the title on the same line)
					// drops the trailing space the previous run baked
					// in. The drop is scoped to horizontal LTR text
					// so RTL flow (Hebrew, Arabic) and orthogonal
					// column layouts (90°-rotated body text) keep
					// their literal spaces.
					if b := sb.String(); len(b) > 0 && b[len(b)-1] == ' ' {
						buf := []byte(b)
						sb.Reset()
						sb.Write(buf[:len(buf)-1])
					}
				} else if prev.Text != "" && shouldInsertSpace(prev, gap, spaceThreshold, sameChunk) {
					sb.WriteString(" ")
				}
			}
			// Collapse consecutive whitespace. A producer that bakes
			// two literal spaces into a Tj (one from word-spacing,
			// one from a degenerate Tm reset that places it backward
			// over the previous glyph) would otherwise leak two
			// spaces into the output. Skip el.Text when it is
			// whitespace and the accumulated output already ends in
			// whitespace.
			emit := el.Text
			if isAllWhitespace(emit) {
				if b := sb.String(); len(b) > 0 && isWhitespaceByte(b[len(b)-1]) {
					emit = ""
				}
			}
			sb.WriteString(emit)
			prevIdx = idx
		}
	}

	return normalizeExtractionUnicode(normalizeSoftHyphens(sb.String()))
}

// isRunningHeadReset reports whether the transition from prev to el is
// the kind of large, horizontal backward jump that producers emit to
// place a running head (right-aligned page number) on the same line as
// the following body text. The check requires:
//
//   - Both elements are horizontal LTR (no rotation, no vertical
//     writing mode) so RTL scripts and column layouts are excluded.
//   - The gap is more negative than negativeSpaceFactor × fontSize so
//     normal kerning never qualifies.
//   - The X-position regresses by at least negativeSpaceJumpMin
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

func isAllWhitespace(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isWhitespaceByte(s[i]) {
			return false
		}
	}
	return true
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
			if ewb[i].Y > ewb[j].Y+2.0 {
				return true
			} else if ewb[i].Y < ewb[j].Y-2.0 {
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

// computeLineGap computes the inter-glyph gap (delta - prevWidth) for a pair.
func computeLineGap(prev, curr models.TextElement) float64 {
	var delta float64
	switch {
	case curr.Vertical || isOrthogonalRotation(curr.Angle):
		delta = curr.Y - prev.Y
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
// line's average font size.
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
