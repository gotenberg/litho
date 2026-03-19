package pdf

// PDFAProfile identifies a specific PDF/A conformance level.
type PDFAProfile string

// PDF/A profile constants covering all ISO 19005 parts and conformance levels.
const (
	PDFA1b PDFAProfile = "PDF/A-1b"
	PDFA1a PDFAProfile = "PDF/A-1a"
	PDFA2b PDFAProfile = "PDF/A-2b"
	PDFA2u PDFAProfile = "PDF/A-2u"
	PDFA2a PDFAProfile = "PDF/A-2a"
	PDFA3b PDFAProfile = "PDF/A-3b"
	PDFA3u PDFAProfile = "PDF/A-3u"
	PDFA3a PDFAProfile = "PDF/A-3a"
	PDFA4  PDFAProfile = "PDF/A-4"
	PDFA4e PDFAProfile = "PDF/A-4e"
	PDFA4f PDFAProfile = "PDF/A-4f"
)

// PDFUAProfile identifies a specific PDF/UA conformance level.
type PDFUAProfile string

// PDF/UA profile constants covering ISO 14289 parts.
const (
	PDFUA1 PDFUAProfile = "PDF/UA-1"
	PDFUA2 PDFUAProfile = "PDF/UA-2"
)

// pdfaPart returns the numeric part (1, 2, 3, 4) of the profile.
func pdfaPart(p PDFAProfile) int {
	switch p {
	case PDFA1b, PDFA1a:
		return 1
	case PDFA2b, PDFA2u, PDFA2a:
		return 2
	case PDFA3b, PDFA3u, PDFA3a:
		return 3
	case PDFA4, PDFA4e, PDFA4f:
		return 4
	}
	return 0
}

// pdfaConformance returns the conformance level character ('a', 'b', 'u', 'e', 'f', or empty).
func pdfaConformance(p PDFAProfile) byte {
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
