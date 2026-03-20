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

	// Check if any single image approximately fills the page. We estimate
	// rendered dimensions by assuming the image is scaled to fit within
	// the page (the overwhelmingly common case for scanned documents).
	// A more precise approach would use the CTM, but for a heuristic
	// this is sufficient.
	for _, img := range images {
		if img.Width <= 0 || img.Height <= 0 {
			continue
		}

		imgAspect := float64(img.Width) / float64(img.Height)
		pageAspect := pageWidth / pageHeight

		var renderedW, renderedH float64
		if imgAspect > pageAspect {
			renderedW = pageWidth
			renderedH = pageWidth / imgAspect
		} else {
			renderedH = pageHeight
			renderedW = pageHeight * imgAspect
		}

		coverage := (renderedW * renderedH) / pageArea
		if coverage >= ocrCoverageThreshold {
			return true
		}
	}

	return false
}
