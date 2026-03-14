package heuristics

import (
	"github.com/gotenberg/litho/pkg/models"
)

// ocrCoverageThreshold is the minimum fraction of the page area that must be
// covered by image XObjects for the page to be flagged as a scanned image.
const ocrCoverageThreshold = 0.80

// RequiresOCR returns true when a page contains no digital text but has one or
// more large Image XObjects that cover at least 80% of the page's physical
// bounding box. This is the hallmark of a "dead" scanned PDF.
func RequiresOCR(elements []models.TextElement, images []models.ImageMeta, pageWidth, pageHeight float64) bool {
	// If the page has any visible text, no OCR is needed.
	for _, el := range elements {
		if el.Text != "" && !el.Hidden && !el.Invisible {
			return false
		}
	}

	if pageWidth <= 0 || pageHeight <= 0 {
		return false
	}

	if len(images) == 0 {
		return false
	}

	pageArea := pageWidth * pageHeight
	var totalImageArea float64
	for _, img := range images {
		totalImageArea += float64(img.Width) * float64(img.Height)
	}

	// Image pixel dimensions aren't directly comparable to page point
	// dimensions. However, for OCR triage we only care about the ratio:
	// if the image is "big enough" relative to the page, it's a scan.
	// We compare the largest image dimension against the page dimension
	// to see if at least one image approximately fills the page.
	//
	// A more precise approach would use the CTM to map image space to
	// page space, but for a heuristic this is sufficient: we check if
	// any single image covers ≥80% of the page area by assuming the
	// image is rendered at its natural aspect ratio.
	for _, img := range images {
		if img.Width <= 0 || img.Height <= 0 {
			continue
		}

		imgAspect := float64(img.Width) / float64(img.Height)
		pageAspect := pageWidth / pageHeight

		// Estimate the rendered dimensions assuming the image is scaled
		// to fit within the page (which is the overwhelmingly common
		// case for scanned documents).
		var renderedW, renderedH float64
		if imgAspect > pageAspect {
			// Image is wider relative to page — width-limited.
			renderedW = pageWidth
			renderedH = pageWidth / imgAspect
		} else {
			// Image is taller relative to page — height-limited.
			renderedH = pageHeight
			renderedW = pageHeight * imgAspect
		}

		coverage := (renderedW * renderedH) / pageArea
		if coverage >= ocrCoverageThreshold {
			return true
		}
	}

	// Fallback: check if the total image area (in pixel space) vs. page
	// area (in point space) suggests a large scan. For typical scanned
	// documents at 150-600 DPI, image pixel counts are much larger than
	// page point counts, so any image with > 10x the page area (in raw
	// numbers) is almost certainly a full-page scan.
	if totalImageArea > pageArea*10 {
		return true
	}

	return false
}
