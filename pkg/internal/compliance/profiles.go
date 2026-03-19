package compliance

import "github.com/gotenberg/litho/pkg/models"

// PdfaPart returns the numeric part (1, 2, 3, 4) of the profile.
func PdfaPart(p models.PDFAProfile) int {
	switch p {
	case models.PDFA1b, models.PDFA1a:
		return 1
	case models.PDFA2b, models.PDFA2u, models.PDFA2a:
		return 2
	case models.PDFA3b, models.PDFA3u, models.PDFA3a:
		return 3
	case models.PDFA4, models.PDFA4e, models.PDFA4f:
		return 4
	}
	return 0
}

// PdfaConformance returns the conformance level character ('a', 'b', 'u', 'e', 'f', or empty).
func PdfaConformance(p models.PDFAProfile) byte {
	s := string(p)
	if len(s) == 0 {
		return 0
	}
	last := s[len(s)-1]
	switch last {
	case 'a', 'b', 'u', 'e', 'f':
		return last
	}
	// PDF/A-4 (no suffix) — treat as base.
	return 0
}
