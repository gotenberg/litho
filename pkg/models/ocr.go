package models

import "image"

// OCREngine is an integration interface for Optical Character Recognition.
// Developers can inject a wrapper for Tesseract, Google Cloud Vision, or any
// other OCR backend via [github.com/gotenberg/litho/pkg/pdf.WithOCREngine] so
// the engine can automatically extract text from scanned image pages.
// Implementations must be safe for concurrent use.
type OCREngine interface {
	// Extract returns the text recognized in img.
	Extract(img image.Image) (string, error)
}
