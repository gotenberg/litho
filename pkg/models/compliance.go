package models

// PDFAProfile identifies a specific PDF/A conformance level.
// Values are immutable and safe for concurrent use.
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
// Values are immutable and safe for concurrent use.
type PDFUAProfile string

// PDF/UA profile constants covering ISO 14289 parts.
const (
	PDFUA1 PDFUAProfile = "PDF/UA-1"
	PDFUA2 PDFUAProfile = "PDF/UA-2"
)

// String returns the canonical string form of the profile, e.g.
// "PDF/A-2b". The zero value renders as the empty string.
func (p PDFAProfile) String() string { return string(p) }

// IsValid reports whether p matches one of the documented PDF/A
// profile constants. Use this to validate user input before passing
// the value to validators or remediation engines that assume a known
// profile.
func (p PDFAProfile) IsValid() bool {
	switch p {
	case PDFA1a, PDFA1b,
		PDFA2a, PDFA2b, PDFA2u,
		PDFA3a, PDFA3b, PDFA3u,
		PDFA4, PDFA4e, PDFA4f:
		return true
	}
	return false
}

// String returns the canonical string form of the profile, e.g.
// "PDF/UA-1". The zero value renders as the empty string.
func (p PDFUAProfile) String() string { return string(p) }

// IsValid reports whether p matches one of the documented PDF/UA
// profile constants. Use this to validate user input before passing
// the value to validators or remediation engines that assume a known
// profile.
func (p PDFUAProfile) IsValid() bool {
	switch p {
	case PDFUA1, PDFUA2:
		return true
	}
	return false
}
