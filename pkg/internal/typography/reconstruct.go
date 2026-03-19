package typography

import (
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
func ReconstructText(elements []models.TextElement, beads []semantics.Bead) string {
	if len(elements) == 0 {
		return ""
	}

	// When beads are present, use bead-aware spatial sorting.
	if len(beads) > 0 {
		return reconstructWithBeads(elements, beads)
	}

	// Content stream order: group elements into lines, apply bidi reordering
	// per line, then join with newlines and inferred spaces.
	type line struct {
		elems []models.TextElement
	}
	var lines []line
	cur := line{elems: []models.TextElement{elements[0]}}

	for i := 1; i < len(elements); i++ {
		prev := elements[i-1]
		el := elements[i]
		newLine := false
		if el.Vertical {
			if math.Abs(prev.X-el.X) > 2.0 {
				newLine = true
			}
		} else if isRotated(el.Angle) {
			// For rotated text, project positions onto the perpendicular axis
			// of the text direction to detect line breaks.
			newLine = perpDist(prev, el) > 2.0
		} else {
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

		// Bidi reorder within the line.
		chars := make([]string, len(ln.elems))
		for i, el := range ln.elems {
			chars[i] = el.Text
		}
		reordered := reorderBidiLine(chars)

		for i, ch := range reordered {
			if i > 0 {
				prev := ln.elems[i-1]
				curr := ln.elems[i]
				if curr.Vertical {
					if needsSpace(prev.Y-curr.Y, prev.Width, curr.FontSize) {
						sb.WriteString(" ")
					}
				} else if isRotated(curr.Angle) {
					// For rotated text, measure distance along the text direction.
					if needsSpace(alongDist(prev, curr), prev.Width, curr.FontSize) {
						sb.WriteString(" ")
					}
				} else {
					if needsSpace(curr.X-prev.X, prev.Width, curr.FontSize) {
						sb.WriteString(" ")
					}
				}
			}
			sb.WriteString(ch)
		}
	}

	return normalizeSoftHyphens(sb.String())
}

// reconstructWithBeads handles the bead-aware spatial sorting path for PDFs
// that have article threads defining explicit reading order.
func reconstructWithBeads(elements []models.TextElement, beads []semantics.Bead) string {
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
		prev := ewb[i-1]
		curr := ewb[i]

		isNewLine := false
		if prev.BeadIndex != curr.BeadIndex {
			isNewLine = true
		} else if isVerticalPage {
			dx := prev.X - curr.X
			if dx > 0.5*curr.FontSize || dx < -0.5*curr.FontSize {
				isNewLine = true
			}
		} else {
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
		chars := make([]string, len(line.elements))
		for i, el := range line.elements {
			chars[i] = el.Text
		}
		reordered := reorderBidiLine(chars)

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

		for i, ch := range reordered {
			if i > 0 {
				prev := line.elements[i-1]
				curr := line.elements[i]
				if isVerticalPage {
					if needsSpace(prev.Y-curr.Y, prev.Width, curr.FontSize) {
						sb.WriteString(" ")
					}
				} else {
					if needsSpace(curr.X-prev.X, prev.Width, curr.FontSize) {
						sb.WriteString(" ")
					}
				}
			}
			sb.WriteString(ch)
		}
	}

	return normalizeSoftHyphens(sb.String())
}

// needsSpace decides whether to insert a space between two consecutive glyphs.
// delta is the distance between the two glyphs along the reading direction
// (curr.X - prev.X for horizontal, prev.Y - curr.Y for vertical).
// prevWidth is the advance width of the previous glyph (0 if unknown).
func needsSpace(delta, prevWidth, fontSize float64) bool {
	if prevWidth > 0 {
		// Use actual glyph width: space exists when the gap after the glyph
		// exceeds a fraction of the font size. A typical inter-word space is
		// ~0.25 em; TJ kerning adjustments rarely exceed 0.1 em in practice.
		// Use 0.15 em as the threshold to catch word gaps while absorbing kerning.
		gap := delta - prevWidth
		return gap > 0.15*fontSize
	}
	// Fallback when Width is not set: use a heuristic.
	// Expected advance is ~0.5*fontSize; a gap well beyond that is a space.
	// Use a higher threshold (0.3) to avoid false positives from kerning.
	expectedAdvance := 0.5 * fontSize
	gap := delta - expectedAdvance
	return gap > 0.3*fontSize
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
