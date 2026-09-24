package heuristics

import (
	"math"

	"github.com/gotenberg/litho/pkg/models"
)

// glyphDescenderFraction approximates how far glyph ink reaches below the
// text baseline, as a fraction of the font size. It is a detection margin,
// not a value any specification states: a redaction mark is drawn around
// visible ink, so a box anchored exactly at the baseline can sit clear of a
// mark that covers the descender.
const glyphDescenderFraction = 0.2

// DetectUnappliedRedactions checks each text element against unapplied /Redact
// annotations and marks elements whose ink overlaps a redaction zone.
// It returns a copy of the elements slice with the Redacted field set where
// applicable.
//
// Overlap is geometric: an element counts when any part of the box it paints
// meets a zone, not only when its origin does. Text elements reach this
// function coalesced into runs, so an origin-only test cleared a run that
// begins outside the mark and continues underneath it. Failing to report text
// that sits under a mark is the dangerous direction for a leak detector, so
// the box is deliberately generous; it always contains the element origin,
// which keeps every element the origin test caught flagged here too.
func DetectUnappliedRedactions(elements []models.TextElement, annots []models.Annotation) []models.TextElement {
	// Collect redaction zones: use QuadPoints if available, otherwise Rect.
	var zones []Zone
	for _, a := range annots {
		if a.Subtype != "Redact" {
			continue
		}
		if len(a.QuadPoints) > 0 {
			for _, qp := range a.QuadPoints {
				zones = append(zones, quadToRect(qp))
			}
		} else {
			zones = append(zones, ZoneFromRect(a.Rect))
		}
	}

	if len(zones) == 0 {
		return elements
	}

	result := make([]models.TextElement, len(elements))
	copy(result, elements)

	for i := range result {
		box := ElementZone(result[i])
		for _, z := range zones {
			if ZonesOverlap(box, z) {
				result[i].Redacted = true
				break
			}
		}
	}

	return result
}

// Zone is an axis-aligned bounding box derived from a /Rect or
// /QuadPoints entry.
type Zone struct {
	minX, minY, maxX, maxY float64
}

// ZoneFromRect normalises a PDF /Rect array into a [Zone].
// PDF rects may have coordinates in any order.
func ZoneFromRect(r [4]float64) Zone {
	z := Zone{minX: r[0], minY: r[1], maxX: r[2], maxY: r[3]}
	if z.minX > z.maxX {
		z.minX, z.maxX = z.maxX, z.minX
	}
	if z.minY > z.maxY {
		z.minY, z.maxY = z.maxY, z.minY
	}
	return z
}

// quadToRect computes the axis-aligned bounding box of a QuadPoints group.
// A QuadPoints group is 8 floats: x1,y1, x2,y2, x3,y3, x4,y4.
func quadToRect(qp [8]float64) Zone {
	z := Zone{
		minX: qp[0], maxX: qp[0],
		minY: qp[1], maxY: qp[1],
	}
	for i := 2; i+1 < len(qp); i += 2 {
		x, y := qp[i], qp[i+1] //nolint:gosec // qp is a fixed-size [8]float64 array
		if x < z.minX {
			z.minX = x
		}
		if x > z.maxX {
			z.maxX = x
		}
		if y < z.minY {
			z.minY = y
		}
		if y > z.maxY {
			z.maxY = y
		}
	}
	return z
}

// ElementZone returns the axis-aligned bounds of the ink a text element
// paints. The element origin sits on the baseline at the start of the run,
// [models.TextElement.Width] measures the advance along that baseline, and the
// ink rises a font size above it. The returned box always contains the origin.
func ElementZone(el models.TextElement) Zone {
	dirX, dirY := baselineDirection(el)
	perpX, perpY := -dirY, dirX

	// Perpendicular reach on either side of the baseline. Horizontal text
	// rises to the ascender and dips to the descender; vertical text centres
	// its glyphs on the baseline instead.
	near, far := -el.FontSize*glyphDescenderFraction, el.FontSize
	if el.Vertical {
		near, far = -el.FontSize/2, el.FontSize/2
	}

	xs := [4]float64{
		el.X + perpX*near,
		el.X + perpX*far,
		el.X + dirX*el.Width + perpX*near,
		el.X + dirX*el.Width + perpX*far,
	}
	ys := [4]float64{
		el.Y + perpY*near,
		el.Y + perpY*far,
		el.Y + dirY*el.Width + perpY*near,
		el.Y + dirY*el.Width + perpY*far,
	}

	z := Zone{minX: xs[0], maxX: xs[0], minY: ys[0], maxY: ys[0]}
	for i := 1; i < len(xs); i++ {
		z.minX = math.Min(z.minX, xs[i])
		z.maxX = math.Max(z.maxX, xs[i])
		z.minY = math.Min(z.minY, ys[i])
		z.maxY = math.Max(z.maxY, ys[i])
	}
	return z
}

// baselineDirection returns the unit vector pointing along the text baseline
// for an element. Vertical text advances down the Y axis of text space;
// horizontal text advances along its rotation angle. This mirrors the
// convention the typography package uses to coalesce glyphs into runs.
func baselineDirection(el models.TextElement) (float64, float64) {
	if el.Vertical {
		return math.Sin(el.Angle), math.Cos(el.Angle)
	}
	return math.Cos(el.Angle), math.Sin(el.Angle)
}

// ZonesOverlap reports whether two axis-aligned boxes share any area.
// Touching edges count: a mark drawn flush against a glyph still covers it.
func ZonesOverlap(a, b Zone) bool {
	return a.minX <= b.maxX && a.maxX >= b.minX &&
		a.minY <= b.maxY && a.maxY >= b.minY
}

// ZoneBounds returns the zone's edges, so a caller working in another
// coordinate space can carry its corners there. A [Zone] is otherwise opaque:
// build one with [ZoneFromRect] or [ElementZone] and compare with
// [ZonesOverlap] or [ZoneContains].
func ZoneBounds(z Zone) (minX, minY, maxX, maxY float64) {
	return z.minX, z.minY, z.maxX, z.maxY
}

// ZoneContains reports whether outer covers all of inner, edges included.
//
// Overlap is the right test for finding what a region selects, and the wrong
// one for deciding what may be removed: content that comes out whole has to sit
// wholly inside, or removing it takes marks the caller never selected.
func ZoneContains(outer, inner Zone) bool {
	return inner.minX >= outer.minX && inner.maxX <= outer.maxX &&
		inner.minY >= outer.minY && inner.maxY <= outer.maxY
}
