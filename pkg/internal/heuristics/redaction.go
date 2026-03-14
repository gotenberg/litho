package heuristics

import (
	"github.com/gotenberg/litho/pkg/models"
)

// DetectUnappliedRedactions checks each text element against unapplied /Redact
// annotations and marks elements whose position falls inside a redaction zone.
// It returns a copy of the elements slice with the Redacted field set where
// applicable.
func DetectUnappliedRedactions(elements []models.TextElement, annots []models.Annotation) []models.TextElement {
	// Collect redaction zones: use QuadPoints if available, otherwise Rect.
	var zones []redactionZone
	for _, a := range annots {
		if a.Subtype != "Redact" {
			continue
		}
		if len(a.QuadPoints) > 0 {
			for _, qp := range a.QuadPoints {
				zones = append(zones, quadToRect(qp))
			}
		} else {
			zones = append(zones, rectToZone(a.Rect))
		}
	}

	if len(zones) == 0 {
		return elements
	}

	result := make([]models.TextElement, len(elements))
	copy(result, elements)

	for i := range result {
		for _, z := range zones {
			if pointInRect(result[i].X, result[i].Y, z) {
				result[i].Redacted = true
				break
			}
		}
	}

	return result
}

// redactionZone is an axis-aligned bounding box derived from a /Rect or
// /QuadPoints entry.
type redactionZone struct {
	minX, minY, maxX, maxY float64
}

// rectToZone normalises a PDF /Rect array into a redactionZone.
// PDF rects may have coordinates in any order.
func rectToZone(r [4]float64) redactionZone {
	z := redactionZone{minX: r[0], minY: r[1], maxX: r[2], maxY: r[3]}
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
func quadToRect(qp [8]float64) redactionZone {
	z := redactionZone{
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

// pointInRect tests whether a point falls inside the redaction zone.
func pointInRect(x, y float64, z redactionZone) bool {
	return x >= z.minX && x <= z.maxX && y >= z.minY && y <= z.maxY
}
