package pdf

import "github.com/gotenberg/litho/pkg/internal/compliance"

// PDFAProfile identifies a specific PDF/A conformance level.
type PDFAProfile = compliance.PDFAProfile

// PDF/A profile constants covering all ISO 19005 parts and conformance levels.
const (
	PDFA1b = compliance.PDFA1b
	PDFA1a = compliance.PDFA1a
	PDFA2b = compliance.PDFA2b
	PDFA2u = compliance.PDFA2u
	PDFA2a = compliance.PDFA2a
	PDFA3b = compliance.PDFA3b
	PDFA3u = compliance.PDFA3u
	PDFA3a = compliance.PDFA3a
	PDFA4  = compliance.PDFA4
	PDFA4e = compliance.PDFA4e
	PDFA4f = compliance.PDFA4f
)

// PDFUAProfile identifies a specific PDF/UA conformance level.
type PDFUAProfile = compliance.PDFUAProfile

// PDF/UA profile constants covering ISO 14289 parts.
const (
	PDFUA1 = compliance.PDFUA1
	PDFUA2 = compliance.PDFUA2
)
